package event

import "testing"

func TestParseLine(t *testing.T) {
	p, ok := ParseLine(`{"type":"UserPrompt","text":"hi","agent":"codex"}`)
	if !ok || p.Type != "UserPrompt" || p.Text != "hi" {
		t.Fatal("parse failed")
	}
	if _, ok := ParseLine(`not json`); ok {
		t.Fatal("malformed should fail")
	}
	if _, ok := ParseLine(``); ok {
		t.Fatal("empty should fail")
	}
}

func TestDedupTokens(t *testing.T) {
	// §108: streaming snapshots 20/50/90 must collapse to 90, not 160.
	in := []Parsed{
		{Type: "AssistantMessage", MessageID: "m1", Text: "a", Out: 20},
		{Type: "AssistantMessage", MessageID: "m1", Text: "ab", Out: 50},
		{Type: "AssistantMessage", MessageID: "m1", Text: "abc", Out: 90},
	}
	d := Dedup(in)
	if len(d) != 1 {
		t.Fatalf("expected 1 deduped, got %d", len(d))
	}
	if d[0].Out != 90 {
		t.Fatalf("expected 90, got %d", d[0].Out)
	}
	_, total := TotalTokens(in)
	if total != 90 {
		t.Fatalf("expected total 90, got %d", total)
	}
}

func TestNativeParsers(t *testing.T) {
	if _, ok := ParseClaudeJSONL(`{"role":"user","content":"hi"}`, "claude-code"); !ok {
		t.Fatal("claude parse failed")
	}
	p, ok := ParseClaudeJSONL(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`, "claude-code")
	if !ok || p.Type != "AssistantMessage" {
		t.Fatal("claude assistant parse failed")
	}
	if _, ok := ParseCodexRollout(`{"type":"user_message","text":"hi"}`); !ok {
		t.Fatal("codex parse failed")
	}
	if _, ok := ParseOpenCodeExport(map[string]any{"role": "user", "text": "hi"}); !ok {
		t.Fatal("opencode parse failed")
	}
	if _, ok := ParseGeminiJSON(map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}); !ok {
		t.Fatal("gemini parse failed")
	}
}

func FuzzAcrossJSONL(f *testing.F) {
	f.Add(`{"type":"UserPrompt","text":"hello"}`)
	f.Add(`{"type":"ToolUse","tool":"bash"}`)
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseLine(s) // must not panic
		_, _ = ParseClaudeJSONL(s, "x")
		_, _ = ParseCodexRollout(s)
	})
}
