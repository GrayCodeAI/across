package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

type backupFixtureEntry struct {
	name     string
	data     []byte
	listed   bool
	typeflag byte
	linkname string
	mode     int64
}

func TestBackupVerifyRejectsTraversal(t *testing.T) {
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: "../evil", data: []byte("bad"), listed: false}})
	err := verifyBackup(archive)
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestBackupVerifyRejectsUnlistedMember(t *testing.T) {
	snapshot := validBackupSnapshot(t)
	archive := writeBackupFixture(t, []backupFixtureEntry{
		{name: backupSnapshotName, data: snapshot, listed: true},
		{name: "unlisted.txt", data: []byte("extra"), listed: false},
	})
	err := verifyBackup(archive)
	if err == nil || !strings.Contains(err.Error(), "unlisted") {
		t.Fatalf("expected unlisted member rejection, got %v", err)
	}
}

func TestBackupVerifyRejectsDuplicateAndSpecialMembers(t *testing.T) {
	tests := []struct {
		name    string
		entries []backupFixtureEntry
		match   string
	}{
		{
			name: "duplicate",
			entries: []backupFixtureEntry{
				{name: "same.txt", data: []byte("one"), listed: true},
				{name: "same.txt", data: []byte("two"), listed: true},
			},
			match: "duplicate",
		},
		{
			name: "symlink",
			entries: []backupFixtureEntry{
				{name: "link", typeflag: tar.TypeSymlink, linkname: "/outside", listed: false},
			},
			match: "special",
		},
		{
			name: "absolute",
			entries: []backupFixtureEntry{
				{name: "/absolute", data: []byte("bad"), listed: false},
			},
			match: "traversal",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := writeBackupFixture(t, test.entries)
			err := verifyBackup(archive)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("expected %s rejection, got %v", test.match, err)
			}
		})
	}
}

func TestBackupCreateExcludesServeToken(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "serve.token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.tgz")
	if err := executeForTest(t, "--home", home, "backup", "create", "--output", archive); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	if err := readBackupArchive(archive, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(stage, "serve.token")); !os.IsNotExist(err) {
		t.Fatalf("serve token entered backup: %v", err)
	}
}

func TestRestoreDoesNotWriteThroughSymlinkParent(t *testing.T) {
	snapshot := validBackupSnapshot(t)
	archive := writeBackupFixture(t, []backupFixtureEntry{
		{name: backupSnapshotName, data: snapshot, listed: true},
		{name: "plugins/evil", data: []byte("restored"), listed: true},
	})
	outside := t.TempDir()
	target := filepath.Join(t.TempDir(), "home")
	writeAcrossHomeMarker(t, target)
	if err := os.Symlink(outside, filepath.Join(target, "plugins")); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupWithOptions(&cobra.Command{}, archive, target, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil")); !os.IsNotExist(err) {
		t.Fatalf("symlink parent escaped: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "plugins", "evil"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "restored" {
		t.Fatalf("restored data: %q", data)
	}
}

func TestRestoreResolvesSymlinkedTargetParentAndKeepsPreviousHome(t *testing.T) {
	snapshot := validBackupSnapshot(t)
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: snapshot, listed: true}})
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(link, "home")
	writeAcrossHomeMarker(t, target)
	if err := os.WriteFile(filepath.Join(target, "sentinel"), []byte("old-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupWithOptions(&cobra.Command{}, archive, target, true); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSQLiteSnapshot(filepath.Join(resolved, "home", "across.db")); err != nil {
		t.Fatalf("restored home is not at the resolved target: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(link, "home")); err != nil {
		t.Fatalf("target is not reachable through the user's path: %v", err)
	}
	previous := displacedHomes(t, filepath.Join(resolved, "home"))
	if len(previous) != 1 {
		t.Fatalf("previous home not kept: %v", previous)
	}
	data, err := os.ReadFile(filepath.Join(previous[0], "sentinel"))
	if err != nil || string(data) != "old-state" {
		t.Fatalf("previous home changed: %q %v", data, err)
	}
}

func TestRestoreRejectsInvalidSnapshotAndPreservesTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home")
	writeAcrossHomeMarker(t, target)
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("old-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: []byte("not sqlite"), listed: true}})
	err := restoreBackupWithOptions(&cobra.Command{}, archive, target, true)
	if err == nil || !strings.Contains(err.Error(), "candidate SQLite") {
		t.Fatalf("expected candidate validation failure, got %v", err)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "old-state" {
		t.Fatalf("old target changed: %q %v", data, readErr)
	}
	if previous := displacedHomes(t, target); len(previous) != 0 {
		t.Fatalf("failed restore displaced the target: %v", previous)
	}
}

func TestRestoreRefusesNonEmptyForeignTarget(t *testing.T) {
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: validBackupSnapshot(t), listed: true}})
	target := filepath.Join(t.TempDir(), "precious")
	important := filepath.Join(target, "docs", "important.txt")
	if err := os.MkdirAll(filepath.Dir(important), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(important, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		err := restoreBackupWithOptions(&cobra.Command{}, archive, target, force)
		if err == nil || ExitCode(err) != 4 || !strings.Contains(err.Error(), "not an Across home") {
			t.Fatalf("force=%v: foreign target was not refused: %v", force, err)
		}
	}
	data, err := os.ReadFile(important)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("foreign target changed: %q %v", data, err)
	}
	if previous := displacedHomes(t, target); len(previous) != 0 {
		t.Fatalf("foreign target was moved: %v", previous)
	}
}

