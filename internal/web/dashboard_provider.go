package web

import (
	"context"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type TaskDashboardService interface {
	Dashboard(ctx context.Context) (orchestrator.DashboardSnapshot, error)
	Reply(ctx context.Context, taskID, text string) error
	Reopen(ctx context.Context, taskID, text string) error
	Complete(ctx context.Context, taskID string) error
	Stop(ctx context.Context, taskID string) error
	Delete(ctx context.Context, taskID string) error
}

type OrchestratorDashboardProvider struct {
	Service TaskDashboardService
}

func (p OrchestratorDashboardProvider) Dashboard(ctx context.Context) (any, error) {
	if p.Service == nil {
		return orchestrator.DashboardSnapshot{}, nil
	}
	return p.Service.Dashboard(ctx)
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
