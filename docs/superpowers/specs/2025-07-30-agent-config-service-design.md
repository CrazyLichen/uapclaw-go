# AgentConfigService 设计文档

## 概述

实现 10.3.13 AgentConfigService — Agent 定义配置管理中心，提供 CRUD 操作、
四层来源合并、config.yaml 联动、LLM 生成 whenToUse+systemPrompt、
以及回填 deep_adapter 和 handle_agents.go stub。

## 在 Agent 会话中的流程位置

```
用户请求（Gateway → AgentServer）
       │
       ▼
AgentServer（E2A 方法分发）
       │
       ├── agents.list ────────→ AgentConfigService.ListAgents()
       ├── agents.get ────────→ AgentConfigService.GetAgent()
       ├── agents.create ─────→ AgentConfigService.CreateAgent()
       │                          + GenerateAgentWithLLM()（可选）
       │                          + UpsertSubagentInConfig()
       │                          + ReloadAgentsConfig()
       ├── agents.update ─────→ AgentConfigService.UpdateAgent()
       │                          + GenerateAgentWithLLM()（可选）
       │                          + ReloadAgentsConfig()
       ├── agents.delete ─────→ AgentConfigService.DeleteAgent()
       │                          + RemoveSubagentFromConfig()
       │                          + ReloadAgentsConfig()
       ├── agents.enable ─────→ UpsertSubagentInConfig(name, true) + ReloadAgentsConfig()
       ├── agents.disable ────→ UpsertSubagentInConfig(name, false) + ReloadAgentsConfig()
       └── agents.tools_list ─→ AgentConfigService.ListAvailableTools()

运行时消费：
       │
       ├── DeepAdapter.loadCustomSubagents()
       │     → ListAgents() → 过滤 source≠builtin && enabled==true
       │     → AgentDefinition → SubAgentConfig → 注入 DeepAgent.Subagents[]
       │
       └── CodeAdapter.loadCustomAgents()
             → ListAgents() → 过滤 source≠builtin && enabled==true
             → AgentDefinition → AgentTool 注册到 Code 模式
```

## AgentConfigService vs AgentManager

| 维度 | AgentConfigService | AgentManager |
|------|-------------------|--------------|
| 管什么 | Agent 定义（模板）— 元数据 | Agent 实例（运行时）— UapClaw 实例 |
| 类比 | Docker Image | Docker Container |
| 存储 | 文件系统 `.uapclaw/agents/*.md` + `config.yaml` | 内存 `agents[channelID][cacheKey]` |
| 生命周期 | 持久化，跨重启存在 | 进程内，重启丢失 |
| CRUD | 增删改查 | get/create/recreate/cleanup |

## 业务触发场景

`agents.*` RPC 目前仅由 TUI `/agents` 斜杠命令触发（Web ConfigPanel 的 agent 标签是 Team 专用）。

## 第 0 步：品牌名统一

修改运行时代码中的 `jiuwenswarm` 硬编码为 `uapclaw`：

| 文件 | 修改 |
|------|------|
| `internal/swarm/server/adapter/deep_adapter.go:388` | `WithAgentID("jiuwenswarm")` → `"uapclaw"` |
| `internal/swarm/server/adapter/deep_adapter.go:579` | `getToolCards("jiuwenswarm")` → `"uapclaw"` |
| `internal/swarm/server/adapter/deep_adapter.go:589` | `WithAgentID("jiuwenswarm")` → `"uapclaw"` |
| `internal/agentcore/harness/prompts/sections/identity.go:34` | `由 JiuwenSwarm 创建` → `由 UapClaw 创建` |
| `internal/agentcore/harness/prompts/sections/identity.go:37` | `BT.jiuwenswarmBT` → `BT.uapclawBT` |
| `internal/agentcore/harness/prompts/sections/identity.go:82` | `created by JiuwenSwarm` → `created by UapClaw` |
| `internal/agentcore/harness/prompts/sections/identity.go:85` | `BT.jiuwenswarmBT` → `BT.uapclawBT` |

