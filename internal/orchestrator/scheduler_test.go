package orchestrator

import "testing"

func TestSelectMachineChoosesLeastLoadedMachine(t *testing.T) {
	repo := RepositoryConfig{
		ID:         "repo_backend",
		MachineIDs: []string{"machine_a", "machine_b"},
	}
	active := []TaskRun{
		{TaskID: "task-1", MachineID: "machine_a", Status: StatusRunning},
		{TaskID: "task-2", MachineID: "machine_a", Status: StatusDetached},
		{TaskID: "task-3", MachineID: "machine_b", Status: StatusRunning},
	}

	machineID, err := SelectMachine(repo, active)
	if err != nil {
		t.Fatalf("SelectMachine returned error: %v", err)
	}
	if machineID != "machine_b" {
		t.Fatalf("machineID = %q, want machine_b", machineID)
	}
}

func TestSelectMachineBreaksTiesByRepositoryOrder(t *testing.T) {
	repo := RepositoryConfig{
		ID:         "repo_backend",
		MachineIDs: []string{"machine_b", "machine_a"},
	}
	active := []TaskRun{
		{TaskID: "task-1", MachineID: "machine_a", Status: StatusRunning},
		{TaskID: "task-2", MachineID: "machine_b", Status: StatusRunning},
	}

	machineID, err := SelectMachine(repo, active)
	if err != nil {
		t.Fatalf("SelectMachine returned error: %v", err)
	}
	if machineID != "machine_b" {
		t.Fatalf("machineID = %q, want machine_b", machineID)
	}
}

func TestSchedulerSkipsWaitingUserDecisionTasks(t *testing.T) {
	scheduler := NewScheduler()
	tasks := []TaskRun{
		{TaskID: "task-waiting", Status: StatusWaitingUserDecision},
		{TaskID: "task-running", Status: StatusRunning},
	}

	task, ok := scheduler.Next(tasks)
	if !ok {
		t.Fatal("Next returned ok=false, want runnable task")
	}
	if task.TaskID != "task-running" {
		t.Fatalf("task.TaskID = %q, want task-running", task.TaskID)
	}
}

func TestSchedulerRotatesRunnableTasksRoundRobin(t *testing.T) {
	scheduler := NewScheduler()
	tasks := []TaskRun{
		{TaskID: "task-a", Status: StatusRunning},
		{TaskID: "task-b", Status: StatusDetached},
		{TaskID: "task-c", Status: StatusWaitingUserDecision},
		{TaskID: "task-d", Status: StatusRunning},
	}

	order := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		task, ok := scheduler.Next(tasks)
		if !ok {
			t.Fatalf("iteration %d: Next returned ok=false", i)
		}
		order = append(order, task.TaskID)
	}

	want := []string{"task-a", "task-b", "task-d", "task-a"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (full order=%v)", i, order[i], want[i], order)
		}
	}
}
