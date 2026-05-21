package orchestrator

import "time"

type TaskStatus string
type CompletionCheckStatus string
type ServerRequestType string
type ServerRequestStatus string

const (
	StatusPending          TaskStatus = "pending"
	StatusStarting         TaskStatus = "starting"
	StatusRunning          TaskStatus = "running"
	StatusWaitingUserInput TaskStatus = "waiting_user_input"
	StatusRecovering       TaskStatus = "recovering"
	StatusCompleted        TaskStatus = "completed"
	StatusFailed           TaskStatus = "failed"
	StatusStopped          TaskStatus = "stopped"
)

const (
	CompletionCheckStatusNotStarted      CompletionCheckStatus = "not_started"
	CompletionCheckStatusSent            CompletionCheckStatus = "sent"
	CompletionCheckStatusConfirmedDone   CompletionCheckStatus = "confirmed_done"
	CompletionCheckStatusReportedPending CompletionCheckStatus = "reported_remaining"
)

const (
	ServerRequestTypeUserInput       ServerRequestType = "request_user_input"
	ServerRequestTypeCommandApproval ServerRequestType = "command_approval"
	ServerRequestTypeFileApproval    ServerRequestType = "file_change_approval"
)

const (
	ServerRequestStatusPending  ServerRequestStatus = "pending"
	ServerRequestStatusReplying ServerRequestStatus = "replying"
	ServerRequestStatusReplied  ServerRequestStatus = "replied"
	ServerRequestStatusResolved ServerRequestStatus = "resolved"
	ServerRequestStatusIgnored  ServerRequestStatus = "ignored"
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
	ThreadID              string
	ActiveTurnID          string
	LastInput             string
	LastOutputSummary     string
	LastDecisionAction    string
	PendingRequestID      string
	CompletionCheckStatus CompletionCheckStatus
	CompletionCheckSentAt *time.Time
	CompletionCheckDoneAt *time.Time
	AwaitingQuestion      *AwaitingQuestion
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type TaskServerRequest struct {
	RequestID      string
	TaskID         string
	ThreadID       string
	TurnID         string
	RequestType    ServerRequestType
	RequestPayload string
	Status         ServerRequestStatus
	DecisionSource string
	ReplyContent   string
	CreatedAt      time.Time
	ReplyStartedAt *time.Time
	RepliedAt      *time.Time
	ResolvedAt     *time.Time
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

func (s TaskStatus) IsDeletable() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusStopped:
		return true
	default:
		return false
	}
}

func (s TaskStatus) IsStoppable() bool {
	switch s {
	case StatusRunning, StatusWaitingUserInput:
		return true
	default:
		return false
	}
}

type TaskQuestion struct {
	ID             int64
	TaskID         string
	QuestionType   string
	QuestionText   string
	OptionsSummary string
	ContextExcerpt string
	AskedAt        time.Time
	AnsweredAt     *time.Time
	AnswerText     string
}
