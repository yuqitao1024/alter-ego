package orchestrator

import (
	"context"
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
			TaskID:                "task-123",
			UserRequest:           "Implement task orchestration",
			CompletionCheckStatus: CompletionCheckStatusNotStarted,
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

func TestModelDecisionEngineEvaluatesCompletionSignal(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"classification":"completion_signal","completion_disposition":"confirmed_done","reason":"Codex explicitly confirmed all work is complete."}`,
	})

	result, err := engine.EvaluateCompletionSignal(context.Background(), TaskRun{
		TaskID:                "task-done",
		CompletionCheckStatus: CompletionCheckStatusSent,
	}, "All requested work is complete.")
	if err != nil {
		t.Fatalf("EvaluateCompletionSignal returned error: %v", err)
	}
	if result.CompletionDisposition != CompletionDispositionConfirmedDone {
		t.Fatalf("CompletionDisposition = %q, want %q", result.CompletionDisposition, CompletionDispositionConfirmedDone)
	}
}

type fakeDecisionModel struct {
	response string
	err      error
}

func (f *fakeDecisionModel) Complete(context.Context, string, string) (string, error) {
	return f.response, f.err
}
