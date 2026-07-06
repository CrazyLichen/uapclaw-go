# 9.3 DeepAgent Factory 设计文档

## 概述

实现 `createDeepAgent()` 工厂函数，用于创建并组装 DeepAgent 实例。对应 Python `openjiuwen/harness/factory.py` 中的 `create_deep_agent()` 函数。

## 在 Agent 会话流程中的位置

```
外部入口(IM/HTTP/REPL/stdio)
  → Gateway
  → E2A编码
  → ChannelTransport/WebSocket
  → E2A解码
  → AgentServer
  → ★ DeepAgent Factory (9.3) ★  ← 创建并组装 DeepAgent 实例
    → DeepAgent (9.1 ✅)
      → DeepAgentConfig (9.2 ✅)     ← 配置注入
      → Rail 注册 (9.8-9.24)         ← 安全/功能护栏（占位）
      → TaskLoopController (9.4 ✅)   ← 任务循环驱动
      → ReActAgent (6.11 ✅)          ← 内层 Think-Act-Observe 循环
  → 响应流式返回
```

**核心作用**：Factory 是连接"配置阶段"和"运行阶段"的桥梁，将 DeepAgent 的复杂组装过程（Config、Tools、Rails、SubAgents、SysOperation、Workspace）封装为单一 `createDeepAgent()` 调用。

## 决策记录

### 决策1：Rails 依赖策略

**结论：方案A — Factory 先实现，Rails 用占位**

- Factory 完整实现所有10步流程，默认 Rail 自动添加逻辑保留
- 具体 Rail（SecurityRail/SkillUseRail/SubagentRail/TaskPlanningRail/SysOperationRail）用 BaseRail 占位或跳过
- 用步骤注释 + ⤵️ 回填标记确保不遗漏
- 等 Rails (9.8-9.24) 实现后回填具体类型

**Why**: 保留与 Python 的完整对应，避免 Factory 逻辑缺失导致回填时重写。

### 决策2：SysOperation 依赖策略

**结论：方案A — 完整实现 Card 创建注册 + BaseSysOperation 桩**

- 完整实现 SysOperationCard 创建和 resource_mgr 注册流程
- 获取到的 SysOperation 是 BaseSysOperation 空桩
- 等 9.32 实现 LocalSysOperation 后回填

**Why**: 与 Rails 占位策略一致，Card 注册逻辑不丢失。

### 决策3：Backend 字段处理

**结论：方案B — 保留 any 占位，注释修正**

- Factory 将 Backend 原样透传到 DeepAgentConfig
- 保持 `any` 占位不变，注释从 `⤵️ 9.3 回填为 BackendProtocol 接口` 修正为 `P2 预留，等 Backend 实现时回填`
- Python 端 Backend 类型也是 `Any`，零方法调用，零逻辑分支

**Why**: 与 Python `Any` 对齐，避免过早定义可能变更的接口。

### 决策4：config_kwargs 额外字段透传

**结论：不实现**

- Go 用 `CreateDeepAgentParams` 结构体显式定义所有参数
- 编译期类型安全，不存在 Python kwargs 的动态属性需求
- 新增 Config 字段时同步在 Params 和 Factory 中加一行赋值即可

**Why**: Go 的理念是显式优于隐式，Python kwargs 在 Go 端无功能缺口。

### 决策5：工具参数设计

**结论：只支持 []tool.Tool**

- Factory 参数：`ToolInstances []tool.Tool`
- `normalizeTools` 函数：提取 Card 列表 + 过滤 disabled free_search
- Python 所有实际调用路径传入的都是 Tool 实例，纯 ToolCard 未被使用

**Why**: 简化设计，与 Python 实际行为等价，无需支持 union type。

### 决策6：通用子 Agent 注入

**结论：方案A — 完整实现逻辑，Rail 用 BaseRail 占位**

- rails 参数类型 `[]rail.AgentRail`（接口切片），直接对应 Python `List[AgentRail]`
- 完整实现：检查是否已存在 → 过滤 SubagentRail → 确保 SysOperationRail → 构建 SubAgentConfig → 注入列表头部
- SubagentRail/SysOperationRail 类型断言逻辑保留，标注回填标记

**Why**: rails 是接口切片，不需要 BaseRail 占位；类型断言逻辑保留确保回填时不遗漏。

### 决策7：默认 Rail 自动添加

**结论：完整实现框架**

- 完整实现 default_rails 列表结构、`_already_provided`、`agent.add_rail()` 调用
- `_already_provided` 用 `reflect.TypeOf` 精确类型匹配（不匹配子类，注释说明 Python 用 issubclass）
- `_collect_disabled_skills_from_state` 完整实现（读 skills_state.json）
- 具体 Rail 构造函数标注 ⤵️ 9.8-9.24 回填

