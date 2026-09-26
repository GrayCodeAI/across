package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/graycodeai/across/internal/config"
)

const (
	acrossHookMarker     = "# across-managed-hook: v1"
	acrossOriginalMarker = "# across-original-hook"
)

func InstallHook(hooksDir, name, content string) error {
	directory, err := validateHookDirectory(hooksDir)
	if err != nil {
		return err
	}
	if err := validateHookName(name); err != nil {
		return err
	}
	if strings.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("hook content contains NUL")
	}
	hookPath := filepath.Join(directory, name)
	originalPath := hookPath + ".across-orig"
	originalExists, err := validateOriginalPath(originalPath)
	if err != nil {
		return err
	}
	currentInfo, currentErr := os.Lstat(hookPath)
	if currentErr != nil && !os.IsNotExist(currentErr) {
		return currentErr
	}
	if currentErr == nil {
		if currentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink hook")
		}
		if !currentInfo.Mode().IsRegular() {
			return fmt.Errorf("hook path is not a regular file")
		}
		existing, err := os.ReadFile(hookPath)
		if err != nil {
			return err
		}
		if !isManagedHook(string(existing), originalPath, originalExists) {
			if originalExists {
				return fmt.Errorf("hook ownership state is inconsistent")
			}
			return installWithOriginal(hookPath, originalPath, content)
		}
	} else if originalExists {
		return fmt.Errorf("hook ownership state is inconsistent")
	}
	return replaceManagedHook(hookPath, originalPath, content, originalExists)
}

func UninstallHook(hooksDir, name string) error {
	directory, err := validateHookDirectory(hooksDir)
	if err != nil {
		return err
	}
	if err := validateHookName(name); err != nil {
		return err
	}
	hookPath := filepath.Join(directory, name)
	originalPath := hookPath + ".across-orig"
	originalExists, err := validateOriginalPath(originalPath)
	if err != nil {
		return err
	}
	currentInfo, currentErr := os.Lstat(hookPath)
	if currentErr != nil && !os.IsNotExist(currentErr) {
		return currentErr
	}
	if currentErr == nil {
		if currentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink hook")
		}
		if !currentInfo.Mode().IsRegular() {
			return fmt.Errorf("hook path is not a regular file")
		}
		existing, readErr := os.ReadFile(hookPath)
		if readErr != nil {
			return readErr
		}
		if !isManagedHook(string(existing), originalPath, originalExists) {
			return fmt.Errorf("hook is not Across-owned")
		}
	}
	if currentErr != nil && !originalExists {
		return nil
	}
	if originalExists {
		if currentErr == nil {
			old, unusedErr := unusedHookPath(directory, filepath.Base(hookPath))
			if unusedErr != nil {
				return unusedErr
			}
			if err := os.Rename(hookPath, old); err != nil {
				return err
			}
			if err := os.Rename(originalPath, hookPath); err != nil {
				if rollbackErr := os.Rename(old, hookPath); rollbackErr != nil {
					return fmt.Errorf("restore original hook: %w; rollback failed: %v", err, rollbackErr)
				}
				return err
			}
			_ = os.Remove(old)
			return nil
		}
		return os.Rename(originalPath, hookPath)
	}
	return os.Remove(hookPath)
}

func validateHookDirectory(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("hooks directory must not be empty")
	}
	resolved, err := config.ResolveDirectory(directory)
	if err != nil {
		if strings.Contains(err.Error(), "symbolic links") {
			return "", fmt.Errorf("refusing symlink hooks dir: %w", err)
		}
		return "", err
	}
	return resolved, nil
}

func validateHookName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || strings.Contains(name, ":") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("bad hook name")
	}
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("bad hook name")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("bad hook name")
		}
	}
	return nil
}

func validateOriginalPath(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("refusing symlink original hook")
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("original hook path is not a regular file")
	}
	return true, nil
}

func isManagedHook(content, originalPath string, originalExists bool) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == acrossHookMarker {
			return true
		}
	}
	if originalExists && strings.Contains(content, "# chained original hook") && strings.Contains(content, originalPath) {
		return true
	}
	if strings.Contains(content, "# Across automatic checkpoint hook") || strings.Contains(content, "# Across pre-receive") {
		return true
	}
	return false
}

