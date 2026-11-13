package provider

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

func testDep(t *testing.T, provider, apiBase string) *intelliroutercore.Deployment {
	t.Helper()
	d, err := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "sk-1", APIBase: apiBase, Provider: provider,
	})
	require.NoError(t, err)
	return d
}

// TestOpenAIAdapter_URLHeaders URL 幂等归一与请求头（openai_provider.py:22-46）。
func TestOpenAIAdapter_URLHeaders(t *testing.T) {
	a := NewOpenAIAdapter()
	require.Equal(t, "https://x/v1/chat/completions", a.GetAPIURL(testDep(t, "openai", "https://x/v1"), false))
	// 已带 /chat/completions 仍幂等（py:24-26）
	require.Equal(t, "https://x/v1/chat/completions", a.GetAPIURL(testDep(t, "openai", "https://x/v1/chat/completions"), false))
	h := a.GetHeaders(testDep(t, "openai", "https://x/v1"))
	require.Equal(t, "Bearer sk-1", h["Authorization"])
	require.Equal(t, "application/json", h["Content-Type"])

	// custom_headers 覆盖 Authorization（py:36-45）
	d := testDep(t, "openai", "https://x/v1")
	d.CustomHeaders = map[string]string{"Authorization": "Bearer custom"}
	h = a.GetHeaders(d)
	require.Equal(t, "Bearer custom", h["Authorization"])
}

// TestOpenAIAdapter_TransformRequest 透传合并（py:51-55）。
func TestOpenAIAdapter_TransformRequest(t *testing.T) {
	a := NewOpenAIAdapter()
	body := a.TransformRequest("m", []map[string]any{{"role": "user", "content": "hi"}},
		testDep(t, "openai", "https://x"), map[string]any{"temperature": 0.7})
	require.Equal(t, "m", body["model"])
	require.Equal(t, 0.7, body["temperature"])
}

// TestAnthropicAdapter_TransformRequestSystem 消息转换与 system 提取（py:40-115）。
func TestAnthropicAdapter_TransformRequestSystem(t *testing.T) {
	a := NewAnthropicAdapter()
	messages := []map[string]any{
		{"role": "system", "content": "sys prompt"},
		{"role": "user", "content": "hi"},
	}
	body := a.TransformRequest("claude-3", messages, testDep(t, "anthropic", "https://x/v1"), map[string]any{"max_tokens": 100})
	// system 提取为 blocks（py:108-110 + :271-282）
	sys := body["system"].([]map[string]any)
	require.Len(t, sys, 1)
	require.Equal(t, "sys prompt", sys[0]["text"])
	// max_tokens 必填（py:51）
	require.Equal(t, 100, body["max_tokens"])
	// 消息只剩非 system 且 content 为 block 列表（py:285 注释）
	msgs := body["messages"].([]map[string]any)
	require.Len(t, msgs, 1)
	require.Equal(t, "user", msgs[0]["role"])
	require.Equal(t, "https://x/v1/messages", a.GetAPIURL(testDep(t, "anthropic", "https://x/v1"), false))
	// 尾部 /v1/messages 剥离后重拼仍是 /v1/messages（py:23-26 幂等）
	require.Equal(t, "https://x/v1/messages", a.GetAPIURL(testDep(t, "anthropic", "https://x/v1/messages"), false))
	// 请求头（py:33-38）
	h := a.GetHeaders(testDep(t, "anthropic", "https://x"))
	require.Equal(t, "sk-1", h["x-api-key"])
	require.Equal(t, AnthropicVersion, h["anthropic-version"])
}

// TestAnthropicAdapter_ThinkingBudget thinking 预算富余与采样互斥（py:81-100）。
func TestAnthropicAdapter_ThinkingBudget(t *testing.T) {
	a := NewAnthropicAdapter()
	// max_tokens <= budget → max_tokens = budget + 1024（py:83-87）
	body := a.TransformRequest("c", nil, nil, map[string]any{
		"max_tokens": 4096,
		"thinking":   map[string]any{"type": "enabled", "budget_tokens": 8000},
	})
	require.Equal(t, 8000+ThinkingBudgetMargin, body["max_tokens"])
	// thinking enabled 时不注入 temperature（py:92-100）
	_, hasTemp := body["temperature"]
	require.False(t, hasTemp)
	// 非 thinking 模式注入 temperature（py:95-96）
	body2 := a.TransformRequest("c", nil, nil, map[string]any{"max_tokens": 100, "temperature": 0.5})
	require.Equal(t, 0.5, body2["temperature"])
}

