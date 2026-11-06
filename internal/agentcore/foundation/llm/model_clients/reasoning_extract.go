package model_clients

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractReasoningContent 从消息或 delta 的 map 形态抽取推理内容（四级回退）。
//
// 顺序：reasoning_details[0].text → reasoning_content → reasoning（OpenRouter）
// → reasoning_token_text。空串视为未命中继续回退。
//
// Python: openai_model_client.py:2751-2763 _extract_reasoning_content
func ExtractReasoningContent(fields map[string]any) string {
	if details, ok := fields["reasoning_details"].([]any); ok && len(details) > 0 {
		if first, ok := details[0].(map[string]any); ok {
			if text, ok := first["text"].(string); ok && text != "" {
				return text
			}
		}
	}
	for _, attr := range [...]string{"reasoning_content", "reasoning", "reasoning_token_text"} {
		if v, ok := fields[attr].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────