### 决策8：_register_tool_instances

**结论：直接实现**

- `ResourceMgr` 已有 `AddTool`/`GetTool`/`AddResourceTag` 方法
- 逻辑：遍历 Tool 实例 → 检查是否已注册 → 同 ID 不同实例报错 → 否则注册

### 决策9：resolve_language

**结论：调用已有实现 + 修复校验差异**

- Go 端 `ResolveLanguage()` 已实现（优先级：config > env > default）
- **差异**：Go 不校验值是否在 `SupportedLanguages` 中，Python 校验后回退到默认语言
- **修复方案**：在 9.3 实现中修复 `ResolveLanguage()`，增加 `SupportedLanguages` 校验

修复前：
```go
func ResolveLanguage(configLanguage string) string {
    if configLanguage != "" {
        return configLanguage  // ← 不校验，"jp" 也会直接使用
    }
    if envLang := os.Getenv("AGENT_PROMPT_LANGUAGE"); envLang != "" {
        return envLang  // ← 不校验
    }
    return DefaultLanguage
}
```

修复后（对齐 Python）：
```go
func ResolveLanguage(configLanguage string) string {
    if configLanguage != "" && isSupportedLanguage(configLanguage) {
        return configLanguage
    }
    if envLang := os.Getenv("AGENT_PROMPT_LANGUAGE"); isSupportedLanguage(envLang) {
        return envLang
    }
    return DefaultLanguage
}

func isSupportedLanguage(lang string) bool {
    for _, supported := range SupportedLanguages {
        if lang == supported {
            return true
        }
    }
    return false
}
```

对应 Python 行为：
```python
def resolve_language(config_language=None):
    if config_language is not None and config_language in SUPPORTED_LANGUAGES:
        return config_language
    env_lang = os.environ.get("AGENT_PROMPT_LANGUAGE")
    if env_lang in SUPPORTED_LANGUAGES:
        return env_lang
    return DEFAULT_LANGUAGE
```

### 决策10：_already_provided 实现

**结论：方案3 — 精确类型匹配**

- 使用 `reflect.TypeOf` 精确比较，不匹配子类
- 注释说明 Python 用 `issubclass`，后续需要可升级为接口断言

## 设计

### 文件组织

```
internal/agentcore/harness/
├── factory.go           # createDeepAgent 工厂函数 + 辅助函数（新增）
├── factory_test.go      # 工厂函数测试（新增）
└── prompts/
    └── builder.go       # ResolveLanguage 校验修复（已有文件修改）
```

### 核心类型

```go
// CreateDeepAgentParams 创建 DeepAgent 的参数集
type CreateDeepAgentParams struct {
    Model                     *llm.Model
    Card                      *schema.AgentCard
    SystemPrompt              string
    ToolInstances             []tool.Tool
    Mcps                      []*mcptypes.McpServerConfig
    Subagents                 []hschema.SubAgentConfig
    Rails                     []rail.AgentRail
    EnableTaskLoop            bool
    EnableAsyncSubagent       bool
    AddGeneralPurposeAgent    bool
    MaxIterations             int
    Workspace                 *workspace.Workspace
    Skills                    []string
    Backend                   any    // P2 预留，等 Backend 实现时回填
    SysOperation              sysop.SysOperation
    Language                  string
    PromptMode                hschema.PromptMode
    VisionModelConfig         *hschema.VisionModelConfig
    AudioModelConfig          *hschema.AudioModelConfig
    EnableReadImageMultimodal bool
    EnableTaskPlanning        bool
    RestrictToWorkDir         bool
    DefaultMode               hschema.AgentMode
    ModelSelection            []hschema.ModelSelectionEntry
    EnableSkillDiscovery      bool
}
```

### 核心函数签名

```go
// CreateDeepAgent 创建并配置 DeepAgent 实例
//
// 对应 Python: openjiuwen/harness/factory.py create_deep_agent()
// 需要 ctx 参数因为 ConfigureDeepConfig 需要 context.Context
// （Python agent.configure(config) 无 ctx，Go 端需要）
func CreateDeepAgent(ctx context.Context, params CreateDeepAgentParams) (*DeepAgent, error)
```

### 辅助函数

