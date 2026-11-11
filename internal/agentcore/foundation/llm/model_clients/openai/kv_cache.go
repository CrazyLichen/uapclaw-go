package openai

// 本文件提供 vLLM KV cache 释放能力。
//
// 迁移来源：inference_affinity 包的 Release + buildReleaseRequestBody。
// 别名归一后 InferenceAffinity 并入本客户端，能力改由
// extensions.kv_cache.mode == "affinity" 驱动（见 client.go SupportsKVCacheRelease）。
//
// Python: openai_model_client.py:542 supports_kv_cache_affinity（上游无独立 release 方法，
// Go 侧 Release 是本仓库的 API 形态，行为对齐原 inference_affinity 实现）

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// releaseKVCachePath vLLM KV Cache 释放 API 路径。
const releaseKVCachePath = "/release_kv_cache"

// ──────────────────────────── 导出函数 ────────────────────────────

// Release 释放 vLLM KV Cache。
// 调用 {api_base}/release_kv_cache 接口，释放指定会话的 KV Cache。
// Python: 200 响应即使非 JSON 也返回 true；非 200 返回 false；异常返回 error。
// Python: InferenceAffinityModelClient.release()
func (c *OpenAIModelClient) Release(
	ctx context.Context,
	opts ...model_clients.ReleaseOption,
) (bool, error) {
	// 0. 能力闸门：仅 kv_cache.mode == "affinity" 的部署支持释放
	// （对齐 SupportsKVCacheRelease；普通 OpenAI/api_key 配置直接拒绝，不发请求）
	if !c.SupportsKVCacheRelease() {
		return false, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("OpenAI client does not support KV cache release"),
		)
	}

	params := model_clients.NewReleaseParams(opts...)

	// 1. 构建请求体
	releaseBody, err := c.buildReleaseRequestBody(params)
	if err != nil {
		return false, err
	}

	// 2. 构建 HTTP 请求
	// api_base 归一（C-E1）：剥离已带的 /chat/completions，避免双段
	apiURL := NormalizeAPIBase(c.ClientConfig.APIBase) + releaseKVCachePath
	bodyBytes, err := json.Marshal(releaseBody)
	if err != nil {
		return false, fmt.Errorf("序列化 release 请求体失败: %w", err)
	}

	// 总超时挂 ctx（非流式语义，方法级>配置级）；须在建请求前派生
	releaseTimeout := c.RequestTimeout(nil)
	if releaseTimeout == nil {
		// Python: schema/config.py:145 默认 360
		def := 360.0
		releaseTimeout = &def
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*releaseTimeout*float64(time.Second)))
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Errorf("创建 release HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.ClientConfig.APIKey)

	// 3. 共享 Transport（C-A/C-14 收敛）
	client := &http.Client{Transport: model_clients.GetSharedTransport(c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert)}

	// 4. 发送请求（对齐 Python: release 使用 llm_logger，非回调）
	logger.Info(logComponent).
		Str("model_name", params.Model).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Str("session_id", params.SessionID).
		Int("messages_released_index", params.MessagesReleasedIndex).
		Msg("Releasing KV cache.")

	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).
			Str("model_name", params.Model).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Str("session_id", params.SessionID).
			Err(err).
			Msg("KV cache release request failed.")
		return false, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("Release error: %s", err.Error())),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	// 5. 处理响应
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		logger.Info(logComponent).
			Str("model_name", params.Model).
			Str("model_provider", c.ClientConfig.ClientProvider).
			Str("session_id", params.SessionID).
			Msg("KV cache release successful.")
		return true, nil
	}

	// 非 200 响应
	logger.Error(logComponent).
		Str("model_name", params.Model).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Str("session_id", params.SessionID).
		Int("status_code", resp.StatusCode).
		Str("response_body", string(respBody)).
		Msg("KV cache release failed.")
	return false, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildReleaseRequestBody 构建 Release 请求体。
// Python: InferenceAffinityModelClient.release() 中的 release_params 构建
func (c *OpenAIModelClient) buildReleaseRequestBody(
	params *model_clients.ReleaseParams,
) (map[string]any, error) {
	// 1. 确定模型名称
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}

	// 2. 转换并清洗消息
	var messagesDict []map[string]any
	if !params.Messages.IsEmpty() {
		converted, err := c.ConvertMessagesToDict(params.Messages)
		if err != nil {
			return nil, err
		}
		messagesDict = converted
		// 清洗 tool_calls
		c.sanitizeToolCalls(messagesDict)
	}

	// 3. 构建请求体
	releaseBody := map[string]any{
		"model":                   model,
		"cache_salt":              params.SessionID,
		"cache_sharing":           true,
		"messages":                messagesDict,
		"messages_released_index": params.MessagesReleasedIndex,
	}

	// 4. 添加可选的 tools
	if len(params.Tools) > 0 {
		releaseBody["tools"] = c.ConvertToolsToDict(params.Tools)
	}

	if params.ToolsReleasedIndex != nil {
		releaseBody["tools_released_index"] = *params.ToolsReleasedIndex
	}

	return releaseBody, nil
}
