package agent

import "strings"

func diagnosticSnippet(raw string, limit int) string {
	trimmed := strings.TrimSpace(raw)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "...(truncated)"
}
