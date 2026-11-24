package native

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/vcs"
	hp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SavedInput 已接受输入的快照，message/turn 身份跨重启存活。
// Python: checkpoint.py:17-37 SavedInput
type SavedInput struct {
	// MessageID 消息标识（min_length=1）
	MessageID string `json:"message_id"`
	// TurnID 轮次标识（min_length=1）
	TurnID string `json:"turn_id"`
	// Content 输入内容（JSON-native）
	Content any `json:"content"`
	// Metadata 输入元数据（JSON-native）
	Metadata map[string]any `json:"metadata"`
	// Mode 接受时的投递模式
	Mode hp.DeliveryMode `json:"mode"`
}

// NativeCheckpoint 可移植的 paused/idle 快照，不含可执行对象与配置。
// Python: checkpoint.py:40-55 NativeCheckpoint
type NativeCheckpoint struct {
	// SessionID 会话标识（f"{host_session_id}:{agent_id}"）
	SessionID string `json:"session_id"`
	// CardID agent 卡片标识
	CardID string `json:"card_id"`
	// Cwd 工作目录绝对路径
	Cwd string `json:"cwd,omitempty"`
	// Contexts 逐 context 编码后的上下文状态
	Contexts map[string]map[string]any `json:"contexts"`
	// DeepAgent DeepAgentState.to_session_dict() 结果
	DeepAgent map[string]any `json:"deepagent"`
	// PausedInput 暂停轮次的输入（idle 快照为 nil）
	PausedInput *SavedInput `json:"paused_input,omitempty"`
	// PausedQuery 暂停时的原始 query
	PausedQuery *string `json:"paused_query,omitempty"`
	// Queued 排队中的输入（仅 paused 快照非空）
	Queued []SavedInput `json:"queued"`
	// OutputState 输出态 6 字段（仅 paused 快照非空）
	OutputState map[string]any `json:"output_state"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CaptureSavedInput 从待决轮次捕获快照。
// Python: checkpoint.py:27-33 SavedInput.capture
func CaptureSavedInput(turn *harness_providers.PendingTurn) SavedInput {
	return SavedInput{
		MessageID: turn.MessageID,
		TurnID:    turn.TurnID,
		Content:   hp.JsonValueToBuiltin(turn.Content.Content),
		Metadata:  jsonValueToBuiltinObject(turn.Content.Metadata),
		Mode:      turn.AcceptedMode,
	}
}

// Restore 从快照还原待决轮次。
// Python: checkpoint.py:35-37 SavedInput.restore
func (s SavedInput) Restore() *harness_providers.PendingTurn {
	return &harness_providers.PendingTurn{
		Content: hp.HarnessInput{
			Content:  hp.JsonValueToBuiltin(s.Content),
			Metadata: hp.JsonObject(s.Metadata),
		},
		MessageID:    s.MessageID,
		TurnID:       s.TurnID,
		AcceptedMode: s.Mode,
	}
}

// DecodeContexts 逐 context 解码上下文状态。
// Python: checkpoint.py:54-55 NativeCheckpoint.decode_contexts
func (c NativeCheckpoint) DecodeContexts() map[string]map[string]any {
	out := make(map[string]map[string]any, len(c.Contexts))
	for key, value := range c.Contexts {
		decoded := vcs.DecodeContextState(value)
		flat := make(map[string]any, len(decoded))
		for k, v := range decoded {
			flat[k] = v
		}
		out[key] = flat
	}
	return out
}

// EncodeContexts 逐 context 编码上下文状态。
// Python: checkpoint.py:58-59 encode_contexts
func EncodeContexts(contexts map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(contexts))
	for key, value := range contexts {
		out[key] = vcs.EncodeContextState(value)
	}
	return out
}

// ValidateNativeCheckpointJSON 校验快照 JSON，未知键一律拒绝。
// Python: checkpoint.py:43 ConfigDict(extra="forbid") 与
//
//	protocol_adapter.py:155 NativeCheckpoint.model_validate
func ValidateNativeCheckpointJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cp NativeCheckpoint
	if err := dec.Decode(&cp); err != nil {
		return fmt.Errorf("invalid native checkpoint: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jsonValueToBuiltinObject 把元数据字典逐值转成 JSON-native 形态。
// Python: checkpoint.py:33 json_value_to_builtin(turn.content.metadata)
func jsonValueToBuiltinObject(in hp.JsonObject) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = hp.JsonValueToBuiltin(v)
	}
	return out
}
