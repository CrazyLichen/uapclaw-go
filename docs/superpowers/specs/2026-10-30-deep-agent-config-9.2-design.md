# 9.2 DeepAgentConfig + harness_config YAML 体系设计

> 对应 Python: `openjiuwen/harness/schema/config.py` + `openjiuwen/harness/harness_config/`

## 流程位置与作用

在 Agent 会话链路中，DeepAgentConfig 位于 **DeepAgent 创建→配置→运行** 的中枢位置：

```
harness_config.yaml (或代码直接构造)
       │
       ▼
DeepAgentConfig ──→ create_deep_agent() (9.3 Factory)
       │                 │
       │                 ▼
       │           DeepAgent.configure(config) (9.1)
       │                 │
       │                 ▼
       │          ReActAgent + Rails + Tools + Skills + SubAgents 组装
       │                 │
       ▼                 ▼
  LoopCoordinator (9.5 ✅) → TaskLoopController (9.4) → 运行
```

**核心作用：**
1. **运行时配置中枢** — DeepAgent 所有可配置参数的唯一来源
2. **连接 YAML ↔ 运行时的桥梁** — harness_config.yaml 通过 Loader→Builder→DeepAgentConfig 变为 DeepAgent 实例
3. **热重载载体** — 已运行的 DeepAgent 通过 load_harness_config() 热加载新配置

## 设计决策

### 决策 1：可选字段表达方式

**选择：混合方式 — 引用类型用 nil + 值类型用零值即默认**

- 引用类型（struct/interface/slice/map）：nil 表示未设置
  - `Model *Model`、`Workspace *Workspace`、`Rails []AgentRail`、`Tools []*ToolCard`
- 值类型（int/float/bool/string）：零值即默认值
  - `MaxIterations int` — 0 表示用默认值 15
  - `CompletionTimeout float64` — 0 表示用默认值 600.0
  - `EnableTaskLoop bool` — false 即默认
- 枚举：零值即默认值
  - `DefaultMode AgentMode` — 0 即 NORMAL

**Why:** MaxIterations 等字段逻辑上不可能为 0，零值天然表达"用默认值"，无需指针包装。

### 决策 2：YAML 模板渲染

**选择：text/template + 占位符格式转换**

- Go 标准库 `text/template` 替代 Python jinja2
- 渲染前自动将 `{{ var }}` 转换为 `{{ .Var }}` 格式
- 支持 `{{ workspace_root }}` 等运行时变量替换

### 决策 3：Entry Points 发现机制

**选择：注册表模式（init 注册）**

- 第三方包在 `init()` 中调用 `HarnessConfigRegistry.Register(info)`
- 主程序 blank import 触发注册
- 与项目已有 ModelClient 注册模式一致

### 决策 4：目录结构（与 Python 对齐）

| Python 路径 | Go 路径 |
|------------|---------|
| `openjiuwen/harness/schema/` | `internal/agentcore/harness/schema/` |
| `openjiuwen/harness/workspace/` | `internal/agentcore/harness/workspace/` |
| `openjiuwen/harness/security/` | `internal/agentcore/harness/security/` |
| `openjiuwen/core/sys_operation/` | `internal/agentcore/sys_operation/` |
| `openjiuwen/harness/harness_config/` | `internal/agentcore/harness/harness_config/` |

## 实现内容

### 一、前置依赖类型（先实现）

#### 1.1 AgentMode 枚举

- **Go 路径**: `internal/agentcore/harness/schema/agent_mode.go`
- **Python**: `openjiuwen/harness/schema/agent_mode.py`
- **内容**:
  ```go
  type AgentMode int
  const (
      AgentModeNormal AgentMode = iota  // 普通执行模式（默认）
      AgentModePlan                     // 只读规划模式
  )
  ```
- **方法**: `String()`, `MarshalJSON()`, `UnmarshalJSON()`

#### 1.2 PromptMode 枚举

