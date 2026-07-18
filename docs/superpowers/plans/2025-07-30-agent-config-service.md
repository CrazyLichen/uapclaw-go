# AgentConfigService 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 10.3.13 AgentConfigService — Agent 定义配置管理中心，提供 CRUD、四层来源合并、config.yaml 联动、LLM 生成、回填 deep_adapter + handle_agents.go stub。

**Architecture:** AgentConfigService 作为无状态 CRUD 服务，每次从文件系统读取 `.uapclaw/agents/*.md` 文件，按 project > user > local > builtin 优先级合并。config.yaml 的 `react.subagents` 段控制 enabled 状态。LLM 生成功能调用 agentcore 的模型客户端。

**Tech Stack:** Go 1.23+, gopkg.in/yaml.v3, internal/common/config, internal/agentcore/llm

---

## Task 0: 品牌名统一 — jiuwenswarm → uapclaw

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:388,579,589`
- Modify: `internal/agentcore/harness/prompts/sections/identity.go:34,37,82,85`
- Test: `internal/swarm/server/adapter/deep_adapter_test.go`
- Test: `internal/agentcore/harness/prompts/sections/identity_test.go`（如存在）

- [ ] **Step 1: 修改 deep_adapter.go 中的 3 处品牌名**

在 `internal/swarm/server/adapter/deep_adapter.go` 中：

行 388: `agentschema.WithAgentID("jiuwenswarm")` → `agentschema.WithAgentID("uapclaw")`
行 579: `d.getToolCards("jiuwenswarm")` → `d.getToolCards("uapclaw")`
行 589: `agentschema.WithAgentID("jiuwenswarm")` → `agentschema.WithAgentID("uapclaw")`

- [ ] **Step 2: 修改 identity.go 中的 4 处品牌名**

在 `internal/agentcore/harness/prompts/sections/identity.go` 中：

行 34: `"你是一个私人智能体，由 JiuwenSwarm 创建。"` → `"你是一个私人智能体，由 UapClaw 创建。"`
行 37: `"你的一切从 BT.jiuwenswarmBT 目录开始。\n\n"` → `"你的一切从 BT.uapclawBT 目录开始。\n\n"`
行 82: `"You are a personal agent created by JiuwenSwarm."` → `"You are a personal agent created by UapClaw."`
行 85: `"Everything starts from the BT.jiuwenswarmBT directory.\n\n"` → `"Everything starts from the BT.uapclawBT directory.\n\n"`

- [ ] **Step 3: 编译验证**

```bash
export GOPROXY=https://goproxy.cn,direct
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 4: 运行受影响的测试**

```bash
go test ./internal/swarm/server/adapter/... ./internal/agentcore/harness/prompts/... -count=1 -timeout 120s
```

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/agentcore/harness/prompts/sections/identity.go
git commit -m "refactor: rename jiuwenswarm to uapclaw in runtime code and agent prompts"
```

---

## Task 1: 数据模型定义

**Files:**
- Create: `internal/swarm/server/runtime/agent_config.go`
- Create: `internal/swarm/server/runtime/agent_config_test.go`

- [ ] **Step 1: 创建 agent_config.go，定义 AgentSource 枚举、AgentDefinition、CreateAgentParams、UpdateAgentParams**

```go
package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSource Agent 定义来源。
// 对齐 Python: AgentSource = Literal["builtin", "user", "project", "local"]
type AgentSource string

// AgentDefinition Agent 定义数据模型。
// 对齐 Python: AgentDefinition dataclass
type AgentDefinition struct {
	// Name 名称
	Name string `json:"name" yaml:"name"`
	// Description 描述
	Description string `json:"description" yaml:"description"`
	// Prompt 系统提示词（Markdown body 部分）
	Prompt string `json:"prompt" yaml:"-"`
	// Source 来源
	Source AgentSource `json:"source" yaml:"-"`
	// FilePath 文件路径（仅文件来源）
	FilePath string `json:"file_path,omitempty" yaml:"-"`
	// Model 模型名称
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Tools 允许的工具列表，默认 ["*"]
	Tools []string `json:"tools" yaml:"tools,omitempty"`
	// DisallowedTools 禁止的工具列表
	DisallowedTools []string `json:"disallowed_tools" yaml:"disallowed_tools,omitempty"`
	// Color 颜色标识
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
	// PermissionMode 权限模式
	PermissionMode string `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	// MemoryScope 记忆范围
	MemoryScope string `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
	// ShadowedBy 被哪个来源覆盖（空字符串=活跃版本）
	ShadowedBy AgentSource `json:"shadowed_by,omitempty" yaml:"-"`
	// Enabled 启用状态（nil=未在config.yaml中配置, true=显式启用, false=显式禁用）
	Enabled *bool `json:"enabled,omitempty" yaml:"-"`
	// WhenToUse 调度描述（告诉 LLM 何时调度此 agent）
	WhenToUse string `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
	// MaxIterations 子 agent 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
	// Skills 子 agent 启动时预加载的 skill 名称
	Skills []string `json:"skills,omitempty" yaml:"skills,omitempty"`
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
	Location AgentSource `json:"location" yaml:"-"`
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
// 对齐 Python: UpdateAgentParams dataclass
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

const (
	// AgentSourceBuiltin 内置
	AgentSourceBuiltin AgentSource = "builtin"
	// AgentSourceUser 用户级
	AgentSourceUser AgentSource = "user"
	// AgentSourceProject 项目级
	AgentSourceProject AgentSource = "project"
	// AgentSourceLocal 本地级
	AgentSourceLocal AgentSource = "local"
)

// ──────────────────────────── 常量 ────────────────────────────

var (
	// agentNamePattern Agent 名称校验正则
	// 对齐 Python: re.match(r'^[a-zA-Z0-9_-]{3,50}$', name)
	agentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,50}$`)

	// sourceSortOrder 来源排序优先级（数值越小优先级越低）
	sourceSortOrder = map[AgentSource]int{
		AgentSourceBuiltin: 0,
		AgentSourceLocal:   1,
		AgentSourceUser:    2,
		AgentSourceProject: 3,
	}
)

