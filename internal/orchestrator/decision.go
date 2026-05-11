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
- Ask the user only when the task requires an implementation solution choice.
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
	if looksLikeImplementationChoice(lower) {
		return DecisionResult{
			DecisionType: "implementation_solution_choice",
			Summary:      summary,
			Question: &AwaitingQuestion{
				QuestionText:   summary,
				OptionsSummary: "",
				ContextExcerpt: summary,
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
	return strings.TrimSpace(decisionType) == "implementation_solution_choice"
}

func looksLikeImplementationChoice(text string) bool {
	markers := []string{
		"which approach",
		"choose",
		"option",
		"implementation choice",
		"implementation options",
		"方案",
		"怎么实现",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
