# 9.60 StreamController 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 StreamController — TeamAgent 的流式控制层，管理轮次生命周期、流式分块处理、输入投递、中断处理和重试逻辑。

**Architecture:** 对齐 Python `StreamController`（`openjiuwen/agent_teams/agent/stream_controller.py`），采用 goroutine + channel 映射 asyncio.Task + asyncio.Queue。通过 `PrivateAgentResources.Harness()` 访问 TeamHarness（不加额外接口层）。streamQueue 统一用指针类型。

**Tech Stack:** Go 1.22+, goroutine/channel, context cancel

**设计文档:** `docs/superpowers/specs/2026-12-01-stream-controller-9.60-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agent_teams/agent/stream_controller.go` | StreamController 核心实现 |
| 创建 | `internal/agent_teams/agent/stream_controller_test.go` | 单元测试 |
| 修改 | `internal/agent_teams/schema/stream.go` | NewTeamOutputSchema 返回指针 |
| 修改 | `internal/agent_teams/harness.go` | RunStreaming 返回具体类型 + 实现分支 1 |
| 修改 | `internal/agent_teams/agent/team_agent.go` | streamController 类型回填 + 14 个方法实现 |
| 修改 | `internal/agent_teams/agent/spawn_manager.go` | wireInprocessChunkForward 回填 |
| 修改 | `internal/agent_teams/spawn/inprocess_handle.go` | chunkForward 类型回填 |
| 修改 | `internal/agent_teams/agent/doc.go` | 更新文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.60 状态更新 |

---

### Task 1: TeamOutputSchema 返回指针 + SchemaType/Validate 方法

**Files:**
- Modify: `internal/agent_teams/schema/stream.go`
- Test: `internal/agent_teams/schema/stream_test.go`（如果不存在则创建）

TeamOutputSchema 嵌入了 `stream.OutputSchema`，但 OutputSchema 的 `SchemaType()` 和 `Validate()` 是值接收者方法。TeamOutputSchema 需要有自己的指针接收者实现 `Schema` 接口，确保 `*TeamOutputSchema` 满足 `streambase.Schema` 接口。

- [ ] **Step 1: 修改 NewTeamOutputSchema 返回指针**

将 `internal/agent_teams/schema/stream.go` 中的 `NewTeamOutputSchema` 改为返回 `*TeamOutputSchema`：

```go
// NewTeamOutputSchema 从普通 OutputSchema 构建带标签的团队 chunk。
// 对齐 Python: TeamOutputSchema.from_output(base, source_member=..., role=...)
//
// 返回新实例指针；原始 base 不会被修改，DeepAgent 内部保留其对象标识。
func NewTeamOutputSchema(base stream.OutputSchema, sourceMember *string, role *TeamRole) *TeamOutputSchema {
	return &TeamOutputSchema{
		OutputSchema: base,
		SourceMember: sourceMember,
		Role:         role,
	}
}
```

- [ ] **Step 2: 为 *TeamOutputSchema 添加 Schema 接口实现**

在 `stream.go` 的非导出函数区块前添加：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// SchemaType 实现 stream.Schema 接口。
func (s *TeamOutputSchema) SchemaType() string { return s.Type }

// Validate 实现 stream.Schema 接口。
func (s *TeamOutputSchema) Validate() error { return s.OutputSchema.Validate() }
```

- [ ] **Step 3: 编写/更新测试**

在 `internal/agent_teams/schema/` 下确保测试覆盖：
- `*TeamOutputSchema` 满足 `stream.Schema` 接口（编译期断言）
- `NewTeamOutputSchema` 返回指针
- `SchemaType()` / `Validate()` 正常工作

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/schema/... -v -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/schema/stream.go internal/agent_teams/schema/stream_test.go
git commit -m "feat(9.60): TeamOutputSchema 返回指针 + 实现 Schema 接口"
```

---

### Task 2: TeamHarness.RunStreaming 返回类型修改

**Files:**
- Modify: `internal/agent_teams/harness.go:255-257`

- [ ] **Step 1: 修改 RunStreaming 签名和实现分支 1**

将 `harness.go` 中的 `RunStreaming` 从：

```go
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (any, error) {
	return nil, nil
}
```

改为：

```go
// RunStreaming 从底层 Agent 流式输出 chunk。
// 对齐 Python: TeamHarness.run_streaming(inputs, session_id, team_session)
//
// 分支 1（teamSession 为 nil 且非 initialPlanMode）：直接调 runner.RunAgentStreaming。
// 分支 2（有 teamSession）：⤵️ 待 9.57+ session 层回填。
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (<-chan stream.Schema, error) {
	if teamSession == nil && !h.initialPlanMode {
		return runner.RunAgentStreaming(ctx,
			runner.AgentRef{Agent: h.deepAgent},
			inputs,
			runner.SessionRef{ID: sessionID},
			nil, nil, nil,
		)
	}
	// ⤵️ 待 9.57+ session 层回填：实现 _prepare_agent_session + _ensure_initial_plan_mode
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}
```

需要在 `harness.go` 头部添加 import：

```go
import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

注意：需要检查 `runner.AgentRef{Agent: h.deepAgent}` 是否类型兼容（`h.deepAgent` 当前是 `any`，`AgentRef.Agent` 可能是 `any` 或具体类型）。如果 `AgentRef.Agent` 是 `any` 则直接兼容。

- [ ] **Step 2: 同步修改 harness.go 中三个 ⤵️ 标记注释**

更新 `IsPendingInterruptResumeValid` / `RequestCompletionPoll` / `WakeMailboxIfInterruptCleared` 的注释，将"待 9.60 StreamController 章节回填"改为"⤴️ 9.60 已实现 StreamController，此方法由 StreamController 通过 resources.Harness() 调用"。

- [ ] **Step 3: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/harness.go
git commit -m "feat(9.60): TeamHarness.RunStreaming 返回具体类型 + 实现分支 1"
```