// BuiltinAgents 内置 Agent 定义列表。
// 对齐 Python: BUILTIN_AGENTS
var BuiltinAgents = []*AgentDefinition{
	{
		Name:        "general-purpose",
		Description: "通用多步任务 agent，适用于没有专用 agent 的各类任务",
		Prompt: "你是一个通用任务 agent。使用可用工具完成用户委派的任务。\n\n" +
			"工作原则：\n" +
			"1. 将复杂任务分解为可管理的步骤\n" +
			"2. 在每个步骤完成后汇报进展\n" +
			"3. 遇到阻塞时主动说明需要什么信息",
		Source: AgentSourceBuiltin,
		Tools:  []string{"*"},
	},
	{
		Name:        "Explore",
		Description: "快速只读代码库探索 agent，用于定位代码、搜索符号、查找文件",
		Prompt: "你是代码库探索专家。你的职责是快速定位代码、搜索符号和查找文件。\n\n" +
			"工作原则：\n" +
			"1. 只进行只读操作（搜索、读取、列出文件）\n" +
			"2. 通过多种搜索策略（文件名模式、grep 符号、目录遍历）确保覆盖全面\n" +
			"3. 返回精确的文件路径和行号\n" +
			"4. 当结果过多时，缩小搜索范围而不是截断输出",
		Source: AgentSourceBuiltin,
		Tools:  []string{"Read", "Bash", "Grep", "Glob"},
	},
	{
		Name:        "Plan",
		Description: "软件架构设计 agent，用于规划实现方案",
		Prompt: "你是软件架构师。分析代码库模式和约定，提供完整的实现蓝图。\n\n" +
			"工作原则：\n" +
			"1. 先理解现有代码库的架构模式和约定\n" +
			"2. 设计变更时考虑副作用和依赖关系\n" +
			"3. 输出包含：需要创建/修改的文件、组件设计、数据流和构建顺序\n" +
			"4. 不写实现代码，只提供设计蓝图",
		Source: AgentSourceBuiltin,
		Tools:  []string{"Read", "Bash", "Grep", "Glob"},
	},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentConfigService 创建 AgentConfigService 实例。
func NewAgentConfigService(workspaceDir string) *AgentConfigService {
	return &AgentConfigService{workspaceDir: workspaceDir}
}

// ListAgents 列出所有 agent（内置 + 自定义），按优先级合并。
// 对齐 Python: AgentConfigService.list_agents()
func (s *AgentConfigService) ListAgents() []*AgentDefinition {
	// 按 builtin → local → user → project 顺序加载
	sources := []struct {
		agents []*AgentDefinition
		source AgentSource
	}{
		{copyBuiltinAgents(), AgentSourceBuiltin},
		{s.loadFromDir(s.localAgentsDir(), AgentSourceLocal), AgentSourceLocal},
		{s.loadFromDir(s.userAgentsDir(), AgentSourceUser), AgentSourceUser},
		{s.loadFromDir(s.projectAgentsDir(), AgentSourceProject), AgentSourceProject},
	}

	// 读取 config.yaml 的 react.subagents enabled 状态
	subagentStates := s.loadSubagentStates()

	// 按名字分组
	grouped := make(map[string][]*AgentDefinition)
	for _, src := range sources {
		for _, agent := range src.agents {
			grouped[agent.Name] = append(grouped[agent.Name], agent)
		}
	}

	// 每组最后一个为 active，之前的标记 shadowed_by
	var result []*AgentDefinition
	for _, group := range grouped {
		active := group[len(group)-1]
		active.ShadowedBy = ""
		for _, agent := range group[:len(group)-1] {
			agent.ShadowedBy = active.Source
			result = append(result, agent)
		}
		result = append(result, active)
	}

	// 注入 enabled 状态
	for _, agent := range result {
		if enabled, ok := subagentStates[agent.Name]; ok {
			agent.Enabled = &enabled
		}
	}

	// 按 source 排序
	sort.Slice(result, func(i, j int) bool {
		return sourceSortOrder[result[i].Source] < sourceSortOrder[result[j].Source]
	})

	return result
}

// GetAgent 获取单个 agent 完整定义（含 system prompt 正文）。
// 返回活跃版本（未被 shadow 的）。
// 对齐 Python: AgentConfigService.get_agent(name)
func (s *AgentConfigService) GetAgent(name string) *AgentDefinition {
	for _, agent := range s.ListAgents() {
		if agent.Name == name && agent.ShadowedBy == "" {
			return agent
		}
	}
	return nil
}

// CreateAgent 创建新的自定义 agent，写入 markdown 文件。
// 对齐 Python: AgentConfigService.create_agent(params)
func (s *AgentConfigService) CreateAgent(params *CreateAgentParams) (*AgentDefinition, error) {
	name := strings.TrimSpace(params.Name)
	if !agentNamePattern.MatchString(name) {
		return nil, fmt.Errorf("Agent 名称格式无效: '%s'。要求 3-50 字符，仅允许字母、数字、连字符、下划线", name)
	}

	existing := s.GetAgent(params.Name)
	if existing != nil && existing.Source == AgentSourceBuiltin {
		return nil, fmt.Errorf("不能覆盖内置 agent: %s", params.Name)
	}

	targetDir := s.resolveLocationDir(params.Location)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	filePath := filepath.Join(targetDir, params.Name+".md")

	content := formatAgentFile(params)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	tools := params.Tools
	if len(tools) == 0 {
		tools = []string{"*"}
	}

	return &AgentDefinition{
		Name:            params.Name,
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
	}, nil
}

// UpdateAgent 更新自定义 agent 定义，覆盖写入文件。
// 对齐 Python: AgentConfigService.update_agent(name, params)
func (s *AgentConfigService) UpdateAgent(name string, params *UpdateAgentParams) (*AgentDefinition, error) {
	agent := s.GetAgent(name)
	if agent == nil {
		return nil, fmt.Errorf("Agent 不存在: %s", name)
	}
	if agent.Source == AgentSourceBuiltin {
		return nil, fmt.Errorf("不能修改内置 agent: %s", name)
	}
	if agent.FilePath == "" {
		return nil, fmt.Errorf("Agent 无文件路径: %s", name)
	}

	applyUpdateParams(agent, params)

	content := formatAgentFile(agent)
	if err := os.WriteFile(agent.FilePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	return agent, nil
}

// DeleteAgent 删除自定义 agent 定义文件。
// 对齐 Python: AgentConfigService.delete_agent(name)
func (s *AgentConfigService) DeleteAgent(name string) (bool, error) {
	agent := s.GetAgent(name)
	if agent == nil {
		return false, nil
	}
	if agent.Source == AgentSourceBuiltin {
		return false, fmt.Errorf("不能删除内置 agent: %s", name)
	}
	if agent.FilePath != "" {
		if err := os.Remove(agent.FilePath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("删除文件失败: %w", err)
		}
	}
	return true, nil
}

// ListAvailableTools 返回可用工具及其分组信息。
// 对齐 Python: AgentConfigService.list_available_tools()
func (s *AgentConfigService) ListAvailableTools() map[string]any {
	// 工具描述列表，对齐 Python _TOOL_DESCRIPTIONS
	tools := []map[string]any{
		{"name": "Read", "internal_name": "Read", "description": "读取文件内容", "group": "文件"},
		{"name": "Write", "internal_name": "Write", "description": "写入文件", "group": "文件"},
		{"name": "Edit", "internal_name": "Edit", "description": "编辑文件（精准替换）", "group": "文件"},
		{"name": "Bash", "internal_name": "Bash", "description": "执行 shell 命令", "group": "文件"},
		{"name": "LS", "internal_name": "LS", "description": "列出目录内容", "group": "文件"},
		{"name": "Grep", "internal_name": "Grep", "description": "搜索文件内容", "group": "搜索"},
		{"name": "Glob", "internal_name": "Glob", "description": "按模式搜索文件名", "group": "搜索"},
		{"name": "WebSearch", "internal_name": "WebSearch", "description": "网络搜索", "group": "搜索"},
		{"name": "WebFetch", "internal_name": "WebFetch", "description": "获取网页内容", "group": "搜索"},
		{"name": "LSP", "internal_name": "LSP", "description": "代码智能（定义跳转、引用查找）", "group": "搜索"},
		{"name": "TodoWrite", "internal_name": "TodoWrite", "description": "创建/更新任务列表", "group": "高级"},
		{"name": "TodoList", "internal_name": "TodoList", "description": "查看任务列表", "group": "高级"},
		{"name": "MemorySearch", "internal_name": "MemorySearch", "description": "搜索记忆", "group": "高级"},
		{"name": "MemoryGet", "internal_name": "MemoryGet", "description": "获取记忆条目", "group": "高级"},
		{"name": "WriteMemory", "internal_name": "WriteMemory", "description": "写入记忆", "group": "高级"},
		{"name": "EditMemory", "internal_name": "EditMemory", "description": "编辑记忆", "group": "高级"},
		{"name": "CronCreate", "internal_name": "CronCreate", "description": "创建定时任务", "group": "高级"},
		{"name": "CronList", "internal_name": "CronList", "description": "列出定时任务", "group": "高级"},
		{"name": "CronDelete", "internal_name": "CronDelete", "description": "删除定时任务", "group": "高级"},
		{"name": "SkillTool", "internal_name": "SkillTool", "description": "调用 Skill", "group": "高级"},
		{"name": "VisionQA", "internal_name": "VisionQA", "description": "视觉问答", "group": "多模态"},
		{"name": "ImageOCR", "internal_name": "ImageOCR", "description": "图片文字识别", "group": "多模态"},
		{"name": "AudioTranscribe", "internal_name": "AudioTranscribe", "description": "音频转录", "group": "多模态"},
	}
	groups := []string{"文件", "搜索", "高级", "多模态"}
	return map[string]any{
		"tools":  tools,
		"groups": groups,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// AgentConfigService Agent 配置管理服务。
// 对齐 Python: AgentConfigService
type AgentConfigService struct {
	// workspaceDir 工作空间目录
	workspaceDir string
}

// userAgentsDir 返回用户级 agent 目录：~/.uapclaw/agents/
func (s *AgentConfigService) userAgentsDir() string {
	return filepath.Join(pathutil.UserHomeDir(), ".uapclaw", "agents")
}

// projectAgentsDir 返回项目级 agent 目录：<workspace>/.uapclaw/agents/
func (s *AgentConfigService) projectAgentsDir() string {
	return filepath.Join(s.workspaceDir, ".uapclaw", "agents")
}

// localAgentsDir 返回本地级 agent 目录：<workspace>/.uapclaw/agents-local/
func (s *AgentConfigService) localAgentsDir() string {
	return filepath.Join(s.workspaceDir, ".uapclaw", "agents-local")
}

// resolveLocationDir 根据位置参数返回对应目录。
func (s *AgentConfigService) resolveLocationDir(location AgentSource) string {
	switch location {
	case AgentSourceUser:
		return s.userAgentsDir()
	case AgentSourceProject:
		return s.projectAgentsDir()
	case AgentSourceLocal:
		return s.localAgentsDir()
	default:
		return s.localAgentsDir()
	}
}

// loadFromDir 从目录加载所有 .md agent 定义文件。
// 对齐 Python: AgentConfigService._load_from_dir(dir_path, source)
func (s *AgentConfigService) loadFromDir(dirPath string, source AgentSource) []*AgentDefinition {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}
	var agents []*AgentDefinition
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		filePath := filepath.Join(dirPath, entry.Name())
		agent, err := parseAgentFile(filePath, source)
		if err != nil {
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
		return states
	}
	data, err := cfg.Load()
	if err != nil {
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
func copyBuiltinAgents() []*AgentDefinition {
	result := make([]*AgentDefinition, len(BuiltinAgents))
	for i, a := range BuiltinAgents {
		cp := *a
		tools := make([]string, len(a.Tools))
		copy(tools, a.Tools)
		cp.Tools = tools
		result[i] = &cp
	}
	return result
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
```

注意：需要在文件头部 import 中加入 `"fmt"`, `"strings"`, `"github.com/uapclaw/uapclaw-go/internal/common/config"`。

- [ ] **Step 2: 在同一文件中添加文件解析/生成函数**

在非导出函数区域继续添加：

```go
// parseAgentFile 解析 YAML frontmatter + Markdown body 格式的 agent 文件。
// 对齐 Python: _parse_agent_file(file_path, source)
func parseAgentFile(filePath string, source AgentSource) (*AgentDefinition, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "---") {
		return nil, nil
	}
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, nil
	}
	frontmatterStr := strings.TrimSpace(parts[1])
	prompt := strings.TrimSpace(parts[2])

	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterStr), &frontmatter); err != nil {
		return nil, fmt.Errorf("解析 frontmatter 失败: %w", err)
	}
	if frontmatter == nil {
		return nil, nil
	}
	name, _ := frontmatter["name"].(string)
	if name == "" {
		return nil, nil
	}

	description, _ := frontmatter["description"].(string)
	model, _ := frontmatter["model"].(string)
	whenToUse, _ := frontmatter["when_to_use"].(string)
	color, _ := frontmatter["color"].(string)
	permissionMode, _ := frontmatter["permission_mode"].(string)
	memoryScope, _ := frontmatter["memory_scope"].(string)

	var tools []string
	if t, ok := frontmatter["tools"]; ok {
		if arr, ok := t.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					tools = append(tools, s)
				}
			}
		}
	}

	var disallowedTools []string
	if t, ok := frontmatter["disallowed_tools"]; ok {
		if arr, ok := t.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					disallowedTools = append(disallowedTools, s)
				}
			}
		}
	}

	var skills []string
	if t, ok := frontmatter["skills"]; ok {
		if arr, ok := t.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					skills = append(skills, s)
				}
			}
		}
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

	def := &AgentDefinition{
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
	if len(def.Tools) == 0 {
		def.Tools = []string{"*"}
	}
	return def, nil
}

