package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflowReadsFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.md")
	workflowText := "Workflow:\n1. Inspect repo\n2. Continue\n"
	if err := os.WriteFile(workflowPath, []byte(workflowText), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	workflow, err := LoadWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("LoadWorkflow returned error: %v", err)
	}
	if workflow != workflowText {
		t.Fatalf("workflow = %q, want %q", workflow, workflowText)
	}
}

func TestBuildSupervisorRequestPromptIncludesTaskAndRequestFields(t *testing.T) {
	t.Parallel()

	prompt := buildSupervisorRequestPrompt(SupervisorContext{
		Task: TaskRun{
			TaskID:      "task-123",
			UserRequest: "Implement task orchestration",
		},
		Request: TaskServerRequest{
			RequestID:      "req-1",
			RequestType:    ServerRequestTypeUserInput,
			RequestPayload: `{"prompt":"continue?"}`,
		},
		Summary: "Codex is asking whether it should continue.",
	})

	for _, part := range []string{"task-123", "Implement task orchestration", "req-1", "request_user_input", `{"prompt":"continue?"}`, "Codex is asking whether it should continue."} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("prompt missing %q:\n%s", part, prompt)
		}
	}
}

func TestBuildSupervisorRequestPromptDoesNotDuplicateCompletedTurnSummary(t *testing.T) {
	t.Parallel()

	summary := "completed turn summary"
	prompt := buildSupervisorRequestPrompt(SupervisorContext{
		Task: TaskRun{
			TaskID:      "task-123",
			UserRequest: "Implement task orchestration",
		},
		EventType: "turn_completed",
		Summary:   summary,
		TurnCompleted: &TurnCompletedEvent{
			TurnID:            "turn-1",
			ThreadStatus:      "completed",
			Summary:           summary,
			ThreadActiveFlags: []string{"idle"},
		},
	})

	if count := strings.Count(prompt, summary); count != 1 {
		t.Fatalf("summary occurrences = %d, want 1\n%s", count, prompt)
	}
}

func TestBuildProgressPromptTruncatesLargeSummaries(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("x", 12000)
	prompt := buildProgressPrompt(TaskRun{
		TaskID:            "task-progress",
		UserRequest:       "do work",
		LastOutputSummary: large,
	}, large)

	if strings.Count(prompt, large) != 0 {
		t.Fatalf("prompt should not contain full large summary")
	}
	if !strings.Contains(prompt, "...(truncated)") {
		t.Fatalf("prompt missing truncation marker")
	}
}

func TestModelDecisionEngineParsesSupervisorClassificationSchema(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"classification":"execution_approval","should_reply_codex":true,"should_notify_user":false,"reply_policy":"auto_continue","codex_reply":"continue","reason":"routine execution resume"}`,
	})

	result, err := engine.ClassifySupervisorEvent(t.Context(), SupervisorContext{
		Task: TaskRun{TaskID: "task-1"},
		Request: TaskServerRequest{
			RequestID:      "req-1",
			RequestType:    ServerRequestTypeUserInput,
			RequestPayload: `{"prompt":"continue?"}`,
		},
		Summary: "continue?",
	})
	if err != nil {
		t.Fatalf("ClassifySupervisorEvent returned error: %v", err)
	}
	if result.Classification != ClassificationExecutionApproval {
		t.Fatalf("Classification = %q, want %q", result.Classification, ClassificationExecutionApproval)
	}
	if result.ReplyPolicy != ReplyPolicyAutoContinue {
		t.Fatalf("ReplyPolicy = %q, want %q", result.ReplyPolicy, ReplyPolicyAutoContinue)
	}
	if result.CodexReply != "continue" {
		t.Fatalf("CodexReply = %q, want continue", result.CodexReply)
	}
}

func TestModelDecisionEngineEvaluatesProgressUpdate(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"classification":"progress_update","should_notify_user":true,"user_update":"Codex completed the migration and passed tests.","reason":"material progress"}`,
	})

	result, err := engine.EvaluateProgressUpdate(context.Background(), TaskRun{TaskID: "task-progress"}, "Completed migration and passed tests.")
	if err != nil {
		t.Fatalf("EvaluateProgressUpdate returned error: %v", err)
	}
	if !result.ShouldNotifyUser {
		t.Fatal("ShouldNotifyUser = false, want true")
	}
}

func TestModelDecisionEngineRejectsEmptyStructuredResponse(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{response: ""})

	_, err := engine.EvaluateProgressUpdate(context.Background(), TaskRun{TaskID: "task-progress"}, "Completed migration and passed tests.")
	if err == nil {
		t.Fatal("EvaluateProgressUpdate returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), "parse decision JSON") {
		t.Fatalf("err = %v, want parse decision JSON", err)
	}
}

func TestModelDecisionEngineIncludesRawPreviewInParseError(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{response: "not json"})

	_, err := engine.EvaluateProgressUpdate(context.Background(), TaskRun{TaskID: "task-progress"}, "Completed migration and passed tests.")
	if err == nil {
		t.Fatal("EvaluateProgressUpdate returned nil error, want parse error")
	}
	if !strings.Contains(err.Error(), `raw_preview="not json"`) {
		t.Fatalf("err = %v, want raw preview", err)
	}
}

func TestModelDecisionEngineRetriesEmptyProviderOutput(t *testing.T) {
	t.Parallel()

	model := &fakeDecisionModel{
		errs: []error{
			errEmptyProviderOutput(),
			nil,
		},
		responses: []string{
			"",
			`{"classification":"progress_update","user_update":"made progress","reason":"material progress"}`,
		},
	}
	engine := NewModelDecisionEngine(model)

	result, err := engine.EvaluateProgressUpdate(context.Background(), TaskRun{TaskID: "task-progress"}, "Completed migration and passed tests.")
	if err != nil {
		t.Fatalf("EvaluateProgressUpdate returned error: %v", err)
	}
	if !result.ShouldNotifyUser {
		t.Fatal("ShouldNotifyUser = false, want true")
	}
	if model.calls != 2 {
		t.Fatalf("calls = %d, want 2", model.calls)
	}
}

func errEmptyProviderOutput() error {
	return fmt.Errorf("openai response contained empty output_text status=200 response_status=%q output_items=0 first_output=%q body=%q", "completed", "", "{}")
}

type fakeDecisionModel struct {
	response  string
	err       error
	responses []string
	errs      []error
	calls     int
}

func (f *fakeDecisionModel) Complete(context.Context, string, string) (string, error) {
	if len(f.responses) > 0 || len(f.errs) > 0 {
		idx := f.calls
		f.calls++
		var response string
		var err error
		if idx < len(f.responses) {
			response = f.responses[idx]
		} else {
			response = f.response
		}
		if idx < len(f.errs) {
			err = f.errs[idx]
		} else {
			err = f.err
		}
		return response, err
	}
	f.calls++
	return f.response, f.err
}
