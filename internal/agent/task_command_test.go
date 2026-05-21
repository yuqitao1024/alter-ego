package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/channel"
	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

func TestTaskCommandStartCreatesTask(t *testing.T) {
	t.Parallel()

	service := &fakeTaskService{
		startTask: orchestrator.TaskRun{
			TaskID:       "task-1",
			TemplateID:   "feature_dev",
			MachineID:    "machine_a",
			Status:       orchestrator.StatusPending,
			UserRequest:  "Implement feature",
			CreatedBy:    "ou_1",
			RepositoryID: "repo_backend",
		},
	}
	handler := NewTaskCommandHandler(service)

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{
		Text:     "/task start feature_dev Implement feature",
		Platform: "lark",
		Sender:   channel.Sender{ID: "ou_1"},
	})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if service.startedTemplate != "feature_dev" || service.startedRequest != "Implement feature" {
		t.Fatalf("start args = %q %q", service.startedTemplate, service.startedRequest)
	}
	if !strings.Contains(reply.Text, "Started task task-1") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestTaskCommandListFormatsActiveTasks(t *testing.T) {
	t.Parallel()

	handler := NewTaskCommandHandler(&fakeTaskService{
		tasks: []orchestrator.TaskRun{
			{TaskID: "task-1", TemplateID: "feature_dev", MachineID: "machine_a", Status: orchestrator.StatusRunning},
			{TaskID: "task-2", TemplateID: "bugfix", MachineID: "machine_b", Status: orchestrator.StatusWaitingUserInput},
		},
	})

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task list"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if !strings.Contains(reply.Text, "task-1") || !strings.Contains(reply.Text, "task-2") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestTaskCommandListAllFormatsAllTasks(t *testing.T) {
	t.Parallel()

	handler := NewTaskCommandHandler(&fakeTaskService{
		allTasks: []orchestrator.TaskRun{
			{TaskID: "task-1", TemplateID: "feature_dev", MachineID: "machine_a", Status: orchestrator.StatusRunning},
			{TaskID: "task-2", TemplateID: "bugfix", MachineID: "machine_b", Status: orchestrator.StatusCompleted},
		},
	})

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task list -a"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if !strings.Contains(reply.Text, "task-1") || !strings.Contains(reply.Text, "task-2") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestTaskCommandStatusFormatsTaskDetails(t *testing.T) {
	t.Parallel()

	handler := NewTaskCommandHandler(&fakeTaskService{
		statusTask: orchestrator.TaskRun{
			TaskID:            "task-1",
			TemplateID:        "feature_dev",
			RepositoryID:      "repo_backend",
			MachineID:         "machine_a",
			Status:            orchestrator.StatusRunning,
			LastOutputSummary: "Tests passed",
			ThreadID: "thread-1",
		},
	})

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task status task-1"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	wantParts := []string{"task-1", "feature_dev", "repo_backend", "machine_a", "thread-1", "Tests passed"}
	for _, part := range wantParts {
		if !strings.Contains(reply.Text, part) {
			t.Fatalf("reply.Text missing %q: %q", part, reply.Text)
		}
	}
}

func TestTaskCommandReplyResumesWaitingTask(t *testing.T) {
	t.Parallel()

	service := &fakeTaskService{}
	handler := NewTaskCommandHandler(service)

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task reply task-1 Use polling"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if service.replyTaskID != "task-1" || service.replyText != "Use polling" {
		t.Fatalf("reply args = %q %q", service.replyTaskID, service.replyText)
	}
	if !strings.Contains(reply.Text, "resumed") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestTaskCommandStopStopsTask(t *testing.T) {
	t.Parallel()

	service := &fakeTaskService{}
	handler := NewTaskCommandHandler(service)

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task stop task-1"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if service.stopTaskID != "task-1" {
		t.Fatalf("stopTaskID = %q", service.stopTaskID)
	}
	if !strings.Contains(reply.Text, "stopped") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestTaskCommandDeleteDeletesTask(t *testing.T) {
	t.Parallel()

	service := &fakeTaskService{}
	handler := NewTaskCommandHandler(service)

	reply, err := handler.HandleCommand(context.Background(), channel.MessageEvent{Text: "/task delete task-1"})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}

	if service.deleteTaskID != "task-1" {
		t.Fatalf("deleteTaskID = %q", service.deleteTaskID)
	}
	if !strings.Contains(reply.Text, "deleted") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

type fakeTaskService struct {
	startTask       orchestrator.TaskRun
	statusTask      orchestrator.TaskRun
	tasks           []orchestrator.TaskRun
	allTasks        []orchestrator.TaskRun
	startedTemplate string
	startedRequest  string
	startedBy       string
	replyTaskID     string
	replyText       string
	stopTaskID      string
	deleteTaskID    string
}

func (f *fakeTaskService) StartTask(ctx context.Context, templateID, createdBy, userRequest string) (orchestrator.TaskRun, error) {
	f.startedTemplate = templateID
	f.startedBy = createdBy
	f.startedRequest = userRequest
	if f.startTask.CreatedAt.IsZero() {
		f.startTask.CreatedAt = time.Now().UTC()
	}
	if f.startTask.UpdatedAt.IsZero() {
		f.startTask.UpdatedAt = f.startTask.CreatedAt
	}
	return f.startTask, nil
}

func (f *fakeTaskService) List(ctx context.Context) ([]orchestrator.TaskRun, error) {
	return f.tasks, nil
}

func (f *fakeTaskService) ListAll(ctx context.Context) ([]orchestrator.TaskRun, error) {
	return f.allTasks, nil
}

func (f *fakeTaskService) Status(ctx context.Context, taskID string) (orchestrator.TaskRun, error) {
	return f.statusTask, nil
}

func (f *fakeTaskService) Reply(ctx context.Context, taskID, text string) error {
	f.replyTaskID = taskID
	f.replyText = text
	return nil
}

func (f *fakeTaskService) Stop(ctx context.Context, taskID string) error {
	f.stopTaskID = taskID
	return nil
}

func (f *fakeTaskService) Delete(ctx context.Context, taskID string) error {
	f.deleteTaskID = taskID
	return nil
}
