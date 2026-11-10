package harness_protocol

import "context"

// 本文件提供「protocol.go 签名需要、但归属后续章节实现」的最小形状占位。
// 占位只保证签名可编译，**不含任何行为**（校验 / 租约 / 持久化语义全部留待回填章节）。
// 每个占位注明 Python 依据、回填章节与回收条件；15.3 / 15.4 落地时
// 在本包原地补全，**不得**回头修改 protocol.go 或 models.go 的 HarnessContext。

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessCheckpoint 不透明的 provider 状态，作用域为单个 agent + 宿主会话。
//
// ⤵️ 15.4 回填：字段已与上游一致；5 必填校验 / sequence>=0 /
// optional 非空串 / JSON 序列化 ≤ MaxCheckpointBytes 全部留 15.4。
// 回收条件：15.4 落地 NewHarnessCheckpoint 时补全（Python: checkpoints.py:46-63）。
// Python: checkpoints.py:28-45 HarnessCheckpoint
type HarnessCheckpoint struct {
	// Provider 拥有该 checkpoint 的 provider 名
	Provider string
	// SchemaVersion provider 私有 schema 版本
	SchemaVersion string
	// AgentID agent 标识
	AgentID string
	// HostSessionID 宿主会话标识
	HostSessionID string
	// CheckpointID checkpoint 标识
	CheckpointID string
	// Sequence 同一 provider/agent/会话作用域内单调递增序号
	Sequence int
	// Data provider 私有数据（已冻结）
	Data JsonObject
	// ProviderSessionID provider 原生会话标识
	ProviderSessionID *string
	// Revision 宿主存储修订号
	Revision *string
}

// CheckpointSaveReceipt 一次幂等 checkpoint 写入的持久化回执。
//
// ⤵️ 15.4 回填：校验（两个 id 非空、sequence>=0）留 15.4。
// Python: checkpoints.py:66-79 CheckpointSaveReceipt
type CheckpointSaveReceipt struct {
	// CheckpointID checkpoint 标识
	CheckpointID string
	// Sequence 单调序号
	Sequence int
	// StorageRevision 宿主存储修订号
	StorageRevision string
}

// ──────────────────────────── 枚举 ────────────────────────────

// CheckpointReason 实现方发布 checkpoint 的原因。
//
// ⤵️ 15.4 回填：取值已与上游一致。
// Python: checkpoints.py:18-26 CheckpointReason（5 项）
type CheckpointReason string

const (
	// CheckpointReasonSessionActivated 会话已激活
	CheckpointReasonSessionActivated CheckpointReason = "session_activated"
	// CheckpointReasonTurnCompleted 轮次完成
	CheckpointReasonTurnCompleted CheckpointReason = "turn_completed"
	// CheckpointReasonStateChanged 状态发生变化
	CheckpointReasonStateChanged CheckpointReason = "state_changed"
	// CheckpointReasonPeriodic 周期性发布
	CheckpointReasonPeriodic CheckpointReason = "periodic"
	// CheckpointReasonProviderRequested provider 主动要求
	CheckpointReasonProviderRequested CheckpointReason = "provider_requested"
)

// ──────────────────────────── 常量 ────────────────────────────

// MaxCheckpointBytes checkpoint data 序列化后的字节上限。
//
// Python: checkpoints.py:15 MAX_CHECKPOINT_BYTES
const MaxCheckpointBytes = 4 * 1024 * 1024

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// HarnessCheckpointSink 宿主侧主动发布 checkpoint 的持久化目标。
//
// ⤵️ 15.4 回填：幂等 / compare-and-set / CheckpointConflictError 语义留 15.4
// （Python: checkpoints.py:79-111）。
// Python: checkpoints.py:79-111 HarnessCheckpointSink（1 方法，3 参数）
type HarnessCheckpointSink interface {
	// Save 持久化 checkpoint 并返回存储修订号
	Save(
		ctx context.Context,
		checkpoint HarnessCheckpoint,
		reason CheckpointReason,
		expectedStorageRevision *string,
	) (CheckpointSaveReceipt, error)
}

// HarnessHookDispatcher 生命周期策略回调分发器。
//
// ⤵️ 15.3 回填：BeforeTool/AfterTool/BeforePrompt/Stop 等回调与决策语义留 15.3
// （Python: hooks.py）。此处只保留让 HarnessContext 字段有类型的最小方法集。
// Python: hooks.py HarnessHookDispatcher
type HarnessHookDispatcher interface {
	// Enabled 返回是否注册了任何钩子
	Enabled() bool
}

// HarnessInteractionHandler 需要应答的控制面（审批 / 用户输入 / MCP elicitation 等）。
//
// ⤵️ 15.3 回填：按 kind 的请求-应答与「应答只回给发起方」语义留 15.3
// （Python: interactions.py）。此处只保留让 HarnessContext 字段有类型的最小方法集。
// Python: interactions.py HarnessInteractionHandler
type HarnessInteractionHandler interface {
	// Pending 返回当前未应答的交互请求数量
	Pending() int
}
