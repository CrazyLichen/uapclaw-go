package toolguard

import "testing"

// TestPackageBuiltinRulesEnabled 测试宿主开关的四种输入形态
func TestPackageBuiltinRulesEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"nil 配置默认开启", nil, true},
		{"键缺失默认开启", map[string]any{}, true},
		{"显式 true", map[string]any{"package_builtin_rules": true}, true},
		{"显式 false", map[string]any{"package_builtin_rules": false}, false},
		{"字符串 false", map[string]any{"package_builtin_rules": "false"}, false},
		{"字符串 0", map[string]any{"package_builtin_rules": "0"}, false},
		{"字符串 no", map[string]any{"package_builtin_rules": "no"}, false},
		{"字符串 off", map[string]any{"package_builtin_rules": "off"}, false},
		{"字符串 OFF 大写", map[string]any{"package_builtin_rules": "OFF"}, false},
		{"字符串 true", map[string]any{"package_builtin_rules": "true"}, true},
		{"字符串 yes", map[string]any{"package_builtin_rules": "yes"}, true},
		{"无法识别的值默认开启", map[string]any{"package_builtin_rules": "maybe"}, true},
		{"整数类型默认开启", map[string]any{"package_builtin_rules": 0}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PackageBuiltinRulesEnabled(c.cfg); got != c.want {
				t.Errorf("PackageBuiltinRulesEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestInlinePackageCommandRules_注入内置规则 测试内置规则带 layer=builtin 且排在用户规则前
func TestInlinePackageCommandRules_注入内置规则(t *testing.T) {
	userRule := map[string]any{"id": "my_rule", "tools": []any{"bash"}, "pattern": "re:.*"}
	cfg := map[string]any{"rules": []any{userRule}}
	got := InlinePackageCommandRules(cfg)

	rules, ok := got["rules"].([]any)
	if !ok {
		t.Fatalf("rules 类型 = %T, want []any", got["rules"])
	}
	if len(rules) < 18 {
		t.Fatalf("规则总数 = %d, want >= 18（17 内置 + 1 用户）", len(rules))
	}
	first, _ := rules[0].(map[string]any)
	if first["layer"] != "builtin" {
		t.Errorf("首条 layer = %v, want builtin（内置在前）", first["layer"])
	}
	last, _ := rules[len(rules)-1].(map[string]any)
	if last["id"] != "my_rule" {
		t.Errorf("末条 id = %v, want my_rule（用户在后）", last["id"])
	}
}

// TestInlinePackageCommandRules_剔除已有builtin层 测试重复调用不会叠加内置规则
func TestInlinePackageCommandRules_剔除已有builtin层(t *testing.T) {
	once := InlinePackageCommandRules(map[string]any{})
	twice := InlinePackageCommandRules(once)
	r1, _ := once["rules"].([]any)
	r2, _ := twice["rules"].([]any)
	if len(r1) != len(r2) {
		t.Errorf("二次注入后规则数 %d != %d，说明内置规则被重复叠加", len(r2), len(r1))
	}
}

// TestInlinePackageCommandRules_开关关闭 测试 package_builtin_rules=false 时不注入
func TestInlinePackageCommandRules_开关关闭(t *testing.T) {
	cfg := map[string]any{"package_builtin_rules": false, "rules": []any{}}
	got := InlinePackageCommandRules(cfg)
	rules, _ := got["rules"].([]any)
	if len(rules) != 0 {
		t.Errorf("开关关闭时规则数 = %d, want 0", len(rules))
	}
}

// TestInlinePackageCommandRules_不修改入参 测试不修改调用方传入的 map
func TestInlinePackageCommandRules_不修改入参(t *testing.T) {
	cfg := map[string]any{"rules": []any{}}
	_ = InlinePackageCommandRules(cfg)
	rules, _ := cfg["rules"].([]any)
	if len(rules) != 0 {
		t.Error("入参被修改，应深拷贝")
	}
}

// TestLoadPackageCommandRules_17条 测试包内命令规则共 17 条
func TestLoadPackageCommandRules_17条(t *testing.T) {
	rules := LoadPackageCommandRules()
	if len(rules) != 17 {
		t.Errorf("命令规则条数 = %d, want 17", len(rules))
	}
	for _, r := range rules {
		if id, _ := r["id"].(string); id == "" {
			t.Error("存在 id 为空的规则")
		}
	}
}

// TestYamlCommandRuleAction_三种非法输入 测试缺失/非法/allow 都返回空串
func TestYamlCommandRuleAction_三种非法输入(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
	}{
		{"action 缺失", map[string]any{"id": "no_action"}},
		{"action 非字符串", map[string]any{"id": "bad_type", "action": 123}},
		{"action 空白", map[string]any{"id": "blank", "action": "   "}},
		{"action 非法值", map[string]any{"id": "invalid", "action": "block"}},
		{"action 为 allow", map[string]any{"id": "allow_rule", "action": "allow"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := yamlCommandRuleAction(c.item); got != "" {
				t.Errorf("yamlCommandRuleAction() = %q, want empty", got)
			}
		})
	}
	// 合法值原样返回（小写化）
	if got := yamlCommandRuleAction(map[string]any{"id": "ok", "action": "DENY"}); got != "deny" {
		t.Errorf("yamlCommandRuleAction() = %q, want deny", got)
	}
}

// TestInlinePackageCommandRules_剔除非法action 测试 action 非法的内置规则被跳过
func TestInlinePackageCommandRules_剔除非法action(t *testing.T) {
	got := InlinePackageCommandRules(map[string]any{})
	rules := listOfMaps(got["rules"])
	if len(rules) != 17 {
		t.Fatalf("规则数 = %d, want 17（17 条内置规则都有合法 action）", len(rules))
	}
	for _, r := range rules {
		action, _ := r["action"].(string)
		if action != "ask" && action != "deny" {
			t.Errorf("规则 %v 的 action = %q，应为 ask 或 deny", r["id"], action)
		}
		if r["layer"] != "builtin" {
			t.Errorf("规则 %v 的 layer = %v，应为 builtin", r["id"], r["layer"])
		}
	}
}
