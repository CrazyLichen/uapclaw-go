# 10.1.7 HookEventBase 钩子事件基类设计

## 概述

本文档定义 10.1.7 步骤（HookEventBase）的 Go 实现方案。HookEventBase 是带作用域（scope）的钩子事件名基类，用于为 Gateway ↔ AgentServer 交互生命周期钩子提供统一的 `scope:event_name` 命名规范。

## 在 Agent 会话流程中的位置

```
Channel → Gateway → [E2A协议] → AgentServer → AgentAdapter → Agent
                    ↑                      ↑
              HookEventBase           HookEventBase
              定义在这里            消费在这里
```

HookEventBase 位于 **领域十 → 10.1 Schema 层 → 第7步**，是 Schema 层中唯一尚未实现的步骤（10.1.1~10.1.6 和 10.1.8 均已完成 ✅）。

## Python 参考实现

### event_base.py

```python
DEFAULT_SCOPE = "_framework"

def build_event_name(scope: str, event_name: str) -> str:
    return f"{scope}:{event_name}"

def parse_event_name(scoped_event: str) -> tuple[str, str]:
    if ":" in scoped_event:
        scope, event_name = scoped_event.split(":", 1)
        return scope, event_name
    return DEFAULT_SCOPE, scoped_event

class HookEventBase:
    scope: str = DEFAULT_SCOPE

    def __init_subclass__(cls, **kwargs):
        super().__init_subclass__(**kwargs)
        for attr_name, attr_value in list(cls.__dict__.items()):
            if isinstance(attr_value, str) and ":" in attr_value:
                scope, event_name = parse_event_name(attr_value)
                if scope == DEFAULT_SCOPE and cls.scope != DEFAULT_SCOPE:
                    setattr(cls, attr_name, build_event_name(cls.scope, event_name))

    @classmethod
    def get_event(cls, event_name: str) -> str:
        return build_event_name(cls.scope, event_name)
```

### 子类（hook_event.py）

```python
class GatewayHookEvents(HookEventBase):
    scope: str = "gateway"
    GATEWAY_STARTED = HookEventBase.get_event("gateway_started")
    GATEWAY_STOPPED = HookEventBase.get_event("gateway_stopped")
    BEFORE_CHAT_REQUEST = HookEventBase.get_event("before_chat_request")

class AgentServerHookEvents(HookEventBase):
    scope: str = "agent_server"
    AGENT_SERVER_STARTED = HookEventBase.get_event("agent_server_started")
    AGENT_SERVER_STOPPED = HookEventBase.get_event("agent_server_stopped")
    BEFORE_CHAT_REQUEST = HookEventBase.get_event("before_chat_request")
    MEMORY_BEFORE_CHAT = HookEventBase.get_event("memory_before_chat")
    MEMORY_AFTER_CHAT = HookEventBase.get_event("memory_after_chat")
    BEFORE_SYSTEM_PROMPT_BUILD = HookEventBase.get_event("before_system_prompt_build")
```

### Python 消费者

| 消费者 | 使用的事件 | 对应 Go 步骤 | 触发时机 |
|--------|-----------|-------------|---------|
| `gateway/message_handler.py` | `GatewayHookEvents.BEFORE_CHAT_REQUEST` | 11.3 MessageHandler | Gateway 收到 chat 请求前 |
| `server/agent_ws_server.py` | `AgentServerHookEvents.BEFORE_CHAT_REQUEST` | 10.3.1 AgentWebSocketServer | WS 入口收到请求前 |
| `server/agent_adapter/interface.py` | `AgentServerHookEvents.MEMORY_BEFORE_CHAT` (×2) | 10.3.2 JiuWenClaw | 调用 Agent 前注入记忆 |
| `server/agent_adapter/interface.py` | `AgentServerHookEvents.MEMORY_AFTER_CHAT` (×2) | 10.3.2 JiuWenClaw | Agent 响应后写回记忆 |

## Go 实现方案

