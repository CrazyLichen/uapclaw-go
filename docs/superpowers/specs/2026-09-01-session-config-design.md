# 5.12 Session Config 设计文档

## 概述

5.12 实现会话配置体系（SessionConfig），包括环境变量管理、工作流配置索引、Agent 配置存取，以及内置超时配置加载机制。同时回填所有 `⤵️ 5.12` 标记点，将 `Config() any` 升级为具体类型。

对应 Python 源码：`openjiuwen/core/session/config/base.py`

## 在 Agent 会话流程中的位置与作用

```
Agent 生命周期流程：
┌─────────────────────────────────────────────────────────────────────┐
│ 1. 创建阶段                                                        │
│    SessionConfig 创建 → 环境变量加载 → 注入到 Session                │  ← 5.12
│                                                                     │
│ 2. 运行阶段                                                        │
│    BaseSession.Config() → GetEnv(key) → 影响超时/行为决策            │  ← 5.12 提供配置查询
│    NodeSessionFacade.GetEnv() → 组件读取配置                        │  ← 5.12 回填
│    NodeSessionFacade.GetNodeConfig() → 节点级配置                   │  ← 5.12 回填
│                                                                     │
│ 3. 检查点阶段                                                      │
│    Checkpointer.PreWorkflowExecute() → Config().GetEnv(force_del)  │  ← 5.12 提供配置
│                                                                     │
│ 4. 追踪阶段                                                        │
│    Tracer → Config().GetWorkflowConfig() → 获取 WorkflowCard 信息    │  ← 5.12 回填
└─────────────────────────────────────────────────────────────────────┘
```

SessionConfig 是会话的"配置中枢"，承上启下：
- **承上**：在 Session 创建时，加载内置默认值 + 合并 context.Value + 合并 os.Getenv，形成统一配置字典
- **启下**：在运行时，各组件通过 `Config().GetEnv(key)` 读取配置来决定行为
- **额外职责**：持有 WorkflowConfig 和 AgentConfig 的引用，供 GetNodeConfig() 和 AgentID() 等使用

## 实现顺序

5.13 Session Constants → 5.12 Session Config → 回填所有 ⤵️ 5.12 标记点

## 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| WorkflowConfig/AgentConfig 前置依赖 | 先定义接口占位（WorkflowConfigProvider/AgentConfigProvider） | 不阻塞 5.12，6.3/8.15 实现后再做具体类型回填 |
| 环境变量加载机制 | 三层优先级：os.Getenv > context.Value > 内置默认值 | 对齐 Python 的 os.environ > workflow_session_vars > 内置默认值 |
| SessionConfig 包位置 | session/config/ 独立子包 | 与 Python 结构对齐 |
| 常量组织 | 先实现 5.13 Session Constants，5.12 再引用 | 常量是纯值定义，依赖方向 constants → config 单向，无循环依赖 |
| CheckpointerConfigProvider 移除 | 5.12 一并移除 | Config() 返回 SessionConfig 后可直接调用 GetEnv()，无需过渡接口 |
| context.Value 键设计 | 字符串常量键 + 导出 WithEnvs 函数 | 简洁实用 |
| 回调元数据（callback_metadata） | 预留字段 + 先定义 MetadataLike 结构体 | Python 中未使用但已定义类型，预留字段位置 |
| SessionConfig 是否抽象 | 接口 + 默认实现（BuiltinConfigLoader 钩子） | 类似 5.9 EntityHooks 模式，支持定制内置配置加载 |
| 接口定义位置 | SessionConfig/WorkflowConfigProvider/AgentConfigProvider 接口统一在 interfaces/ | 与 BaseSession/Checkpointer 等接口统一管理 |

## 类型分布

### session/interfaces/ — 接口定义

| 类型 | 说明 |
|------|------|
| `SessionConfig` 接口 | 会话配置核心接口 |
| `WorkflowConfigProvider` 接口 | 工作流配置提供者（占位，6.3/8.15 后回填） |
| `AgentConfigProvider` 接口 | Agent 配置提供者（占位，6.3 后回填） |

### session/config/ — 具体实现

| 类型 | 说明 |
|------|------|
| `MetadataLike` 结构体 | 回调元数据（Name + Event） |
| `BuiltinConfigLoader` 钩子接口 | 内置配置加载钩子，对应 Python 的 _load_builtin_configs_ |
| `defaultSessionConfig` 结构体 | SessionConfig 的默认实现 |
| `NewSessionConfig(ctx)` | 工厂函数，创建默认实例 |
| `NewSessionConfigWithLoader(ctx, loader)` | 工厂函数，注入自定义 loader |
| `WithEnvs(ctx, envs)` | context.Value 注入函数 |
| `defaultBuiltinConfigLoader` 结构体 | 默认内置配置加载器 |

### session/constants/ — 常量定义

