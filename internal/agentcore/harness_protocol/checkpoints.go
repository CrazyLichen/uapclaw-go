package harness_protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
)

// 本文件对应 Python openjiuwen/harness_protocol/checkpoints.py（111 行）：
// 第三方 harness 的带版本 checkpoint 契约。
//
// checkpoint 是协议层的第四个通道（与 observation / interaction / hooks 并列）：
// 宿主把 provider 的不透明状态持久化，供 REQUIRE_RESUME 时续跑。
// provider 才可解释 data 与 schema_version，宿主只搬运完整信封。

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessCheckpoint 不透明的 provider 状态，作用域为单个 agent + 宿主会话。
//
// 只有 Provider 字段命名的 provider 可解释 Data 与 SchemaVersion；
// 宿主持久化完整信封、不做解释。
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
	// CheckpointID checkpoint 幂等标识
	CheckpointID string
	// Sequence 同一 provider/agent/会话作用域内单调递增序号
	Sequence int
	// Data provider 私有数据（已冻结）
	Data JsonObject
	// ProviderSessionID provider 原生会话标识（nil = 未提供；空串非法）
	ProviderSessionID *string
	// Revision 宿主存储修订号（nil = 未提供；空串非法）
	Revision *string
}

// CheckpointOptions HarnessCheckpoint 的构造入参。
// Python: checkpoints.py:36-44（dataclass 字段与默认值）
type CheckpointOptions struct {
	// Provider 拥有该 checkpoint 的 provider 名
	Provider string
	// SchemaVersion provider 私有 schema 版本
	SchemaVersion string
	// AgentID agent 标识
	AgentID string
	// HostSessionID 宿主会话标识
	HostSessionID string
	// CheckpointID checkpoint 幂等标识
	CheckpointID string
	// Sequence 单调序号
	Sequence int
	// Data provider 私有数据
	Data JsonObject
	// ProviderSessionID provider 原生会话标识（nil = 未提供）
	ProviderSessionID *string
	// Revision 宿主存储修订号（nil = 未提供）
	Revision *string
}

// CheckpointSaveReceipt 一次幂等 checkpoint 写入的持久化回执。
// Python: checkpoints.py:66-73 CheckpointSaveReceipt
type CheckpointSaveReceipt struct {
	// CheckpointID checkpoint 标识
	CheckpointID string
	// Sequence 单调序号
	Sequence int
	// StorageRevision 宿主存储修订号
	StorageRevision string
}

