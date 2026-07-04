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
  - `ShellType` 枚举（AUTO / CMD / POWERSHELL / BASH / SH）
  - `ContainerScope` 枚举（SYSTEM / SESSION / CUSTOM）
  - `SysOperationCard` 结构体（嵌入 BaseCard，含 Mode, WorkConfig 字段）
  - `LocalWorkConfig` 结构体（ShellAllowlist, SandboxRoot, RestrictToSandbox, DangerousPatterns）
  - `SandboxGatewayConfig` 结构体（Isolation, LauncherConfig, TimeoutSeconds, AuthHeaders, AuthQueryParams）
  - `SandboxLauncherConfig` 结构体（LauncherType, GatewayURL, SandboxType, OnStop, IdleTTLSeconds, ExtraParams）
  - `SandboxIsolationConfig` 结构体（CustomID, ContainerScope, Prefix）
  - `SysOperation` 接口（核心方法签名，对齐 Python `SysOperation` 的公开方法）

**SysOperation 接口方法（对齐 Python `sys_operation.py`）：**

```go
type SysOperation interface {
    // Card 返回配置卡片
    Card() *SysOperationCard
    // Fs 返回文件系统操作（对齐 Python: sys_operation.fs()）
    Fs() FsOperation
    // Shell 返回 Shell 操作（对齐 Python: sys_operation.shell()）
    Shell() ShellOperation
    // Code 返回代码执行操作（对齐 Python: sys_operation.code()）
    Code() CodeOperation
    // IsolationKeyTemplate 返回沙箱隔离键模板（对齐 Python: sys_operation.isolation_key_template）
    IsolationKeyTemplate() string
}
```

**FsOperation 接口（对齐 Python `BaseFsOperation` 的 10 个方法）：**

```go
type FsOperation interface {
    ReadFile(ctx context.Context, path string, opts ...FsOption) (*ReadFileResult, error)
    WriteFile(ctx context.Context, path string, content string, opts ...FsOption) (*WriteFileResult, error)
    ListFiles(ctx context.Context, path string, opts ...FsOption) (*ListFilesResult, error)
    ListDirectories(ctx context.Context, path string, opts ...FsOption) (*ListDirsResult, error)
    SearchFiles(ctx context.Context, path string, pattern string, opts ...FsOption) (*SearchFilesResult, error)
    // ... Upload/Download/Stream 方法留 9.38 扩展
    ListTools() []*tool.ToolCard
}
```

**ShellOperation 接口（对齐 Python `BaseShellOperation` 的 3 个方法）：**

```go
type ShellOperation interface {
    ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdResult, error)
    // Stream/Background 方法留 9.38 扩展
    ListTools() []*tool.ToolCard
}
```

**CodeOperation 接口（对齐 Python `BaseCodeOperation` 的 2 个方法）：**

```go
type CodeOperation interface {
    ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*ExecuteCodeResult, error)
    // Stream 方法留 9.38 扩展
    ListTools() []*tool.ToolCard
}
```

- **注意**: Fs/Shell/Code 的具体实现（LocalFsOperation, SandboxFsOperation 等）属于 9.32/9.33/9.34 范畴，本次仅定义接口
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

实现 9.2 后，需回填的已有代码。**每个回填点不仅改类型签名，还须按 Python 逻辑补充实现内容。**

#### 4.1 SysOperation 接口回填（any → SysOperation）

**类型替换：**

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

**实现逻辑补充（对齐 Python `base.py:_write_offload_to_file`）：**

`context_engine/processor/offload.go` 的 `writeOffloadToFile` 方法当前逻辑：
```go
// 当前：仅 os 兜底路径
func (p *BaseProcessor) writeOffloadToFile(..., sysOperation any) bool {
    _ = sysOperation  // 暂时忽略
    // 直接用 os.WriteFile 写入
}
```

