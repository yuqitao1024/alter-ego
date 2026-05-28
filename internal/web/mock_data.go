package web

import "context"

type MockDataProvider struct{}

func (MockDataProvider) MockDashboard(context.Context) any {
	return map[string]any{
		"summary": map[string]any{
			"running": 3,
			"waiting": 1,
			"failed":  0,
		},
		"tasks": []map[string]any{
			{
				"id":      "task-demo-1",
				"title":   "Mission Control shell integration",
				"status":  "running",
				"summary": "Building dashboard phase 1 shell.",
			},
			{
				"id":      "task-demo-2",
				"title":   "Feishu OAuth flow",
				"status":  "waiting_user_input",
				"summary": "Waiting for operator confirmation on login behavior.",
			},
		},
	}
}
