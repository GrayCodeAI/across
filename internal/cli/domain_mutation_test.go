package cli

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func TestDomainMutationRejectsInvalidRevisions(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	if output, err := runMutationCLI(home, "checkpoint", "create", "--repo", repoID, "--revision", "missing-revision"); err == nil || !strings.Contains(FormatError(err), "does not resolve") {
		t.Fatalf("checkpoint revision error: output=%q err=%v", output, err)
	}
	if output, err := runMutationCLI(home, "context", "diff", "--base", "missing-revision", "--head", "HEAD", "--repo", repoID); err == nil || !strings.Contains(FormatError(err), "does not resolve") {
		t.Fatalf("context revision error: output=%q err=%v", output, err)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM checkpoints`); err != nil || count != 0 {
		t.Fatalf("invalid revisions left checkpoints: count=%d err=%v", count, err)
	}
}

func TestDomainMutationRejectsWrongRepositoryAndInactiveSession(t *testing.T) {
	home, repoA, _ := newDomainRepo(t)
	repoB, _ := newDomainRepoInHome(t, home)
	sessionOutput, err := runMutationCLI(home, "session", "start", "--repo", repoB, "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	session := strings.TrimSpace(sessionOutput)
	if output, err := runMutationCLI(home, "checkpoint", "create", "--repo", repoA, "--session", session, "--message", "wrong repo"); err == nil || !strings.Contains(FormatError(err), "belongs to repository") {
		t.Fatalf("wrong-repository session: output=%q err=%v", output, err)
	}
	if _, err := runMutationCLI(home, "session", "close", session); err != nil {
		t.Fatal(err)
	}
	if output, err := runMutationCLI(home, "checkpoint", "create", "--repo", repoB, "--session", session, "--message", "inactive"); err == nil || !strings.Contains(FormatError(err), "not active") {
		t.Fatalf("inactive session: output=%q err=%v", output, err)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM checkpoints`); err != nil || count != 0 {
		t.Fatalf("invalid session left checkpoints: count=%d err=%v", count, err)
	}
}

func TestMemoryRelationshipsAreValidated(t *testing.T) {
	home, repoA, workA := newDomainRepo(t)
	repoB, workB := newDomainRepoInHome(t, home)
	transcriptA := writeRepoTranscript(t, workA, "events.jsonl", `{"type":"UserPrompt","text":"source"}`)
	transcriptB := writeRepoTranscript(t, workB, "events.jsonl", `{"type":"UserPrompt","text":"source"}`)
	sourceAOutput, err := runMutationCLI(home, "source", "import", "--repo", repoA, "--file", transcriptA)
	if err != nil {
		t.Fatal(err)
	}
	sourceBOutput, err := runMutationCLI(home, "source", "import", "--repo", repoB, "--file", transcriptB)
	if err != nil {
		t.Fatal(err)
	}
	sourceA := strings.TrimSpace(sourceAOutput)
	sourceB := strings.TrimSpace(sourceBOutput)
	if _, err := runMutationCLI(home, "memory", "create", "--repo", repoA, "--title", "wrong", "--body", "body", "--source", sourceB); err == nil {
		t.Fatal("cross-repository memory source was accepted")
	}
	memoryAOutput, err := runMutationCLI(home, "memory", "create", "--repo", repoA, "--title", "A", "--body", "body", "--source", sourceA)
	if err != nil {
		t.Fatal(err)
	}
	memoryBOutput, err := runMutationCLI(home, "memory", "create", "--repo", repoB, "--title", "B", "--body", "body", "--source", sourceB)
	if err != nil {
		t.Fatal(err)
	}
	memoryA := strings.TrimSpace(memoryAOutput)
	memoryB := strings.TrimSpace(memoryBOutput)
	if _, err := runMutationCLI(home, "memory", "supersede", memoryA, memoryB); err == nil {
		t.Fatal("cross-repository memory supersession was accepted")
	}
	if _, err := runMutationCLI(home, "memory", "approve", "missing-memory"); err == nil {
		t.Fatal("missing memory approval was accepted")
	}
}

