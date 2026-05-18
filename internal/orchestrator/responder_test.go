package orchestrator

import (
	"testing"
	"time"
)

func TestEvaluateTerminalResponseIgnoresCompressedPlanPrompt(t *testing.T) {
	t.Parallel()

	window := OutputWindow{
		RawOutput: `⚠ Continue according to the already confirmed plan and current workflow.
─ Create a plan?36shift + tab─use─Plan─mode───esc─dismiss`,
		Summary: "Create a plan prompt is visible",
	}

	response := EvaluateTerminalResponse(TaskRun{}, window, time.Now().UTC())
	if response.Handled {
		t.Fatal("response.Handled = true, want false")
	}
}
