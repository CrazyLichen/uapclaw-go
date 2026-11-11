package anthropic

import (
	"fmt"
)

// TestConvertBlocks_块级转换 基础转换函数单测。
// Python: tests/unit_tests/core/foundation/llm/model_clients/test_anthropic_model_client.py 对拍。

import (
	"strings"
	"testing"
)

// TestDefaultMaxTokens_家族上限 对齐 anthropic_model_client.py:107-131。
func TestDefaultMaxTokens_家族上限(t *testing.T) {
	cases := map[string]int{
		"claude-3-5-sonnet":            8192,
		"anthropic/claude-3.5-haiku":   8192,
		"claude-3-opus-20240229":       4096,
		"claude-3-haiku-20240307-v1:0": 4096,
		"claude-sonnet-4-5":            32768,
		"":                             32768,
		"unknown-model":                32768,
	}
	for model, want := range cases {
		if got := DefaultMaxTokens(model); got != want {
			t.Fatalf("DefaultMaxTokens(%q) = %d, want %d", model, got, want)
		}
	}
}

// TestModelForbidsCustomSampling_五代家族 对齐 :246-256。
func TestModelForbidsCustomSampling_五代家族(t *testing.T) {
	cases := map[string]bool{
		"claude-fable-5":           true,
		"claude-mythos-5-x":        true,
		"claude-opus-5":            true,
		"claude-sonnet-5-20260101": true,
		"claude-opus-4-8":          true,
		"claude-opus-4-7-x":        true,
		"claude-sonnet-4-5":        false,
		"claude-3-5-sonnet":        false,
		"gpt-4":                    false,
	}
	for model, want := range cases {
		if got := ModelForbidsCustomSampling(model); got != want {
			t.Fatalf("ModelForbidsCustomSampling(%q) = %v, want %v", model, got, want)
		}
	}
}

// TestSanitizeReplayBlock_白名单 对齐 :145-176。
func TestSanitizeReplayBlock_白名单(t *testing.T) {
	kept := SanitizeReplayBlock(map[string]any{"type": "thinking", "thinking": "t", "signature": "s", "junk": 1})
	if kept["junk"] != nil || kept["signature"] != "s" || kept["thinking"] != "t" {
		t.Fatalf("thinking 白名单失效: %v", kept)
	}
	if SanitizeReplayBlock(map[string]any{"type": "redacted_thinking"}) != nil {
		t.Fatal("redacted_thinking 无 data 应丢弃")
	}
	if kept := SanitizeReplayBlock(map[string]any{"type": "redacted_thinking", "data": "opaque"}); kept == nil || kept["data"] != "opaque" {
		t.Fatalf("redacted_thinking 带 data 应保留: %v", kept)
	}
	if SanitizeReplayBlock(map[string]any{"type": "unknown"}) != nil {
		t.Fatal("未知类型应丢弃")
	}
	if kept := SanitizeReplayBlock(map[string]any{"type": "text", "text": "hi", "citations": []any{"c"}}); kept["citations"] == nil {
		t.Fatalf("text citations 应保留: %v", kept)
	}
}

// TestConvertToolChoice 映射 required/none/any/function。对齐 :221-236。
func TestConvertToolChoice(t *testing.T) {
	if ConvertToolChoice(nil) != nil || ConvertToolChoice("auto") != nil {
		t.Fatal("nil/auto → nil")
	}
	if got := ConvertToolChoice("required"); got["type"] != "any" {
		t.Fatalf("required → any 失效: %v", got)
	}
	if got := ConvertToolChoice("none"); got["type"] != "none" {
		t.Fatalf("none 映射失效: %v", got)
	}
	fn := ConvertToolChoice(map[string]any{"type": "function", "name": "f"})
	if fn == nil || fn["name"] != "f" {
		t.Fatalf("function 形态失效: %v", fn)
	}
	if ConvertToolChoice(map[string]any{"type": "other"}) != nil {
		t.Fatal("未知 dict 形态 → nil")
	}
}

// TestSupportsMidConversationSystem_族匹配 对齐 :350-362。
func TestSupportsMidConversationSystem_族匹配(t *testing.T) {
	cases := map[string]bool{
		"claude-opus-5":            true,
		"claude-opus-4-8-20260101": true,
		"anthropic/claude-fable-5": true,
		"claude-sonnet-4-5":        false,
		"":                         false,
		"unknown":                  false,
	}
	for model, want := range cases {
		if got := SupportsMidConversationSystem(model); got != want {
			t.Fatalf("SupportsMidConversationSystem(%q) = %v, want %v", model, got, want)
		}
	}
}