func TestRestoreRequiresForceToReplaceAcrossHome(t *testing.T) {
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: validBackupSnapshot(t), listed: true}})
	target := filepath.Join(t.TempDir(), "home")
	db, err := store.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = restoreBackupWithOptions(&cobra.Command{}, archive, target, false)
	if err == nil || ExitCode(err) != 4 || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("existing Across home was replaced without --force: %v", err)
	}
	var output strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	if err := restoreBackupWithOptions(cmd, archive, target, true); err != nil {
		t.Fatal(err)
	}
	previous := displacedHomes(t, target)
	if len(previous) != 1 || !strings.Contains(output.String(), previous[0]) {
		t.Fatalf("previous home not kept or not reported: %v %q", previous, output.String())
	}
	if _, err := os.Stat(filepath.Join(previous[0], "across.db")); err != nil {
		t.Fatalf("previous database missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, backupManifestName)); !os.IsNotExist(err) {
		t.Fatalf("backup manifest left in restored home: %v", err)
	}
}

func TestRestoreIntoEmptyDirectory(t *testing.T) {
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: validBackupSnapshot(t), listed: true}})
	target := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupWithOptions(&cobra.Command{}, archive, target, false); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLiteSnapshot(filepath.Join(target, "across.db")); err != nil {
		t.Fatal(err)
	}
	if previous := displacedHomes(t, target); len(previous) != 0 {
		t.Fatalf("empty target left a displaced directory: %v", previous)
	}
}

func writeAcrossHomeMarker(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "across.db"), []byte("old database"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func displacedHomes(t *testing.T, target string) []string {
	t.Helper()
	matches, err := filepath.Glob(target + ".across-old-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func validBackupSnapshot(t *testing.T) []byte {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if _, err := db.Exec("VACUUM INTO " + sqliteString(snapshot)); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeBackupFixture(t *testing.T, entries []backupFixtureEntry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "backup.tgz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	manifestEntries := make([]backupManifestEntry, 0)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{Name: entry.name, Mode: mode, Size: int64(len(entry.data)), Typeflag: typeflag, Linkname: entry.linkname}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg || typeflag == tar.TypeRegA {
			if _, err := tw.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
		if entry.listed && entry.name != backupManifestName {
			digest := sha256.Sum256(entry.data)
			fileMode := mode
			manifestEntries = append(manifestEntries, backupManifestEntry{Path: entry.name, Sha256: hex.EncodeToString(digest[:]), Size: int64(len(entry.data)), Mode: &fileMode})
		}
	}
	manifestBytes, err := json.Marshal(backupManifest{FormatVersion: 1, AcrossVersion: Version, Files: manifestEntries})
	if err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: backupManifestName, Mode: 0o644, Size: int64(len(manifestBytes)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}
