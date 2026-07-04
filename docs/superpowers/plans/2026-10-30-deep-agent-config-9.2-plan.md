# 9.2 DeepAgentConfig + harness_config YAML 体系 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 DeepAgentConfig 运行时配置体系 + harness_config YAML 配置体系（schema/loader/builder/registry），并回填已有代码中的占位类型

**Architecture:** 先实现5个前置依赖类型（AgentMode/PromptMode 枚举 + Workspace 结构体 + SysOperation 接口 + PermissionsSection），然后实现 DeepAgentConfig + 辅助类型，最后实现 harness_config/ YAML 体系 + 所有回填点

**Tech Stack:** Go 1.22+, gopkg.in/yaml.v3, text/template (标准库), testify/assert

**设计文档:** `docs/superpowers/specs/2026-10-30-deep-agent-config-9.2-design.md`

---

## 文件结构

### 新建文件

```
internal/agentcore/sys_operation/
├── doc.go                              # 包文档
├── sys_operation.go                    # SysOperation/FsOperation/ShellOperation/CodeOperation 接口 + 枚举
├── sys_operation_card.go              # SysOperationCard + WorkConfig 类型
├── sys_operation_test.go
└── sys_operation_card_test.go

internal/agentcore/harness/schema/
├── doc.go                              # 包文档
├── agent_mode.go                       # AgentMode 枚举
├── agent_mode_test.go
├── prompt_mode.go                      # PromptMode 枚举
├── prompt_mode_test.go
├── config.go                           # DeepAgentConfig + VisionModelConfig + AudioModelConfig + SubAgentConfig + ModelSelectionEntry
└── config_test.go

internal/agentcore/harness/workspace/
├── doc.go                              # 包文档
├── workspace.go                        # Workspace + WorkspaceNode + DirectoryNode + 默认 schema + 校验
└── workspace_test.go

internal/agentcore/harness/security/
├── doc.go                              # 包文档
├── models.go                           # PermissionLevel + PermissionResult + PermissionConfirmResponse + PermissionsSection
└── models_test.go

internal/agentcore/harness/harness_config/
├── doc.go                              # 包文档
├── schema.go                           # 10 个 YAML Schema 结构体
├── schema_test.go
├── loader.go                           # HarnessConfigLoader + ResolvedSection + ResolvedFileSection + ResolvedHarnessConfig
├── loader_test.go
├── builder.go                          # HarnessConfigBuilder + 内置注册表 + GenerateHarnessConfigYAML
├── builder_test.go
├── registry.go                         # HarnessConfigInfo + HarnessConfigRegistry
└── registry_test.go
```

### 修改文件（回填）

```
internal/agentcore/context_engine/interface/types.go          # SysOperation any → sysop.SysOperation, Workspace any → *workspace.Workspace
internal/agentcore/context_engine/interface/processor.go      # SysOperation any → sysop.SysOperation
internal/agentcore/context_engine/engine.go                   # sysOperation any → sysop.SysOperation, workspace any → *workspace.Workspace
internal/agentcore/context_engine/context/message_buffer.go   # sysOperation any → sysop.SysOperation
internal/agentcore/context_engine/context/session_model_context.go  # sysOperation any → sysop.SysOperation
internal/agentcore/context_engine/processor/offload.go        # writeOffloadToFile 逻辑回填
internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go  # sysOperation any → sysop.SysOperation
internal/agentcore/runner/resources_manager/sys_operation_manager.go  # any → sysop.SysOperation + 实现逻辑回填
internal/agentcore/runner/resources_manager/resource_manager.go       # AddSysOperation/registerSysOperationTools 逻辑回填
internal/agentcore/single_agent/config/agent_config.go               # Workspace any → *workspace.Workspace
internal/agentcore/single_agent/prompts/builder.go                   # 添加 NewSystemPromptBuilderWithPromptMode
```

---

## Task 1: SysOperation 接口定义

