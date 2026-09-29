package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/graycodeai/across/internal/store"
)

const contractSchemaVersion = 1

type EvidenceReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
	Basis    string `json:"basis"`
	Reason   string `json:"reason"`
}

type HandoffEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	ID            string              `json:"id"`
	Type          string              `json:"type"`
	Session       string              `json:"session"`
	Repository    string              `json:"repository"`
	Revision      string              `json:"revision,omitempty"`
	Latest        string              `json:"latest_checkpoint,omitempty"`
	Evidence      []EvidenceReference `json:"evidence"`
	Unknowns      []string            `json:"unknowns"`
	GeneratedAt   string              `json:"generated_at"`
	ContentHash   string              `json:"content_hash"`
}

type CheckpointBundle struct {
	SchemaVersion int                 `json:"schema_version"`
	Type          string              `json:"type"`
	ID            string              `json:"id"`
	Repository    string              `json:"repository"`
	Revision      string              `json:"revision"`
	Session       string              `json:"session,omitempty"`
	ContentHash   string              `json:"content_hash,omitempty"`
	Evidence      []EvidenceReference `json:"evidence"`
	Unknowns      []string            `json:"unknowns"`
	GeneratedAt   string              `json:"generated_at"`
	BundleHash    string              `json:"bundle_hash"`
}

type ContextManifest struct {
	SchemaVersion int           `json:"schema_version"`
	Type          string        `json:"type"`
	ID            string        `json:"id"`
	Repository    string        `json:"repository"`
	Session       string        `json:"session,omitempty"`
	Checkpoint    string        `json:"checkpoint,omitempty"`
	Revision      string        `json:"revision,omitempty"`
	Epoch         int           `json:"epoch"`
	Items         []ContextItem `json:"items"`
	GeneratedAt   string        `json:"generated_at"`
	ContentHash   string        `json:"content_hash"`
}

type ContextItem struct {
	Kind       string `json:"kind"`
	RefID      string `json:"ref_id,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	Repository string `json:"repository,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Basis      string `json:"basis"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Reason     string `json:"reason"`
	TokenCost  int    `json:"token_cost"`
	Included   bool   `json:"included"`
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func digestFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func estimateTokens(value string) int {
	if value == "" {
		return 0
	}
	return (len(value) + 3) / 4
}

func sealHandoff(envelope *HandoffEnvelope) error {
	envelope.ContentHash = ""
	hash, err := digestJSON(envelope)
	if err != nil {
		return err
	}
	envelope.ContentHash = hash
	return nil
}

func sealCheckpointBundle(bundle *CheckpointBundle) error {
	bundle.BundleHash = ""
	hash, err := digestJSON(bundle)
	if err != nil {
		return err
	}
	bundle.BundleHash = hash
	return nil
}

func sealContext(manifest *ContextManifest) error {
	manifest.ContentHash = ""
	hash, err := digestJSON(manifest)
	if err != nil {
		return err
	}
	manifest.ContentHash = hash
	return nil
}

func stableContractID(prefix string, parts ...string) string {
	return prefix + "_" + digestBytes([]byte(strings.Join(parts, "\x00")))[:22]
}

func newContractID(prefix string) string {
	return store.NewID(prefix)
}
