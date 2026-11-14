package openai

import (
	"reflect"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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
	// D2 对齐 Python：显式配置空串/空列表 → 空集（功能关死），不回落默认
	// Python: :161-166 value=""→[""]→过滤后空 frozenset 原样返回
	if got := normalizeOpenrouterProviderSet("", def); len(got) != 0 {
		t.Errorf("空串应返回空集: %v", got)
	}
	if got := normalizeOpenrouterProviderSet([]any{}, def); len(got) != 0 {
		t.Errorf("空列表应返回空集: %v", got)
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
	// D3 对齐 Python :186-189：仅 {type:text}（无 text 键）→ 空串
	if got := withoutCacheControl(map[string]any{
		"type": "text", "cache_control": map[string]any{},
	}); got != "" {
		t.Errorf("无 text 键的单 text 块应还原空串: %#v", got)
	}
	// D3：text 值非字符串也原样取出（Python get 语义）
	if got := withoutCacheControl(map[string]any{
		"type": "text", "text": 42, "cache_control": map[string]any{},
	}); !reflect.DeepEqual(got, 42) {
		t.Errorf("text 非字符串应原样还原: %#v", got)
	}
	// D3：两键但不是 {type,text} 组合（{type,foo}）不还原
	if got := withoutCacheControl(map[string]any{
		"type": "text", "foo": "bar", "cache_control": map[string]any{},
	}); !reflect.DeepEqual(got, map[string]any{"type": "text", "foo": "bar"}) {
		t.Errorf("{type,foo} 不应还原: %#v", got)
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

func TestParseOpenrouterCacheConfigExtra(t *testing.T) {
	// 全默认
	cfg := parseOpenrouterCacheConfigExtra(nil)
	if !cfg.enableExplicitCaching || !cfg.enablePrefixMatching || cfg.enable1HTTL {
		t.Errorf("默认值: %+v", cfg)
	}
	if _, ok := cfg.explicitProviders["qwen"]; !ok {
		t.Errorf("explicit 默认含 qwen: %v", cfg.explicitProviders)
	}
	if _, ok := cfg.ttl1hProviders["anthropic"]; !ok {
		t.Errorf("ttl1h 默认仅 anthropic: %v", cfg.ttl1hProviders)
	}
	// 字符串布尔 + 逗号串 provider
	cfg = parseOpenrouterCacheConfigExtra(map[string]any{
		"openrouter_enable_explicit_prompt_caching":  "false",
		"openrouter_enable_1h_prompt_cache_ttl":      true,
		"openrouter_explicit_prompt_cache_providers": " Anthropic , Qwen ",
		"openrouter_prompt_cache_1h_ttl_providers":   []any{"anthropic", "qwen"},
	})
	if cfg.enableExplicitCaching || !cfg.enable1HTTL || !cfg.enablePrefixMatching {
		t.Errorf("字符串布尔: %+v", cfg)
	}
	if _, ok := cfg.explicitProviders["qwen"]; !ok {
		t.Errorf("逗号串 provider: %v", cfg.explicitProviders)
	}
	if _, ok := cfg.ttl1hProviders["qwen"]; !ok {
		t.Errorf("列表 provider: %v", cfg.ttl1hProviders)
	}
	// 未知字符串布尔键回落默认
	cfg = parseOpenrouterCacheConfigExtra(map[string]any{
		"openrouter_enable_explicit_prompt_caching": "maybe",
	})
	if !cfg.enableExplicitCaching {
		t.Errorf("未知布尔值回落默认: %+v", cfg)
	}
}

func TestNewOpenAIModelClient_OpenrouterCacheDefaults(t *testing.T) {
	c, err := NewOpenAIModelClient(
		&llmschema.ModelRequestConfig{ModelName: "anthropic/claude-4"},
		&llmschema.ModelClientConfig{ClientProvider: "OpenRouter", APIBase: "https://openrouter.ai/api/v1", APIKey: "k"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !c.enableOpenrouterExplicitCaching || !c.enableOpenrouterPromptCachePrefixMatching {
		t.Error("默认应开启显式缓存与前缀匹配")
	}
	if c.enableOpenrouter1HPromptCacheTTL {
		t.Error("1h TTL 默认应关闭")
	}
	if c.previousOpenrouterPromptCacheMessages != nil {
		t.Error("初始 previous 应为 nil")
	}
	if _, ok := c.openrouterCacheProviders["anthropic"]; !ok {
		t.Errorf("构造期应解析 provider 集合: %v", c.openrouterCacheProviders)
	}
}

func newProfileTestClient(t *testing.T, extra map[string]any) *OpenAIModelClient {
	t.Helper()
	c, err := NewOpenAIModelClient(
		&llmschema.ModelRequestConfig{ModelName: "anthropic/claude-4"},
		&llmschema.ModelClientConfig{ClientProvider: "OpenRouter", APIBase: "https://openrouter.ai/api/v1",
			APIKey: "k", Extra: extra},
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func baseProfileMsgs() []any {
	return []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "q"},
	}
}

func TestApplyOpenrouterProfile_矩阵(t *testing.T) {
	prev := baseProfileMsgs()
	// ① 非 openrouter profile：不打标记、不重置 previous
	c := newProfileTestClient(t, nil)
	c.ClientConfig.EndpointProfile = ""
	c.previousOpenrouterPromptCacheMessages = prev
	params := map[string]any{"messages": baseProfileMsgs()}
	c.applyOpenrouterProfile(params, "gpt-4")
	if containsCacheControl(params) {
		t.Error("非 openrouter 不应打标记")
	}
	if c.previousOpenrouterPromptCacheMessages == nil {
		t.Error("非 openrouter 不应重置 previous（Python :1088-1090 无重置）")
	}
	// ② 开关关：重置 previous、不打标记
	c = newProfileTestClient(t, map[string]any{"openrouter_enable_explicit_prompt_caching": false})
	c.ClientConfig.EndpointProfile = "openrouter"
	c.previousOpenrouterPromptCacheMessages = prev
	params = map[string]any{"messages": baseProfileMsgs()}
	c.applyOpenrouterProfile(params, "anthropic/claude-4")
	if c.previousOpenrouterPromptCacheMessages != nil {
		t.Error("开关关应重置 previous")
	}
	// ③ 不支持的 provider：不打标记 + 重置 previous
	c = newProfileTestClient(t, nil)
	c.ClientConfig.EndpointProfile = "openrouter"
	c.previousOpenrouterPromptCacheMessages = prev
	params = map[string]any{"messages": baseProfileMsgs()}
	c.applyOpenrouterProfile(params, "openai/gpt-4")
	if containsCacheControl(params) {
		t.Error("不支持的 provider 不应打标记")
	}
	if c.previousOpenrouterPromptCacheMessages != nil {
		t.Error("不支持应重置 previous")
	}
	// ④ 正常路径：打标记 + previous 保存本轮深拷贝 + 入参不被污染
	c = newProfileTestClient(t, nil)
	c.ClientConfig.EndpointProfile = "openrouter"
	c.previousOpenrouterPromptCacheMessages = prev
	params = map[string]any{
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
		"messages": baseProfileMsgs(),
	}
	c.applyOpenrouterProfile(params, "anthropic/claude-4")
	if _, ok := params["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["cache_control"]; !ok {
		t.Error("正常路径应打标记")
	}
	params["messages"].([]any)[0].(map[string]any)["content"] = "MUTATED"
	if c.previousOpenrouterPromptCacheMessages[0].(map[string]any)["content"] == "MUTATED" {
		t.Error("previous 应是深拷贝，不受后续污染")
	}
	// previous 深拷贝保存于打标记之前（Python :1117 deepcopy 先于 :1119-1121 改写），
	// content 仍是原始字符串形态且无 cache_control
	prevContent := c.previousOpenrouterPromptCacheMessages[0].(map[string]any)["content"]
	if prevContent != "sys" || containsCacheControl(c.previousOpenrouterPromptCacheMessages[0]) {
		t.Errorf("previous 深拷贝应保存未打标记的原始形态: %#v", prevContent)
	}
	// ⑤ 1h TTL：anthropic 支持 → ttl 出现
	c = newProfileTestClient(t, map[string]any{"openrouter_enable_1h_prompt_cache_ttl": true})
	c.ClientConfig.EndpointProfile = "openrouter"
	params = map[string]any{"messages": baseProfileMsgs()}
	c.applyOpenrouterProfile(params, "anthropic/claude-4")
	blk := params["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)
	if blk["ttl"] != "1h" {
		t.Errorf("anthropic 应带 ttl=1h: %v", blk)
	}
	// ⑥ qwen + 1h 开关：不支持 → 无 ttl
	params = map[string]any{"messages": []any{map[string]any{"role": "user", "content": "q"}}}
	c.applyOpenrouterProfile(params, "qwen/qwen-max")
	blk = params["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)
	if _, has := blk["ttl"]; has {
		t.Errorf("qwen 不应带 ttl: %v", blk)
	}
	// ⑦ prefix matching 关：previous 重置、不打 prefix 位
	c = newProfileTestClient(t, map[string]any{"openrouter_enable_prompt_cache_prefix_matching": false})
	c.ClientConfig.EndpointProfile = "openrouter"
	c.previousOpenrouterPromptCacheMessages = prev
	params = map[string]any{"messages": baseProfileMsgs()}
	c.applyOpenrouterProfile(params, "anthropic/claude-4")
	if c.previousOpenrouterPromptCacheMessages != nil {
		t.Error("prefix matching 关应重置 previous（Python :1121-1122）")
	}
}

// TestDeepCopyValue_保数字类型 D1 回归：JSON 往返会把 int 变 float64，
// 破坏 longestPrefixOverlapIndex 的 DeepEqual（多轮对话前缀匹配失效）。
func TestDeepCopyValue_保数字类型(t *testing.T) {
	in := []any{map[string]any{
		"role": "assistant",
		"tool_calls": []any{map[string]any{"id": "1", "index": 0, "weight": 1.5}},
	}}
	out := deepCopyValue(in).([]any)
	blk := out[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if _, isInt := blk["index"].(int); !isInt {
		t.Errorf("index 应保持 int（不得 float64 化）: %#v", blk["index"])
	}
	if w, ok := blk["weight"].(float64); !ok || w != 1.5 {
		t.Errorf("float64 应保型: %#v", blk["weight"])
	}
	// 前缀比较恢复：副本 vs 原树逐值相等
	if got := longestPrefixOverlapIndex(out, in); got != 0 {
		t.Errorf("int 保型后前缀应命中 0: %v", got)
	}
	// 深拷贝隔离
	blk["index"] = 99
	if in[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["index"] != 0 {
		t.Error("深拷贝不应影响原树")
	}
}