回填后逻辑（对齐 Python）：
```go
func (p *BaseProcessor) writeOffloadToFile(..., sysOperation sysop.SysOperation) bool {
    // Python: if sys_operation is not None:
    //             await sys_operation.fs().write_file(file_path, content_json)
    //         else:
    //             # fallback: os.makedirs + open().write()
    if sysOperation != nil && sysOperation.Fs() != nil {
        // 优先使用 SysOperation 的 FS 接口写入（支持沙箱场景）
        result, err := sysOperation.Fs().WriteFile(ctx, offloadPath, content, ...)
        if err != nil || result.Code != 0 {
            // SysOperation 写入失败，回退到 os 兜底
            return p.writeOffloadFallback(offloadPath, content)
        }
        return true
    }
    // 兜底路径：直接用 os.WriteFile
    return p.writeOffloadFallback(offloadPath, content)
}
```

#### 4.2 Workspace 回填（any → *Workspace）

| 文件 | 位置 | 变更 |
|------|------|------|
| `single_agent/config/agent_config.go` | `Workspace any` | → `Workspace *workspace.Workspace` |
| `context_engine/interface/types.go` | `ContextEngineOptions.Workspace any` | → `Workspace *workspace.Workspace` |
| `context_engine/engine.go` | `workspace any` 字段 | → `workspace *workspace.Workspace` |

**实现逻辑补充：**

回填后，ContextEngine 的 workspace 字段可用于：
- 获取 workspace 目录路径（`workspace.RootPath`）作为 context offload 的存储位置
- 对齐 Python: `ContextEngine(config, workspace=, sys_operation=)` 传入 workspace 用于路径解析

#### 4.3 SysOperationMgr 回填

**类型替换：**

| 文件 | 位置 | 变更 |
|------|------|------|
| `runner/resources_manager/sys_operation_manager.go` | `sysOperations *ThreadSafeDict[string, any]` | → `*ThreadSafeDict[string, sysop.SysOperation]` |
| `runner/resources_manager/sys_operation_manager.go` | `AddSysOperation(id string, instance any)` | → `AddSysOperation(id string, instance sysop.SysOperation)` |
| `runner/resources_manager/sys_operation_manager.go` | `RemoveSysOperation(id string) (any, error)` | → `RemoveSysOperation(id string) (sysop.SysOperation, error)` |
| `runner/resources_manager/sys_operation_manager.go` | `GetSysOperation(id string) (any, error)` | → `GetSysOperation(id string) (sysop.SysOperation, error)` |

**实现逻辑补充（对齐 Python `resource_manager.py`）：**

当前 `SysOperationMgr` 的三个方法都返回 `fmt.Errorf("sys operation manager not implemented")`。回填后实现实际逻辑：

**`AddSysOperation`**（对齐 Python `SysOperationMgr.add_sys_operation`）：
```go
func (m *SysOperationMgr) AddSysOperation(sysOperationID string, instance sysop.SysOperation) error {
    // 1. 校验 sysOperationID 非空
    if sysOperationID == "" {
        return fmt.Errorf("sys_operation_id 不能为空")
    }
    // 2. 校验不重复
    if m.sysOperations.Has(sysOperationID) {
        return fmt.Errorf("sys_operation %s 已存在", sysOperationID)
    }
    // 3. 写入 sysOperations
    m.sysOperations.Set(sysOperationID, instance)
    // 4. 写入 sandboxKeyOwnerMap（如果 instance 有 isolation key template）
    //    Python: self._sandbox_key_owner_map[key_template] = sys_operation_id
    if card := instance.Card(); card != nil {
        if keyTpl := card.IsolationKeyTemplate(); keyTpl != "" {
            m.mu.Lock()
            m.sandboxKeyOwnerMap[keyTpl] = sysOperationID
            m.mu.Unlock()
        }
    }
    return nil
}
```

