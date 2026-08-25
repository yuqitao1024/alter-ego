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
	ID                   string   `yaml:"id"`
	DisplayName          string   `yaml:"display_name"`
	Host                 string   `yaml:"host"`
	Port                 int      `yaml:"port"`
	User                 string   `yaml:"user"`
	ShellInit            []string `yaml:"shell_init"`
	AppServerListenHost  string   `yaml:"app_server_listen_host"`
	AppServerListenPort  int      `yaml:"app_server_listen_port"`
	AppServerServiceName string   `yaml:"app_server_service_name"`
	AppServerInstallUser string   `yaml:"app_server_install_user"`
	AppServerWSAuthToken string   `yaml:"app_server_ws_auth_token"`
	AppServerSocket      string   `yaml:"-"`
	AppServerBootstrap   []string `yaml:"-"`
}

type RepositoryConfig struct {
	ID                  string           `yaml:"id"`
	DisplayName         string           `yaml:"display_name"`
	RemoteRepoURL       string           `yaml:"remote_repo_url"`
	RemoteWorkspaceRoot string           `yaml:"remote_workspace_root"`
	DefaultBranch       string           `yaml:"default_branch"`
	MachineIDs          []string         `yaml:"machine_ids"`
	PreCloneBootstrap   []string         `yaml:"pre_clone_bootstrap"`
	PostCloneBootstrap  []string         `yaml:"post_clone_bootstrap"`
	Machines            []*MachineConfig `yaml:"-"`
}

type WorkspaceSetupType string

const (
	WorkspaceSetupTypeRepo   WorkspaceSetupType = "repo"
	WorkspaceSetupTypeEmpty  WorkspaceSetupType = "empty"
	WorkspaceSetupTypeCustom WorkspaceSetupType = "custom"
)

type WorkspaceSetup struct {
	Type               WorkspaceSetupType `yaml:"type"`
	RemoteRepoURL      string             `yaml:"remote_repo_url"`
	CheckoutBranch     string             `yaml:"checkout_branch"`
	PreCloneBootstrap  []string           `yaml:"pre_clone_bootstrap"`
	PostCloneBootstrap []string           `yaml:"post_clone_bootstrap"`
	CustomSteps        []string           `yaml:"steps"`
}

type WorkspaceConfig struct {
	Root       string           `yaml:"root"`
	MachineIDs []string         `yaml:"machine_ids"`
	Setup      *WorkspaceSetup  `yaml:"setup"`
	Machines   []*MachineConfig `yaml:"-"`
}

type WorkspaceProfileConfig struct {
	ID          string           `yaml:"id"`
	DisplayName string           `yaml:"display_name"`
	Description string           `yaml:"description"`
	Root        string           `yaml:"root"`
	MachineIDs  []string         `yaml:"machine_ids"`
	Setup       *WorkspaceSetup  `yaml:"setup"`
	Machines    []*MachineConfig `yaml:"-"`
}

type TaskType string

const (
	TaskTypeGeneral    TaskType = "general"
	TaskTypeCodeReview TaskType = "code_review"
)

type CodeReviewConfig struct {
	GitCodeProject string `yaml:"gitcode_project"`
	PRSelector     string `yaml:"pr_selector"`
	ReviewTool     string `yaml:"review_tool"`
	HumanizerSkill string `yaml:"humanizer_skill"`
	Approval       string `yaml:"approval"`
	Publisher      string `yaml:"publisher"`
}

type TemplateConfig struct {
	ID                   string            `yaml:"id"`
	DisplayName          string            `yaml:"display_name"`
	Description          string            `yaml:"description"`
	TaskType             TaskType          `yaml:"task_type"`
	WorkflowPath         string            `yaml:"workflow_path"`
	WorkspaceID          string            `yaml:"workspace_id"`
	Workspace            *WorkspaceConfig  `yaml:"workspace"`
	CodeReview           *CodeReviewConfig `yaml:"code_review"`
	ResolvedWorkflowPath string            `yaml:"-"`
}

