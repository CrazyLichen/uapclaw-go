# 6.3 ReActAgentConfig 设计文档

## 概述

实现 ReActAgentConfig 结构体，作为 ReAct Agent 的配置中枢，聚合模型客户端配置、上下文引擎配置、处理器规格等子配置。同时定义 AgentConfig 接口用于 BaseAgent/SessionConfig 的类型回填。

对应 Python: `openjiuwen/core/single_agent/agents/react_agent.py` (ReActAgentConfig)

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 配置风格 | Option 模式 | 与已有 ModelClientConfig/ModelRequestConfig 风格一致，Go-idiomatic |
| workspace 字段 | `any` + ⤵️回填 | 等Workspace接口定义后再改 |
| context_processors 字段 | `[]ProcessorSpec` | 项目已有 ProcessorSpec{Type, Config} 类型，直接引用 |
| 跨字段联动 | 复合 Option 函数 | 如 WithModelClient 同时设置多字段并创建子配置 |
| 回填类型 | AgentConfig 接口 | 最小方法集，支持多 Agent 子类扩展 |
| 文件放置 | AgentConfig接口在 interfaces/，ReActAgentConfig在 schema/ | 接口与实现分离，与 BaseAgent 接口同位置 |
| Python 对齐 | 扁平结构体 | 与 Python Pydantic BaseModel 字段一一对应 |

## 1. AgentConfig 接口

**位置**: `internal/agentcore/single_agent/interfaces/interface.go`

```go
// AgentConfig Agent 配置接口，所有 Agent 配置必须实现。
//
// 定义所有 Agent 子类共有的配置访问方法，
// ReActAgentConfig、ControllerAgentConfig 等具体配置均实现此接口。
//
// ⤵️ 6.3 回填：BaseAgent.Config() 返回 AgentConfig，
// SessionConfig.GetAgentConfig()/SetAgentConfig() 使用 AgentConfig。
type AgentConfig interface {
    // ModelName 返回模型名称
    ModelName() string
    // MemScopeID 返回内存作用域标识
    MemScopeID() string
    // ContextEngineConfig 返回上下文引擎配置
    ContextEngineConfig() contextengineschema.ContextEngineConfig
    // ModelClientConfig 返回模型客户端配置
    ModelClientConfig() *llmschema.ModelClientConfig
}
```

## 2. ReActAgentConfig 结构体

**位置**: `internal/agentcore/single_agent/schema/react_agent_config.go`

### 字段定义（与 Python 一一对应）

```go
// ReActAgentConfig ReAct Agent 配置，聚合模型、上下文、提示词等子配置。
//
// 对应 Python: openjiuwen/core/single_agent/agents/react_agent.py (ReActAgentConfig)
type ReActAgentConfig struct {
    // MemScopeID 内存作用域标识
    MemScopeID string `json:"mem_scope_id"`
    // ModelName 模型名称
    ModelName string `json:"model_name"`
    // ModelProvider 模型提供商
    ModelProvider string `json:"model_provider"`
    // APIKey API 密钥
    APIKey string `json:"api_key"`
    // APIBase API 基础 URL
    APIBase string `json:"api_base"`
    // CustomHeaders 自定义请求头
    CustomHeaders map[string]string `json:"custom_headers,omitempty"`
    // PromptTemplateName 提示词模板名称
    PromptTemplateName string `json:"prompt_template_name"`
    // PromptTemplate 提示词模板列表
    PromptTemplate []map[string]any `json:"prompt_template,omitempty"`
    // MaxIterations ReAct 循环最大迭代次数
    MaxIterations int `json:"max_iterations"`
    // LLMReturnTokenIDs 是否请求 token IDs（RL 用）
    LLMReturnTokenIDs bool `json:"llm_return_token_ids"`
    // LLMLogprobs 是否请求 logprobs
    LLMLogprobs bool `json:"llm_logprobs"`
    // LLMTopLogprobs top_logprobs 数量 (0-20)
    LLMTopLogprobs int `json:"llm_top_logprobs"`
    // ModelClientConfig 模型客户端配置
    ModelClientConfig *llmschema.ModelClientConfig `json:"model_client_config,omitempty"`
    // ModelRequestConfig 模型请求配置（对应 Python model_config_obj）
    ModelRequestConfig *llmschema.ModelRequestConfig `json:"model_config_obj,omitempty"`
    // SysOperationID 系统操作标识
    SysOperationID string `json:"sys_operation_id,omitempty"`
    // ContextEngineConfig 上下文引擎配置
    ContextEngineConfig ceschema.ContextEngineConfig `json:"context_engine_config"`
    // ContextProcessors 上下文处理器规格列表
    ContextProcessors []ceiface.ProcessorSpec `json:"context_processors,omitempty"`
    // Workspace 工作区实例
    // ⤵️ 回填：Workspace 接口定义后改为具体类型
    Workspace any `json:"-"`
}
```

