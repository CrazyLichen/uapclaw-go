package adapter

import "testing"

// TestDefaultModelClientConfigFromConfigBase_优先快照 验证 T-07：
// 已发布的 configBase 命中时直接返回，不再读盘。
func TestDefaultModelClientConfigFromConfigBase_优先快照(t *testing.T) {
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{}, map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{
					"api_key":  "snapshot-key",
					"api_base": "https://snapshot.example.com/v1",
				},
			},
		},
	})

	mcc := d.defaultModelClientConfigFromConfigBase()
	if mcc == nil {
		t.Fatal("configBase 命中时不应返回 nil")
	}
	if got, _ := mcc["api_key"].(string); got != "snapshot-key" {
		t.Errorf("api_key = %q, want snapshot-key（应来自 configBase 快照）", got)
	}
}

// TestDefaultModelClientConfigFromConfigBase_缺失时回退 快照无 models 段时回退读盘路径，
// 读不到文件也必须是 nil 而不是 panic。
func TestDefaultModelClientConfigFromConfigBase_缺失时回退(t *testing.T) {
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{}, map[string]any{"react": map[string]any{}})

	// 只断言不 panic；返回 nil 或读盘结果都合法（取决于运行环境的 config.yaml）
	_ = d.defaultModelClientConfigFromConfigBase()
}

// TestModelClientConfigFromBase_各层缺失 覆盖逐层缺失返回 nil。
func TestModelClientConfigFromBase_各层缺失(t *testing.T) {
	cases := []map[string]any{
		nil,
		{},
		{"models": "not-map"},
		{"models": map[string]any{}},
		{"models": map[string]any{"default": map[string]any{}}},
		{"models": map[string]any{"default": map[string]any{"model_client_config": "x"}}},
	}
	for i, in := range cases {
		if got := modelClientConfigFromBase(in); got != nil {
			t.Errorf("case %d: 期望 nil，实际 %v", i, got)
		}
	}
}