---

### Task 3: StreamController 核心结构体 + 构造函数 + 常量

**Files:**
- Create: `internal/agent_teams/agent/stream_controller.go`

- [ ] **Step 1: 创建 stream_controller.go，写入完整结构体定义、常量和构造函数**

```go
package agent

import (
	"context"
	"fmt"
	"regexp"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkObserver 分块观察者回调。
// 对齐 Python: ChunkObserver = Callable[[OutputSchema], Awaitable[None]]
// 每个分块标注来源成员后触发，用于 SpawnManager 将 Teammate chunk 转发到 Leader 的 streamQueue。
type ChunkObserver func(ctx context.Context, chunk streambase.Schema) error

// StreamController 管理轮次生命周期、流式分块处理、输入投递、中断处理和重试逻辑。
// 对齐 Python: StreamController (openjiuwen/agent_teams/agent/stream_controller.py)
//
// 职责：
//   - 轮次生命周期管理（startRound → executeRound → 自动续轮）
//   - 流式分块处理（harness.RunStreaming → tagChunk → streamQueue + observer 扇出）
//   - 输入投递（steer/followUp/pendingInputs 排队）
//   - 中断处理（hasPendingInterrupt / isValidInterruptResume / dequeueValidInterruptResume）
//   - 协作取消（cooperativeCancel: 两阶段关闭）
//   - 重试逻辑（错误码 181001 最多重试 10 次）
type StreamController struct {
	// getBlueprint 蓝图获取器（延迟获取，configure 后才有值）
	getBlueprint func() *TeamAgentBlueprint
	// state 共享可变状态
	state *TeamAgentState
	// resources 每实例运行时资源（持有 harness）
	resources *PrivateAgentResources
	// updateStatus 成员状态更新回调
	updateStatus func(ctx context.Context, status atschema.MemberStatus) error
	// updateExecution 执行状态更新回调
	updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error
	// wakeMailboxCb 中断清除后唤醒邮箱回调
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
	wakeMailboxCb func(ctx context.Context) error
	// requestCompletionPollCb 轮次干净结束时请求完成轮询的回调（仅 Leader 传入）
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
	requestCompletionPollCb func(ctx context.Context) error

	// streamQueue 流式分块队列（nil sentinel 关闭流）
	// Python: self.stream_queue: Optional[asyncio.Queue] = None
	streamQueue chan streambase.Schema
	// cancelRound 取消当前轮次的 context cancel 函数
	cancelRound context.CancelFunc
	// roundDone 当前轮次是否完成的信号（goroutine 关闭表示完成）
	roundDone chan struct{}
	// streamingActive 是否正在流式输出
	streamingActive bool
	// cancelRequested 当前轮次是否被协作取消
	cancelRequested bool
	// pendingInterruptResumes 待处理的中断恢复输入
	// ⤵️ 待 Interaction 层实现后回填类型：当前用 any 占位
	pendingInterruptResumes []any
	// pendingInputs 待处理的输入队列（轮次结束后自动消费）
	pendingInputs []any
	// chunkObservers 分块观察者列表（SpawnManager 注册，用于 Teammate chunk 转发到 Leader）
	chunkObservers []ChunkObserver
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRetryAttempts 最大重试次数
	// 对齐 Python: _MAX_RETRY_ATTEMPTS = 10
	maxRetryAttempts = 10
	// cooperativeAbortTimeout 协作取消超时
	// 对齐 Python: _COOPERATIVE_ABORT_TIMEOUT_SECONDS = 2.0
	cooperativeAbortTimeout = 2 * time.Second
	// retryQuery 重试时的查询内容
	// 对齐 Python: _RETRY_QUERY = "刚才有异常状况，继续执行"
	retryQuery = "刚才有异常状况，继续执行"
	// taskFailedPayloadType task_failed 载荷类型
	// 对齐 Python: _TASK_FAILED_PAYLOAD_TYPE = "task_failed"
	taskFailedPayloadType = "task_failed"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// retryableErrorCodes 可重试的错误码集合
	// 对齐 Python: _RETRYABLE_ERROR_CODES = {181001}
	retryableErrorCodes = map[int]bool{181001: true}
	// errorCodePattern 错误码正则
	// 对齐 Python: _ERROR_CODE_PATTERN = re.compile(r"^\[(\d+)\]")
	errorCodePattern = regexp.MustCompile(`^\[(\d+)\]`)
	// scLogComponent StreamController 日志组件
	scLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StreamControllerOption 流式控制器可选配置
type StreamControllerOption func(*StreamController)

// WithWakeMailbox 设置中断清除后的邮箱唤醒回调
func WithWakeMailbox(cb func(ctx context.Context) error) StreamControllerOption {
	return func(sc *StreamController) { sc.wakeMailboxCb = cb }
}

// WithRequestCompletionPoll 设置轮次干净结束后的完成轮询回调
func WithRequestCompletionPoll(cb func(ctx context.Context) error) StreamControllerOption {
	return func(sc *StreamController) { sc.requestCompletionPollCb = cb }
}

// NewStreamController 创建新的流式控制器。
// 对齐 Python: StreamController.__init__(blueprint_getter, state, resources, status_updater,
//
//	execution_updater, wake_mailbox_callback, request_completion_poll_callback)
func NewStreamController(
	getBlueprint func() *TeamAgentBlueprint,
	state *TeamAgentState,
	resources *PrivateAgentResources,
	updateStatus func(ctx context.Context, status atschema.MemberStatus) error,
	updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error,
	opts ...StreamControllerOption,
) *StreamController {
	sc := &StreamController{
		getBlueprint:    getBlueprint,
		state:           state,
		resources:       resources,
		updateStatus:    updateStatus,
		updateExecution: updateExecution,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}
```

