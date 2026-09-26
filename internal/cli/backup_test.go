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
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "plugins")); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackup(&cobra.Command{}, archive, target); err != nil {
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

func TestRestoreRejectsSymlinkedTargetParent(t *testing.T) {
	snapshot := validBackupSnapshot(t)
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: snapshot, listed: true}})
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(link, "home")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("old-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackup(&cobra.Command{}, archive, target); err == nil {
		t.Fatal("restore beneath a symlinked target parent was accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "old-state" {
		t.Fatalf("old target changed: %q %v", data, err)
	}
}

func TestRestoreRejectsInvalidSnapshotAndPreservesTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("old-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := writeBackupFixture(t, []backupFixtureEntry{{name: backupSnapshotName, data: []byte("not sqlite"), listed: true}})
	err := restoreBackup(&cobra.Command{}, archive, target)
	if err == nil || !strings.Contains(err.Error(), "candidate SQLite") {
		t.Fatalf("expected candidate validation failure, got %v", err)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "old-state" {
		t.Fatalf("old target changed: %q %v", data, readErr)
	}
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
