package web

import (
	"context"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type TaskDashboardService interface {
	Dashboard(ctx context.Context) (orchestrator.DashboardSnapshot, error)
	TaskDetail(ctx context.Context, taskID string) (orchestrator.DashboardTaskDetail, error)
	StartTask(ctx context.Context, templateID, createdBy, userRequest string) (orchestrator.TaskRun, error)
	Reply(ctx context.Context, taskID, text string) error
	Reopen(ctx context.Context, taskID, text string) error
	Complete(ctx context.Context, taskID string) error
	Stop(ctx context.Context, taskID string) error
	Delete(ctx context.Context, taskID string) error
}

type TemplateCatalogService interface {
	ListTemplates(ctx context.Context) ([]TemplateSummary, error)
}

type TemplateSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	TaskType    string `json:"task_type"`
	WorkspaceID string `json:"workspace_id"`
}

type OrchestratorDashboardProvider struct {
	Service TaskDashboardService
	Catalog TemplateCatalogService
}

func (p OrchestratorDashboardProvider) Dashboard(ctx context.Context) (any, error) {
	if p.Service == nil {
		return orchestrator.DashboardSnapshot{}, nil
	}
	return p.Service.Dashboard(ctx)
}

func (p OrchestratorDashboardProvider) Templates(ctx context.Context) (any, error) {
	if p.Catalog == nil {
		return []TemplateSummary{}, nil
	}
	return p.Catalog.ListTemplates(ctx)
}

func (p OrchestratorDashboardProvider) TaskDetail(ctx context.Context, taskID string) (any, error) {
	if p.Service == nil {
		return orchestrator.DashboardTaskDetail{}, nil
	}
	return p.Service.TaskDetail(ctx, taskID)
}

func (p OrchestratorDashboardProvider) StartTask(ctx context.Context, templateID, createdBy, requirement string) (any, error) {
	if p.Service == nil {
		return map[string]any{}, nil
	}
	return p.Service.StartTask(ctx, templateID, createdBy, requirement)
}

func (p OrchestratorDashboardProvider) StopTask(ctx context.Context, taskID string) error {
	if p.Service == nil {
		return nil
	}
	return p.Service.Stop(ctx, taskID)
}

func (p OrchestratorDashboardProvider) CompleteTask(ctx context.Context, taskID string) error {
	if p.Service == nil {
		return nil
	}
	return p.Service.Complete(ctx, taskID)
}

func (p OrchestratorDashboardProvider) DeleteTask(ctx context.Context, taskID string) error {
	if p.Service == nil {
		return nil
	}
	return p.Service.Delete(ctx, taskID)
}

func (p OrchestratorDashboardProvider) ReplyTask(ctx context.Context, taskID, text string) error {
	if p.Service == nil {
		return nil
	}
	return p.Service.Reply(ctx, taskID, text)
}

func (p OrchestratorDashboardProvider) ReopenTask(ctx context.Context, taskID, text string) error {
	if p.Service == nil {
		return nil
	}
	return p.Service.Reopen(ctx, taskID, text)
}
