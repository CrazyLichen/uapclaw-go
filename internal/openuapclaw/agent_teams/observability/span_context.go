package observability

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// executionSubjectRequestSequences 每 (session, subject) 的请求序号。
// Python: span_context.py:522-523
var (
	executionSubjectRequestSequences  = make(map[subjectKey]int)
	executionSubjectRequestSequenceMu sync.Mutex
	trajectorySequenceEpoch           = hexUUID()                // Python: span_context.py:524
	trajectorySubjectSequences        = make(map[subjectKey]int) // Python: span_context.py:525
	trajectorySubjectStates           = make(map[subjectKey]*trajectorySubjectState)
	trajectorySubjectStateMu          sync.Mutex // Python: span_context.py:530
)

// subjectKey (session_id, subject_id) 复合键
type subjectKey struct {
	sessionID string
	subjectID string
}

// trajectorySubjectState 主体已提交上下文窗口的规范状态。
// Python: span_context.py:526-529（tuple[window_id, tuple[(message_id, fingerprint), ...]]）
type trajectorySubjectState struct {
	windowID string
	messages []windowMessage
}

// windowMessage 一条窗口消息的 (message_id, 指纹)
type windowMessage struct {
	messageID   string
	fingerprint string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSessionID 归一化会话标识（nil/空 → 空串）。
// Python: span_context.py:538-539 _normalize_session_id
func NormalizeSessionID(sessionID string) string {
	return sessionID
}

// NextExecutionSubjectRequestNumber 为一个主体在一次会话内分配下一个请求序号。
// Python: span_context.py:555-564 next_execution_subject_request_number
func NextExecutionSubjectRequestNumber(sessionID, subjectID string) int {
	key := subjectKey{NormalizeSessionID(sessionID), subjectID}
	executionSubjectRequestSequenceMu.Lock()
	defer executionSubjectRequestSequenceMu.Unlock()
	n := executionSubjectRequestSequences[key] + 1
	executionSubjectRequestSequences[key] = n
	return n
}

// CurrentContextWindowMessages 返回一个主体最后一次提交的上下文窗口消息。
// 规范状态按消息指纹（消息的 JSON 形态）保存，读回时直接重建、无需第二份拷贝。
// 返回 nil 表示该主体尚未提交过窗口。
// Python: span_context.py:606-629 current_context_window_messages
func CurrentContextWindowMessages(sessionID, subjectID string) []map[string]any {
	key := subjectKey{NormalizeSessionID(sessionID), subjectID}
	trajectorySubjectStateMu.Lock()
	defer trajectorySubjectStateMu.Unlock()
	state := trajectorySubjectStates[key]
	if state == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(state.messages))
	for _, wm := range state.messages {
		var message map[string]any
		if err := json.Unmarshal([]byte(wm.fingerprint), &message); err != nil {
			continue
		}
		result = append(result, message)
	}
	return result
}

