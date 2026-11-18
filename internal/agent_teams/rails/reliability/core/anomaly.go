// Package core 团队可靠性数据层：异常模型。
//
// 对应 Python：openjiuwen/agent_teams/reliability/anomaly.py
package core

// ──────────────────────────── 结构体 ────────────────────────────

// Anomaly detector 判定出的待处置异常。
//
// Python: anomaly.py Anomaly（pydantic model）。
// evidence 保持 map[string]any 自由形状（各 detector 自带键集，spec §3 D5 不收窄）。
type Anomaly struct {
	// Detector 产出该异常的 detector 稳定标识（如 "repeat_tool_call"）
	Detector string `json:"detector"`
	// Kind 异常类别
	Kind AnomalyKind `json:"kind"`
	// Severity 严重度
	Severity Severity `json:"severity"`
	// MemberName 出现异常的成员
	MemberName string `json:"member_name"`
	// Summary 面向人/LLM 的一行描述
	Summary string `json:"summary"`
	// Evidence 支撑判定的结构化证据快照（计数、哈希、降幅、会话对等）
	Evidence map[string]any `json:"evidence"`
	// PeerMember 团队级异常（pingpong）的对端成员；无对端为 nil
	PeerMember *string `json:"peer_member"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// Severity 异常严重度，从低到高。
//
// Python: anomaly.py Severity(str, Enum)。
type Severity string

// 严重度四档取值。
const (
	// SeverityLow 低（质量提示类）
	SeverityLow Severity = "low"
	// SeverityMedium 中（上报 leader 决策）
	SeverityMedium Severity = "medium"
	// SeverityHigh 高（本地纠偏 + 上报）
	SeverityHigh Severity = "high"
	// SeverityCritical 严重（本地纠偏 + 上报用户）
	SeverityCritical Severity = "critical"
)

// AnomalyKind detector 识别出的不健康行为类别。
//
// Python: anomaly.py AnomalyKind(str, Enum)，10 成员全量映射。
type AnomalyKind string

// 异常类别取值。
const (
	// AnomalyKindToolErrorRate 工具调用错误率超标
	AnomalyKindToolErrorRate AnomalyKind = "tool_error_rate"
	// AnomalyKindRepeatToolCall 重复工具调用（同参同结果）
	AnomalyKindRepeatToolCall AnomalyKind = "repeat_tool_call"
	// AnomalyKindToolCallLoop 无进展工具调用循环（含乒乓交替）
	AnomalyKindToolCallLoop AnomalyKind = "tool_call_loop"
	// AnomalyKindModelError 模型调用错误
	AnomalyKindModelError AnomalyKind = "model_error"
	// AnomalyKindModelUnavailable 模型组无法服务该成员，重试无益
	AnomalyKindModelUnavailable AnomalyKind = "model_unavailable"
	// AnomalyKindModelRouteNotice 模型组已从故障 deployment 切换（仅通告）
	AnomalyKindModelRouteNotice AnomalyKind = "model_route_notice"
	// AnomalyKindOutputTooLong 输出文本超长
	AnomalyKindOutputTooLong AnomalyKind = "output_too_long"
	// AnomalyKindThinkingTooLong 思考内容超长
	AnomalyKindThinkingTooLong AnomalyKind = "thinking_too_long"
	// AnomalyKindFrequentCompaction 频繁上下文压缩（推断）
	AnomalyKindFrequentCompaction AnomalyKind = "frequent_compaction"
	// AnomalyKindPingPong 两个成员互发消息无进展（团队级）
	AnomalyKindPingPong AnomalyKind = "ping_pong"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// severityOrder 严重度从低到高的秩序表。
//
// Python: anomaly.py _SEVERITY_ORDER 元组；rank 由下标给出（D-g4：property → 方法）。
var severityOrder = []Severity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}

// ──────────────────────────── 导出函数 ────────────────────────────

// Rank 返回严重度可比秩（LOW=0 .. CRITICAL=3）。
//
// Python: anomaly.py Severity.rank property（D-g4 语言层等价）。
func (s Severity) Rank() int {
	for i, v := range severityOrder {
		if v == s {
			return i
		}
	}
	return -1
}
