package orchestrator

import "time"

type TaskStatus string

const (
	StatusPending             TaskStatus = "pending"
	StatusStarting            TaskStatus = "starting"
	StatusRunning             TaskStatus = "running"
	StatusWaitingUserDecision TaskStatus = "waiting_user_decision"
	StatusDetached            TaskStatus = "detached"
	StatusProbing             TaskStatus = "probing"
	StatusAttaching           TaskStatus = "attaching"
	StatusResuming            TaskStatus = "resuming"
	StatusCompleted           TaskStatus = "completed"
	StatusFailed              TaskStatus = "failed"
	StatusStopped             TaskStatus = "stopped"
)

type TaskRun struct {
	TaskID                string
	TemplateID            string
	RepositoryID          string
	MachineID             string
	Status                TaskStatus
	UserRequest           string
	CreatedBy             string
	RemoteWorkdir         string
	RemoteCodexSessionID  string
	RemoteProcessIdentity string
	LastInput             string
	LastOutputSummary     string
	AwaitingQuestion      *AwaitingQuestion
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type AwaitingQuestion struct {
	QuestionText   string     `json:"question_text"`
	OptionsSummary string     `json:"options_summary"`
	ContextExcerpt string     `json:"context_excerpt"`
	AskedAt        time.Time  `json:"asked_at"`
	AnsweredAt     *time.Time `json:"answered_at,omitempty"`
}

type TaskEvent struct {
	TaskID    string
	EventType string
	Message   string
	CreatedAt time.Time
}
