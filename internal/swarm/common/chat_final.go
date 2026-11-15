package common

// chat.final 落地协议字段。
//
// 前端优先读 final_mode，避免用字符串 includes 启发式折叠整轮：
//   - patch_segment：覆写当前/匹配分段（工具打断后的默认路径）
//   - replace_turn：用 final 替换本轮全部助手气泡（需显式声明）
//   - append：追加新气泡
//
// Python: jiuwenswarm/common/chat_final.py（一比一移植，23.8）

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// FinalModePatchSegment 覆写当前/匹配分段（工具打断后的默认路径）
	FinalModePatchSegment = "patch_segment"
	// FinalModeReplaceTurn 用 final 替换本轮全部助手气泡（需显式声明）
	FinalModeReplaceTurn = "replace_turn"
	// FinalModeAppend 追加新气泡
	FinalModeAppend = "append"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AnnotateChatFinal 确保 chat.final payload 带 final_mode；已有显式值时不覆盖。
// Python: annotate_chat_final:19-33
func AnnotateChatFinal(payload map[string]any, finalMode string) map[string]any {
	out := make(map[string]any, len(payload)+2)
	for k, v := range payload {
		out[k] = v
	}
	// Python: if out.get("event_type") and str(out.get("event_type")).strip() != "chat.final"
	if s := strings.TrimSpace(asString(out["event_type"])); s != "" && s != "chat.final" {
		return out
	}
	// Python: if isinstance(existing, str) and existing.strip(): return out
	if existing, ok := out["final_mode"].(string); ok && strings.TrimSpace(existing) != "" {
		return out
	}
	out["final_mode"] = finalMode
	if _, ok := out["event_type"]; !ok {
		out["event_type"] = "chat.final"
	}
	return out
}

// EnsureFinalModeInplace 就地写入 final_mode（仅当缺失时）。
// Python: ensure_final_mode_inplace:36-47
func EnsureFinalModeInplace(payload map[string]any, finalMode string) {
	// Python: if str(payload.get("event_type") or "").strip() not in ("", "chat.final"): return
	if s := strings.TrimSpace(asString(payload["event_type"])); s != "" && s != "chat.final" {
		return
	}
	if existing, ok := payload["final_mode"].(string); ok && strings.TrimSpace(existing) != "" {
		return
	}
	payload["final_mode"] = finalMode
}