type Registry struct {
	Machines       map[string]*MachineConfig
	Repositories   map[string]*RepositoryConfig
	Workspaces     map[string]*WorkspaceProfileConfig
	Templates      map[string]*TemplateConfig
	MachineList    []*MachineConfig
	RepositoryList []*RepositoryConfig
	WorkspaceList  []*WorkspaceProfileConfig
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
		Workspaces:     map[string]*WorkspaceProfileConfig{},
		Templates:      map[string]*TemplateConfig{},
		MachineList:    []*MachineConfig{},
		RepositoryList: []*RepositoryConfig{},
		WorkspaceList:  []*WorkspaceProfileConfig{},
		TemplateList:   []*TemplateConfig{},
	}

	if err := loadConfigDir(filepath.Join(root, "configs/machines"), &registry.MachineList, registry.Machines); err != nil {
		return nil, err
	}
	if err := loadOptionalConfigDir(filepath.Join(root, "configs/repositories"), &registry.RepositoryList, registry.Repositories); err != nil {
		return nil, err
	}
	if err := loadOptionalConfigDir(filepath.Join(root, "configs/workspaces"), &registry.WorkspaceList, registry.Workspaces); err != nil {
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

	for _, workspace := range registry.WorkspaceList {
		if err := bindWorkspaceMachines(fmt.Sprintf("workspace %q", workspace.ID), workspace.workspaceConfig(), registry.Machines); err != nil {
			return nil, err
		}
		workspace.Machines = workspace.Machines[:0]
		for _, machineID := range workspace.MachineIDs {
			workspace.Machines = append(workspace.Machines, registry.Machines[machineID])
		}
	}

	for _, template := range registry.TemplateList {
		if strings.TrimSpace(template.WorkspaceID) != "" {
			workspace := registry.Workspaces[template.WorkspaceID]
			if workspace == nil {
				return nil, fmt.Errorf("template %q references unknown workspace %q", template.ID, template.WorkspaceID)
			}
			template.Workspace = workspace.AsWorkspaceConfig()
		}

		if template.Workspace != nil {
			if err := bindWorkspaceMachines(fmt.Sprintf("template %q workspace", template.ID), template.Workspace, registry.Machines); err != nil {
				return nil, err
			}
		}

		workflowPath, err := resolveWorkflowPath(registryRoot, template.ID, template.WorkflowPath)
		if err != nil {
			return nil, err
		}

		template.ResolvedWorkflowPath = workflowPath
	}

	return registry, nil
}

type configDocument interface {
	GetID() string
	Validate() error
}

func (m *MachineConfig) GetID() string          { return m.ID }
func (r *RepositoryConfig) GetID() string       { return r.ID }
func (w *WorkspaceProfileConfig) GetID() string { return w.ID }
func (t *TemplateConfig) GetID() string         { return t.ID }

func (m *MachineConfig) Validate() error {
	missing := make([]string, 0, 5)
	for _, field := range []requiredField{
		{name: "id", value: m.ID},
		{name: "host", value: m.Host},
		{name: "user", value: m.User},
		{name: "app_server_listen_host", value: m.AppServerListenHost},
		{name: "app_server_service_name", value: m.AppServerServiceName},
		{name: "app_server_install_user", value: m.AppServerInstallUser},
		{name: "app_server_ws_auth_token", value: m.AppServerWSAuthToken},
	} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if m.AppServerListenPort <= 0 {
		missing = append(missing, "app_server_listen_port")
	}
	if len(missing) > 0 {
		subjectID := m.ID
		if strings.TrimSpace(subjectID) == "" {
			subjectID = "<unknown>"
		}
		return fmt.Errorf("machine %q is missing required field(s): %s", subjectID, strings.Join(missing, ", "))
	}
	return nil
}

func (m MachineConfig) AppServerWebSocketURL() string {
	return fmt.Sprintf("ws://%s:%d", strings.TrimSpace(m.Host), m.AppServerListenPort)
}

