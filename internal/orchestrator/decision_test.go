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

func TestDecisionDetectorRecognizesRequirementClarification(t *testing.T) {
	t.Parallel()

	engine := NewHeuristicDecisionEngine()
	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{
			LastOutputSummary: "I need clarification on the requirement before I proceed.",
		},
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.DecisionType != "requirement_clarification" {
		t.Fatalf("DecisionType = %q, want requirement_clarification", result.DecisionType)
	}
	if result.Question == nil || result.Question.QuestionType != "requirement_clarification" {
		t.Fatalf("Question = %#v, want requirement_clarification", result.Question)
	}
}

func TestDecisionDetectorRecognizesScopeConfirmation(t *testing.T) {
	t.Parallel()

	engine := NewHeuristicDecisionEngine()
	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{
			LastOutputSummary: "Please confirm the scope before I make the change.",
		},
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.DecisionType != "scope_confirmation" {
		t.Fatalf("DecisionType = %q, want scope_confirmation", result.DecisionType)
	}
}

func TestDecisionDetectorRecognizesImplementationSolutionChoice(t *testing.T) {
	t.Parallel()

	engine := NewHeuristicDecisionEngine()
	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{
			LastOutputSummary: "Which approach should I take for the implementation?",
		},
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.DecisionType != "implementation_solution_choice" {
		t.Fatalf("DecisionType = %q, want implementation_solution_choice", result.DecisionType)
	}
}

func TestDecisionDetectorRecognizesMissingContext(t *testing.T) {
	t.Parallel()

	engine := NewHeuristicDecisionEngine()
	result, err := engine.DecideNextStep(t.Context(), DecisionContext{
		Task: TaskRun{
			LastOutputSummary: "I am missing context about the expected API behavior.",
		},
	})
	if err != nil {
		t.Fatalf("DecideNextStep returned error: %v", err)
	}
	if result.DecisionType != "missing_context" {
		t.Fatalf("DecisionType = %q, want missing_context", result.DecisionType)
	}
}

func TestEscalationDetectorRecognizesSupportedUserInputCategories(t *testing.T) {
	t.Parallel()

	for _, decisionType := range []string{
		"requirement_clarification",
		"scope_confirmation",
		"implementation_solution_choice",
		"missing_context",
	} {
		if !ShouldEscalateDecision(decisionType) {
			t.Fatalf("ShouldEscalateDecision returned false for %q", decisionType)
		}
	}
	if ShouldEscalateDecision("continue_execution") {
		t.Fatal("ShouldEscalateDecision returned true for continue_execution")
	}
}
