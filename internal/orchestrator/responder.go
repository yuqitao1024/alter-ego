package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type TerminalResponse struct {
	Name     string
	Handled  bool
	AutoInput string
	AutoKey  string
	Question *AwaitingQuestion
}

func EvaluateTerminalResponse(task TaskRun, window OutputWindow, now time.Time) TerminalResponse {
	text := strings.ToLower(strings.TrimSpace(window.RawOutput + "\n" + window.Summary))
	if looksLikeLoginPrompt(text) {
		return TerminalResponse{
			Name:    "login_required_prompt",
			Handled: true,
			Question: &AwaitingQuestion{
				QuestionText:   firstNonEmpty(strings.TrimSpace(window.RawOutput), strings.TrimSpace(window.Summary)),
				OptionsSummary: "Remote Codex requires login before the task can continue.",
				ContextExcerpt: strings.TrimSpace(window.Summary),
				QuestionType:   "login_required",
				AskedAt:        now,
			},
		}
	}
	if looksLikeUsageLimitPrompt(text) {
		return TerminalResponse{
			Name:    "usage_limit_prompt",
			Handled: true,
			Question: &AwaitingQuestion{
				QuestionText:   firstNonEmpty(strings.TrimSpace(window.RawOutput), strings.TrimSpace(window.Summary)),
				OptionsSummary: "Remote Codex cannot continue because the current account or credits are exhausted.",
				ContextExcerpt: strings.TrimSpace(window.Summary),
				QuestionType:   "usage_limit",
				AskedAt:        now,
			},
		}
	}
	if looksLikeTrustDirectoryPrompt(text) {
		digest := ScreenDigest(window)
		if task.LastScreenDigest == digest && strings.TrimSpace(task.LastInput) == "1" {
			return TerminalResponse{Name: "trust_directory_prompt", Handled: true}
		}
		return TerminalResponse{
			Name:      "trust_directory_prompt",
			Handled:   true,
			AutoInput: "1",
		}
	}
	if looksLikePlanPrompt(text) {
		digest := ScreenDigest(window)
		if task.LastScreenDigest == digest && strings.TrimSpace(task.LastInput) == "[key] Escape" {
			return TerminalResponse{Name: "plan_prompt_dismiss", Handled: true}
		}
		return TerminalResponse{
			Name:    "plan_prompt_dismiss",
			Handled: true,
			AutoKey: "Escape",
		}
	}
	return TerminalResponse{}
}

func ScreenDigest(window OutputWindow) string {
	input := strings.TrimSpace(window.RawOutput)
	if input == "" {
		input = strings.TrimSpace(window.Summary)
	}
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func looksLikeTrustDirectoryPrompt(text string) bool {
	return strings.Contains(text, "do you trust the contents of this directory") &&
		strings.Contains(text, "yes, continue") &&
		strings.Contains(text, "no, quit")
}

func looksLikeLoginPrompt(text string) bool {
	return strings.Contains(text, "welcome to codex") &&
		(strings.Contains(text, "sign in with chatgpt") ||
			strings.Contains(text, "sign in with device code") ||
			strings.Contains(text, "provide your own api key"))
}

func looksLikeUsageLimitPrompt(text string) bool {
	return strings.Contains(text, "you've hit your usage limit") ||
		(strings.Contains(text, "purchase more credits") && strings.Contains(text, "try again")) ||
		(strings.Contains(text, "upgrade to pro") && strings.Contains(text, "usage limit"))
}

func looksLikePlanPrompt(text string) bool {
	return strings.Contains(text, "create a plan?") &&
		strings.Contains(text, "use plan mode") &&
		strings.Contains(text, "esc") &&
		strings.Contains(text, "dismiss")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
