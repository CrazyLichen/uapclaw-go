package server

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyPageSize 历史记录每页大小，对齐 Python _HISTORY_PAGE_SIZE
	historyPageSize = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// restorableAssistantEventTypes 可恢复的助手事件类型集合，对齐 Python _HISTORY_RESTORABLE_ASSISTANT_EVENT_TYPES
	restorableAssistantEventTypes = map[string]bool{
		"chat.final":               true,
		"chat.tool_call":           true,
		"chat.tool_result":         true,
		"chat.usage_summary":       true,
		"chat.file":                true,
		"team.message":             true,
		"context.compact_boundary": true,
		"context.compact_summary":  true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// isRestorableHistoryRecord 判断历史记录是否可恢复，对齐 Python _is_restorable_history_record。
//
// 规则：
//   - record 非 dict → false
//   - role=="user" → content 非空字符串
//   - event_type 存在 → event_type 在 restorableAssistantEventTypes 中
//   - event_type 不存在 → content 非空字符串
func isRestorableHistoryRecord(record map[string]any) bool {
	role, _ := record["role"].(string)
	content, _ := record["content"].(string)
	hasContent := strings.TrimSpace(content) != ""

	// role == "user" 时，需要 content 非空
	if role == "user" {
		return hasContent
	}

	// 检查 event_type
	eventType, hasEventType := record["event_type"].(string)
	if !hasEventType || eventType == "" {
		// 无 event_type 时，需要 content 非空
		return hasContent
	}

	// 有 event_type 时，判断是否在可恢复集合中
	return restorableAssistantEventTypes[eventType]
}

// getConversationHistory 获取会话历史记录，对齐 Python get_conversation_history。
//
// 流程：校验参数 → 读取 history.json → 过滤可恢复记录 → 倒序 → 按 page_idx(1-indexed) 和 historyPageSize 分页。
// 返回 nil 表示参数无效或文件不存在。
func getConversationHistory(sessionID string, pageIdx int) map[string]any {
	// 1. 校验 session_id
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	// 2. 校验 page_idx
	if pageIdx <= 0 {
		return nil
	}

	// 3. 读取 history.json
	sessionsDir := pathutil.AgentSessionsDir()
	historyPath := sessionsDir + "/" + strings.TrimSpace(sessionID) + "/history.json"

	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil
	}

	// 4. 解析 JSON
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	// 5. 过滤可恢复记录
	var restorable []any
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok && isRestorableHistoryRecord(m) {
			restorable = append(restorable, item)
		}
	}

	// 6. 计算分页
	total := len(restorable)
	totalPages := int(math.Ceil(float64(total) / float64(historyPageSize)))
	if totalPages < 1 {
		totalPages = 1
	}
	if pageIdx > totalPages {
		return nil
	}

	// 7. 倒序
	ordered := make([]any, total)
	for i, item := range restorable {
		ordered[total-1-i] = item
	}

	// 8. 分页
	start := (pageIdx - 1) * historyPageSize
	end := start + historyPageSize
	if end > total {
		end = total
	}
	pageMessages := ordered[start:end]

	return map[string]any{
		"messages":    pageMessages,
		"total_pages": totalPages,
		"page_idx":    pageIdx,
	}
}

// handleHistoryGet 处理 history.get 非流式请求，对齐 Python _handle_history_get。
//
// 从 request.Params 读取 session_id 和 page_idx，调用 getConversationHistory 分页返回。
func (s *AgentServer) handleHistoryGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 1. 解析 params
	var params struct {
		SessionID string `json:"session_id"`
		PageIdx   int    `json:"page_idx"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("history.get 参数解析失败")
		}
	}

	// 2. 默认 page_idx=1
	if params.PageIdx <= 0 {
		params.PageIdx = 1
	}

	// 3. 调用 getConversationHistory
	data := getConversationHistory(params.SessionID, params.PageIdx)
	if data == nil {
		// 参数无效或文件不存在，返回空历史（对齐 Python：session_id 无效/文件不存在返回 None，Go 统一返回空）
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"messages":    []any{},
				"total_pages": 0,
				"page_idx":    params.PageIdx,
			}),
		), nil
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(data),
	), nil
}

// handleHistoryGetStream 处理 history.get 流式请求，对齐 Python _handle_history_get_stream。
//
// 调用 getConversationHistory 获取数据，逐条发送 AgentResponseChunk（event_type=history.message），
// 最后发送 done chunk。
func (s *AgentServer) handleHistoryGetStream(_ context.Context, request *schema.AgentRequest) ([]*schema.AgentResponseChunk, error) {
	// 1. 解析 params
	var params struct {
		SessionID string `json:"session_id"`
		PageIdx   int    `json:"page_idx"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("history.get stream 参数解析失败")
		}
	}

	// 2. 默认 page_idx=1
	if params.PageIdx <= 0 {
		params.PageIdx = 1
	}

	// 3. 调用 getConversationHistory
	data := getConversationHistory(params.SessionID, params.PageIdx)
	if data == nil {
		// 参数无效或文件不存在，发送错误 chunk
		chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
			map[string]any{
				"event_type": "chat.error",
				"error":      "获取历史记录失败：参数无效或会话不存在",
			},
			schema.WithChunkIsComplete(true),
		)
		return []*schema.AgentResponseChunk{chunk}, nil
	}

	// 4. 提取数据
	messages, _ := data["messages"].([]any)
	totalPages, _ := data["total_pages"].(int)
	pageIdx, _ := data["page_idx"].(int)

	// 5. 逐条发送 history.message chunk
	chunks := make([]*schema.AgentResponseChunk, 0, len(messages)+1)
	for _, item := range messages {
		chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
			map[string]any{
				"event_type":  "history.message",
				"message":     item,
				"total_pages": totalPages,
				"page_idx":    pageIdx,
			},
			schema.WithChunkIsComplete(false),
		)
		chunks = append(chunks, chunk)
	}

	// 6. 完成消息块
	doneChunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
		map[string]any{
			"event_type":  "history.message",
			"status":      "done",
			"total_pages": totalPages,
			"page_idx":    pageIdx,
		},
		schema.WithChunkIsComplete(true),
	)
	chunks = append(chunks, doneChunk)

	return chunks, nil
}