- [ ] **Step 2: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/stream_controller.go
git commit -m "feat(9.60): StreamController 结构体 + 构造函数 + 常量"
```

---

### Task 4: 状态查询方法 + 观察者管理 + tagChunk

**Files:**
- Modify: `internal/agent_teams/agent/stream_controller.go`

在 Task 3 创建的文件中追加方法。按编码规范声明顺序排列。

- [ ] **Step 1: 添加导出方法**

在 `NewStreamController` 函数之后，非导出函数区块之前添加：

```go
// AddChunkObserver 注册分块观察者。
// 对齐 Python: StreamController.add_chunk_observer(cb)
// 观察者在分块标注来源成员并写入 streamQueue 之后触发。
func (sc *StreamController) AddChunkObserver(cb ChunkObserver) {
	sc.chunkObservers = append(sc.chunkObservers, cb)
}

// RemoveChunkObserver 移除分块观察者（幂等）。
// 对齐 Python: StreamController.remove_chunk_observer(cb)
func (sc *StreamController) RemoveChunkObserver(cb ChunkObserver) {
	for i, ob := range sc.chunkObservers {
		if &ob == &cb {
			// 比较函数指针不靠谱，改为遍历比较
			_ = i
		}
	}
	// 幂等：找不到也无声返回（对齐 Python contextlib.suppress(ValueError)）
	for i, ob := range sc.chunkObservers {
		if fmt.Sprintf("%p", ob) == fmt.Sprintf("%p", cb) {
			sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
			return
		}
	}
}

// IsAgentRunning Agent 是否正在运行（流式输出中）。
// 对齐 Python: StreamController.is_agent_running()
func (sc *StreamController) IsAgentRunning() bool {
	return sc.streamingActive
}

// HasInFlightRound 是否有飞行中的轮次。
// 对齐 Python: StreamController.has_in_flight_round()
// Python: agent_task is not None and not agent_task.done()
// Go: roundDone != nil 且未关闭
func (sc *StreamController) HasInFlightRound() bool {
	if sc.roundDone == nil {
		return false
	}
	select {
	case <-sc.roundDone:
		return false
	default:
		return true
	}
}

// HasPendingInterrupt 是否有待处理的中断。
// 对齐 Python: StreamController.has_pending_interrupt()
func (sc *StreamController) HasPendingInterrupt() bool {
	harness := sc.resources.Harness()
	if harness == nil {
		return false
	}
	return harness.HasPendingInterrupt()
}