func installWithOriginal(hookPath, originalPath, content string) error {
	temporary, err := stageHookFile(hookPath, managedHookContent(filepath.Base(hookPath), content, originalPath, true))
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Rename(hookPath, originalPath); err != nil {
		return fmt.Errorf("preserve original hook: %w", err)
	}
	if err := os.Rename(temporary, hookPath); err != nil {
		if rollbackErr := os.Rename(originalPath, hookPath); rollbackErr != nil {
			return fmt.Errorf("install hook: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("install hook: %w", err)
	}
	return nil
}

func replaceManagedHook(hookPath, originalPath, content string, originalExists bool) error {
	temporary, err := stageHookFile(hookPath, managedHookContent(filepath.Base(hookPath), content, originalPath, originalExists))
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	currentInfo, currentErr := os.Lstat(hookPath)
	if currentErr != nil && !os.IsNotExist(currentErr) {
		return currentErr
	}
	if currentErr != nil {
		return os.Rename(temporary, hookPath)
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() {
		return fmt.Errorf("hook path changed to an unsafe file")
	}
	old, err := unusedHookPath(filepath.Dir(hookPath), filepath.Base(hookPath))
	if err != nil {
		return err
	}
	if err := os.Rename(hookPath, old); err != nil {
		return err
	}
	if err := os.Rename(temporary, hookPath); err != nil {
		if rollbackErr := os.Rename(old, hookPath); rollbackErr != nil {
			return fmt.Errorf("upgrade hook: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("upgrade hook: %w", err)
	}
	_ = os.Remove(old)
	return nil
}

func stageHookFile(hookPath, content string) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(hookPath), "."+filepath.Base(hookPath)+".across-new-*")
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	closed := false
	keep := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err := temporary.WriteString(content); err != nil {
		return "", err
	}
	if err := temporary.Chmod(0o755); err != nil {
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	closed = true
	keep = true
	return path, nil
}

func unusedHookPath(directory, name string) (string, error) {
	path, err := os.MkdirTemp(directory, "."+name+".across-old-*")
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func managedHookContent(name, content, originalPath string, originalExists bool) string {
	if !originalExists {
		body := addHookMarker(content)
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return body
	}
	across := strings.TrimSuffix(withoutShebang(content), "\n")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString(acrossHookMarker + "\n")
	b.WriteString(acrossOriginalMarker + "\n")
	b.WriteString("across_original=" + shellQuote(originalPath) + "\n")
	if hookReadsStdin(name) {
		b.WriteString("across_stdin=\"$(mktemp \"${TMPDIR:-/tmp}/across-hook.XXXXXX\")\" || exit 1\n")
		b.WriteString("trap 'rm -f \"$across_stdin\"' EXIT\n")
		b.WriteString("cat > \"$across_stdin\" || exit 1\n")
		b.WriteString("if [ -x \"$across_original\" ]; then\n  \"$across_original\" \"$@\" < \"$across_stdin\" || exit $?\nfi\n")
		b.WriteString("(\n" + across + "\n) < \"$across_stdin\"\n")
		return b.String()
	}
	b.WriteString("(\n" + across + "\n)\n")
	b.WriteString("across_status=$?\n")
	b.WriteString("if [ -x \"$across_original\" ]; then\n  \"$across_original\" \"$@\" || exit $?\nfi\n")
	b.WriteString("exit \"$across_status\"\n")
	return b.String()
}

func hookReadsStdin(name string) bool {
	switch name {
	case "pre-receive", "post-receive", "pre-push", "post-rewrite", "reference-transaction", "proc-receive":
		return true
	}
	return false
}

func withoutShebang(content string) string {
	if !strings.HasPrefix(content, "#!") {
		return content
	}
	if index := strings.IndexByte(content, '\n'); index >= 0 {
		return content[index+1:]
	}
	return ""
}

func addHookMarker(content string) string {
	if strings.Contains(content, acrossHookMarker) {
		return content
	}
	if strings.HasPrefix(content, "#!") {
		if index := strings.IndexByte(content, '\n'); index >= 0 {
			return content[:index+1] + acrossHookMarker + "\n" + content[index+1:]
		}
		return content + "\n" + acrossHookMarker + "\n"
	}
	return acrossHookMarker + "\n" + content
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