**Files:**
- Create: `internal/agentcore/sys_operation/doc.go`
- Create: `internal/agentcore/sys_operation/sys_operation.go`
- Create: `internal/agentcore/sys_operation/sys_operation_card.go`
- Create: `internal/agentcore/sys_operation/sys_operation_test.go`
- Create: `internal/agentcore/sys_operation/sys_operation_card_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package sys_operation 提供系统操作抽象接口与配置类型。
//
// SysOperation 是 DeepAgent 对文件系统、Shell、代码执行等系统级操作的统一抽象。
// 具体实现分为 LocalSysOperation（本地执行）和 SandboxSysOperation（沙箱执行），
// 由 OperationMode 决定。
//
// 文件目录：
//
//	sys_operation/
//	├── doc.go                   # 包文档
//	├── sys_operation.go         # SysOperation/FsOperation/ShellOperation/CodeOperation 接口 + 枚举
//	└── sys_operation_card.go   # SysOperationCard + WorkConfig 类型
//
// 对应 Python 代码：openjiuwen/core/sys_operation/
package sys_operation
```

- [ ] **Step 2: 创建 sys_operation.go — 接口 + 枚举**

包含：
- `OperationMode` 枚举（LOCAL=0, SANDBOX=1）+ String/MarshalJSON/UnmarshalJSON
- `ShellType` 枚举（AUTO=0, CMD=1, POWERSHELL=2, BASH=3, SH=4）+ String
- `ContainerScope` 枚举（SYSTEM=0, SESSION=1, CUSTOM=2）+ String
- `FsOperation` 接口（ReadFile/WriteFile/ListFiles/ListDirectories/SearchFiles/ListTools）
- `ShellOperation` 接口（ExecuteCmd/ListTools）
- `CodeOperation` 接口（ExecuteCode/ListTools）
- `SysOperation` 接口（Card/Fs/Shell/Code/IsolationKeyTemplate）
- `BaseSysOperation` 空实现桩（所有方法返回 nil/err，供嵌入使用）
- `BaseFsOperation` 空实现桩
- `BaseShellOperation` 空实现桩
- `BaseCodeOperation` 空实现桩

遵循规范：中文注释、声明排列顺序（接口→枚举→常量→全局变量→导出函数→非导出函数）

- [ ] **Step 3: 创建 sys_operation_card.go — Card + WorkConfig 类型**

包含：
- `LocalWorkConfig` 结构体（ShellAllowlist []string, SandboxRoot string, RestrictToSandbox bool, DangerousPatterns []string）
- `SandboxIsolationConfig` 结构体（CustomID string, ContainerScope ContainerScope, Prefix string）
- `SandboxLauncherConfig` 结构体（LauncherType string, GatewayURL string, SandboxType string, OnStop string, IdleTTLSeconds int, ExtraParams map[string]any）
- `SandboxGatewayConfig` 结构体（Isolation SandboxIsolationConfig, LauncherConfig SandboxLauncherConfig, TimeoutSeconds float64, AuthHeaders map[string]string, AuthQueryParams map[string]string）
- `SysOperationCard` 结构体（嵌入 schema.BaseCard，Mode OperationMode, WorkConfig *LocalWorkConfig, GatewayConfig *SandboxGatewayConfig, isolationKeyTemplate string）
- `SysOperationCard` 方法：`GenerateToolID(opType, methodName string) string`, `IsolationKeyTemplate() string`, `Fs()/Shell()/Code()` 返回 ToolIdProxy
- `ToolIdProxy` 类型及方法（动态属性访问生成 tool ID）
- `NewSysOperationCard()` 构造函数 + Option 函数
- `generateIsolationKeyTemplate()` 函数

- [ ] **Step 4: 创建测试文件**

`sys_operation_test.go`: 枚举 String/MarshalJSON/UnmarshalJSON 测试、接口 nil 安全测试
`sys_operation_card_test.go`: 构造函数、GenerateToolID、IsolationKeyTemplate、WorkConfig 默认值测试

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -count=1
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/sys_operation/ && git commit -m "feat(sys_operation): 添加 SysOperation 接口、枚举和配置类型定义 (9.2 前置)"
```

---

## Task 2: AgentMode + PromptMode 枚举

**Files:**
- Create: `internal/agentcore/harness/schema/doc.go`
- Create: `internal/agentcore/harness/schema/agent_mode.go`
- Create: `internal/agentcore/harness/schema/agent_mode_test.go`
- Create: `internal/agentcore/harness/schema/prompt_mode.go`
- Create: `internal/agentcore/harness/schema/prompt_mode_test.go`

- [ ] **Step 1: 创建 doc.go**

- [ ] **Step 2: 创建 agent_mode.go**

```go
// AgentMode DeepAgent 运行模式枚举
type AgentMode int

