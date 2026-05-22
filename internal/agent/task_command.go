package agent

import (
	"context"
	"fmt"
	"strings"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"

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

		reply.Card = &channel.CardMessage{Payload: buildTaskListCard(tasks, showAll)}
	case "status":
		if len(fields) != 3 {
			reply.Text = "Usage: /task status <task-id>"
			return reply, nil
		}
		task, err := h.service.Status(ctx, fields[2])
		if err != nil {
			return reply, err
		}
		reply.Text = formatTaskStatus(task)
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

func (h *TaskCommandHandler) HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error) {
	request, err := parseTaskCardAction(event.Value)
	if err != nil {
		return channel.CardActionResponse{ToastText: err.Error()}, nil
	}

	switch request.kind {
	case "task_action":
		return h.handleTaskActionCard(ctx, event, request.action, request.taskID)
	case "task_reply_action":
		return h.handleTaskReplyCard(ctx, event, request)
	default:
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Unsupported task action: %s", request.action)}, nil
	}
}

func (h *TaskCommandHandler) handleTaskActionCard(ctx context.Context, event channel.CardActionEvent, action, taskID string) (channel.CardActionResponse, error) {
	switch action {
	case "status":
		task, err := h.service.Status(ctx, taskID)
		if err != nil {
			return channel.CardActionResponse{}, err
		}
		message := channel.OutgoingMessage{
			Conversation: event.Conversation,
			Text:         formatTaskStatus(task),
		}
		return channel.CardActionResponse{
			ToastText: "Task status sent.",
			Message:   &message,
		}, nil
	case "stop":
		if err := h.service.Stop(ctx, taskID); err != nil {
			return channel.CardActionResponse{}, err
		}
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Task %s stopped.", taskID)}, nil
	case "delete":
		if err := h.service.Delete(ctx, taskID); err != nil {
			return channel.CardActionResponse{}, err
		}
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Task %s deleted.", taskID)}, nil
	default:
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Unsupported task action: %s", action)}, nil
	}
}

func (h *TaskCommandHandler) handleTaskReplyCard(ctx context.Context, event channel.CardActionEvent, request taskCardAction) (channel.CardActionResponse, error) {
	switch request.action {
	case "copy_command":
		command := request.command
		if command == "" {
			command = fmt.Sprintf("/task reply %s <content>", request.taskID)
		}
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Reply with: %s", command)}, nil
	case "submit":
		replyText := firstNonEmpty(
			formStringValue(event.FormValue, "reply_choice"),
			strings.TrimSpace(event.Option),
			firstOption(event.Options),
			strings.TrimSpace(event.InputValue),
			stringValue(event.Value, "reply_text"),
		)
		if replyText == "" {
			return channel.CardActionResponse{ToastText: "Select a reply first."}, nil
		}
		if err := h.service.Reply(ctx, request.taskID, replyText); err != nil {
			return channel.CardActionResponse{}, err
		}
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Task %s resumed.", request.taskID)}, nil
	default:
		return channel.CardActionResponse{ToastText: fmt.Sprintf("Unsupported task reply action: %s", request.action)}, nil
	}
}

type taskCardAction struct {
	kind    string
	action  string
	taskID  string
	command string
}

func parseTaskCardAction(value map[string]interface{}) (taskCardAction, error) {
	if stringValue(value, "source") != "alterego" {
		return taskCardAction{}, fmt.Errorf("Invalid task action.")
	}
	kind := stringValue(value, "kind")
	if kind != "task_action" && kind != "task_reply_action" {
		return taskCardAction{}, fmt.Errorf("Invalid task action.")
	}
	if intValue(value, "version") != 1 {
		return taskCardAction{}, fmt.Errorf("Unsupported task action version.")
	}

	action := stringValue(value, "action")
	taskID := stringValue(value, "task_id")
	if taskID == "" {
		return taskCardAction{}, fmt.Errorf("Task ID is required.")
	}

	switch kind {
	case "task_action":
		switch action {
		case "status", "stop", "delete":
			return taskCardAction{kind: kind, action: action, taskID: taskID}, nil
		}
		return taskCardAction{}, fmt.Errorf("Unsupported task action: %s", action)
	case "task_reply_action":
		switch action {
		case "submit", "copy_command":
			return taskCardAction{
				kind:    kind,
				action:  action,
				taskID:  taskID,
				command: stringValue(value, "command"),
			}, nil
		}
		return taskCardAction{}, fmt.Errorf("Unsupported task reply action: %s", action)
	default:
		return taskCardAction{}, fmt.Errorf("Invalid task action.")
	}
}

