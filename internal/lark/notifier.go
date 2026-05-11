package lark

import (
	"context"
	"fmt"

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

	text := fmt.Sprintf(
		"Task %s needs your decision.\n\n%s\n\nReply with:\n/task reply %s <decision>",
		task.TaskID,
		task.AwaitingQuestion.QuestionText,
		task.TaskID,
	)
	return n.sender.SendDirectMessage(ctx, task.CreatedBy, text)
}