- **Go 路径**: `internal/agentcore/harness/schema/prompt_mode.go`
- **Python**: harness 层概念（FULL/MINIMAL/NONE）
- **内容**:
  ```go
  type PromptMode int
  const (
      PromptModeFull    PromptMode = iota  // 完整提示词
      PromptModeMinimal                     // 精简提示词
      PromptModeNone                        // 无提示词
  )
  ```
- **回填**: 驱动 `SystemPromptBuilder.sectionsFilter` 的构造

#### 1.3 Workspace 结构体

- **Go 路径**: `internal/agentcore/harness/workspace/`
- **Python**: `openjiuwen/harness/workspace/workspace.py`
- **内容**:
  - `WorkspaceNode` 枚举（AGENT_MD, SOUL_MD, HEARTBEAT_MD, IDENTITY_MD, USER_MD, Memory, CodingMemory, TODO, Messages, Skills, Agents, MemoryMD, DailyMemory, TeamLinks, WorktreeLinks）
  - `DirectoryNode` 类型（map[string]any）
  - `Workspace` 结构体（RootPath, Directories, Language）+ 方法
  - 核心方法：`GetDirectory()`, `SetDirectory()`, `GetNodePath()`, `GetDefaultDirectory()`
  - 目录校验：`validateDirectoryNode()`
  - 中/英默认 schema：`defaultWorkspaceSchemaCN` / `defaultWorkspaceSchemaEN`
  - `getWorkspaceSchema(language)` 函数
- **注意**: 完整 Workspace 对应 9.50，本次先实现核心结构体和基础方法，不实现 link 管理（team/worktree symlink）

#### 1.4 SysOperation 接口定义

- **Go 路径**: `internal/agentcore/sys_operation/`
- **Python**: `openjiuwen/core/sys_operation/`
- **内容**（仅接口+枚举，不含具体实现）:
  - `OperationMode` 枚举（LOCAL / SANDBOX）
  - `SysOperationCard` 结构体（嵌入 BaseCard，含 Mode, WorkConfig 字段）
  - `LocalWorkConfig` 结构体
  - `SandboxWorkConfig` 结构体
  - `SysOperation` 接口（核心方法签名，对齐 Python `SysOperation` 的公开方法）
- **回填**: 将已有代码中 `any` 类型的 SysOperation 字段替换为 `SysOperation` 接口

#### 1.5 PermissionsSection 类型

- **Go 路径**: `internal/agentcore/harness/security/`
- **Python**: `openjiuwen/harness/security/models.py`
- **内容**:
  - `PermissionLevel` 枚举（ALLOW / ASK / DENY）
  - `PermissionResult` 结构体（Permission, MatchedRule, Reason, ExternalPaths）
  - `PermissionConfirmResponse` 结构体（Approved, Feedback, AutoConfirm）
  - `PermissionsSection` 结构体（Enabled, Schema, Defaults, Tools, Rules, ApprovalOverrides, ExternalDirectory）

### 二、DeepAgentConfig 运行时配置（体系①）

- **Go 路径**: `internal/agentcore/harness/schema/config.go`
- **Python**: `openjiuwen/harness/schema/config.py`
- **内容**:

#### DeepAgentConfig 结构体

