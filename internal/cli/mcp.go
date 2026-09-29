package cli

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(*sql.DB, map[string]any) (string, error)
}

var mcpTools = []mcpTool{
	{Name: "across_search", Description: "Search indexed Across records", InputSchema: mcpSchema(map[string]any{"query": map[string]any{"type": "string"}, "repo": map[string]any{"type": "string"}}, []string{"query"}), Handler: mcpSearch},
	{Name: "across_sessions", Description: "List Across sessions", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpSessions},
	{Name: "across_checkpoints", Description: "List Across checkpoints", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpCheckpoints},
	{Name: "across_verifications", Description: "List Across verification records", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpVerifications},
	{Name: "across_issues", Description: "List Across issues", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpIssues},
	{Name: "across_changes", Description: "List Across changes", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpChanges},
	{Name: "across_activity", Description: "List recent Across activity", InputSchema: mcpSchema(map[string]any{}, nil), Handler: mcpActivity},
	{Name: "across_graph_health", Description: "Summarize indexed code symbols", InputSchema: mcpSchema(map[string]any{"repo": map[string]any{"type": "string"}}, nil), Handler: mcpGraphHealth},
}

func newMCPCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp", Args: cobra.NoArgs, Short: "Read-only MCP stdio server", RunE: func(cmd *cobra.Command, args []string) error {
		in := bufio.NewScanner(cmd.InOrStdin())
		in.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		out := cmd.OutOrStdout()
		for in.Scan() {
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(in.Bytes(), &envelope); err != nil {
				code := -32600
				message := "invalid request"
				if !json.Valid(in.Bytes()) {
					code = -32700
					message = "invalid JSON"
				}
				if err := writeMCP(out, mcpError(nil, code, message)); err != nil {
					return err
				}
				continue
			}
			var version string
			if err := json.Unmarshal(envelope["jsonrpc"], &version); err != nil || version != "2.0" {
				if err := writeMCP(out, mcpError(nil, -32600, "jsonrpc must be 2.0")); err != nil {
					return err
				}
				continue
			}
			var method string
			if err := json.Unmarshal(envelope["method"], &method); err != nil || method == "" {
				if err := writeMCP(out, mcpError(nil, -32600, "method is required")); err != nil {
					return err
				}
				continue
			}
			idRaw, hasID := envelope["id"]
			if !hasID {
				continue
			}
			var id any
			if err := json.Unmarshal(idRaw, &id); err != nil {
				if err := writeMCP(out, mcpError(nil, -32600, "invalid request id")); err != nil {
					return err
				}
				continue
			}
			switch method {
			case "initialize":
				if err := writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "across", "version": Version}, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}}}); err != nil {
					return err
				}
			case "tools/list":
				if err := writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": mcpToolDescriptors()}}); err != nil {
					return err
				}
			case "tools/call":
				var params map[string]any
				if len(envelope["params"]) == 0 || json.Unmarshal(envelope["params"], &params) != nil || params == nil {
					if err := writeMCP(out, mcpError(id, -32602, "invalid tools/call parameters")); err != nil {
						return err
					}
					continue
				}
				name, ok := params["name"].(string)
				if !ok || name == "" {
					if err := writeMCP(out, mcpError(id, -32602, "tool name is required")); err != nil {
						return err
					}
					continue
				}
				argsMap := map[string]any{}
				if raw, exists := params["arguments"]; exists && raw != nil {
					var valid bool
					argsMap, valid = raw.(map[string]any)
					if !valid {
						if err := writeMCP(out, mcpError(id, -32602, "tool arguments must be an object")); err != nil {
							return err
						}
						continue
					}
				}
				if err := validateMCPArgs(name, argsMap); err != nil {
					if err := writeMCP(out, mcpError(id, -32602, err.Error())); err != nil {
						return err
					}
					continue
				}
				text, isErr := mcpDispatch(name, argsMap)
				result := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
				if isErr {
					result["isError"] = true
				}
				if err := writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
					return err
				}
			default:
				if err := writeMCP(out, mcpError(id, -32601, "method not found")); err != nil {
					return err
				}
			}
		}
		return in.Err()
	}}
}

func mcpSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func mcpToolDescriptors() []map[string]any {
	tools := make([]map[string]any, 0, len(mcpTools))
	for _, tool := range mcpTools {
		tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema})
	}
	return tools
}

func mcpToolNames() []string {
	names := make([]string, 0, len(mcpTools))
	for _, tool := range mcpTools {
		names = append(names, tool.Name)
	}
	return names
}

func validateMCPArgs(name string, args map[string]any) error {
	var selected *mcpTool
	for i := range mcpTools {
		if mcpTools[i].Name == name {
			selected = &mcpTools[i]
			break
		}
	}
	if selected == nil {
		return nil
	}
	properties, _ := selected.InputSchema["properties"].(map[string]any)
	for key, value := range args {
		property, ok := properties[key]
		if !ok {
			return invalidArgument("unknown argument %q for %s", key, name)
		}
		propertySchema, _ := property.(map[string]any)
		if valueType, _ := propertySchema["type"].(string); valueType == "string" {
			if _, ok := value.(string); !ok {
				return invalidArgument("argument %q for %s must be a string", key, name)
			}
		}
	}
	if required, ok := selected.InputSchema["required"].([]string); ok {
		for _, key := range required {
			if value, exists := args[key]; !exists {
				return invalidArgument("argument %q is required for %s", key, name)
			} else if text, ok := value.(string); !ok || text == "" {
				return invalidArgument("argument %q is required for %s", key, name)
			}
		}
	}
	return nil
}