| 内容 | 说明 |
|------|------|
| 配置键名常量 | 如 `WorkflowExecuteTimeoutKey = "_execute_timeout"` |
| 环境变量键名常量 | 如 `WorkflowExecuteTimeoutEnvKey = "WORKFLOW_EXECUTE_TIMEOUT"` |
| 默认值常量 | 如 `WorkflowExecuteTimeoutDefault = 60.0` |
| 键值映射表 | env_key → config_key |
| 类型映射表 | env_key → "float"/"int"/"bool" |
| 迁移常量 | 从 checkpointer/base.go 迁移 ForceDelWorkflowStateKey、InteractiveInputKey |

## 依赖方向

```
interfaces/          config/            constants/
   │                   │                   │
   │ SessionConfig     │ 实现 interfaces   │ 定义常量
   │ WorkflowConfig    │                   │
   │ AgentConfig       │                   │
   │                   │ ← ─ ─ ─ ─ ─ ─ ─  │
   │                   │   导入 constants   │
   │ ← ─ ─ ─ ─ ─ ─ ─ │                   │
   │   config 实现     │                   │
   │   SessionConfig   │                   │
```

- `interfaces/` 不导入 `config/`，无循环依赖
- `config/` 导入 `interfaces/`（声明实现 SessionConfig 接口）
- `config/` 导入 `constants/`（使用配置键名常量）

## SessionConfig 接口方法

```go
// SessionConfig 会话配置接口。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
type SessionConfig interface {
    // GetEnv 获取环境变量值
    GetEnv(key string, defaultValue ...any) any
    // GetEnvs 获取所有环境变量（深拷贝）
    GetEnvs() map[string]any
    // SetEnvs 合并环境变量
    SetEnvs(envs map[string]any)
    // GetWorkflowConfig 按 workflowID 获取工作流配置
    GetWorkflowConfig(workflowID string) WorkflowConfigProvider
    // GetAgentConfig 获取 Agent 配置
    GetAgentConfig() AgentConfigProvider
    // SetAgentConfig 设置 Agent 配置
    SetAgentConfig(agentConfig AgentConfigProvider)
    // AddWorkflowConfig 添加工作流配置
    AddWorkflowConfig(workflowID string, workflowConfig WorkflowConfigProvider)
}
```

## WorkflowConfigProvider 接口（占位）

```go
// WorkflowConfigProvider 工作流配置提供者接口。
// ⤵️ 8.15 回填：WorkflowConfig 实现后替换为具体类型
type WorkflowConfigProvider interface {
    // 后续由 WorkflowConfig 结构体实现
    // 当前为空接口占位
}
```

## AgentConfigProvider 接口（占位）

```go
// AgentConfigProvider Agent 配置提供者接口。
// ⤵️ 6.3 回填：AgentConfig 实现后替换为具体类型
type AgentConfigProvider interface {
    // ID 获取 Agent ID
    ID() string
}
```

## BuiltinConfigLoader 钩子接口

```go
// BuiltinConfigLoader 内置配置加载钩子接口。
// 对应 Python: Config._load_builtin_configs_
// Go 不支持虚方法分派，通过接口注入实现模板方法模式（同 5.9 EntityHooks 模式）。
type BuiltinConfigLoader interface {
    // LoadBuiltinConfigs 加载内置默认配置到 envs 字典
    LoadBuiltinConfigs(envs map[string]any)
}
```

## defaultSessionConfig 结构体

```go
// defaultSessionConfig SessionConfig 的默认实现。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
type defaultSessionConfig struct {
    // env 环境变量字典
    env map[string]any
    // callbackMetadata 回调元数据（预留，后续回调系统实现时回填）
    callbackMetadata map[string]MetadataLike
    // workflowConfigs 按 workflowID 索引的工作流配置
    workflowConfigs map[string]WorkflowConfigProvider
    // agentConfig Agent 配置
    agentConfig AgentConfigProvider
    // loader 内置配置加载钩子
    loader BuiltinConfigLoader
}
```

## 环境变量加载三层优先级

```
1. 内置默认值（loader.LoadBuiltinConfigs）
     ↓ 覆盖
2. context.Value（ctx.Value(envsContextKey)）
     ↓ 覆盖
3. os.Getenv（进程级环境变量，最高优先级）
```

### 默认内置配置项

| 配置键 | 环境变量键 | 默认值 | 类型 |
|--------|-----------|--------|------|
| `_execute_timeout` | `WORKFLOW_EXECUTE_TIMEOUT` | 60.0 | float |
| `_stream_frame_timeout` | `WORKFLOW_STREAM_FRAME_TIMEOUT` | -1.0 | float |
| `_stream_first_frame_timeout` | `WORKFLOW_STREAM_FIRST_FRAME_TIMEOUT` | -1.0 | float |
| `_comp_stream_call_timeout` | `COMP_STREAM_CALL_TIMEOUT` | -1.0 | float |
| `_stream_input_generator_timeout` | `STREAM_INPUT_GEN_TIMEOUT` | -1.0 | float |
| `_loop_number_max_limit` | `LOOP_NUMBER_MAX_LIMIT` | 1000 | int |
| `_force_del_workflow_state` | `FORCE_DEL_WORKFLOW_STATE` | false | bool |
| `_end_comp_template_render_position_timeout` | （无环境变量） | 5.0 | float |
| `_end_comp_template_branch_render_timeout` | （无环境变量） | 5.0 | float |

