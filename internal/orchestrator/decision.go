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
- planning phase covers requirement discussion, scope clarification, spec writing, plan writing, and solution comparison.
- executing phase starts once Codex has entered development or testing work.
- once a task is in executing, you must not move it back to planning on your own. Re-entering planning requires ask_user so the human can approve it in Lark.
- in executing, do not generate detailed step-by-step implementation instructions unless the user explicitly re-approved a return to planning.
- Return strict JSON with one action: reply_to_codex, ask_user, complete_task, or wait.
- Return exactly one JSON object.
- Do not wrap the JSON in Markdown code fences.
- Do not add any explanation before or after the JSON.
- The response must be valid JSON parsable by Go's encoding/json package.`

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
	NextPhase    TaskPhase
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
	NextPhase    string `json:"next_phase"`
	Summary      string `json:"summary"`
	CodexReply   string `json:"codex_reply"`
	UserQuestion string `json:"user_question"`
}

func (p *decisionPayload) UnmarshalJSON(data []byte) error {
	type rawDecisionPayload struct {
		Action       string          `json:"action"`
		DecisionType json.RawMessage `json:"decision_type"`
		NextPhase    string          `json:"next_phase"`
		Summary      string          `json:"summary"`
		CodexReply   string          `json:"codex_reply"`
		UserQuestion string          `json:"user_question"`
	}

	var raw rawDecisionPayload
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.Action = raw.Action
	p.NextPhase = raw.NextPhase
	p.Summary = raw.Summary
	p.CodexReply = raw.CodexReply
	p.UserQuestion = raw.UserQuestion

	decisionType, err := normalizeDecisionType(raw.DecisionType)
	if err != nil {
		return err
	}
	p.DecisionType = decisionType
	return nil
}

func normalizeDecisionType(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber.String(), nil
	}

	return "", fmt.Errorf("parse decision_type: unsupported JSON value %s", trimmed)
}

func (e *ModelDecisionEngine) DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error) {
	if e == nil || e.model == nil {
		return DecisionResult{}, fmt.Errorf("decision model is not configured")
	}

	raw, err := e.model.Complete(ctx, fixedDecisionRules, BuildDecisionPrompt(in))
	if err != nil {
		return DecisionResult{}, err
	}
	raw = extractJSONPayload(raw)

	var payload decisionPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return DecisionResult{}, fmt.Errorf("parse decision JSON: %w", err)
	}

	result := DecisionResult{
		Action:       strings.TrimSpace(payload.Action),
		DecisionType: strings.TrimSpace(payload.DecisionType),
		NextPhase:    normalizeTaskPhaseValue(payload.NextPhase),
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
	builder.WriteString("\ntask_phase: ")
	builder.WriteString(string(normalizeTaskPhase(in.Task)))
	builder.WriteString("\nuser_request: ")
	builder.WriteString(userRequest)
	builder.WriteString("\nlast_input: ")
	builder.WriteString(in.Task.LastInput)
	builder.WriteString("\nlast_output_summary: ")
	builder.WriteString(in.Task.LastOutputSummary)
	builder.WriteString("\npane_current_command: ")
	builder.WriteString(in.OutputWindow.SessionState.CurrentCommand)
	builder.WriteString("\npane_dead: ")
	builder.WriteString(fmt.Sprintf("%t", in.OutputWindow.SessionState.PaneDead))
	builder.WriteString("\npane_in_mode: ")
	builder.WriteString(fmt.Sprintf("%t", in.OutputWindow.SessionState.InMode))
	builder.WriteString("\ncompletion_rule: If the requested workflow is already complete and Codex only needs another operator prompt, return complete_task.")
	builder.WriteString("\nraw_terminal_excerpt:\n")
	builder.WriteString(strings.TrimSpace(in.OutputWindow.RawOutput))
	builder.WriteString("\n\nReturn exactly one JSON object with fields: action, decision_type, next_phase, summary, codex_reply, user_question.")
	builder.WriteString("\nDo not wrap the JSON in Markdown code fences.")
	builder.WriteString("\nDo not add any explanation before or after the JSON.")
	builder.WriteString("\nThe response must be valid JSON parsable by Go's encoding/json package.")
	return builder.String()
}

func normalizeTaskPhaseValue(raw string) TaskPhase {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(TaskPhaseExecuting):
		return TaskPhaseExecuting
	case string(TaskPhasePlanning):
		return TaskPhasePlanning
	default:
		return ""
	}
}

func extractJSONPayload(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return trimmed
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return trimmed
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "```" {
		return trimmed
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}