const (
    // AgentModeNormal 普通执行模式（默认）
    AgentModeNormal AgentMode = iota
    // AgentModePlan 只读规划模式
    AgentModePlan
)
```

+ String()、MarshalJSON()、UnmarshalJSON()、从字符串解析 ParseAgentMode()

- [ ] **Step 3: 创建 agent_mode_test.go**

测试：默认值=Normal、String()输出、JSON序列化/反序列化、ParseAgentMode()

- [ ] **Step 4: 创建 prompt_mode.go**

```go
// PromptMode 系统提示词过滤模式
type PromptMode int

const (
    // PromptModeFull 完整提示词（不过滤）
    PromptModeFull PromptMode = iota
    // PromptModeMinimal 精简提示词（仅保留 priority <= 20）
    PromptModeMinimal
    // PromptModeNone 无提示词（不注入系统提示词）
    PromptModeNone
)
```

+ String()、MarshalJSON()、UnmarshalJSON()、ParsePromptMode()

- [ ] **Step 5: 创建 prompt_mode_test.go**

- [ ] **Step 6: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/schema/... -v -count=1
git add internal/agentcore/harness/schema/ && git commit -m "feat(harness/schema): 添加 AgentMode 和 PromptMode 枚举 (9.2)"
```

---

## Task 3: PermissionsSection 类型

**Files:**
- Create: `internal/agentcore/harness/security/doc.go`
- Create: `internal/agentcore/harness/security/models.go`
- Create: `internal/agentcore/harness/security/models_test.go`

- [ ] **Step 1: 创建 doc.go**

- [ ] **Step 2: 创建 models.go**

包含（对齐 Python `openjiuwen/harness/security/models.py`）：
- `PermissionLevel` 枚举（ALLOW=0, ASK=1, DENY=2）+ String/MarshalJSON
- `PermissionResult` 结构体（Permission PermissionLevel, MatchedRule string, Reason string, ExternalPaths []string）+ IsAllowed/IsDenied/NeedsApproval 属性方法
- `PermissionConfirmResponse` 结构体（Approved bool, Feedback string, AutoConfirm bool）
- `ApprovalOverrideEntry` 结构体（ID string, Tools []string, MatchType string, Pattern string, Action string）
- `PermissionsSection` 结构体（Enabled bool, Schema string, Defaults map[string]any, Tools map[string]any, Rules []map[string]any, ApprovalOverrides []ApprovalOverrideEntry, ExternalDirectory map[string]string）

- [ ] **Step 3: 创建 models_test.go**

测试 PermissionLevel 枚举、PermissionResult 属性方法、PermissionsSection 默认值和序列化

- [ ] **Step 4: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/security/... -v -count=1
git add internal/agentcore/harness/security/ && git commit -m "feat(harness/security): 添加权限模型类型 (9.2)"
```

---

## Task 4: Workspace 结构体

**Files:**
- Create: `internal/agentcore/harness/workspace/doc.go`
- Create: `internal/agentcore/harness/workspace/workspace.go`
- Create: `internal/agentcore/harness/workspace/workspace_test.go`

- [ ] **Step 1: 创建 doc.go**

- [ ] **Step 2: 创建 workspace.go**

包含（对齐 Python `openjiuwen/harness/workspace/workspace.py`）：
- `WorkspaceNode` 枚举（15 个值：AGENT_MD/SOUL_MD/HEARTBEAT_MD/IDENTITY_MD/USER_MD/Memory/CodingMemory/TODO/Messages/Skills/Agents/MemoryMD/DailyMemory/TeamLinks/WorktreeLinks）
- `DirectoryNode` 类型别名 `= map[string]any`
- `Workspace` 结构体（RootPath string, Directories []DirectoryNode, Language string）
- `Workspace` 方法：
  - `GetDirectory(name any) string` — 按名称查找目录路径
  - `SetDirectory(nodes any) error` — 添加/更新目录节点
  - `GetNodePath(node any) *string` — 返回完整绝对路径
  - `GetDefaultDirectory(language string) []DirectoryNode` — 返回默认 schema 副本
- `validateDirectoryNode(node DirectoryNode) error` — 校验 name/path/description/is_file/default_content/children
- `getWorkspaceSchema(language string) []DirectoryNode` — 按语言返回默认 schema
- `defaultWorkspaceSchemaCN` / `defaultWorkspaceSchemaEN` — 中/英默认目录结构变量
- `NewWorkspace()` 构造函数

注意：不含 link 管理（team/worktree symlink），留到 9.50

- [ ] **Step 3: 创建 workspace_test.go**

测试：默认构造、GetDirectory/SetDirectory、GetNodePath、校验错误、中/英 schema 切换

- [ ] **Step 4: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/workspace/... -v -count=1
git add internal/agentcore/harness/workspace/ && git commit -m "feat(harness/workspace): 添加 Workspace 结构体和目录管理 (9.2)"
```