// IsValidInterruptResume 验证用户输入是否为有效中断恢复。
// 对齐 Python: StreamController.is_valid_interrupt_resume(user_input)
// ⤵️ 待 harness.IsPendingInterruptResumeValid 回填后实现完整逻辑
func (sc *StreamController) IsValidInterruptResume(userInput any) bool {
	harness := sc.resources.Harness()
	if harness == nil {
		return false
	}
	return harness.IsPendingInterruptResumeValid()
}
```

注意：`RemoveChunkObserver` 的函数比较问题 — Go 中 `func` 不可比较。需要改用索引或包装为带 ID 的结构体。实际上 Python 中 `remove_chunk_observer` 用列表的 `remove()` 方法按引用比较，Go 中函数值不可直接用 `==` 比较。最简方案：改 `ChunkObserver` 为带 ID 的接口或使用 slice 遍历+反射。实际项目中最简洁的方式是用 `reflect.ValueOf(cb).Pointer()` 比较函数指针。

更正后的 `RemoveChunkObserver`：

```go
// RemoveChunkObserver 移除分块观察者（幂等）。
// 对齐 Python: StreamController.remove_chunk_observer(cb)
func (sc *StreamController) RemoveChunkObserver(cb ChunkObserver) {
	for i, ob := range sc.chunkObservers {
		// 比较函数指针地址（对齐 Python list.remove 按引用比较）
		if reflect.ValueOf(ob).Pointer() == reflect.ValueOf(cb).Pointer() {
			sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
			return
		}
	}
	// 幂等：找不到也无声返回（对齐 Python contextlib.suppress(ValueError)）
}
```

需要在 import 中添加 `"reflect"`。

- [ ] **Step 2: 添加非导出方法**

在文件末尾非导出函数区块添加：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// memberName 解析当前成员名。
// 对齐 Python: StreamController._member_name()
func (sc *StreamController) memberName() string {
	bp := sc.getBlueprint()
	if bp == nil {
		return ""
	}
	name := bp.MemberName()
	if name == nil {
		return ""
	}
	return *name
}

// tagChunk 标注分块的来源成员和角色。
// 对齐 Python: StreamController._tag_chunk(chunk)
//
// 四种情况：
//  1. 非 OutputSchema（TraceSchema/CustomSchema）或无 memberName → 透传
//  2. 已是 *TeamOutputSchema 且标签匹配 → 透传
//  3. 已是 *TeamOutputSchema 但标签不匹配 → 浅拷贝更新标签
//  4. 是 *OutputSchema → 升级为 *TeamOutputSchema
func (sc *StreamController) tagChunk(chunk streambase.Schema) streambase.Schema {
	bp := sc.getBlueprint()
	if bp == nil {
		return chunk
	}
	memberName := bp.MemberName()
	if memberName == nil || *memberName == "" {
		return chunk
	}

	// 情况 2+3：已是 TeamOutputSchema
	if teamChunk, ok := chunk.(*atschema.TeamOutputSchema); ok {
		if teamChunk.SourceMember != nil && *teamChunk.SourceMember == *memberName &&
			teamChunk.Role != nil && *teamChunk.Role == bp.Role() {
			return chunk // 标签匹配 → 透传
		}
		// 标签不匹配 → 浅拷贝更新（对齐 Python model_copy(update=...)）
		role := bp.Role()
		newChunk := *teamChunk
		newChunk.SourceMember = memberName
		newChunk.Role = &role
		return &newChunk
	}

	// 情况 4：是 OutputSchema → 升级为 TeamOutputSchema
	if outChunk, ok := chunk.(*streambase.OutputSchema); ok {
		role := bp.Role()
		return atschema.NewTeamOutputSchema(*outChunk, memberName, &role)
	}

	// 情况 1：TraceSchema/CustomSchema → 透传
	return chunk
}

// fanOutToObservers 扇出分块到观察者，异常时自动移除。
// 对齐 Python: _chunk_observers 循环 + exception auto-detach
func (sc *StreamController) fanOutToObservers(ctx context.Context, tagged streambase.Schema) {
	for _, ob := range sc.chunkObservers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error(scLogComponent).
						Str("member_name", sc.memberName()).
						Any("panic", r).
						Msg("chunk observer panicked; detaching")
					sc.RemoveChunkObserver(ob)
				}
			}()
			if err := ob(ctx, tagged); err != nil {
				logger.Error(scLogComponent).
					Str("member_name", sc.memberName()).
					Err(err).
					Msg("chunk observer raised; detaching")
				sc.RemoveChunkObserver(ob)
			}
		}()
	}
}

// detectTaskFailed 检测 chunk 中的 task_failed 错误。
// 对齐 Python: _detect_task_failed(chunk)
func detectTaskFailed(chunk streambase.Schema) (errorCode *int, errorText string) {
	outChunk, ok := chunk.(*streambase.OutputSchema)
	if !ok {
		return nil, ""
	}
	payload := outChunk.Payload
	if payload == nil {
		return nil, ""
	}
	// payload 应该是 map[string]any 或带 Type/Data 字段的结构
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return nil, ""
	}
	payloadType, _ := payloadMap["type"].(string)
	if payloadType != taskFailedPayloadType {
		return nil, ""
	}
	data, _ := payloadMap["data"].([]any)
	var text string
	if len(data) > 0 {
		if dataMap, ok := data[0].(map[string]any); ok {
			text, _ = dataMap["text"].(string)
		}
	}
	match := errorCodePattern.FindStringSubmatch(text)
	if len(match) > 1 {
		code := 0
		if _, err := fmt.Sscanf(match[1], "%d", &code); err == nil {
			return &code, text
		}
	}
	return nil, text
}

// isRetryableErrorCode 检查错误码是否可重试。
// 对齐 Python: error_code in _RETRYABLE_ERROR_CODES
func isRetryableErrorCode(code int) bool {
	return retryableErrorCodes[code]
}
```

- [ ] **Step 3: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/stream_controller.go
git commit -m "feat(9.60): 状态查询 + 观察者管理 + tagChunk + 辅助函数"
```

---

### Task 5: 流控制方法（StartRound/Steer/FollowUp/CancelAgent/CloseStream/EmitCompletionAndClose/DrainAgentTask/CooperativeCancel）

**Files:**
- Modify: `internal/agent_teams/agent/stream_controller.go`

- [ ] **Step 1: 在导出函数区块添加流控制方法**

在 `IsValidInterruptResume` 方法之后添加：

```go
// StartRound 启动一个新轮次。
// 对齐 Python: StreamController.start_round(content)
func (sc *StreamController) StartRound(ctx context.Context, content any) error {
	harness := sc.resources.Harness()
	if harness == nil || sc.streamQueue == nil {
		return nil
	}
	preview := "non-string"
	if s, ok := content.(string); ok {
		if len(s) > 120 {
			preview = s[:120]
		} else {
			preview = s
		}
	}
	logger.Info(scLogComponent).Str("member_name", sc.memberName()).
		Str("preview", preview).Msg("start_agent")

	sc.startRound(ctx, content)
	return nil
}

// Steer 运行中转向。
// 对齐 Python: StreamController.steer(content)
func (sc *StreamController) Steer(ctx context.Context, content string) error {
	harness := sc.resources.Harness()
	if harness != nil {
		return harness.Steer(ctx, content)
	}
	return nil
}

// FollowUp 追加输入。
// 对齐 Python: StreamController.follow_up(content)
func (sc *StreamController) FollowUp(ctx context.Context, content string) error {
	harness := sc.resources.Harness()
	if harness != nil {
		return harness.FollowUp(ctx, content)
	}
	return nil
}

// CancelAgent 取消飞行中的轮次，推进执行状态机。
// 对齐 Python: StreamController.cancel_agent()
// 状态机：RUNNING → CANCEL_REQUESTED → CANCELLING → CooperativeCancel
func (sc *StreamController) CancelAgent(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelRequested)
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelling)
	return sc.CooperativeCancel(ctx)
}

// CloseStream 关闭流（向 streamQueue 写入 nil sentinel）。
// 对齐 Python: StreamController.close_stream()
func (sc *StreamController) CloseStream() {
	if sc.streamQueue != nil {
		// 非阻塞写入 nil sentinel
		select {
		case sc.streamQueue <- nil:
		default:
		}
	}
}

