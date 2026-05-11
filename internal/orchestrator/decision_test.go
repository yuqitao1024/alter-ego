package orchestrator

import (
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

func TestEscalationDetectorRecognizesImplementationSolutionChoiceOnly(t *testing.T) {
	t.Parallel()

	if !ShouldEscalateDecision("implementation_solution_choice") {
		t.Fatal("ShouldEscalateDecision returned false for implementation_solution_choice")
	}
	if ShouldEscalateDecision("continue_execution") {
		t.Fatal("ShouldEscalateDecision returned true for continue_execution")
	}
	if ShouldEscalateDecision("dependency_install") {
		t.Fatal("ShouldEscalateDecision returned true for dependency_install")
	}
}