---

## Task 5: DeepAgentConfig + 辅助类型

**Files:**
- Create: `internal/agentcore/harness/schema/config.go`
- Create: `internal/agentcore/harness/schema/config_test.go`

- [ ] **Step 1: 创建 config.go**

包含（对齐 Python `openjiuwen/harness/schema/config.py`）：

**常量：**
```go
const (
    DefaultMaxIterations       = 15
    DefaultCompletionTimeout   = 600.0
    DefaultProgressiveToolMax  = 12
    DefaultLanguage            = "cn"
    DefaultOpenAIBaseURL       = "https://api.openai.com/v1"
    DefaultOpenAIVisionModel   = "gpt-4.1-mini"
    DefaultOpenRouterVisionModel = "google/gemini-2.5-pro"
    // Audio 默认值...
)
```

**结构体：**
- `VisionModelConfig`（APIKey, BaseURL, Model, MaxRetries）+ `FromEnv()` + `NewVisionModelConfig()`
- `AudioModelConfig`（APIKey, BaseURL, TranscriptionModel, QAModel, MaxRetries, HTTPTimeout, MaxAudioBytes, ACR 配置）+ `FromEnv()` + `NewAudioModelConfig()`
- `SubAgentConfig`（AgentCard, SystemPrompt, Tools, Mcps, Model, Rails, Skills, Backend, Workspace, SysOperation, Language, PromptMode, EnableTaskLoop, MaxIterations, FactoryName, FactoryKwargs, EnablePlanMode, RestrictToWorkDir）
- `ModelSelectionEntry`（Model *llm.Model, ModeName string）
- `DeepAgentConfig`（按设计文档字段表定义全部 30+ 字段）+ `NewDeepAgentConfig()` 构造函数
- `DeepAgentConfig` 便捷方法：`EffectiveMaxIterations() int`（0→15）、`EffectiveCompletionTimeout() float64`（0→600）、`EffectiveLanguage() string`（""→"cn"）

遵循编码规范：中文注释、声明排列顺序

- [ ] **Step 2: 创建 config_test.go**

测试：
- `NewDeepAgentConfig` 默认值
- `EffectiveMaxIterations/EffectiveCompletionTimeout/EffectiveLanguage` 便捷方法
- `VisionModelConfig.FromEnv()` 环境变量读取
- `AudioModelConfig.FromEnv()` 环境变量读取
- `SubAgentConfig` 字段完整性
- JSON 序列化/反序列化

- [ ] **Step 3: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/schema/... -v -count=1
git add internal/agentcore/harness/schema/config.go internal/agentcore/harness/schema/config_test.go && git commit -m "feat(harness/schema): 添加 DeepAgentConfig 和辅助类型 (9.2)"
```

---

## Task 6: harness_config YAML Schema

**Files:**
- Create: `internal/agentcore/harness/harness_config/doc.go`
- Create: `internal/agentcore/harness/harness_config/schema.go`
- Create: `internal/agentcore/harness/harness_config/schema_test.go`

- [ ] **Step 1: 创建 doc.go**

- [ ] **Step 2: 创建 schema.go**

10 个结构体，全部带 YAML tag（对齐 Python `harness_config/schema.py`）：

```go
// MetaSchema 治理元数据
type MetaSchema struct { ... }  // Owner, Tags, Visibility