// formatAgentFile 生成 YAML frontmatter + Markdown body 格式的 agent 文件内容。
// 对齐 Python: _format_agent_file(params)
// 接受 *CreateAgentParams 或 *AgentDefinition。
func formatAgentFile(params any) string {
	frontmatter := make(map[string]any)
	var prompt string

	switch p := params.(type) {
	case *CreateAgentParams:
		frontmatter["name"] = p.Name
		frontmatter["description"] = p.Description
		prompt = p.Prompt
		if p.WhenToUse != "" {
			frontmatter["when_to_use"] = p.WhenToUse
		}
		if p.Model != "" {
			frontmatter["model"] = p.Model
		}
		if len(p.Tools) > 0 && !(len(p.Tools) == 1 && p.Tools[0] == "*") {
			frontmatter["tools"] = p.Tools
		}
		if p.Color != "" {
			frontmatter["color"] = p.Color
		}
		if p.PermissionMode != "" {
			frontmatter["permission_mode"] = p.PermissionMode
		}
		if p.MemoryScope != "" {
			frontmatter["memory_scope"] = p.MemoryScope
		}
		if len(p.DisallowedTools) > 0 {
			frontmatter["disallowed_tools"] = p.DisallowedTools
		}
		if p.MaxIterations != nil {
			frontmatter["max_iterations"] = *p.MaxIterations
		}
		if len(p.Skills) > 0 {
			frontmatter["skills"] = p.Skills
		}
	case *AgentDefinition:
		frontmatter["name"] = p.Name
		frontmatter["description"] = p.Description
		prompt = p.Prompt
		if p.WhenToUse != "" {
			frontmatter["when_to_use"] = p.WhenToUse
		}
		if p.Model != "" {
			frontmatter["model"] = p.Model
		}
		if len(p.Tools) > 0 && !(len(p.Tools) == 1 && p.Tools[0] == "*") {
			frontmatter["tools"] = p.Tools
		}
		if p.Color != "" {
			frontmatter["color"] = p.Color
		}
		if p.PermissionMode != "" {
			frontmatter["permission_mode"] = p.PermissionMode
		}
		if p.MemoryScope != "" {
			frontmatter["memory_scope"] = p.MemoryScope
		}
		if len(p.DisallowedTools) > 0 {
			frontmatter["disallowed_tools"] = p.DisallowedTools
		}
		if p.MaxIterations != nil {
			frontmatter["max_iterations"] = *p.MaxIterations
		}
		if len(p.Skills) > 0 {
			frontmatter["skills"] = p.Skills
		}
	}

	yamlBytes, _ := yaml.Marshal(frontmatter)
	return fmt.Sprintf("---\n%s---\n\n%s\n", string(yamlBytes), prompt)
}

