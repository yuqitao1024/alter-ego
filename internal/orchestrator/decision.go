package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const fixedDecisionRules = `You are Alter Ego's remote Codex task coordinator.
- Advance remote Codex work deterministically.
- Ask the user when the task requires requirement clarification, scope confirmation, an implementation solution choice, or missing context.
- Continue automatically for all other decisions.
- Summarize remote progress clearly before asking for input.`

type DecisionContext struct {
	Task         TaskRun
	WorkflowText string
	UserRequest  string
}

type DecisionResult struct {
	DecisionType string
	NextInput    string
	Summary      string
	Question     *AwaitingQuestion
}

type DecisionEngine interface {
	DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error)
}

type HeuristicDecisionEngine struct{}

func NewHeuristicDecisionEngine() *HeuristicDecisionEngine {
	return &HeuristicDecisionEngine{}
}

func (e *HeuristicDecisionEngine) DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error) {
	_ = ctx

	summary := strings.TrimSpace(in.Task.LastOutputSummary)
	lower := strings.ToLower(summary)
	if decisionType := detectDecisionType(lower); decisionType != "" {
		return DecisionResult{
			DecisionType: decisionType,
			Summary:      summary,
			Question: &AwaitingQuestion{
				QuestionText:   summary,
				OptionsSummary: "",
				ContextExcerpt: summary,
				QuestionType:   decisionType,
				AskedAt:        time.Now().UTC(),
			},
		}, nil
	}

	return DecisionResult{
		DecisionType: "continue_execution",
		Summary:      summary,
	}, nil
}

func LoadWorkflow(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read workflow %q: %w", path, err)
	}
	return string(data), nil
}

func BuildDecisionPrompt(in DecisionContext) string {
	userRequest := in.UserRequest
	if strings.TrimSpace(userRequest) == "" {
		userRequest = in.Task.UserRequest
	}

	var builder strings.Builder
	builder.WriteString(fixedDecisionRules)
	builder.WriteString("\n\n[Workflow]\n")
	builder.WriteString(strings.TrimSpace(in.WorkflowText))
	builder.WriteString("\n\n[Runtime Context]\n")
	builder.WriteString("task_id: ")
	builder.WriteString(in.Task.TaskID)
	builder.WriteString("\nrepository_id: ")
	builder.WriteString(in.Task.RepositoryID)
	builder.WriteString("\nmachine_id: ")
	builder.WriteString(in.Task.MachineID)
	builder.WriteString("\nremote_codex_session_id: ")
	builder.WriteString(in.Task.RemoteCodexSessionID)
	builder.WriteString("\nuser_request: ")
	builder.WriteString(userRequest)
	builder.WriteString("\nlast_input: ")
	builder.WriteString(in.Task.LastInput)
	builder.WriteString("\nlast_output_summary: ")
	builder.WriteString(in.Task.LastOutputSummary)
	return builder.String()
}

func ShouldEscalateDecision(decisionType string) bool {
	switch strings.TrimSpace(decisionType) {
	case "requirement_clarification", "scope_confirmation", "implementation_solution_choice", "missing_context":
		return true
	default:
		return false
	}
}

func detectDecisionType(text string) string {
	switch {
	case containsAny(text, []string{
		"need clarification",
		"clarify the requirement",
		"clarification on the requirement",
		"clarify before i proceed",
		"需要澄清",
		"请澄清",
	}):
		return "requirement_clarification"
	case containsAny(text, []string{
		"confirm the scope",
		"scope confirmation",
		"please confirm the scope",
		"范围确认",
		"确认范围",
	}):
		return "scope_confirmation"
	case containsAny(text, []string{
		"which approach",
		"choose",
		"option",
		"implementation choice",
		"implementation options",
		"方案",
		"怎么实现",
	}):
		return "implementation_solution_choice"
	case containsAny(text, []string{
		"missing context",
		"need more context",
		"need additional context",
		"not enough context",
		"缺少上下文",
		"信息不足",
	}):
		return "missing_context"
	default:
		return ""
	}
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