// AdvanceContextWindow 原子推进一个主体的规范上下文窗口状态。
// 出现身份由每条规范消息携带的 message_id 决定；内容相等刻意不用于连接出现。
// 返回 (纪元, 序号, 基窗口ID, 增量, 是否纪元基线)。
// Python: span_context.py:631-699 advance_context_window
func AdvanceContextWindow(sessionID, subjectID, windowID string, messages []map[string]any) (string, int, string, []map[string]any, bool) {
	key := subjectKey{NormalizeSessionID(sessionID), subjectID}
	// 当前窗口：按序保存 (message_id, 指纹)，对齐 Python 的 current 元组序列
	current := make([]windowMessage, 0, len(messages))
	currentByID := make(map[string]int, len(messages)) // message_id -> current 下标
	for i, message := range messages {
		id := ""
		if v, ok := message["message_id"]; ok {
			id, _ = v.(string)
		}
		current = append(current, windowMessage{messageID: id, fingerprint: stableJSONFingerprint(message)})
		currentByID[id] = i
	}
	trajectorySubjectStateMu.Lock()
	defer trajectorySubjectStateMu.Unlock()
	previous := trajectorySubjectStates[key]
	isBaseline := previous == nil
	epoch := trajectorySequenceEpoch
	sequence := nextTrajectorySubjectSequenceLocked(key)
	baseWindowID := ""
	if previous != nil {
		baseWindowID = previous.windowID
	}
	var delta []map[string]any
	if !isBaseline {
		// 移除：before 有、current 无
		for i, wm := range previous.messages {
			if _, ok := currentByID[wm.messageID]; !ok {
				delta = append(delta, map[string]any{
					"op": "remove", "message_id": wm.messageID, "index": i,
				})
			}
		}
		// 插入：current 有、before 无（message 深拷贝）
		beforeByID := make(map[string]int, len(previous.messages))
		for i, wm := range previous.messages {
			beforeByID[wm.messageID] = i
		}
		for i, wm := range current {
			if _, ok := beforeByID[wm.messageID]; !ok {
				var message map[string]any
				if err := json.Unmarshal([]byte(wm.fingerprint), &message); err != nil {
					message = map[string]any{}
				}
				delta = append(delta, map[string]any{
					"op": "insert", "message_id": wm.messageID, "index": i, "message": message,
				})
			}
		}
	}
	trajectorySubjectStates[key] = &trajectorySubjectState{windowID: windowID, messages: current}
	return epoch, sequence, baseWindowID, delta, isBaseline
}

// NextTrajectorySubjectPosition 分配所有 v2 事件类别共享的纪元域内序号。
// Python: span_context.py:702-707 next_trajectory_subject_position
func NextTrajectorySubjectPosition(sessionID, subjectID string) (string, int) {
	key := subjectKey{NormalizeSessionID(sessionID), subjectID}
	trajectorySubjectStateMu.Lock()
	defer trajectorySubjectStateMu.Unlock()
	return trajectorySequenceEpoch, nextTrajectorySubjectSequenceLocked(key)
}

// ResetTrajectoryContextState 清空请求序号与窗口状态并重置纪元。
// Python: span_context.py:1110-1124 段（Go 侧测试与生命周期需要，Python 侧无独立导出面）
func ResetTrajectoryContextState() {
	executionSubjectRequestSequenceMu.Lock()
	executionSubjectRequestSequences = make(map[subjectKey]int)
	executionSubjectRequestSequenceMu.Unlock()
	trajectorySubjectStateMu.Lock()
	defer trajectorySubjectStateMu.Unlock()
	trajectorySequenceEpoch = hexUUID()
	trajectorySubjectSequences = make(map[subjectKey]int)
	trajectorySubjectStates = make(map[subjectKey]*trajectorySubjectState)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nextTrajectorySubjectSequenceLocked 取下一个主体序号（调用方须持锁）。
// Python: span_context.py:710-713 _next_trajectory_subject_sequence_locked
func nextTrajectorySubjectSequenceLocked(key subjectKey) int {
	sequence := trajectorySubjectSequences[key] + 1
	trajectorySubjectSequences[key] = sequence
	return sequence
}

// stableJSONFingerprint 消息的稳定 JSON 指纹（键排序后序列化）。
// Python 侧无对应实现：对应 advance_context_window 内联的
// json.dumps(message, ensure_ascii=False, sort_keys=True, default=str)；
// Go 的 encoding/json 对 map[string]any 本身按键排序输出，故直接 Marshal
// 已满足 sort_keys 语义；ensure_ascii=False 与 default=str 的对应物由
// 调用方传入的 map 保证（值均为 JSON 可表达类型）。
func stableJSONFingerprint(message map[string]any) string {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Sprintf("%v", message)
	}
	return string(data)
}

// hexUUID uuid4 的 32 位十六进制无连字符形态。
// 对齐 Python uuid.uuid4().hex；Python 侧无对应实现（语言层差异）
func hexUUID() string {
	u := uuid.Must(uuid.NewRandom())
	b, _ := u.MarshalText()
	return string(b[:])
}
