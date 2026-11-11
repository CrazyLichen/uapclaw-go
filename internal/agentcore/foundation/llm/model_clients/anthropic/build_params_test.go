package anthropic

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// mustClient 构造最小可用客户端（参数构建测试共用）。
func mustClient(t *testing.T, model string) *AnthropicModelClient {
	t.Helper()
	c, err := NewAnthropicModelClient(
		&llmschema.ModelRequestConfig{ModelName: model},
		&llmschema.ModelClientConfig{ClientID: "c1", ClientProvider: "Anthropic",
			APIKey: "k", APIBase: "https://api.anthropic.com", Timeout: 60},
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestBuildAnthropicParams_基础载荷 model/messages/max_tokens 必备 + system 顶层。
func TestBuildAnthropicParams_基础载荷(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("be nice"),
		llmschema.NewUserMessage("hi"),
	}
	dicts, err := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	if err != nil {
		t.Fatal(err)
	}
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, model_clients.NewInvokeParams(), false)
	if err != nil {
		t.Fatal(err)
	}
	if params["model"] != "claude-sonnet-4-5" {
		t.Fatalf("model = %v", params["model"])
	}
	// 未设 max_tokens → 默认（claude-sonnet-4-5 非 3 族 → 32768）
	if params["max_tokens"] != 32768 {
		t.Fatalf("max_tokens = %v, want 32768", params["max_tokens"])
	}
	if params["system"] == nil {
		t.Fatal("system 应提到顶层")
	}
	msgs := params["messages"].([]map[string]any)
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("system 应从 messages 剥离: %v", msgs)
	}
}

// TestBuildAnthropicParams_maxTokens_家族上限 未设值时按家族兜底。
func TestBuildAnthropicParams_maxTokens_家族上限(t *testing.T) {
	c := mustClient(t, "claude-3-5-sonnet")
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, model_clients.NewInvokeParams(), false)
	if err != nil {
		t.Fatal(err)
	}
	if params["max_tokens"] != 8192 {
		t.Fatalf("claude-3-5 → 8192, got %v", params["max_tokens"])
	}
}

// TestBuildAnthropicParams_reasoning接线 budget_tokens → thinking.enabled + max_tokens 抬升。
func TestBuildAnthropicParams_reasoning接线(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	p := model_clients.NewInvokeParams()
	p.Extra = map[string]any{"reasoning": map[string]any{"mode": "enabled", "budget_tokens": 5000}}
	p.MaxTokens = func() *int { v := 2000; return &v }()
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, p, false)
	if err != nil {
		t.Fatal(err)
	}
	thinking, ok := params["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking 不符: %v", params["thinking"])
	}
	if thinking["budget_tokens"] != 5000 {
		t.Fatalf("budget_tokens = %v", thinking["budget_tokens"])
	}
	// max_tokens 2000 <= budget 5000 → 抬升 6024
	if params["max_tokens"] != 6024 {
		t.Fatalf("max_tokens 应抬到 6024, got %v", params["max_tokens"])
	}
}

