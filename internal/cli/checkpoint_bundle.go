package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newCheckpointBundleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bundle ID [--output F]", Args: cobra.ExactArgs(1), Short: "Export a versioned checkpoint evidence bundle", RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var bundle CheckpointBundle
		var checkpointContentHash string
		if err := db.QueryRow(`SELECT repository_id, revision, session_id, event_cursor, content_hash FROM checkpoints WHERE id=?`, args[0]).Scan(&bundle.Repository, &bundle.Revision, &bundle.Session, &bundle.EventCursor, &checkpointContentHash); err != nil {
			if err == sql.ErrNoRows {
				return notFound("checkpoint %q not found", args[0])
			}
			return err
		}
		bundle.SchemaVersion = contractSchemaVersion
		bundle.Type = "checkpoint_bundle"
		bundle.ID = newContractID("bnd")
		bundle.ContentHash = checkpointContentHash
		bundle.Evidence = []EvidenceReference{{Kind: "checkpoint", ID: args[0], Revision: bundle.Revision, Basis: "checkpoint_record", Reason: "checkpoint evidence bundle"}}
		bundle.Unknowns = []string{"unlinked external evidence is UNKNOWN"}
		bundle.GeneratedAt = store.NowUTC()
		rows, err := db.Query(`SELECT id, name, revision_after, basis, exit_code FROM verifications WHERE repository_id=? AND (revision_before=? OR revision_after=?) ORDER BY started_at`, bundle.Repository, bundle.Revision, bundle.Revision)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name, revision, basis string
			var exitCode int
			if err := rows.Scan(&id, &name, &revision, &basis, &exitCode); err != nil {
				rows.Close()
				return err
			}
			bundle.Evidence = append(bundle.Evidence, EvidenceReference{Kind: "verification", ID: id, Revision: revision, Basis: basis, Reason: fmt.Sprintf("%s exit=%d", name, exitCode)})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := sealCheckpointBundle(&bundle); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return err
		}
		if err := withTx(db, func(tx sqlRunner) error {
			_, err := tx.Exec(`INSERT INTO evidence_bundles(id, kind, subject_kind, subject_id, repository_id, revision, schema_version, payload, content_hash, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				bundle.ID, "checkpoint", "checkpoint", args[0], bundle.Repository, bundle.Revision, contractSchemaVersion, string(encoded), bundle.BundleHash, bundle.GeneratedAt)
			if err != nil {
				return err
			}
			return logActivityTx(tx, "checkpoint.bundle", bundle.Repository, args[0], "exported bundle "+bundle.ID)
		}); err != nil {
			return err
		}
		if output, _ := cmd.Flags().GetString("output"); output != "" {
			resolved, err := requireOutputFile(output)
			if err != nil {
				return err
			}
			return os.WriteFile(resolved, encoded, 0o644)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}}
	cmd.Flags().String("output", "", "output file")
	return cmd
}
