package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigBindsTemplateToRepositoryAndWorkflow(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
display_name: Machine A
host: machine-a.example.com
user: dev
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
display_name: Backend Repo
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/example-feature-dev.md
`)
	writeConfigFile(t, root, "configs/templates/ops-review.yaml", `
id: ops_review
repository_id: repo_backend
display_name: Ops Review
description: Secondary workflow for ordering checks
workflow_path: docs/workflows/ops-review.md
`)
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development

1. Sync the branch.
2. Implement the change.
`)
	writeConfigFile(t, root, "docs/workflows/ops-review.md", `
# Ops Review
`)

	cfg, err := LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry returned error: %v", err)
	}

	template := cfg.Templates["feature_dev"]
	if template == nil {
		t.Fatal("Templates[feature_dev] = nil")
	}
	if template.Repository == nil {
		t.Fatal("template.Repository = nil")
	}
	if template.Repository != cfg.Repositories["repo_backend"] {
		t.Fatal("template.Repository does not point at cfg.Repositories[repo_backend]")
	}
	if template.Repository.ID != "repo_backend" {
		t.Fatalf("template.Repository.ID = %q, want repo_backend", template.Repository.ID)
	}
	if len(template.Repository.Machines) != 1 {
		t.Fatalf("len(template.Repository.Machines) = %d, want 1", len(template.Repository.Machines))
	}
	if template.Repository.Machines[0] != cfg.Machines["machine_a"] {
		t.Fatal("template.Repository.Machines[0] does not point at cfg.Machines[machine_a]")
	}
	if template.Repository.Machines[0] == nil || template.Repository.Machines[0].ID != "machine_a" {
		t.Fatalf("template.Repository.Machines[0] = %#v, want machine_a", template.Repository.Machines[0])
	}

	wantWorkflowPath, err := filepath.EvalSymlinks(filepath.Join(root, "docs/workflows/example-feature-dev.md"))
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	if template.WorkflowPath != "docs/workflows/example-feature-dev.md" {
		t.Fatalf("template.WorkflowPath = %q, want authored relative path", template.WorkflowPath)
	}
	if template.ResolvedWorkflowPath != wantWorkflowPath {
		t.Fatalf("template.ResolvedWorkflowPath = %q, want %q", template.ResolvedWorkflowPath, wantWorkflowPath)
	}
	if len(cfg.TemplateList) != 2 || cfg.TemplateList[0].ID != "feature_dev" || cfg.TemplateList[1].ID != "ops_review" {
		t.Fatalf("TemplateList order = %#v, want [feature_dev ops_review]", templateIDs(cfg.TemplateList))
	}
	if len(cfg.RepositoryList) != 1 || cfg.RepositoryList[0].ID != "repo_backend" {
		t.Fatalf("RepositoryList order = %#v, want [repo_backend]", repositoryIDs(cfg.RepositoryList))
	}
}

func TestLoadConfigRejectsTemplateWithUnknownRepository(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
display_name: Machine A
host: machine-a.example.com
user: dev
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: missing_repo
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/example-feature-dev.md
`)
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development
`)

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "missing_repo") {
		t.Fatalf("LoadRegistry error = %q, want missing_repo", err)
	}
}

func TestLoadConfigRejectsRepositoryWithUnknownMachine(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
host: machine-a.example.com
user: dev
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
display_name: Backend Repo
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_missing
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/example-feature-dev.md
`)
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development
`)

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "machine_missing") {
		t.Fatalf("LoadRegistry error = %q, want machine_missing", err)
	}
}

func TestLoadConfigRejectsTemplateWithMissingWorkflowFile(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
display_name: Machine A
host: machine-a.example.com
user: dev
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
display_name: Backend Repo
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/missing.md
`)

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "docs/workflows/missing.md") {
		t.Fatalf("LoadRegistry error = %q, want missing workflow path", err)
	}
}

func TestLoadConfigRejectsTemplateWorkflowPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outsideWorkflow := filepath.Join(filepath.Dir(root), "escape.md")

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
host: machine-a.example.com
user: dev
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
workflow_path: ../escape.md
`)
	if err := os.WriteFile(outsideWorkflow, []byte("# Escape\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", outsideWorkflow, err)
	}

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "outside") && !strings.Contains(err.Error(), "root") {
		t.Fatalf("LoadRegistry error = %q, want outside-root validation error", err)
	}
}

func TestLoadConfigRejectsMissingRequiredFields(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
display_name: Machine A
host: machine-a.example.com
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
workflow_path: docs/workflows/example-feature-dev.md
`)
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development
`)

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "user") {
		t.Fatalf("LoadRegistry error = %q, want missing required field name", err)
	}
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()

	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
host: machine-a.example.com
user: dev
unexpected: true
`)
	writeConfigFile(t, root, "configs/repositories/backend.yaml", `
id: repo_backend
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/feature-dev.yaml", `
id: feature_dev
repository_id: repo_backend
workflow_path: docs/workflows/example-feature-dev.md
`)
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development
`)

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("LoadRegistry error = %q, want unknown field name", err)
	}
}

func TestLoadConfigRejectsMissingConfigDirectories(t *testing.T) {
	root := t.TempDir()

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "configs/machines") {
		t.Fatalf("LoadRegistry error = %q, want missing machines directory", err)
	}
}

func TestLoadConfigRejectsEmptyConfigCategory(t *testing.T) {
	root := t.TempDir()

	mustMkdirAll(t, filepath.Join(root, "configs/machines"))
	mustMkdirAll(t, filepath.Join(root, "configs/repositories"))
	mustMkdirAll(t, filepath.Join(root, "configs/templates"))

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error")
	}
	if !strings.Contains(err.Error(), "configs/machines") || !strings.Contains(err.Error(), "no config files") {
		t.Fatalf("LoadRegistry error = %q, want empty category error", err)
	}
}

func writeConfigFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()

	fullPath := filepath.Join(root, relativePath)
	mustMkdirAll(t, filepath.Dir(fullPath))
	if err := os.WriteFile(fullPath, []byte(strings.TrimLeft(contents, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", fullPath, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", path, err)
	}
}

func templateIDs(templates []*TemplateConfig) []string {
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		ids = append(ids, template.ID)
	}
	return ids
}

func repositoryIDs(repositories []*RepositoryConfig) []string {
	ids := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.ID)
	}
	return ids
}