// SectionSchema prompt 节声明
type SectionSchema struct { ... }  // Name, Priority *int, File *string, Content any

// ToolResourceSchema 工具规格
type ToolResourceSchema struct { ... }  // Type, Names, Name, Package, Module, ClassName

// RailResourceSchema Rail 规格
type RailResourceSchema struct { ... }  // Type, Name, Package, Module, ClassName

// SkillsSchema 技能配置
type SkillsSchema struct { ... }  // Dirs, Mode

// McpResourceSchema MCP 规格
type McpResourceSchema struct { ... }  // Type, Command, Args, Env

// ResourcesSchema 资源聚合
type ResourcesSchema struct { ... }  // Tools, Rails, Skills, Mcps

// PromptsSchema prompt 声明
type PromptsSchema struct { ... }  // Sections

// WorkspaceSchema 工作空间
type WorkspaceSchema struct { ... }  // RootPath

// HarnessConfig 顶层 harness_config.yaml 结构
type HarnessConfig struct { ... }  // SchemaVersion, Meta, ID, Name, Description, Workspace, Prompts, Resources, Language, MaxIterations, CompletionTimeout
```

+ `HarnessConfig.ToYAML(outputPath ...string) (string, error)` 方法
+ `ValidateHarnessConfig(cfg *HarnessConfig) error` 校验函数

- [ ] **Step 3: 创建 schema_test.go**

测试：YAML 反序列化/序列化、默认值、Validate 校验、ToYAML 输出

- [ ] **Step 4: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/harness_config/... -v -count=1
git add internal/agentcore/harness/harness_config/ && git commit -m "feat(harness_config): 添加 YAML Schema 结构模型 (9.2)"
```

---

## Task 7: harness_config Loader

**Files:**
- Create: `internal/agentcore/harness/harness_config/loader.go`
- Create: `internal/agentcore/harness/harness_config/loader_test.go`

- [ ] **Step 1: 创建 loader.go**

包含（对齐 Python `harness_config/loader.py`）：

**辅助函数：**
- `normalizeContent(content any) map[string]string` — 归一化 section content 为 {lang: text}
- `renderTemplate(text string, params map[string]any) (string, error)` — text/template 渲染，自动转换 `{{ var }}` → `{{ .Var }}`

**结构体：**
- `ResolvedSection` — 内联 section（Name, Priority, Content）
- `ResolvedFileSection` — 文件型 section（Filename, Content）
- `ResolvedHarnessConfig` — 加载结果（Config, SystemPrompt, ExtraSections, FileSections, SourcePath）

**加载器：**
- `HarnessConfigLoader.Load(path, params, workspaceRoot) (*ResolvedHarnessConfig, error)` — 主方法
  1. 读 YAML 文件 → `yaml.v3` 解析为 `HarnessConfig`
  2. 校验 `ValidateHarnessConfig`
  3. 设置 `workspace_root` 默认值（YAML 文件所在目录）
  4. 遍历 `config.Prompts.Sections`：
     - normalizeContent → renderTemplate
     - 有 `file` → ResolvedFileSection
     - name=="identity" → system_prompt
     - 其他 → ResolvedSection（priority 默认 30）

- [ ] **Step 2: 创建 loader_test.go**

测试：
- normalizeContent（string/map/nil）
- renderTemplate（`{{ workspace_root }}` 替换、无占位符透传）
- Load 完整流程（含 YAML fixture 文件）
- identity section 提取
- file section 分类
- 模板渲染

