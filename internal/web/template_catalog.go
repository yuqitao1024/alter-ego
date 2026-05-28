package web

import (
	"context"

	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

type RegistryTemplateCatalog struct {
	Registry *orchestrator.Registry
}

func (c RegistryTemplateCatalog) ListTemplates(context.Context) ([]TemplateSummary, error) {
	if c.Registry == nil {
		return []TemplateSummary{}, nil
	}

	items := make([]TemplateSummary, 0, len(c.Registry.TemplateList))
	for _, template := range c.Registry.TemplateList {
		if template == nil {
			continue
		}
		items = append(items, TemplateSummary{
			ID:           template.ID,
			DisplayName:  template.DisplayName,
			Description:  template.Description,
			RepositoryID: template.RepositoryID,
		})
	}
	return items, nil
}
