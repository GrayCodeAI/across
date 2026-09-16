package event

import (
	"encoding/json"
	"strings"
)

// Canonical types (§22).
var canonical = map[string]bool{
	"SessionStart": true, "TurnStart": true, "UserPrompt": true, "AssistantMessage": true,
	"ToolUse": true, "SubagentStart": true, "SubagentEnd": true, "TurnEnd": true,
	"Compaction": true, "SessionEnd": true,
}

// Parsed is a safe projection of one JSONL line.
type Parsed struct {
	Type      string
	Text      string
	Agent     string
	Model     string
	Tool      string
	MessageID string
	TurnID    string
	ParentSID string
	In, Out   int
	Cached    int
	Cost      float64
	Provider  string // native provider event type, kept separately (§22)
}

// ParseLine parses one Across JSONL line; returns ok=false for malformed.
func ParseLine(line string) (Parsed, bool) {
	if len(line) > 1<<20 || len(line) == 0 {
		return Parsed{}, false
	}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return Parsed{}, false
	}
	t, _ := m["type"].(string)
	if !canonical[t] {
		t = "AssistantMessage"
	}
	p := Parsed{Type: t}
	p.Text, _ = m["text"].(string)
	p.Agent, _ = m["agent"].(string)
	p.Model, _ = m["model"].(string)
	p.Tool, _ = m["tool"].(string)
	p.MessageID, _ = m["message_id"].(string)
	p.TurnID, _ = m["turn_id"].(string)
	p.ParentSID, _ = m["parent_session_id"].(string)
	p.Provider, _ = m["provider_event_type"].(string)
	p.In = num(m["input_tokens"])
	p.Out = num(m["output_tokens"])
	p.Cached = num(m["cached_tokens"])
	if c, ok := m["recorded_cost"].(json.Number); ok {
		f, _ := c.Float64()
		p.Cost = f
	}
	return p, true
}

