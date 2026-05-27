package lark

import (
	"context"
	"fmt"
	"strings"

	larkapi "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type TaskNotifier struct {
	sender *Sender
}

func NewTaskNotifier(cfg Config) *TaskNotifier {
	apiClient := larkapi.NewClient(cfg.AppID, cfg.AppSecret, larkapi.WithOpenBaseUrl(baseURL(cfg.Domain)))
	return &TaskNotifier{
		sender: NewSender(NewSDKMessageCreator(apiClient.Im)),
	}
}

func (n *TaskNotifier) NotifyTaskQuestion(ctx context.Context, task orchestrator.TaskRun) error {
	if n == nil || n.sender == nil || task.AwaitingQuestion == nil {
		return nil
	}

	return n.sender.SendDirectCard(ctx, task.CreatedBy, buildTaskQuestionCard(task))
}

func (n *TaskNotifier) NotifyTaskProgress(ctx context.Context, task orchestrator.TaskRun, message string) error {
	if n == nil || n.sender == nil || message == "" {
		return nil
	}

	text := fmt.Sprintf("Task %s progress update:\n\n%s", task.TaskID, message)
	return n.sender.SendDirectMessage(ctx, task.CreatedBy, text)
}

func buildTaskQuestionCard(task orchestrator.TaskRun) map[string]interface{} {
	question := task.AwaitingQuestion

	elements := []interface{}{
		map[string]interface{}{
			"tag":     "markdown",
			"content": fmt.Sprintf("**Task**: `%s`\n\n%s", task.TaskID, strings.TrimSpace(question.QuestionText)),
		},
	}

	if context := strings.TrimSpace(question.ContextExcerpt); context != "" {
		elements = append(elements, map[string]interface{}{
			"tag":     "markdown",
			"content": fmt.Sprintf("**Context**\n%s", context),
		})
	}

	elements = append(elements, map[string]interface{}{
		"tag":  "form",
		"name": "task_reply_form",
		"elements": []interface{}{
			map[string]interface{}{
				"tag":        "input",
				"name":       "reply_text",
				"input_type": "multiline_text",
				"rows":       4,
				"max_length": 1000,
				"placeholder": map[string]interface{}{
					"tag":     "plain_text",
					"content": "请输入你的回复",
				},
			},
			map[string]interface{}{
				"tag":         "button",
				"name":        "task_reply_submit",
				"type":        "primary",
				"action_type": "form_submit",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": "提交回复",
				},
				"value": map[string]interface{}{
					"source":  "alterego",
					"version": 1,
					"kind":    "task_reply_action",
					"action":  "submit",
					"task_id": task.TaskID,
				},
			},
			map[string]interface{}{
				"tag":         "button",
				"name":        "task_reply_complete",
				"type":        "default",
				"action_type": "form_submit",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": "任务完成",
				},
				"value": map[string]interface{}{
					"source":  "alterego",
					"version": 1,
					"kind":    "task_reply_action",
					"action":  "complete",
					"task_id": task.TaskID,
				},
			},
		},
	})

	return map[string]interface{}{
		"schema": "2.0",
		"config": map[string]interface{}{
			"update_multi": true,
		},
		"header": map[string]interface{}{
			"template": "yellow",
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": fmt.Sprintf("Task %s 需要回复", task.TaskID),
			},
		},
		"body": map[string]interface{}{
			"elements": elements,
		},
	}
}
