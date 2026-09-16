package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallHook installs a hook without overwriting silently: if existing hook
// exists and is not Across-owned, chain it (rename to *.across-orig and call through).
func InstallHook(hooksDir, name, content string) error {
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("bad hook name")
	}
	// symlink safety: hooksDir must not escape via symlink
	fi, err := os.Lstat(hooksDir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink hooks dir")
	}
	p := filepath.Join(hooksDir, name)
	if st, err := os.Lstat(p); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink hook")
		}
		existing, _ := os.ReadFile(p)
		if strings.Contains(string(existing), "Across") {
			// already ours; overwrite
		} else {
			// chain: preserve original
			if err := os.Rename(p, p+".across-orig"); err != nil {
				return fmt.Errorf("preserve original hook: %w", err)
			}
			content = content + "\n# chained original hook\nif [ -x \"" + p + ".across-orig\" ]; then exec \"" + p + ".across-orig\" \"$@\"; fi\n"
		}
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(p, 0o755)
}
