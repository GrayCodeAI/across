package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/graycodeai/across/internal/config"
	"github.com/graycodeai/across/internal/store"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
)

const (
	backupManifestName          = "manifest.json"
	backupSnapshotName          = "__snapshot.db"
	maxBackupArchiveSize  int64 = 1 << 30
	maxBackupInputSize    int64 = 2 << 30
	maxBackupMemberSize   int64 = 256 << 20
	maxBackupManifestSize int64 = 8 << 20
	maxBackupMembers            = 10000
)

var errBackupArchiveTooLarge = errors.New("backup archive exceeds size limit")

type backupManifest struct {
	FormatVersion int                   `json:"format_version"`
	AcrossVersion string                `json:"across_version"`
	CreatedAt     string                `json:"created_at"`
	Files         []backupManifestEntry `json:"files"`
}

type backupManifestEntry struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   *int64 `json:"mode,omitempty"`
}

type backupMember struct {
	name   string
	size   int64
	sha256 string
	mode   os.FileMode
}

type backupSource struct {
	name string
	path string
}

func newBackupCmd() *cobra.Command {
	c := &cobra.Command{Use: "backup", Short: "Backup/restore (plaintext unless protected externally)"}
	c.AddCommand(
		&cobra.Command{
			Use:     "create --output F",
			Short:   "Create backup",
			PreRunE: requiredFlags("output"),
			RunE: func(cmd *cobra.Command, args []string) error {
				out, _ := cmd.Flags().GetString("output")
				return createBackup(cmd, out)
			},
		},
		&cobra.Command{
			Use:   "verify FILE",
			Args:  cobra.ExactArgs(1),
			Short: "Verify backup",
			RunE: func(cmd *cobra.Command, args []string) error {
				return verifyBackup(args[0])
			},
		},
		&cobra.Command{
			Use:     "restore FILE --target-home DIR",
			Args:    cobra.ExactArgs(1),
			Short:   "Restore backup",
			PreRunE: requiredFlags("target-home"),
			RunE: func(cmd *cobra.Command, args []string) error {
				target, _ := cmd.Flags().GetString("target-home")
				if target == "" {
					return invalidArgument("--target-home required for restore target")
				}
				return restoreBackup(cmd, args[0], target)
			},
		},
	)
	c.PersistentFlags().String("output", "", "output file")
	for _, child := range c.Commands() {
		if child.Name() == "restore" {
			child.Flags().String("target-home", "", "restore target home")
		}
	}
	return c
}

func createBackup(cmd *cobra.Command, output string) error {
	resolved, err := requireOutputFile(output)
	if err != nil {
		return err
	}
	db, home, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	snapshot, err := createSnapshot(db, home)
	if err != nil {
		return err
	}
	defer removeSnapshot(snapshot)

	sources, err := collectBackupSources(home)
	if err != nil {
		return err
	}
	sources = append(sources, backupSource{name: backupSnapshotName, path: snapshot})
	sort.Slice(sources, func(i, j int) bool { return sources[i].name < sources[j].name })

	temporary, err := os.CreateTemp(filepath.Dir(resolved), "."+filepath.Base(resolved)+".across-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryName)
	}()
	if err := writeBackupArchive(temporary, sources); err != nil {
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(temporaryName, resolved); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "backup written (PLAINTEXT: protect externally)")
	return nil
}