- [ ] **Step 3: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/harness_config/... -v -count=1
git add internal/agentcore/harness/harness_config/loader.go internal/agentcore/harness/harness_config/loader_test.go && git commit -m "feat(harness_config): 添加 YAML Loader 和模板渲染 (9.2)"
```

---

## Task 8: harness_config Builder

**Files:**
- Create: `internal/agentcore/harness/harness_config/builder.go`
- Create: `internal/agentcore/harness/harness_config/builder_test.go`

- [ ] **Step 1: 创建 builder.go**

包含（对齐 Python `harness_config/builder.py`）：

**内置注册表：**
- `builtinToolGroups` map — group→(modulePath, classNames, needsSysOp)
- `builtinRailRegistry` map — name→dottedPath
- `toolDottedToGroup` / `railDottedToName` — 反向映射

**辅助函数：**
- `resolveBuiltinTools(groupName string, sysOp sysop.SysOperation) ([]*tool.ToolCard, error)`
- `resolveTools(resources *ResourcesSchema, sysOp sysop.SysOperation) ([]*tool.ToolCard, error)`
- `createSysOperation(card *schema.AgentCard) (sysop.SysOperation, error)` — 创建并注册
- `resolveRails(resources *ResourcesSchema) ([]rail.AgentRail, error)`
- `resolveMcps(resources *ResourcesSchema) ([]*mcptypes.McpServerConfig, error)`
- `writeFileSections(fileSections []ResolvedFileSection, workspaceRoot string, language string) error`
- `toolsToYAMLSpecs(tools []*tool.ToolCard) []map[string]any`
- `railsToYAMLSpecs(rails []rail.AgentRail) []map[string]any`

**Builder：**
- `HarnessConfigBuilder.Build(resolved *ResolvedHarnessConfig, model *llm.Model, workspaceRoot ...string) (DeepAgent, error)` — 主方法（9.3 实现前返回桩错误）
- `GenerateHarnessConfigYAML(...)` — 从参数生成 YAML 字符串

注意：Build() 中 `create_deep_agent()` 调用暂用 `fmt.Errorf("create_deep_agent 尚未实现，等待 9.3")`，9.3 后回填

- [ ] **Step 2: 创建 builder_test.go**

测试：内置注册表内容、resolveBuiltinTools 错误场景、resolveMcps 转换、GenerateHarnessConfigYAML 输出、Build 桩错误

- [ ] **Step 3: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/harness_config/... -v -count=1
git add internal/agentcore/harness/harness_config/builder.go internal/agentcore/harness/harness_config/builder_test.go && git commit -m "feat(harness_config): 添加 Builder 和内置工具/Rail 注册表 (9.2)"
```

---

## Task 9: harness_config Registry

**Files:**
- Create: `internal/agentcore/harness/harness_config/registry.go`
- Create: `internal/agentcore/harness/harness_config/registry_test.go`

- [ ] **Step 1: 创建 registry.go**

包含（对齐 Python `harness_config/registry.py`，用 init 注册替代 entry_points）：

- `HarnessConfigInfo` 结构体（ID, Name, Version, PackageName, ConfigPath, Enabled）
- `HarnessConfigRegistry` 全局注册表（用 sync.RWMutex 保护）
  - `registry` map[string]HarnessConfigInfo — 存储
  - `disabled` map[string]bool — 禁用集合
  - `mu` sync.RWMutex
  - `Register(info HarnessConfigInfo)` — 注册
  - `Discover() []HarnessConfigInfo` — 返回已注册且未禁用
  - `Get(configID string) *HarnessConfigInfo` — 按 ID 查找
  - `Load(configID string, model *llm.Model, params map[string]any, workspaceRoot ...string) (DeepAgent, error)` — 便捷方法
  - `Disable(configID string)` / `Enable(configID string)` — 开关
  - `InvalidateCache()` — 清空重新扫描（init 模式下实际是 no-op，保持接口对齐）

- [ ] **Step 2: 创建 registry_test.go**

测试：Register/Discover/Get/Disable/Enable/Load 错误场景

- [ ] **Step 3: 运行测试 + 提交**

```bash
go test ./internal/agentcore/harness/harness_config/... -v -count=1
git add internal/agentcore/harness/harness_config/registry.go internal/agentcore/harness/harness_config/registry_test.go && git commit -m "feat(harness_config): 添加 Registry 注册表 (9.2)"
```

---

## Task 10: 回填 — SysOperation 接口类型替换

