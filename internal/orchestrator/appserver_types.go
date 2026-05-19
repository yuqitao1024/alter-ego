package orchestrator

type WorkflowStage string

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