// TestBuildAnthropicParams_采样互斥 温度+top_p 弃 top_p；禁采样模型全丢；top_p=1.0 跳过。
func TestBuildAnthropicParams_采样互斥(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))

	// temperature 显式 + top_p 显式 → extra_body 只有 temperature
	p := model_clients.NewInvokeParams(
		model_clients.WithInvokeTemperature(0.5),
		model_clients.WithInvokeTopP(0.9),
	)
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, p, false)
	if err != nil {
		t.Fatal(err)
	}
	eb := params["extra_body"].(map[string]any)
	if _, hasT := eb["temperature"]; !hasT {
		t.Fatalf("temperature 应进 extra_body: %v", eb)
	}
	if _, hasP := eb["top_p"]; hasP {
		t.Fatalf("设温度时 top_p 应被丢弃: %v", eb)
	}
	if params["temperature"] != nil || params["top_p"] != nil {
		t.Fatalf("采样键不应留在顶层: %v", params)
	}

	// 禁采样模型（claude-opus-5）→ 全丢
	c2 := mustClient(t, "claude-opus-5")
	p2 := model_clients.NewInvokeParams(model_clients.WithInvokeTemperature(0.5))
	params2, err := c2.BuildAnthropicParams(context.Background(), dicts, src, p2, false)
	if err != nil {
		t.Fatal(err)
	}
	if eb2, ok := params2["extra_body"].(map[string]any); ok {
		if _, has := eb2["temperature"]; has {
			t.Fatalf("禁采样模型不应下发 temperature: %v", eb2)
		}
	}

	// top_p=1.0（默认）→ 跳过
	p3 := model_clients.NewInvokeParams(model_clients.WithInvokeTopP(1.0))
	params3, _ := c.BuildAnthropicParams(context.Background(), dicts, src, p3, false)
	if eb3, ok := params3["extra_body"].(map[string]any); ok {
		if _, has := eb3["top_p"]; has {
			t.Fatalf("top_p=1.0 应跳过: %v", eb3)
		}
	}
}

// TestBuildAnthropicParams_extraBody四键 reasoning_effort 等移入 extra_body。
func TestBuildAnthropicParams_extraBody四键(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	p := model_clients.NewInvokeParams()
	p.Extra = map[string]any{"reasoning_effort": "high"}
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, p, false)
	if err != nil {
		t.Fatal(err)
	}
	eb := params["extra_body"].(map[string]any)
	if eb["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort 应进 extra_body: %v", eb)
	}
}

// TestBuildAnthropicParams_stopSequences stop 字符串/列表 → stop_sequences。
func TestBuildAnthropicParams_stopSequences(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	p := model_clients.NewInvokeParams()
	p.Stop = func() *string { s := "END"; return &s }()
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, p, false)
	if err != nil {
		t.Fatal(err)
	}
	seqs, ok := params["stop_sequences"].([]string)
	if !ok || len(seqs) != 1 || seqs[0] != "END" {
		t.Fatalf("stop_sequences = %v", params["stop_sequences"])
	}
}

// TestBuildAnthropicParams_缓存断点 tools 尾 + system + 最后稳定消息三断点。
func TestBuildAnthropicParams_缓存断点(t *testing.T) {
	c := mustClient(t, "claude-sonnet-4-5")
	src := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("sys"),
		llmschema.NewUserMessage("q"),
		llmschema.NewUserMessage("transient-tail",
			llmschema.WithMetadata(map[string]any{"transient": true})),
	}
	dicts, _ := c.ConvertMessagesToDict(model_clients.NewMessagesParam(src...))
	p := model_clients.NewInvokeParams()
	p.Tools = []commonschema.ToolInfoInterface{commonschema.NewToolInfo("f", "desc",
		map[string]any{"type": "object", "properties": map[string]any{}})}
	params, err := c.BuildAnthropicParams(context.Background(), dicts, src, p, false)
	if err != nil {
		t.Fatal(err)
	}
	// system 块挂 5m
	system := params["system"].([]map[string]any)
	if system[len(system)-1]["cache_control"] == nil {
		t.Fatalf("system 尾应挂 cache_control: %v", system)
	}
	// tools 尾挂 ephemeral
	tools := params["tools"].([]map[string]any)
	if tools[len(tools)-1]["cache_control"] == nil {
		t.Fatalf("tools 尾应挂 ephemeral: %v", tools)
	}
	// 尾消息 transient → 断点锚在前一条稳定消息
	msgs := params["messages"].([]map[string]any)
	stable := msgs[len(msgs)-2]["content"].([]map[string]any)
	if stable[len(stable)-1]["cache_control"] == nil {
		t.Fatalf("稳定消息应挂 5m 断点: %v", msgs)
	}
	// transient 尾不打标
	tail := msgs[len(msgs)-1]["content"].([]map[string]any)
	if tail[len(tail)-1]["cache_control"] != nil {
		t.Fatalf("transient 尾不应打标: %v", msgs)
	}
}