// TestAnthropicAdapter_ToolsToolsChoice 工具与 tool_choice 转换（py:400-428）。
func TestAnthropicAdapter_ToolsToolsChoice(t *testing.T) {
	a := NewAnthropicAdapter()
	tools := []any{map[string]any{"function": map[string]any{"name": "f", "description": "d", "parameters": map[string]any{"type": "object"}}}}
	body := a.TransformRequest("c", nil, nil, map[string]any{
		"max_tokens": 100, "tools": tools, "tool_choice": "any",
	})
	// tools → input_schema（py:403-408）
	anthropicTools := body["tools"].([]any)
	require.Len(t, anthropicTools, 1)
	tt := anthropicTools[0].(map[string]any)
	require.Equal(t, "f", tt["name"])
	require.Equal(t, "object", tt["input_schema"].(map[string]any)["type"])
	// tool_choice 字符串映射（py:413-419）
	require.Equal(t, map[string]any{"type": "any"}, body["tool_choice"])
}

// TestAnthropicAdapter_TransformResponse 非流式响应转换（py:117-157）。
func TestAnthropicAdapter_TransformResponse(t *testing.T) {
	a := NewAnthropicAdapter()
	raw := map[string]any{
		"id": "msg_1", "stop_reason": "tool_use",
		"content": []any{
			map[string]any{"type": "text", "text": "hello "},
			map[string]any{"type": "thinking", "thinking": "reason"},
			map[string]any{"type": "tool_use", "id": "t1", "name": "f", "input": map[string]any{"x": 1}},
		},
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 3},
	}
	got := a.TransformResponse(raw, "c", nil)
	// content 拼接 / reasoning / tool_calls（py:122-141）
	require.Equal(t, "hello ", got["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"])
	msg := got["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	require.Equal(t, "reason", msg["reasoning_content"])
	tcs := msg["tool_calls"].([]any)
	require.Equal(t, "t1", tcs[0].(map[string]any)["id"])
	// tool_use → finish_reason=tool_calls（py:144 映射表）
	require.Equal(t, "tool_calls", got["choices"].([]any)[0].(map[string]any)["finish_reason"])
	// usage 归一（py:238-267）：prompt=10+20+3=33；cache_miss=uncached+write=13
	usage := got["usage"].(map[string]any)
	require.Equal(t, 33, usage["prompt_tokens"])
	require.Equal(t, 5, usage["completion_tokens"])
	require.Equal(t, 20, usage["cache_read_tokens"])
	require.Equal(t, 13, usage["cache_miss_tokens"])
	require.Equal(t, "observed", usage["cache_status"])
	require.Equal(t, true, usage["cache_authoritative"])
	// provider_content 保留原始响应（py:151）
	require.Equal(t, raw, got["provider_content"])
}

