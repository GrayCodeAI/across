package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type CLIError struct {
	Code    string
	Message string
	Err     error
}

func (e *CLIError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

func invalidArgument(format string, args ...any) error {
	return &CLIError{Code: "invalid_argument", Message: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &CLIError{Code: "not_found", Message: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &CLIError{Code: "conflict", Message: fmt.Sprintf(format, args...)}
}

func operationFailed(format string, args ...any) error {
	return &CLIError{Code: "operation_failed", Message: fmt.Sprintf(format, args...)}
}

func internalError(err error) error {
	return &CLIError{Code: "internal", Message: "internal error", Err: err}
}

func requiredFlags(names ...string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return validateRequiredFlagNames(cmd, names)
	}
}

func requireExistingFile(path string) (string, error) {
	if path == "" {
		return "", invalidArgument("file path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", notFound("file not found: %s", path)
		}
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", invalidArgument("path must be a regular file: %s", path)
	}
	return resolved, nil
}

func requireExistingDirectory(path string) (string, error) {
	if path == "" {
		return "", invalidArgument("directory path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", notFound("directory not found: %s", path)
		}
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", invalidArgument("path must be a directory: %s", path)
	}
	return resolved, nil
}

func requireDirectoryChain(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := abs
	for {
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return invalidArgument("directory path must not contain symbolic links: %s", path)
		}
		if !info.IsDir() {
			return invalidArgument("path is not a directory: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func requireOutputFile(path string) (string, error) {
	if path == "" {
		return "", invalidArgument("output path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := requireExistingDirectory(filepath.Dir(abs)); err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", invalidArgument("output path must not be a symbolic link: %s", path)
		}
		if info.IsDir() {
			return "", invalidArgument("output path must not be a directory: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return abs, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func requireNonEmptyValue(name, value string) error {
	if value == "" {
		return invalidArgument("%s must not be empty", name)
	}
	return nil
}

func requireSafeName(name string) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return invalidArgument("name must be a single path component")
	}
	return nil
}

func requireOneOf(flag, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return invalidArgument("--%s must be one of %s", flag, strings.Join(allowed, ", "))
}

func configureCommandContracts(root *cobra.Command) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() {
			args := cmd.Args
			if args == nil {
				args = cobra.NoArgs
			}
			cmd.Args = func(cmd *cobra.Command, values []string) error {
				allowEmpty := cmd.Name() == "run" && cmd.Parent() != nil && (cmd.Parent().Name() == "plugin" || cmd.Parent().Name() == "verify")
				for _, value := range values {
					if value == "" && !allowEmpty {
						return invalidArgument("positional arguments must not be empty")
					}
				}
				if err := args(cmd, values); err != nil {
					return invalidArgument("%s", err)
				}
				return nil
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func validateRequiredFlagNames(cmd *cobra.Command, names []string) error {
	for _, name := range names {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			return internalError(fmt.Errorf("required flag %q is not defined", name))
		}
		if !flag.Changed {
			return invalidArgument("--%s is required", name)
		}
		value, err := cmd.Flags().GetString(name)
		if err != nil {
			return internalError(err)
		}
		if strings.TrimSpace(value) == "" {
			return invalidArgument("--%s must not be empty", name)
		}
	}
	return nil
}

func WrapError(err error) error {
	if err == nil {
		return nil
	}
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return err
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not found") || strings.Contains(message, "no mirror for repo") {
		return notFound("%s", err)
	}
	if strings.Contains(message, "unknown command") || strings.Contains(message, "unknown flag") || strings.Contains(message, "unknown shorthand") || strings.Contains(message, "flag needs an argument") || strings.Contains(message, "invalid argument") {
		return invalidArgument("%s", err)
	}
	return operationFailed("%s", err)
}

func ExitCode(err error) int {
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		return 5
	}
	switch cliErr.Code {
	case "invalid_argument":
		return 2
	case "not_found":
		return 3
	case "conflict":
		return 4
	case "operation_failed":
		return 5
	default:
		return 1
	}
}

func FormatError(err error) string {
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return "across: " + cliErr.Code + ": " + cliErr.Message
	}
	return "across: operation_failed: " + err.Error()
}
