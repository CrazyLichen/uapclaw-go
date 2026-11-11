package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// chatCompletionsPath Chat Completion API 路径
const chatCompletionsPath = "/chat/completions"

// emptyAPIKeyPlaceholder 非 api_key 模式下 provider 要求的占位密钥。
// Python: openai_model_client.py:326-330 _resolved_api_key_for_config → "EMPTY"
const emptyAPIKeyPlaceholder = "EMPTY"

// requestEncodingStage 请求编码阶段的 details.stage 标记。
// Python: request_encoding.py 的 details={"stage": "request_encoding"}
const requestEncodingStage = "request_encoding"

// defaultRequestTimeoutSeconds 默认请求超时（秒）。
// Python: schema/config.py:145 timeout 默认 360（gt=0；注释明文约定
// timeout >= stream_first_chunk_timeout=300，传输层永不先于应用层流超时触发）
const defaultRequestTimeoutSeconds = 360.0

// ──────────────────────────── 导出函数 ────────────────────────────

// AdjustParamsForOpenAI 对请求参数做 OpenAI 特有调整。
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
// 不推断 /v1（上游同样不注入）。用户在 api_base 里粘贴完整端点 URL 时，
// 若不剥离会拼成 ".../chat/completions/chat/completions" 导致 404。
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
	// 旧签名等价于 api_key 模式（默认）
	return BuildHTTPRequestWithAuth(ctx, apiBase, apiKey, params, headers, timeout,
		verifySSL, sslCert, streaming, llmschema.AuthModeAPIKey)
}

// BuildHTTPRequestWithAuth 构建 HTTP 请求并按 auth_mode 决定是否设置 Authorization。
// 鉴权头规则（Python: openai_model_client.py:298-305 / 326-330）：
//   - auth_mode=none / openai_account_oauth → 省略
//   - auth_mode=custom_headers 且 api_key 为空 → 省略（靠自定义头携带凭据）
//   - 其余情况设置 Bearer；api_key 为空时用 "EMPTY" 占位（上游 _resolved_api_key_for_config）
func BuildHTTPRequestWithAuth(
	ctx context.Context,
	apiBase, apiKey string,
	params map[string]any,
	headers map[string]string,
	timeout *float64,
	verifySSL bool,
	sslCert string,
	streaming bool,
	authMode llmschema.AuthMode,
) (*http.Request, *http.Client, error) {
	// 序列化请求体（编码失败/非法 UTF-8 → 181004 + stage=request_encoding，C-E2）
	body, err := encodeJSONChecked(params)
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
	if !shouldOmitAuthorization(authMode, apiKey) {
		// api_key 为空时用占位符（provider 仍要求 Authorization 头存在）
		resolvedKey := apiKey
		if strings.TrimSpace(resolvedKey) == "" {
			resolvedKey = emptyAPIKeyPlaceholder
		}
		req.Header.Set("Authorization", "Bearer "+resolvedKey)
	}

	// 合并自定义请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 共享 Transport 池（C-14）：按安全形态分桶复用连接。
	// Client 是每请求新建的轻对象——共享的是 Transport，因此 Client.Timeout 只作用于本请求。
	client := &http.Client{Transport: model_clients.GetSharedTransport(authMode, apiKey, NormalizeAPIBase(apiBase), verifySSL, sslCert)}
	if !streaming {
		client.Timeout = time.Duration(effectiveTimeout * float64(time.Second))
	}

	return req, client, nil
}

// openaiExtraBodyExtensionFields 必须搬进 extra_body 的厂商私有字段。
//
// 上游注释：Vendor thinking flags must ride in extra_body; OpenAI SDK rejects them
// as top-level chat.completions.create kwargs. Go 虽不走 SDK，仍保持同一语义
// （厂商只从 extra_body 读这些键）。
//
// Python: openai_model_client.py:120-131 _OPENAI_EXTRA_BODY_EXTENSION_FIELDS（7 个）
var openaiExtraBodyExtensionFields = []string{
	"agent_hint",
	"cache_salt",
	"cache_sharing",
	"return_token_ids",
	"enable_thinking",
	"thinking",
	"chat_template_kwargs",
}

// HandleExtraBody 将 params 中的厂商私有字段移入 extra_body。
//
// 上游对 7 个键做统一搬移（循环 _OPENAI_EXTRA_BODY_EXTENSION_FIELDS）：
// 这些键放顶层时厂商插件读不到，必须进 JSON body 的 extra_body。
//
// Python: openai_model_client.py:1149-1157 _move_openai_extra_body_extensions
func HandleExtraBody(params map[string]any) {
	var moved map[string]any
	for _, key := range openaiExtraBodyExtensionFields {
		v, ok := params[key]
		if !ok {
			continue
		}
		if moved == nil {
			moved = make(map[string]any, len(openaiExtraBodyExtensionFields))
		}
		moved[key] = v
		delete(params, key)
	}
	if len(moved) == 0 {
		return
	}
	extraBody, _ := params["extra_body"].(map[string]any)
	if extraBody == nil {
		extraBody = make(map[string]any, len(moved))
	}
	for k, v := range moved {
		extraBody[k] = v
	}
	params["extra_body"] = extraBody
}