// TestAnthropicAdapter_StreamChunks 流事件转换逐分支（py:159-231）。
func TestAnthropicAdapter_StreamChunks(t *testing.T) {
	a := NewAnthropicAdapter()
	model := "c"
	// message_start（py:162-176）
	got := a.TransformStreamChunk(map[string]any{
		"type":    "message_start",
		"message": map[string]any{"id": "m1", "usage": map[string]any{"input_tokens": 7}},
	}, model, nil)
	require.Equal(t, "m1", got["id"])
	require.Equal(t, 7, got["usage"].(map[string]any)["prompt_tokens"])
	require.NotNil(t, got["provider_content"])

	// text_delta（py:182-186）
	got = a.TransformStreamChunk(map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": "hi"},
	}, model, nil)
	require.Equal(t, "hi", got["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["content"])

	// thinking_delta 空跳过（py:190-191）
	got = a.TransformStreamChunk(map[string]any{
		"type":  "content_block_delta",
		"delta": map[string]any{"type": "thinking_delta", "thinking": ""},
	}, model, nil)
	require.Nil(t, got)

	// signature_delta 跳过（py:195-196）
	got = a.TransformStreamChunk(map[string]any{
		"type": "content_block_delta", "delta": map[string]any{"type": "signature_delta", "signature": "x"},
	}, model, nil)
	require.Nil(t, got)

	// input_json_delta（py:197-204）
	got = a.TransformStreamChunk(map[string]any{
		"type": "content_block_delta", "index": 2,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": "{\"a\":"},
	}, model, nil)
	tc := got["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	require.Equal(t, 2, tc["index"])
	require.Equal(t, "{\"a\":", tc["function"].(map[string]any)["arguments"])

	// content_block_start tool_use（py:205-220）
	got = a.TransformStreamChunk(map[string]any{
		"type": "content_block_start", "index": 1,
		"content_block": map[string]any{"type": "tool_use", "id": "t1", "name": "f"},
	}, model, nil)
	tc = got["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	require.Equal(t, "t1", tc["id"])

	// message_delta（py:221-227）finish_reason 映射
	got = a.TransformStreamChunk(map[string]any{
		"type": "message_delta", "delta": map[string]any{"stop_reason": "max_tokens"},
		"usage": map[string]any{"output_tokens": 9},
	}, model, nil)
	require.Equal(t, "length", *got["choices"].([]any)[0].(map[string]any)["finish_reason"].(*string))

	// ping 跳过（py:228-229）
	require.Nil(t, a.TransformStreamChunk(map[string]any{"type": "ping"}, model, nil))
}

// TestAnthropicAdapter_ToolResultMerging 紧邻 tool 消息合并（py:301-311）。
func TestAnthropicAdapter_ToolResultMerging(t *testing.T) {
	a := NewAnthropicAdapter()
	messages := []map[string]any{
		{"role": "user", "content": "q"},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "t1", "function": map[string]any{"name": "f", "arguments": "{}"}}}},
		{"role": "tool", "tool_call_id": "t1", "content": "r1"},
		{"role": "tool", "tool_call_id": "t1", "content": "r2"},
		{"role": "user", "content": "thanks"},
	}
	body := a.TransformRequest("c", messages, nil, map[string]any{"max_tokens": 10})
	msgs := body["messages"].([]map[string]any)
	// 形态：user / assistant(tool_use) / user(tool_result+r1,r2 合并) / user(text)
	require.Len(t, msgs, 4)
	merged := msgs[2]["content"].([]any)
	require.Len(t, merged, 2) // 两条 tool_result 合并进同一条 user
	require.Equal(t, "tool_result", merged[0].(map[string]any)["type"])
	// 普通 user 文本不与 tool 结果混排（py:306-308）
	last := msgs[3]["content"].([]any)
	require.Equal(t, "text", last[0].(map[string]any)["type"])
}

// TestSanitizeToolCalls 清洗非标准字段（base_provider.py:128-153）。
func TestSanitizeToolCalls(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "q"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "t1", "type": "function", "index": 0,
				"function": map[string]any{"name": "f", "arguments": "{}", "extra": "x"}},
			"not-a-dict",
		}},
	}
	got := SanitizeToolCalls(messages)
	// assistant 消息被浅拷贝替换（py:150 {**msg, ...}）：原消息未清洗
	origCalls := messages[1]["tool_calls"].([]any)
	_, origHasIndex := origCalls[0].(map[string]any)["index"]
	require.True(t, origHasIndex) // 原始消息保持未清洗
	calls := got[1]["tool_calls"].([]any)
	require.Len(t, calls, 1) // 非 dict 项被剔除（py:149）
	c := calls[0].(map[string]any)
	require.Equal(t, "t1", c["id"])
	require.Equal(t, "function", c["type"])
	fn := c["function"].(map[string]any)
	require.Equal(t, "f", fn["name"])
	_, hasExtra := fn["extra"]
	require.False(t, hasExtra) // 非标准字段被清洗
	// user 消息原样保留引用（py:145-146）
	require.Equal(t, messages[0], got[0])
}

