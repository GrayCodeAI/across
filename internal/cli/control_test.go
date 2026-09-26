package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	acrossweb "github.com/graycodeai/across/web"
)

func TestWebAssetsAreAligned(t *testing.T) {
	for _, required := range []string{"app.js", "styles.css", "/api/sessions", "/api/checkpoints", "frame-ancestors 'none'"} {
		if !strings.Contains(acrossweb.IndexHTML, required) && !strings.Contains(acrossweb.AppJS, required) {
			t.Fatalf("web asset is missing %q", required)
		}
	}
	if strings.Contains(acrossweb.AppJS, "innerHTML") {
		t.Fatal("web app uses innerHTML")
	}
}

func TestServeRequestValidation(t *testing.T) {
	if !requestHostAllowed("127.0.0.1:7681") || !requestHostAllowed("[::1]:7681") || requestHostAllowed("evil.example:80") {
		t.Fatal("host validation is incorrect")
	}
	allowed := httptest.NewRequest("GET", "http://127.0.0.1:7681/api/repos", nil)
	allowed.Header.Set("Origin", "http://localhost:7681")
	if !requestOriginAllowed(allowed) {
		t.Fatal("loopback origin was rejected")
	}
	blocked := httptest.NewRequest("GET", "http://127.0.0.1:7681/api/repos", nil)
	blocked.Header.Set("Origin", "http://localhost.evil.example")
	if requestOriginAllowed(blocked) {
		t.Fatal("lookalike origin was accepted")
	}
}

func TestCookieAuthRequiresSameOrigin(t *testing.T) {
	const token = "0123456789abcdef"
	request := func(headers map[string]string, cookie string) *http.Request {
		r := httptest.NewRequest("GET", "http://127.0.0.1:7681/api/repos", nil)
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "across_token", Value: cookie})
		}
		return r
	}
	cases := []struct {
		name    string
		headers map[string]string
		cookie  string
		want    bool
	}{
		{"bearer from another site", map[string]string{"Authorization": "Bearer " + token, "Sec-Fetch-Site": "cross-site"}, "", true},
		{"cookie same-origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, token, true},
		{"cookie top-level navigation", map[string]string{"Sec-Fetch-Site": "none"}, token, true},
		{"cookie non-browser client", nil, token, true},
		{"cookie matching origin", map[string]string{"Origin": "http://127.0.0.1:7681"}, token, true},
		{"cookie from another localhost port", map[string]string{"Sec-Fetch-Site": "same-site"}, token, false},
		{"cookie cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, token, false},
		{"cookie with other loopback origin", map[string]string{"Origin": "http://localhost:3000"}, token, false},
		{"cookie with opaque origin", map[string]string{"Origin": "null"}, token, false},
		{"wrong cookie", map[string]string{"Sec-Fetch-Site": "same-origin"}, "wrong", false},
		{"no credentials", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := requestAuthorized(request(c.headers, c.cookie), token); got != c.want {
				t.Fatalf("requestAuthorized = %v, want %v", got, c.want)
			}
		})
	}
}
