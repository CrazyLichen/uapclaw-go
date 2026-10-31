package a2a

import (
	"encoding/base64"
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

// MapA2APartsToParams 把 A2A 消息的 parts 映射为内部 query 与 files 参数。
//
// 文本段按 "\n" 合并为 query；含 url/data/raw 的 Part 转成 files 条目，
// 键与 web 通道保持兼容（name/filename、media_type/type、url/uri、encoding=base64）。
// Python: a2a_connect.py map_a2a_parts_to_params (L457-500)
func MapA2APartsToParams(m *sdka2a.Message) (string, []map[string]any) {
	if m == nil {
		return "", nil
	}

	textSegments := make([]string, 0, len(m.Parts))
	var files []map[string]any

	for idx, part := range m.Parts {
		if part == nil {
			continue
		}
		if t := strings.TrimSpace(part.Text()); t != "" {
			textSegments = append(textSegments, t)
		}

		filename := strings.TrimSpace(part.Filename)
		mediaType := strings.TrimSpace(part.MediaType)
		url := strings.TrimSpace(string(part.URL()))
		data := part.Data()
		raw := part.Raw()

		if url == "" && data == nil && len(raw) == 0 {
			continue
		}

		name := filename
		if name == "" {
			name = fmt.Sprintf("a2a_part_%d", idx)
		}
		entry := map[string]any{
			// web 通道兼容键
			"name":     name,
			"filename": name,
		}
		if mediaType != "" {
			entry["media_type"] = mediaType
			// 通用消费方检查 type
			entry["type"] = mediaType
		}
		if url != "" {
			entry["url"] = url
			entry["uri"] = url
		}
		if data != nil {
			entry["data"] = strVal(data)
			entry["encoding"] = "base64"
		}
		if len(raw) > 0 {
			// 偏差 D4：Python 直接 str(bytes) 会 TypeError，Go 走 base64
			entry["raw"] = base64.StdEncoding.EncodeToString(raw)
		}
		files = append(files, entry)
	}

	query := strings.TrimSpace(strings.Join(textSegments, "\n"))
	return query, files
}

// MessageToA2AParts 把内部消息 payload 映射为 A2A 响应 parts。
//
// CHAT_ERROR 直接转错误文本；content/result 转文本（过滤哨兵）；
// 工具事件文本化为 [tool_call]/[tool_result]；payload.files 转 url/data/raw 三类 Part。
// fallbackToText 为真且无任何 part 时回退 MessageToText。
// Python: a2a_connect.py message_to_a2a_parts (L359-423)
func MessageToA2AParts(msg *schema.Message, fallbackToText bool) []*sdka2a.Part {
	if msg == nil {
		return nil
	}
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	var parts []*sdka2a.Part

	// 错误响应保持可读
	if msg.Type == schema.MessageTypeEvent && msg.EventType == schema.EventTypeChatError {
		text := firstNonEmptyStr(payload["error"], payload["content"])
		if text == "" {
			text = agentRequestFailedText
		}
		return []*sdka2a.Part{sdka2a.NewTextPart(text)}
	}

	switch content := payload["content"].(type) {
	case string:
		if t := strings.TrimSpace(content); t != "" && !IsCompletionSentinelText(t) {
			parts = append(parts, sdka2a.NewTextPart(t))
		}
	case nil, map[string]any, []any:
		// 缺失 / 结构化内容不转文本（对齐 Python 跳过 dict/list）
	default:
		parts = append(parts, sdka2a.NewTextPart(strVal(content)))
	}

	if result, ok := payload["result"].(string); ok {
		if t := strings.TrimSpace(result); t != "" && !IsCompletionSentinelText(t) {
			parts = append(parts, sdka2a.NewTextPart(t))
		}
	}

	// 流式模式暴露工具进度
	if msg.EventType == schema.EventTypeChatToolCall {
		if toolCall, ok := payload["tool_call"].(map[string]any); ok && toolCall != nil {
			name := strings.TrimSpace(strVal(toolCall["name"]))
			if name == "" {
				name = "tool"
			}
			parts = append(parts, sdka2a.NewTextPart("[tool_call] "+name))
		}
	}
	if msg.EventType == schema.EventTypeChatToolResult {
		toolName := strings.TrimSpace(strVal(payload["tool_name"]))
		if toolResult, ok := payload["result"].(string); ok && strings.TrimSpace(toolResult) != "" {
			label := "[tool_result] "
			if toolName != "" {
				label = "[tool_result:" + toolName + "] "
			}
			parts = append(parts, sdka2a.NewTextPart(label+strings.TrimSpace(toolResult)))
		}
	}

	parts = append(parts, filesToParts(payload["files"])...)

	if len(parts) > 0 {
		return parts
	}
	if fallbackToText {
		return []*sdka2a.Part{sdka2a.NewTextPart(MessageToText(msg))}
	}
	return nil
}

// BuildRequestParams 构造内部 chat.send 的 params。
//
// Python: a2a_connect.py _build_request_params (L446-455)
func BuildRequestParams(query string, files []map[string]any) map[string]any {
	params := map[string]any{"query": query}
	if len(files) > 0 {
		params["files"] = files
	}
	return params
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

// filesToParts 把 payload.files 转成 A2A 文件 Part（url/data/raw 三类）。
//
// Python: a2a_connect.py L396-417
func filesToParts(raw any) []*sdka2a.Part {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	var parts []*sdka2a.Part
	for idx, item := range list {
		fi, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstNonEmptyStr(fi["filename"], fi["name"]))
		if name == "" {
			name = fmt.Sprintf("file_%d", idx)
		}
		mediaType := strings.TrimSpace(firstNonEmptyStr(fi["media_type"], fi["type"]))
		url := strings.TrimSpace(firstNonEmptyStr(fi["url"], fi["uri"]))
		data := strings.TrimSpace(strVal(fi["data"]))
		rawText := strings.TrimSpace(strVal(fi["raw"]))

		if url != "" {
			parts = append(parts, &sdka2a.Part{Content: sdka2a.URL(url), Filename: name, MediaType: mediaType})
		}
		if data != "" {
			parts = append(parts, &sdka2a.Part{Content: sdka2a.Data{Value: data}, Filename: name, MediaType: mediaType})
		}
		if rawText != "" {
			rawBytes, err := base64.StdEncoding.DecodeString(rawText)
			if err != nil {
				rawBytes = []byte(rawText)
			}
			parts = append(parts, &sdka2a.Part{Content: sdka2a.Raw(rawBytes), Filename: name, MediaType: mediaType})
		}
	}
	return parts
}
