package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graycodeai/across/internal/store"
)

func TestSessionForkRecordsLineage(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	parent := mustRunCLI(t, home, "session", "start", "--repo", repoID, "--agent", "codex")
	child := mustRunCLI(t, home, "session", "fork", parent, "--repo", repoID, "--agent", "claude-code")
	if child == "" || child == parent {
		t.Fatalf("fork returned %q", child)
	}
	shown := mustRunCLI(t, home, "session", "show", child)
	for _, want := range []string{"parent: " + parent, "fork_type: fork", "lineage_version: 2", "agent: claude-code"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("session show missing %q:\n%s", want, shown)
		}
	}
	if _, err := runMutationCLI(home, "session", "fork", "sess_missing", "--repo", repoID, "--agent", "codex"); ExitCode(err) != 3 {
		t.Fatalf("fork of a missing parent: exit %d %v", ExitCode(err), err)
	}
	otherRepo, _ := newDomainRepoInHome(t, home)
	if _, err := runMutationCLI(home, "session", "fork", parent, "--repo", otherRepo, "--agent", "codex"); ExitCode(err) != 4 {
		t.Fatalf("fork across repositories: exit %d %v", ExitCode(err), err)
	}
}

func TestCheckpointBundleIsSealedAndRecorded(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	session := mustRunCLI(t, home, "session", "start", "--repo", repoID, "--agent", "codex")
	checkpoint := mustRunCLI(t, home, "checkpoint", "create", "--repo", repoID, "--session", session, "--message", "bundle me")
	mustRunCLI(t, home, "verify", "add", "--repo", repoID, "--name", "unit")
	output := filepath.Join(t.TempDir(), "bundle.json")
	mustRunCLI(t, home, "checkpoint", "bundle", checkpoint, "--output", output)
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var bundle CheckpointBundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Type != "checkpoint_bundle" || bundle.SchemaVersion != contractSchemaVersion || bundle.Repository != repoID || bundle.Session != session || bundle.Revision == "" || bundle.ContentHash == "" {
		t.Fatalf("bundle fields: %+v", bundle)
	}
	if len(bundle.Evidence) == 0 || bundle.Evidence[0].Kind != "checkpoint" || bundle.Evidence[0].ID != checkpoint {
		t.Fatalf("bundle evidence: %+v", bundle.Evidence)
	}
	recorded := bundle.BundleHash
	if err := sealCheckpointBundle(&bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.BundleHash != recorded {
		t.Fatalf("bundle hash does not match its content: %s != %s", recorded, bundle.BundleHash)
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var payload, hash string
	if err := db.QueryRow(`SELECT payload, content_hash FROM evidence_bundles WHERE id=? AND subject_id=?`, bundle.ID, checkpoint).Scan(&payload, &hash); err != nil {
		t.Fatalf("bundle not recorded: %v", err)
	}
	if hash != recorded || payload != string(encoded) {
		t.Fatal("recorded bundle differs from the exported bundle")
	}
	if _, err := runMutationCLI(home, "checkpoint", "bundle", "cp_missing"); ExitCode(err) != 3 {
		t.Fatalf("bundle of a missing checkpoint: exit %d %v", ExitCode(err), err)
	}
}

func TestContextPackAndShowRoundTrip(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	memory := mustRunCLI(t, home, "memory", "create", "--repo", repoID, "--kind", "decision", "--title", "use sqlite", "--body", "single file store")
	mustRunCLI(t, home, "memory", "approve", memory)
	mustRunCLI(t, home, "memory", "create", "--repo", repoID, "--kind", "note", "--title", "unapproved", "--body", "candidate only")
	packed := mustRunCLI(t, home, "context", "pack", "--repo", repoID, "--query", "storage")
	var manifest ContextManifest
	if err := json.Unmarshal([]byte(packed), &manifest); err != nil {
		t.Fatalf("pack output: %v\n%s", err, packed)
	}
	if manifest.Type != "context" || manifest.Repository != repoID || len(manifest.Items) != 1 {
		t.Fatalf("manifest: %+v", manifest)
	}
	item := manifest.Items[0]
	if item.Kind != "memory" || item.RefID != memory || !item.Included || item.TokenCost == 0 {
		t.Fatalf("approved memory item: %+v", item)
	}
	shown := mustRunCLI(t, home, "context", "show", manifest.ID)
	var reloaded ContextManifest
	if err := json.Unmarshal([]byte(shown), &reloaded); err != nil {
		t.Fatal(err)
	}
	stored := reloaded.ContentHash
	if err := sealContext(&reloaded); err != nil {
		t.Fatal(err)
	}
	if stored != manifest.ContentHash || reloaded.ContentHash != manifest.ContentHash {
		t.Fatalf("context show does not reproduce the sealed manifest: packed=%s stored=%s recomputed=%s", manifest.ContentHash, stored, reloaded.ContentHash)
	}
	tight := mustRunCLI(t, home, "context", "pack", "--repo", repoID, "--query", "storage", "--budget", "1")
	var small ContextManifest
	if err := json.Unmarshal([]byte(tight), &small); err != nil {
		t.Fatal(err)
	}
	if len(small.Items) != 1 || small.Items[0].Included {
		t.Fatalf("over-budget item was included: %+v", small.Items)
	}
	if _, err := runMutationCLI(home, "context", "pack", "--repo", repoID, "--query", "storage", "--budget", "0"); ExitCode(err) != 2 {
		t.Fatalf("zero budget: exit %d %v", ExitCode(err), err)
	}
	if _, err := runMutationCLI(home, "context", "show", "ctxm_missing"); ExitCode(err) != 3 {
		t.Fatalf("show of a missing manifest: exit %d %v", ExitCode(err), err)
	}
}

func TestContextShowOfEmptyManifestKeepsItsHash(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	packed := mustRunCLI(t, home, "context", "pack", "--repo", repoID, "--query", "nothing yet")
	var manifest ContextManifest
	if err := json.Unmarshal([]byte(packed), &manifest); err != nil {
		t.Fatal(err)
	}
	shown := mustRunCLI(t, home, "context", "show", manifest.ID)
	var reloaded ContextManifest
	if err := json.Unmarshal([]byte(shown), &reloaded); err != nil {
		t.Fatal(err)
	}
	if err := sealContext(&reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.ContentHash != manifest.ContentHash {
		t.Fatalf("empty manifest hash changed on show: %s != %s", reloaded.ContentHash, manifest.ContentHash)
	}
}

func TestHandoffEnvelopeIsSealedAndRecorded(t *testing.T) {
	home, repoID, _ := newDomainRepo(t)
	session := mustRunCLI(t, home, "session", "start", "--repo", repoID, "--agent", "codex")
	checkpoint := mustRunCLI(t, home, "checkpoint", "create", "--repo", repoID, "--session", session, "--message", "handoff point")
	encoded := mustRunCLI(t, home, "handoff", "--session", session, "--format", "json")
	var envelope HandoffEnvelope
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		t.Fatalf("handoff output: %v\n%s", err, encoded)
	}
	if envelope.Type != "handoff" || envelope.Session != session || envelope.Repository != repoID || envelope.Latest != checkpoint {
		t.Fatalf("handoff envelope: %+v", envelope)
	}
	recorded := envelope.ContentHash
	if err := sealHandoff(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ContentHash != recorded {
		t.Fatalf("handoff hash does not match its content")
	}
	db, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var content, hash string
	if err := db.QueryRow(`SELECT content, content_hash FROM handoffs WHERE id=? AND session_id=?`, envelope.ID, session).Scan(&content, &hash); err != nil {
		t.Fatalf("handoff not recorded: %v", err)
	}
	if hash != recorded || strings.TrimSpace(content) != encoded {
		t.Fatal("recorded handoff differs from the emitted handoff")
	}
	if _, err := runMutationCLI(home, "handoff", "--session", session, "--format", "yaml"); ExitCode(err) != 2 {
		t.Fatalf("unknown handoff format: exit %d %v", ExitCode(err), err)
	}
}

func mustRunCLI(t *testing.T, home string, args ...string) string {
	t.Helper()
	output, err := runMutationCLI(home, args...)
	if err != nil {
		t.Fatalf("across %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(output)
}
