package processcli

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 本文件是 channels/process_cli/live_input.py 的 Go 对应物。
//
// Python: channels/process_cli/live_input.py（404 行）
//
// 职责：一轮执行进行中的补充输入（steer）。父 REPL 读到的普通文本转发给
// worker stdin；worker 通过 stderr 上的回执行回报投递结果。
//
// 偏差 E-9：go-prompt 无 alternate screen，本实现不做终端重绘，
// 只维护回执状态与投递判定。

// ──────────────────────────── 结构体 ────────────────────────────

// LiveInputReceipt 一次本地投递结果；永不改变根命令结果。
// Python: live_input.py:52-57 LiveInputReceipt
type LiveInputReceipt struct {
	// Status æéç¶æï¼accepted / rejected / unknown
	Status string
	// Message 人类可读说明
	Message string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// deliveryTimeoutSeconds 回执等待上限
	// Python: live_input.py:32 _DELIVERY_TIMEOUT_SECONDS
	deliveryTimeoutSeconds = 5.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// steerContextKeys steer 请求携带的上下文键（对齐上游 7 键）。
// Python: live_input.py:35-43 _STEER_CONTEXT_KEYS
var steerContextKeys = []string{
	"mode",
	"work_mode",
	"cwd",
	"project_dir",
	"trusted_dirs",
	"supports_user_interaction",
	"supports_permission_approval",
}

// targetChangedMarkers 目标已变化的错误标记（逐字复刻上游 5 条）。
// Python: live_input.py:44-50 _TARGET_CHANGED_MARKERS
var targetChangedMarkers = []string{
	"supplemental input was not sent",
	"targeted execution has ended",
	"waiting for an interaction answer",
	"session is finishing",
	"session output is finishing",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EncodeForwardedReceipt 编码一条转发回执（前缀 + 紧凑 JSON）。
// Python: live_input.py:60-67 encode_forwarded_receipt
func EncodeForwardedReceipt(receipt LiveInputReceipt) string {
	payload, err := json.Marshal(map[string]string{
		"status":  receipt.Status,
		"message": receipt.Message,
	})
	if err != nil {
		return ForwardedReceiptPrefix + `{"status":"unknown","message":""}`
	}
	return ForwardedReceiptPrefix + string(payload)
}

// ParseForwardedReceipt 解析一条转发回执；非法返回 nil。
// Python: repl.py:174-183（收端反序列化）
func ParseForwardedReceipt(line string) *LiveInputReceipt {
	trimmed := strings.TrimRight(strings.TrimLeft(line, " "), "\r\n")
	if !strings.HasPrefix(trimmed, ForwardedReceiptPrefix) {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(trimmed, ForwardedReceiptPrefix)), &decoded); err != nil {
		return nil
	}
	status := stringOr(decoded["status"])
	if status == "" {
		status = "unknown"
	}
	return &LiveInputReceipt{Status: status, Message: stringOr(decoded["message"])}
}

// SupportsLiveSessionInput 判断这条人类命令能否安全接管实时终端输入。
// Python: live_input.py:103-120 supports_live_session_input
//
// 四条件（顺序即语义）：
//  1. output 必须是 human
//  2. operation 必须是 chat
//  3. 非 forwarded 时要求 stdin 与 stdout 都是 TTY
//  4. 模式必须是单 Agent 且非 plan
func SupportsLiveSessionInput(args *AppArgs, stdin io.Reader, stdout io.Writer) bool {
	if args == nil {
		return false
	}
	// Python: :111 output != "human" → False
	if args.Output != OutputHuman {
		return false
	}
	// Python: :113 operation != "chat" → False
	operation := args.Operation
	if operation == "" {
		operation = operationChat
	}
	if operation != operationChat {
		return false
	}
	// Python: :114-116 非 forwarded 要求双 TTY
	if !args.ForwardedLiveInput {
		if !isTerminalReader(stdin) || !isTerminalWriter(stdout) {
			return false
		}
	}
	// Python: :117-119 单 Agent 且非 plan
	mode := deprecateModeText(args.Mode)
	return isSingleAgentModeText(mode) && !isPlanModeText(mode)
}

// ClassifySteerError 按目标变化标记判定投递失败的类型。
// Python: live_input.py:138-148
//
// 命中标记 → rejected（目标已变，输入未投递）；其余 → unknown。
func ClassifySteerError(message string) string {
	normalized := strings.ToLower(message)
	for _, marker := range targetChangedMarkers {
		if strings.Contains(normalized, marker) {
			return supplementStatusRejected
		}
	}
	return supplementStatusUnknown
}

// SteerParams 从根请求参数中筛出 steer 需要的 7 个上下文键。
// Python: live_input.py:130-137
func SteerParams(rootParams map[string]any) map[string]any {
	params := make(map[string]any, len(steerContextKeys))
	for _, key := range steerContextKeys {
		if value, ok := rootParams[key]; ok {
			params[key] = value
		}
	}
	return params
}

// DeliveryTimeout 返回回执等待上限。
// Python: live_input.py:32 _DELIVERY_TIMEOUT_SECONDS
func DeliveryTimeout() time.Duration {
	return time.Duration(deliveryTimeoutSeconds * float64(time.Second))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deprecateModeText 组合 canonicalize + deprecate（对齐上游 live_input.py 的用法）。
func deprecateModeText(mode string) string {
	return deprecateMode(mode)
}

// deprecateMode 旧 canonical 映射到新 canonical（转发 schema，统一入口）。
func deprecateMode(mode string) string {
	return swschema.DeprecateMode(mode)
}

// isSingleAgentModeText 是否单 Agent 模式（转发 schema）。
func isSingleAgentModeText(mode string) bool {
	return swschema.IsSingleAgentMode(mode)
}

// isPlanModeText 是否 plan 模式（转发 schema）。
func isPlanModeText(mode string) bool {
	return swschema.IsPlanMode(mode)
}
