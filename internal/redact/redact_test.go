package redact

import "testing"

func TestRedact(t *testing.T) {
	out, n := Redact("api_key=abcdefgh123456 and Bearer abc.def.ghi")
	if n == 0 {
		t.Fatal("expected redaction")
	}
	if out == "api_key=abcdefgh123456 and Bearer abc.def.ghi" {
		t.Fatal("not redacted")
	}
}
