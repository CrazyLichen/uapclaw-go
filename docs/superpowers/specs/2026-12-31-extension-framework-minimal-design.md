# 10.5 扩展框架最小子集设计

> 对齐 Python: `jiuwenswarm/extensions/`
> Go 包路径: `internal/swarm/extensions/`
> 依赖: schema/event_base ✅, callback.CallbackFramework ✅, transport.AgentTransport ✅

## 1. 设计概述

实现 Python 扩展系统的最小子集，覆盖 ExtensionRegistry 的核心数据结构和回调触发机制，使现有的回填点（cmd.go 扩展系统 TODO、handle_envelope.go trigger TODO）可以闭环。

延后部分：ExtensionLoader/Manager（Go 插件加载机制待定）、CryptoUtility、CallbackCompat（Go 已有 CallbackFramework 无需额外实现）、YuanrongFrontendAgentClient。

## 2. 文件结构

```
internal/swarm/extensions/
├── doc.go                    # 包文档（10.5.1）
├── types.go                  # ExtensionMetadata + ExtensionConfig（10.5.1）
├── hook_event.go             # GatewayHookEvents + AgentServerHookEvents（10.5.2）
├── hooks_context.go          # 4 种 HookContext 结构体（10.5.3）
├── registry.go               # ExtensionRegistry 单例 + 回调触发（10.5.6）
├── loader.go                 # ExtensionLoader stub（10.5.7 ⤵️）
├── manager.go                # ExtensionManager stub（10.5.8 ⤵️）
├── callback_compat.go        # 注释说明 Go 已有 CallbackFramework，无需额外实现（10.5.9 ⤴️）
├── sdk/
│   ├── doc.go                # SDK 子包文档
│   ├── base.go               # BaseExtension 抽象基类（10.5.4）
│   ├── agent_server_client.go # AgentServerClientExtension（10.5.5）
│   └── crypto_utility.go     # CryptoUtility stub（10.5.10 ⤵️）
```

## 3. IMPLEMENTATION_PLAN.md 拆分

将当前合并行 `10.5.1-10 ☐ 扩展框架` 拆为：

| 步骤 | 状态 | 内容 | Python 参考 |
|------|------|------|------------|
| 10.5.1 | ☐ | 数据类型 ExtensionMetadata + ExtensionConfig | `extensions/types.py` |
| 10.5.2 | ☐ | 钩子事件常量 GatewayHookEvents + AgentServerHookEvents | `extensions/hook_event.py` |
| 10.5.3 | ☐ | 钩子上下文结构（4 种 HookContext） | `extensions/hooks_context.py` |
| 10.5.4 | ☐ | BaseExtension 抽象基类 | `extensions/sdk/base.py` |
| 10.5.5 | ☐ | AgentServerClientExtension | `extensions/sdk/agent_server_client.py` |
| 10.5.6 | ☐ | ExtensionRegistry 单例（复用 CallbackFramework） | `extensions/registry.py` |
| 10.5.7 | ☐ ⤵️ | ExtensionLoader（延后，Go 插件机制待定） | `extensions/loader.py` |
| 10.5.8 | ☐ ⤵️ | ExtensionManager（延后，依赖 loader） | `extensions/manager.py` |
| 10.5.9 | ⤴️ | CallbackCompat（Go 已有 CallbackFramework，无需额外实现） | `extensions/callback_compat.py` |
| 10.5.10 | ☐ ⤵️ | CryptoUtility（延后） | `extensions/sdk/crypto_utility.py` |

## 4. 各文件详细设计

### 4.1 types.go（10.5.1，对齐 Python extensions/types.py）

```go
// ExtensionMetadata 扩展元数据，对齐 Python ExtensionMetadata dataclass
type ExtensionMetadata struct {
    ID                 string         `json:"id"`
    Name               string         `json:"name"`
    Version            string         `json:"version"`
    Description        string         `json:"description"`
    Author             string         `json:"author"`
    MinJiuwenSwarmVersion string      `json:"min_jiuwenswarm_version"`
    Dependencies       map[string]string `json:"dependencies"`
    ConfigSchema       map[string]any    `json:"config_schema,omitempty"`
}

// ExtensionConfig 扩展配置，对齐 Python ExtensionConfig dataclass
type ExtensionConfig struct {
    Config map[string]any
    Logger any  // logger 接口，实际使用 logger.ComponentXxx
}
```