// EmitCompletionAndClose 发出 team.completed 标记块再关闭流。
// 对齐 Python: StreamController.emit_completion_and_close(member_count, task_count)
func (sc *StreamController) EmitCompletionAndClose(memberCount, taskCount int) {
	if sc.streamQueue == nil {
		return
	}
	bp := sc.getBlueprint()
	marker := &atschema.TeamOutputSchema{
		OutputSchema: streambase.OutputSchema{
			Type:  "message",
			Index: 0,
			Payload: map[string]any{
				"event_type":   "team.completed",
				"member_count": memberCount,
				"task_count":   taskCount,
			},
		},
	}
	if bp != nil {
		name := bp.MemberName()
		role := bp.Role()
		marker.SourceMember = name
		marker.Role = &role
	}
	// marker 先入队，然后 nil sentinel 关闭流
	select {
	case sc.streamQueue <- marker:
	default:
	}
	sc.CloseStream()
}

// DrainAgentTask 拆卸飞行中的轮次（用于协调生命周期暂停/停止）。
// 对齐 Python: StreamController.drain_agent_task()
// 清除 pendingInputs + pendingInterruptResumes，然后 CancelAgent
func (sc *StreamController) DrainAgentTask(ctx context.Context) error {
	sc.pendingInputs = nil
	sc.pendingInterruptResumes = nil
	return sc.CancelAgent(ctx)
}

// CooperativeCancel 协作取消：两阶段关闭。
// 对齐 Python: StreamController.cooperative_cancel()
// Phase 1: 设 cancelRequested + harness.Abort()
// Phase 2: 等 2s → 超时则 cancelRound()（强制取消 goroutine）
func (sc *StreamController) CooperativeCancel(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	sc.cancelRequested = true
	harness := sc.resources.Harness()
	if harness != nil {
		if err := harness.Abort(ctx); err != nil {
			logger.Debug(scLogComponent).Str("member_name", sc.memberName()).
				Err(err).Msg("harness.Abort failed")
		}
	}
	// 等待 goroutine 自然完成或超时
	select {
	case <-sc.roundDone:
		return nil
	case <-time.After(cooperativeAbortTimeout):
		if sc.cancelRound != nil {
			sc.cancelRound()
		}
		// 等待 goroutine 退出
		select {
		case <-sc.roundDone:
		case <-ctx.Done():
		}
		return nil
	}
}
```

- [ ] **Step 2: 添加非导出 startRound 方法**

在非导出函数区块添加：

```go
// startRound 启动一个新轮次（内部方法，由 StartRound 和 runOneRound 的续轮逻辑调用）。
// 对齐 Python: StreamController.start_round(content) 内部启动逻辑
func (sc *StreamController) startRound(ctx context.Context, content any) {
	harness := sc.resources.Harness()
	if harness == nil || sc.streamQueue == nil {
		return
	}
	roundCtx, cancel := context.WithCancel(ctx)
	sc.cancelRound = cancel
	sc.roundDone = make(chan struct{})
	go func() {
		defer close(sc.roundDone)
		defer sc.logRoundPanic()
		sc.runOneRound(roundCtx, content)
	}()
}

// logRoundPanic 记录轮次 goroutine 的 panic。
// 对齐 Python: StreamController._log_agent_task_exception(task)
func (sc *StreamController) logRoundPanic() {
	if r := recover(); r != nil {
		logger.Error(scLogComponent).Str("member_name", sc.memberName()).
			Any("panic", r).Msg("_run_one_round goroutine panicked")
	}
}
```

- [ ] **Step 3: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/stream_controller.go
git commit -m "feat(9.60): 流控制方法（StartRound/CancelAgent/CloseStream 等）"
```

---

### Task 6: 核心轮次执行方法（runOneRound/executeRound/streamOneRound/runRetryingStream）

**Files:**
- Modify: `internal/agent_teams/agent/stream_controller.go`

- [ ] **Step 1: 添加 runOneRound**

在非导出函数区块添加：

```go
// runOneRound 执行一个完整轮次。
// 对齐 Python: StreamController._run_one_round(message)
func (sc *StreamController) runOneRound(ctx context.Context, message any) {
	// 重置取消标记（对齐 Python: self._cancel_requested = False）
	sc.cancelRequested = false
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：set_member_id 上下文变量

	harness := sc.resources.Harness()
	if harness != nil {
		harness.InitCwdForRound()
	}

	_ = sc.updateStatus(ctx, atschema.MemberStatusReady)
	_ = sc.updateStatus(ctx, atschema.MemberStatusBusy)

	cancelled := false
	func() {
		defer func() {
			// 对齐 Python: agent_task = None
			sc.cancelRound = nil

			// 对齐 Python: if self._state.team_cleaned: close_stream()
			if sc.state.TeamCleaned {
				logger.Info(scLogComponent).Str("member_name", sc.memberName()).
					Msg("team_cleaned set; closing stream after round")
				sc.CloseStream()
				return
			}

			// 对齐 Python: elif not cancelled and not _cancel_requested:
			if !cancelled && !sc.cancelRequested {
				nextResume := sc.dequeueValidInterruptResume()
				if nextResume != nil && sc.streamQueue != nil {
					sc.startRound(ctx, nextResume)
				} else if len(sc.pendingInputs) > 0 && sc.streamQueue != nil {
					drained := sc.pendingInputs
					sc.pendingInputs = nil
					combined := sc.combinePendingInputs(drained)
					sc.startRound(ctx, combined)
				} else {
					_ = sc.wakeMailboxIfInterruptCleared(ctx)
					// ⤵️ 待 TeamMember 状态检查回填：检查 SHUTDOWN_REQUESTED → CloseStream()
					// ⤵️ 待 9.62 CoordinationKernel 章节回填：requestCompletionPollCb
					if sc.requestCompletionPollCb != nil {
						_ = sc.requestCompletionPollCb(ctx)
					}
				}
			}
		}()

		// 对齐 Python: await _execute_round(message)
		// 检查 context 是否已取消
		if ctx.Err() != nil {
			cancelled = true
			_ = sc.updateStatus(ctx, atschema.MemberStatusError)
			return
		}
		sc.executeRound(ctx, message)
		_ = sc.updateStatus(ctx, atschema.MemberStatusReady)
	}()
}

// executeRound 执行状态机。
// 对齐 Python: StreamController._execute_round(message)
// 状态转换：STARTING → RUNNING → (COMPLETING→COMPLETED | CANCELLED | FAILED) → IDLE
func (sc *StreamController) executeRound(ctx context.Context, message any) {
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusStarting)
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusRunning)

	if ctx.Err() != nil {
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
		return
	}

	err := sc.runRetryingStream(ctx, message)

	if err != nil {
		if sc.cancelRequested {
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		} else {
			logger.Error(scLogComponent).Err(err).Str("member_name", sc.memberName()).
				Msg("DeepAgent round error")
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusFailed)
		}
	} else {
		if sc.cancelRequested {
			// 对齐 Python: if self._cancel_requested: CANCELLED
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		} else {
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleting)
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleted)
		}
	}
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
}
```