### 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| scope 前缀注入时机 | 模板方法模式（C-1 嵌入式组合） | Go 无 `__init_subclass__`，通过嵌入 + 构造函数显式初始化实现等价效果 |
| 子类归属 | 基类在 schema，子类在各自消费方 | 关注点分离；gateway/server 包各自定义自己的 HookEvents |
| 与 EventType 关联 | 不关联，纯字符串 | Python 中 HookEventBase 不引用 EventType，事件名与 EventType 值属于不同命名空间 |
| MVP 预留 | 最小实现，不预留 | YAGNI；消费者实现时再按需扩展 |

### 核心类型与函数

| 类型/函数 | 签名 | 说明 |
|-----------|------|------|
| 常量 | `DefaultScope = "_framework"` | 默认作用域，对齐 Python `DEFAULT_SCOPE` |
| 函数 | `BuildEventName(scope, eventName string) string` | 拼接 `scope:eventName`，对齐 Python `build_event_name` |
| 函数 | `ParseEventName(scopedEvent string) (scope, eventName string)` | 解析 `scope:eventName`，无冒号回退 DefaultScope，对齐 Python `parse_event_name` |
| 结构体 | `HookEventBase struct{ Scope string }` | 钩子事件基类 |
| 方法 | `(h *HookEventBase) GetEvent(eventName string) string` | 用 h.Scope 构建完整事件名 |
| 函数 | `NewHookEventBase() *HookEventBase` | 工厂函数，Scope 默认 DefaultScope |

### ParseEventName 行为

| 输入 | 输出 scope | 输出 eventName |
|------|-----------|----------------|
| `"gateway:before_chat_request"` | `"gateway"` | `"before_chat_request"` |
| `"before_chat_request"` | `"_framework"` | `"before_chat_request"` |
| `"a:b:c"` | `"a"` | `"b:c"` |

### 声明排列（Go 编码规范）

```
1. 结构体    — HookEventBase + GetEvent 方法
2. 常量      — DefaultScope
3. 导出函数  — BuildEventName, ParseEventName, NewHookEventBase
```

## 本次实现范围

| 产出 | 位置 |
|------|------|
| HookEventBase 基类 | `internal/swarm/schema/event_base.go` |
| 单元测试 | `internal/swarm/schema/event_base_test.go` |
| doc.go 更新 | `internal/swarm/schema/doc.go` 文件目录新增条目 |

## 子类延迟实现计划

| 子类 | 归属包 | 实现步骤 | 事件列表 |
|------|--------|---------|---------|
| `GatewayHookEvents` | `internal/swarm/gateway/` | 11.3 MessageHandler | `gateway_started`, `gateway_stopped`, `before_chat_request` |
| `AgentServerHookEvents` | `internal/swarm/server/` | 10.3.1 / 10.3.2 | `agent_server_started`, `agent_server_stopped`, `before_chat_request`, `memory_before_chat`, `memory_after_chat`, `before_system_prompt_build` |

回填标记：10.3.1 / 10.3.2 / 11.3 实现子类时，在 IMPLEMENTATION_PLAN.md 中用 `⤴️` 标记回填来源为 10.1.7。

## 测试覆盖

| 测试函数 | 场景 |
|---------|------|
| `TestBuildEventName` | 拼接 scope:eventName |
| `TestBuildEventName_空scope` | scope 为空 |
| `TestBuildEventName_空eventName` | eventName 为空 |
| `TestParseEventName` | 正常解析 |
| `TestParseEventName_无冒号` | 无冒号回退默认 |
| `TestParseEventName_多冒号` | 多冒号只拆第一个 |
| `TestParseEventName_与Build往返` | 往返一致性 |
| `TestNewHookEventBase` | 工厂函数默认 Scope |
| `TestHookEventBase_GetEvent` | GetEvent 方法 |
| `TestHookEventBase_GetEvent_默认Scope` | 默认 Scope 的 GetEvent |
| `TestDefaultScope` | 常量值 |
| `TestHookEventBase_JSON往返` | JSON 序列化往返 |

## 验证标准

Schema 全部类型 JSON 序列化往返通过（mvp_plan.md 层1验证点）。
