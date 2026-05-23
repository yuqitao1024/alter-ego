package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const supervisorRequestRules = `You are Alter Ego's Codex supervisor.
- Alter Ego is a supervisor, not a co-worker.
- Handle two event types: server_request and turn_completed.
- Classify the event as one of: plan_decision, execution_approval, completion_signal, progress_update, or ignore.
- For server_request events, prefer plan_decision when the request asks for scope, architecture, prioritization, or other product/solution choices.
- For server_request events, prefer execution_approval when the request is a routine continue/resume/approval that does not change scope.
- For turn_completed events, use completion_signal only when the completed turn clearly claims the task is done enough to trigger the one fixed completion-check prompt.
- For turn_completed events, prefer plan_decision when the summary is clearly asking the user to choose scope, architecture, priorities, or options.
- For turn_completed events, prefer execution_approval only when a routine continue/resume reply is clearly appropriate.
- For turn_completed events, do not invent a reply unless the summary clearly asks for the next instruction or approval.
- Set reply_policy to auto_continue only when it is safe to answer automatically.
- Set reply_policy to ask_user when the user should decide in Feishu.
- Return strict JSON only.`

const progressUpdateRules = `You are Alter Ego's Codex supervisor.
- Evaluate whether the latest summary represents material progress worth reporting to the user.
- Never suggest sending input to Codex.
- Return strict JSON only.`

const completionSignalRules = `You are Alter Ego's Codex supervisor.
- Evaluate completion-related summaries only.
- completion_disposition must be one of: none, signal_complete, confirmed_done, reported_remaining.
- signal_complete means Codex appears to claim the task is done and should receive the one fixed completion-check prompt.
- confirmed_done means Codex explicitly confirmed all requested work is complete after the completion-check prompt.
- reported_remaining means Codex explicitly said work remains after the completion-check prompt.
- Return strict JSON only.`

type SupervisorContext struct {
	Task          TaskRun
	Request       TaskServerRequest
	TurnCompleted *TurnCompletedEvent
	EventType     string
	Summary       string
}

type DecisionEngine interface {
	ClassifySupervisorEvent(ctx context.Context, in SupervisorContext) (SupervisorDecision, error)
	EvaluateProgressUpdate(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error)
	EvaluateCompletionSignal(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error)
}

type DecisionModel interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type ModelDecisionEngine struct {
	model DecisionModel
}

func NewModelDecisionEngine(model DecisionModel) *ModelDecisionEngine {
	return &ModelDecisionEngine{model: model}
}

func (e *ModelDecisionEngine) ClassifySupervisorEvent(ctx context.Context, in SupervisorContext) (SupervisorDecision, error) {
	if e == nil || e.model == nil {
		return SupervisorDecision{}, fmt.Errorf("decision model is not configured")
	}
	userPrompt := buildSupervisorRequestPrompt(in)
	return e.completeStructured(ctx, supervisorRequestRules, userPrompt)
}

func (e *ModelDecisionEngine) EvaluateProgressUpdate(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error) {
	if e == nil || e.model == nil {
		return SupervisorDecision{}, fmt.Errorf("decision model is not configured")
	}
	userPrompt := buildProgressPrompt(task, summary)
	return e.completeStructured(ctx, progressUpdateRules, userPrompt)
}

func (e *ModelDecisionEngine) EvaluateCompletionSignal(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error) {
	if e == nil || e.model == nil {
		return SupervisorDecision{}, fmt.Errorf("decision model is not configured")
	}
	userPrompt := buildCompletionPrompt(task, summary)
	return e.completeStructured(ctx, completionSignalRules, userPrompt)
}

func (e *ModelDecisionEngine) completeStructured(ctx context.Context, systemPrompt, userPrompt string) (SupervisorDecision, error) {
	raw, err := e.model.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return SupervisorDecision{}, err
	}

	raw = extractJSONPayload(raw)
	trimmed := strings.TrimSpace(raw)

	var decision SupervisorDecision
	if err := json.Unmarshal([]byte(trimmed), &decision); err != nil {
		return SupervisorDecision{}, fmt.Errorf("parse decision JSON: %w (raw_len=%d raw_preview=%q)", err, len(trimmed), diagnosticDecisionSnippet(trimmed, 240))
	}
	return normalizeSupervisorDecision(decision), nil
}

func normalizeSupervisorDecision(decision SupervisorDecision) SupervisorDecision {
	decision.Classification = SupervisorClassification(strings.ToLower(strings.TrimSpace(string(decision.Classification))))
	decision.ReplyPolicy = ReplyPolicy(strings.ToLower(strings.TrimSpace(string(decision.ReplyPolicy))))
	decision.CompletionDisposition = CompletionDisposition(strings.ToLower(strings.TrimSpace(string(decision.CompletionDisposition))))
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.UserUpdate = strings.TrimSpace(decision.UserUpdate)
	decision.UserQuestion = strings.TrimSpace(decision.UserQuestion)
	decision.CodexReply = strings.TrimSpace(decision.CodexReply)
	return decision
}

