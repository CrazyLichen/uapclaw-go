package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInfo 工具信息。
// 对齐 Python: list_available_tools() 返回的 tools 列表项
type ToolInfo struct {
	// Name 显示名称
	Name string `json:"name"`
	// InternalName 内部名称
	InternalName string `json:"internal_name"`
	// Description 描述
	Description string `json:"description"`
	// Group 分组
	Group string `json:"group"`
}

// AvailableToolsResult 可用工具查询结果。
// 对齐 Python: AgentConfigService.list_available_tools() 返回值
type AvailableToolsResult struct {
	// Tools 工具信息列表
	Tools []ToolInfo `json:"tools"`
	// Groups 分组名称列表
	Groups []string `json:"groups"`
	// DisallowedForSubagents 子 agent 禁止使用的工具列表
	// 对齐 Python: DISALLOWED_FOR_SUBAGENTS
	DisallowedForSubagents []string `json:"disallowed_for_subagents"`
}

// AgentConfigService Agent 配置管理服务。
// 对齐 Python: AgentConfigService
//
// 管理内置和自定义 agent 定义的 CRUD 操作。
// 支持四个来源的 agent 定义：内置、用户级、项目级、本地级。
// 同名 agent 按 project > user > local > builtin 优先级覆盖。
type AgentConfigService struct {
	// workspaceDir 工作空间目录
	workspaceDir string
}

