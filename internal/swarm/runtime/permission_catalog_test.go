package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

)

// ──────────────────────────── 权限等级 ────────────────────────────

// TestPermissionLevel_三等级 allow/ask/deny 合法，大小写与空白归一。
// Python: permission_catalog.py:111-119 _permission_level
func TestPermissionLevel_三等级(t *testing.T) {
	cases := map[string]string{
		"allow": "allow", "ask": "ask", "deny": "deny",
		"ALLOW": "allow", "  Ask  ": "ask", "DENY": "deny",
	}
	for raw, want := range cases {
		got := permissionLevel(raw)
		require.NotNil(t, got, "raw=%q", raw)
		require.Equal(t, want, *got)
	}
}

// TestPermissionLevel_非法返回nil 未知等级、非字符串返回 nil。
// Python: permission_catalog.py:114-119
func TestPermissionLevel_非法返回nil(t *testing.T) {
	require.Nil(t, permissionLevel("bogus"))
	require.Nil(t, permissionLevel(42))
	require.Nil(t, permissionLevel(nil))
	require.Nil(t, permissionLevel(""))
}

// TestPermissionLevel_映射取星号键 映射形式取 "*" 键。
// Python: permission_catalog.py:112-113 raw = raw.get("*")
func TestPermissionLevel_映射取星号键(t *testing.T) {
	got := permissionLevel(map[string]any{"*": " deny ", "other": "allow"})
	require.NotNil(t, got)
	require.Equal(t, "deny", *got)

	// 无 "*" 键则非法
	require.Nil(t, permissionLevel(map[string]any{"other": "allow"}))
}

// ──────────────────────────── 具名工具等级 ────────────────────────────

// TestNamedToolLevels_tools映射 从 tools 映射提取。
// Python: permission_catalog.py:124-130
func TestNamedToolLevels_tools映射(t *testing.T) {
	layer := map[string]any{
		"tools": map[string]any{
			"read_file":  "allow",
			"write_file": " deny ",
			"bad_tool":   "bogus",
			"":           "allow",
		},
	}
	got := namedToolLevels(layer)
	require.Len(t, got, 2, "非法等级与空名被跳过")
	require.Equal(t, "allow", got["read_file"])
	require.Equal(t, "deny", got["write_file"])
}

// TestNamedToolLevels_遗留三键 deny_tools/ask_tools/allow_tools 依次覆盖。
// Python: permission_catalog.py:132-144（顺序 deny → ask → allow）
func TestNamedToolLevels_遗留三键(t *testing.T) {
	layer := map[string]any{
		"deny_tools":  []any{"tool_a", "tool_b"},
		"ask_tools":   []any{"tool_b"},
		"allow_tools": []any{"tool_c"},
	}
	got := namedToolLevels(layer)
	require.Equal(t, "deny", got["tool_a"])
	require.Equal(t, "ask", got["tool_b"], "ask_tools 后处理覆盖 deny")
	require.Equal(t, "allow", got["tool_c"])
}

// TestNamedToolLevels_空层返回空 空层不含任何等级。
func TestNamedToolLevels_空层返回空(t *testing.T) {
	require.Empty(t, namedToolLevels(map[string]any{}))
}

// ──────────────────────────── 规则 ────────────────────────────

// TestRuleTools_归一与去重 单值、数组、去空白、保序去重。
// Python: permission_catalog.py:148-155 _rule_tools
func TestRuleTools_归一与去重(t *testing.T) {
	require.Equal(t, []string{"a"}, ruleTools("  a  "))
	require.Equal(t, []string{"a", "b"}, ruleTools([]any{"a", " b ", "a"}))
	require.Equal(t, []string{"a", "b"}, ruleTools([]string{"a", "b"}))
	require.Empty(t, ruleTools(nil))
	require.Empty(t, ruleTools(42))
}

// TestRuleAction_优先action action 合法时直接返回。
// Python: permission_catalog.py:158-163
func TestRuleAction_优先action(t *testing.T) {
	got := ruleAction(map[string]any{"action": "deny", "severity": "LOW"})
	require.NotNil(t, got)
	require.Equal(t, "deny", *got, "action 优先于 severity")
}

