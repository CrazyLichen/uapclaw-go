package anthropic

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// AnthropicContentBlocksMetadataKey thinking 回放块在 message.metadata 中的键。
// Python: anthropic_model_client.py:83
const AnthropicContentBlocksMetadataKey = "anthropic_content_blocks"

// anthropicInternalContentBlocksKey 转换后消息内部的旁路键。Python: :84
const anthropicInternalContentBlocksKey = "__anthropic_content_blocks"

// anthropicVersion anthropic-version 请求头（SDK 默认值）。
const anthropicVersion = "2023-06-01"

// messagesPath Messages API 路径（相对归一后 base_url）。
const messagesPath = "/v1/messages"

// midConversationSystemModelFamilies 接受 messages 内 system role 的模型族
// （GA 且无 beta header；其余模型 400 "role 'system' is not supported"，
// 条目折叠进顶层 system）。
// Python: anthropic_model_client.py:87-93
var midConversationSystemModelFamilies = []string{
	"claude-opus-5", "claude-opus-4-8", "claude-fable-5", "claude-mythos-5",
}

// anthropicCacheableBlockTypes 可挂 cache_control 的块类型。
// Python: anthropic_model_client.py:95-98
var anthropicCacheableBlockTypes = map[string]bool{
	"text": true, "image": true, "document": true, "tool_use": true, "tool_result": true,
}

// imageDataURLPattern base64 图片 data URL（JPEG/PNG/GIF/WebP）。
// Python: anthropic_model_client.py:99-103
var imageDataURLPattern = regexp.MustCompile(`(?is)^data:(image/(?:jpeg|png|gif|webp));base64,(.+)$`)

// lowOutputTokenCaps 老家族输出上限（按 family 子串匹配，兼容网关前缀与日期快照；
// 不要缩短为 claude-3：Claude 3.7 起上限远高于此）。
// Python: anthropic_model_client.py:106-112
var lowOutputTokenCaps = []struct {
	family string
	cap    int
}{
	{"claude-3-5", 8192}, {"claude-3.5", 8192},
	{"claude-3-opus", 4096}, {"claude-3-sonnet", 4096}, {"claude-3-haiku", 4096},
}

// defaultMaxTokensConst 未识别模型的默认输出上限。Python: anthropic_model_client.py:113
const defaultMaxTokensConst = 32768