// DropOrphanToolCalls 删除历史消息中既无 id 又无 function.name 的 tool_call。
//
// 这类条目是流式碎片聚合失败或上游写入残缺留下的，发给厂商会 400。
// 某条消息的 tool_calls 被删空时，连带删除该键。
// 返回新切片，不就地修改入参元素（与上游 list(messages) 后替换元素一致）。
//
// Python: llm_provider_compat_patch.py:148-167 _drop_orphan_tool_calls
func DropOrphanToolCalls(messages []map[string]any) []map[string]any {
	var repaired []map[string]any
	for pos, msg := range messages {
		calls, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(calls))
		for _, call := range calls {
			if !isOrphanToolCall(call) {
				kept = append(kept, call)
			}
		}
		if len(kept) == len(calls) {
			continue
		}
		// 首处改动时整体浅拷贝，避免就地修改调用方切片
		if repaired == nil {
			repaired = make([]map[string]any, len(messages))
			copy(repaired, messages)
		}
		fixed := make(map[string]any, len(msg))
		for k, v := range msg {
			fixed[k] = v
		}
		if len(kept) == 0 {
			delete(fixed, "tool_calls")
		} else {
			fixed["tool_calls"] = kept
		}
		repaired[pos] = fixed
		logger.Warn(logComponent).
			Int("dropped", len(calls)-len(kept)).
			Int("position", pos).
			Msg("Dropped tool call(s) without id and name from history message.")
	}
	if repaired == nil {
		return messages
	}
	return repaired
}

// isOrphanToolCall 判定 tool_call 是否既无 id 又无 function.name。
// 非 map 的条目不算孤儿（保留原样，交给厂商报错更易定位）。
//
// Python: llm_provider_compat_patch.py:142-145 _is_orphan_tool_call
func isOrphanToolCall(call any) bool {
	m, ok := call.(map[string]any)
	if !ok {
		return false
	}
	if id, _ := m["id"].(string); id != "" {
		return false
	}
	fn, _ := m["function"].(map[string]any)
	if fn == nil {
		return true
	}
	name, _ := fn["name"].(string)
	return name == ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// encodeJSONChecked 序列化请求体并校验 UTF-8，失败抛 181004 MODEL_INVOKE_PARAM_ERROR。
// 覆盖四类本地请求构建失败（重试无意义的错误，不应被当成瞬时故障重试）：
//   - json 不支持的值：NaN / ±Inf / chan / func 等不可序列化类型
//   - 非 UTF-8 字节：Python 侧是 UnicodeEncodeError（ValueError 子类），
//     Go 的 encoding/json 会静默替换为 U+FFFD——按上游严格性改为报错
//
// Python: utils/request_encoding.py:27-35 create_encoding_aware_openai_client 的
// _build_request 包装（catch TypeError/ValueError → MODEL_INVOKE_PARAM_ERROR +
// details={"stage": "request_encoding"}）
func encodeJSONChecked(params map[string]any) ([]byte, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("Cannot encode model request: %v", err)),
			exception.WithDetails(map[string]any{"stage": requestEncodingStage}),
			exception.WithCause(err),
		)
	}
	// 输入侧 UTF-8 校验：json.Marshal 会把非法字节静默替换为 U+FFFD（编码后的 body 恒合法），
	// 故必须在编码前逐字符串检查，才能对齐 Python 的 UnicodeEncodeError 语义。
	if badPath := findInvalidUTF8(params, ""); badPath != "" {
		return nil, exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("Cannot encode model request: invalid UTF-8 at %s", badPath)),
			exception.WithDetails(map[string]any{"stage": requestEncodingStage}),
		)
	}
	return body, nil
}

// findInvalidUTF8 递归查找参数树中第一个非法 UTF-8 字符串，返回其路径（"" 表示全部合法）。
// Python 对照：UnicodeEncodeError 在 SDK 构建请求体时抛出（lone surrogate 等），
// Go 的 encoding/json 不报错而是替换字符，故需显式前置检查。
func findInvalidUTF8(value any, path string) string {
	switch v := value.(type) {
	case string:
		if !utf8.ValidString(v) {
			return path
		}
	case map[string]any:
		for key, item := range v {
			child := path + "." + key
			if !utf8.ValidString(key) {
				return child
			}
			if bad := findInvalidUTF8(item, child); bad != "" {
				return bad
			}
		}
	case []any:
		for i, item := range v {
			if bad := findInvalidUTF8(item, fmt.Sprintf("%s[%d]", path, i)); bad != "" {
				return bad
			}
		}
	case []map[string]any:
		// BuildRequestParams 产出的消息列表是该具体类型，不是 []any
		for i, item := range v {
			if bad := findInvalidUTF8(item, fmt.Sprintf("%s[%d]", path, i)); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// IsRequestEncodingErr 判是否为请求编码阶段错误（181004 + stage=request_encoding）。
// 调用方据此原样上抛，不再包装成 MODEL_CALL_FAILED，从而跳过瞬时失败重试。
// Python: openai_model_client.py:2011/2228
//
//	if isinstance(e, ValidationError) and getattr(e, "details", None) == {"stage": "request_encoding"}:
//	    raise
func IsRequestEncodingErr(err error) bool {
	var base *exception.BaseError
	if !errors.As(err, &base) {
		return false
	}
	if base.Code() != exception.StatusModelInvokeParamError.Code() {
		return false
	}
	details, _ := base.Details().(map[string]any)
	stage, _ := details["stage"].(string)
	return stage == requestEncodingStage
}

// encodeJSON 将 map 编码为 JSON 字节。
func encodeJSON(params map[string]any) ([]byte, error) {
	return json.Marshal(params)
}

// shouldOmitAuthorization 判是否省略 Authorization 头。
// Python: openai_model_client.py:298-305 _should_omit_authorization
func shouldOmitAuthorization(authMode llmschema.AuthMode, apiKey string) bool {
	switch authMode {
	case llmschema.AuthModeNone, llmschema.AuthModeOpenAIAccountOAuth:
		return true
	case llmschema.AuthModeCustomHeaders:
		return strings.TrimSpace(apiKey) == ""
	default:
		return false
	}
}
