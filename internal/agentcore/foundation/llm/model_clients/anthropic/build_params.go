package anthropic

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// BuildAnthropicParams 构建 Anthropic Messages API 请求参数。
// Python: anthropic_model_client.py:823-1001 _build_anthropic_params。
// 步骤顺序与 Python 一一对应，禁止重排：
//
//  1. 剥 reasoning 到 reasoning_kwargs（显式参，不进透传）
//  2. 基础 OpenAI 形状参数（BaseClientEmbed.BuildRequestParams）
//  3. apply_reasoning_plan(resolve_reasoning_plan(...), override=is_reasoning_config_intent)
//  4. _copy_preserved_blocks_to_converted_messages（replay 块按位置回填）
//  5. _convert_message_schemas（allow_mid_conversation_system 按模型族判定）
//  6. _convert_tool_schemas
//  7. _apply_static_cache_breakpoints + _apply_messages_cache_breakpoint(exclude_tail=transient)
//  8. max_tokens 兜底 default_max_tokens
//  9. 组装顶层 params；Anthropic 原生四键透传（thinking/output_config/metadata/service_tier）
//  10. budget 抬升：max_tokens <= budget_tokens → max_tokens = budget + 1024（debug 日志）
//  11. 兼容网关私有键进 extra_body（reasoning_effort/thinking_budget/thinking_strategy/enable_thinking）
//  12. tool_choice 转换
//  13. 采样互斥：禁采样模型或 thinking enabled/adaptive 时丢弃采样覆盖；
//     temperature 与 top_p 互斥（设温度弃 top_p）；top_p=1.0 为默认跳过；top_k 透传
//  14. stop → stop_sequences；reasoning_request_controls 日志
func (c *AnthropicModelClient) BuildAnthropicParams(
	ctx context.Context,
	messagesDict []map[string]any,
	sourceMessages []llmschema.BaseMessage,
	params *model_clients.InvokeParams,
	stream bool,
) (map[string]any, error) {
	// 步骤 2：基础 OpenAI 形状参数（Python super()._build_request_params）
	openaiParams, err := c.BuildRequestParams(ctx, messagesDict, params, stream)
	if err != nil {
		return nil, err
	}

	// 步骤 3：reasoning 接线。
	// Python: resolve_reasoning_plan(self.model_client_config, self.model_config,
	//   request_model=model, explicit_kwargs=reasoning_kwargs) + apply_reasoning_plan
	// Go：显式 reasoning 从 params.Extra 取（ReasoningIntent 承载"是否显式传入"）。
	var explicitValue any
	if v, exists := params.Extra["reasoning"]; exists {
		explicitValue = v
	}
	explicitIntent := llmschema.ReasoningIntent{Set: explicitValue != nil, Value: explicitValue}
	plan := reasoning.ResolveReasoningPlan(c.ClientConfig, c.ModelConfig, strArg(openaiParams, "model"), explicitIntent)
	reasoning.ApplyReasoningPlan(openaiParams, plan, llmschema.IsReasoningConfigIntent(explicitValue))

	// 步骤 4：replay 块回填（Python _copy_preserved_blocks_to_converted_messages；
	// 前提：sourceMessages 与 messagesDict 位置一一对应，长度不等跳过）。
	// Go：源消息是 BaseMessage（metadata 在结构体上），先提取为 dict 形再 zip 回填。
	if len(sourceMessages) == len(messagesDict) {
		sourceDicts := make([]map[string]any, len(sourceMessages))
		for i, msg := range sourceMessages {
			d := map[string]any{}
			if meta := msg.GetMetadata(); len(meta) > 0 {
				d["metadata"] = meta
			}
			sourceDicts[i] = d
		}
		CopyPreservedBlocksToConvertedMessages(sourceDicts, messagesDict)
	}

	// 步骤 5：消息转换（含 mid-conversation system 分区）
	oaiMessages, _ := openaiParams["messages"].([]map[string]any)
	systemBlocks, anthropicMessages := ConvertMessageSchemas(
		oaiMessages, SupportsMidConversationSystem(strArg(openaiParams, "model")))

	// 步骤 6：工具转换（params.Tools 是 ToolInfoInterface，先走通用 dict 转换）
	var toolsDict []map[string]any
	if len(params.Tools) > 0 {
		toolsDict = c.ConvertToolsToDict(params.Tools)
	}
	anthropicTools := ConvertToolSchemas(toolsDict)

	// 步骤 7：缓存断点
	ApplyStaticCacheBreakpoints(systemBlocks, anthropicTools)
	excludeTail := false
	if len(sourceMessages) > 0 {
		last := sourceMessages[len(sourceMessages)-1]
		if meta := last.GetMetadata(); meta != nil {
			if v, ok := meta["transient"]; ok {
				switch t := v.(type) {
				case bool:
					excludeTail = t
				default:
					excludeTail = t != nil
				}
			}
		}
	}
	ApplyMessagesCacheBreakpoint(anthropicMessages, excludeTail)

	// 步骤 8：Anthropic 必须带 max_tokens；显式值优先，否则守住老家族输出上限
	effectiveMaxTokens := intOf(openaiParams["max_tokens"])
	if effectiveMaxTokens <= 0 {
		effectiveMaxTokens = DefaultMaxTokens(strArg(openaiParams, "model"))
	}

	// 步骤 9：组装顶层参数
	result := map[string]any{
		"model":      openaiParams["model"],
		"messages":   anthropicMessages,
		"max_tokens": effectiveMaxTokens,
	}
	if len(systemBlocks) > 0 {
		result["system"] = systemBlocks
	}
	if len(anthropicTools) > 0 {
		result["tools"] = anthropicTools
	}
	// Anthropic 原生控制键：通用 OpenAI 形状构建器视其为 extras，此处透传。
	// 采样键（temperature/top_p/top_k）不在此列：anthropic>=1.0 已从
	// messages.create/stream 移除，只能走 extra_body。
	for _, key := range []string{"thinking", "output_config", "metadata", "service_tier"} {
		if v, exists := openaiParams[key]; exists {
			result[key] = v
		}
	}

	// 步骤 10：thinking.budget_tokens 与最终回答共享输出上限；官方与兼容网关都要求
	// max_tokens > budget_tokens——抬升上限而不是静默压缩思考深度。
	if thinkingCfg, ok := result["thinking"].(map[string]any); ok {
		if budget := intOf(thinkingCfg["budget_tokens"]); budget > 0 && effectiveMaxTokens <= budget {
			result["max_tokens"] = budget + 1024
		}
	}

	// 步骤 11：兼容网关可能加 SDK 关键字参数之外的字段——经 extra_body 合并进 JSON
	// 而不是在 HTTP 请求前报 TypeError。
	extraBody := make(map[string]any)
	if src, ok := openaiParams["extra_body"].(map[string]any); ok {
		for k, v := range src {
			extraBody[k] = v
		}
	}
	for _, key := range []string{"reasoning_effort", "thinking_budget", "thinking_strategy", "enable_thinking"} {
		if v, exists := openaiParams[key]; exists {
			extraBody[key] = v
		}
	}

	// 步骤 12：tool_choice
	if tc := ConvertToolChoice(openaiParams["tool_choice"]); tc != nil {
		result["tool_choice"] = tc
	}

	// 步骤 13：采样互斥。
	// ModelRequestConfig 携带 OpenAI 向默认值；对 Anthropic 视为"未设置"，
	// 除非调用方显式配置——现行 Claude 家族对非默认采样回 400。
	// Python 用 model_fields_set 判显式性；Go 近似：请求参非 nil 即显式，
	// model_config 默认值不触发（偏差见 spec D-3 同源，方向保守）。
	temperatureExplicit := params.Temperature != nil
	topPExplicit := params.TopP != nil
	temperature := openaiParams["temperature"]
	topP := openaiParams["top_p"]
	thinkingType := ""
	if thinkingCfg, ok := result["thinking"].(map[string]any); ok {
		thinkingType, _ = thinkingCfg["type"].(string)
	}
	samplingForbidden := ModelForbidsCustomSampling(strArg(result, "model"))
	thinkingRestrictsSampling := thinkingType == "enabled" || thinkingType == "adaptive"
	samplingAllowed := !samplingForbidden && !thinkingRestrictsSampling

	if !samplingAllowed {
		if temperatureExplicit || topPExplicit || openaiParams["top_k"] != nil {
			// Python: llm_logger.debug("Anthropic: dropping sampling overrides ...")
		}
	} else {
		if temperature != nil {
			if temperatureExplicit {
				extraBody["temperature"] = temperature
				if topPExplicit && topP != nil {
					// Python: dropping top_p because temperature is set (API forbids both)
				}
			}
		} else if topP != nil && topPExplicit {
			if floatOf(topP) != 1.0 {
				// top_p=1.0 是默认（无 nucleus 截断）；跳过使请求只含有意义的覆盖
				extraBody["top_p"] = topP
			}
		}
		if topK := openaiParams["top_k"]; topK != nil {
			extraBody["top_k"] = topK
		}
	}
	if len(extraBody) > 0 {
		result["extra_body"] = extraBody
	}
	// Python: 采样键一律从顶层移除（extra_body 是唯一通道）
	for _, key := range []string{"temperature", "top_p", "top_k"} {
		delete(result, key)
	}

	// 步骤 14：stop → stop_sequences
	if stop := openaiParams["stop"]; stop != nil {
		if s, isStr := stop.(string); isStr {
			result["stop_sequences"] = []string{s}
		} else if list, isList := stop.([]any); isList {
			seqs := make([]string, 0, len(list))
			for _, item := range list {
				seqs = append(seqs, fmt.Sprintf("%v", item))
			}
			result["stop_sequences"] = seqs
		} else if list, isList := stop.([]string); isList {
			result["stop_sequences"] = list
		}
	}
	// NOTE: 不设顶层 cache_control。自动缓存锚定在最后一块（瞬态运行时预算消息），
	// 会打断增量历史缓存。上面的显式消息断点锚在最后一条稳定消息上。

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// intOf 把请求参数中的数值安全转为 int（支持 int/int64/float64；缺失或非法返回 0）。
func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// floatOf 把请求参数中的数值安全转为 float64（缺失或非法返回 0）。
func floatOf(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}
