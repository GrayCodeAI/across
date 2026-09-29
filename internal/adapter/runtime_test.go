package adapter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDescriptorReportsProtocolShell(t *testing.T) {
	caps := Descriptor("opencode")
	if caps.Name != "opencode" || caps.Protocol != Protocol || caps.Status != "protocol_shell" {
		t.Fatalf("unexpected descriptor: %+v", caps)
	}
	if len(caps.SupportedMethods) != 1 || caps.SupportedMethods[0] != "ping" {
		t.Fatalf("unexpected methods: %v", caps.SupportedMethods)
	}
	if caps.CaptureEvents || caps.InstallHooks || caps.NativeResume || caps.SessionExport || caps.TokenUsage || caps.Subagents || caps.Review {
		t.Fatalf("provider capability overclaim: %+v", caps)
	}
}

func TestRunCapabilities(t *testing.T) {
	var output bytes.Buffer
	var errors bytes.Buffer
	if code := Run("codex", []string{"capabilities"}, strings.NewReader(""), &output, &errors); code != 0 {
		t.Fatalf("exit %d: %s", code, errors.String())
	}
	var caps Capabilities
	if err := json.Unmarshal(output.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Name != "codex" || caps.Qualification != "UNIMPLEMENTED" {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
}

func TestRunProtocolResponses(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"method":"ping"}`,
		`{"id":2,"method":"capture_events"}`,
		`{"id":3,"method":"unknown"}`,
		`{"id":4}`,
		`{"id":5,"protocol":"version 2","method":"ping"}`,
		`not-json`,
	}, "\n") + "\n")
	var output bytes.Buffer
	var errors bytes.Buffer
	if code := Run("gemini", nil, input, &output, &errors); code != 0 {
		t.Fatalf("exit %d: %s", code, errors.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected six responses, got %d: %s", len(lines), output.String())
	}
	var ping response
	if err := json.Unmarshal([]byte(lines[0]), &ping); err != nil {
		t.Fatal(err)
	}
	if !ping.OK || ping.Method != "ping" {
		t.Fatalf("ping response: %+v", ping)
	}
	for _, index := range []int{1, 2} {
		var unsupported response
		if err := json.Unmarshal([]byte(lines[index]), &unsupported); err != nil {
			t.Fatal(err)
		}
		if unsupported.OK || unsupported.Error == nil || unsupported.Error.Code != "UNSUPPORTED_METHOD" {
			t.Fatalf("unsupported response: %+v", unsupported)
		}
	}
	var missing response
	if err := json.Unmarshal([]byte(lines[3]), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.OK || missing.Error == nil || missing.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("missing method response: %+v", missing)
	}
	var protocol response
	if err := json.Unmarshal([]byte(lines[4]), &protocol); err != nil {
		t.Fatal(err)
	}
	if protocol.OK || protocol.Error == nil || protocol.Error.Code != "UNSUPPORTED_PROTOCOL" {
		t.Fatalf("protocol response: %+v", protocol)
	}
	var malformed response
	if err := json.Unmarshal([]byte(lines[5]), &malformed); err != nil {
		t.Fatal(err)
	}
	if malformed.OK || malformed.Error == nil || malformed.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("malformed response: %+v", malformed)
	}
}

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	var output bytes.Buffer
	var errors bytes.Buffer
	if code := Run("amp", []string{"unknown"}, strings.NewReader(""), &output, &errors); code != 2 {
		t.Fatalf("exit %d: %s", code, errors.String())
	}
	if !strings.Contains(errors.String(), "unsupported adapter command") {
		t.Fatalf("unexpected error: %s", errors.String())
	}
}