// TestIterStreamEvents_SSE SSE 解析与断流检测（base_provider.py:143-204）。
func TestIterStreamEvents_SSE(t *testing.T) {
	a := NewOpenAIAdapter()
	// 正常流：注释行 + data 行 + [DONE]
	body := strings.NewReader(": ping\ndata: {\"choices\":[{\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n")
	it, err := a.IterStreamEvents(body, "")
	require.NoError(t, err)
	chunks := 0
	for {
		chunk, ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		chunks++
		require.NotNil(t, chunk)
	}
	require.Equal(t, 2, chunks)

	// 裸 JSON 行也支持（py:177-178）
	it2, _ := a.IterStreamEvents(strings.NewReader("{\"type\":\"message_stop\"}\n"), "")
	_, ok, err := it2.Next()
	require.NoError(t, err)
	require.True(t, ok)

	// 断流：无 [DONE] 且无终结信号 → DeploymentNetworkError（py:197-204）
	// 首 chunk 正常产出，错误在流耗尽的下一次 Next 抛出（生成器语义）
	it3, _ := a.IterStreamEvents(strings.NewReader("data: {\"choices\":[{\"finish_reason\":null}]}\n"), "")
	_, ok, err = it3.Next()
	require.NoError(t, err)
	require.True(t, ok)
	_, _, err = it3.Next()
	require.Error(t, err)
	require.True(t, intellirouter.IsDeploymentError(err))
	// 错误消息逐字（py:201-203）
	require.Contains(t, err.Error(), "stream ended without [DONE] sentinel or finish signal")
}

// TestChunkHasFinishSignal 四形态判定（base_provider.py:108-133）。
func TestChunkHasFinishSignal(t *testing.T) {
	require.True(t, ChunkHasFinishSignal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop"}}}))
	require.True(t, ChunkHasFinishSignal(map[string]any{"type": "message_stop"}))
	require.True(t, ChunkHasFinishSignal(map[string]any{"delta": map[string]any{"stop_reason": "end_turn"}}))
	require.True(t, ChunkHasFinishSignal(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP"}}}))
	require.True(t, ChunkHasFinishSignal(map[string]any{"stopReason": "end_turn"}))
	require.False(t, ChunkHasFinishSignal(map[string]any{"choices": []any{map[string]any{}}}))
	require.False(t, ChunkHasFinishSignal(nil))
}

// TestRegistry 内置注册表（registry.py:28-44）。
func TestRegistry(t *testing.T) {
	for _, name := range []string{
		"openai", "anthropic", "dashscope", "zhipu",
		"deepseek", "siliconflow", "inference-affinity",
	} {
		adapter, err := GetProviderAdapter(name)
		require.NoError(t, err, name)
		require.NotNil(t, adapter)
	}
	// 未知 provider 报错，消息对齐 py:18-21
	_, err := GetProviderAdapter("bogus")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unknown provider: 'bogus'")
	// bedrock/gemini 未注册（D-2）
	_, err = GetProviderAdapter("aws-bedrock")
	require.Error(t, err)
}

// TestMediaHelpers data URI 工具（utils/media.py）。
func TestMediaHelpers(t *testing.T) {
	require.True(t, intellirouter.IsDataURI("data:image/png;base64,AA"))
	require.False(t, intellirouter.IsDataURI("https://x"))
	mime, data, err := intellirouter.ParseDataURI("data:image/png;base64,iVBOR")
	require.NoError(t, err)
	require.Equal(t, "image/png", mime)
	require.Equal(t, "iVBOR", data)
	// 无 mime → application/octet-stream（py:35）
	mime, _, err = intellirouter.ParseDataURI("data:;base64,AA")
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", mime)
	// 非 data URI 报错（py:21）
	_, _, err = intellirouter.ParseDataURI("https://x")
	require.ErrorContains(t, err, "Not a data URI")
	// 无逗号报错（py:29）
	_, _, err = intellirouter.ParseDataURI("data:image/png;base64")
	require.ErrorContains(t, err, "no comma")
	_ = io.Discard
	_ = json.Marshal
}
