package orchestrator

import "testing"

func TestPolicyForbidsReplyWithoutExplicitRequest(t *testing.T) {
	t.Parallel()

	decision := SupervisorDecision{
		Classification:  ClassificationExecutionApproval,
		ShouldReplyCodex: true,
		ReplyPolicy:     ReplyPolicyAutoContinue,
		CodexReply:      "continue",
	}

	if got := ApplySupervisorPolicy(TaskRun{}, nil, decision); got.AllowReply {
		t.Fatal("AllowReply = true, want false")
	}
}

func TestPolicyAllowsAutoReplyForPendingExecutionApproval(t *testing.T) {
	t.Parallel()

	task := TaskRun{PendingRequestID: "req-1"}
	req := &TaskServerRequest{
		RequestID:   "req-1",
		RequestType: ServerRequestTypeUserInput,
		Status:      ServerRequestStatusPending,
	}
	decision := SupervisorDecision{
		Classification:  ClassificationExecutionApproval,
		ShouldReplyCodex: true,
		ReplyPolicy:     ReplyPolicyAutoContinue,
		CodexReply:      "continue",
	}

	got := ApplySupervisorPolicy(task, req, decision)
	if !got.AllowReply {
		t.Fatal("AllowReply = false, want true")
	}
	if got.ReplyContent != "continue" {
		t.Fatalf("ReplyContent = %q, want continue", got.ReplyContent)
	}
}