func (r *RepositoryConfig) Validate() error {
	if err := requireFields("repository", r.ID, []requiredField{
		{name: "id", value: r.ID},
		{name: "remote_repo_url", value: r.RemoteRepoURL},
		{name: "remote_workspace_root", value: r.RemoteWorkspaceRoot},
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
	if err := requireFields("template", t.ID, []requiredField{
		{name: "id", value: t.ID},
		{name: "workflow_path", value: t.WorkflowPath},
	}); err != nil {
		return err
	}
	if strings.TrimSpace(t.WorkspaceID) != "" && t.Workspace != nil {
		return fmt.Errorf("template %q must define only one of %q or %q", t.ID, "workspace_id", "workspace")
	}
	if strings.TrimSpace(t.WorkspaceID) == "" && t.Workspace == nil {
		return fmt.Errorf("template %q is missing required field %q or %q", t.ID, "workspace_id", "workspace")
	}
	if t.Workspace != nil {
		if err := t.Workspace.Validate(t.ID); err != nil {
			return err
		}
	}

	switch t.EffectiveTaskType() {
	case TaskTypeGeneral:
		if t.CodeReview != nil {
			return fmt.Errorf("template %q has code_review config but task_type is %q", t.ID, t.EffectiveTaskType())
		}
	case TaskTypeCodeReview:
		if t.CodeReview == nil {
			return fmt.Errorf("template %q with task_type %q is missing required field %q", t.ID, t.EffectiveTaskType(), "code_review")
		}
		if err := t.CodeReview.Validate(t.ID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("template %q has unsupported task_type %q", t.ID, t.TaskType)
	}
	return nil
}

func (t *TemplateConfig) EffectiveTaskType() TaskType {
	if t == nil || strings.TrimSpace(string(t.TaskType)) == "" {
		return TaskTypeGeneral
	}
	return t.TaskType
}

func (w *WorkspaceProfileConfig) Validate() error {
	if err := requireFields("workspace", w.ID, []requiredField{
		{name: "id", value: w.ID},
	}); err != nil {
		return err
	}
	return w.workspaceConfig().Validate(w.ID)
}

func (w *WorkspaceProfileConfig) workspaceConfig() *WorkspaceConfig {
	if w == nil {
		return nil
	}
	return &WorkspaceConfig{
		Root:       w.Root,
		MachineIDs: w.MachineIDs,
		Setup:      w.Setup,
		Machines:   w.Machines,
	}
}

func (w *WorkspaceProfileConfig) AsWorkspaceConfig() *WorkspaceConfig {
	if w == nil {
		return nil
	}
	return &WorkspaceConfig{
		Root:       w.Root,
		MachineIDs: append([]string(nil), w.MachineIDs...),
		Setup:      cloneWorkspaceSetup(w.Setup),
		Machines:   append([]*MachineConfig(nil), w.Machines...),
	}
}

func (c *CodeReviewConfig) Validate(templateID string) error {
	if err := requireFields("template code_review", templateID, []requiredField{
		{name: "gitcode_project", value: c.GitCodeProject},
		{name: "pr_selector", value: c.PRSelector},
		{name: "review_tool", value: c.ReviewTool},
		{name: "humanizer_skill", value: c.HumanizerSkill},
		{name: "approval", value: c.Approval},
		{name: "publisher", value: c.Publisher},
	}); err != nil {
		return err
	}
	if c.PRSelector != "latest_open" {
		return fmt.Errorf("template code_review %q has unsupported pr_selector %q", templateID, c.PRSelector)
	}
	if c.ReviewTool != "codex_builtin" {
		return fmt.Errorf("template code_review %q has unsupported review_tool %q", templateID, c.ReviewTool)
	}
	if c.Approval != "lark" {
		return fmt.Errorf("template code_review %q has unsupported approval %q", templateID, c.Approval)
	}
	if c.Publisher != "gitcode" {
		return fmt.Errorf("template code_review %q has unsupported publisher %q", templateID, c.Publisher)
	}
	return nil
}

func (w *WorkspaceConfig) Validate(templateID string) error {
	if err := requireFields("template workspace", templateID, []requiredField{
		{name: "root", value: w.Root},
	}); err != nil {
		return err
	}
	if len(w.MachineIDs) == 0 {
		return fmt.Errorf("template workspace %q is missing required field %q", templateID, "machine_ids")
	}
	for _, machineID := range w.MachineIDs {
		if strings.TrimSpace(machineID) == "" {
			return fmt.Errorf("template workspace %q has empty machine_ids entry", templateID)
		}
	}
	if w.Setup == nil {
		return fmt.Errorf("template workspace %q is missing required field %q", templateID, "setup")
	}
	return w.Setup.Validate(templateID)
}

func (s *WorkspaceSetup) Validate(templateID string) error {
	switch s.Type {
	case WorkspaceSetupTypeRepo:
		return requireFields("template workspace setup", templateID, []requiredField{
			{name: "remote_repo_url", value: s.RemoteRepoURL},
			{name: "checkout_branch", value: s.CheckoutBranch},
		})
	case WorkspaceSetupTypeEmpty:
		return nil
	case WorkspaceSetupTypeCustom:
		if len(s.CustomSteps) == 0 {
			return fmt.Errorf("template workspace setup %q requires at least one step for custom setup", templateID)
		}
		for _, step := range s.CustomSteps {
			if strings.TrimSpace(step) == "" {
				return fmt.Errorf("template workspace setup %q has empty custom step", templateID)
			}
		}
		return nil
	default:
		return fmt.Errorf("template workspace setup %q has unsupported type %q", templateID, s.Type)
	}
}

func bindWorkspaceMachines(label string, workspace *WorkspaceConfig, machines map[string]*MachineConfig) error {
	if workspace == nil {
		return nil
	}
	workspace.Machines = workspace.Machines[:0]
	for _, machineID := range workspace.MachineIDs {
		machine := machines[machineID]
		if machine == nil {
			return fmt.Errorf("%s references unknown machine %q", label, machineID)
		}
		workspace.Machines = append(workspace.Machines, machine)
	}
	return nil
}

func cloneWorkspaceSetup(setup *WorkspaceSetup) *WorkspaceSetup {
	if setup == nil {
		return nil
	}
	return &WorkspaceSetup{
		Type:               setup.Type,
		RemoteRepoURL:      setup.RemoteRepoURL,
		CheckoutBranch:     setup.CheckoutBranch,
		PreCloneBootstrap:  append([]string(nil), setup.PreCloneBootstrap...),
		PostCloneBootstrap: append([]string(nil), setup.PostCloneBootstrap...),
		CustomSteps:        append([]string(nil), setup.CustomSteps...),
	}
}

func cloneCodeReviewConfig(config *CodeReviewConfig) *CodeReviewConfig {
	if config == nil {
		return nil
	}
	clone := *config
	return &clone
}

func workspaceFromRepository(repo *RepositoryConfig) *WorkspaceConfig {
	if repo == nil {
		return nil
	}
	return &WorkspaceConfig{
		Root:       repo.RemoteWorkspaceRoot,
		MachineIDs: append([]string(nil), repo.MachineIDs...),
		Machines:   append([]*MachineConfig(nil), repo.Machines...),
		Setup: &WorkspaceSetup{
			Type:               WorkspaceSetupTypeRepo,
			RemoteRepoURL:      repo.RemoteRepoURL,
			CheckoutBranch:     repo.DefaultBranch,
			PreCloneBootstrap:  append([]string(nil), repo.PreCloneBootstrap...),
			PostCloneBootstrap: append([]string(nil), repo.PostCloneBootstrap...),
		},
	}
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

func loadOptionalConfigDir[T configDocument](dir string, ordered *[]T, destination map[string]T) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
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