| 字段 | Go 类型 | 默认值说明 | 依赖 |
|------|---------|-----------|------|
| Model | `*llm.Model` | nil = 未设置 | ✅ 已实现 |
| Card | `*schema.AgentCard` | nil = 未设置 | ✅ 已实现 |
| SystemPrompt | `string` | "" = 未设置 | — |
| ContextEngineConfig | `*ceschema.ContextEngineConfig` | nil = 未设置 | ✅ 已实现 |
| EnableTaskLoop | `bool` | false | — |
| EnableAsyncSubagent | `bool` | false | — |
| AddGeneralPurposeAgent | `bool` | false | — |
| MaxIterations | `int` | 0=用默认15 | — |
| Subagents | `[]SubAgentConfig` | nil = 未设置 | — |
| Tools | `[]*tool.ToolCard` | nil = 未设置 | ✅ 已实现 |
| Mcps | `[]*mcptypes.McpServerConfig` | nil = 未设置 | ✅ 已实现 |
| Workspace | `*workspace.Workspace` | nil = 未设置 | 本次实现 |
| Skills | `[]string` | nil = 未设置 | — |
| EnableSkillDiscovery | `bool` | false | — |
| Backend | `any` | nil = 未设置 | — |
| SysOperation | `sysop.SysOperation` | nil = 未设置 | 本次接口 |
| AutoCreateWorkspace | `bool` | true | — |
| CompletionTimeout | `float64` | 0=用默认600.0 | — |
| Language | `string` | "" = 用默认"cn" | — |
| PromptMode | `PromptMode` | 0 = PromptModeFull | 本次实现 |
| VisionModelConfig | `*VisionModelConfig` | nil = 未设置 | — |
| AudioModelConfig | `*AudioModelConfig` | nil = 未设置 | — |
| EnableReadImageMultimodal | `bool` | true | — |
| Rails | `[]rail.AgentRail` | nil = 未设置 | ✅ 已实现 |
| EnablePlanMode | `bool` | false | — |
| ModelSelection | `[]ModelSelectionEntry` | nil = 未设置 | — |
| ProgressiveToolEnabled | `bool` | false | — |
| ProgressiveToolAlwaysVisibleTools | `[]string` | nil | — |
| ProgressiveToolDefaultVisibleTools | `[]string` | nil | — |
| ProgressiveToolMaxLoadedTools | `int` | 0=用默认12 | — |
| DefaultMode | `AgentMode` | 0 = AgentModeNormal | 本次实现 |
| Permissions | `*security.PermissionsSection` | nil = 未设置 | 本次实现 |
| PermissionHost | `any` | nil = 未设置 | — |

#### 辅助类型

- `VisionModelConfig` — APIKey, BaseURL, Model, MaxRetries + `FromEnv()` 构造
- `AudioModelConfig` — APIKey, BaseURL, TranscriptionModel, QAModel, MaxRetries, HTTPTimeout, MaxAudioBytes, ACR 配置 + `FromEnv()` 构造
- `SubAgentConfig` — AgentCard, SystemPrompt, Tools, Mcps, Model, Rails, Skills, Backend, Workspace, SysOperation, Language, PromptMode, EnableTaskLoop, MaxIterations, FactoryName, FactoryKwargs, EnablePlanMode, RestrictToWorkDir
- `ModelSelectionEntry` — Model *llm.Model + ModeName string，替代 Python `Dict[Model, str]`（Go 中 Model 含指针不可做 map key，改用 slice）

### 三、harness_config YAML 体系（体系②）

#### 3.1 schema.go — YAML 结构模型

- **Go 路径**: `internal/agentcore/harness/harness_config/schema.go`
- **Python**: `openjiuwen/harness/harness_config/schema.py`
- **内容**: 10 个结构体，全部带 YAML tag

```
HarnessConfig        ← 顶层 YAML 结构
├── MetaSchema       ← 治理元数据（owner, tags, visibility）
├── SectionSchema    ← prompt section（name, priority, file, content）
├── ToolResourceSchema ← 工具规格（type, names/name, package, module, class_name）
├── RailResourceSchema ← Rail 规格（type, name, package, module, class_name）
├── SkillsSchema     ← 技能配置（dirs, mode）
├── McpResourceSchema ← MCP 规格（type, command, args, env）
├── ResourcesSchema  ← 资源聚合（tools, rails, skills, mcps）
├── PromptsSchema    ← prompt 声明（sections）
└── WorkspaceSchema  ← 工作空间（root_path）
```

- `HarnessConfig.ToYAML()` 方法 — 序列化为 YAML 字符串

#### 3.2 loader.go — YAML 解析 + 验证 + 模板渲染

