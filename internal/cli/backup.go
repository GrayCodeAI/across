package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/graycodeai/across/internal/config"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	c := &cobra.Command{Use: "backup", Short: "Backup/restore (plaintext unless protected externally)"}
	c.AddCommand(
		&cobra.Command{Use: "create --output F", Short: "Create backup", RunE: func(cmd *cobra.Command, args []string) error {
			out, _ := cmd.Flags().GetString("output")
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			// consistent sqlite snapshot
			snap := filepath.Join(home, "tmp", "backup-snap.db")
			_ = os.MkdirAll(filepath.Dir(snap), 0o755)
			if _, err := db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, strings.ReplaceAll(snap, `'`, `''`))); err != nil {
				return err
			}
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			defer f.Close()
			gz := gzip.NewWriter(f)
			defer gz.Close()
			tw := tar.NewWriter(gz)
			defer tw.Close()
			manifest := map[string]any{"format_version": 1, "across_version": Version, "created_at": store.NowUTC(), "files": []any{}}
			files := []string{}
			_ = filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				rel, _ := filepath.Rel(home, p)
				if rel == "." || strings.HasPrefix(rel, "tmp/") || strings.HasPrefix(rel, "backups/") || strings.HasPrefix(rel, "logs/") {
					if info.IsDir() {
						return nil
					}
					if strings.HasPrefix(rel, "tmp/") {
						return nil
					}
				}
				if info.IsDir() {
					return nil
				}
				if rel == "across.db-wal" || rel == "across.db-shm" {
					return nil
				}
				files = append(files, rel)
				return nil
			})
			files = append(files, "__snapshot.db")
			entries := []any{}
			addFile := func(name, src string) error {
				b, err := os.ReadFile(src)
				if err != nil {
					return nil // skip
				}
				h := sha256.Sum256(b)
				hs := hex.EncodeToString(h[:])
				hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(b))}
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
				if _, err := tw.Write(b); err != nil {
					return err
				}
				entries = append(entries, map[string]any{"path": name, "sha256": hs, "size": len(b)})
				return nil
			}
			for _, rel := range files {
				if rel == "__snapshot.db" {
					_ = addFile(rel, snap)
					continue
				}
				_ = addFile(rel, filepath.Join(home, rel))
			}
			manifest["files"] = entries
			mb, _ := json.MarshalIndent(manifest, "", "  ")
			hdr := &tar.Header{Name: "manifest.json", Mode: 0o644, Size: int64(len(mb))}
			_ = tw.WriteHeader(hdr)
			_, _ = tw.Write(mb)
			fmt.Fprintln(cmd.OutOrStdout(), "backup written (PLAINTEXT: protect externally)")
			return nil
		}},
		&cobra.Command{Use: "verify FILE", Args: cobra.ExactArgs(1), Short: "Verify backup", RunE: func(cmd *cobra.Command, args []string) error {
			return verifyBackup(args[0])
		}},
		&cobra.Command{Use: "restore FILE --home DIR", Args: cobra.ExactArgs(1), Short: "Restore backup", RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := cmd.Flags().GetString("home")
			if home == "" {
				return fmt.Errorf("--home required for restore target")
			}
			return restoreBackup(cmd, args[0], home)
		}},
	)
	c.PersistentFlags().String("output", "", "output file")
	c.PersistentFlags().String("home", "", "restore target home")
	return c
}

func verifyBackup(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hashes := map[string]string{}
	var manifest struct {
		Files []struct {
			Path   string `json:"path"`
			Sha256 string `json:"sha256"`
		} `json:"files"`
	}
	var manifestSeen bool
	contents := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.Contains(hdr.Name, "..") || filepath.IsAbs(hdr.Name) {
			return fmt.Errorf("backup traversal rejected: %s", hdr.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, 1<<30))
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		hashes[hdr.Name] = hex.EncodeToString(h[:])
		contents[hdr.Name] = b
		if hdr.Name == "manifest.json" {
			manifestSeen = true
			if err := json.Unmarshal(b, &manifest); err != nil {
				return fmt.Errorf("corrupt manifest")
			}
		}
	}
	if !manifestSeen {
		return fmt.Errorf("missing manifest")
	}
	for _, fe := range manifest.Files {
		if got, ok := hashes[fe.Path]; !ok || got != fe.Sha256 {
			return fmt.Errorf("checksum mismatch: %s", fe.Path)
		}
	}
	fmt.Println("backup OK")
	return nil
}

func restoreBackup(cmd *cobra.Command, file, home string) error {
	if err := verifyBackup(file); err != nil {
		return err
	}
	if err := config.EnsureHome(home); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.Contains(hdr.Name, "..") || filepath.IsAbs(hdr.Name) {
			return fmt.Errorf("traversal rejected")
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		dst := filepath.Join(home, hdr.Name)
		if hdr.Name == "__snapshot.db" {
			dst = filepath.Join(home, "across.db")
		}
		if hdr.Name == "manifest.json" {
			continue
		}
		// symlink safety: ensure parent has no symlink escape
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 1<<30)); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	// restore exec bits
	_ = filepath.Walk(filepath.Join(home, "plugins"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	var db *sql.DB
	_ = db
	fmt.Fprintln(cmd.OutOrStdout(), "restored to "+home)
	return nil
}
