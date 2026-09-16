package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/graycodeai/across/internal/git"
)

// symEntry is a file-scoped symbol for diffing.
type symEntry struct {
	Key       string // file + "#" + symbol
	Signature string
}

type symChange struct {
	Key      string
	Old, New string
}

// symbolsAtRevision materializes revision into an isolated temp worktree
// (read-only w.r.t. the user's checkout) and collects Go symbols via go/parser.
func symbolsAtRevision(canon, home, rev string) (map[string]symEntry, error) {
	tmp, err := os.MkdirTemp("", "across-graphdiff-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	wt := filepath.Join(tmp, "wt")
	if _, err := git.Run(canon, "worktree", "add", "--detach", wt, rev); err != nil {
		return nil, err
	}
	defer git.Run(canon, "worktree", "remove", "--force", wt)
	return collectSymbols(wt), nil
}

func collectSymbols(root string) map[string]symEntry {
	m := map[string]symEntry{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(p, "/.git/") || info.Size() > 1<<20 {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if strings.HasSuffix(p, ".go") {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return nil
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					key := rel + "#" + d.Name.Name
					m[key] = symEntry{Key: key, Signature: "func " + d.Name.Name}
				case *ast.GenDecl:
					for _, sp := range d.Specs {
						if ts, ok := sp.(*ast.TypeSpec); ok {
							key := rel + "#" + ts.Name.Name
							m[key] = symEntry{Key: key, Signature: "type " + ts.Name.Name}
						}
					}
				}
			}
		}
		return nil
	})
	return m
}

func diffSymbolSets(base, head map[string]symEntry) (added, removed []symEntry, changed []symChange) {
	for k, h := range head {
		b, ok := base[k]
		if !ok {
			added = append(added, h)
		} else if b.Signature != h.Signature {
			changed = append(changed, symChange{Key: k, Old: b.Signature, New: h.Signature})
		}
	}
	for k, b := range base {
		if _, ok := head[k]; !ok {
			removed = append(removed, b)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Key < added[j].Key })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Key < removed[j].Key })
	sort.Slice(changed, func(i, j int) bool { return changed[i].Key < changed[j].Key })
	return added, removed, changed
}