func num(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// Dedup collapses streaming partials by message ID (§33): for repeated
// message_id, keep the LAST text but the MAX token counts (never sum partials).
// Events without message_id pass through unchanged. Returns deduped slice.
func Dedup(in []Parsed) []Parsed {
	lastIdx := map[string]int{}
	out := make([]Parsed, 0, len(in))
	maxIn := map[string]int{}
	maxOut := map[string]int{}
	for _, p := range in {
		if p.MessageID == "" {
			out = append(out, p)
			continue
		}
		if p.In > maxIn[p.MessageID] {
			maxIn[p.MessageID] = p.In
		}
		if p.Out > maxOut[p.MessageID] {
			maxOut[p.MessageID] = p.Out
		}
		if idx, ok := lastIdx[p.MessageID]; ok {
			// find position in out: stored as offset marker
			out[idx] = p // last text wins
		} else {
			lastIdx[p.MessageID] = len(out)
			out = append(out, p)
		}
	}
	for i := range out {
		if out[i].MessageID != "" {
			out[i].In = maxIn[out[i].MessageID]
			out[i].Out = maxOut[out[i].MessageID]
		}
	}
	return out
}

// TotalTokens returns collapsed totals (no double-count of partials).
func TotalTokens(in []Parsed) (inp, outp int) {
	d := Dedup(in)
	for _, p := range d {
		inp += p.In
		outp += p.Out
	}
	return inp, outp
}

// --- Native transcript parsers (§30). Each maps provider-native shapes to
// canonical Parsed events. Unknown fields are dropped (privacy boundary).

// ParseClaudeJSONL parses one line of Claude/Cursor JSONL.
// Native: {"role":"user"|"assistant","content":"...","model":"...","usage":{"input_tokens":..}}
func ParseClaudeJSONL(line, agent string) (Parsed, bool) {
	if len(line) > 1<<20 || line == "" {
		return Parsed{}, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return Parsed{}, false
	}
	p := Parsed{Agent: agent}
	role, _ := m["role"].(string)
	switch role {
	case "user":
		p.Type = "UserPrompt"
	case "assistant":
		p.Type = "AssistantMessage"
	default:
		// tool_use shapes
		if _, ok := m["tool_use"]; ok {
			p.Type = "ToolUse"
			if t, ok := m["tool_use"].(map[string]any); ok {
				p.Tool, _ = t["name"].(string)
			}
		} else {
			p.Type = "AssistantMessage"
		}
	}
	if c, ok := m["content"].(string); ok {
		p.Text = c
	} else if arr, ok := m["content"].([]any); ok {
		var sb strings.Builder
		for _, b := range arr {
			if bm, ok := b.(map[string]any); ok {
				if t, _ := bm["type"].(string); t == "text" {
					if s, _ := bm["text"].(string); s != "" {
						sb.WriteString(s + "\n")
					}
				}
				if t, _ := bm["type"].(string); t == "tool_use" {
					p.Type = "ToolUse"
					p.Tool, _ = bm["name"].(string)
				}
			}
		}
		p.Text = sb.String()
	}
	p.Model, _ = m["model"].(string)
	p.MessageID, _ = m["id"].(string)
	if u, ok := m["usage"].(map[string]any); ok {
		p.In = numJSON(u["input_tokens"])
		p.Out = numJSON(u["output_tokens"])
		p.Cached = numJSON(u["cache_read_input_tokens"])
	}
	p.Provider = "claude_jsonl"
	return p, true
}

// ParseCodexRollout parses one Codex rollout JSONL line.
// Native: {"type":"message"|"tool_call",...}
func ParseCodexRollout(line string) (Parsed, bool) {
	if len(line) > 1<<20 || line == "" {
		return Parsed{}, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return Parsed{}, false
	}
	p := Parsed{Agent: "codex", Provider: "codex_rollout"}
	t, _ := m["type"].(string)
	switch t {
	case "user_message":
		p.Type = "UserPrompt"
	case "tool_call":
		p.Type = "ToolUse"
		p.Tool, _ = m["tool"].(string)
		if p.Tool == "" {
			p.Tool, _ = m["name"].(string)
		}
	default:
		p.Type = "AssistantMessage"
	}
	p.Text, _ = m["text"].(string)
	if p.Text == "" {
		p.Text, _ = m["content"].(string)
	}
	p.MessageID, _ = m["id"].(string)
	p.Model, _ = m["model"].(string)
	if u, ok := m["usage"].(map[string]any); ok {
		p.In = numJSON(u["input_tokens"])
		p.Out = numJSON(u["output_tokens"])
	}
	return p, true
}

// ParseGeminiJSON parses a Gemini session JSON object (single message).
func ParseGeminiJSON(obj map[string]any) (Parsed, bool) {
	p := Parsed{Agent: "gemini", Provider: "gemini_session"}
	role, _ := obj["role"].(string)
	if role == "user" {
		p.Type = "UserPrompt"
	} else {
		p.Type = "AssistantMessage"
	}
	parts, _ := obj["parts"].([]any)
	var sb strings.Builder
	for _, part := range parts {
		if pm, ok := part.(map[string]any); ok {
			if s, _ := pm["text"].(string); s != "" {
				sb.WriteString(s + "\n")
			}
			if _, ok := pm["functionCall"]; ok {
				p.Type = "ToolUse"
			}
		}
	}
	p.Text = sb.String()
	return p, true
}

// ParseOpenCodeExport parses an OpenCode session export message.
func ParseOpenCodeExport(obj map[string]any) (Parsed, bool) {
	p := Parsed{Agent: "opencode", Provider: "opencode_export"}
	role, _ := obj["role"].(string)
	if role == "user" {
		p.Type = "UserPrompt"
	} else {
		p.Type = "AssistantMessage"
	}
	p.Text, _ = obj["text"].(string)
	if p.Text == "" {
		p.Text, _ = obj["content"].(string)
	}
	p.MessageID, _ = obj["id"].(string)
	p.Model, _ = obj["model"].(string)
	return p, true
}

func numJSON(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
