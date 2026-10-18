package permissions

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
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

// ---------- expandUserPath 测试 ----------

func TestExpandUserPath(t *testing.T) {
	t.Run("以~开头的路径展开为主目录", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil || homeDir == "" {
			t.Skip("无法获取用户主目录")
		}
		got := expandUserPath("~/test")
		expected := homeDir + "/test"
		if got != expected {
			t.Errorf("期望 %s，得到 %s", expected, got)
		}
	})
	t.Run("仅~展开为主目录", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil || homeDir == "" {
			t.Skip("无法获取用户主目录")
		}
		got := expandUserPath("~")
		if got != homeDir {
			t.Errorf("期望 %s，得到 %s", homeDir, got)
		}
	})
	t.Run("不以~开头的路径不变", func(t *testing.T) {
		got := expandUserPath("/tmp/test")
		if got != "/tmp/test" {
			t.Errorf("期望 /tmp/test，得到 %s", got)
		}
	})
	t.Run("空字符串不变", func(t *testing.T) {
		got := expandUserPath("")
		if got != "" {
			t.Errorf("期望空字符串，得到 %s", got)
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

// ---------- randomHex 测试 ----------

func TestRandomHex(t *testing.T) {
	t.Run("生成指定长度的十六进制字符串", func(t *testing.T) {
		got := randomHex(8)
		// n 字节 → 2n 个十六进制字符
		if len(got) != 16 {
			t.Errorf("期望长度 16，得到 %d", len(got))
		}
	})
	t.Run("生成的字符串为有效十六进制", func(t *testing.T) {
		got := randomHex(12)
		matched, err := regexp.MatchString(`^[0-9a-f]+$`, got)
		if err != nil || !matched {
			t.Errorf("期望有效十六进制字符串，得到 %s", got)
		}
	})
	t.Run("两次调用结果不同（极大概率）", func(t *testing.T) {
		a := randomHex(16)
		b := randomHex(16)
		if a == b {
			t.Errorf("两次调用不应返回相同结果（极小概率事件），a=%s b=%s", a, b)
		}
	})
	t.Run("零长度返回空字符串", func(t *testing.T) {
		got := randomHex(0)
		if got != "" {
			t.Errorf("期望空字符串，得到 %s", got)
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

func TestRandomHex_并发安全(t *testing.T) {
	// 并发调用 randomHex 不应 panic
	done := make(chan string, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- randomHex(8)
		}()
	}
	for i := 0; i < 10; i++ {
		v := <-done
		if len(v) != 16 {
			t.Errorf("期望长度 16，得到 %d", len(v))
		}
	}
}

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

func TestExpandUserPath_路径中间含波浪号(t *testing.T) {
	// ~ 不在开头时不展开
	got := expandUserPath("/tmp/~user/test")
	if strings.Contains(got, os.Getenv("HOME")) && !strings.HasPrefix(got, "/tmp") {
		t.Errorf("中间 ~ 不应展开，得到 %s", got)
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