func LoadWorkflow(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read workflow %q: %w", path, err)
	}
	return string(data), nil
}

func buildSupervisorRequestPrompt(in SupervisorContext) string {
	var builder strings.Builder
	builder.WriteString("[Task]\n")
	builder.WriteString("task_id: ")
	builder.WriteString(in.Task.TaskID)
	builder.WriteString("\nevent_type: ")
	builder.WriteString(firstNonEmpty(in.EventType, "server_request"))
	builder.WriteString("\nuser_request: ")
	builder.WriteString(in.Task.UserRequest)
	builder.WriteString("\ncompletion_check_status: ")
	builder.WriteString(string(in.Task.CompletionCheckStatus))
	if in.TurnCompleted != nil {
		builder.WriteString("\nturn_id: ")
		builder.WriteString(in.TurnCompleted.TurnID)
		builder.WriteString("\nturn_status: ")
		builder.WriteString(in.TurnCompleted.ThreadStatus)
		builder.WriteString("\nthread_active_flags: ")
		builder.WriteString(strings.Join(in.TurnCompleted.ThreadActiveFlags, ","))
	}
	if strings.EqualFold(strings.TrimSpace(in.EventType), "turn_completed") {
		builder.WriteString("\n\n[Turn Completed]\n")
		builder.WriteString("turn_id: ")
		builder.WriteString(in.TurnCompleted.TurnID)
		builder.WriteString("\nturn_status: ")
		builder.WriteString(in.TurnCompleted.ThreadStatus)
		builder.WriteString("\nthread_active_flags: ")
		builder.WriteString(strings.Join(in.TurnCompleted.ThreadActiveFlags, ","))
	} else {
		builder.WriteString("\n\n[Server Request]\n")
		builder.WriteString("request_id: ")
		builder.WriteString(in.Request.RequestID)
		builder.WriteString("\nrequest_type: ")
		builder.WriteString(string(in.Request.RequestType))
		builder.WriteString("\nrequest_payload: ")
		builder.WriteString(in.Request.RequestPayload)
	}
	builder.WriteString("\n\n[Latest Summary]\n")
	builder.WriteString(promptSnippet(in.Summary, 4000))
	builder.WriteString("\n\nReturn exactly one JSON object with fields: classification, should_reply_codex, should_notify_user, reply_policy, reason, user_update, user_question, codex_reply.")
	builder.WriteString("\nDo not wrap the JSON in Markdown code fences.")
	builder.WriteString("\nDo not add any explanation before or after the JSON.")
	return builder.String()
}

func buildProgressPrompt(task TaskRun, summary string) string {
	var builder strings.Builder
	builder.WriteString("[Task]\n")
	builder.WriteString("task_id: ")
	builder.WriteString(task.TaskID)
	builder.WriteString("\nuser_request: ")
	builder.WriteString(task.UserRequest)
	builder.WriteString("\nlast_output_summary: ")
	builder.WriteString(promptSnippet(task.LastOutputSummary, 4000))
	builder.WriteString("\n\n[Latest Summary]\n")
	builder.WriteString(promptSnippet(summary, 4000))
	builder.WriteString("\n\nReturn exactly one JSON object with fields: classification, should_notify_user, user_update, reason.")
	builder.WriteString("\nDo not wrap the JSON in Markdown code fences.")
	builder.WriteString("\nDo not add any explanation before or after the JSON.")
	return builder.String()
}

func buildCompletionPrompt(task TaskRun, summary string) string {
	var builder strings.Builder
	builder.WriteString("[Task]\n")
	builder.WriteString("task_id: ")
	builder.WriteString(task.TaskID)
	builder.WriteString("\ncompletion_check_status: ")
	builder.WriteString(string(task.CompletionCheckStatus))
	builder.WriteString("\nuser_request: ")
	builder.WriteString(task.UserRequest)
	builder.WriteString("\n\n[Latest Summary]\n")
	builder.WriteString(promptSnippet(summary, 4000))
	builder.WriteString("\n\nReturn exactly one JSON object with fields: classification, completion_disposition, should_notify_user, user_update, reason.")
	builder.WriteString("\nDo not wrap the JSON in Markdown code fences.")
	builder.WriteString("\nDo not add any explanation before or after the JSON.")
	return builder.String()
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

func diagnosticDecisionSnippet(raw string, limit int) string {
	trimmed := strings.TrimSpace(raw)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "...(truncated)"
}

func promptSnippet(raw string, limit int) string {
	trimmed := strings.TrimSpace(raw)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "...(truncated)"
}
