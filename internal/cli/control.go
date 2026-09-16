package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newControlCmd() *cobra.Command {
	c := &cobra.Command{Use: "control", Short: "Local control plane (org/project/principal/grant)"}
	c.AddCommand(
		&cobra.Command{Use: "org-create NAME", Args: cobra.ExactArgs(1), Short: "Create org", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("org")
			_, _ = db.Exec(`INSERT INTO organizations(id, name, created_at) VALUES(?,?,?)`, id, args[0], store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "project-create --org ID --name N", Short: "Create project", RunE: func(cmd *cobra.Command, args []string) error {
			org, _ := cmd.Flags().GetString("org")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("proj")
			_, _ = db.Exec(`INSERT INTO projects(id, org_id, name, created_at) VALUES(?,?,?,?)`, id, org, name, store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "project-list [--org ID]", Short: "List projects", RunE: func(cmd *cobra.Command, args []string) error {
			org, _ := cmd.Flags().GetString("org")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var rows, err2 = db.Query(`SELECT id, org_id, name FROM projects ORDER BY created_at`)
			if org != "" {
				rows, err2 = db.Query(`SELECT id, org_id, name FROM projects WHERE org_id=? ORDER BY created_at`, org)
			}
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, o, n string
				rows.Scan(&id, &o, &n)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", id, o, n)
			}
			return nil
		}},
		&cobra.Command{Use: "principal-create --org ID --name N", Short: "Create principal", RunE: func(cmd *cobra.Command, args []string) error {
			org, _ := cmd.Flags().GetString("org")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("prn")
			_, _ = db.Exec(`INSERT INTO principals(id, org_id, name, created_at) VALUES(?,?,?,?)`, id, org, name, store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "grant --principal ID --scope S --perm P", Short: "Grant permission", RunE: func(cmd *cobra.Command, args []string) error {
			prn, _ := cmd.Flags().GetString("principal")
			scope, _ := cmd.Flags().GetString("scope")
			perm, _ := cmd.Flags().GetString("perm")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`INSERT INTO grants(id, principal_id, scope, permission, created_at) VALUES(?,?,?,?,?)`, store.NewID("grt"), prn, scope, perm, store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), "granted")
			return nil
		}},
	)
	c.PersistentFlags().String("org", "", "org")
	c.PersistentFlags().String("name", "", "name")
	c.PersistentFlags().String("principal", "", "principal")
	c.PersistentFlags().String("scope", "", "scope")
	c.PersistentFlags().String("perm", "read", "perm")
	return c
}

func newTokenCmd() *cobra.Command {
	c := &cobra.Command{Use: "token", Short: "Principal tokens (hash stored, secret shown once)"}
	c.AddCommand(
		&cobra.Command{Use: "create --principal ID --name N", Short: "Create token", RunE: func(cmd *cobra.Command, args []string) error {
			prn, _ := cmd.Flags().GetString("principal")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var raw [32]byte
			_, _ = rand.Read(raw[:])
			secret := "across_" + hex.EncodeToString(raw[:])
			h := sha256.Sum256([]byte(secret))
			id := store.NewID("tok")
			_, _ = db.Exec(`INSERT INTO principal_tokens(id, principal_id, name, hash, created_at) VALUES(?,?,?,?,?)`, id, prn, name, hex.EncodeToString(h[:]), store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), secret)
			fmt.Fprintln(os.Stderr, "stored hash only; raw secret shown once")
			return nil
		}},
		&cobra.Command{Use: "list", Short: "List tokens (hashes)", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, _ := db.Query(`SELECT id, principal_id, name, created_at, revoked_at FROM principal_tokens`)
			defer rows.Close()
			for rows.Next() {
				var id, p, n, ca, ra string
				rows.Scan(&id, &p, &n, &ca, &ra)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\trevoked=%s\n", id, p, n, ca, ra)
			}
			return nil
		}},
		&cobra.Command{Use: "revoke ID", Args: cobra.ExactArgs(1), Short: "Revoke token", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`UPDATE principal_tokens SET revoked_at=? WHERE id=?`, store.NowUTC(), args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "revoked")
			return nil
		}},
	)
	c.PersistentFlags().String("principal", "", "principal")
	c.PersistentFlags().String("name", "", "name")
	return c
}

func checkToken(db interface {
	QueryRow(string, ...any) interface{}
}, token string) bool {
	return false
}

var _ = subtle.ConstantTimeCompare