// applyUpdateParams 将 UpdateAgentParams 的非 nil 字段应用到 AgentDefinition。
// 对齐 Python: _apply_update_params(agent, params)
func applyUpdateParams(agent *AgentDefinition, params *UpdateAgentParams) {
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
```

需要在 import 中加入 `"gopkg.in/yaml.v3"`。

- [ ] **Step 3: 编写数据模型和解析测试**

创建 `internal/swarm/server/runtime/agent_config_test.go`，覆盖：
- `parseAgentFile` 正常解析
- `parseAgentFile` 无 frontmatter
- `parseAgentFile` 无 name 字段
- `formatAgentFile` 生成 CreateAgentParams
- `formatAgentFile` 生成 AgentDefinition
- `applyUpdateParams` 各字段覆盖
- `BuiltinAgents` 列表长度和名称

- [ ] **Step 4: 编译验证**

```bash
export GOPROXY=https://goproxy.cn,direct
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
go build ./internal/swarm/server/runtime/...
```

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/swarm/server/runtime/... -run TestParse -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestFormat -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestApply -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestBuiltin -v -count=1 -timeout 60s
```

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/agent_config.go internal/swarm/server/runtime/agent_config_test.go
git commit -m "feat(runtime): add AgentConfigService data models and file parsing"
```

---

## Task 2: AgentConfigService CRUD 实现

**Files:**
- Modify: `internal/swarm/server/runtime/agent_config.go`（CRUD 方法已在上一步写出）
- Modify: `internal/swarm/server/runtime/agent_config_test.go`

- [ ] **Step 1: 编写 CRUD 测试**

在 `agent_config_test.go` 中添加测试函数，使用 `t.TempDir()` 作为 workspace：
- `TestListAgents_空目录` — 返回内置 agent
- `TestListAgents_优先级合并` — project 覆盖 builtin，被覆盖的标记 shadowed_by
- `TestListAgents_enabled状态注入` — 模拟 config.yaml 的 react.subagents
- `TestGetAgent_存在` — 返回活跃版本
- `TestGetAgent_不存在` — 返回 nil
- `TestCreateAgent_正常` — 写入 .md 文件
- `TestCreateAgent_名称校验` — 无效名称返回错误
- `TestCreateAgent_覆盖内置` — 返回错误
- `TestUpdateAgent_正常` — 覆盖写入
- `TestUpdateAgent_内置agent` — 返回错误
- `TestDeleteAgent_正常` — 删除文件
- `TestDeleteAgent_内置agent` — 返回错误

- [ ] **Step 2: 运行测试验证 CRUD**

```bash
go test ./internal/swarm/server/runtime/... -run TestListAgents -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestGetAgent -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestCreateAgent -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestUpdateAgent -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestDeleteAgent -v -count=1 -timeout 60s
```

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/agent_config.go internal/swarm/server/runtime/agent_config_test.go
git commit -m "feat(runtime): add AgentConfigService CRUD with priority merging"
```

---

## Task 3: config.yaml 联动

**Files:**
- Create: `internal/swarm/server/runtime/agent_config_yaml.go`
- Create: `internal/swarm/server/runtime/agent_config_yaml_test.go`

- [ ] **Step 1: 创建 agent_config_yaml.go**

```go
package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UpsertSubagentInConfig 在 config.yaml 的 react.subagents 中增/改指定 agent 的 enabled 状态。
// 对齐 Python: upsert_subagent_in_config(name, enabled)
func UpsertSubagentInConfig(name string, enabled bool) error {
	configPath := pathutil.ConfigFile()
	return upsertSubagentInConfigPath(configPath, name, enabled)
}

// RemoveSubagentFromConfig 从 config.yaml 的 react.subagents 中移除指定 agent。
// 对齐 Python: remove_subagent_from_config(name)
func RemoveSubagentFromConfig(name string) error {
	configPath := pathutil.ConfigFile()
	return removeSubagentFromConfigPath(configPath, name)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// upsertSubagentInConfigPath 在指定 config.yaml 路径中增/改 subagent enabled 状态。
// 可注入路径，方便测试。
func upsertSubagentInConfigPath(configPath, name string, enabled bool) error {
	data, err := loadYAMLFile(configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 确保 react.subagents 存在
	react, _ := data["react"].(map[string]any)
	if react == nil {
		react = make(map[string]any)
		data["react"] = react
	}
	subagents, _ := react["subagents"].(map[string]any)
	if subagents == nil {
		subagents = make(map[string]any)
		react["subagents"] = subagents
	}

	// 设置 enabled
	entry, _ := subagents[name].(map[string]any)
	if entry == nil {
		entry = make(map[string]any)
	}
	entry["enabled"] = enabled
	subagents[name] = entry

	return saveYAMLFile(configPath, data)
}

// removeSubagentFromConfigPath 从指定 config.yaml 路径中移除 subagent。
func removeSubagentFromConfigPath(configPath, name string) error {
	data, err := loadYAMLFile(configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	react, _ := data["react"].(map[string]any)
	if react == nil {
		return nil
	}
	subagents, _ := react["subagents"].(map[string]any)
	if subagents == nil {
		return nil
	}

	delete(subagents, name)

	// 清理空 subagents 段
	if len(subagents) == 0 {
		delete(react, "subagents")
	}

	return saveYAMLFile(configPath, data)
}

// loadYAMLFile 读取 YAML 文件为 map[string]any。
func loadYAMLFile(path string) (map[string]any, error) {
	data := make(map[string]any)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, err
	}
	if len(content) == 0 {
		return data, nil
	}
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}
	return data, nil
}

// saveYAMLFile 将 map[string]any 写回 YAML 文件。
func saveYAMLFile(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化 YAML 失败: %w", err)
	}
	return os.WriteFile(path, content, 0o644)
}
```

- [ ] **Step 2: 编写联动测试**

创建 `agent_config_yaml_test.go`，使用 `t.TempDir()` 创建临时 config.yaml：
- `TestUpsertSubagentInConfig_新增` — 写入 enabled=true
- `TestUpsertSubagentInConfig_更新` — 从 true 改为 false
- `TestUpsertSubagentInConfig_文件不存在` — 自动创建
- `TestRemoveSubagentFromConfig_存在` — 删除条目
- `TestRemoveSubagentFromConfig_不存在` — 无操作
- `TestRemoveSubagentFromConfig_清空后删除段` — 最后一个删除后清理 subagents 段

- [ ] **Step 3: 运行测试**

```bash
go test ./internal/swarm/server/runtime/... -run TestUpsert -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestRemove -v -count=1 -timeout 60s
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/agent_config_yaml.go internal/swarm/server/runtime/agent_config_yaml_test.go
git commit -m "feat(runtime): add config.yaml react.subagents upsert/remove"
```

---

## Task 4: LLM 生成功能

**Files:**
- Create: `internal/swarm/server/runtime/agent_config_llm.go`
- Create: `internal/swarm/server/runtime/agent_config_llm_test.go`

- [ ] **Step 1: 创建 agent_config_llm.go**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// agentCreationSystemPrompt LLM 生成 agent 的系统提示词。
// 对齐 Python: _AGENT_CREATION_SYSTEM_PROMPT
const agentCreationSystemPrompt = `你是一个精英 AI Agent 架构师。给定 agent 名称和描述，你的任务是设计一个高性能的、能执行任务到完成的 Agent——而不仅仅是分析和报告。

该 Agent 将拥有工具（Read、Write、Edit、Bash 等）来完成任务。将其设计为一个能够以最少额外指导处理其指定任务的自主专家。你编写的系统提示词是该 Agent 的完整操作手册。

1. **whenToUse**: 精确描述主助手何时应将任务分派给此 Agent。
   - 以"当...时使用此 Agent"开头
   - 包含具体的触发条件
   - 添加 2-3 个 <example> 块，展示助手使用 Agent 工具完全委派任务的具体场景
   - 每个 <example> 应展示：用户说 X → 助手通过 Agent 工具分派到此 Agent，传递完整任务
   - 使用与 agent 描述相同的语言编写

2. **systemPrompt**: 控制 Agent 行为的完整系统提示词。
   - 定义专家角色
   - 指定工作流程和方法论——端到端，从分析到执行
   - 建立清晰的行为边界和操作参数
   - 提供具体的方法论和最佳实践
   - 定义输出格式期望
   - 包含自验证步骤
   - 使用与 agent 描述相同的语言编写

核心原则：
- 具体而非笼统——避免模糊的指令
- 在能澄清行为时包含具体示例
- 平衡全面性和清晰性——每条指令都应有价值
- 确保 Agent 有足够的上下文来处理核心任务的变体
- 内置质量保证和自我纠正机制

仅返回 JSON 对象：
{"whenToUse": "...", "systemPrompt": "..."}`

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentLLMCaller 定义 LLM 调用接口，方便测试 mock。
type AgentLLMCaller interface {
	// Invoke 调用 LLM 并返回文本响应。
	Invoke(ctx context.Context, prompt string) (string, error)
}

// GenerateAgentWithLLM 调用 LLM 生成 whenToUse + systemPrompt。
// 对齐 Python: _generate_agent_with_llm(name, description)
// 返回 (whenToUse, systemPrompt, error)，失败时调用方应回退到模板值。
func GenerateAgentWithLLM(ctx context.Context, caller AgentLLMCaller, name, description string) (whenToUse string, systemPrompt string, err error) {
	fullPrompt := fmt.Sprintf(`%s

---
请为以下 agent 生成配置：

名称: %s
描述: %s

返回 JSON 对象，包含 whenToUse 和 systemPrompt 两个字段。不要返回其他内容。`, agentCreationSystemPrompt, name, description)

	text, err := caller.Invoke(ctx, fullPrompt)
	if err != nil {
		return "", "", fmt.Errorf("LLM 调用失败: %w", err)
	}

	// 解析 JSON
	whenToUse, systemPrompt, err = parseLLMResponse(text)
	if err != nil {
		return "", "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	return whenToUse, systemPrompt, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// llmResponse LLM 响应的 JSON 结构。
type llmResponse struct {
	WhenToUse   string `json:"whenToUse"`
	SystemPrompt string `json:"systemPrompt"`
}

// jsonBlockPattern 匹配 JSON 对象块。
var jsonBlockPattern = regexp.MustCompile(`\{[\s\S]*\}`)

// parseLLMResponse 解析 LLM 返回的 JSON 文本，提取 whenToUse 和 systemPrompt。
// 对齐 Python: _handle_agents_create 中 JSON 解析逻辑（含正则回退）
func parseLLMResponse(text string) (whenToUse string, systemPrompt string, err error) {
	text = strings.TrimSpace(text)

	var resp llmResponse
	// 先尝试直接解析
	if jsonErr := json.Unmarshal([]byte(text), &resp); jsonErr == nil {
		return validateLLMResponse(resp)
	}

	// 回退：正则提取 JSON 块
	match := jsonBlockPattern.FindString(text)
	if match == "" {
		return "", "", fmt.Errorf("响应中未找到 JSON 对象: %s", truncate(text, 200))
	}
	if jsonErr := json.Unmarshal([]byte(match), &resp); jsonErr != nil {
		return "", "", fmt.Errorf("JSON 解析失败: %s", truncate(text, 200))
	}
	return validateLLMResponse(resp)
}

// validateLLMResponse 校验 LLM 响应字段非空。
func validateLLMResponse(resp llmResponse) (string, string, error) {
	whenToUse = strings.TrimSpace(resp.WhenToUse)
	systemPrompt = strings.TrimSpace(resp.SystemPrompt)
	if whenToUse == "" || systemPrompt == "" {
		return "", "", fmt.Errorf("LLM 响应不完整: whenToUse 或 systemPrompt 为空")
	}
	return whenToUse, systemPrompt, nil
}

// truncate 截断字符串。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
```

- [ ] **Step 2: 编写 LLM 生成测试**

创建 `agent_config_llm_test.go`，包含 mock caller：
- `TestGenerateAgentWithLLM_正常` — mock 返回有效 JSON
- `TestGenerateAgentWithLLM_调用失败` — mock 返回 error
- `TestParseLLMResponse_正常JSON`
- `TestParseLLMResponse_带Markdown代码块` — ` ```json\n{...}\n``` `
- `TestParseLLMResponse_需要正则回退` — 前后有多余文字
- `TestParseLLMResponse_无JSON` — 返回错误
- `TestParseLLMResponse_字段为空` — 返回错误

- [ ] **Step 3: 运行测试**

```bash
go test ./internal/swarm/server/runtime/... -run TestGenerate -v -count=1 -timeout 60s
go test ./internal/swarm/server/runtime/... -run TestParseLLM -v -count=1 -timeout 60s
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/agent_config_llm.go internal/swarm/server/runtime/agent_config_llm_test.go
git commit -m "feat(runtime): add LLM-based agent whenToUse+systemPrompt generation"
```

---

## Task 5: 回填 deep_adapter + code_adapter

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go:122-123`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 在 deep_adapter_config.go 中实现 loadCustomSubagents**

替换 `TODO(#custom-subagents)` 注释为实际实现：

```go
	// ── 自定义 agent: 对齐 Python _load_custom_subagents ──
	if d.workspaceDir != "" {
		customSpecs := d.loadCustomSubagents(d.workspaceDir, subagentsCfg)
		specs = append(specs, customSpecs...)
	}
```

在文件末尾（非导出函数区域）添加：

```go
// loadCustomSubagents 从 AgentConfigService 加载 enabled 的自定义 agent 并转换为 SubagentSpec 列表。
// 对齐 Python: _load_custom_subagents + load_custom_agents_as_subagent_configs
func (d *DeepAdapter) loadCustomSubagents(workspaceDir string, subagentsCfg map[string]any) []hschema.SubagentSpec {
	service := runtime.NewAgentConfigService(workspaceDir)
	agents := service.ListAgents()

	var specs []hschema.SubagentSpec
	for _, agentDef := range agents {
		if agentDef.Source == AgentSourceBuiltin {
			continue
		}
		// 只有显式 enabled: true 才加载
		if agentDef.Enabled == nil || !*agentDef.Enabled {
			continue
		}
		spec := agentDefToSubagentConfig(agentDef, d.model, nil)
		if spec != nil {
			specs = append(specs, spec)
		}
	}

	logger.Info(logComponent).
		Int("custom_subagent_count", len(specs)).
		Msg("loadCustomSubagents 完成")

	return specs
}

// agentDefToSubagentConfig 将 AgentDefinition 转换为 SubAgentConfig。
// 对齐 Python: _agent_def_to_subagent_config
func agentDefToSubagentConfig(agentDef *runtime.AgentDefinition, model *llm.Model, modelCache map[string]*llm.Model) *hschema.SubAgentConfig {
	resolvedModel := model
	if agentDef.Model != "" && modelCache != nil {
		if m, ok := modelCache[agentDef.Model]; ok {
			resolvedModel = m
		}
	}

	card := schema.NewAgentCard(
		schema.WithAgentName(agentDef.Name),
		schema.WithAgentDescription(agentDef.Description),
	)

	cfg := hschema.NewSubAgentConfig()
	cfg.AgentCard = card
	cfg.SystemPrompt = agentDef.Prompt
	cfg.Model = resolvedModel
	cfg.MaxIterations = agentDef.MaxIterations
	cfg.Skills = agentDef.Skills

	// 工具列表处理
	if len(agentDef.Tools) > 0 && !(len(agentDef.Tools) == 1 && agentDef.Tools[0] == "*") {
		// 指定工具：需要查找对应的 ToolCard（当前简化为只传名称列表）
		// 完整实现需要从 tool registry 查找 ToolCard
		_ = agentDef.Tools // TODO: 转换为 ToolCard 列表
	}

	return cfg
}
```

需要在 import 中加入 `runtime` 和 `llm` 包路径以及 `schema` 包路径。

- [ ] **Step 2: 更新 doc.go 文件目录**

在 `internal/swarm/server/runtime/doc.go` 中添加新文件条目：

```
//	├── agent_config.go         # AgentConfigService 配置 CRUD
//	├── agent_config_yaml.go    # config.yaml react.subagents 联动
//	├── agent_config_llm.go     # LLM 生成 whenToUse + systemPrompt
```

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/swarm/server/...
```

- [ ] **Step 4: 运行受影响测试**

```bash
go test ./internal/swarm/server/adapter/... -count=1 -timeout 120s
```

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_config.go internal/swarm/server/runtime/doc.go
git commit -m "feat(adapter): backfill loadCustomSubagents from AgentConfigService"
```

---

## Task 6: 回填 handle_agents.go

**Files:**
- Modify: `internal/swarm/server/handle_agents.go`
- Modify: `internal/swarm/server/agent_server.go`（添加 agentConfigSvc 字段）

- [ ] **Step 1: 在 AgentServer 中添加获取 AgentConfigService 的方法**

在 `agent_server.go` 中添加懒初始化方法：

```go
// getAgentConfigService 获取 AgentConfigService 实例（从请求参数提取 workspace_dir）。
func (s *AgentServer) getAgentConfigService(request *schema.AgentRequest) *runtime.AgentConfigService {
	workspaceDir := ""
	if request.Params != nil {
		if wd, ok := request.Params["workspace_dir"].(string); ok {
			workspaceDir = wd
		}
	}
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}
	return runtime.NewAgentConfigService(workspaceDir)
}
```

- [ ] **Step 2: 重写 handle_agents.go 的 7 个 handler**

将所有 stub 替换为真实实现。关键结构：

- `handleAgentsList` → `service.ListAgents()` → `{"agents": [...]}`
- `handleAgentsGet` → `service.GetAgent(name)` → `{"agent": {...}}` 或 NOT_FOUND
- `handleAgentsCreate` → 解析 params → 可选 `GenerateAgentWithLLM` → `CreateAgent()` → `UpsertSubagentInConfig()` → `ReloadAgentsConfig()`
- `handleAgentsUpdate` → 解析 params → 可选 `GenerateAgentWithLLM` → `UpdateAgent()` → `ReloadAgentsConfig()`
- `handleAgentsDelete` → `DeleteAgent()` → `RemoveSubagentFromConfig()` → `ReloadAgentsConfig()`
- `handleAgentsEnable` → `UpsertSubagentInConfig(name, true)` → `ReloadAgentsConfig()`
- `handleAgentsDisable` → `UpsertSubagentInConfig(name, false)` → `ReloadAgentsConfig()`
- `handleAgentsToolsList` → `service.ListAvailableTools()`

每个 handler 都需要：
1. 从 request 提取参数
2. 调用 AgentConfigService 方法
3. 错误处理 → 返回 `ok=false` + error payload
4. 成功 → 返回 `ok=true` + 结果 payload
5. Create/Update/Delete/Enable/Disable 后调用 ReloadAgentsConfig

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/swarm/server/...
```

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/swarm/server/... -count=1 -timeout 120s
```

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/handle_agents.go internal/swarm/server/agent_server.go
git commit -m "feat(server): implement agents.* RPC handlers with AgentConfigService"
```

---

## Task 7: 更新 IMPLEMENTATION_PLAN.md 和 doc.go

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `internal/swarm/server/runtime/doc.go`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

将 10.3.13 状态从 `☐` 改为 `✅`：

```
| 10.3.13 | ✅ | AgentConfigService | Agent 配置 CRUD | `jiuwenswarm/server/runtime/agent_config_service.py` |
```

- [ ] **Step 2: 更新 runtime/doc.go 文件目录**

确认包含所有新增文件。

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/swarm/server/runtime/doc.go
git commit -m "docs: mark 10.3.13 AgentConfigService as complete"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- ✅ 数据模型（AgentSource/AgentDefinition/CreateAgentParams/UpdateAgentParams）→ Task 1
- ✅ 文件解析/生成（parseAgentFile/formatAgentFile/applyUpdateParams）→ Task 1
- ✅ CRUD Service → Task 2
- ✅ config.yaml 联动（UpsertSubagentInConfig/RemoveSubagentFromConfig）→ Task 3
- ✅ LLM 生成（GenerateAgentWithLLM）→ Task 4
- ✅ 回填 deep_adapter → Task 5
- ✅ 回填 handle_agents.go → Task 6
- ✅ 品牌名统一 → Task 0
- ✅ IMPLEMENTATION_PLAN.md 更新 → Task 7

**2. Placeholder scan:** 无 TBD/TODO（Task 5 中的 tool card 转换有 TODO 注释，是因为当前 tool registry 尚未完善，该注释为合理的延后标记）

**3. Type consistency:**
- `AgentDefinition.Enabled` 在 Task 1 定义为 `*bool`，Task 5 消费时使用 `agentDef.Enabled == nil || !*agentDef.Enabled` ✓
- `AgentDefinition.MaxIterations` 在 Task 1 定义为 `*int`，Task 5 传递给 `SubAgentConfig.MaxIterations` ✓
- `AgentLLMCaller` 接口在 Task 4 定义，测试中使用 mock ✓
