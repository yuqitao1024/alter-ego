package web

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

func TestRegistryTemplateCatalogIncludesTaskTypeAndWorkspaceID(t *testing.T) {
	t.Parallel()

	catalog := RegistryTemplateCatalog{
		Registry: &orchestrator.Registry{
			TemplateList: []*orchestrator.TemplateConfig{
				{
					ID:          "code_review",
					DisplayName: "Code Review",
					Description: "Review the latest pull request.",
					TaskType:    orchestrator.TaskTypeCodeReview,
					WorkspaceID: "backend_workspace",
				},
			},
		},
	}

	items, err := catalog.ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListTemplates returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].TaskType != string(orchestrator.TaskTypeCodeReview) {
		t.Fatalf("items[0].TaskType = %q, want code_review", items[0].TaskType)
	}
	if items[0].WorkspaceID != "backend_workspace" {
		t.Fatalf("items[0].WorkspaceID = %q, want backend_workspace", items[0].WorkspaceID)
	}
}
