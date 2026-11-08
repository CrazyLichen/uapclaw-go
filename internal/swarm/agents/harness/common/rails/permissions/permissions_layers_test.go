package permissions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 键函数 ────────────────────────────

// TestScalarLevel 测试级别标量解析（含 dict 包装）
func TestScalarLevel(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"allow", "allow"},
		{"ASK", "ask"},
		{"  deny  ", "deny"},
		{map[string]any{"*": "deny"}, "deny"},
		{map[string]any{"read": "deny"}, ""}, // 无 * 键
		{"bogus", ""},
		{123, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := scalarLevel(c.in); got != c.want {
			t.Errorf("scalarLevel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestStrNames 测试字符串列表清洗
func TestStrNames(t *testing.T) {
	got := strNames([]any{"a", "", "  b  ", 123, nil, "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// 非列表
	if got := strNames("not a list"); len(got) != 0 {
		t.Errorf("非列表应返回空，got %v", got)
	}
}

// TestOrderedToolNamesAtLevel_tools先于列表 测试 tools 字段优先，且去重
func TestOrderedToolNamesAtLevel_tools先于列表(t *testing.T) {
	layer := map[string]any{
		"tools": map[string]any{
			"t1": "allow",
			"t2": "deny",
			"t3": map[string]any{"*": "allow"},
		},
		"allow_tools": []any{"t4", "t1"}, // t1 重复
	}
	got := orderedToolNamesAtLevel(layer, "allow")
	// tools 里的 t1、t3 先出（保序由 map 遍历决定，此处只校验集合与去重）
	if len(got) != 3 {
		t.Fatalf("got %v, want 3 项（t1/t3/t4，t1 去重）", got)
	}
	set := map[string]struct{}{}
	for _, n := range got {
		set[n] = struct{}{}
	}
	for _, want := range []string{"t1", "t3", "t4"} {
		if _, hit := set[want]; !hit {
			t.Errorf("缺 %q，got %v", want, got)
		}
	}
	if _, hit := set["t2"]; hit {
		t.Error("t2 是 deny，不应出现在 allow 列表")
	}
}

// TestToolNamesAtLevel_set 测试返回集合
func TestToolNamesAtLevel_set(t *testing.T) {
	layer := map[string]any{"deny_tools": []any{"x", "y"}}
	got := toolNamesAtLevel(layer, "deny")
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 项", got)
	}
	if _, hit := got["x"]; !hit {
		t.Error("缺 x")
	}
}

// TestNormalizedToolNames 测试 tools 字段的三种形态
func TestNormalizedToolNames(t *testing.T) {
	// 字符串
	if got := normalizedToolNames(map[string]any{"tools": "bash"}); len(got) != 1 || got[0] != "bash" {
		t.Errorf("字符串形态 got %v", got)
	}
	// 列表
	if got := normalizedToolNames(map[string]any{"tools": []any{"a", "b"}}); len(got) != 2 {
		t.Errorf("列表形态 got %v", got)
	}
	// 缺失 / 非法
	if got := normalizedToolNames(map[string]any{}); len(got) != 0 {
		t.Errorf("缺失应返回空，got %v", got)
	}
	if got := normalizedToolNames(map[string]any{"tools": 123}); len(got) != 0 {
		t.Errorf("非法应返回空，got %v", got)
	}
}

// TestOverrideKey_signature与id 两个分支 测试冲突键的两种形态
func TestOverrideKey_signature与id(t *testing.T) {
	// 有 signature 字段 → signature 分支
	sig := overrideKey(map[string]any{
		"tools": []any{"bash", "sh"}, "match_type": "command",
		"pattern": "rm *", "action": "allow", "id": "ignored",
	})
	if sig[0] != "signature" {
		t.Errorf("kind = %q, want signature", sig[0])
	}
	// tools 顺序不影响键
	sig2 := overrideKey(map[string]any{
		"tools": []any{"sh", "bash"}, "match_type": "command",
		"pattern": "rm *", "action": "allow",
	})
	if sig[1] != sig2[1] {
		t.Error("tools 顺序不同应产生相同 signature")
	}
	// 全空 → id 分支
	id := overrideKey(map[string]any{"id": "rule1"})
	if id[0] != "id" || id[1] != "rule1" {
		t.Errorf("got %v, want [id rule1]", id)
	}
}

// TestOverrideKeys 测试批量取键
func TestOverrideKeys(t *testing.T) {
	raw := []any{
		map[string]any{"id": "a"},
		map[string]any{"id": "b"},
		"not a dict",
		map[string]any{"id": "a"}, // 重复
	}
	if got := overrideKeys(raw); len(got) != 2 {
		t.Errorf("got %d 个键, want 2（去重）", len(got))
	}
	if got := overrideKeys("not a list"); len(got) != 0 {
		t.Errorf("非列表应返回空，got %v", got)
	}
}

// TestStableOverrideID 测试稳定 id 生成：同内容同 id，不同内容不同 id
func TestStableOverrideID(t *testing.T) {
	item := map[string]any{
		"tools": []any{"bash"}, "match_type": "command",
		"pattern": "rm *", "action": "allow",
	}
	a := stableOverrideID(item)
	b := stableOverrideID(item)
	if a != b {
		t.Errorf("同内容应产生相同 id：%q vs %q", a, b)
	}
	if len(a) > 120 {
		t.Errorf("id 超长: %d > 120", len(a))
	}
	if !strings.HasPrefix(a, "user_allow_") {
		t.Errorf("id = %q，应以 user_allow_ 开头", a)
	}
	// 不同 action → 不同 id
	other := map[string]any{
		"tools": []any{"bash"}, "match_type": "command",
		"pattern": "rm *", "action": "deny",
	}
	if stableOverrideID(other) == a {
		t.Error("不同 action 应产生不同 id")
	}
}

// TestUniqueOverrideIDs 测试 id 冲突时加序号
func TestUniqueOverrideIDs(t *testing.T) {
	overrides := []map[string]any{
		{"id": "same", "x": 1},
		{"id": "same", "x": 2},
		{"id": "same", "x": 3},
	}
	got := uniqueOverrideIDs(overrides, nil)
	ids := make([]string, 0, 3)
	for _, item := range got {
		ids = append(ids, item["id"].(string))
	}
	if ids[0] != "same" || ids[1] != "same_2" || ids[2] != "same_3" {
		t.Errorf("ids = %v, want [same same_2 same_3]", ids)
	}
	// reserved 冲突
	reserved := map[string]struct{}{"same": {}}
	got2 := uniqueOverrideIDs([]map[string]any{{"id": "same"}}, reserved)
	if got2[0]["id"] != "same_2" {
		t.Errorf("保留 id 冲突时应从 _2 开始，got %v", got2[0]["id"])
	}
	// 无 id 的原样保留
	got3 := uniqueOverrideIDs([]map[string]any{{"x": 1}}, nil)
	if _, has := got3[0]["id"]; has {
		t.Error("无 id 的不应被补 id")
	}
}

// TestPathKey 测试路径键归一（反斜杠、尾部斜杠、大小写、match 默认）
func TestPathKey(t *testing.T) {
	a := pathKey(map[string]any{"path": `C:\Users\X\`, "match": "prefix"})
	b := pathKey(map[string]any{"path": "c:/users/x", "match": "PREFIX"})
	if a[0] != b[0] {
		t.Errorf("路径归一不一致：%q vs %q", a[0], b[0])
	}
	if a[1] != "prefix" {
		t.Errorf("match = %q, want prefix", a[1])
	}
	// match 缺失默认 prefix
	c := pathKey(map[string]any{"path": "/a"})
	if c[1] != "prefix" {
		t.Errorf("缺失 match 应默认 prefix，got %q", c[1])
	}
}

// TestPathKeys 测试批量取路径键
func TestPathKeys(t *testing.T) {
	layer := map[string]any{
		"file_guard": map[string]any{
			"paths": []any{
				map[string]any{"path": "/a"},
				map[string]any{"path": "/b", "match": "glob"},
				"not a dict",
			},
		},
	}
	if got := pathKeys(layer); len(got) != 2 {
		t.Errorf("got %d 个键, want 2", len(got))
	}
	// 无 file_guard
	if got := pathKeys(map[string]any{}); len(got) != 0 {
		t.Errorf("无 file_guard 应返回空，got %v", got)
	}
}

// TestOverrideIDs 测试取 override id 集合
func TestOverrideIDs(t *testing.T) {
	raw := []any{
		map[string]any{"id": "a"},
		map[string]any{"id": "  b  "},
		map[string]any{"id": ""},
		map[string]any{},
		"not a dict",
	}
	got := overrideIDs(raw)
	if len(got) != 2 {
		t.Errorf("got %v, want 2 项", got)
	}
	if _, hit := got["a"]; !hit {
		t.Error("缺 a")
	}
	if _, hit := got["b"]; !hit {
		t.Error("缺 b（应 trim）")
	}
}

// TestAsPermissionsSection 测试 permissions 段包裹解包
func TestAsPermissionsSection(t *testing.T) {
	// 有 permissions 包裹且无 overlay 键 → 解包
	got := asPermissionsSection(map[string]any{
		"permissions": map[string]any{"allow_tools": []any{"x"}},
	})
	if _, has := got["permissions"]; has {
		t.Error("应解包，不应保留 permissions 键")
	}
	if _, has := got["allow_tools"]; !has {
		t.Error("应取出内层 allow_tools")
	}
	// 有 overlay 键 → 不解包
	outer := map[string]any{
		"permissions": map[string]any{"x": 1},
		"allow_tools": []any{"y"},
	}
	got2 := asPermissionsSection(outer)
	if _, has := got2["permissions"]; !has {
		t.Error("有 overlay 键时不应解包")
	}
	// 非 dict
	if got := asPermissionsSection("not a dict"); len(got) != 0 {
		t.Errorf("非 dict 应返回空，got %v", got)
	}
}

// ──────────────────────────── 落盘集成测试 ────────────────────────────

// setupTempPermissionDirs 把配置目录与会话目录指到临时目录，测试结束自动恢复。
func setupTempPermissionDirs(t *testing.T) (configDir, sessionsDir string) {
	t.Helper()
	configDir = t.TempDir()
	sessionsDir = t.TempDir()
	t.Setenv("UAPCLAW_CONFIG_DIR", configDir)
	t.Setenv("UAPCLAW_SESSIONS_DIR", sessionsDir)
	t.Setenv("UAPCLAW_CONFIG_YAML", filepath.Join(configDir, "config.yaml"))
	return configDir, sessionsDir
}

// TestOverlayFromEffective_user层 测试 user overlay 只保留生效层独有的 allow
func TestOverlayFromEffective_user层(t *testing.T) {
	global := map[string]any{"allow_tools": []any{"g1"}, "ask_tools": []any{"g2"}}
	sessionLayer := map[string]any{"allow_tools": []any{"s1"}}
	effective := map[string]any{
		"allow_tools": []any{"g1", "s1", "u_new"}, // g1/s1 在更低层，u_new 是新增
		"ask_tools":   []any{"g2", "u_ask"},       // g2 在 global
		"deny_tools":  []any{"u_deny"},
	}
	got := OverlayFromEffective(effective, false, global, nil, sessionLayer)

	allow, _ := got["allow_tools"].([]any)
	if len(allow) != 1 || allow[0] != "u_new" {
		t.Errorf("allow_tools = %v, want [u_new]", allow)
	}
	ask, _ := got["ask_tools"].([]any)
	if len(ask) != 1 || ask[0] != "u_ask" {
		t.Errorf("ask_tools = %v, want [u_ask]", ask)
	}
	deny, _ := got["deny_tools"].([]any)
	if len(deny) != 1 || deny[0] != "u_deny" {
		t.Errorf("deny_tools = %v, want [u_deny]", deny)
	}
}

// TestOverlayFromEffective_session层不记ask与deny 测试 session 层只输出 allow
func TestOverlayFromEffective_session层不记ask与deny(t *testing.T) {
	global := map[string]any{"allow_tools": []any{"g1"}}
	effective := map[string]any{
		"allow_tools": []any{"s_new"},
		"ask_tools":   []any{"s_ask"},
		"deny_tools":  []any{"s_deny"},
	}
	got := OverlayFromEffective(effective, true, global, nil, nil)
	if _, has := got["ask_tools"]; has {
		t.Error("session 层不应输出 ask_tools")
	}
	if _, has := got["deny_tools"]; has {
		t.Error("session 层不应输出 deny_tools")
	}
	allow, _ := got["allow_tools"].([]any)
	if len(allow) != 1 || allow[0] != "s_new" {
		t.Errorf("allow_tools = %v, want [s_new]", allow)
	}
}

// TestOverlayFromEffective_builtin路径被排除 测试 layer=builtin 的路径不写进 overlay
func TestOverlayFromEffective_builtin路径被排除(t *testing.T) {
	effective := map[string]any{
		"file_guard": map[string]any{
			"paths": []any{
				map[string]any{"path": "/builtin", "layer": "builtin"},
				map[string]any{"path": "/user"},
			},
		},
	}
	got := OverlayFromEffective(effective, false, map[string]any{}, nil, nil)
	fg, _ := got["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want 1 项（builtin 被排除）", paths)
	}
	item, _ := paths[0].(map[string]any)
	if item["path"] != "/user" {
		t.Errorf("path = %v, want /user", item["path"])
	}
}

// TestMergeOverlayIntoCurrent_剔除引擎键 测试引擎专属键被剔除
func TestMergeOverlayIntoCurrent_剔除引擎键(t *testing.T) {
	current := map[string]any{
		"net_guard":   map[string]any{"enabled": true},
		"shell_guard": map[string]any{"x": 1},
		"defaults":    map[string]any{"*": "ask"},
		"tools":       map[string]any{"t": "allow"},
		"enabled":     true,
		"keep_me":     "yes",
	}
	overlay := map[string]any{"allow_tools": []any{"new"}}
	got := mergeOverlayIntoCurrent(current, overlay, false)
	for _, key := range []string{"net_guard", "shell_guard", "defaults", "tools", "enabled"} {
		if _, has := got[key]; has {
			t.Errorf("引擎专属键 %q 应被剔除", key)
		}
	}
	if got["keep_me"] != "yes" {
		t.Error("非引擎键应保留")
	}
	if _, has := got["allow_tools"]; !has {
		t.Error("overlay 的 allow_tools 应合并进来")
	}
}

// TestMergeOverlayIntoCurrent_session剔除ask与deny 测试 session 层不保留 ask/deny/enabled
func TestMergeOverlayIntoCurrent_session剔除ask与deny(t *testing.T) {
	current := map[string]any{"allow_tools": []any{"old"}, "ask_tools": []any{"a"}, "deny_tools": []any{"d"}}
	overlay := map[string]any{"allow_tools": []any{"new"}}
	got := mergeOverlayIntoCurrent(current, overlay, true)
	if _, has := got["ask_tools"]; has {
		t.Error("session 层不应有 ask_tools")
	}
	if _, has := got["deny_tools"]; has {
		t.Error("session 层不应有 deny_tools")
	}
	allow, _ := got["allow_tools"].([]any)
	if len(allow) != 1 || allow[0] != "new" {
		t.Errorf("allow_tools = %v, want [new]", allow)
	}
}

// TestUpdateUserToolLevel_三级切换 测试工具在三个列表间正确迁移
func TestUpdateUserToolLevel_三级切换(t *testing.T) {
	setupTempPermissionDirs(t)

	// 初始设为 allow
	if got := UpdateUserToolLevel("bash", "allow"); got == nil {
		t.Fatal("设置 allow 失败")
	}
	user := LoadUserPermissions()
	if !listHasTool(toolList(user, "allow_tools"), "bash") {
		t.Error("bash 应在 allow_tools")
	}

	// 改为 deny → 应从 allow 移除、加到 deny
	UpdateUserToolLevel("bash", "deny")
	user = LoadUserPermissions()
	if listHasTool(toolList(user, "allow_tools"), "bash") {
		t.Error("bash 不应还在 allow_tools")
	}
	if !listHasTool(toolList(user, "deny_tools"), "bash") {
		t.Error("bash 应在 deny_tools")
	}

	// 改为 ask → 应在 ask_tools
	UpdateUserToolLevel("bash", "ask")
	user = LoadUserPermissions()
	if !listHasTool(toolList(user, "ask_tools"), "bash") {
		t.Error("bash 应在 ask_tools")
	}
	if listHasTool(toolList(user, "deny_tools"), "bash") {
		t.Error("bash 不应还在 deny_tools")
	}
}

// TestUpdateUserToolLevel_参数非法 测试空名与非法级别返回 nil
func TestUpdateUserToolLevel_参数非法(t *testing.T) {
	setupTempPermissionDirs(t)
	if got := UpdateUserToolLevel("", "allow"); got != nil {
		t.Error("空工具名应返回 nil")
	}
	if got := UpdateUserToolLevel("bash", "bogus"); got != nil {
		t.Error("非法级别应返回 nil")
	}
}

// TestUpdateUserToolLevel_ask撤销会话允许 测试改 ask 时清掉会话层的 allow
func TestUpdateUserToolLevel_ask撤销会话允许(t *testing.T) {
	configDir, sessionsDir := setupTempPermissionDirs(t)
	_ = configDir
	const sid = "sess-1"

	// 造一个会话层允许
	sessionDir := filepath.Join(sessionsDir, sid)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDir, "session_permissions.yaml")
	if err := os.WriteFile(sessionPath, []byte("allow_tools:\n  - bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := UpdateUserToolLevel("bash", "ask")
	if got == nil {
		t.Fatal("更新失败")
	}
	revoked, _ := got["revoked"].(map[string]int)
	if revoked["session_allow"] != 1 {
		t.Errorf("revoked.session_allow = %d, want 1", revoked["session_allow"])
	}
	// 会话文件里的 bash 应被移除
	session := LoadSessionPermissions(sid)
	if listHasTool(toolList(session, "allow_tools"), "bash") {
		t.Error("会话层 bash 应被撤销")
	}
}

// TestStripToolOverrides_独有与共享 测试只含该工具的删除、共享的只摘名
func TestStripToolOverrides_独有与共享(t *testing.T) {
	layer := map[string]any{
		"approval_overrides": []any{
			map[string]any{"id": "only", "tools": []any{"bash"}},
			map[string]any{"id": "shared", "tools": []any{"bash", "sh"}},
			map[string]any{"id": "other", "tools": []any{"zsh"}},
			map[string]any{"id": "notools"},
		},
	}
	removed := stripToolOverrides(layer, "bash")
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	kept, _ := layer["approval_overrides"].([]any)
	if len(kept) != 3 {
		t.Fatalf("kept = %d 条, want 3（shared 摘名保留 + other + notools）", len(kept))
	}
	// shared 应只剩 sh
	for _, item := range kept {
		m, _ := item.(map[string]any)
		if m["id"] == "shared" {
			tools, _ := m["tools"].([]any)
			if len(tools) != 1 || tools[0] != "sh" {
				t.Errorf("shared tools = %v, want [sh]", tools)
			}
		}
	}
}

// TestDropSessionAllow 测试会话层允许撤销
func TestDropSessionAllow(t *testing.T) {
	layer := map[string]any{
		"allow_tools": []any{"bash", "sh"},
		"tools":       map[string]any{"bash": "allow", "zsh": "deny"},
	}
	changed := dropSessionAllow(layer, "bash")
	if !changed {
		t.Error("应发生变更")
	}
	if listHasTool(toolList(layer, "allow_tools"), "bash") {
		t.Error("allow_tools 里的 bash 应被移除")
	}
	tools, _ := layer["tools"].(map[string]any)
	if _, has := tools["bash"]; has {
		t.Error("tools 里的 bash 应被移除")
	}
	if _, has := tools["zsh"]; !has {
		t.Error("zsh（deny）不应被移除")
	}
}

// TestPersistUserOverlayFromEffective 测试从生效配置落盘 user overlay
func TestPersistUserOverlayFromEffective(t *testing.T) {
	setupTempPermissionDirs(t)
	effective := map[string]any{"allow_tools": []any{"bash"}}
	if !PersistUserOverlayFromEffective(effective, "") {
		t.Fatal("落盘失败")
	}
	user := LoadUserPermissions()
	if !listHasTool(toolList(user, "allow_tools"), "bash") {
		t.Error("bash 应写入 user 层")
	}
}

// TestPersistSessionOverlayFromEffective 测试会话层落盘
func TestPersistSessionOverlayFromEffective(t *testing.T) {
	setupTempPermissionDirs(t)
	const sid = "sess-2"
	effective := map[string]any{"allow_tools": []any{"sh"}}
	if !PersistSessionOverlayFromEffective(sid, effective) {
		t.Fatal("落盘失败")
	}
	session := LoadSessionPermissions(sid)
	if !listHasTool(toolList(session, "allow_tools"), "sh") {
		t.Error("sh 应写入会话层")
	}
	// 空 sessionID
	if PersistSessionOverlayFromEffective("", effective) {
		t.Error("空 sessionID 应返回 false")
	}
}

// TestListSavedSessionIDs 测试列出已存会话
func TestListSavedSessionIDs(t *testing.T) {
	_, sessionsDir := setupTempPermissionDirs(t)
	if got := ListSavedSessionIDs(); len(got) != 0 {
		t.Errorf("空目录应返回空，got %v", got)
	}
	for _, sid := range []string{"b", "a"} {
		dir := filepath.Join(sessionsDir, sid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session_permissions.yaml"), []byte("allow_tools: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 无权限文件的目录不算
	if err := os.MkdirAll(filepath.Join(sessionsDir, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ListSavedSessionIDs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]（已排序，c 无文件不计）", got)
	}
}

// TestPermissionStorageLock_可获取与释放 测试锁的基本可用性
func TestPermissionStorageLock_可获取与释放(t *testing.T) {
	setupTempPermissionDirs(t)
	release, err := PermissionStorageLock("")
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	release()
	// 释放后可再次获取
	release2, err := PermissionStorageLock("sess-x")
	if err != nil {
		t.Fatalf("二次加锁失败: %v", err)
	}
	release2()
}

// TestSessionPermissionsPath_空id报错 测试空 sessionID 返回错误
func TestSessionPermissionsPath_空id报错(t *testing.T) {
	if _, err := SessionPermissionsPath(""); err == nil {
		t.Error("空 sessionID 应返回错误")
	}
	if _, err := SessionPermissionsPath("   "); err == nil {
		t.Error("空白 sessionID 应返回错误")
	}
}

// TestReadPermissionLayersLocked 测试严格读取三层
func TestReadPermissionLayersLocked(t *testing.T) {
	configDir, sessionsDir := setupTempPermissionDirs(t)
	const sid = "sess-r"

	// 造 global / user / session 三层
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"),
		[]byte("permissions:\n  allow_tools:\n    - g1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "user_permissions.yaml"),
		[]byte("allow_tools:\n  - u1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(sessionsDir, sid)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session_permissions.yaml"),
		[]byte("allow_tools:\n  - s1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	global, user, session, err := ReadPermissionLayersLocked(sid, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !listHasTool(toolList(global, "allow_tools"), "g1") {
		t.Error("global 层缺 g1")
	}
	if !listHasTool(toolList(user, "allow_tools"), "u1") {
		t.Error("user 层缺 u1")
	}
	if !listHasTool(toolList(session, "allow_tools"), "s1") {
		t.Error("session 层缺 s1")
	}
}

// TestReadPermissionLayersLocked_传入global 测试显式传 global 时不读文件
func TestReadPermissionLayersLocked_传入global(t *testing.T) {
	setupTempPermissionDirs(t)
	globalGiven := map[string]any{"allow_tools": []any{"from_param"}}
	global, _, _, err := ReadPermissionLayersLocked("", globalGiven)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !listHasTool(toolList(global, "allow_tools"), "from_param") {
		t.Error("应使用传入的 global")
	}
}

// TestCapturePermissionLayers 测试一次加锁读取并合成
func TestCapturePermissionLayers(t *testing.T) {
	configDir, _ := setupTempPermissionDirs(t)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"),
		[]byte("permissions:\n  allow_tools:\n    - g1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	layers, err := CapturePermissionLayers("")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if layers.Global == nil || layers.User == nil || layers.Session == nil {
		t.Error("三层都应为非 nil")
	}
	tools, _ := layers.Effective["tools"].(map[string]any)
	if tools["g1"] != "allow" {
		t.Errorf("effective.tools[g1] = %v, want allow", tools["g1"])
	}
}

// TestSaveUserPermissions 测试覆盖写用户层
func TestSaveUserPermissions(t *testing.T) {
	setupTempPermissionDirs(t)
	if !SaveUserPermissions(map[string]any{"allow_tools": []any{"x"}}) {
		t.Fatal("保存失败")
	}
	user := LoadUserPermissions()
	if !listHasTool(toolList(user, "allow_tools"), "x") {
		t.Error("x 应写入")
	}
}

// TestSaveSessionPermissionsLocked 测试会话层保存
func TestSaveSessionPermissionsLocked(t *testing.T) {
	setupTempPermissionDirs(t)
	const sid = "sess-save"
	if !SaveSessionPermissionsLocked(sid, map[string]any{"allow_tools": []any{"y"}}) {
		t.Fatal("保存失败")
	}
	session := LoadSessionPermissions(sid)
	if !listHasTool(toolList(session, "allow_tools"), "y") {
		t.Error("y 应写入")
	}
	// 空 sessionID 失败
	if SaveSessionPermissionsLocked("", map[string]any{}) {
		t.Error("空 sessionID 应返回 false")
	}
}

// TestUpdateUserPermissions 测试在锁内做一次变更
func TestUpdateUserPermissions(t *testing.T) {
	setupTempPermissionDirs(t)
	ok := UpdateUserPermissions(func(current map[string]any) map[string]any {
		current["allow_tools"] = []any{"mutated"}
		return current
	})
	if !ok {
		t.Fatal("更新失败")
	}
	user := LoadUserPermissions()
	if !listHasTool(toolList(user, "allow_tools"), "mutated") {
		t.Error("变更未生效")
	}
}

// TestRenameAndDedupOverrides_冲突重命名 测试与更低层冲突时用稳定 id
func TestRenameAndDedupOverrides_冲突重命名(t *testing.T) {
	overrides := []map[string]any{
		{"id": "clash", "tools": []any{"bash"}, "pattern": "rm *", "action": "allow"},
	}
	baseIDs := map[string]struct{}{"clash": {}}
	got := renameAndDedupOverrides(overrides, baseIDs)
	if len(got) != 1 {
		t.Fatalf("got %d 条, want 1", len(got))
	}
	item, _ := got[0].(map[string]any)
	newID, _ := item["id"].(string)
	if newID == "clash" {
		t.Error("与更低层冲突的 id 应被重命名")
	}
	if !strings.HasPrefix(newID, "user_allow_") {
		t.Errorf("新 id = %q，应为稳定 id 形态", newID)
	}
}

// TestRenameAndDedupOverrides_批次内重复 测试同批次重复 id 也重命名
func TestRenameAndDedupOverrides_批次内重复(t *testing.T) {
	overrides := []map[string]any{
		{"id": "dup", "tools": []any{"bash"}, "pattern": "a", "action": "allow"},
		{"id": "dup", "tools": []any{"sh"}, "pattern": "b", "action": "allow"},
	}
	got := renameAndDedupOverrides(overrides, nil)
	ids := make(map[string]struct{}, len(got))
	for _, item := range got {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		if _, dup := ids[id]; dup {
			t.Errorf("id %q 重复", id)
		}
		ids[id] = struct{}{}
	}
}

// TestComposeHostEffectivePermissions_整工具deny优先 测试 deny 不可被放宽
func TestComposeHostEffectivePermissions_整工具deny优先(t *testing.T) {
	global := map[string]any{"deny_tools": []any{"danger"}}
	user := map[string]any{"allow_tools": []any{"danger"}}
	got := ComposeHostEffectivePermissions(global, user, nil, "", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["danger"] != "deny" {
		t.Errorf("danger = %v, want deny（user allow 不能放宽 global deny）", tools["danger"])
	}
}

// TestComposeHostEffectivePermissions_allow放宽ask 测试 allow 可放宽 ask
func TestComposeHostEffectivePermissions_allow放宽ask(t *testing.T) {
	global := map[string]any{"ask_tools": []any{"tool1"}}
	user := map[string]any{"allow_tools": []any{"tool1"}}
	got := ComposeHostEffectivePermissions(global, user, nil, "", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["tool1"] != "allow" {
		t.Errorf("tool1 = %v, want allow（user allow 放宽 global ask）", tools["tool1"])
	}
}

// TestComposeHostEffectivePermissions_session层丢弃ask与deny 测试 session 只贡献 allow
func TestComposeHostEffectivePermissions_session层丢弃ask与deny(t *testing.T) {
	session := map[string]any{
		"allow_tools": []any{"allowed"},
		"ask_tools":   []any{"asked"},
		"deny_tools":  []any{"denied"},
	}
	got := ComposeHostEffectivePermissions(nil, nil, session, "", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["allowed"] != "allow" {
		t.Errorf("allowed = %v, want allow", tools["allowed"])
	}
	if _, has := tools["asked"]; has {
		t.Error("session 的 ask 不应参与合成")
	}
	if _, has := tools["denied"]; has {
		t.Error("session 的 deny 不应参与合成")
	}
}
