package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const fixedDecisionRules = `You are Alter Ego's remote Codex task coordinator.
- Advance remote Codex work deterministically after terminal responders have already been applied.
- Ask the user when the task requires requirement clarification, scope confirmation, an implementation solution choice, or missing context.
- If Codex is still working, return wait.
- If Codex is waiting for a direct operator answer, either ask the user or reply to Codex directly.
- If the task is already complete and Codex is only waiting for the next operator instruction, return complete_task.
- Return strict JSON with one action: reply_to_codex, ask_user, complete_task, or wait.`

const (
	DecisionActionWait         = "wait"
	DecisionActionAskUser      = "ask_user"
	DecisionActionReplyToCodex = "reply_to_codex"
	DecisionActionCompleteTask = "complete_task"
)

type DecisionContext struct {
	Task         TaskRun
	WorkflowText string
	UserRequest  string
	OutputWindow OutputWindow
}

type DecisionResult struct {
	Action       string
	DecisionType string
	NextInput    string
	CodexReply   string
	Summary      string
	Question     *AwaitingQuestion
}

type DecisionEngine interface {
	DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error)
}

type DecisionModel interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type ModelDecisionEngine struct {
	model DecisionModel
}

func NewModelDecisionEngine(model DecisionModel) *ModelDecisionEngine {
	return &ModelDecisionEngine{
		model: model,
	}
}

type decisionPayload struct {
	Action       string `json:"action"`
	DecisionType string `json:"decision_type"`
	Summary      string `json:"summary"`
	CodexReply   string `json:"codex_reply"`
	UserQuestion string `json:"user_question"`
}

func (e *ModelDecisionEngine) DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error) {
	if e == nil || e.model == nil {
		return DecisionResult{}, fmt.Errorf("decision model is not configured")
	}

	raw, err := e.model.Complete(ctx, fixedDecisionRules, BuildDecisionPrompt(in))
	if err != nil {
		return DecisionResult{}, err
	}

	var payload decisionPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return DecisionResult{}, fmt.Errorf("parse decision JSON: %w", err)
	}

	result := DecisionResult{
		Action:       strings.TrimSpace(payload.Action),
		DecisionType: strings.TrimSpace(payload.DecisionType),
		Summary:      strings.TrimSpace(payload.Summary),
		CodexReply:   strings.TrimSpace(payload.CodexReply),
	}
	if result.Action == DecisionActionAskUser {
		questionText := strings.TrimSpace(payload.UserQuestion)
		if questionText == "" {
			questionText = strings.TrimSpace(in.Task.LastOutputSummary)
		}
		result.Question = &AwaitingQuestion{
			QuestionText:   questionText,
			OptionsSummary: "",
			ContextExcerpt: strings.TrimSpace(in.Task.LastOutputSummary),
			QuestionType:   coalesceString(result.DecisionType, "missing_context"),
			AskedAt:        time.Now().UTC(),
		}
	}
	if result.Action == "" {
		result.Action = DecisionActionWait
	}
	if result.Summary == "" {
		result.Summary = strings.TrimSpace(in.Task.LastOutputSummary)
	}
	return result, nil
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
	builder.WriteString("\ncompletion_rule: If the requested workflow is already complete and Codex only needs another operator prompt, return complete_task.")
	builder.WriteString("\nraw_terminal_excerpt:\n")
	builder.WriteString(strings.TrimSpace(in.OutputWindow.RawOutput))
	builder.WriteString("\n\nReturn JSON only with fields: action, decision_type, summary, codex_reply, user_question.")
	return builder.String()
}