// CreateAgentParams 创建 Agent 请求参数。
// 对齐 Python: CreateAgentParams dataclass
type CreateAgentParams struct {
	// Name 名称
	Name string `json:"name" yaml:"name"`
	// Description 描述
	Description string `json:"description" yaml:"description"`
	// Prompt 系统提示词
	Prompt string `json:"prompt" yaml:"-"`
	// Location 存储位置（user/project/local）
	Location string `json:"location" yaml:"-"`
	// Model 模型名称
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Tools 允许的工具列表
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	// Color 颜色标识
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode string `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope string `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools,omitempty" yaml:"disallowed_tools,omitempty"`
	// WhenToUse 调度描述
	WhenToUse string `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
	// Skills 预加载 skill
	Skills []string `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// UpdateAgentParams 更新 Agent 请求参数（指针字段，nil 表示不修改）。
// 对齐 Python: UpdateAgentParams dataclass（所有字段可选，None 表示不修改）
type UpdateAgentParams struct {
	// Description 描述（nil=不修改）
	Description *string `json:"description,omitempty"`
	// WhenToUse 调度描述
	WhenToUse *string `json:"when_to_use,omitempty"`
	// Prompt 系统提示词
	Prompt *string `json:"prompt,omitempty"`
	// Model 模型名称
	Model *string `json:"model,omitempty"`
	// Tools 允许的工具列表（nil=不修改）
	Tools []string `json:"tools,omitempty"`
	// Color 颜色标识
	Color *string `json:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode *string `json:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope *string `json:"memory_scope,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty"`
	// Skills 预加载 skill
	Skills []string `json:"skills,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// （logComponent 已在 session_manager.go 中声明）

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// agentNamePattern Agent 名称校验正则
	// 对齐 Python: re.match(r'^[a-zA-Z0-9_-]{3,50}$', name)
	agentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,50}$`)

	// sourceSortOrder 来源排序优先级（数值越小优先级越低）
	// 对齐 Python: _SOURCE_SORT_ORDER
	sourceSortOrder = map[string]int{
		types.AgentSourceBuiltin: 0,
		types.AgentSourceLocal:   1,
		types.AgentSourceUser:    2,
		types.AgentSourceProject: 3,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentConfigService 创建 AgentConfigService 实例。
// 对齐 Python: AgentConfigService.__init__(workspace_dir)
func NewAgentConfigService(workspaceDir string) *AgentConfigService {
	return &AgentConfigService{workspaceDir: workspaceDir}
}

// ListAgents 列出所有 agent（内置 + 自定义），按优先级合并。
// 对齐 Python: AgentConfigService.list_agents()
//
// 加载顺序决定优先级：后加载的覆盖先加载的，因此
// project > user > local > builtin。被覆盖的同名 agent 标记 shadowed_by。
// 同时从 config.yaml 的 react.subagents 读取 enabled 状态。
func (s *AgentConfigService) ListAgents() []*types.AgentDefinition {
	// 步骤 1: 按 builtin → local → user → project 顺序加载
	// 对齐 Python: sources = [(BUILTIN_AGENTS, "builtin"), (...)]
	sources := []struct {
		agents []*types.AgentDefinition
		source string
	}{
		{copyBuiltinAgents(), types.AgentSourceBuiltin},
		{s.loadFromDir(s.localAgentsDir(), types.AgentSourceLocal), types.AgentSourceLocal},
		{s.loadFromDir(s.userAgentsDir(), types.AgentSourceUser), types.AgentSourceUser},
		{s.loadFromDir(s.projectAgentsDir(), types.AgentSourceProject), types.AgentSourceProject},
	}

	// 步骤 2: 读取 config.yaml 的 react.subagents enabled 状态
	// 对齐 Python: subagent_states = {}; try: config = get_config(); ...
	subagentStates := s.loadSubagentStates()

	// 步骤 3: 按名字分组，保持所有来源的 agent（包括被 shadow 的）
	// 对齐 Python: grouped = {}; for agents, _ in sources: for agent in agents: ...
	grouped := make(map[string][]*types.AgentDefinition)
	for _, src := range sources {
		for _, agent := range src.agents {
			grouped[agent.Name] = append(grouped[agent.Name], agent)
		}
	}

	// 步骤 4: 每组的最后一个为 active（最高优先级），之前的标记 shadowed_by
	// 对齐 Python: for name, group in grouped.items(): active = group[-1]; ...
	var result []*types.AgentDefinition
	for _, group := range grouped {
		active := group[len(group)-1]
		active.ShadowedBy = ""
		for _, agent := range group[:len(group)-1] {
			agent.ShadowedBy = active.Source
			result = append(result, agent)
		}
		result = append(result, active)
	}

	// 步骤 5: 注入 enabled 状态
	// 对齐 Python: for agent in result: if agent.name in subagent_states: agent.enabled = ...
	for _, agent := range result {
		if enabled, ok := subagentStates[agent.Name]; ok {
			agent.Enabled = &enabled
		}
	}

	// 步骤 6: 按 source 排序
	// 对齐 Python: return sorted(result, key=_source_sort_key)
	sort.Slice(result, func(i, j int) bool {
		return sourceSortOrder[result[i].Source] < sourceSortOrder[result[j].Source]
	})

	return result
}

// GetAgent 获取单个 agent 完整定义（含 system prompt 正文）。
// 对齐 Python: AgentConfigService.get_agent(name)
//
// 返回活跃版本（未被 shadow 的），与 ListAgents 保持一致的优先级语义。
func (s *AgentConfigService) GetAgent(name string) *types.AgentDefinition {
	// 对齐 Python: agents = self.list_agents(); for a in agents: if a.name == name and a.shadowed_by is None: return a
	for _, agent := range s.ListAgents() {
		if agent.Name == name && agent.ShadowedBy == "" {
			return agent
		}
	}
	return nil
}

// ListCustomAgents 列出自定义 agent（非 builtin）。
// 通过 types.AgentDefinition 直接返回，无需中间转换类型。
func (s *AgentConfigService) ListCustomAgents() []*types.AgentDefinition {
	agents := s.ListAgents()
	var result []*types.AgentDefinition
	for _, a := range agents {
		if a.Source == types.AgentSourceBuiltin {
			continue
		}
		result = append(result, a)
	}
	return result
}

// CreateAgent 创建新的自定义 agent，写入 markdown 文件。
// 对齐 Python: AgentConfigService.create_agent(params)
func (s *AgentConfigService) CreateAgent(params *CreateAgentParams) (*types.AgentDefinition, error) {
	// 步骤 1: 名称校验
	// 对齐 Python: name = params.name.strip(); if not re.match(r'^[a-zA-Z0-9_-]{3,50}$', name): raise ValueError(...)
	name := strings.TrimSpace(params.Name)
	if !agentNamePattern.MatchString(name) {
		return nil, fmt.Errorf("Agent 名称格式无效: '%s'。要求 3-50 字符，仅允许字母、数字、连字符、下划线", name)
	}

	// 步骤 2: 检查是否覆盖内置 agent
	// 对齐 Python: existing = self.get_agent(params.name); if existing is not None and existing.source == "builtin": raise ValueError(...)
	existing := s.GetAgent(name)
	if existing != nil && existing.Source == types.AgentSourceBuiltin {
		return nil, fmt.Errorf("不能覆盖内置 agent: %s", name)
	}

	// 步骤 3: 确定目标目录并创建
	// 对齐 Python: target_dir = self._resolve_location_dir(params.location); target_dir.mkdir(parents=True, exist_ok=True)
	targetDir, err := s.resolveLocationDir(params.Location)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	filePath := filepath.Join(targetDir, name+".md")

	// 步骤 4: 构造 AgentDefinition（提前到写文件之前）
	// 对齐 Python: return AgentDefinition(name=..., tools=params.tools or ["*"], ...)
	tools := params.Tools
	if len(tools) == 0 {
		tools = []string{"*"}
	}
	def := &types.AgentDefinition{
		Name:            name,
		Description:     params.Description,
		Prompt:          params.Prompt,
		Source:          params.Location,
		FilePath:        filePath,
		Model:           params.Model,
		Tools:           tools,
		Color:           params.Color,
		PermissionMode:  params.PermissionMode,
		MemoryScope:     params.MemoryScope,
		DisallowedTools: params.DisallowedTools,
		WhenToUse:       params.WhenToUse,
		MaxIterations:   params.MaxIterations,
		Skills:          params.Skills,
	}

	// 步骤 5: 生成文件内容并写入
	// 对齐 Python: content = _format_agent_file(agent); file_path.write_text(content, encoding="utf-8")
	content := formatAgentFile(def)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	// 步骤 6: 记录日志
	// 对齐 Python: logger.info("Created agent '%s' at %s", params.name, file_path)
	logger.Info(logComponent).
		Str("agent_name", name).
		Str("file_path", filePath).
		Msg("Created agent")

	return def, nil
}

// UpdateAgent 更新自定义 agent 定义，覆盖写入文件。
// 对齐 Python: AgentConfigService.update_agent(name, params)
func (s *AgentConfigService) UpdateAgent(name string, params *UpdateAgentParams) (*types.AgentDefinition, error) {
	// 步骤 1: 查找 agent
	// 对齐 Python: agent = self.get_agent(name)
	agent := s.GetAgent(name)
	if agent == nil {
		return nil, fmt.Errorf("Agent 不存在: %s", name)
	}
	if agent.Source == types.AgentSourceBuiltin {
		return nil, fmt.Errorf("不能修改内置 agent: %s", name)
	}
	if agent.FilePath == "" {
		return nil, fmt.Errorf("Agent 无文件路径: %s", name)
	}

	// 步骤 2: 应用更新参数
	// 对齐 Python: _apply_update_params(agent, params)
	applyUpdateParams(agent, params)

	// 步骤 3: 生成文件内容并覆盖写入
	// 对齐 Python: content = _format_agent_file(agent); Path(agent.file_path).write_text(content, encoding="utf-8")
	content := formatAgentFile(agent)
	if err := os.WriteFile(agent.FilePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	// 步骤 4: 记录日志
	// 对齐 Python: logger.info("Updated agent '%s' at %s", name, agent.file_path)
	logger.Info(logComponent).
		Str("agent_name", name).
		Str("file_path", agent.FilePath).
		Msg("Updated agent")

	return agent, nil
}

// DeleteAgent 删除自定义 agent 定义文件。
// 对齐 Python: AgentConfigService.delete_agent(name)
func (s *AgentConfigService) DeleteAgent(name string) (bool, error) {
	// 步骤 1: 查找 agent
	// 对齐 Python: agent = self.get_agent(name)
	agent := s.GetAgent(name)
	if agent == nil {
		return false, nil
	}
	if agent.Source == types.AgentSourceBuiltin {
		return false, fmt.Errorf("不能删除内置 agent: %s", name)
	}

	// 步骤 2: 删除文件
	// 对齐 Python: if agent.file_path: p = Path(agent.file_path); if p.exists(): p.unlink()
	if agent.FilePath != "" {
		if err := os.Remove(agent.FilePath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("删除文件失败: %w", err)
		}
		// 步骤 3: 记录日志
		// 对齐 Python: logger.info("Deleted agent '%s' at %s", name, agent.file_path)
		logger.Info(logComponent).
			Str("agent_name", name).
			Str("file_path", agent.FilePath).
			Msg("Deleted agent")
	}
	return true, nil
}

// ListAvailableTools 返回可用工具及其分组信息。
// 对齐 Python: AgentConfigService.list_available_tools()
func (s *AgentConfigService) ListAvailableTools() *AvailableToolsResult {
	// ⤵️ 10.3.7-11: 当前工具列表和分组硬编码，
	// 等 code_agent_rail 实现后从 TOOL_GROUPS / _TOOL_DISPLAY_NAMES 动态构建。
	// 对齐 Python: TOOL_GROUPS = {"核心": [...], "搜索": [...], "代码智能": [...], "高级": [...], "可视化": [...]}

	// 步骤 1: 构建工具描述列表
	// 对齐 Python: tools.append({"name": display_name, "internal_name": internal_name, ...})
	tools := []ToolInfo{
		{Name: "Read", InternalName: "Read", Description: "读取文件内容", Group: "核心"},
		{Name: "Write", InternalName: "Write", Description: "写入文件", Group: "核心"},
		{Name: "Edit", InternalName: "Edit", Description: "编辑文件（精准替换）", Group: "核心"},
		{Name: "Bash", InternalName: "Bash", Description: "执行 shell 命令", Group: "核心"},
		{Name: "LS", InternalName: "LS", Description: "列出目录内容", Group: "核心"},
		{Name: "Grep", InternalName: "Grep", Description: "搜索文件内容", Group: "搜索"},
		{Name: "Glob", InternalName: "Glob", Description: "按模式搜索文件名", Group: "搜索"},
		{Name: "WebSearch", InternalName: "WebSearch", Description: "网络搜索", Group: "搜索"},
		{Name: "WebFetch", InternalName: "WebFetch", Description: "获取网页内容", Group: "搜索"},
		{Name: "LSP", InternalName: "LSP", Description: "代码智能（定义跳转、引用查找）", Group: "代码智能"},
		{Name: "TodoWrite", InternalName: "TodoWrite", Description: "创建/更新任务列表", Group: "代码智能"},
		{Name: "TodoList", InternalName: "TodoList", Description: "查看任务列表", Group: "代码智能"},
		{Name: "MemorySearch", InternalName: "MemorySearch", Description: "搜索记忆", Group: "高级"},
		{Name: "MemoryGet", InternalName: "MemoryGet", Description: "获取记忆条目", Group: "高级"},
		{Name: "WriteMemory", InternalName: "WriteMemory", Description: "写入记忆", Group: "高级"},
		{Name: "EditMemory", InternalName: "EditMemory", Description: "编辑记忆", Group: "高级"},
		{Name: "CronCreate", InternalName: "CronCreate", Description: "创建定时任务", Group: "高级"},
		{Name: "CronList", InternalName: "CronList", Description: "列出定时任务", Group: "高级"},
		{Name: "CronDelete", InternalName: "CronDelete", Description: "删除定时任务", Group: "高级"},
		{Name: "SkillTool", InternalName: "SkillTool", Description: "调用 Skill", Group: "高级"},
		{Name: "VisionQA", InternalName: "VisionQA", Description: "视觉问答", Group: "可视化"},
		{Name: "ImageOCR", InternalName: "ImageOCR", Description: "图片文字识别", Group: "可视化"},
		{Name: "AudioTranscribe", InternalName: "AudioTranscribe", Description: "音频转录", Group: "可视化"},
	}

	// 步骤 2: 构建分组列表
	// 对齐 Python: groups = list(TOOL_GROUPS.keys())
	groups := []string{"核心", "搜索", "代码智能", "高级", "可视化"}

	// 步骤 3: 子 agent 禁用工具列表
	// ⤵️ 10.3.7-11: 对齐 Python: DISALLOWED_FOR_SUBAGENTS
	// 等 code_agent_rail 实现后从 DISALLOWED_FOR_SUBAGENTS 动态获取
	disallowedForSubagents := []string{
		"Agent", "task", "enter_plan_mode", "exit_plan_mode",
		"ask_user_question", "task_stop", "switch_mode",
	}

	return &AvailableToolsResult{
		Tools:                  tools,
		Groups:                 groups,
		DisallowedForSubagents: disallowedForSubagents,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// userAgentsDir 返回用户级 agent 目录：~/.uapclaw/agents/
// 对齐 Python: _get_user_agents_dir() → get_user_workspace_dir() / "agents"
func (s *AgentConfigService) userAgentsDir() string {
	return filepath.Join(pathutil.UserHomeDir(), ".uapclaw", "agents")
}

// projectAgentsDir 返回项目级 agent 目录：<workspace>/.uapclaw/agents/
// 对齐 Python: _get_project_agents_dir() → self._workspace_dir / ".jiuwenswarm" / "agents"
func (s *AgentConfigService) projectAgentsDir() string {
	return filepath.Join(s.workspaceDir, ".uapclaw", "agents")
}

// localAgentsDir 返回本地级 agent 目录：<workspace>/.uapclaw/agents-local/
// 对齐 Python: _get_local_agents_dir() → self._workspace_dir / ".jiuwenswarm" / "agents-local"
func (s *AgentConfigService) localAgentsDir() string {
	return filepath.Join(s.workspaceDir, ".uapclaw", "agents-local")
}

// resolveLocationDir 根据位置参数返回对应目录。
// 对齐 Python: _resolve_location_dir(location) — 无效 location 抛 ValueError
func (s *AgentConfigService) resolveLocationDir(location string) (string, error) {
	switch location {
	case types.AgentSourceUser:
		return s.userAgentsDir(), nil
	case types.AgentSourceProject:
		return s.projectAgentsDir(), nil
	case types.AgentSourceLocal:
		return s.localAgentsDir(), nil
	default:
		return "", fmt.Errorf("无效的 location: %s，有效值: user, project, local", location)
	}
}

// loadFromDir 从目录加载所有 .md agent 定义文件。
// 对齐 Python: _load_from_dir(dir_path, source)
func (s *AgentConfigService) loadFromDir(dirPath string, source string) []*types.AgentDefinition {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}
	var agents []*types.AgentDefinition
	// 对齐 Python: for md_file in sorted(dir_path.glob("*.md"))
	// Go 的 os.ReadDir 已按文件名排序
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		filePath := filepath.Join(dirPath, entry.Name())
		agent, err := parseAgentFile(filePath, source)
		if err != nil {
			// 对齐 Python: except Exception: logger.warning("Failed to parse agent file: %s", md_file, exc_info=True)
			logger.Warn(logComponent).
				Str("file_path", filePath).
				Err(err).
				Msg("解析 agent 文件失败")
			continue
		}
		if agent != nil {
			agents = append(agents, agent)
		}
	}
	return agents
}

// loadSubagentStates 从 config.yaml 的 react.subagents 读取 enabled 状态。
// 对齐 Python: list_agents() 中读取 subagent_states 的逻辑
func (s *AgentConfigService) loadSubagentStates() map[string]bool {
	states := make(map[string]bool)
	cfg, err := config.New("")
	if err != nil {
		// 对齐 Python: except Exception as e: logger.debug("Failed to load subagent states from config: %s", e)
		logger.Debug(logComponent).Err(err).Msg("创建配置管理器失败")
		return states
	}
	data, err := cfg.Load()
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("加载配置失败")
		return states
	}
	react, _ := data["react"].(map[string]any)
	if react == nil {
		return states
	}
	subagentsCfg, _ := react["subagents"].(map[string]any)
	if subagentsCfg == nil {
		return states
	}
	// 对齐 Python: for name, cfg in subagents_cfg.items(): if isinstance(cfg, dict) and "enabled" in cfg: states[name] = bool(cfg["enabled"])
	for name, cfg := range subagentsCfg {
		if m, ok := cfg.(map[string]any); ok {
			if enabled, ok := m["enabled"]; ok {
				states[name] = boolVal(enabled)
			}
		}
	}
	return states
}

// copyBuiltinAgents 深拷贝内置 agent 列表（避免修改原始定义）。
// 对齐 Python: list(BUILTIN_AGENTS)
func copyBuiltinAgents() []*types.AgentDefinition {
	result := make([]*types.AgentDefinition, len(types.BuiltinAgents))
	for i, a := range types.BuiltinAgents {
		cp := *a
		tools := make([]string, len(a.Tools))
		copy(tools, a.Tools)
		cp.Tools = tools
		result[i] = &cp
	}
	return result
}

// parseAgentFile 解析 YAML frontmatter + Markdown body 格式的 agent 文件。
// 对齐 Python: _parse_agent_file(file_path, source)
func parseAgentFile(filePath string, source string) (*types.AgentDefinition, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	text := string(content)

	// 步骤 1: 检查 frontmatter 开头
	// 对齐 Python: if not content.startswith("---"): return None
	if !strings.HasPrefix(text, "---") {
		return nil, nil
	}

	// 步骤 2: 分割 frontmatter 和 body
	// 对齐 Python: parts = content.split("---", 2); if len(parts) < 3: return None
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, nil
	}
	frontmatterStr := strings.TrimSpace(parts[1])
	prompt := strings.TrimSpace(parts[2])

	// 步骤 3: 解析 YAML frontmatter
	// 对齐 Python: frontmatter = yaml.safe_load(parts[1])
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterStr), &frontmatter); err != nil {
		return nil, fmt.Errorf("解析 frontmatter 失败: %w", err)
	}
	if frontmatter == nil {
		return nil, nil
	}

	// 步骤 4: 校验 name 字段
	// 对齐 Python: if not frontmatter or "name" not in frontmatter: return None
	name, _ := frontmatter["name"].(string)
	if name == "" {
		return nil, nil
	}

	// 步骤 5: 提取各字段
	// 对齐 Python: return AgentDefinition(name=..., description=frontmatter.get("description", ""), ...)
	description, _ := frontmatter["description"].(string)
	model, _ := frontmatter["model"].(string)
	whenToUse, _ := frontmatter["when_to_use"].(string)
	color, _ := frontmatter["color"].(string)
	permissionMode, _ := frontmatter["permission_mode"].(string)
	memoryScope, _ := frontmatter["memory_scope"].(string)

	var tools []string
	if t, ok := frontmatter["tools"]; ok {
		tools = toStringSlice(t)
	}

	var disallowedTools []string
	if t, ok := frontmatter["disallowed_tools"]; ok {
		disallowedTools = toStringSlice(t)
	}

	var skills []string
	if t, ok := frontmatter["skills"]; ok {
		skills = toStringSlice(t)
	}

	var maxIterations *int
	if mi, ok := frontmatter["max_iterations"]; ok {
		switch v := mi.(type) {
		case int:
			maxIterations = &v
		case float64:
			iv := int(v)
			maxIterations = &iv
		}
	}

	def := &types.AgentDefinition{
		Name:            name,
		Description:     description,
		Prompt:          prompt,
		Source:          source,
		FilePath:        filePath,
		Model:           model,
		Tools:           tools,
		DisallowedTools: disallowedTools,
		Color:           color,
		PermissionMode:  permissionMode,
		MemoryScope:     memoryScope,
		WhenToUse:       whenToUse,
		MaxIterations:   maxIterations,
		Skills:          skills,
	}
	// 对齐 Python: tools=frontmatter.get("tools", ["*"])
	if len(def.Tools) == 0 {
		def.Tools = []string{"*"}
	}
	return def, nil
}