// TestRuleAction_severity映射 action 缺失时按 severity 映射。
// Python: permission_catalog.py:13-18 _SEVERITY_ACTIONS
func TestRuleAction_severity映射(t *testing.T) {
	cases := map[string]string{
		"LOW":      "allow",
		"MEDIUM":   "allow",
		"HIGH":     "ask",
		"CRITICAL": "deny",
		"low":      "allow",
	}
	for severity, want := range cases {
		got := ruleAction(map[string]any{"severity": severity})
		require.NotNil(t, got, "severity=%q", severity)
		require.Equal(t, want, *got)
	}
}

// TestRuleAction_未知severity返回nil 未映射的严重级返回 nil。
func TestRuleAction_未知severity返回nil(t *testing.T) {
	require.Nil(t, ruleAction(map[string]any{"severity": "UNKNOWN"}))
	require.Nil(t, ruleAction(map[string]any{}))
}

// TestRuleSnapshots_完整映射 规则字段逐一映射且 severity 大写。
// Python: permission_catalog.py:170-189
func TestRuleSnapshots_完整映射(t *testing.T) {
	layer := map[string]any{
		"rules": []any{
			map[string]any{
				"id": "r1", "tools": []any{"read_file"}, "pattern": "*.go",
				"action": "ask", "severity": "high", "description": "desc",
				"match_type": "glob",
			},
			"not-a-mapping",
			map[string]any{"id": "r2", "severity": "critical"},
		},
	}
	got := ruleSnapshots(layer)
	require.Len(t, got, 2, "非映射项跳过")
	require.Equal(t, "r1", got[0].RuleID)
	require.Equal(t, []string{"read_file"}, got[0].Tools)
	require.Equal(t, "*.go", got[0].Pattern)
	require.NotNil(t, got[0].Action)
	require.Equal(t, "ask", *got[0].Action)
	require.Equal(t, "HIGH", got[0].Severity, "severity 大写")
	require.Equal(t, "desc", got[0].Description)
	require.Equal(t, "glob", got[0].MatchType)
	// 第二条无 action，靠 severity 映射
	require.NotNil(t, got[1].Action)
	require.Equal(t, "deny", *got[1].Action)
}

// TestRuleSnapshots_无rules键返回空 层内无 rules 时返回 nil。
// Python: permission_catalog.py:172-173
func TestRuleSnapshots_无rules键返回空(t *testing.T) {
	require.Empty(t, ruleSnapshots(map[string]any{}))
	require.Empty(t, ruleSnapshots(map[string]any{"rules": "not-a-list"}))
}

// ──────────────────────────── 层快照 ────────────────────────────

// TestPermissionLayerSnapshot_enabled仅接受bool true/false 保留，非 bool 为 nil。
// Python: permission_catalog.py:194-197
func TestPermissionLayerSnapshot_enabled仅接受bool(t *testing.T) {
	enabled := permissionLayerSnapshot(map[string]any{"enabled": true})
	require.NotNil(t, enabled.Enabled)
	require.True(t, *enabled.Enabled)

	disabled := permissionLayerSnapshot(map[string]any{"enabled": false})
	require.NotNil(t, disabled.Enabled)
	require.False(t, *disabled.Enabled)

	for _, value := range []any{"true", 1, nil} {
		got := permissionLayerSnapshot(map[string]any{"enabled": value})
		require.Nil(t, got.Enabled, "value=%v", value)
	}
}

// TestPermissionLayerSnapshot_工具按名称排序 输出顺序稳定。
// Python: permission_catalog.py:198-201 sorted(tool_levels.items())
func TestPermissionLayerSnapshot_工具按名称排序(t *testing.T) {
	layer := map[string]any{
		"tools": map[string]any{"zebra": "allow", "alpha": "deny", "mike": "ask"},
	}
	got := permissionLayerSnapshot(layer)
	require.Len(t, got.Tools, 3)
	require.Equal(t, "alpha", got.Tools[0].Name)
	require.Equal(t, "mike", got.Tools[1].Name)
	require.Equal(t, "zebra", got.Tools[2].Name)
}

// TestPermissionLayerSnapshot_nil层安全 nil 层投影为空快照。
// Python: permission_catalog.py:193 source = layer if isinstance(layer, Mapping) else {}
func TestPermissionLayerSnapshot_nil层安全(t *testing.T) {
	got := permissionLayerSnapshot(nil)
	require.Nil(t, got.Enabled)
	require.Empty(t, got.Tools)
	require.Empty(t, got.Rules)
}

