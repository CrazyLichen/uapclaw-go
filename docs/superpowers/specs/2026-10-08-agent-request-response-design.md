# 步骤 10.1.5：AgentRequest / AgentResponse + PermissionContext 实现设计

> 本文档描述 IMPLEMENTATION_PLAN 步骤 10.1.5（AgentRequest / AgentResponse）和
> 步骤 10.1.8（PermissionContext）的 Go 实现设计。因 AgentRequest 强依赖
> PermissionContext 字段，两步骤合并实现。

---

## 1. 在 MVP 会话流程中的位置

```
领域十：AgentServer + 独立交互入口
  └── 10.1 Schema 层（基础类型定义，所有上层依赖此层）
        ├── 10.1.1 ReqMethod 枚举      ✅
        ├── 10.1.2 EventType 枚举      ✅
        ├── 10.1.3 Mode 枚举           ✅
        ├── 10.1.4 Message 模型        ✅
        ├── 10.1.5 AgentRequest/AgentResponse  ☐ ← 本设计
        ├── 10.1.6 AgentResponseChunk         ☐（骨架一并定义）
        ├── 10.1.7 HookEventBase              ☐
        └── 10.1.8 PermissionContext          ☐ ← 合并到本设计
```

**MVP 层 1（Schema 层）第 5 步 / 共 8 步**，依赖步骤 1（ReqMethod）、2（EventType）、3（Mode）。

---

## 2. 作用

**AgentRequest**：Gateway 向 AgentServer 发起的请求模型，承载 RPC 方法名（req_method）、
请求参数（params）、流式标识、权限上下文等。E2A 协议解码后交由 AgentServer 处理。

**AgentResponse**：AgentServer 向 Gateway 返回的非流式完整响应模型，承载执行结果（ok）、
响应负载（payload）、元数据等。

**PermissionContext**：统一承载权限判定所需的身份与场景信息，AgentRequest 的字段之一。

**在通信链路中的位置**：

```
REPL/HTTP/Web → Channel → Gateway → E2A编码 → Go channel → E2A解码
    → AgentRequest → AgentServer → AgentAdapter → Agent
                                    ↓
    Channel ← Gateway ← AgentResponse ←──────────┘
```

**被谁消费**：
- AgentRequest：agent_compat.go（E2A→AgentRequest 转换，10.2.8）、AgentAdapter 接口、AgentWebSocketServer 方法分发
- AgentResponse：wire_codec.go（编解码，10.2.5）、gateway_normalize.go（格式互转，10.2.7）

---

## 3. 文件组织

严格按 mvp_plan.md 目录结构：

```
internal/swarm/schema/
├── agent.go         # AgentRequest + AgentResponse + AgentResponseChunk（骨架）
├── agent_test.go    # 测试
├── permission.go    # PermissionContext + Scene/OwnerScopeKey/ToDict/FromDict
└── permission_test.go
```

---

## 4. Python 参考源码

路径：`jiuwenswarm/common/schema/agent.py`

```python
@dataclass
class PermissionContext:
    principal_user_id: str = ""
    triggering_user_id: str = ""
    channel_id: str = ""
    group_digital_avatar: bool = False
    web_user_id: str = ""

    @property
    def scene(self) -> str: ...
    @property
    def owner_scope_key(self) -> tuple[str, str]: ...
    def to_dict(self) -> dict[str, Any]: ...
    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> PermissionContext: ...

@dataclass
class AgentRequest:
    request_id: str
    channel_id: str = ""
    session_id: str | None = None
    chat_id: str | None = None
    req_method: ReqMethod | None = None
    params: dict = field(default_factory=dict)
    is_stream: bool = False
    timestamp: float = 0.0
    metadata: dict[str, Any] | None = None
    enable_memory: bool | None = None
    permission_context: PermissionContext | None = None

@dataclass
class AgentResponse:
    request_id: str
    channel_id: str
    ok: bool = True
    payload: dict | None = None
    metadata: dict[str, Any] | None = None

@dataclass
class AgentResponseChunk:
    request_id: str
    channel_id: str
    payload: dict | None = None
    is_complete: bool = False
```

---

## 5. Go 类型设计

### 5.1 PermissionContext

```go
type PermissionContext struct {
    PrincipalUserID    string `json:"principal_user_id"`
    TriggeringUserID   string `json:"triggering_user_id"`
    ChannelID          string `json:"channel_id"`
    GroupDigitalAvatar bool   `json:"group_digital_avatar"`
    WebUserID          string `json:"web_user_id"`
}
```

方法：
- `Scene() string`：channel_id=="web" → "web"；group_digital_avatar → "group_digital_avatar"；否则 "normal_im"
- `OwnerScopeKey() [2]string`：返回 [ChannelID, PrincipalUserID]
- `ToDict() map[string]any`：序列化为 dict
- `NewPermissionContextFromDict(data map[string]any) *PermissionContext`：从 dict 反序列化
- `NewPermissionContext(opts ...PermissionContextOption) *PermissionContext`：工厂函数
- `Validate() error`：校验 PrincipalUserID 非空

Option：WithPrincipalUserID、WithTriggeringUserID、WithChannelID、WithGroupDigitalAvatar、WithWebUserID

### 5.2 AgentRequest

```go
type AgentRequest struct {
    RequestID        string             `json:"request_id"`
    ChannelID        string             `json:"channel_id"`
    SessionID        *string            `json:"session_id,omitempty"`
    ChatID           *string            `json:"chat_id,omitempty"`
    ReqMethod        ReqMethod          `json:"req_method,omitempty"`
    Params           json.RawMessage    `json:"params,omitempty"`
    IsStream         bool               `json:"is_stream"`
    Timestamp        float64            `json:"timestamp"`
    Metadata         map[string]any     `json:"metadata,omitempty"`
    EnableMemory     *bool              `json:"enable_memory,omitempty"`
    PermissionContext *PermissionContext `json:"permission_context,omitempty"`
}
```

