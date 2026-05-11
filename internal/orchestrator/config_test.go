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
	writeConfigFile(t, root, "docs/workflows/example-feature-dev.md", `
# Feature Development

1. Sync the branch.
2. Implement the change.
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
	if template.Repository.ID != "repo_backend" {
		t.Fatalf("template.Repository.ID = %q, want repo_backend", template.Repository.ID)
	}

	wantWorkflowPath := filepath.Join(root, "docs/workflows/example-feature-dev.md")
	if template.WorkflowPath != wantWorkflowPath {
		t.Fatalf("template.WorkflowPath = %q, want %q", template.WorkflowPath, wantWorkflowPath)
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

func writeConfigFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()

	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(strings.TrimLeft(contents, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", fullPath, err)
	}
}