方法名对齐：
- Python `ExtensionMetadata` 字段 → Go `ExtensionMetadata` 字段（CamelCase 映射）
- Python `ExtensionConfig.config` → Go `ExtensionConfig.Config`
- Python `ExtensionConfig.logger` → Go `ExtensionConfig.Logger`

### 4.2 hook_event.go（10.5.2，对齐 Python extensions/hook_event.py）

```go
// GatewayHookEvents Gateway 侧钩子事件常量，对齐 Python GatewayHookEvents
// scope = "gateway"，继承 HookEventBase
type GatewayHookEvents struct {
    // Scope 事件作用域
    Scope string
}
// 常量：GatewayStarted, GatewayStopped, BeforeChatRequest

// AgentServerHookEvents AgentServer 侧钩子事件常量，对齐 Python AgentServerHookEvents
// scope = "agent_server"，继承 HookEventBase
type AgentServerHookEvents struct {
    // Scope 事件作用域
    Scope string
}
// 常量：AgentServerStarted, AgentServerStopped, BeforeChatRequest,
//       MemoryBeforeChat, MemoryAfterChat, BeforeSystemPromptBuild
```

方法名对齐：
- Python `GatewayHookEvents.GATEWAY_STARTED` → Go `GatewayHookEvents.GatewayStarted`
- Python `GatewayHookEvents.BEFORE_CHAT_REQUEST` → Go `GatewayHookEvents.BeforeChatRequest`
- Python `AgentServerHookEvents.MEMORY_BEFORE_CHAT` → Go `AgentServerHookEvents.MemoryBeforeChat`
- Python `GatewayHookEvents.get_event()` → Go `GatewayHookEvents.GetEvent()` (继承 HookEventBase.GetEvent)

事件名格式与 Python 一致：`scope:event_name`，如 `"gateway:before_chat_request"`。

### 4.3 hooks_context.go（10.5.3，对齐 Python extensions/hooks_context.py）

```go
// MemoryHookContext 记忆钩子上下文，对齐 Python MemoryHookContext
type MemoryHookContext struct {
    SessionID        string
    RequestID        string
    ChannelID        *string     // 可选
    AgentName        string
    WorkspaceDir     string
    AssistantMessage *string     // 输出字段
    Extra            map[string]any
    MemoryBlocks     []string    // before_chat 扩展写入，宿主读取
    Metadata         map[string]any
}
// 方法：ToDict() → ToMap()

// GatewayChatHookContext Gateway 聊天钩子上下文，对齐 Python GatewayChatHookContext
type GatewayChatHookContext struct {
    RequestID  string
    ChannelID  string
    SessionID  *string     // 可选
    ReqMethod  *string     // 可选
    Params     map[string]any  // 扩展可原地修改
}
// 方法：ToDict() → ToMap()

// AgentServerChatHookContext AgentServer 聊天钩子上下文，对齐 Python AgentServerChatHookContext
type AgentServerChatHookContext struct {
    RequestID  string
    ChannelID  string
    SessionID  *string     // 可选
    ReqMethod  *string     // 可选
    Params     map[string]any  // 扩展可原地修改
}
// 方法：ToDict() → ToMap()

// SystemPromptHookContext 系统提示词钩子上下文，对齐 Python SystemPromptHookContext
type SystemPromptHookContext struct {
    HomeDir  *string  // 扩展可设置此目录覆盖默认 home_dir
    SkillDir *string  // 扩展可设置此目录扩展默认 skill_dir
}
// 方法：ToDict() → ToMap()
```

方法名对齐：
- Python `to_dict()` → Go `ToMap()` (dict/map 术语差异，Go 用 map)

### 4.4 sdk/base.go（10.5.4，对齐 Python extensions/sdk/base.py）

