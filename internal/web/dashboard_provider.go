package web

import (
	"context"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type TaskDashboardService interface {
	Dashboard(ctx context.Context) (orchestrator.DashboardSnapshot, error)
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