### 默认值

| 字段 | 默认值 | Python 默认值 |
|------|--------|--------------|
| MemScopeID | `""` | `""` |
| ModelName | `""` | `""` |
| ModelProvider | `"openai"` | `"openai"` |
| APIKey | `""` | `""` |
| APIBase | `""` | `""` |
| CustomHeaders | `nil` | `None` |
| PromptTemplateName | `""` | `""` |
| PromptTemplate | `nil` | `[]` |
| MaxIterations | `5` | `5` |
| LLMReturnTokenIDs | `false` | `False` |
| LLMLogprobs | `false` | `False` |
| LLMTopLogprobs | `1` | `1` |
| ModelClientConfig | `nil` | `None` |
| ModelRequestConfig | `nil` | `None` |
| SysOperationID | `""` | `None` |
| ContextEngineConfig | `NewContextEngineConfig()` 并设置 MaxContextMessageNum=200, DefaultWindowRoundNum=10 | `ContextEngineConfig(max_context_message_num=200, default_window_round_num=10)` |
| ContextProcessors | `nil` | `None` |
| Workspace | `nil` | `None` |

## 3. Option 函数

### 基础 Option（逐字段设置）

```go
type ReActAgentConfigOption func(*ReActAgentConfig)

// 字段级
WithMemScopeID(id string) ReActAgentConfigOption
WithModelName(name string) ReActAgentConfigOption
WithModelProvider(provider string) ReActAgentConfigOption
WithAPIKey(key string) ReActAgentConfigOption
WithAPIBase(base string) ReActAgentConfigOption
WithCustomHeaders(headers map[string]string) ReActAgentConfigOption
WithPromptTemplateName(name string) ReActAgentConfigOption
WithPromptTemplate(template []map[string]any) ReActAgentConfigOption
WithMaxIterations(n int) ReActAgentConfigOption
WithLLMReturnTokenIDs(b bool) ReActAgentConfigOption
WithLLMLogprobs(b bool) ReActAgentConfigOption
WithLLMTopLogprobs(n int) ReActAgentConfigOption
WithModelClientConfig(cfg *llmschema.ModelClientConfig) ReActAgentConfigOption
WithModelRequestConfig(cfg *llmschema.ModelRequestConfig) ReActAgentConfigOption
WithSysOperationID(id string) ReActAgentConfigOption
WithContextEngineConfig(cfg ceschema.ContextEngineConfig) ReActAgentConfigOption
WithContextProcessors(procs []ceiface.ProcessorSpec) ReActAgentConfigOption
WithWorkspace(ws any) ReActAgentConfigOption
```

### 复合 Option（跨字段联动）