// formatAgentFile 生成 YAML frontmatter + Markdown body 格式的 agent 文件内容。
// 对齐 Python: _format_agent_file(agent) — 只接受 *AgentDefinition
func formatAgentFile(def *types.AgentDefinition) string {
	frontmatter := make(map[string]any)
	frontmatter["name"] = def.Name
	frontmatter["description"] = def.Description
	if def.WhenToUse != "" {
		frontmatter["when_to_use"] = def.WhenToUse
	}
	if def.Model != "" {
		frontmatter["model"] = def.Model
	}
	// 对齐 Python: if agent.tools and agent.tools != ["*"]: frontmatter["tools"] = agent.tools
	if len(def.Tools) > 0 && !(len(def.Tools) == 1 && def.Tools[0] == "*") {
		frontmatter["tools"] = def.Tools
	}
	if def.Color != "" {
		frontmatter["color"] = def.Color
	}
	if def.PermissionMode != "" {
		frontmatter["permission_mode"] = def.PermissionMode
	}
	if def.MemoryScope != "" {
		frontmatter["memory_scope"] = def.MemoryScope
	}
	if len(def.DisallowedTools) > 0 {
		frontmatter["disallowed_tools"] = def.DisallowedTools
	}
	if def.MaxIterations != nil {
		frontmatter["max_iterations"] = *def.MaxIterations
	}
	if len(def.Skills) > 0 {
		frontmatter["skills"] = def.Skills
	}

	// 对齐 Python: yaml_str = yaml.dump(frontmatter, allow_unicode=True, default_flow_style=False).strip()
	yamlBytes, _ := yaml.Marshal(frontmatter)
	// 对齐 Python: return f"---\n{yaml_str}\n---\n\n{prompt}\n"
	return fmt.Sprintf("---\n%s---\n\n%s\n", string(yamlBytes), def.Prompt)
}