**不改**：doc.go 中 `对应 Python: jiuwenswarm/...` 的注释（代码溯源信息）。

## 第 1 步：数据模型

文件：`internal/swarm/server/runtime/agent_config.go`

### AgentSource 枚举

```go
type AgentSource string

const (
    AgentSourceBuiltin AgentSource = "builtin"
    AgentSourceUser    AgentSource = "user"
    AgentSourceProject AgentSource = "project"
    AgentSourceLocal   AgentSource = "local"
)
```

### AgentDefinition

```go
type AgentDefinition struct {
    Name            string      `json:"name" yaml:"name"`
    Description     string      `json:"description" yaml:"description"`
    Prompt          string      `json:"prompt" yaml:"-"`
    Source          AgentSource `json:"source" yaml:"-"`
    FilePath        string      `json:"file_path,omitempty" yaml:"-"`
    Model           string      `json:"model,omitempty" yaml:"model,omitempty"`
    Tools           []string    `json:"tools" yaml:"tools,omitempty"`
    DisallowedTools []string    `json:"disallowed_tools" yaml:"disallowed_tools,omitempty"`
    Color           string      `json:"color,omitempty" yaml:"color,omitempty"`
    PermissionMode  string      `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
    MemoryScope     string      `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
    ShadowedBy      AgentSource `json:"shadowed_by,omitempty" yaml:"-"`
    Enabled         *bool       `json:"enabled,omitempty" yaml:"-"`      // nil=未配置, true=启用, false=禁用
    WhenToUse       string      `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
    MaxIterations   *int        `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
    Skills          []string    `json:"skills,omitempty" yaml:"skills,omitempty"`
}
```

字段类型决策：
- `string` 类型：零值 `""` 等价 Python `None`，用值类型
- `[]string` 类型：零值 `nil` 等价 Python `[]` 或 `None`
- `*bool`：三态，nil=未在config.yaml中配置, true=显式启用, false=显式禁用
- `*int`：nil=未设置, 正值=有效值
- `yaml:"-"`：不参与 YAML frontmatter 序列化（运行时计算字段）

### CreateAgentParams

```go
type CreateAgentParams struct {
    Name            string   `json:"name" yaml:"name"`
    Description     string   `json:"description" yaml:"description"`
    Prompt          string   `json:"prompt" yaml:"-"`
    Location        AgentSource `json:"location" yaml:"-"`
    Model           string   `json:"model,omitempty" yaml:"model,omitempty"`
    Tools           []string `json:"tools,omitempty" yaml:"tools,omitempty"`
    Color           string   `json:"color,omitempty" yaml:"color,omitempty"`
    PermissionMode  string   `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
    MemoryScope     string   `json:"memory_scope,omitempty" yaml:"memory_scope,omitempty"`
    DisallowedTools []string `json:"disallowed_tools,omitempty" yaml:"disallowed_tools,omitempty"`
    WhenToUse       string   `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
    MaxIterations   *int     `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
    Skills          []string `json:"skills,omitempty" yaml:"skills,omitempty"`
}
```

### UpdateAgentParams

```go
type UpdateAgentParams struct {
    Description     *string  `json:"description,omitempty"`      // nil=不修改
    WhenToUse       *string  `json:"when_to_use,omitempty"`
    Prompt          *string  `json:"prompt,omitempty"`
    Model           *string  `json:"model,omitempty"`
    Tools           []string `json:"tools,omitempty"`            // nil=不修改
    Color           *string  `json:"color,omitempty"`
    PermissionMode  *string  `json:"permission_mode,omitempty"`
    MemoryScope     *string  `json:"memory_scope,omitempty"`
    DisallowedTools []string `json:"disallowed_tools,omitempty"`
    MaxIterations   *int     `json:"max_iterations,omitempty"`
    Skills          []string `json:"skills,omitempty"`
}
```

UpdateAgentParams 中 string 字段用 `*string`：需区分"不修改"（nil）和"清空"（指向空字符串的指针）。

## 第 2 步：文件解析/生成

同文件 `agent_config.go` 非导出函数。

### 文件格式

YAML frontmatter + Markdown body，与 Python 一致：

```markdown
---
name: code-reviewer
description: 代码审查 agent
when_to_use: 当用户需要代码审查时使用
tools:
  - Read
  - Bash
  - Grep
