package orchestrator

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	ID                   string            `yaml:"id"`
	RepositoryID         string            `yaml:"repository_id"`
	DisplayName          string            `yaml:"display_name"`
	Description          string            `yaml:"description"`
	WorkflowPath         string            `yaml:"workflow_path"`
	ResolvedWorkflowPath string            `yaml:"-"`
	Repository           *RepositoryConfig `yaml:"-"`
}

type Registry struct {
	Machines       map[string]*MachineConfig
	Repositories   map[string]*RepositoryConfig
	Templates      map[string]*TemplateConfig
	MachineList    []*MachineConfig
	RepositoryList []*RepositoryConfig
	TemplateList   []*TemplateConfig
}

func LoadRegistry(root string) (*Registry, error) {
	registryRoot, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}

	registry := &Registry{
		Machines:       map[string]*MachineConfig{},
		Repositories:   map[string]*RepositoryConfig{},
		Templates:      map[string]*TemplateConfig{},
		MachineList:    []*MachineConfig{},
		RepositoryList: []*RepositoryConfig{},
		TemplateList:   []*TemplateConfig{},
	}

	if err := loadConfigDir(filepath.Join(root, "configs/machines"), &registry.MachineList, registry.Machines); err != nil {
		return nil, err
	}
	if err := loadConfigDir(filepath.Join(root, "configs/repositories"), &registry.RepositoryList, registry.Repositories); err != nil {
		return nil, err
	}
	if err := loadConfigDir(filepath.Join(root, "configs/templates"), &registry.TemplateList, registry.Templates); err != nil {
		return nil, err
	}

	for _, repository := range registry.RepositoryList {
		repository.Machines = repository.Machines[:0]
		for _, machineID := range repository.MachineIDs {
			machine := registry.Machines[machineID]
			if machine == nil {
				return nil, fmt.Errorf("repository %q references unknown machine %q", repository.ID, machineID)
			}
			repository.Machines = append(repository.Machines, machine)
		}
	}

	for _, template := range registry.TemplateList {
		repository := registry.Repositories[template.RepositoryID]
		if repository == nil {
			return nil, fmt.Errorf("template %q references unknown repository %q", template.ID, template.RepositoryID)
		}

		workflowPath, err := resolveWorkflowPath(registryRoot, template.ID, template.WorkflowPath)
		if err != nil {
			return nil, err
		}

		template.Repository = repository
		template.ResolvedWorkflowPath = workflowPath
	}

	return registry, nil
}

type configDocument interface {
	GetID() string
	Validate() error
}

func (m *MachineConfig) GetID() string    { return m.ID }
func (r *RepositoryConfig) GetID() string { return r.ID }
func (t *TemplateConfig) GetID() string   { return t.ID }

func (m *MachineConfig) Validate() error {
	return requireFields("machine", m.ID, []requiredField{
		{name: "id", value: m.ID},
		{name: "host", value: m.Host},
		{name: "user", value: m.User},
	})
}

func (r *RepositoryConfig) Validate() error {
	if err := requireFields("repository", r.ID, []requiredField{
		{name: "id", value: r.ID},
		{name: "remote_path", value: r.RemotePath},
		{name: "default_branch", value: r.DefaultBranch},
	}); err != nil {
		return err
	}
	if len(r.MachineIDs) == 0 {
		return fmt.Errorf("repository %q is missing required field %q", r.ID, "machine_ids")
	}
	for _, machineID := range r.MachineIDs {
		if strings.TrimSpace(machineID) == "" {
			return fmt.Errorf("repository %q has empty machine_ids entry", r.ID)
		}
	}
	return nil
}

func (t *TemplateConfig) Validate() error {
	return requireFields("template", t.ID, []requiredField{
		{name: "id", value: t.ID},
		{name: "repository_id", value: t.RepositoryID},
		{name: "workflow_path", value: t.WorkflowPath},
	})
}

func loadConfigDir[T configDocument](dir string, ordered *[]T, destination map[string]T) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config directory %q does not exist", dir)
		}
		return fmt.Errorf("stat config directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config path %q is not a directory", dir)
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("glob %q: %w", dir, err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("config directory %q has no config files", dir)
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
		*ordered = append(*ordered, entry)
	}

	return nil
}

func loadConfigFile[T configDocument](path string) (T, error) {
	var cfg T

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return cfg, fmt.Errorf("parse config %q: multiple YAML documents are not supported", path)
		}
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config %q: %w", path, err)
	}

	return cfg, nil
}

type requiredField struct {
	name  string
	value string
}

func requireFields(kind, id string, fields []requiredField) error {
	subjectID := id
	if strings.TrimSpace(subjectID) == "" {
		subjectID = "<unknown>"
	}

	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s %q is missing required field %q", kind, subjectID, field.name)
		}
	}

	return nil
}

func canonicalRoot(root string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve registry root %q: %w", root, err)
	}

	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve registry root %q: %w", root, err)
	}

	return rootResolved, nil
}

func resolveWorkflowPath(root, templateID, authoredPath string) (string, error) {
	joinedPath := filepath.Join(root, authoredPath)

	info, err := os.Stat(joinedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("template %q workflow path %q does not exist", templateID, authoredPath)
		}
		return "", fmt.Errorf("stat workflow for template %q: %w", templateID, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("template %q workflow path %q is a directory", templateID, authoredPath)
	}

	resolvedPath, err := filepath.EvalSymlinks(joinedPath)
	if err != nil {
		return "", fmt.Errorf("resolve workflow for template %q: %w", templateID, err)
	}

	relativePath, err := filepath.Rel(root, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("resolve workflow for template %q: %w", templateID, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("template %q workflow path %q resolves outside registry root", templateID, authoredPath)
	}

	return resolvedPath, nil
}