// ──────────────────────────── 会话 ID 归一化 ────────────────────────────

// TestNormalizePermissionSessionID_归一化 nil/空白返回空串；非法报错。
// Python: permission_catalog.py:214-223
func TestNormalizePermissionSessionID_归一化(t *testing.T) {
	got, err := normalizePermissionSessionID(nil)
	require.NoError(t, err)
	require.Equal(t, "", got)

	blank := "   "
	got, err = normalizePermissionSessionID(&blank)
	require.NoError(t, err)
	require.Equal(t, "", got)

	valid := "  sess-1  "
	got, err = normalizePermissionSessionID(&valid)
	require.NoError(t, err)
	require.Equal(t, "sess-1", got)

	for _, invalid := range []string{"../escape", "a/b", "a\\b"} {
		value := invalid
		_, err := normalizePermissionSessionID(&value)
		require.ErrorIs(t, err, ErrPermissionSessionIDInvalid, "value=%q", invalid)
	}
}

// ──────────────────────────── 会话归属校验 ────────────────────────────

// TestRequireOwnedSession_不存在报NOT_FOUND 缺失与外部渠道共用同一结果。
// Python: permission_catalog.py:246-249
func TestRequireOwnedSession_不存在报NOT_FOUND(t *testing.T) {
	err := requireOwnedSession("web", "definitely-not-exist-20-1")
	require.ErrorIs(t, err, ErrPermissionSessionNotFound)
}

// TestRequireOwnedSession_空渠道报错 渠道缺失透传。
// Python: permission_catalog.py:242-245 透传 code
func TestRequireOwnedSession_空渠道报错(t *testing.T) {
	err := requireOwnedSession("", "s1")
	require.ErrorIs(t, err, ErrSessionChannelRequired)
}

// ──────────────────────────── 快照读取 ────────────────────────────

// stubCapturePermissionLayers 用固定四层替换真实捕获，测试期间还原。
func stubCapturePermissionLayers(t *testing.T, layers PermissionLayers) {
	t.Helper()
	original := capturePermissionLayers
	t.Cleanup(func() { capturePermissionLayers = original })
	capturePermissionLayers = func(string) (PermissionLayers, error) {
		return layers, nil
	}
}

// stubCapturePermissionLayersError 让捕获返回错误，测试期间还原。
func stubCapturePermissionLayersError(t *testing.T, err error) {
	t.Helper()
	original := capturePermissionLayers
	t.Cleanup(func() { capturePermissionLayers = original })
	capturePermissionLayers = func(string) (PermissionLayers, error) {
		return PermissionLayers{}, err
	}
}

// TestReadPermissionSnapshot_宿主快照 无会话时 scope=host，四层全部投影。
// Python: permission_catalog.py:276-283
func TestReadPermissionSnapshot_宿主快照(t *testing.T) {
	stubCapturePermissionLayers(t, PermissionLayers{
		Global: map[string]any{
			"enabled": true,
			"tools":   map[string]any{"read_file": "allow"},
			"rules": []any{
				map[string]any{"id": "g1", "action": "deny", "severity": "high"},
			},
		},
		User:      map[string]any{"tools": map[string]any{"write_file": "ask"}},
		Session:   map[string]any{},
		Effective: map[string]any{"tools": map[string]any{"read_file": "allow"}},
	})

	got, err := ReadPermissionSnapshot(PermissionSnapshotInput{ChannelID: "web"})
	require.NoError(t, err)
	require.Equal(t, PermissionScopeHost, got.Scope)
	require.Equal(t, "", got.SessionID)

	require.NotNil(t, got.GlobalLayer.Enabled)
	require.True(t, *got.GlobalLayer.Enabled)
	require.Len(t, got.GlobalLayer.Tools, 1)
	require.Equal(t, "read_file", got.GlobalLayer.Tools[0].Name)
	require.Len(t, got.GlobalLayer.Rules, 1)
	require.Equal(t, "deny", *got.GlobalLayer.Rules[0].Action)

	require.Len(t, got.UserLayer.Tools, 1)
	require.Equal(t, "write_file", got.UserLayer.Tools[0].Name)
	require.Empty(t, got.SessionLayer.Tools)
	require.Len(t, got.Effective.Tools, 1)
}

