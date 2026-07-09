package server

import (
	"context"
	"encoding/json"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyDefaultPageSize 历史记录默认每页大小
	historyDefaultPageSize = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleHistoryGet 处理 history.get 请求。对齐 Python _handle_history_get。
//
// 从 request.Params 读取 session_id，读取 history.json，分页返回。
// 当前仅实现非流式模式。
func (s *AgentServer) handleHistoryGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 1. 解析 params
	var params struct {
		SessionID string `json:"session_id"`
		Page      int    `json:"page"`
		PageSize  int    `json:"page_size"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("history.get 参数解析失败")
		}
	}

	// 2. 默认分页参数
	if params.PageSize <= 0 {
		params.PageSize = historyDefaultPageSize
	}
	if params.Page <= 0 {
		params.Page = 1
	}

	// 3. 如果没有 session_id，返回空历史
	if params.SessionID == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"history": []any{},
				"page":    params.Page,
				"total":   0,
			}),
		), nil
	}

	// 4. 读取 history.json
	sessionsDir := pathutil.AgentSessionsDir()
	historyPath := sessionsDir + "/" + params.SessionID + "/history.json"

	data, err := os.ReadFile(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 历史文件不存在，返回空
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithPayload(map[string]any{
					"history": []any{},
					"page":    params.Page,
					"total":   0,
				}),
			), nil
		}
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("session_id", params.SessionID).
			Str("history_path", historyPath).
			Msg("读取 history.json 失败")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": map[string]any{
					"code":    "HISTORY_READ_ERROR",
					"message": "读取历史记录失败",
				},
			}),
		), nil
	}

	// 5. 解析 JSON
	var history []any
	if err := json.Unmarshal(data, &history); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("session_id", params.SessionID).
			Msg("解析 history.json 失败")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": map[string]any{
					"code":    "HISTORY_PARSE_ERROR",
					"message": "解析历史记录失败",
				},
			}),
		), nil
	}

	// 6. 分页
	total := len(history)
	start := (params.Page - 1) * params.PageSize
	if start >= total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	pageData := history[start:end]

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"history": pageData,
			"page":    params.Page,
			"total":   total,
		}),
	), nil
}
