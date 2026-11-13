package permissions

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ---------- normalizeToolArgs 测试 ----------

func TestNormalizeToolArgs(t *testing.T) {
	t.Run("nil输入返回空map", func(t *testing.T) {
		got := normalizeToolArgs(nil)
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("map输入原样返回", func(t *testing.T) {
		input := map[string]any{"key": "value", "num": 42}
		got := normalizeToolArgs(input)
		if len(got) != 2 || got["key"] != "value" || got["num"] != 42 {
			t.Errorf("期望原样返回，得到 %v", got)
		}
	})
	t.Run("空map输入返回空map", func(t *testing.T) {
		got := normalizeToolArgs(map[string]any{})
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("有效JSON字符串解析成功", func(t *testing.T) {
		input := `{"name":"test","count":3}`
		got := normalizeToolArgs(input)
		if len(got) != 2 || got["name"] != "test" {
			t.Errorf("期望解析后的 map，得到 %v", got)
		}
	})
	t.Run("无效JSON字符串返回空map", func(t *testing.T) {
		input := "not a json"
		got := normalizeToolArgs(input)
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("空字符串返回空map", func(t *testing.T) {
		got := normalizeToolArgs("")
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("空白字符串返回空map", func(t *testing.T) {
		got := normalizeToolArgs("   ")
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("有效JSON字节切片解析成功", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"path": "/tmp"})
		got := normalizeToolArgs(input)
		if len(got) != 1 || got["path"] != "/tmp" {
			t.Errorf("期望解析后的 map，得到 %v", got)
		}
	})
	t.Run("无效JSON字节切片返回空map", func(t *testing.T) {
		got := normalizeToolArgs([]byte("{invalid}"))
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("其他类型返回空map", func(t *testing.T) {
		got := normalizeToolArgs(42)
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
}

// ---------- ensurePermissionsDict 测试 ----------

func TestEnsurePermissionsDict(t *testing.T) {
	t.Run("nil输入返回空map", func(t *testing.T) {
		got := ensurePermissionsDict(nil)
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
	})
	t.Run("无permissions键时创建空map", func(t *testing.T) {
		data := map[string]any{"other": "value"}
		got := ensurePermissionsDict(data)
		if len(got) != 0 {
			t.Errorf("期望空 map，得到 %v", got)
		}
		// 验证 data["permissions"] 已被设置
		if _, ok := data["permissions"].(map[string]any); !ok {
			t.Error("期望 data[\"permissions\"] 被创建")
		}
	})
	t.Run("已有permissions键时原样返回", func(t *testing.T) {
		perms := map[string]any{"schema": "tiered_policy"}
		data := map[string]any{"permissions": perms}
		got := ensurePermissionsDict(data)
		if got["schema"] != "tiered_policy" {
			t.Errorf("期望已有 permissions，得到 %v", got)
		}
	})
	t.Run("permissions键为非map类型时覆盖", func(t *testing.T) {
		data := map[string]any{"permissions": "invalid"}
		got := ensurePermissionsDict(data)
		if len(got) != 0 {
			t.Errorf("期望覆盖为空 map，得到 %v", got)
		}
	})
}

// ---------- ensureExternalDirectoryDict 测试 ----------

func TestEnsureExternalDirectoryDict(t *testing.T) {
	t.Run("无external_directory键时创建默认配置", func(t *testing.T) {
		perms := map[string]any{}
		got := ensureExternalDirectoryDict(perms)
		if got["*"] != "ask" {
			t.Errorf("期望默认 \"*\": \"ask\"，得到 %v", got)
		}
		// 验证 perms 已被设置
		if ext, ok := perms["external_directory"].(map[string]any); !ok || ext["*"] != "ask" {
			t.Error("期望 perms[\"external_directory\"] 被创建")
		}
	})
	t.Run("已有external_directory键时原样返回", func(t *testing.T) {
		extCfg := map[string]any{"/home": "allow"}
		perms := map[string]any{"external_directory": extCfg}
		got := ensureExternalDirectoryDict(perms)
		if got["/home"] != "allow" {
			t.Errorf("期望已有 external_directory，得到 %v", got)
		}
	})
}

// ---------- ensureApprovalOverridesList 测试 ----------

func TestEnsureApprovalOverridesList(t *testing.T) {
	t.Run("无approval_overrides键时创建空切片", func(t *testing.T) {
		perms := map[string]any{}
		got := ensureApprovalOverridesList(perms)
		if len(got) != 0 {
			t.Errorf("期望空切片，得到 %v", got)
		}
		// 验证 perms 已被设置
		if _, ok := perms["approval_overrides"].([]any); !ok {
			t.Error("期望 perms[\"approval_overrides\"] 被创建")
		}
	})
	t.Run("已有有效approval_overrides时返回过滤结果", func(t *testing.T) {
		overrides := []any{
			map[string]any{"id": "rule1", "action": "allow"},
			"invalid_item",
			map[string]any{"id": "rule2", "action": "deny"},
		}
		perms := map[string]any{"approval_overrides": overrides}
		got := ensureApprovalOverridesList(perms)
		if len(got) != 2 {
			t.Errorf("期望 2 项，得到 %d 项", len(got))
		}
	})
	t.Run("空approval_overrides切片返回空", func(t *testing.T) {
		perms := map[string]any{"approval_overrides": []any{}}
		got := ensureApprovalOverridesList(perms)
		if len(got) != 0 {
			t.Errorf("期望空切片，得到 %v", got)
		}
	})
	t.Run("全部为非map项时返回空", func(t *testing.T) {
		overrides := []any{"str1", 42, true}
		perms := map[string]any{"approval_overrides": overrides}
		got := ensureApprovalOverridesList(perms)
		if len(got) != 0 {
			t.Errorf("期望空切片，得到 %d 项", len(got))
		}
	})
}

// ---------- hasOverrideID 测试 ----------

func TestHasOverrideID(t *testing.T) {
	t.Run("空切片返回false", func(t *testing.T) {
		if hasOverrideID(nil, "rule1") {
			t.Error("期望 false")
		}
	})
	t.Run("存在指定ID返回true", func(t *testing.T) {
		overrides := []map[string]any{
			{"id": "rule1"},
			{"id": "rule2"},
		}
		if !hasOverrideID(overrides, "rule1") {
			t.Error("期望 true")
		}
	})
	t.Run("不存在指定ID返回false", func(t *testing.T) {
		overrides := []map[string]any{
			{"id": "rule1"},
		}
		if hasOverrideID(overrides, "rule99") {
			t.Error("期望 false")
		}
	})
	t.Run("id字段为非字符串时按字符串转换匹配", func(t *testing.T) {
		overrides := []map[string]any{
			{"id": 123},
		}
		// StrVal 会将 123 转为 "123"
		if !hasOverrideID(overrides, "123") {
			t.Error("期望 StrVal 转换后匹配")
		}
	})
}

// ---------- appendOverrideIfMissing 测试 ----------

func TestAppendOverrideIfMissing(t *testing.T) {
	t.Run("ID不存在时追加", func(t *testing.T) {
		perms := map[string]any{"approval_overrides": []any{}}
		overrides := []map[string]any{}
		appendOverrideIfMissing(perms, overrides, "rule1", []string{"bash"}, "command", "re:.*", "allow", "cli")
		ovRaw, _ := perms["approval_overrides"].([]any)
		if len(ovRaw) != 1 {
			t.Fatalf("期望 1 项，得到 %d 项", len(ovRaw))
		}
		item, _ := ovRaw[0].(map[string]any)
		if item["id"] != "rule1" {
			t.Errorf("期望 id=rule1，得到 %v", item["id"])
		}
		if item["action"] != "allow" {
			t.Errorf("期望 action=allow，得到 %v", item["action"])
		}
	})
	t.Run("ID已存在时不追加", func(t *testing.T) {
		existing := map[string]any{"id": "rule1"}
		perms := map[string]any{"approval_overrides": []any{existing}}
		overrides := []map[string]any{existing}
		appendOverrideIfMissing(perms, overrides, "rule1", []string{"bash"}, "command", "re:.*", "allow", "cli")
		ovRaw, _ := perms["approval_overrides"].([]any)
		if len(ovRaw) != 1 {
			t.Errorf("期望仍为 1 项，得到 %d 项", len(ovRaw))
		}
	})
	t.Run("approval_overrides为nil时创建新切片", func(t *testing.T) {
		perms := map[string]any{}
		overrides := []map[string]any{}
		appendOverrideIfMissing(perms, overrides, "rule1", []string{"read"}, "path", "/tmp", "allow", "test")
		ovRaw, _ := perms["approval_overrides"].([]any)
		if len(ovRaw) != 1 {
			t.Fatalf("期望 1 项，得到 %d 项", len(ovRaw))
		}
	})
	t.Run("追加内容字段完整", func(t *testing.T) {
		perms := map[string]any{"approval_overrides": []any{}}
		overrides := []map[string]any{}
		appendOverrideIfMissing(perms, overrides, "oid", []string{"bash", "grep"}, "command", "re:.*tmp.*", "allow", "cli_add_dir")
		ovRaw, _ := perms["approval_overrides"].([]any)
		item, _ := ovRaw[0].(map[string]any)
		// 逐字段校验
		if item["id"] != "oid" {
			t.Errorf("字段 id: 期望 oid，得到 %v", item["id"])
		}
		if item["match_type"] != "command" {
			t.Errorf("字段 match_type: 期望 command，得到 %v", item["match_type"])
		}
		if item["pattern"] != "re:.*tmp.*" {
			t.Errorf("字段 pattern: 期望 re:.*tmp.*，得到 %v", item["pattern"])
		}
		if item["action"] != "allow" {
			t.Errorf("字段 action: 期望 allow，得到 %v", item["action"])
		}
		if item["source"] != "cli_add_dir" {
			t.Errorf("字段 source: 期望 cli_add_dir，得到 %v", item["source"])
		}
		// tools 字段需要类型断言后逐个比较
		tools, ok := item["tools"].([]string)
		if !ok {
			t.Fatalf("字段 tools: 期望 []string 类型")
		}
		expectedTools := []string{"bash", "grep"}
		if len(tools) != len(expectedTools) {
			t.Fatalf("字段 tools: 期望 %v，得到 %v", expectedTools, tools)
		}
		for i, v := range expectedTools {
			if tools[i] != v {
				t.Errorf("字段 tools[%d]: 期望 %s，得到 %s", i, v, tools[i])
			}
		}
	})
}

// ---------- sortedSet 测试 ----------

func TestSortedSet(t *testing.T) {
	t.Run("空切片返回空", func(t *testing.T) {
		got := sortedSet(nil)
		if len(got) != 0 {
			t.Errorf("期望空切片，得到 %v", got)
		}
	})
	t.Run("无重复项排序", func(t *testing.T) {
		got := sortedSet([]string{"bash", "read", "grep"})
		expected := []string{"bash", "grep", "read"}
		if len(got) != len(expected) {
			t.Fatalf("期望 %v，得到 %v", expected, got)
		}
		for i, v := range expected {
			if got[i] != v {
				t.Errorf("索引 %d: 期望 %s，得到 %s", i, v, got[i])
			}
		}
	})
	t.Run("有重复项去重排序", func(t *testing.T) {
		got := sortedSet([]string{"bash", "grep", "bash", "read", "grep"})
		expected := []string{"bash", "grep", "read"}
		if len(got) != len(expected) {
			t.Fatalf("期望 %v，得到 %v", expected, got)
		}
		for i, v := range expected {
			if got[i] != v {
				t.Errorf("索引 %d: 期望 %s，得到 %s", i, v, got[i])
			}
		}
	})
	t.Run("全部相同项去重为一个", func(t *testing.T) {
		got := sortedSet([]string{"bash", "bash", "bash"})
		if len(got) != 1 || got[0] != "bash" {
			t.Errorf("期望 [bash]，得到 %v", got)
		}
	})
	t.Run("结果已排序", func(t *testing.T) {
		got := sortedSet([]string{"z", "a", "m", "b"})
		if !sort.StringsAreSorted(got) {
			t.Errorf("期望结果已排序，得到 %v", got)
		}
	})
}

// ---------- expandAndResolvePath 测试 ----------

func TestExpandAndResolvePath(t *testing.T) {
	t.Run("空白字符串返回空", func(t *testing.T) {
		got := expandAndResolvePath("   ")
		if got != "" {
			t.Errorf("期望空字符串，得到 %s", got)
		}
	})
	t.Run("空字符串返回空", func(t *testing.T) {
		got := expandAndResolvePath("")
		if got != "" {
			t.Errorf("期望空字符串，得到 %s", got)
		}
	})
	t.Run("绝对路径原样返回（去除尾部斜杠）", func(t *testing.T) {
		got := expandAndResolvePath("/tmp/test/")
		if got != "/tmp/test" {
			t.Errorf("期望 /tmp/test，得到 %s", got)
		}
	})
	t.Run("绝对路径不带尾部斜杠", func(t *testing.T) {
		got := expandAndResolvePath("/tmp/test")
		if got != "/tmp/test" {
			t.Errorf("期望 /tmp/test，得到 %s", got)
		}
	})
	t.Run("~展开为主目录", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil || homeDir == "" {
			t.Skip("无法获取用户主目录")
		}
		got := expandAndResolvePath("~/project")
		expected := homeDir + "/project"
		if got != expected {
			t.Errorf("期望 %s，得到 %s", expected, got)
		}
	})
	t.Run("路径使用正斜杠格式", func(t *testing.T) {
		got := expandAndResolvePath("/tmp/test")
		if strings.Contains(got, "\\") {
			t.Errorf("期望 POSIX 格式（正斜杠），得到 %s", got)
		}
	})
}

// ---------- PersistCliTrustedDirectoryWithOverrides 测试 ----------

func TestPersistCliTrustedDirectoryWithOverrides(t *testing.T) {
	t.Run("空路径返回错误", func(t *testing.T) {
		got := PersistCliTrustedDirectoryWithOverrides("")
		if got["ok"] != false {
			t.Errorf("期望 ok=false，得到 %v", got["ok"])
		}
		if got["error"] != "path is empty" {
			t.Errorf("期望 error=path is empty，得到 %v", got["error"])
		}
	})
	t.Run("空白路径返回错误", func(t *testing.T) {
		got := PersistCliTrustedDirectoryWithOverrides("   ")
		if got["ok"] != false {
			t.Errorf("期望 ok=false，得到 %v", got["ok"])
		}
	})
	t.Run("有效绝对路径返回成功", func(t *testing.T) {
		got := PersistCliTrustedDirectoryWithOverrides("/tmp/myproject")
		if got["ok"] != true {
			t.Errorf("期望 ok=true，得到 %v", got["ok"])
		}
		if got["normalized"] != "/tmp/myproject" {
			t.Errorf("期望 normalized=/tmp/myproject，得到 %v", got["normalized"])
		}
		// 验证 path_pattern 和 shell_pattern
		pathPattern, _ := got["path_pattern"].(string)
		shellPattern, _ := got["shell_pattern"].(string)
		if !strings.HasPrefix(pathPattern, "re:^") {
			t.Errorf("期望 path_pattern 以 re:^ 开头，得到 %s", pathPattern)
		}
		if !strings.HasPrefix(shellPattern, "re:") {
			t.Errorf("期望 shell_pattern 以 re: 开头，得到 %s", shellPattern)
		}
	})
	t.Run("路径含正则特殊字符时转义", func(t *testing.T) {
		got := PersistCliTrustedDirectoryWithOverrides("/tmp/project.name")
		pathPattern, _ := got["path_pattern"].(string)
		// . 应被转义为 \.
		if !strings.Contains(pathPattern, `\.`) {
			t.Errorf("期望 path_pattern 中 . 被转义，得到 %s", pathPattern)
		}
	})
	t.Run("返回结果包含tiered_overrides字段", func(t *testing.T) {
		got := PersistCliTrustedDirectoryWithOverrides("/tmp/testdir")
		if _, ok := got["tiered_overrides"]; !ok {
			t.Error("期望返回结果包含 tiered_overrides 字段")
		}
	})
}

// ---------- PersistPermissionAllowRule 纯逻辑部分测试 ----------

func TestNormalizeToolArgs_集成PersistPermissionAllowRule(t *testing.T) {
	t.Run("map[string]any参数直接传入normalizeToolArgs", func(t *testing.T) {
		// 验证 normalizeToolArgs 被 PersistPermissionAllowRule 使用时行为正确
		args := map[string]any{"path": "/tmp/file"}
		got := normalizeToolArgs(args)
		if got["path"] != "/tmp/file" {
			t.Errorf("期望 path=/tmp/file，得到 %v", got["path"])
		}
	})
}

// ---------- 额外边界条件测试 ----------

func TestSortedSet_大输入(t *testing.T) {
	items := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		items[i] = fmt.Sprintf("item_%04d", i%100)
	}
	got := sortedSet(items)
	if len(got) != 100 {
		t.Errorf("期望 100 项（去重后），得到 %d 项", len(got))
	}
	if !sort.StringsAreSorted(got) {
		t.Error("期望结果已排序")
	}
}

func TestEnsurePermissionsDict_返回值与data同步(t *testing.T) {
	data := map[string]any{}
	perms := ensurePermissionsDict(data)
	perms["new_key"] = "new_value"
	// 因为返回的是同一个 map 引用，data["permissions"] 也应更新
	dataPerms, _ := data["permissions"].(map[string]any)
	if dataPerms["new_key"] != "new_value" {
		t.Error("返回的 map 应与 data[\"permissions\"] 是同一引用")
	}
}

func TestEnsureExternalDirectoryDict_返回值与permissions同步(t *testing.T) {
	perms := map[string]any{}
	extCfg := ensureExternalDirectoryDict(perms)
	extCfg["/custom"] = "allow"
	dataExt, _ := perms["external_directory"].(map[string]any)
	if dataExt["/custom"] != "allow" {
		t.Error("返回的 map 应与 permissions[\"external_directory\"] 是同一引用")
	}
}

func TestAppendOverrideIfMissing_多次追加不同ID(t *testing.T) {
	perms := map[string]any{"approval_overrides": []any{}}
	overrides := []map[string]any{}
	appendOverrideIfMissing(perms, overrides, "rule_a", []string{"bash"}, "command", "re:.*", "allow", "cli")
	// 需要重新获取 overrides 以反映最新数据
	ovRaw, _ := perms["approval_overrides"].([]any)
	updatedOverrides := make([]map[string]any, 0, len(ovRaw))
	for _, item := range ovRaw {
		if m, ok := item.(map[string]any); ok {
			updatedOverrides = append(updatedOverrides, m)
		}
	}
	appendOverrideIfMissing(perms, updatedOverrides, "rule_b", []string{"read"}, "path", "/tmp", "allow", "cli")
	ovRaw2, _ := perms["approval_overrides"].([]any)
	if len(ovRaw2) != 2 {
		t.Errorf("期望 2 项，得到 %d 项", len(ovRaw2))
	}
}

// init 确保测试中 rand 已就绪
func init() {
	_, _ = rand.Read(make([]byte, 1))
}

// ──────────────────────────── BuildCommandAllowPattern 测试分组 ────────────────────────────

// TestBuildCommandAllowPattern 测试命令通配符构造。
//
// Python: return cmd.strip() + " *"（注意尾部是 " *" 不是 "*"）
func TestBuildCommandAllowPattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"npm test", "npm test *"},
		{"  npm test  ", "npm test *"},
		{"ls", "ls *"},
		{"", " *"},
	}
	for _, c := range cases {
		if got := BuildCommandAllowPattern(c.in); got != c.want {
			t.Errorf("BuildCommandAllowPattern(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ──────────────────────────── NormalizeExactAccesses 测试分组 ────────────────────────────

// TestNormalizeExactAccesses_合法输入 测试归一化与去重保序。
func TestNormalizeExactAccesses_合法输入(t *testing.T) {
	in := [][2]string{
		{"/data/a/", "READ"},
		{`C:\data\b\`, "write"},
		{"/data/a", "read"},
	}
	got, ok := NormalizeExactAccesses(in)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（去重后）", len(got))
	}
	if got[0][0] != "/data/a" || got[0][1] != "read" {
		t.Errorf("got[0] = %v", got[0])
	}
	if got[1][0] != "C:/data/b" || got[1][1] != "write" {
		t.Errorf("got[1] = %v（反斜杠应转正斜杠）", got[1])
	}
}

// TestNormalizeExactAccesses_非法输入 测试任一不合格整体失败。
//
// Python: 返回 None 表示整体非法，调用方应拒绝（不是跳过该项）
func TestNormalizeExactAccesses_非法输入(t *testing.T) {
	cases := map[string][][2]string{
		"空 path":     {{"", "read"}},
		"非法 action":  {{"/data/a", "delete"}},
		"path 全是斜杠":  {{"///", "read"}},
		"action 为空":  {{"/data/a", ""}},
		"action 仅空格": {{"/data/a", "  "}},
	}
	for name, in := range cases {
		if _, ok := NormalizeExactAccesses(in); ok {
			t.Errorf("%s: ok = true, want false", name)
		}
	}
}

// TestNormalizeExactAccesses_空切片 测试空输入返回空且 true。
func TestNormalizeExactAccesses_空切片(t *testing.T) {
	got, ok := NormalizeExactAccesses(nil)
	if !ok {
		t.Fatal("ok = false, want true（空输入合法）")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ──────────────────────────── MergeFileGuardPathIntoPermissions 测试分组 ────────────────────────────

// TestMergeFileGuardPathIntoPermissions 测试原地改写语义。
//
// Python 用 permissions.clear() + permissions.update(merged)：
// 改的是同一 dict 引用，供调用方 dump。
func TestMergeFileGuardPathIntoPermissions(t *testing.T) {
	perms := map[string]any{}
	wrote := MergeFileGuardPathIntoPermissions(perms, "/data/ws", "allow", "allow", "ask")
	if !wrote {
		t.Fatal("wrote = false, want true")
	}
	fg, _ := perms["file_guard"].(map[string]any)
	if fg == nil {
		t.Fatal("file_guard 必须写回同一 map 引用（Python 用 clear+update）")
	}
	paths, _ := fg["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths len = %d, want 1", len(paths))
	}
}

// TestMergeFileGuardPathIntoPermissions_保留已有键 测试原地改写的完整性。
func TestMergeFileGuardPathIntoPermissions_保留已有键(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	_ = MergeFileGuardPathIntoPermissions(perms, "/data/ws", "allow", "ask", "ask")

	if _, exists := perms["tools"]; !exists {
		t.Error("原有 tools 键丢失（clear+update 应保留全部内容）")
	}
	if _, exists := perms["file_guard"]; !exists {
		t.Error("file_guard 未写入")
	}
}

// ──────────────────────────── ErrPermissionApprovalInvalidated 测试分组 ────────────────────────────

// TestErrPermissionApprovalInvalidated_errorsIs识别 测试哨兵语义。
func TestErrPermissionApprovalInvalidated_errorsIs识别(t *testing.T) {
	wrapped := fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_accesses_changed")
	if !errors.Is(wrapped, ErrPermissionApprovalInvalidated) {
		t.Error("errors.Is 识别失败")
	}
}

// ──────────────────────────── CheckApprovalAccesses 测试分组 ────────────────────────────

// TestCheckApprovalAccesses_deny抛错 测试策略 deny 时抛哨兵。
func TestCheckApprovalAccesses_deny抛错(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "deny"},
	}
	_, _, err := CheckApprovalAccesses(perms, "bash", map[string]any{"command": "ls"}, "")
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestCheckApprovalAccesses_allow返回级别 测试正常路径。
func TestCheckApprovalAccesses_allow返回级别(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	level, _, err := CheckApprovalAccesses(perms, "bash", map[string]any{"command": "ls"}, "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if level != "allow" {
		t.Errorf("level = %q, want allow", level)
	}
}

// TestCheckApprovalAccesses_ask返回级别 测试 ask 级别不抛错。
func TestCheckApprovalAccesses_ask返回级别(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "ask"},
	}
	level, _, err := CheckApprovalAccesses(perms, "bash", map[string]any{"command": "ls"}, "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if level != "ask" {
		t.Errorf("level = %q, want ask", level)
	}
}

// TestCheckApprovalAccesses_sendFile不可评估抛错 测试 unevaluable 分支。
func TestCheckApprovalAccesses_sendFile不可评估抛错(t *testing.T) {
	perms := map[string]any{}
	_, _, err := CheckApprovalAccesses(perms, SendFileToolName, map[string]any{}, "")
	if err == nil {
		t.Fatal("err = nil, want invalidated（send_file 缺 abs_file_path_list 应 unevaluable）")
	}
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// ──────────────────────────── PersistExactPermissionAllowRule 测试分组 ────────────────────────────

// TestPersistExactPermissionAllowRule_非法scope 测试空 name 抛错。
func TestPersistExactPermissionAllowRule_非法scope(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "",
		ToolArgs:       map[string]any{},
		RaiseOnInvalid: true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_非法action 测试 action 非 read/write/exec 抛错。
func TestPersistExactPermissionAllowRule_非法action(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		AskAccesses:    [][2]string{{"/data/a", "delete"}},
		RaiseOnInvalid: true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_sessionOnly缺sessionID 测试参数校验。
func TestPersistExactPermissionAllowRule_sessionOnly缺sessionID(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		SessionOnly:    true,
		SessionID:      "",
		RaiseOnInvalid: true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_raiseOnInvalid为false静默 测试静默模式。
func TestPersistExactPermissionAllowRule_raiseOnInvalid为false静默(t *testing.T) {
	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "",
		RaiseOnInvalid: false,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil（raise_on_invalid=false 应静默）", err)
	}
}

// ──────────────────────────── PersistExactPermissionAllowRule 主逻辑覆盖 ────────────────────────────

// setupPersistEnv 把配置目录与配置 YAML 都指到临时目录，返回各路径。
func setupPersistEnv(t *testing.T, configYAML string) (configDir string, configPath string) {
	t.Helper()
	configDir = t.TempDir()
	configPath = filepath.Join(configDir, "config.yaml")
	if configYAML == "" {
		configYAML = "permissions:\n  tools:\n    bash: ask\n"
	}
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("UAPCLAW_CONFIG_DIR", configDir)
	t.Setenv("UAPCLAW_CONFIG_YAML", configPath)
	t.Setenv("UAPCLAW_SESSIONS_DIR", filepath.Join(configDir, "sessions"))
	return configDir, configPath
}

// readWrittenConfig 读回写入的配置。
func readWrittenConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return parsed
}

// TestPersistExactPermissionAllowRule_写入user层 测试 ask 工具落盘产生 approval_override。
//
// 注意两点：
//  1. User 层落盘到 <configDir>/user_permissions.yaml，不是 config.yaml
//     （config.yaml 是 global 层，只读不写）
//  2. shell 命令走 approval_overrides 精确模式（tools[name] 未变时不加 allow_tools，
//     对齐 Python 的 `if merged.tools[name] != baseline.tools[name]` 判据）
func TestPersistExactPermissionAllowRule_写入user层(t *testing.T) {
	configDir, _ := setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		WorkspaceRoot:  t.TempDir(),
		RaiseOnInvalid: true,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil（ask → 应写入放行增量）", err)
	}

	written := readWrittenConfig(t, filepath.Join(configDir, "user_permissions.yaml"))
	overrides, _ := written["approval_overrides"].([]any)
	if len(overrides) == 0 {
		t.Fatalf("user_permissions.approval_overrides 为空，实际 %v", written)
	}
	first, _ := overrides[0].(map[string]any)
	if first["pattern"] != "ls" {
		t.Errorf("override.pattern = %v, want ls", first["pattern"])
	}
	if first["action"] != "allow" {
		t.Errorf("override.action = %v, want allow", first["action"])
	}
}

// TestPersistExactPermissionAllowRule_validateOnly不写盘 测试只校验不落盘。
func TestPersistExactPermissionAllowRule_validateOnly不写盘(t *testing.T) {
	_, configPath := setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	err = PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:      "bash",
		ToolArgs:      map[string]any{"command": "ls"},
		WorkspaceRoot: t.TempDir(),
		ValidateOnly:  true,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(before) != string(after) {
		t.Error("validateOnly 不应改动配置文件")
	}
}

// TestPersistExactPermissionAllowRule_installedPolicyChanged 测试全局快照不一致时抛错。
func TestPersistExactPermissionAllowRule_installedPolicyChanged(t *testing.T) {
	setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:      "bash",
		ToolArgs:      map[string]any{"command": "ls"},
		WorkspaceRoot: t.TempDir(),
		ExpectedGlobalPermissions: map[string]any{
			"tools": map[string]any{"bash": "deny"},
		},
		RaiseOnInvalid: true,
	})
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
	if !strings.Contains(err.Error(), "installed_policy_changed") {
		t.Errorf("err 应含 installed_policy_changed，实际 %v", err)
	}
}

// TestPersistExactPermissionAllowRule_installedPolicy提示但validateOnly 测试只校验时不抛该错。
func TestPersistExactPermissionAllowRule_installedPolicy提示但validateOnly(t *testing.T) {
	setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:      "bash",
		ToolArgs:      map[string]any{"command": "ls"},
		WorkspaceRoot: t.TempDir(),
		ExpectedGlobalPermissions: map[string]any{
			"tools": map[string]any{"bash": "deny"},
		},
		ValidateOnly:   true,
		RaiseOnInvalid: true,
	})
	// validateOnly 时不因策略变化抛错，但仍可能因后续校验失败
	if err != nil && strings.Contains(err.Error(), "installed_policy_changed") {
		t.Errorf("validateOnly 不应抛 installed_policy_changed，实际 %v", err)
	}
}

// TestPersistExactPermissionAllowRule_approvalAccessesChanged 测试 asks 未被审批覆盖时抛错。
//
// 配置层已 allow，但审批只确认了空集合 → current_asks 非空 → 子集检查失败。
func TestPersistExactPermissionAllowRule_approvalAccessesChanged(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "outside.txt")
	setupPersistEnv(t, `permissions:
  tools:
    read_file: ask
  file_guard:
    enabled: true
    paths:
      - path: "`+ws+`"
        read: ask
        write: ask
        exec: ask
        match: prefix
`)

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "read_file",
		ToolArgs:       map[string]any{"file_path": target},
		WorkspaceRoot:  ws,
		AskAccesses:    [][2]string{},
		RaiseOnInvalid: true,
	})
	if err == nil {
		t.Fatal("err = nil, want 错误（审批范围未覆盖实际 asks）")
	}
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_sessionOnly写session层 测试写 session 文件。
func TestPersistExactPermissionAllowRule_sessionOnly写session层(t *testing.T) {
	configDir, _ := setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		WorkspaceRoot:  t.TempDir(),
		SessionOnly:    true,
		SessionID:      "sess-1",
		RaiseOnInvalid: true,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	sessionPath := filepath.Join(configDir, "sessions", "sess-1", "session_permissions.yaml")
	if _, statErr := os.Stat(sessionPath); statErr != nil {
		t.Errorf("session 权限文件应存在：%v", statErr)
	}
}

// TestPersistExactPermissionAllowRule_幂等 测试重复调用结果一致。
func TestPersistExactPermissionAllowRule_幂等(t *testing.T) {
	setupPersistEnv(t, "permissions:\n  tools:\n    bash: ask\n")
	ws := t.TempDir()
	opts := ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		WorkspaceRoot:  ws,
		RaiseOnInvalid: true,
	}

	if err := PersistExactPermissionAllowRule(opts); err != nil {
		t.Fatalf("第一次 err = %v", err)
	}
	if err := PersistExactPermissionAllowRule(opts); err != nil {
		t.Fatalf("第二次 err = %v（幂等应返回 nil）", err)
	}
}

// TestPersistExactPermissionAllowRule_autoMode跳过durable检查 测试 auto_mode 分支。
//
// Python: if not auto_mode and not session_only and current_level == ALLOW
// auto_mode 为真时不走 durable 短路，继续走增量路径。
func TestPersistExactPermissionAllowRule_autoMode跳过durable检查(t *testing.T) {
	setupPersistEnv(t, "permissions:\n  tools:\n    bash: allow\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		WorkspaceRoot:  t.TempDir(),
		AutoMode:       true,
		RaiseOnInvalid: true,
	})
	// auto_mode 下当前已 allow，走完增量后 proposed 仍 allow → 返回 nil
	if err != nil && !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want nil 或 invalidated", err)
	}
}

// TestPersistExactPermissionAllowRule_路径授权写入fileguard 测试 targetAsks 非空路径。
//
// read_file 的路径守卫产生 asks，审批覆盖后应写进 file_guard.paths。
func TestPersistExactPermissionAllowRule_路径授权写入fileguard(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "inside.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	configDir, _ := setupPersistEnv(t, `permissions:
  tools:
    read_file: ask
  file_guard:
    enabled: true
    paths:
      - path: "`+ws+`"
        read: ask
        write: ask
        exec: ask
        match: prefix
`)

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "read_file",
		ToolArgs:       map[string]any{"file_path": target},
		WorkspaceRoot:  ws,
		AskAccesses:    [][2]string{{target, "read"}},
		RaiseOnInvalid: true,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil（审批已覆盖 asks 应成功）", err)
	}

	written := readWrittenConfig(t, filepath.Join(configDir, "user_permissions.yaml"))
	overrides, _ := written["approval_overrides"].([]any)
	paths, _ := written["file_guard"].(map[string]any)
	hasPath := paths != nil && paths["paths"] != nil
	if len(overrides) == 0 && !hasPath {
		t.Errorf("应写入 approval_overrides 或 file_guard.paths，实际 %v", written)
	}
}

// TestPersistExactPermissionAllowRule_approvalNotCovered 测试最终校验失败。
//
// 审批确认的访问集合与实际 asks 不一致时，最终 compose 校验不通过。
func TestPersistExactPermissionAllowRule_approvalNotCovered(t *testing.T) {
	setupPersistEnv(t, "permissions:\n  tools:\n    bash: deny\n")

	err := PersistExactPermissionAllowRule(ExactAllowOptions{
		ToolName:       "bash",
		ToolArgs:       map[string]any{"command": "ls"},
		WorkspaceRoot:  t.TempDir(),
		RaiseOnInvalid: true,
	})
	if err == nil {
		t.Fatal("err = nil, want 错误（deny 策略无法通过审批）")
	}
	if !errors.Is(err, ErrPermissionApprovalInvalidated) {
		t.Fatalf("err = %v, want ErrPermissionApprovalInvalidated", err)
	}
}