func buildTaskListCard(tasks []orchestrator.TaskRun, showAll bool) interface{} {
	title := "Active Tasks"
	if showAll {
		title = "All Tasks"
	}

	elements := []larkcard.MessageCardElement{
		larkcard.NewMessageCardMarkdown().
			Content(fmt.Sprintf("Total: **%d**", len(tasks))).
			Build(),
	}
	for _, task := range tasks {
		elements = append(elements,
			larkcard.NewMessageCardHr().Build(),
			larkcard.NewMessageCardMarkdown().
				Content(fmt.Sprintf("**%s**\nTemplate: `%s`\nMachine: `%s`\nStatus: `%s`", task.TaskID, task.TemplateID, task.MachineID, task.Status)).
				Build(),
			taskActionRow(task).Build(),
		)
	}

	return larkcard.NewMessageCard().
		Config(larkcard.NewMessageCardConfig().WideScreenMode(true).Build()).
		Header(larkcard.NewMessageCardHeader().
			Template(larkcard.TemplateBlue).
			Title(larkcard.NewMessageCardPlainText().Content(title).Build()).
			Build()).
		Elements(elements).
		Build()
}

func taskActionRow(task orchestrator.TaskRun) *larkcard.MessageCardAction {
	actions := []larkcard.MessageCardActionElement{
		taskButton("Status", "status", task.TaskID, larkcard.MessageCardButtonTypeDefault, nil),
	}
	if task.Status == orchestrator.StatusRunning || task.Status == orchestrator.StatusWaitingUserInput {
		actions = append(actions, taskButton("Stop", "stop", task.TaskID, larkcard.MessageCardButtonTypeDanger, nil))
	}
	if task.Status == orchestrator.StatusCompleted || task.Status == orchestrator.StatusFailed || task.Status == orchestrator.StatusStopped {
		confirm := larkcard.NewMessageCardActionConfirm().
			Title(larkcard.NewMessageCardPlainText().Content("Delete task?").Build()).
			Text(larkcard.NewMessageCardPlainText().Content("This will delete the task record and workspace directory.").Build()).
			Build()
		actions = append(actions, taskButton("Delete", "delete", task.TaskID, larkcard.MessageCardButtonTypeDanger, confirm))
	}
	layout := larkcard.MessageCardActionLayoutFlow
	return larkcard.NewMessageCardAction().
		Actions(actions).
		Layout(layout.Ptr()).
		Build()
}

func taskButton(label, action, taskID string, buttonType larkcard.MessageCardButtonType, confirm *larkcard.MessageCardActionConfirm) *larkcard.MessageCardEmbedButton {
	button := larkcard.NewMessageCardEmbedButton().
		Text(larkcard.NewMessageCardPlainText().Content(label).Build()).
		Type(buttonType).
		Value(map[string]interface{}{
			"source":  "alterego",
			"version": 1,
			"kind":    "task_action",
			"action":  action,
			"task_id": taskID,
		}).
		Build()
	if confirm != nil {
		button.Confirm(confirm)
	}
	return button
}

func formatTaskStatus(task orchestrator.TaskRun) string {
	return fmt.Sprintf(
		"task: %s\ntemplate: %s\nrepository: %s\nmachine: %s\nstatus: %s\nthread: %s\nsummary: %s",
		task.TaskID,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.ThreadID,
		task.LastOutputSummary,
	)
}

func stringValue(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intValue(values map[string]interface{}, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func formStringValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]interface{}:
		if text, ok := value["value"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func firstOption(options []string) string {
	if len(options) == 0 {
		return ""
	}
	return strings.TrimSpace(options[0])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
