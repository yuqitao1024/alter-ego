package orchestrator

type SupervisorClassification string
type ReplyPolicy string

const (
	ClassificationPlanDecision      SupervisorClassification = "plan_decision"
	ClassificationExecutionApproval SupervisorClassification = "execution_approval"
	ClassificationProgressUpdate    SupervisorClassification = "progress_update"
)

const (
	ReplyPolicyAutoContinue ReplyPolicy = "auto_continue"
	ReplyPolicyAskUser      ReplyPolicy = "ask_user"
)

type SupervisorDecision struct {
	Classification   SupervisorClassification `json:"classification"`
	ShouldReplyCodex bool                     `json:"should_reply_codex"`
	ShouldNotifyUser bool                     `json:"should_notify_user"`
	ReplyPolicy      ReplyPolicy              `json:"reply_policy"`
	Reason           string                   `json:"reason"`
	UserUpdate       string                   `json:"user_update"`
	UserQuestion     string                   `json:"user_question"`
	CodexReply       string                   `json:"codex_reply"`
}

type PolicyResult struct {
	AllowReply     bool
	EscalateToUser bool
	NotifyUser     bool
	ReplyContent   string
	UserQuestion   string
}

func ApplySupervisorPolicy(task TaskRun, req *TaskServerRequest, decision SupervisorDecision) PolicyResult {
	if req == nil || task.PendingRequestID == "" || req.RequestID != task.PendingRequestID {
		return PolicyResult{}
	}
	if req.Status != ServerRequestStatusPending {
		return PolicyResult{}
	}

	result := PolicyResult{
		NotifyUser:   decision.ShouldNotifyUser,
		UserQuestion: decision.UserQuestion,
	}

	switch decision.Classification {
	case ClassificationExecutionApproval:
		if decision.ShouldReplyCodex && decision.ReplyPolicy == ReplyPolicyAutoContinue {
			result.AllowReply = true
			result.ReplyContent = decision.CodexReply
		}
	case ClassificationPlanDecision:
		result.EscalateToUser = true
	}

	if decision.ReplyPolicy == ReplyPolicyAskUser {
		result.EscalateToUser = true
	}

	return result
}