```go
// BaseExtension 扩展抽象基类，对齐 Python BaseExtension ABC
type BaseExtension interface {
    // Initialize 扩展初始化入口，对齐 Python initialize(config: ExtensionConfig)
    Initialize(ctx context.Context, config *ExtensionConfig) error
    // Shutdown 扩展关闭释放资源，对齐 Python shutdown()
    Shutdown(ctx context.Context) error
    // Metadata 返回扩展元数据，对齐 Python metadata @property
    Metadata() *ExtensionMetadata
    // SetExtensionDir 设置扩展目录，对齐 Python set_extension_dir(path)
    SetExtensionDir(path string)
}

// BaseExtensionImpl BaseExtension 默认实现（嵌入使用）
type BaseExtensionImpl struct {
    metadataCache  *ExtensionMetadata
    extensionDir   *string
    configCache    map[string]any
}
// 方法：Metadata(), SetExtensionDir(), LoadMetadataFromYAML(), LoadConfigFromYAML()
```

方法名对齐：
- Python `initialize()` → Go `Initialize()`
- Python `shutdown()` → Go `Shutdown()`
- Python `metadata` (property) → Go `Metadata()`
- Python `set_extension_dir()` → Go `SetExtensionDir()`
- Python `_load_metadata_from_yaml()` → Go `LoadMetadataFromYAML()`
- Python `_load_config_from_yaml()` → Go `LoadConfigFromYAML()`
- Python `_get_extension_dir()` → Go `getExtensionDir()` (非导出)

常量对齐：
- Python `MANIFEST_FILENAME = "extension.yaml"` → Go `ManifestFilename = "extension.yaml"`

### 4.5 sdk/agent_server_client.go（10.5.5，对齐 Python extensions/sdk/agent_server_client.py）

```go
// AgentServerClientExtension AgentServer 客户端扩展，对齐 Python AgentServerClientExtension
type AgentServerClientExtension interface {
    BaseExtension
    // GetClient 返回与 AgentServer 通信的客户端，对齐 Python get_client() @abstractmethod
    // 返回 transport.AgentTransport 接口（Go 的 AgentClient 实现该接口）
    GetClient() transport.AgentTransport
}

// AgentServerClientExtensionImpl 默认实现
type AgentServerClientExtensionImpl struct {
    BaseExtensionImpl
    client transport.AgentTransport
}
```

方法名对齐：
- Python `get_client()` → Go `GetClient()`
- Python `AgentServerClient` (返回类型) → Go `transport.AgentTransport` (Go 用 AgentTransport 接口)

注意：Python 的 `AgentServerClient` 是 gateway/routing 中的 ABC（有 connect/disconnect/send_request/send_request_stream 等方法），Go 中对应的是 `transport.AgentTransport` 接口（Send/Recv/Close）。两者方法名不同是因为 Go 已有 transport 层架构（见规则 6），此处 `GetClient()` 返回 `transport.AgentTransport` 是对齐 Go 项目的已有设计。

### 4.6 registry.go（10.5.6，对齐 Python extensions/registry.py）

```go
// ExtensionRegistry 扩展注册中心单例，对齐 Python ExtensionRegistry
// 回调机制复用 callback.CallbackFramework.OnCustom/TriggerCustom/OffCustom
type ExtensionRegistry struct {
    mu                  sync.RWMutex
    callbackFramework   *callback.CallbackFramework
    config              *ExtensionConfig
    agentServerClientExt AgentServerClientExtension
    cryptoUtil          CryptoUtility  // ⤵️ 10.5.10 延后，暂 nil
}
```

方法名对齐：

| Python 方法 | Go 方法 | 说明 |
|-------------|---------|------|
| `get_instance()` (classmethod) | `GetInstance()` | 获取单例，未初始化则 panic |
| `create_instance(framework, config, logger)` (classmethod) | `CreateInstance(framework, config, logger)` | 创建单例，已存在则 panic |
| `reset_instance()` (classmethod) | `ResetInstance()` | 重置单例为 nil |
| `register(event, handler, priority=100)` | `Register(event, handler, priority)` | 注册回调，内部调 callbackFramework.OnCustom() |
| `unregister(event, handler)` | `Unregister(event, handler)` | 取消回调，内部调 callbackFramework.OffCustom() |
| `trigger(event, context)` (async) | `Trigger(ctx, event, context)` | 触发事件，内部调 callbackFramework.TriggerCustom() |
| `register_agent_server_client(ext)` | `RegisterAgentServerClient(ext)` | 注册 AgentServerClient 扩展 |
| `register_crypto_utility(ext)` | `RegisterCryptoUtility(ext)` | 注册 CryptoUtility 扩展 |
| `get_agent_server_client_extension()` | `GetAgentServerClientExtension()` | 获取扩展实例 |
| `get_agent_server_client()` | `GetAgentServerClient()` | 获取底层 AgentTransport |
| `get_crypto_utility_extension()` | `GetCryptoUtilityExtension()` | 获取 Crypto 扩展实例 |
| `get_crypto_provider()` | `GetCryptoProvider()` | 获取 CryptoProvider |
| `config` (property) | `Config()` | 获取 ExtensionConfig |