---

你是代码审查专家...
```

### 核心函数

- `parseAgentFile(path string, source AgentSource) (*AgentDefinition, error)`
  - 读取文件内容
  - 以 `---` 分割 frontmatter 和 body
  - `yaml.Unmarshal` 解析 frontmatter
  - body 作为 Prompt
  - 校验 `name` 字段必须存在

- `formatAgentFile(params interface{}) string`
  - 接受 `*CreateAgentParams` 或 `*AgentDefinition`
  - 生成 frontmatter map，省略零值字段
  - `yaml.Marshal` + 拼接 body
  - `tools: ["*"]` 不写入 YAML（默认值）

- `applyUpdateParams(agent *AgentDefinition, params *UpdateAgentParams)`
  - 遍历所有字段，非 nil 则覆盖

## 第 3 步：AgentConfigService CRUD

```go
type AgentConfigService struct {
    workspaceDir string
}

func NewAgentConfigService(workspaceDir string) *AgentConfigService
func (s *AgentConfigService) ListAgents() []*AgentDefinition
func (s *AgentConfigService) GetAgent(name string) *AgentDefinition
func (s *AgentConfigService) CreateAgent(params *CreateAgentParams) (*AgentDefinition, error)
func (s *AgentConfigService) UpdateAgent(name string, params *UpdateAgentParams) (*AgentDefinition, error)
func (s *AgentConfigService) DeleteAgent(name string) (bool, error)
func (s *AgentConfigService) ListAvailableTools() map[string]any
```

### 路径规则

| 来源 | 路径 |
|------|------|
| project | `<workspace>/.uapclaw/agents/*.md` |
| user | `~/.uapclaw/agents/*.md` |
| local | `<workspace>/.uapclaw/agents-local/*.md` |
| builtin | 硬编码 `BuiltinAgents` 变量 |

使用 `internal/common/utils/path` 包获取 `~/.uapclaw/` 路径。

### 合并优先级

project > user > local > builtin（与 Python 一致）。

`ListAgents()` 逻辑：
1. 按 builtin → local → user → project 顺序加载
2. 按名字分组，后加载的覆盖先加载的
3. 被覆盖的标记 `shadowed_by = active.source`
4. 从 config.yaml 的 `react.subagents` 读取 enabled 状态，注入到对应 agent
5. 按 source 排序返回

### 名称校验

`CreateAgent` 校验：`^[a-zA-Z0-9_-]{3,50}$`，与 Python 一致。

### 内置 Agent

```go
var BuiltinAgents = []*AgentDefinition{
    {Name: "general-purpose", Description: "通用多步任务 agent，适用于没有专用 agent 的各类任务", Source: AgentSourceBuiltin, Tools: []string{"*"}, ...},
    {Name: "Explore", Description: "快速只读代码库探索 agent，用于定位代码、搜索符号、查找文件", Source: AgentSourceBuiltin, Tools: []string{"Read", "Bash", "Grep", "Glob"}, ...},
    {Name: "Plan", Description: "软件架构设计 agent，用于规划实现方案", Source: AgentSourceBuiltin, Tools: []string{"Read", "Bash", "Grep", "Glob"}, ...},
}
```

Prompt 内容完整对齐 Python `BUILTIN_AGENTS`。

## 第 4 步：config.yaml 联动

文件：`internal/swarm/server/runtime/agent_config_yaml.go`

```go
// UpsertSubagentInConfig 在 config.yaml 的 react.subagents 中增/改指定 agent 的 enabled 状态。
// 对齐 Python: upsert_subagent_in_config(name, enabled)
func UpsertSubagentInConfig(name string, enabled bool) error

// RemoveSubagentFromConfig 从 config.yaml 的 react.subagents 中移除指定 agent。
// 对齐 Python: remove_subagent_from_config(name)
func RemoveSubagentFromConfig(name string) error
```

config.yaml 键路径：`react.subagents.<name>.enabled`，对齐 Python。

使用 `internal/common/config` 包读写 YAML。

## 第 5 步：LLM 生成功能

文件：`internal/swarm/server/runtime/agent_config_llm.go`

```go
// GenerateAgentWithLLM 调用 LLM 生成 whenToUse + systemPrompt。
// 对齐 Python: _generate_agent_with_llm(name, description)
// 返回 (whenToUse, systemPrompt, error)，失败时返回 error 供调用方回退。
func GenerateAgentWithLLM(ctx context.Context, modelName string, name, description string) (whenToUse string, systemPrompt string, err error)
```

实现要点：
- 使用 agentcore LLM 客户端接口
- System prompt 对齐 Python `_AGENT_CREATION_SYSTEM_PROMPT`
- 调用参数：`max_tokens=2000, temperature=0.3`
- JSON 解析带回退：先 `json.Unmarshal`，失败则正则提取 `{...}` 再解析
- 返回值需包含 `whenToUse` 和 `systemPrompt` 两个非空字段

### Prompt 内容

对齐 Python `agent_ws_server.py:103-133` 的 `_AGENT_CREATION_SYSTEM_PROMPT`，完整中文翻译版：

```
你是一个精英 AI Agent 架构师。给定 agent 名称和描述，你的任务是设计一个高性能的、能执行任务到完成的 Agent——而不仅仅是分析和报告。

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
{"whenToUse": "...", "systemPrompt": "..."}
```

## 第 6 步：回填 deep_adapter

文件：`internal/swarm/server/adapter/deep_adapter_config.go`

回填 `TODO(#custom-subagents)`：

```go
// loadCustomSubagents 对齐 Python _load_custom_subagents
// 从 AgentConfigService 加载 enabled 的自定义 agent 并转换为 SubagentSpec 列表
func (d *DeepAdapter) loadCustomSubagents(workspaceDir string, subagentsCfg map[string]any) []hschema.SubagentSpec
```

逻辑：
1. `NewAgentConfigService(workspaceDir)`
2. `ListAgents()` 过滤 `source != builtin && enabled == true`
3. `AgentDefinition → SubAgentConfig` 转换：
   - `AgentCard{Name: name, Description: description}`
   - `SystemPrompt = prompt`
   - `Tools = agent_def.Tools`（若 `["*"]` 则传 `nil` 表示全量）
   - `Model` 解析（若指定模型名，从 model_cache 查找）
   - `MaxIterations`、`Skills` 直接映射
4. 同样回填 code_adapter 中的 custom agents 调用点

### AgentDefinition → SubAgentConfig 转换

对齐 Python `_agent_def_to_subagent_config`：

```go
func agentDefToSubagentConfig(agentDef *AgentDefinition, model *llm.Model, modelCache map[string]*llm.Model) *hschema.SubAgentConfig {
    resolvedModel := model
    if agentDef.Model != "" {
        if m, ok := modelCache[agentDef.Model]; ok {
            resolvedModel = m
        }
    }
    tools := agentDef.Tools
    if len(tools) == 0 {
        tools = []string{"*"}
    }
    return &hschema.SubAgentConfig{
        AgentCard:     schema.NewAgentCard(schema.WithAgentName(agentDef.Name), schema.WithAgentDescription(agentDef.Description)),
        SystemPrompt:  agentDef.Prompt,
        Tools:         resolveToolCards(tools, agentDef.DisallowedTools),
        Model:         resolvedModel,
        MaxIterations: agentDef.MaxIterations,
        Skills:        agentDef.Skills,
    }
}
```

## 第 7 步：回填 handle_agents.go

将 7 个 stub handler 改为调用 AgentConfigService 的真实实现。

### AgentServer 持有 AgentConfigService

```go
type AgentServer struct {
    // ... 已有字段
    agentConfigSvc *runtime.AgentConfigService  // 懒初始化
}
```

通过 `getAgentConfigService(request)` 获取实例（从 request.params 读取 workspace_dir）。

### Handler 实现

| Handler | 实现逻辑 |
|---------|---------|
| `handleAgentsList` | `ListAgents()` → `{"agents": [...]}` |
| `handleAgentsGet` | `GetAgent(name)` → `{"agent": {...}}` 或 NOT_FOUND |
| `handleAgentsCreate` | 解析 params → 若 generate=true 调 `GenerateAgentWithLLM` → `CreateAgent()` → `UpsertSubagentInConfig(name, true)` → `ReloadAgentsConfig()` → `{"agent": {...}, "generated": bool, "applied": bool}` |
| `handleAgentsUpdate` | 解析 params → 若 generate=true 调 `GenerateAgentWithLLM` → `UpdateAgent()` → `ReloadAgentsConfig()` → `{"agent": {...}, "generated": bool, "applied": bool}` |
| `handleAgentsDelete` | `DeleteAgent(name)` → `RemoveSubagentFromConfig(name)` → `ReloadAgentsConfig()` → `{"ok": bool, "applied": bool}` |
| `handleAgentsEnable` | `GetAgent(name)` 校验 → `UpsertSubagentInConfig(name, true)` → `ReloadAgentsConfig()` → `{"applied": bool}` |
| `handleAgentsDisable` | `GetAgent(name)` 校验 → `UpsertSubagentInConfig(name, false)` → `ReloadAgentsConfig()` → `{"applied": bool}` |
| `handleAgentsToolsList` | `ListAvailableTools()` → `{"tools": [...], "groups": [...]}` |

## 测试计划

| 文件 | 覆盖内容 |
|------|---------|
| `agent_config_test.go` | CRUD 全流程、优先级合并、shadow 标记、名称校验、文件解析/生成、内置 agent 定义 |
| `agent_config_yaml_test.go` | `UpsertSubagentInConfig` / `RemoveSubagentFromConfig` 在临时 config.yaml 上的增删改 |
| `agent_config_llm_test.go` | JSON 解析（正常/异常/需要正则回退）、空字段校验 |

测试使用 `t.TempDir()` 创建临时目录，不依赖外部环境。

## 文件目录变更

```
internal/swarm/server/runtime/
├── doc.go                    # 更新文件目录
├── agent_config.go           # 新增：数据模型 + CRUD + 文件解析/生成
├── agent_config_test.go      # 新增：CRUD 测试
├── agent_config_yaml.go      # 新增：config.yaml 联动
├── agent_config_yaml_test.go # 新增：联动测试
├── agent_config_llm.go       # 新增：LLM 生成
├── agent_config_llm_test.go  # 新增：LLM 测试
└── ...

internal/swarm/server/adapter/
├── deep_adapter_config.go    # 修改：回填 loadCustomSubagents
├── code_adapter.go           # 修改：回填 loadCustomAgents
└── ...

internal/swarm/server/
├── handle_agents.go          # 修改：7 个 stub → 真实实现
└── ...
```

## 对应 Python 代码

`jiuwenswarm/server/runtime/agent_config_service.py`

## 实现计划步骤标记

- 第 0 步：品牌名统一（不涉及 IMPLEMENTATION_PLAN.md 章节号）
- 第 1-7 步：对应 IMPLEMENTATION_PLAN.md 10.3.13
- 回填点：`deep_adapter_config.go:TODO(#custom-subagents)` 标记为 ⤴️
