package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/agent"
	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

func TestBuildTaskSubsystemRequiresConfigRoot(t *testing.T) {
	t.Parallel()

	_, err := buildTaskSubsystem(context.Background(), taskSubsystemConfig{
		RegistryRoot: filepath.Join(t.TempDir(), "missing"),
		DBPath:       filepath.Join(t.TempDir(), "orchestrator.db"),
	})
	if err == nil {
		t.Fatal("buildTaskSubsystem returned nil error, want missing config root error")
	}
}

func TestBuildTaskSubsystemBuildsService(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTaskConfigFixtures(t, root)

	subsystem, err := buildTaskSubsystem(context.Background(), taskSubsystemConfig{
		RegistryRoot: root,
		DBPath:       filepath.Join(root, "orchestrator.db"),
		LLMConfig: agent.Config{
			Provider: "dashscope",
			APIKey:   "test-key",
			BaseURL:  "https://example.invalid/v1",
			Model:    "test-model",
		},
	})
	if err != nil {
		t.Fatalf("buildTaskSubsystem returned error: %v", err)
	}
	defer subsystem.Close()

	if subsystem.Service == nil {
		t.Fatal("subsystem.Service is nil")
	}
	if subsystem.TaskHandler == nil {
		t.Fatal("subsystem.TaskHandler is nil")
	}
	if subsystem.Registry == nil || subsystem.Registry.Templates["feature_dev"] == nil {
		t.Fatalf("subsystem.Registry = %#v", subsystem.Registry)
	}
	if _, ok := subsystem.Runner.(*orchestrator.AppServerRunner); !ok {
		t.Fatalf("subsystem.Runner = %T, want *orchestrator.AppServerRunner", subsystem.Runner)
	}
}

func TestBuildTaskSubsystemRequiresLLMConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTaskConfigFixtures(t, root)

	_, err := buildTaskSubsystem(context.Background(), taskSubsystemConfig{
		RegistryRoot: root,
		DBPath:       filepath.Join(root, "orchestrator.db"),
	})
	if err == nil {
		t.Fatal("buildTaskSubsystem returned nil error, want missing LLM config error")
	}
}

func TestBuildTaskSubsystemRequiresMachineAppServerFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTaskConfigFixturesWithoutAppServerFields(t, root)

	_, err := buildTaskSubsystem(context.Background(), taskSubsystemConfig{
		RegistryRoot: root,
		DBPath:       filepath.Join(root, "orchestrator.db"),
		LLMConfig: agent.Config{
			Provider: "dashscope",
			APIKey:   "test-key",
			BaseURL:  "https://example.invalid/v1",
			Model:    "test-model",
		},
	})
	if err == nil {
		t.Fatal("buildTaskSubsystem returned nil error, want missing app-server fields error")
	}
	for _, part := range []string{
		"app_server_listen_host",
		"app_server_listen_port",
		"app_server_service_name",
		"app_server_install_user",
	} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("buildTaskSubsystem error = %q, want substring %q", err, part)
		}
	}
}

func writeTaskConfigFixtures(t *testing.T, root string) {
	t.Helper()

	writeFile(t, filepath.Join(root, "configs/machines/machine_a.yaml"), `id: machine_a
display_name: Machine A
host: 127.0.0.1
port: 22
user: coder
app_server_listen_host: 0.0.0.0
app_server_listen_port: 4317
app_server_service_name: codex-app-server
app_server_install_user: coder
app_server_ws_auth_token: test-token
`)
	writeFile(t, filepath.Join(root, "configs/repositories/repo_backend.yaml"), `id: repo_backend
display_name: Backend Repo
remote_repo_url: git@github.com:example/backend.git
remote_workspace_root: /srv/codex-tasks
default_branch: main
machine_ids:
  - machine_a
pre_clone_bootstrap:
  - setup-git-auth
post_clone_bootstrap:
  - pnpm install
`)
	writeFile(t, filepath.Join(root, "configs/templates/feature_dev.yaml"), `id: feature_dev
repository_id: repo_backend
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/feature_dev.md
`)
	writeFile(t, filepath.Join(root, "docs/workflows/feature_dev.md"), "Workflow: analyze and implement.\n")
}

func writeTaskConfigFixturesWithoutAppServerFields(t *testing.T, root string) {
	t.Helper()

	writeTaskConfigFixtures(t, root)

	writeFile(t, filepath.Join(root, "configs/machines/machine_a.yaml"), `id: machine_a
display_name: Machine A
host: 127.0.0.1
port: 22
user: coder
`)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