```go
// normalizeTools 将 Tool 实例列表拆分为 ToolCard 列表和 Tool 实例列表，过滤被禁用的 free_search
func normalizeTools(tools []tool.Tool) (normalizedCards []*tool.ToolCard, toolInstances []tool.Tool)

// isDisabledFreeSearchTool 检查工具是否为被禁用的 free_search 工具
func isDisabledFreeSearchTool(card *tool.ToolCard) bool

// registerToolInstances 将 Tool 实例注册到全局资源管理器
func registerToolInstances(toolInstances []tool.Tool, tag string) error

// injectGeneralPurposeSubagent 当 add_general_purpose_agent=True 时注入通用子 Agent
func injectGeneralPurposeSubagent(subagents []hschema.SubAgentConfig, ...) []hschema.SubAgentConfig

// buildSysOperation 构建 SysOperation：未提供时自动创建默认 SysOperationCard 并注册
func buildSysOperation(card *schema.AgentCard, sysOp sysop.SysOperation, restrictToWorkDir bool) (sysop.SysOperation, error)

// buildWorkspace 构建 Workspace：字符串/nil/Workspace 实例
func buildWorkspace(ws *workspace.Workspace, wsPath string, language string) *workspace.Workspace

// alreadyProvided 检查调用方是否已显式提供了指定类型的 Rail
func alreadyProvided(rails []rail.AgentRail, target rail.AgentRail) bool

// collectDisabledSkillsFromState 从 skills_state.json 收集被禁用的技能名称
func collectDisabledSkillsFromState(skillsDirs []string) []string
```

### CreateDeepAgent 完整流程（10 步，对齐 Python）

```
1. 默认 AgentCard    — card 为 nil 时创建默认 AgentCard
2. 工具规范化        — normalizeTools()：提取 Card + 过滤 free_search
3. 语言解析          — ResolveLanguage(language)
4. 通用子 Agent 注入 — injectGeneralPurposeSubagent()
5. Workspace 构建    — buildWorkspace()
6. SysOperation 构建 — buildSysOperation()：Card 创建 + resource_mgr 注册
7. DeepAgentConfig 组装 — 所有字段赋值
8. DeepAgent 实例化  — NewDeepAgent(card) + agent.ConfigureDeepConfig(ctx, config)
9. 工具注册          — registerToolInstances() + ability_manager.add()
10. Rail 注册        — 显式 Rails + 默认 Rails 自动添加
```

### 需要回填的位置

| 回填标记 | 依赖章节 | 内容 |
|----------|---------|------|
| `⤵️ 9.8-9.24 回填` | Rails 实现 | SecurityRail/SkillUseRail/SubagentRail/TaskPlanningRail/SysOperationRail 具体实例化 |
| `⤵️ 9.32 回填` | LocalSysOperation | buildSysOperation 中 BaseSysOperation → LocalSysOperation |
| `⤵️ P2 回填` | Backend | config.go 中 Backend any → BackendProtocol 接口 |

### 需要回填的已有占位（13 处源码）

| 文件 | 回填内容 |
|------|---------|
| `deep_agent.go` (7处) | create_deep_agent、load_harness_config、unload_harness_config、resolve_plan_file_path、DirectoryBuilder |
| `deep_agent.go:610` | CreateSubagent default 分支：`create_deep_agent 尚未实现` → 调用 `CreateDeepAgent()` |
| `builder.go` (2处) | Build 方法调用 create_deep_agent |
| `config.go` (2处) | Backend 字段注释修正（不是 9.3 回填，改为 P2 预留） |

### 测试策略

1. **normalizeTools** — 测试 Card 提取、free_search 过滤、空输入
2. **isDisabledFreeSearchTool** — 测试名称匹配 + 启用/禁用状态
3. **registerToolInstances** — 测试正常注册、重复同实例、重复不同实例报错
4. **injectGeneralPurposeSubagent** — 测试 add=false 不注入、已存在不注入、注入到头部、Rail 过滤
5. **buildSysOperation** — 测试提供时直接使用、未提供时自动创建 Card + 注册
6. **buildWorkspace** — 测试 nil/字符串/实例三种输入
7. **alreadyProvided** — 测试匹配/不匹配/空列表
8. **collectDisabledSkillsFromState** — 测试正常读取、文件不存在、JSON 解析失败
9. **ResolveLanguage 校验修复** — 测试不支持的语言值回退到默认、支持的语言值正常返回、空值走环境变量/默认
10. **CreateDeepAgent** — 集成测试：完整10步流程、参数透传、默认值填充

## Python 参考路径

- 核心工厂函数：`openjiuwen/harness/factory.py`
- DeepAgent 本体：`openjiuwen/harness/deep_agent.py`
- DeepAgentConfig：`openjiuwen/harness/schema/config.py`
- Rails：`openjiuwen/harness/rails/`
- Workspace：`openjiuwen/harness/workspace/workspace.py`
- 语言解析：`openjiuwen/harness/prompts/__init__.py` resolve_language()