// applyUpdateParams 将 UpdateAgentParams 的非 nil 字段应用到 AgentDefinition。
// 对齐 Python: _apply_update_params(agent, params)
func applyUpdateParams(agent *types.AgentDefinition, params *UpdateAgentParams) {
	// 对齐 Python: if params.description is not None: agent.description = params.description
	if params.Description != nil {
		agent.Description = *params.Description
	}
	if params.WhenToUse != nil {
		agent.WhenToUse = *params.WhenToUse
	}
	if params.Prompt != nil {
		agent.Prompt = *params.Prompt
	}
	if params.Model != nil {
		agent.Model = *params.Model
	}
	if params.Tools != nil {
		agent.Tools = params.Tools
	}
	if params.Color != nil {
		agent.Color = *params.Color
	}
	if params.PermissionMode != nil {
		agent.PermissionMode = *params.PermissionMode
	}
	if params.MemoryScope != nil {
		agent.MemoryScope = *params.MemoryScope
	}
	if params.DisallowedTools != nil {
		agent.DisallowedTools = params.DisallowedTools
	}
	if params.MaxIterations != nil {
		agent.MaxIterations = params.MaxIterations
	}
	if params.Skills != nil {
		agent.Skills = params.Skills
	}
}

// boolVal 将 any 转为 bool。
func boolVal(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.ToLower(b) == "true"
	default:
		return false
	}
}

// toStringSlice 将 []any 转为 []string。
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
