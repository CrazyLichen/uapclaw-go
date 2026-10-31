// Package a2a 提供 A2A（Agent2Agent）协议入站通道。
//
// 详见 doc.go。
package a2a

import (
	"encoding/json"
	"fmt"
	"strings"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentRequestFailedText 错误兜底文案
	// Python: a2a_connect.py message_to_text L352
	agentRequestFailedText = "agent request failed"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsCompletionSentinelText 判断文本是否为"完成哨兵"（应被过滤，不发给 A2A 客户端）。
//
// 去掉全部空白后小写比较，等价 Python "".join(text.split()).lower()。
// Python: a2a_connect.py is_completion_sentinel_text (L425-428)
func IsCompletionSentinelText(text string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	return compact == "{'is_complete':true}" || compact == `{"is_complete":true}`
}

// IsTerminalMessage 判断内部消息是否为一次 A2A 任务的终态消息。
//
// Python: a2a_connect.py is_terminal_message (L430-444)
func IsTerminalMessage(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	if msg.Type == schema.MessageTypeRes {
		return true
	}
	if msg.Type != schema.MessageTypeEvent {
		return false
	}
	if msg.EventType == schema.EventTypeChatError || msg.EventType == schema.EventTypeChatInterruptResult {
		return true
	}
	// Python: payload.get("is_complete") is True —— 严格布尔
	if v, ok := msg.Payload["is_complete"].(bool); ok && v {
		return true
	}
	return false
}

// MessageToText 把内部消息压成纯文本（A2A fallback 用）。
//
// Python: a2a_connect.py message_to_text (L348-357)
func MessageToText(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	payload := msg.Payload
	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatError {
		if v := firstNonEmptyStr(payload["error"], payload["content"]); v != "" {
			return v
		}
		return agentRequestFailedText
	}
	if content, ok := payload["content"]; ok {
		return strVal(content)
	}
	if len(payload) > 0 {
		// 偏差 D5：Python str(dict) 为单引号 repr，Go 用 JSON（确定性、可读）
		if b, err := json.Marshal(payload); err == nil {
			return string(b)
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 把任意值转字符串；nil 返回空串。
func strVal(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// firstNonEmptyStr 返回第一个非空字符串化值。
func firstNonEmptyStr(vals ...any) string {
	for _, v := range vals {
		if s := strVal(v); s != "" {
			return s
		}
	}
	return ""
}

// joinTextParts 把 A2A 消息的文本段按换行拼接。
//
// 对应 Python context.get_user_input() 兜底：当 parts 里没有可识别的 query 时再取一次文本。
// Python: a2a_connect.py execute (L65-67)
func joinTextParts(m *sdka2a.Message) string {
	if m == nil {
		return ""
	}
	segs := make([]string, 0, len(m.Parts))
	for _, p := range m.Parts {
		if p == nil {
			continue
		}
		if t := strings.TrimSpace(p.Text()); t != "" {
			segs = append(segs, t)
		}
	}
	return strings.TrimSpace(strings.Join(segs, "\n"))
}
