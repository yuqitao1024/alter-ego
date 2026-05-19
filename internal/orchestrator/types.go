package orchestrator

import "time"

type TaskStatus string
type TaskPhase string
type WorkflowStage string

const (
	StatusPending            TaskStatus = "pending"
	StatusPreparingWorkspace TaskStatus = "preparing_workspace"
	StatusStartingSession    TaskStatus = "starting_session"
	StatusRunning            TaskStatus = "running"
	StatusWaitingUserInput   TaskStatus = "waiting_user_input"
	StatusDetached           TaskStatus = "detached"
	StatusCompleted          TaskStatus = "completed"
	StatusFailed             TaskStatus = "failed"
	StatusStopped            TaskStatus = "stopped"
)

const (
	TaskPhasePlanning  TaskPhase = "planning"
	TaskPhaseExecuting TaskPhase = "executing"
)

const (
	WorkflowStageRequirementDiscussion WorkflowStage = "requirement_discussion"
	WorkflowStageSpecWriting           WorkflowStage = "spec_writing"
	WorkflowStageSpecReview            WorkflowStage = "spec_review"
	WorkflowStagePlanWriting           WorkflowStage = "plan_writing"
	WorkflowStagePlanReview            WorkflowStage = "plan_review"
	WorkflowStageImplementation        WorkflowStage = "implementation"
	WorkflowStageVerification          WorkflowStage = "verification"
	WorkflowStageIntegration           WorkflowStage = "integration"
)

type TaskRun struct {
	TaskID               string
	TemplateID           string
	RepositoryID         string
	MachineID            string
	Status               TaskStatus
	Phase                TaskPhase
	WorkflowStage        WorkflowStage
	UserRequest          string
	CreatedBy            string
	RemoteWorkdir        string
	AppServerState
	LastInput             string
	LastOutputSummary     string
	LastDecisionAction    string
	AwaitingQuestion      *AwaitingQuestion
	CreatedAt             time.Time
	UpdatedAt             time.Time
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
