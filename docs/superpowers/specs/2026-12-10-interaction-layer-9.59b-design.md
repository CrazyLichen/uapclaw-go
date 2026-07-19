# 9.59b Interaction 层设计

## 概述

Interaction 层是 TeamAgent 对外暴露 `interact()` API 的核心实现——**团队 Agent 的「收件箱系统」**。
当外部用户有多种意图（直连 Leader、@某成员、广播、驱动 Human Agent Avatar）时，
Interaction 层负责把自由文本或结构化输入正确路由到团队内部。

### 在 Agent 会话中的流程位置

```
用户/SDK
   │
   ▼ (1) 字符串输入 或 结构化 Payload
┌─────────────────────────────┐
│  Interaction Layer (9.59b)  │  ← 本次实现范围
│  ┌─────────────────────────┐│
│  │ parse_interact_str()    ││  解析 # / $ / @ 语法
│  │ resolve_targets()       ││  校验 @<member> 是否在花名册
│  └─────────────────────────┘│
│  ┌─────────────────────────┐│
│  │ InteractGate            ││  并发门控（Run 期间允许 interact）
│  └─────────────────────────┘│
│  ┌─────────────────────────┐│
│  │ _dispatch_payload()     ││  按类型分发
│  │  ├ GodView → deliver_to_leader (leader DeepAgent)
│  │  ├ Operator → UserInbox.direct/broadcast
│  │  └ HumanAgent → HumanAgentInbox.send
│  └─────────────────────────┘│
└─────────────────────────────┘
   │
   ▼ (2) 消息到达 TeamAgent
┌─────────────────────────────┐
│  StreamController (9.60)    │  流式执行（startRound → executeRound）
└─────────────────────────────┘
   │
   ▼ (3) 协调
┌─────────────────────────────┐
│  CoordinationKernel (9.62)  │  事件总线 + 邮箱 + 任务调度
└─────────────────────────────┘
```

## 设计决策

| 决策点 | 结论 | 原因 |
|--------|------|------|
| 实现范围 | 完整 6 子模块 | 全部落地，不留半成品 |
| 外部依赖处理 | 字段 `any` + 方法空 stub + 注释标注回填章节 | 与项目现有 ⤵️ 模式一致 |
| 包结构 | 1:1 对齐 Python 目录 | `interaction/` + `runtime/` 两个包 |
| 并发模型 | CloseAndDrain 带 ctx，其余方法不带 ctx | InteractGate 是纯内存状态机，admit 不阻塞；仅 drain 可能需超时 |
| InteractPayload | 接口 + 类型断言，接口方法含 `Kind()` 返回载荷类型枚举 | Python isinstance 直接对译 + Kind() 辅助前筛 |
| TeamRuntimeManager | interact 完整实现，其余方法空 stub + 注释标注回填章节 | 核心方法落地，生命周期方法留到 9.61/9.62 |

## 包结构

### interaction 包

```
agent_teams/interaction/
├── doc.go                  # 包文档
├── payload.go              # Payload 类型（GodViewMessage/OperatorMessage/HumanAgentMessage/InteractPayload/DeliverResult/HumanAgentInboundEvent）
├── router.go               # 输入解析器（parse_interact_str/parse_mention/resolve_targets/deliver_direct）
├── user_inbox.go           # 用户侧收件箱（UserInbox）
└── human_agent_inbox.go    # Human-Agent 收件箱（HumanAgentInbox + 错误类型）
```

### runtime 包

```
agent_teams/runtime/
├── doc.go                  # 包文档
├── gate.go                 # InteractGate 并发门控
├── pool.go                 # ActiveTeam/TeamRuntimePool
└── manager.go              # TeamRuntimeManager（interact 完整实现，其余空 stub）
```

## 详细设计

### 1. Payload 类型（payload.go）

对齐 Python: `openjiuwen/agent_teams/interaction/payload.py`