```go
// WithModelClient 设置模型客户端（复合 Option）。
// 同时设置 ModelProvider/APIKey/APIBase/ModelName，
// 并创建 ModelClientConfig + ModelRequestConfig。
// 对应 Python: ReActAgentConfig.configure_model_client()
WithModelClient(provider, apiKey, apiBase, modelName string, opts ...ModelClientExtraOption) ReActAgentConfigOption

// WithModelProviderDetails 设置模型提供商详情（复合 Option）。
// 同时设置 ModelProvider/APIKey/APIBase，不创建子配置。
// 对应 Python: ReActAgentConfig.configure_model_provider()
WithModelProviderDetails(provider, apiKey, apiBase string) ReActAgentConfigOption

// WithContextEngine 构建并设置上下文引擎配置（复合 Option）。
// 对应 Python: ReActAgentConfig.configure_context_engine()
WithContextEngine(maxMsgNum, windowRoundNum int, enableReload, enableKVCacheRelease bool) ReActAgentConfigOption

// WithCustomHeadersSync 设置自定义请求头并同步到已有 ModelClientConfig（复合 Option）。
// 对应 Python: ReActAgentConfig.configure_custom_headers()
WithCustomHeadersSync(headers map[string]string) ReActAgentConfigOption
```

**ModelClientExtraOption** 用于 WithModelClient 的扩展参数：

```go
type ModelClientExtraOption func(*modelClientExtra)

type modelClientExtra struct {
    verifySSL    bool
    customHeaders map[string]string
}

func WithVerifySSL(verify bool) ModelClientExtraOption
func WithExtraCustomHeaders(headers map[string]string) ModelClientExtraOption
```

## 4. AgentConfig 接口实现

```go
func (c *ReActAgentConfig) ModelName() string             { return c.ModelName }
func (c *ReActAgentConfig) MemScopeID() string            { return c.MemScopeID }
func (c *ReActAgentConfig) ContextEngineConfig() ceschema.ContextEngineConfig { return c.ContextEngineConfig }
func (c *ReActAgentConfig) ModelClientConfig() *llmschema.ModelClientConfig  { return c.ModelClientConfig }
```

## 5. Validate 方法

```go
func (c *ReActAgentConfig) Validate() error
```

校验规则：
- `LLMTopLogprobs` 范围 [0, 20]（对应 Python `ge=0, le=20`）
- `MaxIterations` > 0
- 子配置非 nil 时递归校验（ModelClientConfig.Validate()、ContextEngineConfig.Validate()）

## 6. 回填清单

### 6.1 single_agent/interfaces/interface.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `BaseAgent.Config()` 返回值 | `any` | `AgentConfig` |
| `BaseAgent.Configure()` 参数 | `config any` | `config AgentConfig` |

### 6.2 single_agent/base.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `WarpBaseAgent.config` 字段 | `any` | `AgentConfig` |
| `WarpBaseAgent.Configure()` 参数 | `config any` | `config AgentConfig` |
| `WarpBaseAgent.Config()` 返回值 | `any` | `AgentConfig` |

### 6.3 session/config/config.go

**⚠ 循环依赖限制：** `single_agent/interfaces → session → session/config`，
`session/config` 不能导入 `single_agent/interfaces`，否则形成循环。
因此 config 包中 `GetAgentConfig()/SetAgentConfig()` 签名**保留 `any`**，
通过注释说明调用方应传入 `AgentConfig` 实现者，
`agent_session.go` 中通过类型断言 `ac.(agentConfigProvider)` 获取接口方法。

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `SessionConfig.GetAgentConfig()` 返回值 | `any` | `any`（保留，注释说明应传入 AgentConfig） |
| `SessionConfig.SetAgentConfig()` 参数 | `agentConfig any` | `agentConfig any`（保留，注释说明应传入 AgentConfig） |
| `defaultSessionConfig.agentConfig` 字段 | `any` | `any`（保留） |

### 6.4 session/internal/agent_session.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `AgentSession.AgentID()` 中的类型断言 | `ac.(agentConfigIDProvider)` | 简化逻辑：`GetAgentConfig()` 返回 `AgentConfig` 后，用 `ac.MemScopeID()` 替代类型断言 |

注意：Python 中 `agent_config.id` 访问的是 `ReActAgentConfig` 上未显式定义的 `id` 属性
（`ReActAgentConfig` 无 `id` 字段，Python 代码实际走 `self._card.id` 分支）。
Go 回填后应使用 `MemScopeID()` 作为 agent 配置提供的 ID，与 `mem_scope_id` 语义一致。
若 `MemScopeID()` 为空则 fallback 到 `card.AbilityID()`。