// HarnessCheckpointSink 宿主侧主动发布 checkpoint 的持久化目标。
//
// 三条必须遵守的语义（Python: checkpoints.py:83-103 的 docstring）：
//  1. Sequence 在「provider / agent / host-session」三元组作用域内单调递增。
//  2. 重试同一 CheckpointID 幂等，返回原 receipt。
//  3. 对「已存在或更低 sequence」的不同写入、或 ExpectedStorageRevision 比对失败，
//     须返回 CheckpointConflictError，不得覆盖新状态。
//
// 生产实现位于 agent_teams/external/member_runtime.py:124-152
// （_MemberSessionCheckpointSink），归领域十五 15.13。
// Python: checkpoints.py:83-103 HarnessCheckpointSink（Protocol，1 方法）
type HarnessCheckpointSink interface {
	// Save 持久化 checkpoint 并返回存储修订号
	Save(
		ctx context.Context,
		checkpoint HarnessCheckpoint,
		reason CheckpointReason,
		expectedStorageRevision *string,
	) (CheckpointSaveReceipt, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// CheckpointReason 实现方发布 checkpoint 的原因。
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
// Python: checkpoints.py:15 MAX_CHECKPOINT_BYTES
const MaxCheckpointBytes = 4 * 1024 * 1024

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHarnessCheckpoint 创建 checkpoint 信封，校验顺序对齐上游 __post_init__。
// Python: checkpoints.py:46-65 HarnessCheckpoint.__post_init__（6 步）
func NewHarnessCheckpoint(opts CheckpointOptions) (HarnessCheckpoint, error) {
	// 步骤 1：5 个必填字段逐项非空
	// Python: checkpoints.py:47-56
	required := []struct {
		name  string
		value string
	}{
		{"provider", opts.Provider},
		{"schema_version", opts.SchemaVersion},
		{"agent_id", opts.AgentID},
		{"host_session_id", opts.HostSessionID},
		{"checkpoint_id", opts.CheckpointID},
	}
	for _, f := range required {
		if f.value == "" {
			return HarnessCheckpoint{}, &HarnessProtocolError{
				Msg: "checkpoint " + f.name + " must not be empty",
			}
		}
	}

	// 步骤 2：sequence 非负
	// Python: checkpoints.py:57-58
	if opts.Sequence < 0 {
		return HarnessCheckpoint{}, &HarnessProtocolError{
			Msg: "checkpoint sequence must be non-negative",
		}
	}

	// 步骤 3：optional ID 空串即非法；nil = 未提供，不算空串
	// Python: checkpoints.py:59-60
	if (opts.ProviderSessionID != nil && *opts.ProviderSessionID == "") ||
		(opts.Revision != nil && *opts.Revision == "") {
		return HarnessCheckpoint{}, &HarnessProtocolError{
			Msg: "optional checkpoint ids must not be empty strings",
		}
	}

	// 步骤 4：冻结 data（先冻结，后量尺寸）
	// Python: checkpoints.py:61
	frozenData, err := FreezeJsonObject(opts.Data)
	if err != nil {
		return HarnessCheckpoint{}, err
	}

	// 步骤 5：紧凑 UTF-8 字节数超过上限即报错
	// Python: checkpoints.py:62-64
	encodedSize, err := checkpointDataSize(frozenData)
	if err != nil {
		return HarnessCheckpoint{}, err
	}
	if encodedSize > MaxCheckpointBytes {
		return HarnessCheckpoint{}, &HarnessProtocolError{
			Msg: "checkpoint data exceeds " + strconv.Itoa(MaxCheckpointBytes) + " bytes",
		}
	}

	// 步骤 6：冻结结果回写
	// Python: checkpoints.py:65
	return HarnessCheckpoint{
		Provider:          opts.Provider,
		SchemaVersion:     opts.SchemaVersion,
		AgentID:           opts.AgentID,
		HostSessionID:     opts.HostSessionID,
		CheckpointID:      opts.CheckpointID,
		Sequence:          opts.Sequence,
		Data:              frozenData,
		ProviderSessionID: opts.ProviderSessionID,
		Revision:          opts.Revision,
	}, nil
}

// NewCheckpointSaveReceipt 创建持久化回执，校验顺序对齐上游 __post_init__。
// Python: checkpoints.py:76-80 CheckpointSaveReceipt.__post_init__（2 步）
func NewCheckpointSaveReceipt(checkpointID string, sequence int, storageRevision string) (CheckpointSaveReceipt, error) {
	// 步骤 1：两个 id 非空
	// Python: checkpoints.py:77-78
	if checkpointID == "" || storageRevision == "" {
		return CheckpointSaveReceipt{}, &HarnessProtocolError{
			Msg: "checkpoint save receipt ids must not be empty",
		}
	}
	// 步骤 2：sequence 非负
	// Python: checkpoints.py:79-80
	if sequence < 0 {
		return CheckpointSaveReceipt{}, &HarnessProtocolError{
			Msg: "checkpoint save receipt sequence must be non-negative",
		}
	}
	return CheckpointSaveReceipt{
		CheckpointID:    checkpointID,
		Sequence:        sequence,
		StorageRevision: storageRevision,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkpointDataSize 返回冻结后 data 的紧凑 JSON UTF-8 字节数。
//
// 编码口径（spec Q12 决策 B）：SetEscapeHTML(false) 对齐 Python 的
// ensure_ascii=False——仓内 github.py:408、gitcode.py:522 均用该口径，
// 属上游主流。checkpoints.py:62 未传 ensure_ascii=False 是其自身疏漏，
// 中文场景两侧差 33 vs 39 字节已登记为 spec D5。
//
// json.Encoder.Encode 会追加 "\n"，必须裁掉，否则每条 checkpoint 多算 1 字节。
// Python: checkpoints.py:62
func checkpointDataSize(data JsonObject) (int, error) {
	// 步骤 1：归一化为标准 JSON 容器
	// Python: checkpoints.py:62 json_value_to_builtin(data)
	builtin := JsonValueToBuiltin(data)

	// 步骤 2：紧凑编码，不转义 HTML
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(builtin); err != nil {
		return 0, err
	}

	// 步骤 3：裁掉 Encoder 追加的换行
	raw := buf.Bytes()
	if n := len(raw); n > 0 && raw[n-1] == '\n' {
		raw = raw[:n-1]
	}
	return len(raw), nil
}