**Files:**
- Modify: `internal/agentcore/context_engine/interface/types.go`
- Modify: `internal/agentcore/context_engine/interface/processor.go`
- Modify: `internal/agentcore/context_engine/engine.go`
- Modify: `internal/agentcore/context_engine/context/message_buffer.go`
- Modify: `internal/agentcore/context_engine/context/session_model_context.go`
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go`

- [ ] **Step 1: 替换 context_engine/interface/types.go 中的 SysOperation any**

将 `ContextEngineOptions.SysOperation any` → `SysOperation sysop.SysOperation`
将 `CompressContextOptions.SysOperation any` → `SysOperation sysop.SysOperation`
将 `WithEngineSysOperation(op any)` → `WithEngineSysOperation(op sysop.SysOperation)`
将 `WithCompressSysOperation(op any)` → `WithCompressSysOperation(op sysop.SysOperation)`
添加 import `sysop "internal/agentcore/sys_operation"`
移除相关 `⤵️ 9.32` 注释

- [ ] **Step 2: 替换 context_engine/interface/processor.go**

将 `ProcessorOption.SysOperation any` → `SysOperation sysop.SysOperation`
将 `WithSysOperation(op any)` → `WithSysOperation(op sysop.SysOperation)`
添加 import，移除 `⤵️ 9.32` 注释

- [ ] **Step 3: 替换 context_engine/engine.go**

将 `sysOperation any` 字段 → `sysOperation sysop.SysOperation`
将 `workspace any` 字段 → `workspace *workspace.Workspace`
添加 import，移除 `⤵️ 9.32` 注释

- [ ] **Step 4: 替换 context_engine/context/message_buffer.go**

将 `sysOperation any` → `sysOperation sysop.SysOperation`
将 `SetSysOperation(op any)` → `SetSysOperation(op sysop.SysOperation)`
添加 import

- [ ] **Step 5: 替换 context_engine/context/session_model_context.go**

将 `sysOperation any` 字段和构造参数 → `sysOperation sysop.SysOperation`
添加 import

- [ ] **Step 6: 替换 context_engine/processor/offloader/tool_result_budget_processor.go**

将 `sysOperation any` 字段 → `sysOperation sysop.SysOperation`
将 `WithSysOption` 方法签名更新
添加 import

- [ ] **Step 7: 运行受影响的测试**

```bash
go test ./internal/agentcore/context_engine/... -v -count=1
```

- [ ] **Step 8: 提交**

```bash
git add -u && git commit -m "refactor: 回填 SysOperation 接口类型替换 any (9.2 回填)"
```

---

## Task 11: 回填 — Workspace 类型替换

**Files:**
- Modify: `internal/agentcore/single_agent/config/agent_config.go`
- Modify: `internal/agentcore/context_engine/interface/types.go`
- Modify: `internal/agentcore/context_engine/engine.go`

- [ ] **Step 1: 替换 single_agent/config/agent_config.go**

将 `Workspace any` → `Workspace *workspace.Workspace`
添加 import `workspace "internal/agentcore/harness/workspace"`
移除 `⤵️ 回填` 注释

- [ ] **Step 2: 替换 context_engine/interface/types.go**

将 `ContextEngineOptions.Workspace any` → `Workspace *workspace.Workspace`
添加 import

- [ ] **Step 3: 替换 context_engine/engine.go**

将 `workspace any` → `workspace *workspace.Workspace`（如 Step 10.3 未替换）

- [ ] **Step 4: 运行测试 + 提交**

```bash
go test ./internal/agentcore/single_agent/... ./internal/agentcore/context_engine/... -v -count=1
git add -u && git commit -m "refactor: 回填 Workspace 类型替换 any (9.2 回填)"
```

---

## Task 12: 回填 — SysOperationMgr 实现逻辑

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/sys_operation_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/sys_operation_manager_test.go`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager_test.go`

- [ ] **Step 1: 实现 SysOperationMgr.AddSysOperation**

替换 `return fmt.Errorf("sys operation manager not implemented")` 为：
- 校验 sysOperationID 非空
- 校验不重复
- 写入 sysOperations
- 写入 sandboxKeyOwnerMap（如果 instance.Card().IsolationKeyTemplate() 非空）
类型签名 `AddSysOperation(id string, instance sysop.SysOperation) error`

- [ ] **Step 2: 实现 SysOperationMgr.RemoveSysOperation**

类型签名 `RemoveSysOperation(id string) (sysop.SysOperation, error)`
- 校验 ID
- 从 sysOperations 弹出
- 从 sandboxKeyOwnerMap 清除对应条目

- [ ] **Step 3: 实现 SysOperationMgr.GetSysOperation**

类型签名 `GetSysOperation(id string) (sysop.SysOperation, error)`
- 校验 ID → 查询 → 返回

- [ ] **Step 4: 更新 sysOperations 字段类型**

`*ThreadSafeDict[string, any]` → `*ThreadSafeDict[string, sysop.SysOperation]`

- [ ] **Step 5: 更新 ResourceMgr.AddSysOperation 签名**

`instance any` → `instance sysop.SysOperation`，添加 import

- [ ] **Step 6: 更新相关测试**

修改 `sys_operation_manager_test.go` 和 `resource_manager_test.go` 中使用 `any`/`struct{}{}` 的地方改为使用 mock SysOperation

- [ ] **Step 7: 运行测试**

```bash
go test ./internal/agentcore/runner/resources_manager/... -v -count=1
```

- [ ] **Step 8: 提交**

```bash
git add -u && git commit -m "refactor: 回填 SysOperationMgr 实现逻辑 (9.2 回填)"
```

---

## Task 13: 回填 — writeOffloadToFile 逻辑

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offload.go`

