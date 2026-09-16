package redact

import "regexp"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)['\"]?([A-Za-z0-9_\-]{8,})`),
	regexp.MustCompile(`(?i)(bearer\s+[A-Za-z0-9_\-\.=~+/]+)`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(authorization\s*:\s*)(Bearer\s+[^\s]+)`),
	regexp.MustCompile(`(?i)(aws_secret_access_key\s*[:=]\s*)([A-Za-z0-9/+=]{16,})`),
	regexp.MustCompile(`(?i)(postgres(ql)?://[^/\s]*:)([^@\s]+)(@)`),
	regexp.MustCompile(`sk-(live|test)-[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`xox[bpas]-[A-Za-z0-9\-]{8,}`),
}

// Redact replaces suspected secrets with [REDACTED]. Second return is count.
func Redact(s string) (string, int) {
	n := 0
	out := s
	for _, re := range patterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			n++
			return "[REDACTED]"
		})
	}
	return out, n
}