func mcpDispatch(name string, args map[string]any) (string, bool) {
	var selected *mcpTool
	for i := range mcpTools {
		if mcpTools[i].Name == name {
			selected = &mcpTools[i]
			break
		}
	}
	if selected == nil {
		return "UNSUPPORTED_TOOL: " + name, true
	}
	db, _, err := openDB()
	if err != nil {
		return "STORE_UNAVAILABLE: " + err.Error(), true
	}
	defer db.Close()
	out, err := selected.Handler(db, args)
	if err != nil {
		return "TOOL_FAILED: " + err.Error(), true
	}
	return out, false
}

func mcpSearch(db *sql.DB, args map[string]any) (string, error) {
	q, err := mcpRequiredString(args, "query")
	if err != nil {
		return "", err
	}
	rows, err := searchDocs(db, q, mcpString(args, "repo"))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var result strings.Builder
	for rows.Next() {
		var kind, refID, repoID, title, snippet string
		if err := rows.Scan(&kind, &refID, &repoID, &title, &snippet); err != nil {
			return "", err
		}
		fmt.Fprintf(&result, "%s %s %s\n", kind, refID, title)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if result.Len() == 0 {
		return "no results (UNKNOWN beyond this)", nil
	}
	return result.String(), nil
}

func mcpSessions(db *sql.DB, args map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT id, agent, state FROM sessions WHERE (?='' OR repository_id=?) ORDER BY started_at DESC LIMIT 20`, func(values []any) (string, error) {
		var id, agent, state string
		if err := scanStrings(values, &id, &agent, &state); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", id, agent, state), nil
	}, mcpString(args, "repo"), mcpString(args, "repo"))
}

func mcpCheckpoints(db *sql.DB, args map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT id, revision, message FROM checkpoints WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, func(values []any) (string, error) {
		var id, revision, message string
		if err := scanStrings(values, &id, &revision, &message); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", id, revision, message), nil
	}, mcpString(args, "repo"), mcpString(args, "repo"))
}

func mcpVerifications(db *sql.DB, args map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT name, exit_code, basis FROM verifications WHERE (?='' OR repository_id=?) ORDER BY started_at DESC LIMIT 20`, func(values []any) (string, error) {
		var name, basis string
		var exitCode int
		if err := scanStrings(values, &name, &exitCode, &basis); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s exit=%d %s", name, exitCode, basis), nil
	}, mcpString(args, "repo"), mcpString(args, "repo"))
}

func mcpIssues(db *sql.DB, args map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT id, title, state FROM issues WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, func(values []any) (string, error) {
		var id, title, state string
		if err := scanStrings(values, &id, &title, &state); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", id, state, title), nil
	}, mcpString(args, "repo"), mcpString(args, "repo"))
}

func mcpChanges(db *sql.DB, args map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT id, title, state FROM changes WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, func(values []any) (string, error) {
		var id, title, state string
		if err := scanStrings(values, &id, &title, &state); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", id, state, title), nil
	}, mcpString(args, "repo"), mcpString(args, "repo"))
}

func mcpActivity(db *sql.DB, _ map[string]any) (string, error) {
	return mcpQueryLines(db, `SELECT kind, ref_id, summary FROM activities ORDER BY occurred_at DESC LIMIT 20`, func(values []any) (string, error) {
		var kind, refID, summary string
		if err := scanStrings(values, &kind, &refID, &summary); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", kind, refID, summary), nil
	})
}

func mcpGraphHealth(db *sql.DB, args map[string]any) (string, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE (?='' OR repository_id=?)`, mcpString(args, "repo"), mcpString(args, "repo")).Scan(&count); err != nil {
		return "", err
	}
	return fmt.Sprintf("symbols: %d\n", count), nil
}

func mcpQueryLines(db *sql.DB, query string, format func([]any) (string, error), values ...any) (string, error) {
	rows, err := db.Query(query, values...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		line, err := format(values)
		if err != nil {
			return "", err
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return result.String(), nil
}

func scanStrings(values []any, targets ...any) error {
	if len(values) != len(targets) {
		return fmt.Errorf("unexpected column count %d", len(values))
	}
	for i, value := range values {
		switch target := targets[i].(type) {
		case *string:
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("expected string column %d", i)
			}
			*target = text
		case *int:
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("expected integer column %d", i)
			}
			*target = int(number)
		default:
			return fmt.Errorf("unsupported scan target %T", target)
		}
	}
	return nil
}

func mcpRequiredString(args map[string]any, key string) (string, error) {
	value := mcpString(args, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func mcpString(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func mcpError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func writeMCP(out interface{ Write([]byte) (int, error) }, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write MCP response: %w", err)
	}
	return nil
}
