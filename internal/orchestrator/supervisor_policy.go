package orchestrator

type SupervisorClassification string
type ReplyPolicy string
type CompletionDisposition string

const (
	ClassificationPlanDecision      SupervisorClassification = "plan_decision"
	ClassificationExecutionApproval SupervisorClassification = "execution_approval"
	ClassificationProgressUpdate    SupervisorClassification = "progress_update"
	ClassificationCompletionSignal  SupervisorClassification = "completion_signal"
	ClassificationIgnore            SupervisorClassification = "ignore"
)

const (
	ReplyPolicyNoReply      ReplyPolicy = "no_reply"
	ReplyPolicyAutoContinue ReplyPolicy = "auto_continue"
	ReplyPolicyAskUser      ReplyPolicy = "ask_user"
)

const (
	CompletionDispositionNone             CompletionDisposition = "none"
	CompletionDispositionSignalComplete   CompletionDisposition = "signal_complete"
	CompletionDispositionConfirmedDone    CompletionDisposition = "confirmed_done"
	CompletionDispositionReportedRemaining CompletionDisposition = "reported_remaining"
)

type SupervisorDecision struct {
	Classification         SupervisorClassification `json:"classification"`
	ShouldReplyCodex       bool                     `json:"should_reply_codex"`
	ShouldNotifyUser       bool                     `json:"should_notify_user"`
	ReplyPolicy            ReplyPolicy              `json:"reply_policy"`
	Reason                 string                   `json:"reason"`
	UserUpdate             string                   `json:"user_update"`
	UserQuestion           string                   `json:"user_question"`
	CodexReply             string                   `json:"codex_reply"`
	CompletionDisposition  CompletionDisposition    `json:"completion_disposition"`
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
	case ClassificationIgnore:
	}

	if decision.ReplyPolicy == ReplyPolicyAskUser {
		result.EscalateToUser = true
	}

	return result
}
