package configpanel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// withTempConfig 将主目录指向临时目录并写 fixture（同 control 包 useTestConfigDir：
// ConfigDir = <home>/.uapclaw/config，pathutil.SetUserHome 重置路径缓存）。
func withTempConfig(t *testing.T, yamlContent string) string {
	t.Helper()
	home := t.TempDir()
	pathutil.SetUserHome(home)
	t.Cleanup(func() {
		pathutil.SetUserHome("")
	})
	configDir := filepath.Join(home, ".uapclaw", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if yamlContent == "" {
		yamlContent = "{}\n"
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return configDir
}

// TestConfigChangeSet_ReloadScopes 验证 scope 推导（Python :215 reload_scopes）。
func TestConfigChangeSet_ReloadScopes(t *testing.T) {
	cs := ConfigChangeSet{EnvUpdates: map[string]string{"API_KEY": "x", "VIDEO_ENABLED": "true", "BOCHA_API_KEY": "y"}}
	scopes := cs.ReloadScopes()
	has := func(s string) bool {
		for _, v := range scopes {
			if v == s {
				return true
			}
		}
		return false
	}
	if !has("model") || !has("multimodal") || !has("search") {
		t.Fatalf("期望 model/multimodal/search scope，got %v", scopes)
	}

	cs2 := ConfigChangeSet{YAMLUpdated: []string{"models.defaults"}}
	if s := cs2.ReloadScopes(); len(s) != 1 || s[0] != "model" {
		t.Fatalf("models.defaults 期望 [model]，got %v", s)
	}
	cs3 := ConfigChangeSet{YAMLUpdated: []string{"setup_guide_enabled"}}
	if s := cs3.ReloadScopes(); len(s) != 1 || s[0] != "web_ui" {
		t.Fatalf("setup_guide 期望 [web_ui]，got %v", s)
	}
	cs4 := ConfigChangeSet{YAMLUpdated: []string{"unknown_key"}}
	if s := cs4.ReloadScopes(); len(s) != 1 || s[0] != "agent_runtime" {
		t.Fatalf("未知键期望 [agent_runtime]，got %v", s)
	}
	if !cs.Changed() {
		t.Fatal("有变更应 Changed()=true")
	}
	empty := ConfigChangeSet{}
	if empty.Changed() {
		t.Fatal("空变更集 Changed() 应为 false")
	}
	force := ConfigChangeSet{Force: true}
	if s := force.ReloadScopes(); len(s) != 1 || s[0] != "agent_runtime" {
		t.Fatalf("force 期望 agent_runtime 兜底，got %v", s)
	}
}

// TestApplyConfigSet_EnvWrite env 键写入 .env + 进程环境（Python :1539-1560/:1717-1723）。
func TestApplyConfigSet_EnvWrite(t *testing.T) {
	withTempConfig(t, "")
	envPath := filepath.Join(withTempConfig(t, ""), ".env")
	res, err := ApplyConfigSet(context.Background(),
		map[string]any{"api_key": "sk-test", "model": " m1 ", "api_base": "http://x"}, nil)
	if err != nil {
		t.Fatalf("ApplyConfigSet 失败: %v", err)
	}
	if res.EnvUpdates["API_KEY"] != "sk-test" || res.EnvUpdates["MODEL_NAME"] != "m1" {
		t.Fatalf("env 增量不符: %v", res.EnvUpdates)
	}
	data, _ := os.ReadFile(envPath)
	content := string(data)
	if !strings.Contains(content, `API_KEY="sk-test"`) {
		t.Fatalf(".env 应含 API_KEY=\"sk-test\"，got: %s", content)
	}
	if !strings.Contains(content, `MODEL_NAME="m1"`) {
		t.Fatalf(".env 应含 MODEL_NAME=\"m1\"")
	}
	if os.Getenv("API_KEY") != "sk-test" {
		t.Fatalf("进程内环境变量应同步更新")
	}
	// env 变更推导出 model scope → 需要重载
	if !NeedsAgentReload(ConfigChangeSet{EnvUpdates: res.EnvUpdates}.ReloadScopes()) {
		t.Fatal("model scope 应需重载")
	}
	if !NeedsAgentReload([]string{"model"}) {
		t.Fatal("model scope 应需重载")
	}
	if NeedsAgentReload([]string{"web_ui"}) {
		t.Fatal("web_ui scope 无需 AgentServer 重载")
	}
}

// TestApplyConfigSet_YAMLSwitches yaml 开关写入与联动（Python :1594-1612）。
func TestApplyConfigSet_YAMLSwitches(t *testing.T) {
	withTempConfig(t, "modes:\n  team:\n    jiuwen_team:\n      dispatch_mode: scheduled\n")
	res, err := ApplyConfigSet(context.Background(), map[string]any{
		"setup_guide_enabled":             "true",
		"rsi_enabled":                     true,
		"swarmflow_enabled":               "on",
		"swarmflow_budget":                "5.0",
		"team_scheduled_dispatch_enabled": false,
	}, nil)
	if err != nil {
		t.Fatalf("ApplyConfigSet 失败: %v", err)
	}
	raw := ReadRawConfig()
	check := func(path []string, want any) {
		t.Helper()
		got := GetNestedConfigValue(raw, path, nil)
		if fmtAny(got) != fmtAny(want) {
			t.Fatalf("path %v 期望 %v got %v", path, want, got)
		}
	}
	check([]string{"setup_guide", "enabled"}, true)
	check([]string{"rsi", "enabled"}, true)
	check([]string{"modes", "team", "jiuwen_team", "enable_swarmflow"}, true)
	check([]string{"modes", "team", "jiuwen_team", "swarmflow_budget"}, "5.0")
	// 联动：调度关闭 → dispatch_mode=autonomous 且三键关闭
	check([]string{"modes", "team", "jiuwen_team", "dispatch_mode"}, "autonomous")
	check([]string{"modes", "team", "jiuwen_team", "enable_task_verification"}, false)
	check([]string{"modes", "team", "jiuwen_team", "enable_member_messaging"}, false)
	check([]string{"modes", "team", "jiuwen_team", "max_subtask_depth"}, 0)
	if res.YAMLUpdated == nil || len(res.YAMLUpdated) == 0 {
		t.Fatal("yamlUpdated 应非空")
	}
}

// TestApplyConfigSet_ProactiveValidate proactive 数值校验（Python :1324）。
func TestApplyConfigSet_ProactiveValidate(t *testing.T) {
	withTempConfig(t, "")
	if _, err := ApplyConfigSet(context.Background(),
		map[string]any{"proactive_recommendation_max_recommend_per_day": "3.5"}, nil); err == nil {
		t.Fatal("浮点值应报 ConfigPanelBadRequest")
	}
	res, err := ApplyConfigSet(context.Background(),
		map[string]any{"proactive_recommendation_max_recommend_per_day": "20"}, nil)
	if err != nil {
		t.Fatalf("合法值失败: %v", err)
	}
	raw := ReadRawConfig()
	if got := GetNestedConfigValue(raw, []string{"proactive_recommendation", "max_recommend_per_day"}, nil); fmtAny(got) != fmtAny(20) {
		t.Fatalf("期望 20 got %v", got)
	}
	_ = res
}

// TestApplyConfigSet_PermissionsProfile 权限画像分支（Python :1520-1536）。
func TestApplyConfigSet_PermissionsProfile(t *testing.T) {
	withTempConfig(t, "")
	res, err := ApplyConfigSet(context.Background(), map[string]any{"permissions_profile": "automatic"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if res.CanonicalConfig["permissions_profile"] != "automatic" || res.CanonicalConfig["permissions_enabled"] != "true" {
		t.Fatalf("canonical 不符: %v", res.CanonicalConfig)
	}
	if _, err := ApplyConfigSet(context.Background(),
		map[string]any{"permissions_profile": "automatic", "permissions_enabled": "true"}, nil); err == nil {
		t.Fatal("互斥应报 400")
	}
	if _, err := ApplyConfigSet(context.Background(), map[string]any{"permissions_profile": "bogus"}, nil); err == nil {
		t.Fatal("非法画像应报 400")
	}
	// enabled=false → full_access
	res2, err := ApplyConfigSet(context.Background(), map[string]any{"permissions_enabled": "false"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if res2.PendingPermissionProfile != "full_access" {
		t.Fatalf("期望 full_access，got %s", res2.PendingPermissionProfile)
	}
	if err := CommitPendingPermissionProfile(res2); err != nil {
		t.Fatalf("commit 失败: %v", err)
	}
	if GetNestedConfigValue(ReadRawConfig(), []string{"permissions", "enabled"}, nil) != false {
		t.Fatal("permissions.enabled 应为 false")
	}
}

// TestApplyConfigSet_MutualExclusiveAndProvider 参数防线（Python :1459-1465/:1550）。
func TestApplyConfigSet_MutualExclusiveAndProvider(t *testing.T) {
	withTempConfig(t, "")
	if _, err := ApplyConfigSet(context.Background(), map[string]any{"permissions_mode": "x"}, nil); err == nil {
		t.Fatal("permissions_mode 应拒绝写入")
	}
	if _, err := ApplyConfigSet(context.Background(), map[string]any{"model_provider": "NoSuchProvider"}, nil); err == nil {
		t.Fatal("未知 provider 应报 400")
	}
	if _, err := ApplyConfigSet(context.Background(), map[string]any{"video_context_window_tokens": "abc"}, nil); err == nil {
		t.Fatal("非法上下文窗口应报 400")
	}
	res, err := ApplyConfigSet(context.Background(), map[string]any{"video_context_window_tokens": "256K"}, nil)
	if err != nil {
		t.Fatalf("256K 解析失败: %v", err)
	}
	if res.EnvUpdates["VIDEO_CONTEXT_WINDOW_TOKENS"] == "" {
		t.Fatalf("256K 应解析为整数落 env，got %v", res.EnvUpdates)
	}
}

// TestCoerceAndNested 工具函数边界。
func TestCoerceAndNested(t *testing.T) {
	v, _ := CoerceConfigPanelValue("TRUE", "bool", false)
	if v != true {
		t.Fatal("bool 强转失败")
	}
	v, _ = CoerceConfigPanelValue("0.5", "ratio", 1)
	if v != 0.5 {
		t.Fatal("ratio 0.5 应通过")
	}
	v, _ = CoerceConfigPanelValue("1.5", "ratio", 1)
	if v != 1 {
		t.Fatal("ratio 1.5 应回默认")
	}
	if _, err := CoerceConfigPanelValue("100", "output_chars", 0); err == nil {
		t.Fatal("output_chars 超界应报错")
	}
	target := map[string]any{}
	SetNestedConfigValue(target, []string{"a", "b", "c"}, 1)
	if GetNestedConfigValue(target, []string{"a", "b", "c"}, 0) != 1 {
		t.Fatal("嵌套写读失败")
	}
	if GetNestedConfigValue(target, []string{"a", "x"}, "d") != "d" {
		t.Fatal("缺路径应回默认")
	}
	if !ParseConfigBool("enabled") || ParseConfigBool("nope") {
		t.Fatal("ParseConfigBool 边界失败")
	}
}

// fmtAny 值比较格式化。
func fmtAny(v any) string {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
	return s
}
