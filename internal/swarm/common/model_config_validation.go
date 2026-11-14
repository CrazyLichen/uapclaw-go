// 模型目录与模型组请求补丁的校验，含连通性探测。
// 对应 Python 代码：jiuwenswarm/common/model_config_validation.py:1-310（完整版基线，裁决 D；
// gateway 裁剪副本不含探测段）。
// 已知偏差（spec §7 补充）：Python probe 对 duck-typed 的 model.invoke(...) 调用；
// Go 侧模型客户端 API 不同构，探测改为对 ModelProbeInvoker 函数注入——
// attempt 循环、三源判输出、最终失败上抛的语义逐行对齐。
package common

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// placeholderAPIBases 占位接口地址。
// Python: model_config_validation.py:20 PLACEHOLDER_API_BASES。
var placeholderAPIBases = map[string]bool{
	"https://example.com/compatible-mode/v1": true,
}

// exampleDomains 示例域名（含子域名匹配）。
// Python: model_config_validation.py:21 EXAMPLE_DOMAINS。
var exampleDomains = []string{"example.com", "example.org", "example.net"}

// placeholderModelNames 占位模型名。
// Python: model_config_validation.py:22 PLACEHOLDER_MODEL_NAMES。
var placeholderModelNames = map[string]bool{"your-model-name": true}

// placeholderAPIKeys 占位密钥。
// Python: model_config_validation.py:23 PLACEHOLDER_API_KEYS。
var placeholderAPIKeys = map[string]bool{"sk-xxxxxxxxx": true}