// TestConvertToolSchemas 对齐 :525-540。
func TestConvertToolSchemas(t *testing.T) {
	if ConvertToolSchemas(nil) != nil {
		t.Fatal("空 tools → nil")
	}
	got := ConvertToolSchemas([]map[string]any{
		{"type": "function", "function": map[string]any{
			"name": "f", "description": "d", "parameters": map[string]any{"type": "object"},
		}},
		{"name": "g", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	})
	if len(got) != 2 {
		t.Fatalf("应输出 2 条: %v", got)
	}
	if got[0]["name"] != "f" || got[0]["input_schema"].(map[string]any)["type"] != "object" {
		t.Fatalf("OpenAI 嵌套形态失效: %v", got[0])
	}
	if got[1]["name"] != "g" {
		t.Fatalf("扁平形态失效: %v", got[1])
	}
}

// TestMarkCacheControl_最后可缓存块 对齐 :318-330。
func TestMarkCacheControl_最后可缓存块(t *testing.T) {
	blocks := []map[string]any{
		{"type": "text", "text": "a"},
		{"type": "non_cacheable"},
		{"type": "text", "text": "b"},
	}
	MarkCacheControl(blocks, "5m")
	if blocks[2]["cache_control"] == nil {
		t.Fatal("应标在最后可缓存块")
	}
	if blocks[0]["cache_control"] != nil {
		t.Fatal("前面的块不应打标")
	}
	// 1h TTL
	blocks2 := []map[string]any{{"type": "tool_result", "content": "r"}}
	MarkCacheControl(blocks2, "1h")
	cc := blocks2[0]["cache_control"].(map[string]any)
	if cc["ttl"] != "1h" {
		t.Fatalf("1h TTL 失效: %v", cc)
	}
	// 全部不可缓存：无打标（不 panic）
	MarkCacheControl([]map[string]any{{"type": "nope"}}, "5m")
}

// TestContentToBlocks_多模态归一 对齐 :289-314。
func TestContentToBlocks_多模态归一(t *testing.T) {
	if ContentToBlocks(nil) != nil {
		t.Fatal("nil → nil")
	}
	if ContentToBlocks("") != nil {
		t.Fatal("空串 → nil")
	}
	if blocks := ContentToBlocks("hello"); len(blocks) != 1 || blocks[0]["text"] != "hello" {
		t.Fatalf("纯文本: %v", blocks)
	}
	// image_url → image block（url source）
	blocks := ContentToBlocks([]any{map[string]any{"type": "image_url", "image_url": "https://x/y.png"}})
	if blocks[0]["type"] != "image" {
		t.Fatalf("image_url 应转 image: %v", blocks)
	}
	// data URL → base64 source
	b64 := ContentToBlocks([]any{map[string]any{"type": "image", "data_url": "data:image/png;base64,AAA"}})
	src := b64[0]["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" {
		t.Fatalf("base64 形态失效: %v", src)
	}
	// 非 base64 的 data: 前缀 → panic（Python ValueError 上抛）
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "JPEG, PNG, GIF, or WebP") {
			t.Fatalf("非法 data URL 应报不支持: %v", r)
		}
	}()
	_ = ContentToBlocks([]any{map[string]any{"type": "image", "data_url": "data:text/html;base64,AAA"}})
}

// TestContentToText_拍平 对齐 :333-347。
func TestContentToText_拍平(t *testing.T) {
	if ContentToText("sys") != "sys" {
		t.Fatal("字符串原样")
	}
	got := ContentToText([]any{map[string]any{"type": "text", "text": "a"},
		map[string]any{"type": "image", "source": map[string]any{}},
		map[string]any{"type": "text", "text": "b"}})
	if got != "a\nb" {
		t.Fatalf("只保留 text 块: %q", got)
	}
}

// TestSamplingFromAnthropicParams_extraBody优先 对齐 :238-244。
func TestSamplingFromAnthropicParams_extraBody优先(t *testing.T) {
	params := map[string]any{"temperature": 0.5,
		"extra_body": map[string]any{"temperature": 0.9}}
	if SamplingFromAnthropicParams(params, "temperature") != 0.9 {
		t.Fatal("extra_body 优先")
	}
	if SamplingFromAnthropicParams(params, "missing") != nil {
		t.Fatal("缺失 → nil")
	}
}

// TestStreamBlocksMetadata_升序打包 对齐 :210-219。
func TestStreamBlocksMetadata_升序打包(t *testing.T) {
	if StreamBlocksMetadata(nil) != nil {
		t.Fatal("空累积 → nil")
	}
	meta := StreamBlocksMetadata(map[int]map[string]any{
		2: {"type": "thinking", "thinking": "b", "signature": ""},
		0: {"type": "text", "text": "a"},
	})
	blocks := meta[AnthropicContentBlocksMetadataKey].([]map[string]any)
	if len(blocks) != 2 || blocks[0]["type"] == "thinking" {
		t.Fatalf("应按 index 升序: %v", blocks)
	}
}

// TestCopyPreservedBlocks_长度不等不回填 对齐 :194-208。
func TestCopyPreservedBlocks_长度不等不回填(t *testing.T) {
	src := []map[string]any{{"metadata": map[string]any{AnthropicContentBlocksMetadataKey: []any{}}}}
	CopyPreservedBlocksToConvertedMessages(src, nil) // 不 panic
	dst := []map[string]any{{}}
	CopyPreservedBlocksToConvertedMessages(src, dst)
	// Python isinstance([], list) 为真：空列表同样回填（忠实对齐）
	if dst[0][anthropicInternalContentBlocksKey] == nil {
		t.Fatal("空块列表同样回填（对齐 Python isinstance 分支）")
	}
	// 正常回填
	src[0]["metadata"].(map[string]any)[AnthropicContentBlocksMetadataKey] = []any{
		map[string]any{"type": "thinking", "thinking": "t", "signature": "s"}}
	CopyPreservedBlocksToConvertedMessages(src, dst)
	if dst[0][anthropicInternalContentBlocksKey] == nil {
		t.Fatal("正常回填失败")
	}
}
