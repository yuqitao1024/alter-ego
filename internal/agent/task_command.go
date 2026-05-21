package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type TaskService interface {
	StartTask(ctx context.Context, templateID, createdBy, userRequest string) (orchestrator.TaskRun, error)
	List(ctx context.Context) ([]orchestrator.TaskRun, error)
	ListAll(ctx context.Context) ([]orchestrator.TaskRun, error)
	Status(ctx context.Context, taskID string) (orchestrator.TaskRun, error)
	Reply(ctx context.Context, taskID, text string) error
	Stop(ctx context.Context, taskID string) error
	Delete(ctx context.Context, taskID string) error
	DeleteTerminalTasks(ctx context.Context) (int, error)
}

type TaskCommandHandler struct {
	service TaskService
}

func NewTaskCommandHandler(service TaskService) *TaskCommandHandler {
	return &TaskCommandHandler{service: service}
}

func (h *TaskCommandHandler) HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	reply := channel.OutgoingMessage{Conversation: event.Conversation}
	if h.service == nil {
		reply.Text = "Task service is not configured."
		return reply, nil
	}

	fields := strings.Fields(strings.TrimSpace(event.Text))
	if len(fields) < 2 || fields[0] != "/task" {
		reply.Text = "Usage: /task <start|list|status|reply|stop> ..."
		return reply, nil
	}

	switch fields[1] {
	case "start":
		if len(fields) < 4 {
			reply.Text = "Usage: /task start <template> <requirement text>"
			return reply, nil
		}
		templateID := fields[2]
		requestText := strings.TrimSpace(strings.Join(fields[3:], " "))
		task, err := h.service.StartTask(ctx, templateID, event.Sender.ID, requestText)
		if err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Started task %s\ntemplate: %s\nrepository: %s\nmachine: %s\nstatus: %s", task.TaskID, task.TemplateID, task.RepositoryID, task.MachineID, task.Status)
	case "list":
		showAll := len(fields) == 3 && fields[2] == "-a"
		if len(fields) > 3 || (len(fields) == 3 && !showAll) {
			reply.Text = "Usage: /task list [-a]"
			return reply, nil
		}
		var tasks []orchestrator.TaskRun
		var err error
		if showAll {
			tasks, err = h.service.ListAll(ctx)
		} else {
			tasks, err = h.service.List(ctx)
		}
		if err != nil {
			return reply, err
		}
		if len(tasks) == 0 {
			reply.Text = "No active tasks."
			return reply, nil
		}

		var lines []string
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf("%s | template=%s | machine=%s | status=%s", task.TaskID, task.TemplateID, task.MachineID, task.Status))
		}
		reply.Text = strings.Join(lines, "\n")
	case "status":
		if len(fields) != 3 {
			reply.Text = "Usage: /task status <task-id>"
			return reply, nil
		}
		task, err := h.service.Status(ctx, fields[2])
		if err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf(
			"task: %s\ntemplate: %s\nrepository: %s\nmachine: %s\nstatus: %s\nthread: %s\nsummary: %s",
			task.TaskID,
			task.TemplateID,
			task.RepositoryID,
			task.MachineID,
			task.Status,
			task.ThreadID,
			task.LastOutputSummary,
		)
	case "reply":
		if len(fields) < 4 {
			reply.Text = "Usage: /task reply <task-id> <decision text>"
			return reply, nil
		}
		taskID := fields[2]
		decisionText := strings.TrimSpace(strings.Join(fields[3:], " "))
		if err := h.service.Reply(ctx, taskID, decisionText); err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Task %s resumed.", taskID)
	case "stop":
		if len(fields) != 3 {
			reply.Text = "Usage: /task stop <task-id>"
			return reply, nil
		}
		taskID := fields[2]
		if err := h.service.Stop(ctx, taskID); err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Task %s stopped.", taskID)
	case "delete":
		if len(fields) != 3 {
			reply.Text = "Usage: /task delete <task-id|-a>"
			return reply, nil
		}
		if fields[2] == "-a" {
			count, err := h.service.DeleteTerminalTasks(ctx)
			if err != nil {
				return reply, err
			}
			reply.Text = fmt.Sprintf("Deleted %d terminal task(s).", count)
			return reply, nil
		}
		taskID := fields[2]
		if err := h.service.Delete(ctx, taskID); err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Task %s deleted.", taskID)
	default:
		reply.Text = "Usage: /task <start|list|status|reply|stop|delete> ..."
	}

	return reply, nil
}
