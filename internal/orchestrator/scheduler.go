package orchestrator

import "fmt"

type Scheduler struct {
	nextIndex int
}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

func SelectMachine(repo RepositoryConfig, active []TaskRun) (string, error) {
	if len(repo.MachineIDs) == 0 {
		return "", fmt.Errorf("repository %q has no machines", repo.ID)
	}

	loads := make(map[string]int, len(repo.MachineIDs))
	for _, machineID := range repo.MachineIDs {
		loads[machineID] = 0
	}

	for _, task := range active {
		if _, ok := loads[task.MachineID]; ok && countsTowardMachineLoad(task.Status) {
			loads[task.MachineID]++
		}
	}

	selected := repo.MachineIDs[0]
	lowest := loads[selected]
	for _, machineID := range repo.MachineIDs[1:] {
		if load := loads[machineID]; load < lowest {
			selected = machineID
			lowest = load
		}
	}

	return selected, nil
}

func (s *Scheduler) Next(tasks []TaskRun) (TaskRun, bool) {
	if len(tasks) == 0 {
		return TaskRun{}, false
	}

	start := s.nextIndex % len(tasks)
	for offset := 0; offset < len(tasks); offset++ {
		index := (start + offset) % len(tasks)
		task := tasks[index]
		if !isRunnable(task.Status) {
			continue
		}

		s.nextIndex = (index + 1) % len(tasks)
		return task, true
	}

	return TaskRun{}, false
}

func countsTowardMachineLoad(status TaskStatus) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusStopped:
		return false
	default:
		return true
	}
}

func isRunnable(status TaskStatus) bool {
	switch status {
	case StatusWaitingUserDecision, StatusCompleted, StatusFailed, StatusStopped:
		return false
	default:
		return true
	}
}