func TestDomainMutationRollsBackDerivedWrites(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	sessionOutput, err := runMutationCLI(home, "session", "start", "--repo", repoID, "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	session := strings.TrimSpace(sessionOutput)
	transcript := writeRepoTranscript(t, work, "events.jsonl", `{"type":"UserPrompt","text":"hello"}`)
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_activities BEFORE INSERT ON activities BEGIN SELECT RAISE(ABORT, 'forced activity failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if output, err := runMutationCLI(home, "checkpoint", "create", "--repo", repoID, "--session", session, "--message", "m"); err == nil || strings.TrimSpace(output) != "" {
		t.Fatalf("checkpoint failure emitted success: output=%q err=%v", output, err)
	}
	if output, err := runMutationCLI(home, "memory", "create", "--repo", repoID, "--title", "t", "--body", "b"); err == nil || strings.TrimSpace(output) != "" {
		t.Fatalf("memory failure emitted success: output=%q err=%v", output, err)
	}
	if output, err := runMutationCLI(home, "source", "import", "--repo", repoID, "--file", transcript); err == nil || strings.TrimSpace(output) != "" {
		t.Fatalf("source failure emitted success: output=%q err=%v", output, err)
	}
	db, err = store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	checks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM checkpoints`, 0},
		{`SELECT COUNT(*) FROM memories`, 0},
		{`SELECT COUNT(*) FROM sources`, 1},
		{`SELECT COUNT(*) FROM sources WHERE kind='note'`, 0},
		{`SELECT COUNT(*) FROM source_events`, 0},
		{`SELECT COUNT(*) FROM search_index`, 0},
	}
	for _, check := range checks {
		if count, err := scalarCount(db, check.query); err != nil || count != check.want {
			t.Fatalf("query %s after rollback: count=%d want=%d err=%v", check.query, count, check.want, err)
		}
	}
	var latest string
	if err := db.QueryRow(`SELECT latest_checkpoint_id FROM sessions WHERE id=?`, session).Scan(&latest); err != nil || latest != "" {
		t.Fatalf("session latest checkpoint after rollback: %q %v", latest, err)
	}
}

func TestCheckpointRestoreRollsBackWorkspaceMetadata(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	sessionOutput, err := runMutationCLI(home, "session", "start", "--repo", repoID, "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	checkpointOutput, err := runMutationCLI(home, "checkpoint", "create", "--repo", repoID, "--session", strings.TrimSpace(sessionOutput), "--message", "m")
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := strings.TrimSpace(checkpointOutput)
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_restore_activity BEFORE INSERT ON activities BEGIN SELECT RAISE(ABORT, 'forced restore activity failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if output, err := runMutationCLI(home, "checkpoint", "restore", checkpointID); err == nil || strings.TrimSpace(output) != "" {
		t.Fatalf("restore failure emitted success: output=%q err=%v", output, err)
	}
	db, err = store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM workspaces`); err != nil || count != 0 {
		t.Fatalf("workspace metadata after rollback: count=%d err=%v", count, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("restore cleanup left workspace paths: %v", entries)
	}
}

func TestAutoCheckpointIsIdempotent(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	if _, err := runMutationCLI(home, "session", "start", "--repo", repoID, "--agent", "codex"); err != nil {
		t.Fatal(err)
	}
	first, err := invokePostCommitForTest(home, work)
	if err != nil {
		t.Fatal(err)
	}
	second, err := invokePostCommitForTest(home, work)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("hook ids: first=%q second=%q", first, second)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM checkpoints`); err != nil || count != 1 {
		t.Fatalf("checkpoint count: %d %v", count, err)
	}
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM activities WHERE kind='checkpoint.auto'`); err != nil || count != 1 {
		t.Fatalf("auto activity count: %d %v", count, err)
	}
}

func TestAutoCheckpointConcurrentCallsRemainIdempotent(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	if _, err := runMutationCLI(home, "session", "start", "--repo", repoID, "--agent", "codex"); err != nil {
		t.Fatal(err)
	}
	oldHome := homeDir
	homeDir = home
	defer func() { homeDir = oldHome }()
	const callers = 8
	outputs := make(chan string, callers)
	errs := make(chan error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			err := runPostCommitHook(cmd, work)
			outputs <- strings.TrimSpace(output.String())
			errs <- err
		}()
	}
	wait.Wait()
	close(outputs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for output := range outputs {
		if first == "" {
			first = output
		}
		if output != first {
			t.Fatalf("concurrent hook ids: %q and %q", first, output)
		}
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM checkpoints`); err != nil || count != 1 {
		t.Fatalf("concurrent checkpoint count: %d %v", count, err)
	}
}

func TestTombstonedSourceRequiresNewIdentity(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	transcript := writeRepoTranscript(t, work, "events.jsonl", `{"type":"UserPrompt","text":"keep"}`)
	oldID, err := importTranscriptForTest(home, repoID, transcript, "native-old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runMutationCLI(home, "source", "delete", oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := importTranscriptForTest(home, repoID, transcript, ""); err == nil {
		t.Fatal("tombstoned source was accepted without an identity")
	}
	if output, err := importTranscriptWithIdentityForTest(home, repoID, transcript, "native-old"); err == nil || !strings.Contains(FormatError(err), "tombstoned") {
		t.Fatalf("same native identity was accepted: output=%q err=%v", output, err)
	}
	if output, err := runMutationCLI(home, "source", "import", "--repo", repoID, "--file", transcript, "--native-id", "native-new"); err != nil {
		t.Fatalf("new identity import: output=%q err=%v", output, err)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM sources WHERE deleted_at=''`); err != nil || count != 1 {
		t.Fatalf("live source count: %d %v", count, err)
	}
	if count, err := scalarCount(db, `SELECT COUNT(*) FROM sources WHERE deleted_at<>''`); err != nil || count != 1 {
		t.Fatalf("tombstoned source count: %d %v", count, err)
	}
}

