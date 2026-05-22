package lark

import (
	"context"
	"fmt"
	"regexp"
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

type taskReplyOption struct {
	Label string
	Value string
}

func buildTaskQuestionCard(task orchestrator.TaskRun) map[string]interface{} {
	question := task.AwaitingQuestion
	command := fmt.Sprintf("/task reply %s <content>", task.TaskID)
	options := questionReplyOptions(question)

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

	if len(options) > 0 {
		optionItems := make([]interface{}, 0, len(options))
		for _, option := range options {
			optionItems = append(optionItems, map[string]interface{}{
				"text": map[string]interface{}{
					"tag":     "plain_text",
					"content": option.Label,
				},
				"value": option.Value,
			})
		}

		elements = append(elements, map[string]interface{}{
			"tag":  "form",
			"name": "task_reply_form",
			"elements": []interface{}{
				map[string]interface{}{
					"tag":  "select_static",
					"name": "reply_choice",
					"placeholder": map[string]interface{}{
						"tag":     "plain_text",
						"content": "请选择回复",
					},
					"options": optionItems,
				},
			},
		})
		elements = append(elements, map[string]interface{}{
			"tag": "action",
			"actions": []interface{}{
				map[string]interface{}{
					"tag":  "button",
					"type": "primary",
					"text": map[string]interface{}{
						"tag":     "plain_text",
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
					"tag": "button",
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": "复制命令",
					},
					"value": map[string]interface{}{
						"source":  "alterego",
						"version": 1,
						"kind":    "task_reply_action",
						"action":  "copy_command",
						"task_id": task.TaskID,
						"command": command,
					},
				},
			},
		})
	} else {
		elements = append(elements, map[string]interface{}{
			"tag": "action",
			"actions": []interface{}{
				map[string]interface{}{
					"tag": "button",
					"text": map[string]interface{}{
						"tag":     "plain_text",
						"content": "复制命令",
					},
					"value": map[string]interface{}{
						"source":  "alterego",
						"version": 1,
						"kind":    "task_reply_action",
						"action":  "copy_command",
						"task_id": task.TaskID,
						"command": command,
					},
				},
			},
		})
	}

	elements = append(elements, map[string]interface{}{
		"tag":     "markdown",
		"content": fmt.Sprintf("使用命令回复自定义内容：`%s`", command),
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

func questionReplyOptions(question *orchestrator.AwaitingQuestion) []taskReplyOption {
	if question == nil {
		return nil
	}

	switch strings.TrimSpace(question.QuestionType) {
	case "plan_decision":
		options := parsePlanReplyOptions(question.OptionsSummary)
		if len(options) > 0 {
			return options
		}
		return parsePlanReplyOptions(question.QuestionText)
	default:
		return []taskReplyOption{
			{Label: "同意", Value: "continue"},
			{Label: "拒绝", Value: "reject"},
		}
	}
}

var optionPrefixPattern = regexp.MustCompile(`^(?:[-*]\s+|\d+[.)]\s+|[A-Za-z][.:：)]\s*)`)

func parsePlanReplyOptions(raw string) []taskReplyOption {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	fields := strings.Split(raw, "\n")
	if len(fields) == 1 && strings.Contains(raw, ";") {
		fields = strings.Split(raw, ";")
	}

	options := make([]taskReplyOption, 0, len(fields))
	for _, field := range fields {
		text := strings.TrimSpace(field)
		text = optionPrefixPattern.ReplaceAllString(text, "")
		if text == "" {
			continue
		}
		options = append(options, taskReplyOption{
			Label: text,
			Value: text,
		})
	}
	return options
}
