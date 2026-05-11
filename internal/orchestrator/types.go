package orchestrator

import "time"

type TaskStatus string

const (
	StatusPending            TaskStatus = "pending"
	StatusPreparingWorkspace TaskStatus = "preparing_workspace"
	StatusStartingSession   TaskStatus = "starting_session"
	StatusRunning           TaskStatus = "running"
	StatusWaitingUserInput  TaskStatus = "waiting_user_input"
	StatusDetached          TaskStatus = "detached"
	StatusCompleted         TaskStatus = "completed"
	StatusFailed            TaskStatus = "failed"
	StatusStopped           TaskStatus = "stopped"
)

type TaskRun struct {
	TaskID               string
	TemplateID           string
	RepositoryID         string
	MachineID            string
	Status               TaskStatus
	UserRequest          string
	CreatedBy            string
	RemoteWorkdir        string
	TMUXSessionName      string
	RemoteCodexSessionID string
	LastInput            string
	LastOutputSummary    string
	LastScreenDigest     string
	AwaitingQuestion     *AwaitingQuestion
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AwaitingQuestion struct {
	QuestionText   string     `json:"question_text"`
	OptionsSummary string     `json:"options_summary"`
	ContextExcerpt string     `json:"context_excerpt"`
	QuestionType   string     `json:"question_type,omitempty"`
	AskedAt        time.Time  `json:"asked_at"`
	AnsweredAt     *time.Time `json:"answered_at,omitempty"`
}

type TaskEvent struct {
	TaskID    string
	EventType string
	Message   string
	CreatedAt time.Time
}