字段类型映射要点：
- `str | None` → `*string` + omitempty（session_id、chat_id）
- `bool | None` → `*bool` + omitempty（enable_memory，三态：nil/true/false）
- `dict` → `json.RawMessage`（params，对齐 mvp_plan 决策）
- `dict | None` → `json.RawMessage` + omitempty（payload）
- `float` → `float64`（timestamp）
- `PermissionContext | None` → `*PermissionContext` + omitempty

方法：
- `NewAgentRequest(requestID, channelID string, reqMethod ReqMethod, params json.RawMessage, opts ...AgentRequestOption) *AgentRequest`：工厂函数，自动生成 Timestamp
- `Validate() error`：校验 request_id 非空、channel_id 非空、req_method 非零值

Option：WithSessionID、WithChatID、WithIsStream、WithMetadata、WithEnableMemory、WithPermissionContext

### 5.3 AgentResponse

```go
type AgentResponse struct {
    RequestID string          `json:"request_id"`
    ChannelID string          `json:"channel_id"`
    OK        bool            `json:"ok"`
    Payload   json.RawMessage `json:"payload,omitempty"`
    Metadata  map[string]any  `json:"metadata,omitempty"`
}
```

方法：
- `NewAgentResponse(requestID, channelID string, opts ...AgentResponseOption) *AgentResponse`：工厂函数，默认 OK=true
- `Validate() error`：校验 request_id 非空、channel_id 非空

Option：WithResponseOK、WithPayload、WithResponseMetadata
（加 Response 前缀避免与 AgentRequest Option 命名冲突）

### 5.4 AgentResponseChunk（骨架）

```go
type AgentResponseChunk struct {
    RequestID  string          `json:"request_id"`
    ChannelID  string          `json:"channel_id"`
    Payload    json.RawMessage `json:"payload,omitempty"`
    IsComplete bool            `json:"is_complete"`
}
```

10.1.5 只定义结构体字段 + JSON tag，不加工厂函数和 Validate。步骤 10.1.6 时补全。

---

## 6. 测试覆盖

### permission_test.go

| 测试函数 | 覆盖内容 |
|----------|----------|
| TestNewPermissionContext | 工厂函数默认值 |
| TestNewPermissionContext_使用Option | 通过 Option 设置各字段 |
| TestPermissionContext_Scene_web | channel_id="web" → "web" |
| TestPermissionContext_Scene_groupDigitalAvatar | group_digital_avatar=true → "group_digital_avatar" |
| TestPermissionContext_Scene_normalIM | 默认 → "normal_im" |
| TestPermissionContext_OwnerScopeKey | 返回 [channel_id, principal_user_id] |
| TestPermissionContext_ToDict | 序列化完整字段 |
| TestNewPermissionContextFromDict | 反序列化往返 |
| TestPermissionContext_Validate_正常 | 正常数据通过 |
| TestPermissionContext_Validate_校验失败 | 缺少必填字段返回错误 |
| TestPermissionContext_JSON往返 | JSON marshal/unmarshal 往返一致 |

### agent_test.go

| 测试函数 | 覆盖内容 |
|----------|----------|
| TestNewAgentRequest | 工厂函数默认值 |
| TestNewAgentRequest_使用Option | 通过 Option 设置各字段 |
| TestAgentRequest_Validate_正常 | request_id + channel_id + req_method 非空 |
| TestAgentRequest_Validate_requestID为空 | 返回错误 |
| TestAgentRequest_Validate_channelID为空 | 返回错误 |
| TestAgentRequest_Validate_reqMethod为零值 | 返回错误 |
| TestAgentRequest_JSON往返 | JSON marshal/unmarshal 往返一致 |
| TestAgentRequest_EnableMemory三态 | nil/true/false 三态序列化正确 |
| TestNewAgentResponse | 工厂函数默认值（OK=true） |
| TestNewAgentResponse_使用Option | 通过 Option 设置各字段 |
| TestAgentResponse_Validate_正常 | request_id + channel_id 非空 |
| TestAgentResponse_Validate_校验失败 | 缺少必填字段返回错误 |
| TestAgentResponse_JSON往返 | JSON marshal/unmarshal 往返一致 |
| TestAgentResponseChunk_JSON序列化 | 骨架结构体 JSON 序列化/反序列化基本验证 |

---

## 7. doc.go 更新

更新后文件目录：

```
schema/
├── doc.go           # 包文档
├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
├── event_type.go    # EventType 枚举（26 个事件类型）
├── mode.go          # Mode 枚举（6 个运行模式）
├── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
├── agent.go         # AgentRequest/AgentResponse/AgentResponseChunk 模型 + 工厂函数 + Validate
└── permission.go    # PermissionContext 权限上下文 + 派生方法 + 序列化 + 工厂函数 + Validate
```

包功能概述追加：Agent 请求/响应模型、权限上下文。

---

## 8. IMPLEMENTATION_PLAN 状态回填

| 步骤 | 原状态 | 新状态 | 说明 |
|------|--------|--------|------|
| 10.1.5 | ☐ | ✅ | AgentRequest / AgentResponse 完成 |
| 10.1.6 | ☐ | 🔄 | AgentResponseChunk 骨架已定义，工厂/Validate/测试留给 10.1.6 补全 |
| 10.1.8 | ☐ | ✅ | PermissionContext 与 10.1.5 一并完成 |

10.1.7（HookEventBase）仍为 ☐，不在本次范围内。