**`RemoveSysOperation`**（对齐 Python `SysOperationMgr.remove_sys_operation`）：
```go
func (m *SysOperationMgr) RemoveSysOperation(sysOperationID string) (sysop.SysOperation, error) {
    // 1. 校验 sysOperationID 非空
    if sysOperationID == "" {
        return nil, fmt.Errorf("sys_operation_id 不能为空")
    }
    // 2. 从 sysOperations 弹出实例
    instance, ok := m.sysOperations.Get(sysOperationID)
    if !ok {
        return nil, fmt.Errorf("sys_operation %s 不存在", sysOperationID)
    }
    m.sysOperations.Delete(sysOperationID)
    // 3. 从 sandboxKeyOwnerMap 清除对应条目
    //    Python: 反向查找 key_template → 删除
    m.mu.Lock()
    for keyTpl, ownerID := range m.sandboxKeyOwnerMap {
        if ownerID == sysOperationID {
            delete(m.sandboxKeyOwnerMap, keyTpl)
        }
    }
    m.mu.Unlock()
    return instance, nil
}
```

**`GetSysOperation`**（对齐 Python `SysOperationMgr.get_sys_operation`）：
```go
func (m *SysOperationMgr) GetSysOperation(sysOperationID string) (sysop.SysOperation, error) {
    // 1. 校验 sysOperationID 非空
    if sysOperationID == "" {
        return nil, fmt.Errorf("sys_operation_id 不能为空")
    }
    // 2. 从 sysOperations 查询并返回
    instance, ok := m.sysOperations.Get(sysOperationID)
    if !ok {
        return nil, fmt.Errorf("sys_operation %s 不存在", sysOperationID)
    }
    return instance, nil
}
```

**同时回填 `ResourceMgr` 中 SysOperation 相关方法：**

**`AddSysOperation`**（对齐 Python `ResourceManager.add_sys_operation`）：
```go
func (m *ResourceMgr) AddSysOperation(sysOperationID string, instance sysop.SysOperation, opts ...ResourceOption) error {
    // 1. 校验 ID
    // 2. innerAddResource 写入注册表
    // 3. ⤵️ 预留：9.32 实现后补充 registerSysOperationTools 调用
    //    Python: add 成功后自动调用 _register_sys_operation_tools
}
```

**`registerSysOperationTools`**（对齐 Python `ResourceManager._register_sys_operation_tools`）：
```go
func (m *ResourceMgr) registerSysOperationTools(card *sysop.SysOperationCard, instance sysop.SysOperation, tag Tag) {
    // Python 逻辑：
    //   1. SysOperationToolAdapter.ExtractTools(card, instance) → []ToolAdapterEntry
    //   2. 对每个 (toolID, localFunc)：m.innerAddResource(toolID, "tool", localFunc, ...)
    //   3. m.tool().AddSysOperationTools(card.ID, toolIDs)
    // 当前 9.2 阶段：SysOperationToolAdapter 尚未实现（依赖 9.38-49 内置工具集）
    // 保留 ⤵️ 标记，等 9.32/9.38 后回填
}
```

**`GetSysOpToolCards`**（对齐 Python `ResourceManager.get_sys_op_tool_cards`）：
```go
func (m *ResourceMgr) GetSysOpToolCards(sysOperationID string, operationName string, toolName string) ([]*tool.ToolCard, error) {
    // Python 逻辑：
    //   1. 获取 SysOperation 实例
    //   2. 获取对应 operation（如 fs/shell/code）的 sub-operation
    //   3. 调用 sub_op.list_tools() 获取 ToolCard 列表
    //   4. 按 operation_name/tool_name 过滤
    // 当前 9.2 阶段：保留 ⤵️ 标记，等 9.32 后回填
}
```

#### 4.4 PromptMode 回填

| 文件 | 位置 | 变更 |
|------|------|------|
| `single_agent/prompts/builder.go` | `sectionsFilter` 钩子 | 添加 `NewSystemPromptBuilderWithPromptMode(mode PromptMode)` 构造函数 |

