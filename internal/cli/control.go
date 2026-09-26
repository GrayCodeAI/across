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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/graycodeai/across/internal/store"
	acrossweb "github.com/graycodeai/across/web"
	"github.com/spf13/cobra"
)

func newControlCmd() *cobra.Command {
	c := &cobra.Command{Use: "control", Short: "Local control plane (org/project/principal/grant)"}
	c.AddCommand(
		&cobra.Command{Use: "org-create NAME", Args: cobra.ExactArgs(1), Short: "Create org", RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return invalidArgument("organization name must not be empty")
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("org")
			if _, err := db.Exec(`INSERT INTO organizations(id, name, created_at) VALUES(?,?,?)`, id, args[0], store.NowUTC()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "project-create --org ID --name N", Short: "Create project", PreRunE: requiredFlags("org", "name"), RunE: func(cmd *cobra.Command, args []string) error {
			org, _ := cmd.Flags().GetString("org")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("proj")
			if _, err := db.Exec(`INSERT INTO projects(id, org_id, name, created_at) VALUES(?,?,?,?)`, id, org, name, store.NowUTC()); err != nil {
				return err
			}
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
		&cobra.Command{Use: "principal-create --org ID --name N", Short: "Create principal", PreRunE: requiredFlags("org", "name"), RunE: func(cmd *cobra.Command, args []string) error {
			org, _ := cmd.Flags().GetString("org")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("prn")
			if _, err := db.Exec(`INSERT INTO principals(id, org_id, name, created_at) VALUES(?,?,?,?)`, id, org, name, store.NowUTC()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "grant --principal ID --scope S --perm P", Short: "Grant permission", PreRunE: requiredFlags("principal", "scope"), RunE: func(cmd *cobra.Command, args []string) error {
			prn, _ := cmd.Flags().GetString("principal")
			scope, _ := cmd.Flags().GetString("scope")
			perm, _ := cmd.Flags().GetString("perm")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if _, err := db.Exec(`INSERT INTO grants(id, principal_id, scope, permission, created_at) VALUES(?,?,?,?,?)`, store.NewID("grt"), prn, scope, perm, store.NowUTC()); err != nil {
				return err
			}
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
		&cobra.Command{Use: "create --principal ID --name N", Short: "Create token", PreRunE: requiredFlags("principal", "name"), RunE: func(cmd *cobra.Command, args []string) error {
			prn, _ := cmd.Flags().GetString("principal")
			name, _ := cmd.Flags().GetString("name")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var raw [32]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return err
			}
			secret := "across_" + hex.EncodeToString(raw[:])
			h := sha256.Sum256([]byte(secret))
			id := store.NewID("tok")
			if _, err := db.Exec(`INSERT INTO principal_tokens(id, principal_id, name, hash, created_at) VALUES(?,?,?,?,?)`, id, prn, name, hex.EncodeToString(h[:]), store.NowUTC()); err != nil {
				return err
			}
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
			result, err := db.Exec(`UPDATE principal_tokens SET revoked_at=? WHERE id=?`, store.NowUTC(), args[0])
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return notFound("token %q not found", args[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), "revoked")
			return nil
		}},
	)
	c.PersistentFlags().String("principal", "", "principal")
	c.PersistentFlags().String("name", "", "name")
	return c
}

var _ = subtle.ConstantTimeCompare

func newServeCmd() *cobra.Command {
	c := &cobra.Command{Use: "serve [--addr 127.0.0.1:7681]", Args: cobra.NoArgs, Short: "Loopback web server (auth token, Host/Origin checks)", RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		if err := validateServeAddr(addr); err != nil {
			return err
		}
		db, home, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(raw[:])
		if err := os.WriteFile(filepath.Join(home, "serve.token"), []byte(token), 0o600); err != nil {
			return err
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"version":"` + Version + `"}`))
		})
		mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) {
			rows, err := db.Query(`SELECT id, display_name, authority_mode, default_branch FROM repositories ORDER BY created_at`)
			if err != nil {
				http.Error(w, "query failed", 500)
				return
			}
			defer rows.Close()
			out := make([]map[string]string, 0)
			for rows.Next() {
				var id, name, authority, branch string
				if err := rows.Scan(&id, &name, &authority, &branch); err != nil {
					http.Error(w, "read failed", 500)
					return
				}
				out = append(out, map[string]string{"id": id, "name": name, "authority": authority, "default_branch": branch})
			}
			if err := rows.Err(); err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			writeJSON(w, map[string]any{"repos": out})
		})
		mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
			rows, err := db.Query(`SELECT id, repository_id, agent, state, started_at FROM sessions ORDER BY started_at DESC LIMIT 50`)
			if err != nil {
				http.Error(w, "query failed", 500)
				return
			}
			defer rows.Close()
			out := make([]map[string]string, 0)
			for rows.Next() {
				var id, repoID, agent, state, started string
				if err := rows.Scan(&id, &repoID, &agent, &state, &started); err != nil {
					http.Error(w, "read failed", 500)
					return
				}
				out = append(out, map[string]string{"id": id, "repository": repoID, "agent": agent, "state": state, "started": started})
			}
			if err := rows.Err(); err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			writeJSON(w, map[string]any{"sessions": out})
		})
		mux.HandleFunc("/api/checkpoints", func(w http.ResponseWriter, r *http.Request) {
			rows, err := db.Query(`SELECT id, repository_id, revision, session_id, created_at, message, basis FROM checkpoints ORDER BY created_at DESC LIMIT 50`)
			if err != nil {
				http.Error(w, "query failed", 500)
				return
			}
			defer rows.Close()
			out := make([]map[string]string, 0)
			for rows.Next() {
				var id, repoID, revision, sessionID, created, message, basis string
				if err := rows.Scan(&id, &repoID, &revision, &sessionID, &created, &message, &basis); err != nil {
					http.Error(w, "read failed", 500)
					return
				}
				out = append(out, map[string]string{"id": id, "repository": repoID, "revision": revision, "session": sessionID, "created": created, "message": message, "basis": basis})
			}
			if err := rows.Err(); err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			writeJSON(w, map[string]any{"checkpoints": out})
		})
		mux.HandleFunc("/api/activity", func(w http.ResponseWriter, r *http.Request) {
			rows, err := db.Query(`SELECT kind, ref_id, summary, occurred_at FROM activities ORDER BY occurred_at DESC LIMIT 50`)
			if err != nil {
				http.Error(w, "query failed", 500)
				return
			}
			defer rows.Close()
			out := make([]map[string]string, 0)
			for rows.Next() {
				var kind, refID, summary, occurred string
				if err := rows.Scan(&kind, &refID, &summary, &occurred); err != nil {
					http.Error(w, "read failed", 500)
					return
				}
				out = append(out, map[string]string{"kind": kind, "ref": refID, "summary": summary, "at": occurred})
			}
			if err := rows.Err(); err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			writeJSON(w, map[string]any{"activity": out})
		})
		mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte(acrossweb.AppJS))
		})
		mux.HandleFunc("/styles.css", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write([]byte(acrossweb.StylesCSS))
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			_, _ = w.Write([]byte(acrossweb.IndexHTML))
		})
		mux.HandleFunc("/git/", gitSmartHTTPHandler(home))
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requestHostAllowed(r.Host) {
				http.Error(w, "forbidden host", 403)
				return
			}
			if !requestOriginAllowed(r) {
				http.Error(w, "forbidden origin", 403)
				return
			}
			if r.URL.Path == "/" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) == 1 {
				http.SetCookie(w, &http.Cookie{Name: "across_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			if !requestAuthorized(r, token) && r.URL.Path != "/health" {
				http.Error(w, "unauthorized", 401)
				return
			}
			mux.ServeHTTP(w, r)
		})
		fmt.Fprintf(cmd.OutOrStdout(), "token: %s\nlistening on http://%s/?token=%s (loopback only)\n", token, ln.Addr().String(), token)
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}}
	c.Flags().String("addr", "127.0.0.1:7681", "loopback address")
	return c
}

func validateServeAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return invalidArgument("addr must be host:port")
	}
	if !requestHostAllowed(host) {
		return invalidArgument("addr must bind to loopback")
	}
	return nil
}

func requestHostAllowed(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requestOriginAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || !requestHostAllowed(parsed.Host) {
		return false
	}
	return true
}

func requestAuthorized(r *http.Request, token string) bool {
	header := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(header), []byte(token)) == 1 {
		return true
	}
	cookie, err := r.Cookie("across_token")
	return err == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) == 1
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

var _ = sql.ErrNoRows
