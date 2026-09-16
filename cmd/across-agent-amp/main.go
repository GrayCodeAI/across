package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Across external adapter protocol version 1: JSON stdin/stdout.
func main() {
	name := "amp"
	if len(os.Args) > 1 && os.Args[1] == "capabilities" {
		caps := map[string]any{"name": name, "protocol": "version 1", "capture_events": true, "install_hooks": true, "native_resume": true, "session_export": false, "token_usage": true, "subagents": false, "review": false}
		b, _ := json.Marshal(caps)
		fmt.Println(string(b))
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 65536), 1<<20)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for sc.Scan() {
		var req map[string]any
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		resp := map[string]any{"protocol": "version 1", "adapter": name, "ok": true, "echo_method": req["method"]}
		b, _ := json.Marshal(resp)
		w.Write(b)
		w.Write([]byte("\n"))
		w.Flush()
	}
}