- [ ] **Step 2: 添加 streamOneRound + runRetryingStream + 辅助方法**

```go
// streamOneRound 执行单轮流式读取。
// 对齐 Python: StreamController._stream_one_round(query)
// 返回 nil 表示成功，返回 (errorCode, errorText) 表示检测到 task_failed
func (sc *StreamController) streamOneRound(ctx context.Context, query any) (errorCode *int, errorText string) {
	sc.streamingActive = true
	defer func() { sc.streamingActive = false }()

	harness := sc.resources.Harness()
	if harness == nil {
		return nil, ""
	}

	// ⤵️ 待 Interaction 层回填：sessionID 和 teamSession 从 state 读取
	inputMap := map[string]any{"query": query}
	chunkCh, err := harness.RunStreaming(ctx, inputMap, "", nil)
	if err != nil {
		return nil, ""
	}

	errorSeen := false
	for chunk := range chunkCh {
		if chunk == nil {
			// nil sentinel，流关闭
			break
		}
		if errorSeen {
			continue
		}
		detectedCode, detectedText := detectTaskFailed(chunk)
		if detectedCode != nil || detectedText != "" {
			errorSeen = true
			errorCode = detectedCode
			errorText = detectedText
			continue
		}
		tagged := sc.tagChunk(chunk)
		if sc.streamQueue != nil {
			select {
			case sc.streamQueue <- tagged:
			case <-ctx.Done():
				return nil, ""
			}
		}
		sc.fanOutToObservers(ctx, tagged)
	}
	if !errorSeen {
		return nil, ""
	}
	return errorCode, errorText
}

// runRetryingStream 带重试的流式执行。
// 对齐 Python: StreamController._run_retrying_stream(initial_query)
func (sc *StreamController) runRetryingStream(ctx context.Context, initialQuery any) error {
	currentQuery := initialQuery
	attempt := 0
	for {
		errorCode, errorText := sc.streamOneRound(ctx, currentQuery)
		if errorCode == nil {
			return nil
		}
		code := *errorCode
		if isRetryableErrorCode(code) && attempt < maxRetryAttempts {
			attempt++
			logger.Warn(scLogComponent).Int("error_code", code).
				Int("attempt", attempt).Int("max_attempts", maxRetryAttempts).
				Str("error_text", errorText).Msg("DeepAgent round transient error")
			currentQuery = retryQuery
			continue
		}
		logger.Error(scLogComponent).Int("error_code", code).
			Int("attempts", attempt).Str("error_text", errorText).
			Msg("DeepAgent round failed")
		return fmt.Errorf("streaming task failed after %d retries, last error code=%d: %s",
			attempt, code, errorText)
	}
}

// dequeueValidInterruptResume 弹出有效中断恢复。
// 对齐 Python: StreamController._dequeue_valid_interrupt_resume()
// ⤵️ 待 Interaction 层实现后回填具体类型
func (sc *StreamController) dequeueValidInterruptResume() any {
	for len(sc.pendingInterruptResumes) > 0 {
		candidate := sc.pendingInterruptResumes[0]
		sc.pendingInterruptResumes = sc.pendingInterruptResumes[1:]
		if sc.IsValidInterruptResume(candidate) {
			return candidate
		}
	}
	return nil
}

// wakeMailboxIfInterruptCleared 中断清除后唤醒邮箱。
// 对齐 Python: StreamController._wake_mailbox_if_interrupt_cleared()
func (sc *StreamController) wakeMailboxIfInterruptCleared(ctx context.Context) error {
	if sc.wakeMailboxCb == nil {
		return nil
	}
	return sc.wakeMailboxCb(ctx)
}

// combinePendingInputs 合并多个待处理输入。
// 对齐 Python: "\n\n---\n\n".join(items) — 多个用分隔符合并，单个直接用
func (sc *StreamController) combinePendingInputs(items []any) any {
	if len(items) == 1 {
		return items[0]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		} else {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += p
	}
	return result
}
```

