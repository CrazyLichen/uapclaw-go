package openai

import (
	"reflect"
	"testing"
)

func TestOpenrouterModelProvider(t *testing.T) {
	cases := []struct{ in, want string }{
		{"anthropic/claude-4", "anthropic"},
		{"~anthropic/claude-4", "anthropic"}, // 剥 "~"
		{"Qwen/qwen-max", "qwen"},
		{"gpt-4", ""},               // 无 "/" → None
		{"", ""},
		{"openai/gpt/a", "openai"}, // 只取第一段
	}
	for _, c := range cases {
		if got := openrouterModelProvider(c.in); got != c.want {
			t.Errorf("openrouterModelProvider(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeOpenrouterProviderSet(t *testing.T) {
	def := map[string]struct{}{"anthropic": {}, "qwen": {}}
	if got := normalizeOpenrouterProviderSet(nil, def); !reflect.DeepEqual(got, def) {
		t.Errorf("nil 应回落默认: %v", got)
	}
	got := normalizeOpenrouterProviderSet(" Anthropic , QWEN ,", def)
	if len(got) != 2 {
		t.Errorf("逗号串解析: %v", got)
	}
	got = normalizeOpenrouterProviderSet([]any{"Anthropic", "qwen"}, def)
	if _, ok := got["anthropic"]; !ok {
		t.Errorf("列表解析: %v", got)
	}
	got = normalizeOpenrouterProviderSet([]any{}, def)
	if _, ok := got["qwen"]; !ok {
		t.Errorf("空集回落默认: %v", got)
	}
}

func TestWithoutCacheControl(t *testing.T) {
	// Python :183-199：剥除 + 两种还原分支
	if got := withoutCacheControl(map[string]any{
		"type": "text", "text": "hi", "cache_control": map[string]any{"type": "ephemeral"},
	}); !reflect.DeepEqual(got, "hi") {
		t.Errorf("单 text 块应还原为字符串: %#v", got)
	}
	if got := withoutCacheControl(map[string]any{
		"type": "tool_use", "id": "1", "cache_control": map[string]any{},
	}); !reflect.DeepEqual(got, map[string]any{"type": "tool_use", "id": "1"}) {
		t.Errorf("非 text 块保留其余键: %#v", got)
	}
	if got := withoutCacheControl([]any{
		map[string]any{"type": "text", "text": "a", "cache_control": map[string]any{}},
	}); !reflect.DeepEqual(got, []any{"a"}) {
		t.Errorf("列表递归: %#v", got)
	}
	// content 为单字符串 list 还原为字符串
	if got := withoutCacheControl(map[string]any{
		"type": "text", "content": []any{"solo"}, "cache_control": map[string]any{},
	}); !reflect.DeepEqual(got, map[string]any{"type": "text", "content": "solo"}) {
		t.Errorf("单字符串 content 还原: %#v", got)
	}
	// 无 cache_control：剥除后只剩 type+text 仍还原为字符串（Python :186-189 语义与
	// 是否有过 cache_control 无关）
	if got := withoutCacheControl(map[string]any{"type": "text", "text": "keep"}); got != "keep" {
		t.Errorf("单 text 块还原: %#v", got)
	}
	// 非 text 块无标记原样保留
	in := map[string]any{"type": "tool_use", "id": "9"}
	if got := withoutCacheControl(in); !reflect.DeepEqual(got, in) {
		t.Errorf("非 text 块原样: %#v", got)
	}
}

func TestContainsCacheControl(t *testing.T) {
	if !containsCacheControl(map[string]any{"cache_control": map[string]any{}}) {
		t.Error("顶层应命中")
	}
	if !containsCacheControl(map[string]any{"content": []any{map[string]any{"cache_control": map[string]any{}}}}) {
		t.Error("嵌套应命中")
	}
	if containsCacheControl(map[string]any{"type": "text"}) {
		t.Error("缺省不应命中")
	}
	if containsCacheControl("scalar") {
		t.Error("标量不应命中")
	}
}

func TestMarkMessageWithCacheControl(t *testing.T) {
	// 已含标记跳过（顶层嵌套检测）
	msg := map[string]any{"content": []any{map[string]any{"type": "text", "text": "x", "cache_control": map[string]any{}}}}
	markMessageWithCacheControl(msg, false)
	blk := msg["content"].([]any)[0].(map[string]any)
	if blk["text"] != "x" || len(blk) != 3 {
		t.Errorf("已含标记不应改动: %#v", blk)
	}
	// list 尾块 dict
	msg = map[string]any{"content": []any{map[string]any{"type": "text", "text": "a"}}}
	markMessageWithCacheControl(msg, false)
	blk = msg["content"].([]any)[0].(map[string]any)
	if _, ok := blk["cache_control"]; !ok {
		t.Errorf("尾块 dict 应打标记: %#v", blk)
	}
	// list 尾块字符串 → 包成 text 块
	msg = map[string]any{"content": []any{"plain"}}
	markMessageWithCacheControl(msg, false)
	blk = msg["content"].([]any)[0].(map[string]any)
	if blk["type"] != "text" || blk["text"] != "plain" {
		t.Errorf("字符串尾块应包装: %#v", blk)
	}
	// 纯字符串 content → 整体包装
	msg = map[string]any{"content": "hello"}
	markMessageWithCacheControl(msg, false)
	list, ok := msg["content"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("字符串 content 应转单元素 list: %#v", msg["content"])
	}
	if blk := list[0].(map[string]any); blk["text"] != "hello" || blk["type"] != "text" {
		t.Errorf("包装内容: %#v", blk)
	}
	// nil content → 空字符串 text 块
	msg = map[string]any{"content": nil}
	markMessageWithCacheControl(msg, false)
	if blk := msg["content"].([]any)[0].(map[string]any); blk["text"] != "" {
		t.Errorf("None content → 空串: %#v", blk)
	}
	// 空 list content → 不动
	msg = map[string]any{"content": []any{}}
	markMessageWithCacheControl(msg, false)
	if _, isList := msg["content"].([]any); !isList {
		t.Errorf("空 list 不应改写: %#v", msg["content"])
	}
	// enable1HTTL 透传
	msg = map[string]any{"content": "ttl-test"}
	markMessageWithCacheControl(msg, true)
	blk = msg["content"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)
	if blk["ttl"] != "1h" {
		t.Errorf("1h TTL 透传: %#v", blk)
	}
}

func TestLongestPrefixOverlapIndex(t *testing.T) {
	m1 := []any{map[string]any{"role": "user", "content": "a"}}
	m2 := []any{map[string]any{"role": "user", "content": "a"}, map[string]any{"role": "user", "content": "b"}}
	if got := longestPrefixOverlapIndex(m1, m2); got != 0 {
		t.Errorf("全等前缀应返回 overlap-1=0: %v", got)
	}
	m3 := []any{map[string]any{"role": "user", "content": "zzz"}}
	if got := longestPrefixOverlapIndex(m1, m3); got != -1 {
		t.Errorf("零重叠应返回 -1（None 的 Go 形态）: %v", got)
	}
	if got := longestPrefixOverlapIndex(nil, m2); got != -1 {
		t.Errorf("无 previous 应返回 -1: %v", got)
	}
	// cache_control 不参与比较（剥除后比较）
	mA := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "s", "cache_control": map[string]any{}}}}}
	mB := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "s"}}}}
	if got := longestPrefixOverlapIndex(mA, mB); got != 0 {
		t.Errorf("剥 cache_control 后应视为相等: %v", got)
	}
}

