package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// chatCompletionsPath Chat Completion API 路径
const chatCompletionsPath = "/chat/completions"

// defaultRequestTimeoutSeconds 默认请求超时（秒）。
//
// Python: schema/config.py:145 timeout 默认 360（gt=0；注释明文约定
// timeout >= stream_first_chunk_timeout=300，传输层永不先于应用层流超时触发）
const defaultRequestTimeoutSeconds = 360.0

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AdjustParamsForOpenAI 对请求参数做 OpenAI 特有调整。
//
// 当 api_base 含 "openai.com" 时，temperature 和 top_p 互斥：
//   - temperature 优先级高于 top_p
//   - 如果同时存在，删除 top_p
//   - 如果只有 top_p，保留 top_p
//
// Python: OpenAIModelClient._build_request_params() 中的 OpenAI 特有逻辑
func AdjustParamsForOpenAI(params map[string]any, apiBase string) {
	apiBaseLower := strings.ToLower(apiBase)
	if !strings.Contains(apiBaseLower, "openai.com") {
		return
	}

	_, hasTemp := params["temperature"]
	_, hasTopP := params["top_p"]

	// temperature 和 top_p 同时存在时，删除 top_p
	if hasTemp && hasTopP {
		delete(params, "top_p")
	}
}

// NormalizeAPIBase 归一 api_base：strip 空白 → 去尾斜杠 → 剥掉已带的 /chat/completions。
//
// 不推断 /v1（上游同样不注入）。用户在 api_base 里粘贴完整端点 URL 时，
// 若不剥离会拼成 ".../chat/completions/chat/completions" 导致 404。
//
// Python: openai_model_client.py:307-321 _normalize_openai_base_url
func NormalizeAPIBase(apiBase string) string {
	base := strings.TrimSpace(apiBase)
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, chatCompletionsPath) {
		base = strings.TrimSuffix(base, chatCompletionsPath)
		base = strings.TrimRight(base, "/")
	}
	return base
}

// BuildHTTPRequest 构建发送给 OpenAI API 的 HTTP 请求。
//
// 超时语义（设计文档批次 C-A，对齐上游 httpx 分阶段超时）：
//   - 非流式：`http.Client.Timeout` 承载总超时（Client 对象每请求新建、仅共享 Transport，
//     不会污染其他请求）；流式下与 Python read 语义不符，故置 0
//   - 流式：无总超时。等头由共享 Transport 的 ResponseHeaderTimeout 兜；
//     读 body 由 model.go 帧超时（首帧 300s/空闲 60s）+ cancelStream 兜
//   - timeout 为 nil 时取默认 360（满足 timeout >= first_chunk=300 不变量）
//
// Python: openai_model_client.py:1477-1490（final_timeout → SDK/httpx read 语义）
func BuildHTTPRequest(
	ctx context.Context,
	apiBase, apiKey string,
	params map[string]any,
	headers map[string]string,
	timeout *float64,
	verifySSL bool,
	sslCert string,
	streaming bool,
) (*http.Request, *http.Client, error) {
	// 序列化请求体（编码失败/非法 UTF-8 → 181004，C-E2 批次接入分类）
	body, err := encodeJSON(params)
	if err != nil {
		return nil, nil, err
	}

	// 构建 API URL（api_base 归一：C-E1）
	apiURL := NormalizeAPIBase(apiBase) + chatCompletionsPath

	// 超时预算：默认 360（对齐 Python schema/config.py:145）
	effectiveTimeout := defaultRequestTimeoutSeconds
	if timeout != nil && *timeout > 0 {
		effectiveTimeout = *timeout
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置必要请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 合并自定义请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 共享 Transport 池（C-14）：按安全形态分桶复用连接。
	// Client 是每请求新建的轻对象——共享的是 Transport，因此 Client.Timeout 只作用于本请求。
	client := &http.Client{Transport: model_clients.GetSharedTransport(verifySSL, sslCert)}
	if !streaming {
		client.Timeout = time.Duration(effectiveTimeout * float64(time.Second))
	}

	return req, client, nil
}

// HandleExtraBody 将 params 中的 return_token_ids 移入 extra_body。
//
// OpenAI SDK 会丢弃未知的顶级参数，vLLM 需要 return_token_ids 在 JSON body 中，
// 因此将其移入 extra_body 字段。
//
// Python: OpenAIModelClient.invoke() 中的 return_token_ids 处理
func HandleExtraBody(params map[string]any) {
	tokenIDs, ok := params["return_token_ids"]
	if !ok {
		return
	}
	delete(params, "return_token_ids")

	// 获取或创建 extra_body
	extraBody, _ := params["extra_body"].(map[string]any)
	if extraBody == nil {
		extraBody = make(map[string]any)
	}
	extraBody["return_token_ids"] = tokenIDs
	params["extra_body"] = extraBody
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// encodeJSON 将 map 编码为 JSON 字节。
func encodeJSON(params map[string]any) ([]byte, error) {
	return json.Marshal(params)
}
