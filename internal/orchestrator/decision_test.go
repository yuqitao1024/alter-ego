package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecisionContextIncludesWorkflowDocument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.md")
	workflowText := "Workflow:\n1. Inspect repo\n2. Propose implementation options when needed\n"
	if err := os.WriteFile(workflowPath, []byte(workflowText), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	workflow, err := LoadWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("LoadWorkflow returned error: %v", err)
	}

	prompt := BuildDecisionPrompt(DecisionContext{
		WorkflowText: workflow,
		UserRequest:  "Implement task orchestration",
	})

	if !strings.Contains(prompt, fixedDecisionRules) {
		t.Fatalf("prompt does not contain fixed decision rules:\n%s", prompt)
	}
	if !strings.Contains(prompt, workflowText) {
		t.Fatalf("prompt does not contain workflow text:\n%s", prompt)
	}
}

func TestDecisionContextIncludesRuntimeTaskFields(t *testing.T) {
	t.Parallel()

	prompt := BuildDecisionPrompt(DecisionContext{
		Task: TaskRun{
			TaskID:               "task-123",
			RepositoryID:         "repo_backend",
			MachineID:            "machine_a",
			UserRequest:          "Add /task commands",
			LastInput:            "Review current router",
			LastOutputSummary:    "Router parsed command prefix successfully",
			RemoteCodexSessionID: "session-9",
		},
		WorkflowText: "Workflow body",
		UserRequest:  "Add /task commands",
	})

	wantParts := []string{
		"task-123",
		"repo_backend",
		"machine_a",
		"Add /task commands",
		"Review current router",
		"Router parsed command prefix successfully",
		"session-9",
	}
	for _, part := range wantParts {
		if !strings.Contains(prompt, part) {
			t.Fatalf("prompt missing %q:\n%s", part, prompt)
		}
	}
}

func TestDecisionPromptForbidsMarkdownAndExtraText(t *testing.T) {
	t.Parallel()

	prompt := BuildDecisionPrompt(DecisionContext{
		Task:         TaskRun{TaskID: "task-prompt"},
		OutputWindow: OutputWindow{RawOutput: "Choose 1 or 2"},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})

	wantParts := []string{
		"Return exactly one JSON object.",
		"Do not wrap the JSON in Markdown code fences.",
		"Do not add any explanation before or after the JSON.",
		"The response must be valid JSON parsable by Go's encoding/json package.",
	}
	for _, part := range wantParts {
		if !strings.Contains(prompt, part) {
			t.Fatalf("prompt missing %q:\n%s", part, prompt)
		}
	}
}

func TestModelDecisionEngineReturnsWait(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"action":"wait","decision_type":"none","summary":"Codex is still working."}`,
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task:         TaskRun{TaskID: "task-wait", LastOutputSummary: "working"},
		OutputWindow: OutputWindow{RawOutput: "Working", Summary: "working"},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionWait {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionWait)
	}
}

func TestModelDecisionEngineReturnsAskUser(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"action":"ask_user","decision_type":"implementation_solution_choice","summary":"Need user choice","user_question":"Choose option 1 or 2."}`,
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{TaskID: "task-1", LastOutputSummary: "Need a choice"},
		OutputWindow: OutputWindow{
			RawOutput: "Choose 1 or 2",
			Summary:   "Need a choice",
		},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionAskUser {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionAskUser)
	}
	if result.Question == nil || result.Question.QuestionText != "Choose option 1 or 2." {
		t.Fatalf("Question = %#v", result.Question)
	}
}

func TestModelDecisionEngineReturnsReplyToCodex(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"action":"reply_to_codex","decision_type":"none","summary":"Continue with issue 30","codex_reply":"切回 issue #30 继续开发。"}`,
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{TaskID: "task-2", LastOutputSummary: "Need a choice"},
		OutputWindow: OutputWindow{
			RawOutput: "Choose 1 or 2",
			Summary:   "Need a choice",
		},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionReplyToCodex {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionReplyToCodex)
	}
	if result.CodexReply != "切回 issue #30 继续开发。" {
		t.Fatalf("CodexReply = %q", result.CodexReply)
	}
}

func TestModelDecisionEngineReturnsCompleteTask(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"action":"complete_task","decision_type":"none","summary":"Task completed successfully."}`,
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{TaskID: "task-3", LastOutputSummary: "Done"},
		OutputWindow: OutputWindow{
			RawOutput: "Implementation finished. Waiting for next instruction.",
			Summary:   "Done",
		},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionCompleteTask {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionCompleteTask)
	}
}

func TestModelDecisionEngineParsesJSONCodeFence(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: "```json\n{\"action\":\"ask_user\",\"decision_type\":\"implementation_solution_choice\",\"summary\":\"Need user choice\",\"user_question\":\"Choose option 1 or 2.\"}\n```",
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task:         TaskRun{TaskID: "task-fence", LastOutputSummary: "Need a choice"},
		OutputWindow: OutputWindow{RawOutput: "Choose 1 or 2", Summary: "Need a choice"},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionAskUser {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionAskUser)
	}
}

func TestModelDecisionEngineAcceptsNumericDecisionType(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"action":"reply_to_codex","decision_type":2,"summary":"Continue with step 1","codex_reply":"Read the headers first."}`,
	})

	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task:         TaskRun{TaskID: "task-numeric-decision-type", LastOutputSummary: "Need next step"},
		OutputWindow: OutputWindow{RawOutput: "Need next step", Summary: "Need next step"},
		WorkflowText: "workflow",
		UserRequest:  "request",
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.Action != DecisionActionReplyToCodex {
		t.Fatalf("Action = %q, want %q", result.Action, DecisionActionReplyToCodex)
	}
	if result.DecisionType != "2" {
		t.Fatalf("DecisionType = %q, want %q", result.DecisionType, "2")
	}
}

type fakeDecisionModel struct {
	response string
	err      error
}

func (f *fakeDecisionModel) Complete(context.Context, string, string) (string, error) {
	return f.response, f.err
}