```go
// PayloadKind 载荷类型枚举
type PayloadKind int

const (
    PayloadKindGodView    PayloadKind = iota // GodViewMessage — 直连 Leader
    PayloadKindOperator                      // OperatorMessage — 操作者视角
    PayloadKindHumanAgent                    // HumanAgentMessage — Human Agent 视角
)

// InteractPayload 交互载荷接口
// Python Union[GodViewMessage, OperatorMessage, HumanAgentMessage] 的 Go 对译
type InteractPayload interface {
    Kind() PayloadKind
    Body() string
}

// GodViewMessage 直达 Leader DeepAgent 的消息
// 对齐 Python: GodViewMessage(body: str)
type GodViewMessage struct {
    body string
}

// OperatorMessage 操作者视角消息（@成员/广播）
// 对齐 Python: OperatorMessage(body: str, target: Optional[str])
type OperatorMessage struct {
    body   string
    target *string  // nil = 广播
}

// HumanAgentMessage Human-Agent 成员消息
// 对齐 Python: HumanAgentMessage(body: str, sender: str, target: Optional[str])
type HumanAgentMessage struct {
    body   string
    sender string
    target *string  // nil = 驱动 avatar; "*" / "all" = 广播
}

// DeliverResult 投递结果
// 对齐 Python: DeliverResult(ok, message_id, reason)
type DeliverResult struct {
    OK        bool
    MessageID *string
    Reason    *string
}
// 工厂方法: NewDeliverResultSuccess(messageID), NewDeliverResultFailure(reason)
// 方法: IsOK() bool

// HumanAgentInboundEvent 团队→用户通知事件
// 对齐 Python: HumanAgentInboundEvent
type HumanAgentInboundEvent struct {
    MemberName string
    Sender     string
    Body       string
    Broadcast  bool
    MessageID  string
    Timestamp  int64  // 毫秒时间戳
}
```

### 2. Router 解析器（router.go）

对齐 Python: `openjiuwen/agent_teams/interaction/router.py`

```go
// 语法规则：
//   input := channel? recipients? body
//   channel := "# " | "$" name (" " | "@")    // 默认 "# "
//   recipients := ("@" name " ")*
//   body := <remaining text>

// BroadcastTargets 广播目标集合
var BroadcastTargets = map[string]bool{"all": true, "*": true}

// ParseInteractStr 将自由文本解析为 InteractPayload 列表
// 对齐 Python: parse_interact_str(body)
func ParseInteractStr(body string) []InteractPayload

// ParseMention 解析单个 @target body
// 对齐 Python: parse_mention(content)
func ParseMention(content string) (target string, body string, ok bool)

// IsReservedName 检查是否为运行时保留成员名
// 对齐 Python: is_reserved_name(name)
func IsReservedName(name string) bool

// MemberExistsCheck 成员存在性检查函数类型
// 对齐 Python: MemberExistsCheck = Callable[[str], Awaitable[bool]]
type MemberExistsCheck func(name string) (bool, error)

// ResolveTargets 校验 @<member> 是否在花名册中
// 对齐 Python: resolve_targets(payloads, *, member_exists)
func ResolveTargets(payloads []InteractPayload, memberExists MemberExistsCheck) ([]InteractPayload, error)

// DeliverDirect 验证 target 并发送点对点消息
// 对齐 Python: deliver_direct(body, *, sender, target, message_manager, member_exists)
func DeliverDirect(body string, sender string, target string, messageManager any, memberExists MemberExistsCheck) (*DeliverResult, error)
```

内部辅助函数：
- `namedTarget(payload InteractPayload) *string` — 提取点对点接收者
- `foldUnknownMentions(unknown []InteractPayload) InteractPayload` — 折叠未知 @提及

### 3. UserInbox（user_inbox.go）

对齐 Python: `openjiuwen/agent_teams/interaction/user_inbox.py`

```go
// UserInbox 用户侧收件箱
type UserInbox struct {
    messageManager any  // ⤵️ 待 9.55 回填: TeamMessageManager
}

// NewUserInbox 创建用户收件箱
func NewUserInbox(messageManager any) *UserInbox

// Direct 发送 @target body 点对点消息
// 对齐 Python: UserInbox.direct(target, body)
func (u *UserInbox) Direct(target string, body string) (*DeliverResult, error)

// Broadcast 广播用户侧公告
// 对齐 Python: UserInbox.broadcast(body)
func (u *UserInbox) Broadcast(body string) (*DeliverResult, error)

// DeliverToLeader 将输入投递到 Leader DeepAgent
// 对齐 Python: UserInbox.deliver_to_leader(deliver_input, body)
func DeliverToLeader(deliverInput func(ctx context.Context, content string) error, body string) (*DeliverResult, error)
```

### 4. HumanAgentInbox（human_agent_inbox.go）

对齐 Python: `openjiuwen/agent_teams/interaction/human_agent_inbox.py`