- **Go 路径**: `internal/agentcore/harness/harness_config/loader.go`
- **Python**: `openjiuwen/harness/harness_config/loader.py`
- **内容**:
  - `normalizeContent()` — 内容归一化为 `map[string]string`（{lang: text}）
  - `renderTemplate()` — 用 `text/template` 渲染，自动转换 `{{ var }}` → `{{ .Var }}`
  - `ResolvedSection` — 内联 prompt section
  - `ResolvedFileSection` — 文件型 prompt section
  - `ResolvedHarnessConfig` — 加载结果（Config, SystemPrompt, ExtraSections, FileSections, SourcePath）
  - `HarnessConfigLoader.Load()` — 主方法：读 YAML → 校验 → 渲染模板 → 分类 sections

#### 3.3 builder.go — ResolvedHarnessConfig → DeepAgent 组装

- **Go 路径**: `internal/agentcore/harness/harness_config/builder.go`
- **Python**: `openjiuwen/harness/harness_config/builder.py`
- **内容**:
  - 内置工具组注册表 `builtinToolGroups`（filesystem/shell/code/web_search/web_fetch）
  - 内置 Rail 注册表 `builtinRailRegistry`（task_planning）
  - 反向映射 `toolDottedToGroup` / `railDottedToName`
  - `resolveBuiltinTools()` — 按组名解析内置工具
  - `loadDottedPath()` — 从 Go 包路径加载类（通过注册表而非 importlib）
  - `resolveTools()` — 解析所有工具（builtin/package/entry_point）
  - `createSysOperation()` — 创建并注册 SysOperation
  - `resolveRails()` — 解析 Rails
  - `resolveMcps()` — 解析 MCP 配置
  - `writeFileSections()` — 写文件型 section 到 workspace
  - `toolsToYAMLSpecs()` / `railsToYAMLSpecs()` — 反向映射用于 YAML 生成
  - `GenerateHarnessConfigYAML()` — 从参数生成 YAML 字符串
  - `HarnessConfigBuilder.Build()` — 主方法：解析→组装→注入→返回 DeepAgent
- **注意**: `Build()` 中对 `create_deep_agent()` 的调用暂用接口桩，9.3 实现后回填

#### 3.4 registry.go — 注册表 + 发现

- **Go 路径**: `internal/agentcore/harness/harness_config/registry.go`
- **Python**: `openjiuwen/harness/harness_config/registry.py`
- **内容**:
  - `HarnessConfigInfo` — 注册信息（ID, Name, Version, PackageName, ConfigPath, Enabled）
  - `HarnessConfigRegistry` — 全局注册表
    - `Register(info)` — 注册（init 中调用）
    - `Discover()` — 返回所有已注册且启用的 config
    - `Get(configID)` — 按 ID 查找
    - `Load(configID, model, ...)` — 便捷方法：发现→加载→构建
    - `Disable(configID)` / `Enable(configID)` — 开关
    - `InvalidateCache()` — 刷新缓存

### 四、回填点

实现 9.2 后，需回填的已有代码：

#### 4.1 SysOperation 接口回填（any → SysOperation）

| 文件 | 位置 | 变更 |
|------|------|------|
| `context_engine/interface/types.go` | `ContextEngineOptions.SysOperation any` | → `SysOperation sysop.SysOperation` |
| `context_engine/interface/types.go` | `CompressContextOptions.SysOperation any` | → `SysOperation sysop.SysOperation` |
| `context_engine/interface/types.go` | `WithEngineSysOperation(op any)` | → `WithEngineSysOperation(op sysop.SysOperation)` |
| `context_engine/interface/types.go` | `WithCompressSysOperation(op any)` | → `WithCompressSysOperation(op sysop.SysOperation)` |
| `context_engine/interface/processor.go` | `ProcessorOption.SysOperation any` | → `SysOperation sysop.SysOperation` |
| `context_engine/interface/processor.go` | `WithSysOperation(op any)` | → `WithSysOperation(op sysop.SysOperation)` |
| `context_engine/engine.go` | `sysOperation any` 字段 | → `sysOperation sysop.SysOperation` |
| `context_engine/context/message_buffer.go` | `sysOperation any` 字段 | → `sysOperation sysop.SysOperation` |
| `context_engine/context/message_buffer.go` | `SetSysOperation(op any)` | → `SetSysOperation(op sysop.SysOperation)` |
| `context_engine/context/session_model_context.go` | `sysOperation any` 字段 + 构造参数 | → `sysOperation sysop.SysOperation` |
| `context_engine/processor/offloader/tool_result_budget_processor.go` | `sysOperation any` 字段 | → `sysOperation sysop.SysOperation` |