单例管理对齐 Python 的 `_instance` 类变量模式，Go 用包级变量 + mutex。

### 4.7 延后 stub 文件

**loader.go（10.5.7 ⤵️）**：
```go
// Package extensions 提供扩展系统基础设施...
// ExtensionLoader — ⤵️ 10.5.7 延后实现，Go 插件加载机制待定
// 当前仅定义接口占位，DiscoverExtensionRoots / LoadExtension 等方法返回空结果
type ExtensionLoader struct { registry *ExtensionRegistry }
func (l *ExtensionLoader) DiscoverExtensionRoots() []string { return nil }
func (l *ExtensionLoader) LoadExtension(ctx context.Context, root string) (BaseExtension, error) {
    return nil, fmt.Errorf("ExtensionLoader 尚未实现，⤵️ 10.5.7")
}
```

**manager.go（10.5.8 ⤵️）**：
```go
// ExtensionManager — ⤵️ 10.5.8 延后实现，依赖 ExtensionLoader
type ExtensionManager struct { registry *ExtensionRegistry }
func (m *ExtensionManager) LoadAllExtensions(ctx context.Context) error {
    return fmt.Errorf("ExtensionManager 尚未实现，⤵️ 10.5.8")
}
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error { return nil }
func (m *ExtensionManager) ListExtensions() []map[string]string { return nil }
```

**callback_compat.go（10.5.9 ⤴️）**：
```go
// Go 已有 callback.CallbackFramework 提供 OnCustom/TriggerCustom/OffCustom，
// 对应 Python AsyncCallbackFramework.register_sync/trigger/unregister_sync。
// Python 的 callback_compat.py 是为兼容 openjiuwen <0.1.9 的 API 缺口，
// Go 项目中 CallbackFramework 已完整实现，无需额外兼容层。
// 此文件仅作为文档注释存在，标注 ⤴️ 10.5.9 已覆盖。
```

**sdk/crypto_utility.go（10.5.10 ⤵️）**：
```go
// CryptoUtility — ⤵️ 10.5.10 延后实现
type CryptoUtility interface {
    BaseExtension
    GetCrypto() CryptoProvider  // ⤵️ CryptoProvider 接口待定义
}
```

## 5. 回填点闭环

| 回填点 | 位置 | 闭环方式 |
|--------|------|---------|
| `TODO(⤵️ 扩展系统)` | cmd.go:197 | 部分：可初始化 ExtensionRegistry.CreateInstance()，但 ExtensionManager.LoadAllExtensions 仍为 TODO（loader 延后） |
| `AgentServerChatHookContext + trigger` | handle_envelope.go:67 | ✅ 完全闭环：构建 AgentServerChatHookContext + 调用 registry.Trigger |
| `云端记忆对话前钩子` | runtime/uapclaw.go:152 | 部分：registry.Trigger 可用，MemoryHookContext 的 memory_blocks 写入需要 10.3.2 配合 |

## 6. 依赖关系

```
schema/event_base.go (HookEventBase) ← hook_event.go (继承)
callback.CallbackFramework           ← registry.go (OnCustom/TriggerCustom/OffCustom)
transport.AgentTransport             ← sdk/agent_server_client.go (GetClient 返回类型)
```

所有依赖均已实现 ✅，无阻塞。

## 7. 后续章节衔接

做完 10.5 最小子集后，可继续：
- **10.3.23 Hooks executor + user_hook_rail**：依赖 ExtensionRegistry.Trigger，最小子集已提供
- **10.3.24 Sandbox**：依赖 12.x JiuwenBox，延后
- **10.3.25 Utils**：parse_stream_chunk 和 DiffService，无强依赖
- **10.3.26 入口**：uapclaw agentserver 独立启动命令
