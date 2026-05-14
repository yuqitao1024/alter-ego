package orchestrator

import (
	"testing"
	"time"
)

func TestEvaluateTerminalResponseDismissesCompressedPlanPrompt(t *testing.T) {
	t.Parallel()

	window := OutputWindow{
		RawOutput: `⚠ Continue according to the already confirmed plan and current workflow.
─ Create a plan?36shift + tab─use─Plan─mode───esc─dismiss`,
		Summary: "Create a plan prompt is visible",
	}

	response := EvaluateTerminalResponse(TaskRun{}, window, time.Now().UTC())
	if !response.Handled {
		t.Fatal("response.Handled = false, want true")
	}
	if response.Name != "plan_prompt_dismiss" {
		t.Fatalf("response.Name = %q, want %q", response.Name, "plan_prompt_dismiss")
	}
	if response.AutoKey != "Escape" {
		t.Fatalf("response.AutoKey = %q, want %q", response.AutoKey, "Escape")
	}
}
