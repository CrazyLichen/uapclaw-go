package interrupt

// ──────────────────────────── 结构体 ────────────────────────────

// PayloadEntry 中断 payload 条目 (innerID, payload)。
// 替代原有 [2]any{innerID, payloadObj} tuple 模拟，提供类型安全。
//
// 对应 Python: (inner_id, payload) 元组
// （Python 中 _collect_interrupts 返回 payloads: list[tuple[str, ToolCallInterruptRequest|OutputSchema]]）
type PayloadEntry struct {
	// InnerID 内部工具调用 ID
	InnerID string
	// Payload 负载对象，可能的类型：
	//   - *ToolCallInterruptRequest
	//   - *stream.OutputSchema（子 Agent 中断）
	Payload any
}