#### 4.2 Workspace 回填（any → *Workspace）

| 文件 | 位置 | 变更 |
|------|------|------|
| `single_agent/config/agent_config.go` | `Workspace any` | → `Workspace *workspace.Workspace` |
| `context_engine/interface/types.go` | `ContextEngineOptions.Workspace any` | → `Workspace *workspace.Workspace` |
| `context_engine/engine.go` | `workspace any` 字段 | → `workspace *workspace.Workspace` |

#### 4.3 SysOperationMgr 回填

| 文件 | 位置 | 变更 |
|------|------|------|
| `runner/resources_manager/sys_operation_manager.go` | `sysOperations *ThreadSafeDict[string, any]` | → `*ThreadSafeDict[string, sysop.SysOperation]` |
| `runner/resources_manager/sys_operation_manager.go` | `AddSysOperation(id string, instance any)` | → `AddSysOperation(id string, instance sysop.SysOperation)` |
| `runner/resources_manager/sys_operation_manager.go` | `RemoveSysOperation(id string) (any, error)` | → `RemoveSysOperation(id string) (sysop.SysOperation, error)` |
| `runner/resources_manager/sys_operation_manager.go` | `GetSysOperation(id string) (any, error)` | → `GetSysOperation(id string) (sysop.SysOperation, error)` |

#### 4.4 PromptMode 回填

| 文件 | 位置 | 变更 |
|------|------|------|
| `single_agent/prompts/builder.go` | `sectionsFilter` 钩子 | 添加 `NewSystemPromptBuilderWithPromptMode(mode PromptMode)` 构造函数 |

#### 4.5 ReActAgentConfig.SysOperationID — 不回填

`ReActAgentConfig.SysOperationID string` 保持不变。原因：
- ReActAgent 是底层 Agent，通过 string ID 从 `ResourceMgr` 查找 SysOperation
- DeepAgentConfig 是上层配置，直接持有 `SysOperation` 接口实例
- 两个层级职责不同：DeepAgent 层持有实例，ReActAgent 层持有 ID

### 五、文件清单

```
internal/agentcore/
├── sys_operation/                          # 新建（1.4）
│   ├── doc.go
│   ├── sys_operation.go                    # SysOperation 接口 + OperationMode 枚举
│   ├── sys_operation_card.go              # SysOperationCard + WorkConfig 类型
│   ├── sys_operation_test.go
│   └── sys_operation_card_test.go
├── harness/
│   ├── schema/                             # 新建（1.1 + 1.2 + 二）
│   │   ├── doc.go
│   │   ├── agent_mode.go                   # AgentMode 枚举
│   │   ├── agent_mode_test.go
│   │   ├── prompt_mode.go                  # PromptMode 枚举
│   │   ├── prompt_mode_test.go
│   │   ├── config.go                       # DeepAgentConfig + 辅助类型
│   │   └── config_test.go
│   ├── workspace/                          # 新建（1.3）
│   │   ├── doc.go
│   │   ├── workspace.go                    # Workspace 结构体 + WorkspaceNode + 目录管理
│   │   └── workspace_test.go
│   ├── security/                           # 新建（1.5）
│   │   ├── doc.go
│   │   ├── models.go                       # PermissionLevel + PermissionResult + PermissionsSection
│   │   └── models_test.go
│   └── harness_config/                     # 新建（三）
│       ├── doc.go
│       ├── schema.go                       # YAML 结构模型（10 个 Schema）
│       ├── schema_test.go
│       ├── loader.go                       # YAML 解析 + 模板渲染
│       ├── loader_test.go
│       ├── builder.go                      # DeepAgent 组装
│       ├── builder_test.go
│       ├── registry.go                     # 注册表 + 发现
│       └── registry_test.go
```