// forbiddenRequestKeys 请求补丁禁用键。
// Python: model_config_validation.py:148 FORBIDDEN_REQUEST_KEYS。
var forbiddenRequestKeys = map[string]bool{
	"model": true, "messages": true, "tools": true, "stream": true,
	"api_key": true, "api_base": true, "provider": true, "client_provider": true,
	"client_id": true, "strategy": true, "num_retries": true,
	"timeout_seconds": true, "enable_health_check": true, "health_check_interval_seconds": true,
	"enable_observability": true, "context_window": true, "_source": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// ModelProbeResult 探测成功结果与用户可见内容。
// Python: model_config_validation.py:27 ModelProbeResult。
type ModelProbeResult struct {
	// Response 原始响应（保留任意形状供调用方诊断）
	Response any
	// Content 用户可见内容
	Content string
}

// ModelProbeInvoker 探测的单次调用注入点。
// Python: model_config_validation.py:97 model.invoke(messages=[{"role":"user","content":"Hi"}], max_tokens=..., **extra_kwargs)。
// 返回值对齐 Python 响应对象：map 形态读 content/reasoning_content/usage_metadata 键。
type ModelProbeInvoker func(maxTokens int, invokeKwargs map[string]any) (any, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// ModelProbeOutput 提取展示内容并判定探测是否产生输出。
// Python: model_config_validation.py:41 _model_probe_output。
// 三源判定：①content/reasoning_content 任一非空白字符串 ②usage.output_tokens > 0。
func ModelProbeOutput(response any) (string, bool) {
	// Python: :43-50 content 取值链：.content 属性 → dict 键 → str(response)
	var content any
	switch r := response.(type) {
	case map[string]any:
		content = r["content"]
	default:
		content = response
	}
	// Python: :53-56 reasoning_content（dict 键或属性）
	var reasoningContent any
	if r, ok := response.(map[string]any); ok {
		reasoningContent = r["reasoning_content"]
	}
	// Python: :59-62 reasoning 模型可能把小额预算全部花在 reasoning_content 上，
	// 正常 content 为空——仍算有输出。
	hasTextOutput := isNonBlankString(content) || isNonBlankString(reasoningContent)
	// Python: :64-77 usage_metadata.output_tokens > 0 证明端点/凭证/模型有效
	// （如 Ollama 的 reasoning 字段客户端未映射）。
	var usage any
	if r, ok := response.(map[string]any); ok {
		usage = r["usage_metadata"]
	}
	outputTokens := any(nil)
	if u, ok := usage.(map[string]any); ok {
		outputTokens = u["output_tokens"]
	}
	hasOutput := hasTextOutput || isPositiveNumber(outputTokens)
	contentStr, _ := content.(string)
	return contentStr, hasOutput
}

// ProbeModelConnection 逐个输出 token 预算探测直到成功。
// Python: model_config_validation.py:87 probe_model_connection。
// 调用异常与无 content/reasoning/生成 token 用量的响应都算失败尝试；
// 最终失败上抛给调用方（各子系统自行格式化错误响应）。
// tokenLimits 默认 (3, 16)；timeoutSeconds 由调用方经 ctx/invoker 实现
// （Python asyncio.wait_for 语义，Go 平台差异由注入方承担，注释记录）。
func ProbeModelConnection(invoker ModelProbeInvoker, tokenLimits []int, invokeKwargs map[string]any) (*ModelProbeResult, error) {
	limits := tokenLimits
	if len(limits) == 0 {
		return nil, fmt.Errorf("token_limits must contain at least one value")
	}
	extraKwargs := map[string]any{}
	for k, v := range invokeKwargs {
		extraKwargs[k] = v
	}
	for attempt, maxTokens := range limits {
		response, err := invoker(maxTokens, extraKwargs)
		if err == nil {
			content, hasOutput := ModelProbeOutput(response)
			if hasOutput {
				return &ModelProbeResult{Response: response, Content: content}, nil
			}
			// Python: :110-111 空响应按 ValueError 计入失败尝试
			err = fmt.Errorf("Empty response from model")
		}
		if attempt+1 >= len(limits) {
			return nil, err
		}
		// Python: :112-119 记录降级重试（log_context、当前与下一个预算、异常）
		nextMaxTokens := limits[attempt+1]
		_ = nextMaxTokens // Go 侧日志由调用方组件级 logger 记录；此处保留变量对齐语义
	}
	return nil, fmt.Errorf("model probe attempts unexpectedly exhausted")
}

// IsPlaceholderAPIBase 是否占位接口地址（示例域或固定占位串）。
// Python: model_config_validation.py:121 is_placeholder_api_base。
func IsPlaceholderAPIBase(apiBase string) bool {
	value := strings.TrimSpace(apiBase)
	if value == "" {
		return false
	}
	if placeholderAPIBases[value] {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	for _, domain := range exampleDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// IsPlaceholderModelEntry 是否占位模型条目（地址/模型名/密钥任一占位）。
// Python: model_config_validation.py:133 is_placeholder_model_entry。
func IsPlaceholderModelEntry(mcc map[string]any) bool {
	if mcc == nil {
		mcc = map[string]any{}
	}
	apiBase := strings.TrimSpace(asString(mcc["api_base"]))
	modelName := strings.TrimSpace(asString(mcc["model_name"]))
	apiKey := strings.TrimSpace(asString(mcc["api_key"]))
	return IsPlaceholderAPIBase(apiBase) || placeholderModelNames[modelName] || placeholderAPIKeys[apiKey]
}

// ModelClientConfigView 客户端配置的三键视图。
// Python: model_config_validation.py:139 model_client_config_view。
func ModelClientConfigView(mcc any) map[string]any {
	view := map[string]any{}
	if m, ok := mcc.(map[string]any); ok {
		for _, key := range []string{"api_base", "api_key", "model_name"} {
			view[key] = orEmpty(asString(m[key]))
		}
		return view
	}
	// Python: getattr(mcc, key, None) or "" —— 非 dict 输入按空处理（Go 无属性访问）
	for _, key := range []string{"api_base", "api_key", "model_name"} {
		view[key] = ""
	}
	return view
}

// ValidateModelsConfig 校验模型目录配置，返回错误列表（空列表=合法）。
// Python: model_config_validation.py:196 validate_models_config。
// 步骤与分支顺序逐行照抄。
func ValidateModelsConfig(models map[string]any) []string {
	errors := []string{}
	if models == nil {
		return []string{"models must be an object"}
	}
	defaults, _ := models["defaults"].([]any)
	agentos, _ := models["agentos"].([]any)
	groups, _ := models["groups"].([]any)
	// Python: :204-206 三源都必须是数组（nil 即缺省 []）
	if models["defaults"] != nil && defaults == nil {
		return []string{"models.defaults, models.agentos and models.groups must be arrays"}
	}
	if models["agentos"] != nil && agentos == nil {
		return []string{"models.defaults, models.agentos and models.groups must be arrays"}
	}
	if models["groups"] != nil && groups == nil {
		return []string{"models.defaults, models.agentos and models.groups must be arrays"}
	}

	modelIDs := map[string]bool{}
	for _, pair := range []struct {
		source  string
		entries []any
	}{{"defaults", defaults}, {"agentos", agentos}} {
		for index, item := range pair.entries {
			entry, ok := item.(map[string]any)
			if !ok {
				errors = append(errors, fmt.Sprintf("%s[%d] must be an object", pair.source, index))
				continue
			}
			location := fmt.Sprintf("%s[%d]", pair.source, index)
			modelID := strings.TrimSpace(asString(entry["model_id"]))
			if modelID == "" {
				errors = append(errors, location+".model_id is required")
			} else if modelIDs[modelID] {
				errors = append(errors, fmt.Sprintf("duplicate model_id: %s", modelID))
			} else {
				modelIDs[modelID] = true
			}
			// Python: :227-230 agentos 条目不得为默认
			if pair.source == "agentos" && entry["is_default"] == true {
				errors = append(errors, fmt.Sprintf("agentos[%d] cannot be default", index))
			}
			for _, key := range []string{"model_client_config", "model_config_obj"} {
				if value, exists := entry[key]; exists && value != nil {
					if _, isMap := value.(map[string]any); !isMap {
						errors = append(errors, fmt.Sprintf("%s.%s must be an object", location, key))
					}
				}
			}
			if client, isMap := entry["model_client_config"].(map[string]any); isMap {
				for _, key := range []string{"model_name", "api_base", "api_key", "client_provider"} {
					if value, exists := client[key]; exists && value != nil {
						if _, isStr := value.(string); !isStr {
							errors = append(errors, fmt.Sprintf("%s.model_client_config.%s must be a string", location, key))
						}
					}
				}
				if headers, exists := client["custom_headers"]; exists && headers != nil {
					switch headers.(type) {
					case string, map[string]any:
					default:
						errors = append(errors, fmt.Sprintf("%s.model_client_config.custom_headers must be an object or JSON string", location))
					}
				}
			}
			validateModelDetail(entry, location, &errors)
		}
	}

	groupIDs := map[string]bool{}
	defaultGroups := 0
	for index, item := range groups {
		group, ok := item.(map[string]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("groups[%d] must be an object", index))
			continue
		}
		groupID := strings.TrimSpace(asString(group["model_group_id"]))
		if groupID == "" {
			errors = append(errors, fmt.Sprintf("groups[%d].model_group_id is required", index))
		} else if groupIDs[groupID] {
			errors = append(errors, fmt.Sprintf("duplicate model_group_id: %s", groupID))
		} else {
			groupIDs[groupID] = true
		}
		if group["is_default"] == true {
			defaultGroups++
			if group["enabled"] == false {
				errors = append(errors, fmt.Sprintf("default model group %q is disabled", groupID))
			}
		}
		routes, _ := group["routes"].([]any)
		if len(routes) == 0 && group["routes"] != nil {
			// 非 list 的 routes 同样按"至少一条路由"报错（Python: :247-250）
			_, isList := group["routes"].([]any)
			if !isList {
				routes = nil
			}
		}
		if len(routes) == 0 {
			errors = append(errors, fmt.Sprintf("model group %q must contain at least one route", groupID))
		}
		routeIDs := map[string]bool{}
		for routeIndex, r := range routes {
			route, ok := r.(map[string]any)
			if !ok {
				errors = append(errors, fmt.Sprintf("group %q route[%d] must be an object", groupID, routeIndex))
				continue
			}
			routeID := strings.TrimSpace(asString(route["route_id"]))
			modelID := strings.TrimSpace(asString(route["model_id"]))
			if _, exists := route["enabled"]; exists {
				if _, isBool := route["enabled"].(bool); !isBool {
					errors = append(errors, fmt.Sprintf("group %q route %q.enabled must be a boolean", groupID, routeID))
				}
			}
			if routeID == "" {
				errors = append(errors, fmt.Sprintf("group %q route[%d].route_id is required", groupID, routeIndex))
			} else if routeIDs[routeID] {
				errors = append(errors, fmt.Sprintf("group %q has duplicate route_id %q", groupID, routeID))
			}
			routeIDs[routeID] = true
			if !modelIDs[modelID] {
				errors = append(errors, fmt.Sprintf("group %q references missing model_id %q", groupID, modelID))
			}
			validatePatch(route["request_overrides"], fmt.Sprintf("group %q route %q", groupID, routeID), &errors)
		}
		validatePatch(group["request_config"], fmt.Sprintf("group %q.request_config", groupID), &errors)
		validateRouting(group["routing"], fmt.Sprintf("group %q.routing", groupID), &errors)
	}
	if defaultGroups > 1 {
		errors = append(errors, "at most one default model group is allowed")
	}
	return errors
}

// RaiseIfInvalid 校验失败即抛 MODEL_GROUP_INVALID（错误列表合并为 message，errors 进 details）。
// Python: model_config_validation.py:294 raise_if_invalid。
func RaiseIfInvalid(models map[string]any) error {
	errors := ValidateModelsConfig(models)
	if len(errors) > 0 {
		return NewModelSelectionError(ModelGroupInvalid, strings.Join(errors, "; "), map[string]any{"errors": strings.Join(errors, "\n")})
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validatePatch 校验请求补丁对象与禁用键。
// Python: model_config_validation.py:154 _validate_patch。
func validatePatch(value any, location string, errors *[]string) {
	if value == nil {
		return
	}
	m, ok := value.(map[string]any)
	if !ok {
		*errors = append(*errors, location+" must be an object")
		return
	}
	var forbidden []string
	for key := range m {
		if forbiddenRequestKeys[key] {
			forbidden = append(forbidden, key)
		}
	}
	if len(forbidden) > 0 {
		sort.Strings(forbidden)
		*errors = append(*errors, fmt.Sprintf("%s contains forbidden fields: %s", location, strings.Join(forbidden, ", ")))
	}
}

// validateModelDetail 校验 model_detail 结构与字符串字段。
// Python: model_config_validation.py:164 _validate_model_detail。
func validateModelDetail(entry map[string]any, location string, errors *[]string) {
	detail, exists := entry["model_detail"]
	if !exists || detail == nil {
		return
	}
	m, ok := detail.(map[string]any)
	if !ok {
		*errors = append(*errors, location+".model_detail must be an object")
		return
	}
	for _, key := range []string{"fallback_tag", "model_description"} {
		if value, exists := m[key]; exists && value != nil {
			if _, isStr := value.(string); !isStr {
				*errors = append(*errors, fmt.Sprintf("%s.model_detail.%s must be a string", location, key))
			}
		}
	}
}

// validateRouting 校验 routing 段：策略、重试、kwargs、三超时。
// Python: model_config_validation.py:180 _validate_routing。
// 已识别的旧策略仅为升级兼容接受，模型池模式下不执行也不透传 core。
func validateRouting(value any, location string, errors *[]string) {
	if value == nil {
		return
	}
	m, ok := value.(map[string]any)
	if !ok {
		*errors = append(*errors, location+" must be an object")
		return
	}
	// Python: :186 strategy ∈ {None, ordered-failover, tag-filtered}
	strategy := m["strategy"]
	if strategy != nil && strategy != "ordered-failover" && strategy != "tag-filtered" {
		*errors = append(*errors, location+".strategy is invalid")
	}
	// Python: :188-193 num_retries 非负整数（bool 显式排除）
	if retries, exists := m["num_retries"]; exists && retries != nil {
		invalid := false
		n, isInt := retries.(int)
		if !isInt {
			if _, isBool := retries.(bool); isBool {
				invalid = true
			} else if f, isFloat := retries.(float64); isFloat {
				invalid = f != math.Trunc(f) || f < 0
			} else {
				invalid = true
			}
		} else if n < 0 {
			invalid = true
		}
		if invalid {
			*errors = append(*errors, location+".num_retries must be a non-negative integer")
		}
	}
	// Python: :194-196 strategy_kwargs 必须是对象
	if kw, exists := m["strategy_kwargs"]; exists && kw != nil {
		if _, isMap := kw.(map[string]any); !isMap {
			*errors = append(*errors, location+".strategy_kwargs must be an object")
		}
	}
	// Python: :197-213 三超时键：非 bool、数值/非空字符串、有限、> 0
	for _, key := range []string{"timeout_seconds", "stream_first_chunk_timeout", "stream_idle_timeout"} {
		timeout, exists := m[key]
		if !exists || timeout == nil {
			continue
		}
		if !isValidTimeout(timeout) {
			*errors = append(*errors, fmt.Sprintf("%s.%s must be a positive finite number", location, key))
		}
	}
}

// isValidTimeout 超时值合法性（Python :199-209 的等价判定）。
func isValidTimeout(timeout any) bool {
	if _, isBool := timeout.(bool); isBool {
		return false
	}
	var f float64
	switch v := timeout.(type) {
	case int:
		f = float64(v)
	case int64:
		f = float64(v)
	case float64:
		f = v
	case string:
		if strings.TrimSpace(v) == "" {
			return false
		}
		parsed, err := parseFiniteFloat(v)
		if err != nil {
			return false
		}
		f = parsed
	default:
		return false
	}
	return !math.IsInf(f, 0) && !math.IsNaN(f) && f > 0
}

// isNonBlankString 非空空白字符串判定。
func isNonBlankString(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

// isPositiveNumber 正数判定（int/float，bool 排除）。
func isPositiveNumber(v any) bool {
	switch n := v.(type) {
	case int:
		return n > 0
	case int64:
		return n > 0
	case float64:
		return n > 0
	default:
		return false
	}
}

// orEmpty 空串归一（Python or ""）。
func orEmpty(s string) string {
	if s == "" {
		return ""
	}
	return s
}