**实现逻辑补充（对齐 Python `prompts/builder.py`）：**

Python 中 `SystemPromptBuilder.__init__(language, mode)` 根据 PromptMode 构建 sectionsFilter：
- `FULL` → 不过滤，返回所有 sections
- `MINIMAL` → 只保留 priority <= 20 的 sections（identity + soul + 心跳等核心）
- `NONE` → 返回空 sections（不注入系统提示词）

回填后 Go 实现：
```go
func NewSystemPromptBuilderWithPromptMode(language string, mode PromptMode) *SystemPromptBuilder {
    switch mode {
    case PromptModeFull:
        return NewSystemPromptBuilder(language)  // 默认不过滤
    case PromptModeMinimal:
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
    case PromptModeNone:
        filter := func(sections []PromptSection) []PromptSection {
            return nil
        }
        return NewSystemPromptBuilderWithFilter(language, filter)
    default:
        return NewSystemPromptBuilder(language)
    }
}
```

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

### 六、回填标注总表

所有 ⤵️ 回填点在代码中已标注，便于后续章节实现时搜索定位。

**待回填（⤵️）：**

| 标注位置 | ⤵️ 章节 | 回填目标 | 当前类型/实现 |
|----------|---------|----------|--------------|
| `harness/schema/config.go` SingleAgentConfig.Backend | 9.3 | BackendProtocol 接口 | `any` |
| `harness/schema/config.go` DeepAgentConfig.Backend | 9.3 | BackendProtocol 接口 | `any` |
| `harness/schema/config.go` DeepAgentConfig.PermissionHost | 9.1 | PermissionHostCallback 接口 | `any` |
| `harness/harness_config/builder.go` Build | 9.3 | create_deep_agent 调用 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveBuiltinTools | 9.38 | 内置工具实例化 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveTools "package" | 9.38 | 包级工具加载 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveTools "entry_point" | 9.38 | entry_point 工具加载 | 返回 error 桩 |
| `harness/harness_config/builder.go` createSysOperation | 9.32 | LocalSysOperation 创建 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveRails "builtin" | 9.19-9.24 | 内置 Rail 实例化 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveRails "package" | 9.19-9.24 | 包级 Rail 加载 | 返回 error 桩 |
| `harness/harness_config/builder.go` resolveRails "entry_point" | 9.19-9.24 | entry_point Rail 加载 | 返回 error 桩 |
| `runner/resources_manager/resource_manager.go` RemoveSysOperation | 9.32 | 关联工具清理逻辑 | 缺少清理步骤 |
| `runner/resources_manager/resource_manager.go` GetSysOpToolCards | 9.32 | SysOperation 工具卡片获取 | 返回 error 桩 |
| `runner/resources_manager/resource_manager.go` registerSysOperationTools | 9.32 | SysOperation 工具自动注册 | 空实现 |

**已完成回填（✅）：**

| 回填点 | 原类型 | 新类型 | 文件 |
|--------|--------|--------|------|
| SysOperation 字段 | `any` | `sysop.SysOperation` | context_engine/interface/types.go, processor.go, engine.go, context/message_buffer.go, context/session_model_context.go, processor/offloader/tool_result_budget_processor.go |
| Workspace 字段 | `any` | `*hworkspace.Workspace` | context_engine/interface/types.go, engine.go, context/session_model_context.go, single_agent/config/agent_config.go |
| GetSysOperation 返回 | `[]any` | `[]sysop.SysOperation` | runner/resources_manager/resource_manager.go |
| writeOffloadToFile 优先路径 | `os.WriteFile` | SysOperation.Fs().WriteFile() 优先 | context_engine/processor/offload.go |
| PromptMode 过滤 | 无 | FULL/MINIMAL/NONE 过滤逻辑 | single_agent/prompts/builder.go |
| SysOperationMgr 方法 | error 桩 | 完整实现（Add/Remove/Get） | runner/resources_manager/sys_operation_manager.go |
