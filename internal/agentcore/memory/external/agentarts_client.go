package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentartsClient AgentArts Data Plane HTTP 客户端。
// 对齐 Python: agentarts.sdk.memory.MemoryClient（自封装 REST 替代 SDK 依赖）
type agentartsClient struct {
	// baseURL Data Plane 基础 URL
	baseURL string
	// apiKey Bearer token
	apiKey string
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// memorySearchFilter 记忆搜索过滤条件。
// 对齐 Python: MemorySearchFilter (agentarts/sdk/memory/inner/config.py)
type memorySearchFilter struct {
	Query        string  `json:"query"`
	TopK         int     `json:"top_k,omitempty"`
	MinScore     float64 `json:"min_score,omitempty"`
	StrategyType string  `json:"strategy_type,omitempty"`
	ActorID      string  `json:"actor_id,omitempty"`
	AssistantID  string  `json:"assistant_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
}

// memorySearchResponse 记忆搜索响应。
type memorySearchResponse struct {
	Records []memorySearchRecord `json:"records"`
	Total   int                  `json:"total"`
	Query   string               `json:"query"`
}

// memorySearchRecord 单条搜索结果。
type memorySearchRecord struct {
	Record struct {
		ID           string `json:"id"`
		Content      string `json:"content"`
		StrategyType string `json:"strategy_type"`
	} `json:"record"`
	Score float64 `json:"score"`
}

// sessionCreateRequest 创建会话请求。
type sessionCreateRequest struct {
	ActorID     string `json:"actor_id,omitempty"`
	AssistantID string `json:"assistant_id,omitempty"`
}

// sessionInfo 会话信息响应。
type sessionInfo struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	ActorID     string `json:"actor_id"`
	AssistantID string `json:"assistant_id"`
}

// textMessagePart 文本消息部分。
type textMessagePart struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// textMessage 文本消息。
// 对齐 Python: TextMessage.to_dict() — wire 格式使用 parts 数组
type textMessage struct {
	Role        string            `json:"role"` // "user"/"assistant"/"system"
	Parts       []textMessagePart `json:"parts"`
	ActorID     string            `json:"actor_id,omitempty"`
	AssistantID string            `json:"assistant_id,omitempty"`
}

// addMessagesRequest 添加消息请求。
type addMessagesRequest struct {
	Messages []textMessage `json:"messages"`
}

// normalizedMemoryItem 归一化记忆条目（Provider 内部使用）。
// 对齐 Python: {"memory": content, "score": score}
type normalizedMemoryItem struct {
	Memory string  `json:"memory"`
	Score  float64 `json:"score"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentartsDefaultBaseURL 默认 Data Plane 基础 URL
	// 对齐 Python: DEFAULT_BASE_URL
	agentartsDefaultBaseURL = "https://memory.cn-southwest-2.huaweicloud-agentarts.com"
	// agentartsHTTPTimeout HTTP 请求超时
	agentartsHTTPTimeout = 60 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAgentArtsClient 创建 AgentArts HTTP 客户端。
// 对齐 Python: _get_client() 中 MemoryClient(api_key=...) 的初始化
func newAgentArtsClient(baseURL, apiKey string) *agentartsClient {
	// 对齐 Python: (base_url or DEFAULT_BASE_URL).rstrip("/")
	if baseURL == "" {
		baseURL = agentartsDefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &agentartsClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: agentartsHTTPTimeout,
		},
	}
}

// doRequest 执行 HTTP 请求并解析 JSON 响应。
func (c *agentartsClient) doRequest(ctx context.Context, method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	// 对齐 Python: Authorization: Bearer {api_key}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AgentArts API 错误: status=%d body=%s", resp.StatusCode, string(respData))
	}

	if result != nil {
		if err := json.Unmarshal(respData, result); err != nil {
			return fmt.Errorf("解析响应 JSON 失败: %w", err)
		}
	}

	return nil
}

// searchMemories 搜索记忆。
// 对齐 Python: client.search_memories(space_id, filters=...)
// REST: POST /v1/core/spaces/{space_id}/memories/search
func (c *agentartsClient) searchMemories(ctx context.Context, spaceID string, filter memorySearchFilter) ([]normalizedMemoryItem, error) {
	path := fmt.Sprintf("/v1/core/spaces/%s/memories/search", spaceID)
	var resp memorySearchResponse
	if err := c.doRequest(ctx, http.MethodPost, path, filter, &resp); err != nil {
		return nil, err
	}

	// 对齐 Python: normalized = [... for item in items if content]
	items := make([]normalizedMemoryItem, 0, len(resp.Records))
	for _, r := range resp.Records {
		if r.Record.Content != "" {
			items = append(items, normalizedMemoryItem{
				Memory: r.Record.Content,
				Score:  r.Score,
			})
		}
	}
	return items, nil
}

// createMemorySession 创建记忆会话。
// 对齐 Python: client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)
// REST: POST /v1/core/spaces/{space_id}/sessions
func (c *agentartsClient) createMemorySession(ctx context.Context, spaceID string, req sessionCreateRequest) (*sessionInfo, error) {
	path := fmt.Sprintf("/v1/core/spaces/%s/sessions", spaceID)
	var info sessionInfo
	if err := c.doRequest(ctx, http.MethodPost, path, req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// addMessages 添加消息到会话。
// 对齐 Python: client.add_messages(space_id=..., session_id=..., messages=[...])
// REST: POST /v1/core/spaces/{space_id}/sessions/{session_id}/messages
func (c *agentartsClient) addMessages(ctx context.Context, spaceID, sessionID string, msgs []textMessage) error {
	path := fmt.Sprintf("/v1/core/spaces/%s/sessions/%s/messages", spaceID, sessionID)
	return c.doRequest(ctx, http.MethodPost, path, addMessagesRequest{Messages: msgs}, nil)
}

// buildTextMessage 构建 TextMessage。
// 对齐 Python: _text_message(role, content, actor_id=..., assistant_id=...)
func buildTextMessage(role, content, actorID, assistantID string) textMessage {
	msg := textMessage{
		Role:  role,
		Parts: []textMessagePart{{Type: "text", Text: content}},
	}
	// 对齐 Python: 仅在非空时设置 actor_id/assistant_id
	if actorID != "" {
		msg.ActorID = actorID
	}
	if assistantID != "" {
		msg.AssistantID = assistantID
	}
	return msg
}