- [ ] **Step 3: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/stream_controller.go
git commit -m "feat(9.60): 核心轮次执行方法（runOneRound/executeRound/streamOneRound/runRetryingStream）"
```

---

### Task 7: StreamController 单元测试

**Files:**
- Create: `internal/agent_teams/agent/stream_controller_test.go`

- [ ] **Step 1: 创建测试文件，编写核心测试用例**

测试覆盖：
1. `NewStreamController` 构造 + Option
2. `AddChunkObserver` / `RemoveChunkObserver`
3. `tagChunk` 四种情况
4. `IsAgentRunning` / `HasInFlightRound` / `HasPendingInterrupt`
5. `CloseStream` / `EmitCompletionAndClose`
6. `detectTaskFailed` / `isRetryableErrorCode`
7. `combinePendingInputs`
8. `fanOutToObservers` 异常自动移除
9. `CooperativeCancel` 超时 + 强制取消
10. `runRetryingStream` 重试逻辑

需要构造 fake harness（实现 `*agentteams.TeamHarness` 的方法）。由于 TeamHarness 是具体类型不是接口，测试需要通过设置 `PrivateAgentResources.Harness` 字段，构造真实的（但 harness 内部 deepAgent 为 nil 的）TeamHarness。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -v -count=1 -run TestStreamController`

- [ ] **Step 3: 确保覆盖率 ≥ 85%**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/agent/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/stream_controller_test.go
git commit -m "test(9.60): StreamController 单元测试"
```

---

### Task 8: team_agent.go 回填 — streamController 类型 + 14 个方法

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`

- [ ] **Step 1: 修改 streamController 字段类型**

将 `team_agent.go:79` 的：
```go
// streamController 流式控制器
// TODO(#9.60): StreamController 类型
streamController any
```
改为：
```go
// streamController 流式控制器
streamController *StreamController
```

- [ ] **Step 2: 修改 NewTeamAgent 中的构造逻辑**

将 `team_agent.go:105` 的：
```go
// TODO(#9.60): 构建 StreamController(blueprintGetter, state, resources, ...)
```
改为：
```go
a.streamController = NewStreamController(
	a.configurator.Blueprint,
	a.state,
	a.configurator.Resources(),
	a.UpdateStatus,
	a.updateExecution,
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：WithWakeMailbox / WithRequestCompletionPoll
)
```

注意：需要确认 `UpdateStatus` 方法签名与 `func(ctx context.Context, status atschema.MemberStatus) error` 一致，以及 `updateExecution` 是否存在。如果 `updateExecution` 不存在，需要在 TeamAgent 上添加非导出方法。

