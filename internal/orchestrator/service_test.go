package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreateTaskSelectsMachineAndPersistsPendingTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	seedTask(t, store, TaskRun{
		TaskID:       "existing",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusRunning,
		UserRequest:  "existing work",
		CreatedBy:    "tester",
		CreatedAt:    time.Now().UTC().Add(-time.Minute),
		UpdatedAt:    time.Now().UTC().Add(-time.Minute),
	})

	task, err := service.StartTask(ctx, "feature_dev", "yuqitao", "Add remote control")
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}

	if task.Status != StatusPending {
		t.Fatalf("task.Status = %q, want %q", task.Status, StatusPending)
	}
	if task.MachineID != "machine_b" {
		t.Fatalf("task.MachineID = %q, want machine_b", task.MachineID)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.TemplateID != "feature_dev" || persisted.RepositoryID != "repo_backend" {
		t.Fatalf("persisted template/repository = %q/%q", persisted.TemplateID, persisted.RepositoryID)
	}
}

func TestTickStartsPendingTaskAndStoresRemoteSession(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:       "task-start",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusPending,
		UserRequest:  "Implement remote start",
		CreatedBy:    "tester",
	})
	service.runner.(*fakeServiceRunner).startSession = RemoteSession{
		MachineID:       "machine_a",
		Workdir:         "/srv/codex-tasks/task-start/repo",
		CodexSessionID:  "session-start",
		ProcessIdentity: "pid-100",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.RemoteCodexSessionID != "session-start" {
		t.Fatalf("persisted.RemoteCodexSessionID = %q, want session-start", persisted.RemoteCodexSessionID)
	}
	if persisted.RemoteWorkdir != "/srv/codex-tasks/task-start/repo" {
		t.Fatalf("persisted.RemoteWorkdir = %q, want /srv/codex-tasks/task-start/repo", persisted.RemoteWorkdir)
	}
	if persisted.RemoteProcessIdentity != "pid-100" {
		t.Fatalf("persisted.RemoteProcessIdentity = %q, want pid-100", persisted.RemoteProcessIdentity)
	}
}

func TestTickMovesTaskToWaitingUserDecisionWhenDecisionRequiresUser(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:                "task-wait",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusRunning,
		UserRequest:           "Implement orchestrator",
		CreatedBy:             "tester",
		RemoteWorkdir:         "/srv/backend",
		RemoteCodexSessionID:  "session-wait",
		RemoteProcessIdentity: "pid-200",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{Summary: "Two implementation approaches are possible"}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		DecisionType: "implementation_solution_choice",
		Summary:      "Need user to choose approach",
		Question: &AwaitingQuestion{
			QuestionText:   "Choose event-driven or polling design?",
			OptionsSummary: "event-driven | polling",
			ContextExcerpt: "Both approaches are viable",
		},
	}
	notifier := service.notifier.(*fakeTaskNotifier)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserDecision {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserDecision)
	}
	if persisted.AwaitingQuestion == nil || !strings.Contains(persisted.AwaitingQuestion.QuestionText, "Choose") {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want question", persisted.AwaitingQuestion)
	}
	if notifier.lastTaskID != task.TaskID || notifier.lastUserID != "tester" {
		t.Fatalf("notifier captured task=%q user=%q", notifier.lastTaskID, notifier.lastUserID)
	}
}

func TestReplyResumesWaitingTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:                "task-reply",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusWaitingUserDecision,
		UserRequest:           "Implement orchestrator",
		CreatedBy:             "tester",
		RemoteWorkdir:         "/srv/backend",
		RemoteCodexSessionID:  "session-reply",
		RemoteProcessIdentity: "pid-300",
		AwaitingQuestion: &AwaitingQuestion{
			QuestionText: "Which approach?",
			AskedAt:      time.Now().UTC().Add(-time.Minute),
		},
	})

	if err := service.Reply(ctx, task.TaskID, "Use polling."); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}
	if persisted.LastInput != "Use polling." {
		t.Fatalf("persisted.LastInput = %q, want %q", persisted.LastInput, "Use polling.")
	}
}

