package cli

import (
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