func createSnapshot(db *sql.DB, home string) (string, error) {
	dir := filepath.Join(home, "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	placeholder, err := os.CreateTemp(dir, ".backup-snapshot-*")
	if err != nil {
		return "", err
	}
	path := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if _, err := db.Exec("VACUUM INTO " + sqliteString(path)); err != nil {
		removeSnapshot(path)
		return "", err
	}
	return path, nil
}

func removeSnapshot(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-journal")
}

func sqliteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func collectBackupSources(home string) ([]backupSource, error) {
	sources := make([]backupSource, 0)
	err := filepath.Walk(home, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(home, filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if excludedBackupPath(name) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if _, err := validateArchiveName(name); err != nil {
			return err
		}
		sources = append(sources, backupSource{name: name, path: filePath})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

func excludedBackupPath(name string) bool {
	if name == backupManifestName || name == backupSnapshotName || name == "across.db" || strings.HasPrefix(name, "across.db-") {
		return true
	}
	parts := strings.Split(name, "/")
	if len(parts) > 0 {
		switch parts[0] {
		case "tmp", "backups", "logs":
			return true
		}
	}
	return transientBackupPath(name)
}

func transientBackupPath(name string) bool {
	base := path.Base(name)
	return base == "serve.token" || base == ".serve.token" || strings.HasPrefix(base, "serve.token.") || strings.HasPrefix(base, ".serve.token.")
}

func writeBackupArchive(out *os.File, sources []backupSource) error {
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	twClosed := false
	gzClosed := false
	defer func() {
		if !twClosed {
			_ = tw.Close()
		}
		if !gzClosed {
			_ = gz.Close()
		}
	}()

	entries := make([]backupManifestEntry, 0, len(sources))
	if len(sources)+1 > maxBackupMembers {
		return fmt.Errorf("backup member count exceeds limit")
	}
	var totalSize int64
	for _, source := range sources {
		entry, err := addBackupFile(tw, source.name, source.path)
		if err != nil {
			return err
		}
		totalSize += entry.size
		if totalSize > maxBackupArchiveSize {
			return fmt.Errorf("backup archive exceeds size limit")
		}
		mode := int64(entry.mode.Perm())
		entries = append(entries, backupManifestEntry{
			Path:   entry.name,
			Sha256: entry.sha256,
			Size:   entry.size,
			Mode:   &mode,
		})
	}
	manifest := backupManifest{
		FormatVersion: 1,
		AcrossVersion: Version,
		CreatedAt:     store.NowUTC(),
		Files:         entries,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(manifestBytes)) > maxBackupManifestSize {
		return fmt.Errorf("backup manifest exceeds size limit")
	}
	manifestHeader := &tar.Header{Name: backupManifestName, Mode: 0o644, Size: int64(len(manifestBytes)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(manifestHeader); err != nil {
		return err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	twClosed = true
	if err := gz.Close(); err != nil {
		return err
	}
	gzClosed = true
	return nil
}

func addBackupFile(tw *tar.Writer, name, source string) (backupMember, error) {
	if _, err := validateArchiveName(name); err != nil {
		return backupMember{}, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return backupMember{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return backupMember{}, fmt.Errorf("backup source is not a regular file: %s", source)
	}
	if info.Size() < 0 || info.Size() > maxBackupMemberSize {
		return backupMember{}, fmt.Errorf("backup member exceeds size limit: %s", name)
	}
	file, err := os.Open(source)
	if err != nil {
		return backupMember{}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return backupMember{}, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || openedInfo.Size() != info.Size() {
		return backupMember{}, fmt.Errorf("backup source changed while reading: %s", source)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	header := &tar.Header{Name: name, Mode: int64(mode), Size: info.Size(), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(header); err != nil {
		return backupMember{}, err
	}
	hash := sha256.New()
	written, err := io.CopyN(io.MultiWriter(tw, hash), file, info.Size())
	if err != nil {
		return backupMember{}, fmt.Errorf("write backup member %s: %w", name, err)
	}
	if written != info.Size() {
		return backupMember{}, fmt.Errorf("short backup member: %s", name)
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return backupMember{}, err
	}
	if afterInfo.Size() != info.Size() {
		return backupMember{}, fmt.Errorf("backup source changed while reading: %s", source)
	}
	return backupMember{
		name:   name,
		size:   info.Size(),
		sha256: hex.EncodeToString(hash.Sum(nil)),
		mode:   mode,
	}, nil
}

func verifyBackup(file string) error {
	resolved, err := requireExistingFile(file)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "across-backup-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := readBackupArchive(resolved, stage); err != nil {
		return err
	}
	fmt.Println("backup OK")
	return nil
}

type backupLimitReader struct {
	source    io.Reader
	remaining int64
}

func (r *backupLimitReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var one [1]byte
		n, err := r.source.Read(one[:])
		if n > 0 {
			return 0, errBackupArchiveTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.source.Read(p)
	if int64(n) > r.remaining {
		return 0, errBackupArchiveTooLarge
	}
	r.remaining -= int64(n)
	return n, err
}

func readBackupArchive(file, stage string) error {
	resolved, err := requireExistingFile(file)
	if err != nil {
		return err
	}
	inputInfo, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if inputInfo.Size() < 0 || inputInfo.Size() > maxBackupInputSize {
		return fmt.Errorf("backup input exceeds size limit")
	}
	input, err := os.Open(resolved)
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	limited := &backupLimitReader{source: gz, remaining: maxBackupArchiveSize}
	tr := tar.NewReader(limited)
	members := make(map[string]backupMember)
	stagedNames := make(map[string]struct{})
	var memberBytes int64
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			if errors.Is(nextErr, errBackupArchiveTooLarge) {
				return errBackupArchiveTooLarge
			}
			return nextErr
		}
		if len(members) >= maxBackupMembers {
			return fmt.Errorf("backup member count exceeds limit")
		}
		name, err := validateArchiveName(header.Name)
		if err != nil {
			return err
		}
		if _, exists := members[name]; exists {
			return fmt.Errorf("duplicate backup member: %s", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("special backup member rejected: %s", name)
		}
		if header.Linkname != "" {
			return fmt.Errorf("linked backup member rejected: %s", name)
		}
		if header.Size < 0 || header.Size > maxBackupMemberSize {
			return fmt.Errorf("backup member exceeds size limit: %s", name)
		}
		if header.Mode < 0 {
			return fmt.Errorf("invalid backup member mode: %s", name)
		}
		if name == backupManifestName && header.Size > maxBackupManifestSize {
			return fmt.Errorf("backup manifest exceeds size limit")
		}
		memberBytes += header.Size
		if memberBytes > maxBackupArchiveSize {
			return errBackupArchiveTooLarge
		}
		relativeName := name
		switch name {
		case backupSnapshotName:
			relativeName = "across.db"
		case backupManifestName:
			relativeName = backupManifestName
		default:
			if name == "across.db" || strings.HasPrefix(name, "across.db-") {
				return fmt.Errorf("transient backup member rejected: %s", name)
			}
			if transientBackupPath(name) {
				return fmt.Errorf("transient backup member rejected: %s", name)
			}
		}
		if _, exists := stagedNames[relativeName]; exists {
			return fmt.Errorf("duplicate backup member: %s", name)
		}
		destination, err := stageDestination(stage, relativeName, stagedNames)
		if err != nil {
			return err
		}
		mode := os.FileMode(header.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.CopyN(io.MultiWriter(out, hash), tr, header.Size)
		if copyErr != nil {
			_ = out.Close()
			_ = os.Remove(destination)
			return fmt.Errorf("read backup member %s: %w", name, copyErr)
		}
		if written != header.Size {
			_ = out.Close()
			_ = os.Remove(destination)
			return fmt.Errorf("short backup member: %s", name)
		}
		if err := out.Chmod(mode); err != nil {
			_ = out.Close()
			_ = os.Remove(destination)
			return err
		}
		if err := out.Sync(); err != nil {
			_ = out.Close()
			_ = os.Remove(destination)
			return err
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(destination)
			return err
		}
		stagedNames[relativeName] = struct{}{}
		members[name] = backupMember{
			name:   name,
			size:   header.Size,
			sha256: hex.EncodeToString(hash.Sum(nil)),
			mode:   mode,
		}
	}
	if _, ok := members[backupManifestName]; !ok {
		return fmt.Errorf("missing manifest")
	}
	var extra [1]byte
	if n, err := limited.Read(extra[:]); n > 0 {
		return fmt.Errorf("trailing backup data")
	} else if err != nil && err != io.EOF {
		if errors.Is(err, errBackupArchiveTooLarge) {
			return errBackupArchiveTooLarge
		}
		return err
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return err
	}
	manifestBytes, err := readStagedFile(filepath.Join(stage, backupManifestName), maxBackupManifestSize)
	if err != nil {
		return err
	}
	var manifest backupManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("corrupt manifest: %w", err)
	}
	if manifest.FormatVersion != 1 {
		return fmt.Errorf("unsupported backup format version: %d", manifest.FormatVersion)
	}
	listed := make(map[string]struct{}, len(manifest.Files))
	for _, entry := range manifest.Files {
		name, err := validateArchiveName(entry.Path)
		if err != nil {
			return err
		}
		if name == backupManifestName || name == "across.db" || strings.HasPrefix(name, "across.db-") || transientBackupPath(name) {
			return fmt.Errorf("invalid manifest member: %s", name)
		}
		if _, exists := listed[name]; exists {
			return fmt.Errorf("duplicate manifest member: %s", name)
		}
		if entry.Size < 0 || entry.Size > maxBackupMemberSize {
			return fmt.Errorf("invalid manifest member size: %s", name)
		}
		digest := strings.ToLower(strings.TrimSpace(entry.Sha256))
		if len(digest) != sha256.Size*2 {
			return fmt.Errorf("invalid manifest checksum: %s", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("invalid manifest checksum: %s", name)
		}
		member, ok := members[name]
		if !ok {
			return fmt.Errorf("missing manifest member: %s", name)
		}
		if member.size != entry.Size || member.sha256 != digest {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
		if entry.Mode != nil {
			if *entry.Mode < 0 || *entry.Mode > 0o777 || os.FileMode(*entry.Mode) != member.mode {
				return fmt.Errorf("mode mismatch: %s", name)
			}
		}
		listed[name] = struct{}{}
	}
	for name := range members {
		if name == backupManifestName {
			continue
		}
		if _, ok := listed[name]; !ok {
			return fmt.Errorf("unlisted backup member: %s", name)
		}
	}
	if _, ok := listed[backupSnapshotName]; !ok {
		return fmt.Errorf("missing SQLite snapshot")
	}
	return nil
}

func validateArchiveName(name string) (string, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 || strings.Contains(name, "\\") || strings.Contains(name, ":") {
		return "", fmt.Errorf("invalid backup member name: %q", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("invalid backup member name: %q", name)
		}
	}
	if strings.HasPrefix(name, "/") || path.IsAbs(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || (len(name) >= 2 && name[1] == ':') {
		return "", fmt.Errorf("backup traversal rejected: %s", name)
	}
	if path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("backup traversal rejected: %s", name)
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("backup traversal rejected: %s", name)
		}
	}
	return name, nil
}

func stageDestination(root, name string, staged map[string]struct{}) (string, error) {
	cleanName, err := validateArchiveName(name)
	if err != nil {
		return "", err
	}
	parts := strings.Split(cleanName, "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		rel, err := filepath.Rel(root, current)
		if err != nil || !pathWithinArchiveRoot(root, current) {
			return "", fmt.Errorf("backup path escaped staging root: %s", name)
		}
		if _, exists := staged[filepath.ToSlash(rel)]; exists {
			return "", fmt.Errorf("backup path conflicts with member: %s", name)
		}
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("backup staging path is a symlink: %s", name)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("backup path parent is not a directory: %s", name)
			}
			continue
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return "", err
		}
	}
	destination := filepath.Join(root, filepath.FromSlash(cleanName))
	if !pathWithinArchiveRoot(root, destination) {
		return "", fmt.Errorf("backup path escaped staging root: %s", name)
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", fmt.Errorf("duplicate backup member: %s", name)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return destination, nil
}

func pathWithinArchiveRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readStagedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("staged file exceeds size limit: %s", path)
	}
	return data, nil
}

func restoreBackup(cmd *cobra.Command, file, home string) error {
	resolved, err := requireExistingFile(file)
	if err != nil {
		return err
	}
	target, existed, err := prepareRestoreTarget(home)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".across-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := readBackupArchive(resolved, stage); err != nil {
		return err
	}
	if err := validateSQLiteSnapshot(filepath.Join(stage, "across.db")); err != nil {
		return fmt.Errorf("candidate SQLite snapshot rejected: %w", err)
	}
	if err := config.EnsureHome(stage); err != nil {
		return err
	}
	if err := commitRestore(stage, target, existed); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "restored to "+target)
	return nil
}

func prepareRestoreTarget(home string) (string, bool, error) {
	if strings.TrimSpace(home) == "" {
		return "", false, fmt.Errorf("restore target must not be empty")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", false, err
	}
	abs = filepath.Clean(abs)
	if abs == string(filepath.Separator) {
		return "", false, fmt.Errorf("refusing filesystem root as restore target")
	}
	parent := filepath.Dir(abs)
	if err := config.EnsureDirectory(parent); err != nil {
		return "", false, err
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", false, err
	}
	target := filepath.Join(resolvedParent, filepath.Base(abs))
	info, err := os.Lstat(target)
	existed := true
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, err
		}
		existed = false
	} else {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("restore target must not be a symlink: %s", home)
		}
		if !info.IsDir() {
			return "", false, fmt.Errorf("restore target must be a directory: %s", home)
		}
	}
	return target, existed, nil
}

func validateSQLiteSnapshot(file string) error {
	info, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("snapshot is not a regular file")
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(file)}).String() + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			_ = rows.Close()
			return err
		}
		found = true
		if result != "ok" {
			_ = rows.Close()
			return fmt.Errorf("integrity check failed: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("integrity check returned no result")
	}
	var table string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&table); err != nil {
		return fmt.Errorf("missing schema metadata")
	}
	var version int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("missing schema version")
	}
	return nil
}

func commitRestore(stage, target string, existed bool) error {
	parent := filepath.Dir(target)
	currentInfo, err := os.Lstat(target)
	if err == nil {
		if currentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore target changed to a symlink")
		}
		if !currentInfo.IsDir() {
			return fmt.Errorf("restore target changed to a non-directory")
		}
		existed = true
	} else if os.IsNotExist(err) {
		existed = false
	} else {
		return err
	}
	if !existed {
		if err := os.Rename(stage, target); err != nil {
			return fmt.Errorf("commit restore: %w", err)
		}
		syncRestoreParent(parent)
		return nil
	}
	old, err := unusedPath(parent, "."+filepath.Base(target)+".across-old-*")
	if err != nil {
		return err
	}
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("stage old restore target: %w", err)
	}
	if err := os.Rename(stage, target); err != nil {
		if rollbackErr := os.Rename(old, target); rollbackErr != nil {
			return fmt.Errorf("commit restore: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("commit restore: %w", err)
	}
	_ = os.RemoveAll(old)
	syncRestoreParent(parent)
	return nil
}

func unusedPath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func syncRestoreParent(parent string) {
	directory, err := os.Open(parent)
	if err != nil {
		return
	}
	_ = directory.Sync()
	_ = directory.Close()
}