func TestTombstoneOnlyBlocksTheDeletedIdentity(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	first := writeRepoTranscript(t, work, "one.jsonl", `{"type":"UserPrompt","text":"one"}`)
	second := writeRepoTranscript(t, work, "two.jsonl", `{"type":"UserPrompt","text":"two"}`)
	oldID, err := importTranscriptForTest(home, repoID, first, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runMutationCLI(home, "source", "delete", oldID); err != nil {
		t.Fatal(err)
	}
	if output, err := importTranscriptForTest(home, repoID, second, ""); err != nil {
		t.Fatalf("unrelated import without a native identity was blocked: output=%q err=%v", output, err)
	}
	if _, err := importTranscriptForTest(home, repoID, first, ""); err == nil || !strings.Contains(FormatError(err), "tombstoned") {
		t.Fatalf("re-import of the deleted origin was accepted: %v", err)
	}
}

func TestImportSessionRecordsCallerPathAsOrigin(t *testing.T) {
	home, repoID, work := newDomainRepo(t)
	session, err := runMutationCLI(home, "session", "start", "--repo", repoID, "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	transcript := writeRepoTranscript(t, work, "export.jsonl", `{"type":"UserPrompt","text":"hello"}`)
	output, err := runMutationCLI(home, "agent", "import-session", "--agent", "across", "--repo", repoID, "--session", strings.TrimSpace(session), "--file", transcript)
	if err != nil {
		t.Fatalf("import-session: output=%q err=%v", output, err)
	}
	expected, err := filepath.EvalSymlinks(transcript)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var origin string
	if err := db.QueryRow(`SELECT origin FROM sources WHERE id=?`, strings.TrimSpace(output)).Scan(&origin); err != nil {
		t.Fatal(err)
	}
	if origin != expected {
		t.Fatalf("source origin %q, want the caller's export %q", origin, expected)
	}
}

// writeRepoTranscript stages a transcript inside the repository working tree.
// importTranscript confines transcript paths to the repository root, so tests
// cannot stage them in an unrelated t.TempDir() — doing so makes every import
// fail for the wrong reason and hides the behaviour under test.
func writeRepoTranscript(t *testing.T, work, name, line string) string {
	t.Helper()
	path := filepath.Join(work, name)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newDomainRepo(t *testing.T) (string, string, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	repoID, work := newDomainRepoInHome(t, home)
	return home, repoID, work
}

func newDomainRepoInHome(t *testing.T, home string) (string, string) {
	t.Helper()
	work := t.TempDir()
	runGitForTest(t, "", "init", work)
	runGitForTest(t, work, "config", "user.email", "test@example.com")
	runGitForTest(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", ".")
	runGitForTest(t, work, "commit", "-m", "initial")
	output, err := runMutationCLI(home, "repo", "add", work)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(output), work
}

func runGitForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func runMutationCLI(home string, args ...string) (string, error) {
	root := NewRoot()
	root.SetArgs(append([]string{"--home", home}, args...))
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	err := WrapError(root.Execute())
	return output.String(), err
}

func importTranscriptForTest(home, repoID, file, nativeID string) (string, error) {
	oldHome := homeDir
	homeDir = home
	defer func() { homeDir = oldHome }()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	err := importTranscript(cmd, repoID, "note", file, "across", nativeID)
	return strings.TrimSpace(output.String()), err
}

func importTranscriptWithIdentityForTest(home, repoID, file, nativeID string) (string, error) {
	return importTranscriptForTest(home, repoID, file, nativeID)
}

func invokePostCommitForTest(home, work string) (string, error) {
	oldHome := homeDir
	homeDir = home
	defer func() { homeDir = oldHome }()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	err := runPostCommitHook(cmd, work)
	return strings.TrimSpace(output.String()), err
}

func scalarCount(db interface {
	QueryRow(string, ...any) *sql.Row
}, query string) (int, error) {
	var count int
	err := db.QueryRow(query).Scan(&count)
	return count, err
}
