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
	for _, want := range []string{"提交回复", "任务完成", "reply_text", "请输入你的回复"} {
		if !strings.Contains(fake.content, want) {
			t.Fatalf("content missing %q: %s", want, fake.content)
		}
	}
	for _, unwanted := range []string{"同意", "拒绝", "/task reply task-1", "select_static", "reply_choice"} {
		if strings.Contains(fake.content, unwanted) {
			t.Fatalf("content should not contain %q: %s", unwanted, fake.content)
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
	foundInput := false
	foundSubmit := false
	foundComplete := false
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
			if !ok {
				continue
			}
			if child["tag"] == "input" && child["name"] == "reply_text" {
				foundInput = true
			}
			if child["tag"] == "button" && child["action_type"] == "form_submit" {
				value, _ := child["value"].(map[string]interface{})
				switch value["action"] {
				case "submit":
					foundSubmit = true
				case "complete":
					foundComplete = true
				}
			}
		}
	}

	if !foundForm {
		t.Fatal("card missing form element")
	}
	if !foundInput {
		t.Fatal("form missing reply input")
	}
	if !foundSubmit {
		t.Fatal("form missing submit button")
	}
	if !foundComplete {
		t.Fatal("form missing complete button")
	}
}

func TestTaskNotifierQuestionCardDoesNotUseActionTag(t *testing.T) {
	t.Parallel()

	card := buildTaskQuestionCard(orchestrator.TaskRun{
		TaskID:    "task-5",
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
		if !ok {
			continue
		}
		if node["tag"] == "action" {
			t.Fatalf("card contains unsupported action tag: %#v", node)
		}
	}
}