func newServeCmd() *cobra.Command {
	c := &cobra.Command{Use: "serve [--addr 127.0.0.1:0]", Short: "Loopback web server (auth token, Host/Origin checks)", RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		db, home, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var raw [16]byte
		_, _ = rand.Read(raw[:])
		token := hex.EncodeToString(raw[:])
		_ = os.WriteFile(filepath.Join(home, "serve.token"), []byte(token), 0o600)
		fmt.Fprintf(cmd.OutOrStdout(), "token: %s\nlistening on %s (loopback only)\n", token, addr)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "" {
				http.Error(w, "bad host", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true,"version":"` + Version + `"}`))
		})
		mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) {
			rows, _ := db.Query(`SELECT id, display_name, authority_mode, default_branch FROM repositories ORDER BY created_at`)
			var out []map[string]string
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var id, n, a, d string
					rows.Scan(&id, &n, &a, &d)
					out = append(out, map[string]string{"id": id, "name": n, "authority": a, "default_branch": d})
				}
			}
			writeJSON(w, map[string]any{"repos": out})
		})
		mux.HandleFunc("/api/activity", func(w http.ResponseWriter, r *http.Request) {
			rows, _ := db.Query(`SELECT kind, ref_id, summary, occurred_at FROM activities ORDER BY occurred_at DESC LIMIT 50`)
			var out []map[string]string
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var k, ref, s, a string
					rows.Scan(&k, &ref, &s, &a)
					out = append(out, map[string]string{"kind": k, "ref": ref, "summary": s, "at": a})
				}
			}
			writeJSON(w, map[string]any{"activity": out})
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'")
			w.Write([]byte(webIndexHTML))
		})
		mux.HandleFunc("/git/", gitSmartHTTPHandler(home))
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Host validation: loopback only (§76).
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if host != "127.0.0.1" && host != "localhost" && host != "::1" {
				http.Error(w, "forbidden host", 403)
				return
			}
			// Origin validation for browser flows.
			if o := r.Header.Get("Origin"); o != "" {
				if !strings.HasPrefix(o, "http://127.0.0.1") && !strings.HasPrefix(o, "http://localhost") {
					http.Error(w, "forbidden origin", 403)
					return
				}
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				if r.URL.Path != "/health" {
					http.Error(w, "unauthorized", 401)
					return
				}
			}
			mux.ServeHTTP(w, r)
		}))
		return nil
	}}
	c.Flags().String("addr", "127.0.0.1:7681", "addr")
	_ = filepath.Separator
	return c
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// gitSmartHTTPHandler proxies to `git http-backend` CGI for Across-hosted
// bare repos. Loopback only; authenticated by the outer handler.
func gitSmartHTTPHandler(home string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/git/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "repo required", 400)
			return
		}
		name := strings.TrimSuffix(parts[0], ".git")
		if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
			http.Error(w, "bad repo", 400)
			return
		}
		if _, err := os.Stat(filepath.Join(home, "repositories", name+".git")); err != nil {
			http.Error(w, "repo not found", 404)
			return
		}
		pathInfo := "/" + name + ".git"
		if len(parts) == 2 {
			pathInfo += "/" + parts[1]
		}
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 32<<20))
		}
		cmd := exec.Command("git", "http-backend")
		cmd.Env = append(os.Environ(),
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_PROJECT_ROOT="+filepath.Join(home, "repositories"),
			"PATH_INFO="+pathInfo,
			"REQUEST_METHOD="+r.Method,
			"QUERY_STRING="+r.URL.RawQuery,
			"CONTENT_TYPE="+r.Header.Get("Content-Type"),
			fmt.Sprintf("CONTENT_LENGTH=%d", len(body)),
			"REMOTE_ADDR=127.0.0.1",
		)
		cmd.Stdin = bytes.NewReader(body)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			http.Error(w, "http-backend: "+stderr.String(), 502)
			return
		}
		// Split CGI headers from body.
		raw := stdout.Bytes()
		idx := bytes.Index(raw, []byte("\r\n\r\n"))
		sep := 4
		if idx < 0 {
			idx = bytes.Index(raw, []byte("\n\n"))
			sep = 2
		}
		if idx < 0 {
			http.Error(w, "bad gateway response", 502)
			return
		}
		for _, ln := range strings.Split(string(raw[:idx]), "\n") {
			ln = strings.TrimRight(ln, "\r")
			if ln == "" {
				continue
			}
			kv := strings.SplitN(ln, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if strings.EqualFold(k, "Status") {
				var code int
				fmt.Sscanf(v, "%d", &code)
				if code != 0 {
					w.WriteHeader(code)
				}
			} else {
				w.Header().Set(k, v)
			}
		}
		w.Write(raw[idx+sep:])
	}
}

const webIndexHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Across Local Alpha</title></head>
<body><h1>Across v0.0.1 Local Alpha</h1>
<p>Read-only observability console. CLI remains primary for sensitive mutations.</p>
<div id="repos"></div><div id="activity"></div>
<script>
async function load(path, el, keys){
  const t = new URLSearchParams(location.search).get('token') || '';
  const r = await fetch(path, {headers:{'Authorization':'Bearer '+t}});
  const j = await r.json();
  const div = document.getElementById(el);
  const list = j[keys] || j.repos || j.activity || [];
  list.forEach(function(item){
    var p = document.createElement('p');
    p.textContent = JSON.stringify(item);
    div.appendChild(p);
  });
}
load('/api/repos','repos','repos');
load('/api/activity','activity','activity');
</script></body></html>`

var _ = sql.ErrNoRows
