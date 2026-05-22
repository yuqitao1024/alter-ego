package lark

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

func TestTaskNotifierSendsApprovalCard(t *testing.T) {
	t.Parallel()

	fake := &fakeMessageCreator{}
	notifier := &TaskNotifier{sender: NewSender(fake)}

	err := notifier.NotifyTaskQuestion(context.Background(), orchestrator.TaskRun{
		TaskID:    "task-1",
		CreatedBy: "ou_user",
		AwaitingQuestion: &orchestrator.AwaitingQuestion{
			QuestionType: "execution_approval",
			QuestionText: "Codex is asking to continue execution.",
			AskedAt:      time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("NotifyTaskQuestion returned error: %v", err)
	}

	if fake.receiveIDType != "open_id" {
		t.Fatalf("receiveIDType = %q, want open_id", fake.receiveIDType)
	}
	if fake.receiveID != "ou_user" {
		t.Fatalf("receiveID = %q, want ou_user", fake.receiveID)
	}
	if fake.msgType != "interactive" {
		t.Fatalf("msgType = %q, want interactive", fake.msgType)
	}
	if !json.Valid([]byte(fake.content)) {
		t.Fatalf("content is not valid JSON: %q", fake.content)
	}
	if !strings.Contains(fake.content, "task-1") {
		t.Fatalf("content missing task ID: %s", fake.content)
	}
	for _, want := range []string{"同意", "拒绝", "提交回复", "复制命令", "/task reply task-1"} {
		if !strings.Contains(fake.content, want) {
			t.Fatalf("content missing %q: %s", want, fake.content)
		}
	}
}

func TestTaskNotifierQuestionCardDoesNotNestActionInsideForm(t *testing.T) {
	t.Parallel()

	card := buildTaskQuestionCard(orchestrator.TaskRun{
		TaskID:    "task-3",
		CreatedBy: "ou_user",
		AwaitingQuestion: &orchestrator.AwaitingQuestion{
			QuestionType: "execution_approval",
			QuestionText: "Continue?",
			AskedAt:      time.Now().UTC(),
		},
	})

	body, ok := card["body"].(map[string]interface{})
	if !ok {
		t.Fatalf("body = %#v", card["body"])
	}
	elements, ok := body["elements"].([]interface{})
	if !ok {
		t.Fatalf("elements = %#v", body["elements"])
	}

	for _, element := range elements {
		node, ok := element.(map[string]interface{})
		if !ok || node["tag"] != "form" {
			continue
		}
		formElements, ok := node["elements"].([]interface{})
		if !ok {
			t.Fatalf("form.elements = %#v", node["elements"])
		}
		for _, formElement := range formElements {
			child, ok := formElement.(map[string]interface{})
			if !ok {
				continue
			}
			if child["tag"] == "action" {
				t.Fatalf("form contains nested action element: %#v", child)
			}
		}
	}
}

func TestTaskNotifierQuestionCardFormIncludesSubmitButton(t *testing.T) {
	t.Parallel()

	card := buildTaskQuestionCard(orchestrator.TaskRun{
		TaskID:    "task-4",
		CreatedBy: "ou_user",
		AwaitingQuestion: &orchestrator.AwaitingQuestion{
			QuestionType: "execution_approval",
			QuestionText: "Continue?",
			AskedAt:      time.Now().UTC(),
		},
	})

	body, ok := card["body"].(map[string]interface{})
	if !ok {
		t.Fatalf("body = %#v", card["body"])
	}
	elements, ok := body["elements"].([]interface{})
	if !ok {
		t.Fatalf("elements = %#v", body["elements"])
	}

	foundForm := false
	foundSubmit := false
	for _, element := range elements {
		node, ok := element.(map[string]interface{})
		if !ok || node["tag"] != "form" {
			continue
		}
		foundForm = true
		formElements, ok := node["elements"].([]interface{})
		if !ok {
			t.Fatalf("form.elements = %#v", node["elements"])
		}
		for _, formElement := range formElements {
			child, ok := formElement.(map[string]interface{})
			if !ok || child["tag"] != "button" {
				continue
			}
			if child["action_type"] == "form_submit" {
				foundSubmit = true
			}
		}
	}

	if !foundForm {
		t.Fatal("card missing form element")
	}
	if !foundSubmit {
		t.Fatal("form missing submit button")
	}
}

func TestTaskNotifierUsesPlanOptionsWhenAvailable(t *testing.T) {
	t.Parallel()

	fake := &fakeMessageCreator{}
	notifier := &TaskNotifier{sender: NewSender(fake)}

	err := notifier.NotifyTaskQuestion(context.Background(), orchestrator.TaskRun{
		TaskID:    "task-2",
		CreatedBy: "ou_user",
		AwaitingQuestion: &orchestrator.AwaitingQuestion{
			QuestionType:   "plan_decision",
			QuestionText:   "Choose an approach.",
			OptionsSummary: "方案A: 保留轮询\n方案B: 改成订阅",
			AskedAt:        time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("NotifyTaskQuestion returned error: %v", err)
	}

	for _, want := range []string{"方案A: 保留轮询", "方案B: 改成订阅"} {
		if !strings.Contains(fake.content, want) {
			t.Fatalf("content missing %q: %s", want, fake.content)
		}
	}
	if strings.Contains(fake.content, "同意") {
		t.Fatalf("content should not use approval defaults when plan options exist: %s", fake.content)
	}
}
