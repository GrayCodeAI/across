package adapter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const Protocol = "version 1"

var supportedMethods = []string{"ping"}

func IsKnownProvider(name string) bool {
	switch name {
	case "claude-code", "codex", "cursor", "gemini", "opencode", "qwen", "factory-droid", "amp", "goose":
		return true
	default:
		return false
	}
}

type Capabilities struct {
	Name             string   `json:"name"`
	Protocol         string   `json:"protocol"`
	Status           string   `json:"status"`
	SupportedMethods []string `json:"supported_methods"`
	CaptureEvents    bool     `json:"capture_events"`
	InstallHooks     bool     `json:"install_hooks"`
	NativeResume     bool     `json:"native_resume"`
	SessionExport    bool     `json:"session_export"`
	TokenUsage       bool     `json:"token_usage"`
	Subagents        bool     `json:"subagents"`
	Review           bool     `json:"review"`
	Qualification    string   `json:"qualification"`
}

type request struct {
	Protocol string `json:"protocol"`
	ID       any    `json:"id"`
	Method   string `json:"method"`
}

type responseError struct {
	Code             string   `json:"code"`
	Message          string   `json:"message"`
	Method           string   `json:"method,omitempty"`
	SupportedMethods []string `json:"supported_methods,omitempty"`
}

type response struct {
	Protocol string         `json:"protocol"`
	Adapter  string         `json:"adapter"`
	ID       any            `json:"id,omitempty"`
	OK       bool           `json:"ok"`
	Method   string         `json:"method,omitempty"`
	Error    *responseError `json:"error,omitempty"`
}

func Descriptor(name string) Capabilities {
	return Capabilities{
		Name:             name,
		Protocol:         Protocol,
		Status:           "protocol_shell",
		SupportedMethods: append([]string(nil), supportedMethods...),
		Qualification:    "UNIMPLEMENTED",
	}
}

func Run(name string, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 {
		if len(args) != 1 || args[0] != "capabilities" {
			fmt.Fprintln(errOut, "unsupported adapter command")
			return 2
		}
		if err := writeJSON(out, Descriptor(name)); err != nil {
			fmt.Fprintln(errOut, "failed to write capabilities")
			return 1
		}
		if _, err := fmt.Fprintln(out); err != nil {
			fmt.Fprintln(errOut, "failed to write capabilities")
			return 1
		}
		return 0
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err := writeResponse(writer, response{
				Protocol: Protocol,
				Adapter:  name,
				OK:       false,
				Error:    &responseError{Code: "INVALID_REQUEST", Message: "invalid JSON request"},
			}); err != nil {
				fmt.Fprintln(errOut, "failed to write response")
				return 1
			}
			continue
		}
		if strings.TrimSpace(req.Method) == "" {
			if err := writeResponse(writer, response{
				Protocol: Protocol,
				Adapter:  name,
				ID:       req.ID,
				OK:       false,
				Error:    &responseError{Code: "INVALID_REQUEST", Message: "method is required"},
			}); err != nil {
				fmt.Fprintln(errOut, "failed to write response")
				return 1
			}
			continue
		}
		if req.Protocol != "" && req.Protocol != Protocol {
			if err := writeResponse(writer, response{
				Protocol: Protocol,
				Adapter:  name,
				ID:       req.ID,
				OK:       false,
				Error: &responseError{
					Code:    "UNSUPPORTED_PROTOCOL",
					Message: "unsupported protocol version",
					Method:  req.Method,
				},
			}); err != nil {
				fmt.Fprintln(errOut, "failed to write response")
				return 1
			}
			continue
		}
		if req.Method != "ping" {
			if err := writeResponse(writer, response{
				Protocol: Protocol,
				Adapter:  name,
				ID:       req.ID,
				OK:       false,
				Error: &responseError{
					Code:             "UNSUPPORTED_METHOD",
					Message:          "method not supported",
					Method:           req.Method,
					SupportedMethods: append([]string(nil), supportedMethods...),
				},
			}); err != nil {
				fmt.Fprintln(errOut, "failed to write response")
				return 1
			}
			continue
		}
		if err := writeResponse(writer, response{
			Protocol: Protocol,
			Adapter:  name,
			ID:       req.ID,
			OK:       true,
			Method:   req.Method,
		}); err != nil {
			fmt.Fprintln(errOut, "failed to write response")
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(errOut, "failed to read request")
		return 1
	}
	if err := writer.Flush(); err != nil {
		fmt.Fprintln(errOut, "failed to write response")
		return 1
	}
	return 0
}

func writeResponse(w io.Writer, value response) error {
	if err := writeJSON(w, value); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if buffered, ok := w.(*bufio.Writer); ok {
		return buffered.Flush()
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}