- [ ] **Step 3: 回填 14 个 TODO(#9.60) 方法**

逐个修改 `team_agent.go` 中的 14 个 `TODO(#9.60)` 方法，委托给 `streamController`：

1. `IsAgentRunning()` → `return a.streamController.IsAgentRunning()`
2. `HasInFlightRound()` → `return a.streamController.HasInFlightRound()`
3. `HasPendingInterrupt()` → `return a.streamController.HasPendingInterrupt()`
4. `Invoke()` — 保留 9.62 的 TODO，但添加 streamQueue 创建逻辑
5. `Stream()` — 同 Invoke
6. `DeliverInput()` → 运行中 steer/follow-up；飞行中入队；否则 startRound
7. `StartAgent()` → `a.streamController.StartRound(ctx, content)`
8. `FollowUp()` → `a.streamController.FollowUp(ctx, content)`
9. `CancelAgent()` → `a.streamController.CancelAgent(ctx)`
10. `Steer()` → `a.streamController.Steer(ctx, content)`
11. `ResumeInterrupt()` → 验证中断 → 飞行中入队 → 否则 startRound
12. `ShutdownSelf()` → `a.streamController.CooperativeCancel(ctx)` + 状态更新
13. `ConcludeCompletedRound()` → `a.streamController.EmitCompletionAndClose(...)`
14. `IsShutdownRequested()` — 需检查 teamMember 状态

- [ ] **Step 4: 添加 updateExecution 非导出方法**

在 TeamAgent 的非导出函数区块添加：
```go
// updateExecution 更新执行状态。
// 对齐 Python: TeamAgent._update_execution(status)
func (a *TeamAgent) updateExecution(ctx context.Context, status atschema.ExecutionStatus) error {
	// ⤵️ 待 9.55 完善后实现具体状态持久化逻辑
	logger.Debug(logComponent).Str("member_name", a.MemberName()).
		Str("execution_status", string(status)).Msg("updateExecution")
	return nil
}
```

- [ ] **Step 5: 修改 StreamController() getter 返回具体类型**

将 `team_agent.go:291-293` 的：
```go
func (a *TeamAgent) StreamController() any {
	return a.streamController
}
```
改为：
```go
func (a *TeamAgent) StreamController() *StreamController {
	return a.streamController
}
```

- [ ] **Step 6: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`

- [ ] **Step 7: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "feat(9.60): team_agent.go streamController 类型回填 + 14 个方法实现"
```

---

### Task 9: spawn_manager.go + inprocess_handle.go 回填

**Files:**
- Modify: `internal/agent_teams/agent/spawn_manager.go:352-353`
- Modify: `internal/agent_teams/spawn/inprocess_handle.go:31,207-216`

- [ ] **Step 1: 修改 inprocess_handle.go 的 chunkForward 类型**

将 `inprocess_handle.go:31-32` 的：
```go
// chunkForward chunk 转发观察者引用，cleanup 时可确定性断开
// ⤵️ 预留：StreamController（9.60）实现后回填类型
chunkForward any
```
改为：
```go
// chunkForward chunk 转发观察者引用，cleanup 时可确定性断开
chunkForward agent.ChunkObserver
```

需要在 import 中添加 agent 包的引用。但 `spawn` 包和 `agent` 包之间可能存在循环依赖 — 检查依赖方向：`agent` 包 import `spawn` 包，`spawn` 包不能 import `agent` 包。

**解决方案**：在 `spawn` 包中定义 `ChunkObserver` 类型别名，或者将 `ChunkObserver` 移到一个共享的 schema 包中。最简方案：在 `spawn` 包中重新定义 `type ChunkObserver func(ctx context.Context, chunk stream.Schema) error`，然后 `agent.ChunkObserver` 与 `spawn.ChunkObserver` 类型相同但包不同，`inprocess_handle.go` 使用 `spawn.ChunkObserver`。`spawn_manager.go` 在设置时做类型转换。

更简方案：`inprocess_handle.go` 继续用 `any`，`spawn_manager.go` 中的 `wireInprocessChunkForward` 内部做类型断言。这样不引入循环依赖。

最终方案：
- `inprocess_handle.go`: `chunkForward` 保持 `any`，注释更新为 `⤴️ 9.60 已回填，类型为 agent.ChunkObserver，因循环依赖用 any 桥接`
- `ChunkForward()` getter 返回 `any`
- `SetChunkForward(v any)` setter
- `spawn_manager.go` 中 `wireInprocessChunkForward` 使用时做类型断言

- [ ] **Step 2: 实现 wireInprocessChunkForward**

在 `spawn_manager.go` 中添加方法：

```go
// wireInprocessChunkForward 接入 chunk 转发观察者。
// 对齐 Python: SpawnManager._wire_inprocess_chunk_forward(handle)
func (m *SpawnManager) wireInprocessChunkForward(handle *spawn.InProcessSpawnHandle) {
	agentRef := handle.AgentRef()
	ta, ok := agentRef.(*TeamAgent)
	if !ok {
		return
	}
	sc := ta.StreamController()
	if sc == nil {
		return
	}
	leaderSC := m.teamAgentGetter().StreamController()
	if leaderSC == nil {
		return
	}
	// 创建转发回调：teammate chunk → leader streamQueue
	var forwardCb ChunkObserver
	forwardCb = func(ctx context.Context, chunk streambase.Schema) error {
		if leaderSC.streamQueue != nil {
			select {
			case leaderSC.streamQueue <- chunk:
			default:
			}
		}
		return nil
	}
	sc.AddChunkObserver(forwardCb)
	handle.SetChunkForward(forwardCb)
}
```

然后在 `spawnInprocess` 方法中取消注释：
```go
// 原来：⤵️ 预留：StreamController（9.60）实现后回填
// m.wireInprocessChunkForward(handle)
// 改为：
m.wireInprocessChunkForward(handle)
```

注意：`streamQueue` 是 StreamController 的非导出字段，同包可访问（`agent` 包内）。

- [ ] **Step 3: 更新 inprocess_handle.go 注释**

将 `⤵️ 预留：StreamController（9.60）实现后回填类型` 改为 `⤴️ 9.60 已回填，因循环依赖保留 any 类型桥接，实际值为 agent.ChunkObserver`

将 `⤵️ 预留：StreamController（9.60）实现后回填` 改为 `⤴️ 9.60 已回填`

- [ ] **Step 4: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/agent/spawn_manager.go internal/agent_teams/spawn/inprocess_handle.go
git commit -m "feat(9.60): spawn_manager/inprocess_handle chunk 转发回填"
```

---

### Task 10: doc.go + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `internal/agent_teams/agent/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go**

将 `doc.go:29` 的：
```
//	├── stream_controller.go  # TODO(#9.60) 流式控制器
```
改为：
```
//	├── stream_controller.go  # StreamController 流式控制器（9.60）
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 `IMPLEMENTATION_PLAN.md:595` 的：
```
| 9.60 | ☐ | StreamController | 流式控制 | `openjiuwen/agent_teams/` |
```
改为：
```
| 9.60 | ✅ | StreamController | 流式控制器（结构体+构造函数+26个方法+常量+回填team_agent/spawn_manager/inprocess_handle）；⤵️ Interaction 层回填类型 | `openjiuwen/agent_teams/agent/stream_controller.py` |
```

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(9.60): 更新 doc.go 和 IMPLEMENTATION_PLAN.md"
```

---

### Task 11: 全量编译 + 测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -v -count=1`

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/agent/...`

- [ ] **Step 4: 修复任何编译/测试问题**

如果发现问题，逐个修复并 commit。

---

## 自查清单

**1. Spec 覆盖度：**
- ✅ 结构体定义 → Task 3
- ✅ 构造函数 + Option → Task 3
- ✅ 26 个方法 → Task 4-6
- ✅ 常量/变量 → Task 3
- ✅ tagChunk 四种情况 → Task 4
- ✅ 观察者管理 → Task 4
- ✅ 流控制方法 → Task 5
- ✅ 轮次执行 → Task 6
- ✅ team_agent.go 回填 → Task 8
- ✅ spawn_manager.go 回填 → Task 9
- ✅ inprocess_handle.go 回填 → Task 9
- ✅ TeamHarness.RunStreaming 修改 → Task 2
- ✅ TeamOutputSchema 指针修改 → Task 1
- ✅ doc.go 更新 → Task 10
- ✅ IMPLEMENTATION_PLAN.md 更新 → Task 10
- ✅ 测试 → Task 7

**2. Placeholder 扫描：** 无 TBD/TODO 未决项。所有 ⤵️ 标记有明确等待章节。

**3. 类型一致性：** `NewTeamOutputSchema` 在 Task 1 改为返回 `*TeamOutputSchema`，Task 4 中 `tagChunk` 使用 `atschema.NewTeamOutputSchema` 返回指针，与 streamQueue 的 `chan streambase.Schema` 一致。`ChunkObserver` 在 Task 3 定义，Task 4/5/9 统一使用。