- [ ] **Step 1: 更新 writeOffloadToFile 方法**

对齐 Python `base.py:_write_offload_to_file`：
1. 参数 `sysOperation any` → `sysOperation sysop.SysOperation`
2. 逻辑：如果 `sysOperation != nil && sysOperation.Fs() != nil`，优先用 `sysOperation.Fs().WriteFile()` 写入
3. 失败或 nil 时回退到 os 兜底路径（提取为 `writeOffloadFallback` 内部方法）
4. 移除 `⤵️ 9.32` 注释

- [ ] **Step 2: 运行测试**

```bash
go test ./internal/agentcore/context_engine/processor/... -v -count=1
```

- [ ] **Step 3: 提交**

```bash
git add -u && git commit -m "refactor: 回填 writeOffloadToFile SysOperation 写入逻辑 (9.2 回填)"
```

---

## Task 14: 回填 — PromptMode 构造函数

**Files:**
- Modify: `internal/agentcore/single_agent/prompts/builder.go`
- Modify: `internal/agentcore/single_agent/prompts/builder_test.go`

- [ ] **Step 1: 添加 NewSystemPromptBuilderWithPromptMode**

```go
func NewSystemPromptBuilderWithPromptMode(language string, mode hs.PromptMode) *SystemPromptBuilder {
    switch mode {
    case hs.PromptModeFull:
        return NewSystemPromptBuilder(language)
    case hs.PromptModeMinimal:
        filter := func(sections []PromptSection) []PromptSection {
            var filtered []PromptSection
            for _, s := range sections {
                if s.Priority <= 20 {
                    filtered = append(filtered, s)
                }
            }
            return filtered
        }
        return NewSystemPromptBuilderWithFilter(language, filter)
    case hs.PromptModeNone:
        filter := func(sections []PromptSection) []PromptSection { return nil }
        return NewSystemPromptBuilderWithFilter(language, filter)
    default:
        return NewSystemPromptBuilder(language)
    }
}
```

添加 import `hs "internal/agentcore/harness/schema"`

- [ ] **Step 2: 添加测试**

测试 PromptModeFull 不过滤、PromptModeMinimal 过滤 priority>20、PromptModeNone 返回空

- [ ] **Step 3: 运行测试 + 提交**

```bash
go test ./internal/agentcore/single_agent/prompts/... -v -count=1
git add -u && git commit -m "feat(prompts): 回填 PromptMode 驱动的 SystemPromptBuilder 构造 (9.2 回填)"
```

---

## Task 15: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 2: 全量测试**

```bash
go test -cover ./internal/agentcore/sys_operation/... ./internal/agentcore/harness/... ./internal/agentcore/context_engine/... ./internal/agentcore/single_agent/... ./internal/agentcore/runner/resources_manager/... -v -count=1
```

- [ ] **Step 3: 修复编译/测试问题（如有）**

- [ ] **Step 4: 提交最终状态**

```bash
git add -u && git commit -m "chore: 9.2 DeepAgentConfig 实现完成 — 全量编译测试通过"
```

---

## Task 16: 更新 IMPLEMENTATION_PLAN.md

- [ ] **Step 1: 更新 9.2 状态为 ✅**

同时更新被回填的章节状态注释

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新实现计划 9.2 状态为已完成"
```