func TestApplyOpenrouterPromptCacheControl(t *testing.T) {
	params := map[string]any{
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "q"},
		},
	}
	applyOpenrouterPromptCacheControl(params, nil, false)
	tools := params["tools"].([]any)
	if _, ok := tools[len(tools)-1].(map[string]any)["cache_control"]; !ok {
		t.Error("tools 尾块应打标记")
	}
	msgs := params["messages"].([]any)
	// 标记打在 content 块内（字符串 content 被包成单 text 块）
	for i := 0; i < 2; i++ {
		m := msgs[i].(map[string]any)
		blk, isMap := m["content"].([]any)[0].(map[string]any)
		if !isMap {
			t.Fatalf("messages[%d] content 应被包成块列表: %#v", i, m["content"])
		}
		if _, ok := blk["cache_control"]; !ok {
			t.Errorf("messages[%d] 的 content 块应打标记", i)
		}
	}
	// 无 messages 不 panic
	applyOpenrouterPromptCacheControl(map[string]any{}, nil, false)
	// prefix 命中时三处消息都打标记
	prev := []any{map[string]any{"role": "system", "content": "sys"}}
	params = map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "new"},
	}}
	applyOpenrouterPromptCacheControl(params, prev, false)
	msgs = params["messages"].([]any)
	for i := 0; i < 2; i++ {
		m := msgs[i].(map[string]any)
		blk, isMap := m["content"].([]any)[0].(map[string]any)
		if !isMap {
			t.Fatalf("prefix 场景 messages[%d] content 应被包成块列表", i)
		}
		if _, ok := blk["cache_control"]; !ok {
			t.Errorf("prefix 场景 messages[%d] 应打标记", i)
		}
	}
}