```go
// AgentLookup 解析 human-agent 成员名到活跃 TeamAgent 运行时
// 对齐 Python: AgentLookup
type AgentLookup func(sender string) *TeamAgent  // ⤵️ TeamAgent 类型待 9.55 回填

// OnInbound 团队→用户通知回调
// 对齐 Python: OnInbound
type OnInbound func(event HumanAgentInboundEvent) error

// HumanAgentNotEnabledError 团队未注册 human-agent 成员
type HumanAgentNotEnabledError struct{}

// UnknownHumanAgentError 发送者不是已注册的 human-agent 成员
type UnknownHumanAgentError struct {
    Sender string
}

// HumanAgentInbox Human-Agent 收件箱
type HumanAgentInbox struct {
    team          any          // ⤵️ 待 9.55 回填: TeamBackend
    messageManager any         // ⤵️ 待 9.55 回填: TeamMessageManager
    agentLookup   AgentLookup
    onInbound     OnInbound
}

// NewHumanAgentInbox 创建 Human-Agent 收件箱
func NewHumanAgentInbox(team any, messageManager any, agentLookup AgentLookup, onInbound OnInbound) *HumanAgentInbox

// Send 分发已解析的 human-agent 载荷
// 对齐 Python: HumanAgentInbox.send(body, to, sender)
func (h *HumanAgentInbox) Send(body string, to *string, sender *string) (*DeliverResult, error)

// OnInbound 返回团队→用户通知回调
func (h *HumanAgentInbox) GetOnInbound() OnInbound
```

### 5. InteractGate（runtime/gate.go）

对齐 Python: `openjiuwen/agent_teams/runtime/gate.py`

状态机：
```
OPEN    --Admit()------>      OPEN, inflight++
OPEN    --CloseAndDrain()--> CLOSING --(inflight==0)--> DRAINED
CLOSING --Admit()------>      nil (rejected)
*       --ConsumeDone()-->    inflight--; signal drained when zero
```

```go
// AdmissionTicket admit 成功后的不透明票据
type AdmissionTicket struct {
    gate *InteractGate
}

// InteractGate Run/Interact 并发门控
type InteractGate struct {
    closed   bool
    inflight int
    drained  chan struct{}  // inflight==0 时关闭
    mu       sync.Mutex
}

// NewInteractGate 创建新门控
func NewInteractGate() *InteractGate

// Closed 门控是否已关闭
func (g *InteractGate) Closed() bool

// Inflight 当前飞行中的载荷数
func (g *InteractGate) Inflight() int

// Admit 尝试准入一个新载荷
// 返回 nil 表示门控已关闭
// 对齐 Python: InteractGate.admit()
func (g *InteractGate) Admit() *AdmissionTicket

// ConsumeDone 标记载荷已消费
// 对齐 Python: InteractGate.consume_done(ticket)
func (g *InteractGate) ConsumeDone(ticket *AdmissionTicket)

// CloseAndDrain 关闭门控并等待飞行中载荷排空
// 对齐 Python: InteractGate.close_and_drain()
func (g *InteractGate) CloseAndDrain(ctx context.Context) error

// Reset 重置门控供新一轮 Run 使用
// 对齐 Python: InteractGate.reset()
func (g *InteractGate) Reset()
```

### 6. TeamRuntimePool（runtime/pool.go）

对齐 Python: `openjiuwen/agent_teams/runtime/pool.py`

```go
// RuntimeState 运行时状态
type RuntimeState int

const (
    RuntimeStateActive  RuntimeState = iota  // 活跃
    RuntimeStatePaused                       // 暂停
    RuntimeStateStopped                      // 已停止
)

// ActiveTeamInfo 活跃团队信息（只读视图）
type ActiveTeamInfo struct {
    TeamName  string
    SessionID string
    State     RuntimeState
}

// ActiveTeam 活跃团队条目
type ActiveTeam struct {
    Agent       any           // ⤵️ 待 9.55 回填: *TeamAgent
    SessionID   string
    State       RuntimeState
    InteractGate *InteractGate
}

// TeamRuntimePool 团队运行时池
type TeamRuntimePool struct {
    entries map[string]*ActiveTeam  // key = team_name
    mu      sync.RWMutex
}

// NewTeamRuntimePool 创建运行时池
func NewTeamRuntimePool() *TeamRuntimePool

// Get 获取活跃团队
func (p *TeamRuntimePool) Get(teamName string) *ActiveTeam

// Set 设置活跃团队
func (p *TeamRuntimePool) Set(teamName string, entry *ActiveTeam)

// Remove 移除活跃团队
func (p *TeamRuntimePool) Remove(teamName string) *ActiveTeam

// List 列出所有活跃团队信息
func (p *TeamRuntimePool) List() []ActiveTeamInfo
```

### 7. TeamRuntimeManager（runtime/manager.go）

对齐 Python: `openjiuwen/agent_teams/runtime/manager.py`

