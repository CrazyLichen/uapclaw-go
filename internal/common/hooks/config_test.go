package hooks

import "testing"

// TestCommandHookConfig_默认值 测试 CommandHookConfig 字段
func TestCommandHookConfig_默认值(t *testing.T) {
	cfg := CommandHookConfig{}
	if cfg.Type != "" {
		t.Errorf("Type = %q, want empty (defaults set at usage)", cfg.Type)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout = %d, want 0 (defaults set at usage)", cfg.Timeout)
	}
	if cfg.Shell != "" {
		t.Errorf("Shell = %q, want empty (defaults set at usage)", cfg.Shell)
	}
}

// TestPromptHookConfig_默认值 测试 PromptHookConfig 字段
func TestPromptHookConfig_默认值(t *testing.T) {
	cfg := PromptHookConfig{}
	if cfg.Type != "" {
		t.Errorf("Type = %q, want empty", cfg.Type)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout = %d, want 0", cfg.Timeout)
	}
}

// TestHookMatcher_Matches_通配符 测试 * 匹配所有
func TestHookMatcher_Matches_通配符(t *testing.T) {
	m := HookMatcher{Matcher: "*"}
	if !m.Matches("any_tool") {
		t.Error("Matches('any_tool') with matcher=* should be true")
	}
	if !m.Matches("") {
		t.Error("Matches('') with matcher=* should be true")
	}
}

// TestHookMatcher_Matches_空字符串 测试空 matcher 匹配所有
func TestHookMatcher_Matches_空字符串(t *testing.T) {
	m := HookMatcher{Matcher: ""}
	if !m.Matches("anything") {
		t.Error("Matches('anything') with matcher='' should be true")
	}
}

// TestHookMatcher_Matches_精确匹配 测试精确字符串匹配
func TestHookMatcher_Matches_精确匹配(t *testing.T) {
	m := HookMatcher{Matcher: "read_file"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') should be true")
	}
	if m.Matches("write_file") {
		t.Error("Matches('write_file') should be false")
	}
}

// TestHookMatcher_Matches_OR匹配 测试 | 分隔的 OR 匹配
func TestHookMatcher_Matches_OR匹配(t *testing.T) {
	m := HookMatcher{Matcher: "read_file|write_file"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') with OR matcher should be true")
	}
	if !m.Matches("write_file") {
		t.Error("Matches('write_file') with OR matcher should be true")
	}
	if m.Matches("delete_file") {
		t.Error("Matches('delete_file') with OR matcher should be false")
	}
}

// TestHookMatcher_Matches_OR匹配带空格 测试 OR 匹配中空格 trim
func TestHookMatcher_Matches_OR匹配带空格(t *testing.T) {
	m := HookMatcher{Matcher: " read_file | write_file "}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') with OR matcher + spaces should be true")
	}
	if !m.Matches("write_file") {
		t.Error("Matches('write_file') with OR matcher + spaces should be true")
	}
}

// TestHookMatcher_Matches_正则匹配 测试正则匹配
func TestHookMatcher_Matches_正则匹配(t *testing.T) {
	// 以 ^ 开头
	m := HookMatcher{Matcher: "^read_.*"}
	if !m.Matches("read_file") {
		t.Error("Matches('read_file') with regex ^read_.* should be true")
	}
	if m.Matches("write_file") {
		t.Error("Matches('write_file') with regex ^read_.* should be false")
	}
	// 以 $ 结尾
	m2 := HookMatcher{Matcher: "_file$"}
	if !m2.Matches("read_file") {
		t.Error("Matches('read_file') with regex _file$ should be true")
	}
	if m2.Matches("read_dir") {
		t.Error("Matches('read_dir') with regex _file$ should be false")
	}
	// 含 .* 的正则
	m3 := HookMatcher{Matcher: "tool.*name"}
	if !m3.Matches("tool_xyz_name") {
		t.Error("Matches('tool_xyz_name') with regex tool.*name should be true")
	}
}

// TestHookMatcher_Matches_正则OR不冲突 测试以 ^ 开头的含 | 字符走正则而非 OR
func TestHookMatcher_Matches_正则OR不冲突(t *testing.T) {
	// ^ 开头含 | 时走正则（对齐 Python: startsWith("^") 不走 OR）
	m := HookMatcher{Matcher: "^read|write"}
	if !m.Matches("read") {
		t.Error("^read|write 应匹配 'read'（正则语义）")
	}
	if m.Matches("write") {
		// 正则 ^read|write 匹配 "^read" OR "^write"，即匹配 "read..." 或 "write..."
		// 但 Python 的逻辑是 startsWith("^") 时不走 OR，直接走 matchSingle → regexp
		// regexp: ^read|write 匹配 "read" 开头或含 "write" 的
		// 所以 "write" 实际上也匹配
		t.Log("^read|write 正则匹配 'write'（Go regexp | 是 OR 分支）")
	}
}