// TestReadPermissionSnapshot_捕获失败报READ_FAILED 捕获异常统一为 READ_FAILED。
// Python: permission_catalog.py:268-274
func TestReadPermissionSnapshot_捕获失败报READ_FAILED(t *testing.T) {
	stubCapturePermissionLayersError(t, ErrPermissionReadFailed)
	_, err := ReadPermissionSnapshot(PermissionSnapshotInput{ChannelID: "web"})
	require.ErrorIs(t, err, ErrPermissionReadFailed)
}

// TestReadPermissionSnapshot_会话非法ID报错 非法会话标识在归一化阶段拒绝。
// Python: permission_catalog.py:261
func TestReadPermissionSnapshot_会话非法ID报错(t *testing.T) {
	invalid := "../escape"
	_, err := ReadPermissionSnapshot(PermissionSnapshotInput{
		ChannelID: "web", SessionID: &invalid,
	})
	require.ErrorIs(t, err, ErrPermissionSessionIDInvalid)
}

// TestReadPermissionSnapshot_会话不存在报错 归属校验先于捕获。
// Python: permission_catalog.py:262-263
func TestReadPermissionSnapshot_会话不存在报错(t *testing.T) {
	missing := "definitely-not-exist-20-1"
	_, err := ReadPermissionSnapshot(PermissionSnapshotInput{
		ChannelID: "web", SessionID: &missing,
	})
	require.ErrorIs(t, err, ErrPermissionSessionNotFound)
}

// ──────────────────────────── 序列化 ────────────────────────────

// TestPermissionToolSnapshotToDict_两字段 name 与 level。
// Python: permission_catalog.py:45-46
func TestPermissionToolSnapshotToDict_两字段(t *testing.T) {
	got := PermissionToolSnapshotToDict(PermissionToolSnapshot{Name: "t", Level: "ask"})
	require.Len(t, got, 2)
	require.Equal(t, "t", got["name"])
	require.Equal(t, "ask", got["level"])
}

// TestPermissionRuleSnapshotToDict_七字段 id 键名保持上游。
// Python: permission_catalog.py:61-70（键名 "id" 而非 "rule_id"）
func TestPermissionRuleSnapshotToDict_七字段(t *testing.T) {
	action := "deny"
	got := PermissionRuleSnapshotToDict(PermissionRuleSnapshot{
		RuleID: "r1", Tools: []string{"t1"}, Pattern: "p",
		Action: &action, Severity: "HIGH", Description: "d", MatchType: "glob",
	})
	require.Len(t, got, 7)
	require.Equal(t, "r1", got["id"], "键名为 id")
	require.Equal(t, []string{"t1"}, got["tools"])
	require.Equal(t, "deny", got["action"])
}

// TestPermissionRuleSnapshotToDict_无action为nil action 为空时序列化为 nil。
// Python: permission_catalog.py:66 "action": self.action（可为 None）
func TestPermissionRuleSnapshotToDict_无action为nil(t *testing.T) {
	got := PermissionRuleSnapshotToDict(PermissionRuleSnapshot{RuleID: "r1"})
	require.Nil(t, got["action"])
}

// TestPermissionLayerSnapshotToDict_三字段 enabled/tools/rules。
// Python: permission_catalog.py:81-86
func TestPermissionLayerSnapshotToDict_三字段(t *testing.T) {
	enabled := true
	got := PermissionLayerSnapshotToDict(PermissionLayerSnapshot{
		Enabled: &enabled,
		Tools:   []PermissionToolSnapshot{{Name: "t", Level: "allow"}},
		Rules:   []PermissionRuleSnapshot{{RuleID: "r1"}},
	})
	require.Len(t, got, 3)
	require.Equal(t, true, got["enabled"])
	require.Len(t, got["tools"], 1)
	require.Len(t, got["rules"], 1)
}

// TestPermissionSnapshotResultToDict_六字段 scope/session_id/四层。
// Python: permission_catalog.py:100-108
func TestPermissionSnapshotResultToDict_六字段(t *testing.T) {
	got := PermissionSnapshotResultToDict(PermissionSnapshotResult{
		Scope: PermissionScopeHost,
	})
	require.Len(t, got, 6)
	for _, key := range []string{
		"scope", "session_id", "global", "user", "session", "effective",
	} {
		require.Contains(t, got, key)
	}
}