### 6.5 session/config/config_test.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `TestNewSessionConfig_AgentConfig` | 使用 `string` 类型测试 | 改用 `*ReActAgentConfig` 实例测试 |

### 6.6 single_agent/base_test.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `TestWarpBaseAgent_Configure` | 使用 `map[string]any` 作为 config | 改用 `*ReActAgentConfig` 实例 |
| `TestWarpBaseAgent_访问器` | Config 类型断言为 `map[string]any` | 改为 `AgentConfig` 接口 |

### 6.7 single_agent/ability_manager_test.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `fakeAgent.Config()` 返回值 | `any` | `AgentConfig` |

### 6.8 session/doc.go

| 位置 | 当前 | 回填为 |
|------|------|--------|
| `AgentConfigProvider` 说明 | 占位，⤵️ 6.3 回填 | 改为引用 `AgentConfig` 接口，或删除占位（直接用 SessionConfig 的 GetAgentConfig 即可） |

## 7. 测试覆盖

| 测试函数 | 覆盖内容 |
|----------|----------|
| `TestNewReActAgentConfig` | 默认值验证 |
| `TestNewReActAgentConfig_WithOptions` | 各基础 Option 函数 |
| `TestNewReActAgentConfig_WithModelClient` | 复合 Option 联动（ModelClientConfig + ModelRequestConfig 创建） |
| `TestNewReActAgentConfig_WithModelProviderDetails` | 复合 Option 联动（仅 provider/key/base） |
| `TestNewReActAgentConfig_WithContextEngine` | 复合 Option 联动（ContextEngineConfig 构建） |
| `TestNewReActAgentConfig_WithCustomHeadersSync` | 复合 Option 联动（CustomHeaders 同步到 ModelClientConfig） |
| `TestReActAgentConfig_Validate` | 校验逻辑（边界值、递归校验） |
| `TestReActAgentConfig_AgentConfig接口` | 接口实现验证 |
| `TestReActAgentConfig_JSON序列化` | JSON round-trip |

## 8. 新增文件

| 文件 | 内容 |
|------|------|
| `schema/react_agent_config.go` | ReActAgentConfig 结构体 + Option 函数 + AgentConfig 接口实现 + Validate |
| `schema/react_agent_config_test.go` | 单元测试 |

## 9. 修改文件

| 文件 | 修改内容 |
|------|----------|
| `interfaces/interface.go` | 新增 AgentConfig 接口；BaseAgent.Config() 返回 AgentConfig；BaseAgent.Configure() 参数改 AgentConfig |
| `base.go` | WarpBaseAgent.config 字段改 AgentConfig；相关方法签名同步 |
| `base_test.go` | 测试适配新类型 |
| `ability_manager_test.go` | fakeAgent 适配新类型 |
| `session/config/config.go` | GetAgentConfig/SetAgentConfig 改 AgentConfig |
| `session/config/config_test.go` | 测试适配新类型 |
| `session/internal/agent_session.go` | AgentID() 中的类型断言逻辑优化 |
| `session/doc.go` | AgentConfigProvider 说明更新 |
| `schema/doc.go` | 文件目录新增 react_agent_config.go |
| `interfaces/doc.go` | 核心类型索引新增 AgentConfig |

## 10. 依赖关系

```
interfaces/interface.go  (AgentConfig 接口)
    ↓ 导入
    context_engine/schema (ContextEngineConfig)
    foundation/llm/schema (ModelClientConfig)

schema/react_agent_config.go  (ReActAgentConfig 实现)
    ↓ 导入
    interfaces (AgentConfig 接口，编译期接口检查)
    context_engine/interface (ProcessorSpec)
    context_engine/schema (ContextEngineConfig)
    foundation/llm/schema (ModelClientConfig, ModelRequestConfig)
```

无循环依赖风险。