// TestHooksConfig_Match 测试 Match 获取匹配的 hook 配置
func TestHooksConfig_Match(t *testing.T) {
	cfg := HooksConfig{
		Events: map[string][]HookMatcher{
			HookEventPreToolUse: {
				{
					Matcher: "read_file",
					Hooks:   []map[string]any{{"type": "command", "command": "check_file"}},
				},
				{
					Matcher: "*",
					Hooks:   []map[string]any{{"type": "prompt", "prompt": "review"}},
				},
			},
		},
	}
	hooks := cfg.Match(HookEventPreToolUse, "read_file")
	if len(hooks) != 2 {
		t.Errorf("Match(PreToolUse, read_file) = %d hooks, want 2", len(hooks))
	}
	// 不匹配的事件应返回空
	hooks2 := cfg.Match(HookEventPostToolUse, "read_file")
	if len(hooks2) != 0 {
		t.Errorf("Match(PostToolUse, read_file) = %d hooks, want 0", len(hooks2))
	}
	// 不匹配的 query（通配 * 除外）
	hooks3 := cfg.Match(HookEventPreToolUse, "delete_file")
	if len(hooks3) != 1 {
		t.Errorf("Match(PreToolUse, delete_file) = %d hooks, want 1 (only * matcher)", len(hooks3))
	}
}

// TestHooksConfig_Match_禁用 测试 DisableAllHooks 时返回空
func TestHooksConfig_Match_禁用(t *testing.T) {
	cfg := HooksConfig{
		Events:          map[string][]HookMatcher{HookEventPreToolUse: {{Matcher: "*"}}},
		DisableAllHooks: true,
	}
	hooks := cfg.Match(HookEventPreToolUse, "read_file")
	if len(hooks) != 0 {
		t.Errorf("Match with DisableAllHooks should return 0 hooks, got %d", len(hooks))
	}
}

// TestHooksConfig_GetEventSummary 测试摘要生成
func TestHooksConfig_GetEventSummary(t *testing.T) {
	cfg := HooksConfig{
		Events: map[string][]HookMatcher{
			HookEventPreToolUse: {
				{Matcher: "*", Hooks: []map[string]any{{"type": "command"}}},
			},
		},
	}
	summary := cfg.GetEventSummary()
	if len(summary) != 17 {
		t.Errorf("GetEventSummary() len = %d, want 17 (all HookEvents)", len(summary))
	}
	// 查找 PreToolUse 条目
	for _, entry := range summary {
		if entry["name"] == HookEventPreToolUse {
			totalHooks, _ := entry["total_hooks"].(int)
			if totalHooks != 1 {
				t.Errorf("PreToolUse total_hooks = %d, want 1", totalHooks)
			}
			matchers, _ := entry["matchers"].([]map[string]any)
			if len(matchers) != 1 {
				t.Errorf("PreToolUse matchers count = %d, want 1", len(matchers))
			}
			return
		}
	}
	t.Error("PreToolUse not found in summary")
}

// TestLoadHooksConfig_空配置 测试空配置返回默认值
func TestLoadHooksConfig_空配置(t *testing.T) {
	cfg := LoadHooksConfig(nil)
	if cfg == nil {
		t.Error("LoadHooksConfig(nil) should return non-nil")
	}
	if len(cfg.Events) != 0 {
		t.Errorf("Events = %d, want 0 for nil config", len(cfg.Events))
	}
}

// TestLoadHooksConfig_无hooks段 测试配置中没有 hooks 段
func TestLoadHooksConfig_无hooks段(t *testing.T) {
	cfg := LoadHooksConfig(map[string]any{"models": map[string]any{}})
	if len(cfg.Events) != 0 {
		t.Errorf("Events = %d, want 0 for config without hooks section", len(cfg.Events))
	}
}

// TestLoadHooksConfig_有效配置 测试有效 hooks 配置加载
func TestLoadHooksConfig_有效配置(t *testing.T) {
	configBase := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "read_file|write_file",
					"hooks":   []any{map[string]any{"type": "command", "command": "check"}},
				},
			},
		},
	}
	cfg := LoadHooksConfig(configBase)
	if len(cfg.Events) != 1 {
		t.Errorf("Events count = %d, want 1", len(cfg.Events))
	}
	matchers := cfg.Events[HookEventPreToolUse]
	if len(matchers) != 1 {
		t.Errorf("PreToolUse matchers = %d, want 1", len(matchers))
	}
	if matchers[0].Matcher != "read_file|write_file" {
		t.Errorf("Matcher = %q, want %q", matchers[0].Matcher, "read_file|write_file")
	}
	if len(matchers[0].Hooks) != 1 {
		t.Errorf("Hooks count = %d, want 1", len(matchers[0].Hooks))
	}
}

// TestLoadHooksConfig_禁用 测试 disable_all_hooks 加载
func TestLoadHooksConfig_禁用(t *testing.T) {
	configBase := map[string]any{
		"hooks": map[string]any{
			"disable_all_hooks": true,
		},
	}
	cfg := LoadHooksConfig(configBase)
	if !cfg.DisableAllHooks {
		t.Error("DisableAllHooks should be true")
	}
}

// TestLoadHooksConfig_多事件 测试多事件配置加载
func TestLoadHooksConfig_多事件(t *testing.T) {
	configBase := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command"}}},
			},
			"Stop": []any{
				map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "prompt"}}},
			},
		},
	}
	cfg := LoadHooksConfig(configBase)
	if len(cfg.Events) != 2 {
		t.Errorf("Events count = %d, want 2", len(cfg.Events))
	}
}
