# 5.2 BaseSession 接口设计

## 概述

实现会话基类接口 `BaseSession` 和代理类 `ProxySession`，对应 Python `openjiuwen/core/session/session.py` 中的 `BaseSession` 和 `ProxySession`。

本步骤仅定义接口和代理类，依赖的 Config/Tracer/StreamWriterManager/Checkpointer/ActorManager 等类型用 `any` 占位，后续步骤回填具体类型。

## 决策记录

| 决策项 | 选择 | 理由 |
|---|---|---|
| 实现范围 | 仅接口 + ProxySession | 对齐实现文档 5.2 范围，后续步骤回填 |
| 子类特有方法 | 方案 A：留在各自类型 | Python BaseSession 只定义 8 个方法，agent_id/workflow_id 等靠鸭子类型，Go 不支持 |
| 未实现依赖类型 | `any` 占位 + ⤵️ 回填标记 | 最小依赖，编译通过即可 |
| Close 签名 | `Close() error`（同步） | 对齐 Go io.Closer 惯例 |
| ProxySession 委托 | 全部 8 个方法都委托给 stub | 修正 Python 遗漏（actor_manager/close 未覆盖） |

## Python 原始接口

```python
class BaseSession(ABC):
    @abstractmethod
    def config(self) -> Config: ...
    @abstractmethod
    def state(self) -> State: ...
    @abstractmethod
    def tracer(self) -> Any: ...
    @abstractmethod
    def stream_writer_manager(self) -> StreamWriterManager: ...
    @abstractmethod
    def session_id(self) -> str: ...
    @abstractmethod
    def checkpointer(self): ...
    def actor_manager(self) -> "ActorManager": pass  # 默认空实现
    async def close(self): pass  # 默认空实现
```

Python 消费者通过 `BaseSession` 类型注解调用子类特有方法（`workflow_id()`、`agent_id()`、`executable_id()` 等），依赖鸭子类型。Go 中这些方法不属于 BaseSession 接口，由各子类自行定义，消费者需要时做类型断言。

## 文件结构

```
session/
├── doc.go              # 包文档（新建）
├── session.go          # BaseSession 接口 + ProxySession（新建）
└── state/              # 已有（5.1）
```

## BaseSession 接口定义

```go
// BaseSession 会话基类接口，定义所有会话类型共有的核心能力
// 对应 Python: openjiuwen/core/session/session.py BaseSession
type BaseSession interface {
    // Config 获取会话配置
    // ⤵️ 5.12 回填：返回类型从 any 改为 SessionConfig
    Config() any
    // State 获取会话状态
    State() state.State
    // Tracer 获取会话追踪器
    // ⤵️ 5.11 回填：返回类型从 any 改为 Tracer
    Tracer() any
    // StreamWriterManager 获取流写入管理器
    // ⤵️ 5.10 回填：返回类型从 any 改为 StreamWriterManager
    StreamWriterManager() any
    // SessionID 获取会话唯一标识
    SessionID() string
    // Checkpointer 获取检查点管理器
    // ⤵️ 5.8 回填：返回类型从 any 改为 Checkpointer
    Checkpointer() any
    // ActorManager 获取 Actor 管理器（可选，默认返回 nil）
    // ⤵️ 后续回填：返回类型从 any 改为 ActorManager
    ActorManager() any
    // Close 关闭会话，释放资源
    Close() error
}
```

## ProxySession 实现

```go
// ProxySession 代理会话，将所有调用委托给内部 stub
// 对应 Python: ProxySession(BaseSession)
// 修正 Python 遗漏：actor_manager 和 close 也委托给 stub
type ProxySession struct {
    stub BaseSession
}

// NewProxySession 创建代理会话实例
func NewProxySession() *ProxySession

// SetSession 设置被代理的底层会话
func (p *ProxySession) SetSession(stub BaseSession)

// 全部 8 个 BaseSession 方法委托给 stub：
func (p *ProxySession) Config() any
func (p *ProxySession) State() state.State
func (p *ProxySession) Tracer() any
func (p *ProxySession) StreamWriterManager() any
func (p *ProxySession) SessionID() string
func (p *ProxySession) Checkpointer() any
func (p *ProxySession) ActorManager() any
func (p *ProxySession) Close() error
```

### nil stub 行为

当 stub 为 nil 时调用方法会 panic，与 Python 中 `self._stub = None` 时调用报 `AttributeError` 对齐。`NewProxySession()` 创建 stub 为 nil 的实例，等待 `SetSession()` 注入。

## 回填清单

| 回填点 | 来源步骤 | 当前占位 | 回填后类型 |
|---|---|---|---|
| `Config()` 返回类型 | 5.12 Session Config | `any` | `SessionConfig` |
| `Tracer()` 返回类型 | 5.11 Session Tracer | `any` | `Tracer` |
| `StreamWriterManager()` 返回类型 | 5.10 StreamWriter | `any` | `StreamWriterManager` |
| `Checkpointer()` 返回类型 | 5.8 Checkpointer | `any` | `Checkpointer` |
| `ActorManager()` 返回类型 | 后续 | `any` | `ActorManager` |

回填时需同时更新：
1. `BaseSession` 接口中的返回类型
2. `ProxySession` 中的委托方法返回类型
3. `doc.go` 中的类型索引

## 测试计划

- `TestNewProxySession`：创建空 ProxySession，验证 stub 为 nil
- `TestProxySession_SetSession`：设置 stub，验证委托调用正确传递
- `TestProxySession_DelegateAllMethods`：构造 mock stub，验证 8 个方法全部委托
- `TestProxySession_NilStub_Panic`：stub 为 nil 时调用方法 panic
- `TestBaseSession_Interface_Compliance`：验证 ProxySession 满足 BaseSession 接口

## 不在本步骤范围内

- AgentSession 实现（5.3）
- WorkflowSession / NodeSession / SubWorkflowSession 实现（5.4）
- Config / Tracer / StreamWriterManager / Checkpointer / ActorManager 的具体类型定义
- Session 生命周期管理（PreRun/PostRun）