### context.Value 传递

```go
const envsContextKey = "session_envs"

// WithEnvs 将请求级环境变量注入到 context 中。
func WithEnvs(ctx context.Context, envs map[string]any) context.Context {
    return context.WithValue(ctx, envsContextKey, envs)
}
```

## 创建方式

```go
// NewSessionConfig 创建默认 SessionConfig 实例。
// 对应 Python: Config()
func NewSessionConfig(ctx context.Context) *defaultSessionConfig {
    return NewSessionConfigWithLoader(ctx, &defaultBuiltinConfigLoader{})
}

// NewSessionConfigWithLoader 创建注入自定义 loader 的 SessionConfig 实例。
func NewSessionConfigWithLoader(ctx context.Context, loader BuiltinConfigLoader) *defaultSessionConfig
```

## 回填清单（15 项）

| # | 文件 | 变更 | 说明 |
|---|------|------|------|
| 1 | `interfaces/interfaces.go` | `Config() any` → `Config() SessionConfig` | 接口返回类型回填 |
| 2 | `interfaces/interfaces.go` | 移除 `CheckpointerConfigProvider` 接口 | 不再需要过渡接口 |
| 3 | `internal/agent_session.go` | `config any` → `config SessionConfig` | 字段类型回填 |
| 4 | `internal/agent_session.go` | `WithConfig(any)` → `WithConfig(SessionConfig)` | 选项参数类型回填 |
| 5 | `internal/agent_session.go` | `AgentID()` 启用 config 获取 agent_config.id | 注释代码启用 |
| 6 | `internal/workflow_session.go` | `config any` → `config SessionConfig` | 字段类型回填 |
| 7 | `internal/workflow_session.go` | 无 parent 时创建 `NewSessionConfig()` | 默认配置创建 |
| 8 | `internal/workflow_session.go` | `NodeConfig()` 实现 get_workflow_config | 节点配置获取 |
| 9 | `agent.go` | config 创建改为 `NewSessionConfig() + SetEnvs()` | 替换 any(s.envs) 占位 |
| 10 | `agent.go` | `GetEnv()`/`GetEnvs()` 改为调用 config 方法 | 消除类型断言 |
| 11 | `workflow.go` | `WithWorkflowSessionParent` 从 `parent.Config().GetEnvs()` 获取 envs | 替换空 map 占位 |
| 12 | `node.go` | `GetEnv()` 委托 `inner.Config().GetEnv(key)` | 替换 nil 返回 |
| 13 | `node.go` | `GetNodeConfig()` 委托 `inner.NodeConfig()` | 替换 nil 返回 |
| 14 | `checkpointer/base.go` | `GetConfigEnv` 改为直接 `session.Config().GetEnv()` | 移除类型断言辅助函数 |
| 15 | `tracer/workflow.go` | `getWorkflowMetadata()` 从 config 获取 WorkflowCard | 提取 version/name |

## Python 对应关系

| Python | Go |
|--------|-----|
| `Config(ABC)` | `SessionConfig` 接口 + `defaultSessionConfig` 实现 |
| `Config._env` | `defaultSessionConfig.env` |
| `Config._callback_metadata` | `defaultSessionConfig.callbackMetadata`（预留） |
| `Config._workflow_configs` | `defaultSessionConfig.workflowConfigs` |
| `Config._agent_config` | `defaultSessionConfig.agentConfig` |
| `Config._load_builtin_configs_` | `BuiltinConfigLoader.LoadBuiltinConfigs` 钩子 |
| `Config.set_envs` | `SessionConfig.SetEnvs` |
| `Config.get_env` | `SessionConfig.GetEnv` |
| `Config.get_envs` | `SessionConfig.GetEnvs` |
| `Config.get_workflow_config` | `SessionConfig.GetWorkflowConfig` |
| `Config.get_agent_config` | `SessionConfig.GetAgentConfig` |
| `Config.set_agent_config` | `SessionConfig.SetAgentConfig` |
| `Config.add_workflow_config` | `SessionConfig.AddWorkflowConfig` |
| `MetadataLike(TypedDict)` | `MetadataLike` 结构体 |
| `workflow_session_vars` (contextvars) | `WithEnvs(ctx, envs)` (context.Value) |
| `_ENV_CONFIG_KEYS` | `constants.EnvConfigKeys` |
| `_ENV_CONFIG_TYPES` | `constants.EnvConfigTypes` |
