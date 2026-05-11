package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type MachineConfig struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"display_name"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	User        string `yaml:"user"`
}

type RepositoryConfig struct {
	ID            string           `yaml:"id"`
	DisplayName   string           `yaml:"display_name"`
	RemotePath    string           `yaml:"remote_path"`
	DefaultBranch string           `yaml:"default_branch"`
	MachineIDs    []string         `yaml:"machine_ids"`
	Machines      []*MachineConfig `yaml:"-"`
}

type TemplateConfig struct {
	ID           string            `yaml:"id"`
	RepositoryID string            `yaml:"repository_id"`
	DisplayName  string            `yaml:"display_name"`
	Description  string            `yaml:"description"`
	WorkflowPath string            `yaml:"workflow_path"`
	Repository   *RepositoryConfig `yaml:"-"`
}

type Registry struct {
	Machines     map[string]*MachineConfig
	Repositories map[string]*RepositoryConfig
	Templates    map[string]*TemplateConfig
}

func LoadRegistry(root string) (*Registry, error) {
	registry := &Registry{
		Machines:     map[string]*MachineConfig{},
		Repositories: map[string]*RepositoryConfig{},
		Templates:    map[string]*TemplateConfig{},
	}

	if err := loadConfigDir(filepath.Join(root, "configs/machines"), registry.Machines); err != nil {
		return nil, err
	}
	if err := loadConfigDir(filepath.Join(root, "configs/repositories"), registry.Repositories); err != nil {
		return nil, err
	}
	if err := loadConfigDir(filepath.Join(root, "configs/templates"), registry.Templates); err != nil {
		return nil, err
	}

	for _, repository := range registry.Repositories {
		repository.Machines = repository.Machines[:0]
		for _, machineID := range repository.MachineIDs {
			machine := registry.Machines[machineID]
			if machine == nil {
				return nil, fmt.Errorf("repository %q references unknown machine %q", repository.ID, machineID)
			}
			repository.Machines = append(repository.Machines, machine)
		}
	}

	for _, template := range registry.Templates {
		repository := registry.Repositories[template.RepositoryID]
		if repository == nil {
			return nil, fmt.Errorf("template %q references unknown repository %q", template.ID, template.RepositoryID)
		}

		workflowPath := filepath.Join(root, template.WorkflowPath)
		info, err := os.Stat(workflowPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("template %q workflow path %q does not exist", template.ID, template.WorkflowPath)
			}
			return nil, fmt.Errorf("stat workflow for template %q: %w", template.ID, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("template %q workflow path %q is a directory", template.ID, template.WorkflowPath)
		}

		template.Repository = repository
		template.WorkflowPath = workflowPath
	}

	return registry, nil
}

type configDocument interface {
	GetID() string
}

func (m *MachineConfig) GetID() string    { return m.ID }
func (r *RepositoryConfig) GetID() string { return r.ID }
func (t *TemplateConfig) GetID() string   { return t.ID }

func loadConfigDir[T configDocument](dir string, destination map[string]T) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("glob %q: %w", dir, err)
	}

	for _, path := range paths {
		entry, err := loadConfigFile[T](path)
		if err != nil {
			return err
		}

		id := entry.GetID()
		if id == "" {
			return fmt.Errorf("config %q is missing id", path)
		}
		if _, exists := destination[id]; exists {
			return fmt.Errorf("duplicate config id %q in %q", id, path)
		}

		destination[id] = entry
	}

	return nil
}

func loadConfigFile[T any](path string) (T, error) {
	var cfg T

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
}
