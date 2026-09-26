package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newContextPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pack --repo ID --query QUERY [--session SID] [--checkpoint CP] [--budget N] [--output F]",
		Short:   "Build a versioned, source-linked context manifest",
		PreRunE: requiredFlags("repo", "query"),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			query, _ := cmd.Flags().GetString("query")
			sessionID, _ := cmd.Flags().GetString("session")
			checkpointID, _ := cmd.Flags().GetString("checkpoint")
			budget, _ := cmd.Flags().GetInt("budget")
			output, _ := cmd.Flags().GetString("output")
			if strings.TrimSpace(query) == "" {
				return invalidArgument("query must not be empty")
			}
			if budget < 1 || budget > 100000 {
				return invalidArgument("budget must be between 1 and 100000")
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			canon, _, err := repoMustExist(db, repoID)
			if err != nil {
				return err
			}
			if sessionID != "" {
				if _, err := sessionMustBelong(db, repoID, sessionID, false); err != nil {
					return err
				}
			}
			revision := git.Head(canon)
			if checkpointID == "" && sessionID != "" {
				if err := db.QueryRow(`SELECT latest_checkpoint_id FROM sessions WHERE id=?`, sessionID).Scan(&checkpointID); err != nil && err != sql.ErrNoRows {
					return err
				}
			}
			if checkpointID != "" {
				if err := db.QueryRow(`SELECT revision FROM checkpoints WHERE id=? AND repository_id=?`, checkpointID, repoID).Scan(&revision); err != nil {
					if err == sql.ErrNoRows {
						return notFound("checkpoint %q not found", checkpointID)
					}
					return err
				}
				resolved, err := git.ResolveRevision(canon, revision)
				if err != nil {
					return invalidArgument("checkpoint revision does not resolve")
				}
				revision = resolved
			}
			items := make([]ContextItem, 0)
			if checkpointID != "" {
				var message, basis string
				if err := db.QueryRow(`SELECT message, basis FROM checkpoints WHERE id=?`, checkpointID).Scan(&message, &basis); err != nil {
					return err
				}
				items = append(items, ContextItem{Kind: "checkpoint", RefID: checkpointID, Repository: repoID, Revision: revision, Basis: basis, Title: message, Body: revision, Reason: "checkpoint boundary", Included: true})
			}
			rows, err := db.Query(`SELECT id, title, body, state FROM memories WHERE repository_id=? AND state='approved' ORDER BY created_at DESC LIMIT 30`, repoID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, title, body, state string
				if err := rows.Scan(&id, &title, &body, &state); err != nil {
					rows.Close()
					return err
				}
				items = append(items, ContextItem{Kind: "memory", RefID: id, Repository: repoID, Revision: revision, Basis: state, Title: title, Body: body, Reason: "approved engineering context", Included: true})
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			verificationRows, err := db.Query(`SELECT id, name, revision_after, basis, exit_code FROM verifications WHERE repository_id=? ORDER BY started_at DESC LIMIT 20`, repoID)
			if err != nil {
				return err
			}
			for verificationRows.Next() {
				var id, name, rev, basis string
				var exitCode int
				if err := verificationRows.Scan(&id, &name, &rev, &basis, &exitCode); err != nil {
					verificationRows.Close()
					return err
				}
				items = append(items, ContextItem{Kind: "verification", RefID: id, Repository: repoID, Revision: rev, Basis: basis, Title: name, Body: fmt.Sprintf("exit=%d", exitCode), Reason: "execution evidence", Included: true})
			}
			if err := verificationRows.Err(); err != nil {
				verificationRows.Close()
				return err
			}
			if err := verificationRows.Close(); err != nil {
				return err
			}
			selected := make([]ContextItem, 0, len(items))
			used := 0
			for _, item := range items {
				item.TokenCost = estimateTokens(item.Title + "\n" + item.Body)
				if used+item.TokenCost > budget {
					item.Included = false
					selected = append(selected, item)
					continue
				}
				used += item.TokenCost
				selected = append(selected, item)
			}
			manifest := ContextManifest{
				SchemaVersion: contractSchemaVersion,
				Type:          "context",
				ID:            newContractID("ctxm"),
				Repository:    repoID,
				Session:       sessionID,
				Checkpoint:    checkpointID,
				Revision:      revision,
				Epoch:         1,
				Items:         selected,
				GeneratedAt:   store.NowUTC(),
			}
			if err := sealContext(&manifest); err != nil {
				return err
			}
			if err := withTx(db, func(tx sqlRunner) error {
				if _, err := tx.Exec(`INSERT INTO context_manifests(id, repository_id, session_id, checkpoint_id, revision, epoch, schema_version, status, content_hash, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
					manifest.ID, repoID, sessionID, checkpointID, revision, manifest.Epoch, contractSchemaVersion, "ready", manifest.ContentHash, manifest.GeneratedAt); err != nil {
					return err
				}
				for position, item := range manifest.Items {
					if _, err := tx.Exec(`INSERT INTO context_items(id, manifest_id, position, kind, ref_id, source_id, repository_id, revision, basis, title, body, reason, token_cost, included) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
						newContractID("ctx"), manifest.ID, position, item.Kind, item.RefID, item.SourceID, item.Repository, item.Revision, item.Basis, item.Title, item.Body, item.Reason, item.TokenCost, boolInt(item.Included)); err != nil {
						return err
					}
				}
				if checkpointID != "" {
					if _, err := tx.Exec(`UPDATE checkpoints SET context_manifest_id=? WHERE id=? AND repository_id=?`, manifest.ID, checkpointID, repoID); err != nil {
						return err
					}
				}
				return logActivityTx(tx, "context.pack", repoID, manifest.ID, fmt.Sprintf("context items=%d budget=%d", len(manifest.Items), budget))
			}); err != nil {
				return err
			}
			encoded, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return err
			}
			if output != "" {
				resolved, err := requireOutputFile(output)
				if err != nil {
					return err
				}
				return os.WriteFile(resolved, encoded, 0o644)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return nil
		},
	}
	cmd.Flags().String("query", "", "retrieval objective")
	cmd.Flags().String("session", "", "session id")
	cmd.Flags().String("checkpoint", "", "checkpoint id")
	cmd.Flags().Int("budget", 4000, "token budget")
	cmd.Flags().String("output", "", "output file")
	return cmd
}

func newContextShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show MANIFEST_ID", Args: cobra.ExactArgs(1), Short: "Show a context manifest", RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var manifest ContextManifest
		var created string
		if err := db.QueryRow(`SELECT id, repository_id, session_id, checkpoint_id, revision, epoch, schema_version, content_hash, created_at FROM context_manifests WHERE id=?`, args[0]).Scan(&manifest.ID, &manifest.Repository, &manifest.Session, &manifest.Checkpoint, &manifest.Revision, &manifest.Epoch, &manifest.SchemaVersion, &manifest.ContentHash, &created); err != nil {
			if err == sql.ErrNoRows {
				return notFound("context manifest %q not found", args[0])
			}
			return err
		}
		manifest.Type = "context"
		manifest.GeneratedAt = created
		manifest.Items = make([]ContextItem, 0)
		rows, err := db.Query(`SELECT kind, ref_id, source_id, repository_id, revision, basis, title, body, reason, token_cost, included FROM context_items WHERE manifest_id=? ORDER BY position`, args[0])
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ContextItem
			var included int
			if err := rows.Scan(&item.Kind, &item.RefID, &item.SourceID, &item.Repository, &item.Revision, &item.Basis, &item.Title, &item.Body, &item.Reason, &item.TokenCost, &included); err != nil {
				return err
			}
			item.Included = included != 0
			manifest.Items = append(manifest.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
