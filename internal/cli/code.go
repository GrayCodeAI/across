package cli

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newCodeCmd() *cobra.Command {
	c := &cobra.Command{Use: "code", Short: "Code search"}
	c.AddCommand(&cobra.Command{Use: "search QUERY [--repo ID]", Args: cobra.ExactArgs(1), Short: "Search code (respects .gitignore, skips binaries)", RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var canon string
		if repoID != "" {
			c, _, err := repoMustExist(db, repoID)
			if err != nil {
				return err
			}
			canon = c
		} else {
			cwd, _ := os.Getwd()
			canon = cwd
		}
		q := args[0]
		count := 0
		_ = filepath.Walk(canon, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(p)
				if base == ".git" || base == "node_modules" || base == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Size() > 1<<20 {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, c := range b[:minInt(512, len(b))] {
				if c == 0 {
					return nil
				}
			}
			if strings.Contains(string(b), q) {
				rel, _ := filepath.Rel(canon, p)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", rel)
				count++
				if count >= 50 {
					return fmt.Errorf("limit")
				}
			}
			return nil
		})
		return nil
	}})
	c.PersistentFlags().String("repo", "", "repository id")
	return c
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func newIndexCmd() *cobra.Command {
	c := &cobra.Command{Use: "index --repo ID", Short: "Index code symbols", RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		canon, _, err := repoMustExist(db, repoID)
		if err != nil {
			return err
		}
		rev := git.Head(canon)
		_, _ = db.Exec(`DELETE FROM code_symbols WHERE repository_id=?`, repoID)
		_, _ = db.Exec(`DELETE FROM code_relations WHERE repository_id=?`, repoID)
		n := 0
		_ = filepath.Walk(canon, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.Contains(p, "/.git/") {
				return nil
			}
			if info.Size() > 1<<20 {
				return nil
			}
			if strings.HasSuffix(p, ".go") {
				n += indexGoFile(db, repoID, canon, p, rev)
			} else if hasExt(p, []string{".py", ".js", ".ts", ".rs", ".java", ".rb", ".php", ".c", ".cpp", ".cs", ".swift", ".kt", ".scala"}) {
				n += indexHeuristic(db, repoID, canon, p, rev)
			}
			return nil
		})
		logActivity(db, "code.index", repoID, rev, fmt.Sprintf("indexed %d symbols", n))
		fmt.Fprintf(cmd.OutOrStdout(), "indexed %d symbols at %s\n", n, rev)
		return nil
	}}
	c.Flags().String("repo", "", "repository id")
	return c
}

func hasExt(p string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(p, e) {
			return true
		}
	}
	return false
}

func indexGoFile(db *sql.DB, repoID, root, path, rev string) int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return 0
	}
	rel, _ := filepath.Rel(root, path)
	n := 0
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			kind := "function"
			sig := "func " + name
			if d.Recv != nil {
				kind = "method"
			}
			pos := fset.Position(d.Pos())
			end := fset.Position(d.End())
			_, _ = db.Exec(`INSERT INTO code_symbols(id, repository_id, file, language, symbol, kind, signature, start_line, end_line, revision, analysis_quality) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				store.NewID("sym"), repoID, rel, "go", name, kind, sig, pos.Line, end.Line, rev, "AST")
			n++
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				if ts, ok := sp.(*ast.TypeSpec); ok {
					pos := fset.Position(ts.Pos())
					end := fset.Position(ts.End())
					_, _ = db.Exec(`INSERT INTO code_symbols(id, repository_id, file, language, symbol, kind, signature, start_line, end_line, revision, analysis_quality) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
						store.NewID("sym"), repoID, rel, "go", ts.Name.Name, "type", "type "+ts.Name.Name, pos.Line, end.Line, rev, "AST")
					n++
				}
			}
		}
	}
	// imports as relations
	for _, imp := range f.Imports {
		to := strings.Trim(imp.Path.Value, `"`)
		_, _ = db.Exec(`INSERT INTO code_relations(id, repository_id, from_symbol, to_symbol, kind, basis, confidence, revision) VALUES(?,?,?,?,?,?,?,?)`,
			store.NewID("rel"), repoID, rel, to, "imports", "AST", 1.0, rev)
	}
	return n
}

var heuristicRe = regexp.MustCompile(`(?m)^\s*(func|function|def|class|interface|type|struct)\s+([A-Za-z0-9_]+)`)

func indexHeuristic(db *sql.DB, repoID, root, path, rev string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	rel, _ := filepath.Rel(root, path)
	m := heuristicRe.FindAllStringSubmatch(string(b), 20)
	n := 0
	for _, g := range m {
		_, _ = db.Exec(`INSERT INTO code_symbols(id, repository_id, file, language, symbol, kind, signature, start_line, end_line, revision, analysis_quality) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			store.NewID("sym"), repoID, rel, langOf(path), g[2], g[1], g[1]+" "+g[2], 0, 0, rev, "heuristic")
		n++
	}
	return n
}

func langOf(p string) string {
	switch filepath.Ext(p) {
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	default:
		return "other"
	}
}