```go
// TeamRuntimeManager 团队运行时管理器
type TeamRuntimeManager struct {
    pool *TeamRuntimePool
    // ⤵️ 待后续章节回填的其他依赖
}

// NewTeamRuntimeManager 创建运行时管理器
func NewTeamRuntimeManager() *TeamRuntimeManager

// ──────────── interact 完整实现 ────────────

// Interact 路由交互载荷通过活跃团队的门控
// 对齐 Python: TeamRuntimeManager.interact(payload, *, team_name, session_id)
//
// 接受 InteractPayload / string / InteractiveInput：
//   - InteractiveInput → 恢复中断
//   - string → parse_interact_str → payloads
//   - InteractPayload → 直接分发
//
// 流程：admit → resolve_targets → _dispatch_payload → consume_done
func (m *TeamRuntimeManager) Interact(
    ctx context.Context,
    payload any,  // InteractPayload | string | InteractiveInput
    teamName string,
    sessionID string,
) (*DeliverResult, error)

// resolveRecipients 校验 @<member> 接收者是否在花名册
// 对齐 Python: TeamRuntimeManager._resolve_recipients(agent, payloads)
func (m *TeamRuntimeManager) resolveRecipients(
    agent any,  // ⤵️ 待 9.55 回填: *TeamAgent
    payloads []InteractPayload,
) ([]InteractPayload, error)

// dispatchPayload 按载荷类型分发
// 对齐 Python: TeamRuntimeManager._dispatch_payload(agent, payload)
//
//   GodViewMessage    → DeliverToLeader
//   OperatorMessage   → UserInbox.direct/broadcast（含 auto_start）
//   HumanAgentMessage → HumanAgentInbox.send
func (m *TeamRuntimeManager) dispatchPayload(
    ctx context.Context,
    agent any,  // ⤵️ 待 9.55 回填: *TeamAgent
    payload InteractPayload,
) (*DeliverResult, error)

// ──────────── 其余方法空 stub + 注释标注回填章节 ────────────

// Activate 激活团队
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Activate(ctx context.Context, teamName string, sessionID string, agent any) error

// Finalize 终结团队运行
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Finalize(ctx context.Context, teamName string, sessionID string) error

// Pause 暂停团队
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Pause(ctx context.Context, teamName string, sessionID string) (bool, error)

// StopTeam 停止团队
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) StopTeam(ctx context.Context, teamName string, sessionID string) (bool, error)

// DeleteTeam 删除团队
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) DeleteTeam(ctx context.Context, teamName string, sessionID string) (bool, error)

// RegisterHumanAgentInbound 注册团队→用户通知回调
// ⤵️ 待 9.55 TeamBackend 回填
func (m *TeamRuntimeManager) RegisterHumanAgentInbound(ctx context.Context, teamName string, sessionID string, memberName string, callback any) (bool, error)
```

## 回填清单

实现完成后需要回填的已有 ⤵️ 标记：

| 文件 | 行 | 当前标记 | 回填内容 |
|------|-----|---------|---------|
| `stream_controller.go:58` | pendingInterruptResumes | `[]any` → `[]interaction.InteractPayload` 或 `[]session.InteractiveInput` |
| `stream_controller.go:558` | streamOneRound | inputMap 补充 sessionID/teamSession |
| `team_agent.go:489` | Interact() | 委托 `runtimeManager.Interact()` |
| `team_agent.go:496` | Broadcast() | 委托 `UserInbox.Broadcast()` |
| `team_agent.go:503` | HumanAgentSay() | 委托 `HumanAgentInbox.Send()` |

## 前置章节建议

实现本章节前，以下尚未完成的章节可能需要优先考虑：

| 章节 | 状态 | 影响 | 建议 |
|------|------|------|------|
| 9.55 TeamAgent | ☐ | `dispatchPayload` 和 `resolveRecipients` 依赖 TeamAgent 的 `deliver_input`/`auto_start_member`/`auto_start_all`/`lookup_human_agent_runtime`/`team_backend` 等方法 | 部分方法已在 team_agent.go 有空 stub，可用 any 占位 |
| 9.61 RecoveryManager | ☐ | 不影响 Interaction 层 | 无需前置 |
| 9.62 CoordinationKernel | ☐ | 不影响 Interaction 层（Interaction 在 Coordination 上方） | 无需前置 |

**结论**：Interaction 层可以独立实现，不需要等待其他章节。所有对 TeamAgent 的依赖通过 `any` 占位 + 接口函数类型（如 `AgentLookup`、`MemberExistsCheck`）解耦。