// modelForbidsCustomSamplingRe 禁采样模型判定正则（claude- 前缀 + 五代家族）。
// Python: anthropic_model_client.py:246-256 _model_forbids_custom_sampling
var (
	modelForbidsOpusSonnet5Re = regexp.MustCompile(`claude-(?:opus|sonnet)-5(?:-|$)`)
	modelForbidsOpus47_48Re   = regexp.MustCompile(`claude-opus-4-(?:7|8)(?:-|$)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMaxTokens 调用方未设 max_tokens 时的默认值：识别的低上限家族守住
// 官方输出上限；其余（含未识别 id）保持高默认。
// Python: anthropic_model_client.py:119-131 default_max_tokens
func DefaultMaxTokens(model string) int {
	name := strings.ToLower(strings.TrimSpace(model))
	for _, fc := range lowOutputTokenCaps {
		if strings.Contains(name, fc.family) {
			return fc.cap
		}
	}
	return defaultMaxTokensConst
}

// ModelForbidsCustomSampling 返回当前 Claude 家族是否只接受默认采样。
// Python: anthropic_model_client.py:246-256 _model_forbids_custom_sampling
func ModelForbidsCustomSampling(model string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(model)), ".", "-")
	if !strings.HasPrefix(normalized, "claude-") {
		return false
	}
	if strings.Contains(normalized, "fable") || strings.Contains(normalized, "mythos") {
		return true
	}
	if modelForbidsOpusSonnet5Re.MatchString(normalized) {
		return true
	}
	return modelForbidsOpus47_48Re.MatchString(normalized)
}

// ConvertToolChoice 把常见/OpenAI tool-choice 形状翻译为 Anthropic 形状。
// Python: anthropic_model_client.py:221-236 _convert_tool_choice
func ConvertToolChoice(toolChoice any) map[string]any {
	if toolChoice == nil {
		return nil
	}
	switch tc := toolChoice.(type) {
	case string:
		if tc == "auto" {
			return nil
		}
		mapped := map[string]string{"required": "any", "none": "none", "any": "any"}[tc]
		if mapped == "" {
			return nil
		}
		return map[string]any{"type": mapped}
	case map[string]any:
		if name, _ := tc["name"].(string); tc["type"] == "function" && name != "" {
			return map[string]any{"type": "function", "name": name}
		}
	}
	return nil
}

// SamplingFromAnthropicParams 从请求参数读采样字段：extra_body（SDK 1.x）优先，
// 其次顶层 dict。
// Python: anthropic_model_client.py:238-244 _sampling_from_anthropic_params
func SamplingFromAnthropicParams(params map[string]any, key string) any {
	if extra, ok := params["extra_body"].(map[string]any); ok {
		if v, exists := extra[key]; exists {
			return v
		}
	}
	return params[key]
}

// SupportsMidConversationSystem 报告该模型是否接受 messages 内 system role。
// 只有部分模型支持（无 beta header）；网关会加前后缀
// （anthropic/、us.anthropic.、-v1:0），故按 family 子串匹配而非相等，
// 并保持保守：未识别 id 保持「折叠进顶层 system」的始终合法行为。
// Python: anthropic_model_client.py:350-362 _supports_mid_conversation_system
func SupportsMidConversationSystem(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return false
	}
	for _, family := range midConversationSystemModelFamilies {
		if strings.Contains(name, family) {
			return true
		}
	}
	return false
}

// ContentToBlocks 把 OJ content（string | list[str|dict]）归一为 Anthropic block 列表。
// Python: anthropic_model_client.py:289-314 _content_to_blocks
func ContentToBlocks(content any) []map[string]any {
	if content == nil {
		return nil
	}
	switch c := content.(type) {
	case string:
		if c == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": c}}
	case []any:
		blocks := make([]map[string]any, 0, len(c))
		for _, item := range c {
			switch v := item.(type) {
			case map[string]any:
				itemType, _ := v["type"].(string)
				var imageValue any
				switch itemType {
				case "image_url", "input_image":
					imageValue = v["image_url"]
				case "image":
					if _, hasSource := v["source"]; !hasSource {
						if dv, exists := v["data_url"]; exists {
							imageValue = dv
						} else {
							imageValue = v["image"]
						}
					}
				}
				if imageValue != nil || itemType == "image_url" || itemType == "input_image" {
					blocks = append(blocks, ImageSourceBlock(imageValue))
					continue
				}
				cp := make(map[string]any, len(v))
				for k, vv := range v {
					cp[k] = vv
				}
				blocks = append(blocks, cp)
			case string:
				if v != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": v})
				}
			default:
				blocks = append(blocks, map[string]any{"type": "text", "text": fmt.Sprintf("%v", item)})
			}
		}
		return blocks
	default:
		return []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", content)}}
	}
}

// ImageSourceBlock 把 OpenAI 风格图片值转换为 Anthropic image block。
// Python: anthropic_model_client.py:258-285 _image_source_block
func ImageSourceBlock(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		value = m["url"]
	}
	strValue, _ := value.(string)
	if strValue == "" {
		panic(errAnthropicImageInput())
	}
	if m := imageDataURLPattern.FindStringSubmatch(strValue); m != nil {
		return map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": strings.ToLower(m[1]), "data": m[2],
		}}
	}
	if strings.HasPrefix(strValue, "data:") {
		panic(errAnthropicImageUnsupported())
	}
	return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": strValue}}
}

// MarkCacheControl 给最后一个可缓存块挂 cache_control（5m 或 1h ephemeral）。
// Anthropic 缓存标记块之前的前缀；一条消息只需最后一个块打标。
// Python: anthropic_model_client.py:318-330 _mark_cache_control
func MarkCacheControl(blocks []map[string]any, ttl string) {
	marker := map[string]any{"type": "ephemeral"}
	if ttl == "1h" {
		marker["ttl"] = "1h"
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		blockType, _ := blocks[i]["type"].(string)
		if anthropicCacheableBlockTypes[blockType] {
			blocks[i]["cache_control"] = marker
			return
		}
	}
}

// ContentToText 把 message content 拍平为 system turn 允许的纯文本。
// 中位 system 消息是纯文本：图片/工具块无处安放；实践中不会出现，丢弃比
// 发送 API 拒绝的形状更好。
// Python: anthropic_model_client.py:333-347 _content_to_text
func ContentToText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts := make([]string, 0)
	for _, block := range ContentToBlocks(content) {
		if block["type"] == "text" {
			parts = append(parts, fmt.Sprintf("%v", block["text"]))
		}
	}
	return strings.Join(parts, "\n")
}

// ConvertToolSchemas 把 OpenAI tool schema 翻译为 Anthropic tool schema。
// Python: anthropic_model_client.py:525-540 _convert_tool_schemas
func ConvertToolSchemas(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]any)
		inputSchema, exists := tool["input_schema"]
		if !exists {
			if p, ok := fn["parameters"]; ok {
				inputSchema = p
			} else {
				inputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		}
		name, _ := tool["name"].(string)
		if name == "" {
			name, _ = fn["name"].(string)
		}
		desc, _ := tool["description"].(string)
		if desc == "" {
			desc, _ = fn["description"].(string)
		}
		out = append(out, map[string]any{
			"name": name, "description": desc, "input_schema": inputSchema,
		})
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errAnthropicImageInput 构造图片输入错误。
func errAnthropicImageInput() error {
	return fmt.Errorf("Anthropic image input requires a non-empty image URL.")
}

// errAnthropicImageUnsupported 构造不支持的 data URL 错误。
func errAnthropicImageUnsupported() error {
	return fmt.Errorf("Anthropic image inputs support base64 JPEG, PNG, GIF, or WebP data URLs only.")
}

// dictArg 安全取 dict 字段。
func dictArg(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// strArg 安全取 string 字段。
func strArg(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// anyArg 安全取 any 字段（nil 保持 nil）。
func anyArg(m map[string]any, key string) any {
	return m[key]
}

// mustJSONArgs 把 tool arguments 归一为对象或 _raw_arguments 兜底。
// Python: anthropic_model_client.py:511-518（args 解析失败 → {"_raw_arguments": ...}）
func mustJSONArgs(argsStr string) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsStr), &obj); err != nil {
		return map[string]any{"_raw_arguments": argsStr}
	}
	return obj
}