func TestRecoverDetachedTaskAttachesLiveSessionBeforeResume(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:                "task-detached",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusDetached,
		UserRequest:           "Resume detached task",
		CreatedBy:             "tester",
		RemoteWorkdir:         "/srv/backend",
		RemoteCodexSessionID:  "session-detached",
		RemoteProcessIdentity: "pid-old",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.probeResult = ProbeResult{Alive: true, ProcessIdentity: "pid-live"}
	runner.attachSession = RemoteSession{
		MachineID:       "machine_a",
		Workdir:         "/srv/backend",
		CodexSessionID:  "session-detached",
		ProcessIdentity: "pid-live",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}

	wantCalls := []string{"probe", "attach"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestStopMarksTaskStoppedAndCallsRunner(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:                "task-stop",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusRunning,
		UserRequest:           "Stop me",
		CreatedBy:             "tester",
		RemoteWorkdir:         "/srv/backend",
		RemoteCodexSessionID:  "session-stop",
		RemoteProcessIdentity: "pid-stop",
	})

	if err := service.Stop(ctx, task.TaskID); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusStopped {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusStopped)
	}

	runner := service.runner.(*fakeServiceRunner)
	if len(runner.calls) == 0 || runner.calls[len(runner.calls)-1] != "stop" {
		t.Fatalf("runner calls = %v, want stop", runner.calls)
	}
}

func newTestService(t *testing.T) (*Service, *Store, func()) {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "orchestrator.db")
	store, err := OpenStore(storePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	registry := &Registry{
		Machines: map[string]*MachineConfig{
			"machine_a": {ID: "machine_a", Host: "host-a", User: "coder"},
			"machine_b": {ID: "machine_b", Host: "host-b", User: "coder"},
		},
		Repositories: map[string]*RepositoryConfig{
			"repo_backend": {
				ID:                  "repo_backend",
				RemoteRepoURL:       "git@github.com:example/backend.git",
				RemoteWorkspaceRoot: "/srv/codex-tasks",
				DefaultBranch:       "main",
				MachineIDs:          []string{"machine_a", "machine_b"},
				PreCloneBootstrap:   []string{"setup-git-auth"},
				PostCloneBootstrap:  []string{"pnpm install"},
			},
		},
		Templates: map[string]*TemplateConfig{
			"feature_dev": {
				ID:                   "feature_dev",
				RepositoryID:         "repo_backend",
				ResolvedWorkflowPath: writeWorkflowFixture(t, "Feature workflow: analyze first\n"),
			},
		},
	}
	registry.Repositories["repo_backend"].Machines = []*MachineConfig{
		registry.Machines["machine_a"],
		registry.Machines["machine_b"],
	}
	registry.Templates["feature_dev"].Repository = registry.Repositories["repo_backend"]

	runner := &fakeServiceRunner{}
	decider := &fakeDecisionEngine{}
	notifier := &fakeTaskNotifier{}
	service := NewService(store, registry, NewScheduler(), runner, decider)
	service.SetNotifier(notifier)

	return service, store, func() { _ = store.Close() }
}

func seedTask(t *testing.T, store *Store, task TaskRun) TaskRun {
	t.Helper()

	if task.TaskID == "" {
		task.TaskID = "task-seed"
	}
	now := time.Now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = now
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	return task
}

func writeWorkflowFixture(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "workflow.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	return path
}

type fakeServiceRunner struct {
	calls []string

	startSession  RemoteSession
	probeResult   ProbeResult
	attachSession RemoteSession
	resumeSession RemoteSession
	outputWindow  OutputWindow
}

func (f *fakeServiceRunner) StartNewSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	return f.startSession, nil
}

func (f *fakeServiceRunner) ProbeSession(context.Context, ProbeRequest) (ProbeResult, error) {
	f.calls = append(f.calls, "probe")
	return f.probeResult, nil
}

func (f *fakeServiceRunner) AttachLiveSession(context.Context, AttachRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "attach")
	return f.attachSession, nil
}

func (f *fakeServiceRunner) ResumeExitedSession(context.Context, ResumeRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "resume")
	return f.resumeSession, nil
}

func (f *fakeServiceRunner) SendInput(context.Context, RemoteSession, string) error {
	f.calls = append(f.calls, "send")
	return nil
}

func (f *fakeServiceRunner) ReadWindow(context.Context, RemoteSession) (OutputWindow, error) {
	f.calls = append(f.calls, "read")
	return f.outputWindow, nil
}

func (f *fakeServiceRunner) StopTask(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "stop")
	return nil
}

type fakeDecisionEngine struct {
	result DecisionResult
}

func (f *fakeDecisionEngine) DecideNextStep(context.Context, DecisionContext) (DecisionResult, error) {
	return f.result, nil
}

type fakeTaskNotifier struct {
	lastTaskID string
	lastUserID string
}

func (f *fakeTaskNotifier) NotifyTaskQuestion(ctx context.Context, task TaskRun) error {
	f.lastTaskID = task.TaskID
	f.lastUserID = task.CreatedBy
	return nil
}
