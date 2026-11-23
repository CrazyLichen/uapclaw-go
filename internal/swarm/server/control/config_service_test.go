package control

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// useTestConfigDir 将主目录指向临时目录并写入 fixture 文件。
// 路径布局：ConfigDir = <home>/config（WorkspaceDir()/config），用 pathutil.SetUserHome
// 重置路径缓存；测试结束恢复真实主目录。
func useTestConfigDir(t *testing.T, envContent, yamlContent string) string {
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
	if envContent != "" {
		if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(envContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if yamlContent != "" {
		if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configDir
}

// TestGetPanel_EnvMapReadsFileNotProcessEnv env 回读以 .env 文件为准而非进程内存
// （Python config_service.py:402-417 跨进程时序修复的回归实证）。
func TestGetPanel_EnvMapReadsFileNotProcessEnv(t *testing.T) {
	useTestConfigDir(t, "API_KEY=sk-from-file\nMODEL_NAME=m-from-file\n", "")
	// 进程内环境变量被伪造成过期旧值——面板仍须读文件值
	t.Setenv("API_KEY", "sk-stale-in-memory")
	t.Setenv("MODEL_NAME", "stale-in-memory")
	panel := GetPanel()
	if panel["api_key"] != "sk-from-file" {
		t.Errorf("api_key = %v, want sk-from-file（必须读文件）", panel["api_key"])
	}
	if panel["model"] != "m-from-file" {
		t.Errorf("model = %v, want m-from-file", panel["model"])
	}
}

// TestGetPanel_YamlSwitches yaml 开关墙逐键断言
func TestGetPanel_YamlSwitches(t *testing.T) {
	useTestConfigDir(t, "", `
react:
  context_engine_config:
    enabled: true
  evolution:
    skill_evolution: true
experimental:
  team_organization_ui_enabled: true
  comfyui_workflow_enabled: true
permissions:
  enabled: true
setup_guide:
  enabled: false
memory:
  forbidden_memory_definition:
    enabled: true
    description: "no secrets"
trajectory_ui:
  enabled: true
  diagnosis_enabled: true
modes:
  team:
    jiuwen_team:
      enable_swarmflow: true
      dispatch_mode: scheduled
      enable_task_verification: true
      enable_member_messaging: true
      max_subtask_depth: 3
      swarmflow_budget: 50
proactive_recommendation:
  enabled: true
  max_recommend_per_day: 5
  max_rounds_per_tick: 8
models:
  enable_free_models: false
progressive_tool_optimize: true
`)
	panel := GetPanel()
	checks := map[string]string{
		"context_engine_enabled":           "true",
		"skill_evolution":                  "true",
		"team_organization_ui_enabled":     "true",
		"comfyui_workflow_enabled":         "true",
		"permissions_enabled":              "true",
		"setup_guide_enabled":              "false",
		"memory_forbidden_enabled":         "true",
		"memory_forbidden_description":     "no secrets",
		"trajectory_ui_enabled":            "true",
		"trajectory_ui_diagnosis_enabled":  "true",
		"swarmflow_enabled":                "true",
		"team_scheduled_dispatch_enabled":  "true",
		"team_task_verification_enabled":   "true",
		"team_member_messaging_enabled":    "true",
		"team_member_subtasks_enabled":     "true",
		"swarmflow_budget":                 "50",
		"proactive_recommendation_enabled": "true",
		"progressive_tool_optimize":        "true",
	}
	for key, want := range checks {
		if got := fmtAny(panel[key]); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if panel["proactive_recommendation_max_recommend_per_day"] != "5" {
		t.Errorf("max_recommend_per_day = %v, want 5", panel["proactive_recommendation_max_recommend_per_day"])
	}
	if panel["proactive_recommendation_max_rounds_per_tick"] != "8" {
		t.Errorf("max_rounds_per_tick = %v, want 8", panel["proactive_recommendation_max_rounds_per_tick"])
	}
	if panel["enable_free_models"] != "false" {
		t.Errorf("enable_free_models = %v, want false", panel["enable_free_models"])
	}
}

// TestGetPanel_DepthSwitch 边界：depth 0 → false；布尔恒 false（F_128）
func TestGetPanel_DepthSwitch(t *testing.T) {
	if depthSwitchText(0) != "false" {
		t.Errorf("depthSwitchText(0) = %q, want false", depthSwitchText(0))
	}
	if depthSwitchText(true) != "false" {
		t.Error("depthSwitchText(true) 恒 false（布尔输入）")
	}
	if depthSwitchText(2) != "true" {
		t.Errorf("depthSwitchText(2) = %q, want true", depthSwitchText(2))
	}
	if depthSwitchText("3") != "true" {
		t.Errorf("depthSwitchText(\"3\") = %q, want true", depthSwitchText("3"))
	}
	if depthSwitchText("abc") != "false" {
		t.Errorf("depthSwitchText(\"abc\") = %q, want false", depthSwitchText("abc"))
	}
}

// TestGetPanel_EnvMapCount 对照表条目数与 Python 一致（74，机器核对）
func TestGetPanel_EnvMapCount(t *testing.T) {
	if len(ConfigSetEnvMap) != 74 {
		t.Errorf("ConfigSetEnvMap = %d 条, want 74", len(ConfigSetEnvMap))
	}
}

// TestGetPanel_DefaultsAndVersion 空配置默认值 + 版本/平台字段
func TestGetPanel_DefaultsAndVersion(t *testing.T) {
	useTestConfigDir(t, "", "")
	t.Setenv("JIUWENSWARM_RUNTIME_PLATFORM", "")
	panel := GetPanel()
	checks := map[string]string{
		"context_engine_enabled":          "false",
		"kv_cache_affinity_enabled":       "false",
		"permissions_enabled":             "false",
		"setup_guide_enabled":             "true",
		"skill_evolution":                 "false",
		"a2ui_enabled":                    "false",
		"team_scheduled_dispatch_enabled": "false",
		"team_member_subtasks_enabled":    "false",
		"free_search_ddg_enabled":         "false",
		"free_search_bing_enabled":        "false",
		"enable_free_models":              "true",
		"runtime_platform":                "default",
		"external_cli_agents_supported":   "true",
	}
	for key, want := range checks {
		if got := fmtAny(panel[key]); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if panel["app_version"] == "" || panel["app_version"] == nil {
		t.Errorf("app_version 缺失")
	}
}

// TestGetPanel_HarmonyPlatform harmony 平台不支持外部 CLI（Python :419-421）
func TestGetPanel_HarmonyPlatform(t *testing.T) {
	useTestConfigDir(t, "", "")
	t.Setenv("JIUWENSWARM_RUNTIME_PLATFORM", "Harmony")
	panel := GetPanel()
	if panel["runtime_platform"] != "harmony" {
		t.Errorf("runtime_platform = %v, want harmony", panel["runtime_platform"])
	}
	if panel["external_cli_agents_supported"] != "false" {
		t.Errorf("external_cli_agents_supported = %v, want false", panel["external_cli_agents_supported"])
	}
}

// TestHandleConfigRequest_LocaleGetConf 语言读取与非法值回落
func TestHandleConfigRequest_LocaleGetConf(t *testing.T) {
	useTestConfigDir(t, "", "preferred_language: en")
	payload, err := HandleConfigRequest("locale.get_conf", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if payload["preferred_language"] != "en" {
		t.Errorf("payload = %v, want en", payload)
	}
	useTestConfigDir(t, "", "preferred_language: fr")
	payload, _ = HandleConfigRequest("locale.get_conf", nil)
	if payload["preferred_language"] != "zh" {
		t.Errorf("payload = %v, want zh（非法回落）", payload["preferred_language"])
	}
}

// TestHandleConfigRequest_PathGet 浏览器路径 + decision 投影
func TestHandleConfigRequest_PathGet(t *testing.T) {
	useTestConfigDir(t, "TYPESAFE_API_KEY=sk-x", "")
	useTestConfigDir(t, "TYPESAFE_API_KEY=sk-x", `
browser:
  chrome_path: " /usr/bin/chrome "
  headless: false
  decision:
    provider: typesafe
    mode: shadow
`)
	payload, err := HandleConfigRequest("path.get", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if payload["chrome_path"] != "/usr/bin/chrome" {
		t.Errorf("chrome_path = %v", payload["chrome_path"])
	}
	if payload["headless"] != false {
		t.Errorf("headless = %v, want false", payload["headless"])
	}
	if payload["decision_mode"] != "shadow" {
		t.Errorf("decision_mode = %v, want shadow", payload["decision_mode"])
	}
	if payload["jev_key_configured"] != true {
		t.Errorf("jev_key_configured = %v, want true（.env 里有 TYPESAFE_API_KEY）", payload["jev_key_configured"])
	}
}

// TestHandleConfigRequest_MemoryForbiddenGet 记忆禁区段读取
func TestHandleConfigRequest_MemoryForbiddenGet(t *testing.T) {
	useTestConfigDir(t, "", `
memory:
  forbidden_memory_definition:
    enabled: true
    patterns: ["secret"]
`)
	payload, err := HandleConfigRequest("memory_forbidden.get", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if payload["enabled"] != true {
		t.Errorf("enabled = %v, want true", payload["enabled"])
	}
}

// TestHandleConfigRequest_UnsupportedMethod 未知方法 BAD_REQUEST
func TestHandleConfigRequest_UnsupportedMethod(t *testing.T) {
	useTestConfigDir(t, "", "")
	_, err := HandleConfigRequest("config.bogus", nil)
	var badReq *ControlConfigError
	if !errors.As(err, &badReq) {
		t.Fatalf("期望 ControlConfigError，got %v", err)
	}
	if badReq.Message != "unsupported control config method" {
		t.Errorf("message = %q, want unsupported control config method", badReq.Message)
	}
}

// TestResolveEnvVars ${VAR:-default} 展开语义
func TestResolveEnvVars(t *testing.T) {
	t.Setenv("EXIST_VAR", "val")
	if got := resolveEnvVars("${EXIST_VAR}"); got != "val" {
		t.Errorf("${EXIST_VAR} = %v", got)
	}
	if got := resolveEnvVars("${MISSING_X:-fallback}"); got != "fallback" {
		t.Errorf("${MISSING_X:-fallback} = %v", got)
	}
	if got := resolveEnvVars("${EXIST_VAR:-fallback}"); got != "val" {
		t.Errorf("${EXIST_VAR:-fallback} = %v", got)
	}
	if got := resolveEnvVars("${MISSING_X}"); got != "" {
		t.Errorf("${MISSING_X} = %v, want 空串", got)
	}
	if got := resolveEnvVars("${MISSING_X:-}"); got != "" {
		t.Errorf("${MISSING_X:-} = %v", got)
	}
}

// TestFlattenModesTeam 团队展平（前 10 团队 + agent 索引）
func TestFlattenModesTeam(t *testing.T) {
	raw := map[string]any{
		"modes": map[string]any{
			"team": map[string]any{
				"jiuwen_team": map[string]any{
					"team_name":          "alpha",
					"lifecycle":          "persist",
					"teammate_mode":      "collab",
					"spawn_mode":         "clone",
					"enable_permissions": true,
					"leader": map[string]any{
						"member_name": "boss", "display_name": "Boss", "persona": "p1",
					},
					"predefined_members": []any{
						map[string]any{"member_name": "m1", "display_name": "M1", "persona": "", "prompt_hint": "h"},
					},
				},
			},
		},
	}
	flat := flattenModesTeam(raw)
	if flat["team_0_name"] != "alpha" {
		t.Errorf("team_0_name = %q, want alpha", flat["team_0_name"])
	}
	if flat["team_0_enable_permissions"] != "true" {
		t.Errorf("team_0_enable_permissions = %q", flat["team_0_enable_permissions"])
	}
	// Python: leader_key 取 team 字典键（此处 "jiuwen_team"）而非 team_name 字段
	if flat["team_0_leader_agent_key"] != "jiuwen_team_leader" {
		t.Errorf("leader_agent_key = %q, want jiuwen_team_leader", flat["team_0_leader_agent_key"])
	}
	if !strings.Contains(flat["team_0_predefined_members"], `"member_name":"m1"`) {
		t.Errorf("predefined_members = %q", flat["team_0_predefined_members"])
	}
}

// TestFlattenSymphony Symphony 展平（默认 false + 显式 true）
func TestFlattenSymphony(t *testing.T) {
	flat := flattenSymphony(map[string]any{})
	if flat["symphony_enabled"] != "false" {
		t.Errorf("symphony_enabled = %q, want false（代码默认）", flat["symphony_enabled"])
	}
	flat = flattenSymphony(map[string]any{"symphony": map[string]any{"enabled": true}})
	if flat["symphony_enabled"] != "true" {
		t.Errorf("symphony_enabled = %q, want true", flat["symphony_enabled"])
	}
}

// TestFlattenRecommendation 推荐两开关（键存在走 resolve / 缺失回默认）
func TestFlattenRecommendation(t *testing.T) {
	flat := flattenRecommendation(map[string]any{})
	if flat["topic_recommendation_enabled"] != "false" || flat["prompt_suggestion_enabled"] != "false" {
		t.Errorf("默认 = %v", flat)
	}
	flat = flattenRecommendation(map[string]any{"recommendation": map[string]any{"topic_enabled": "on"}})
	if flat["topic_recommendation_enabled"] != "true" {
		t.Errorf("topic = %q, want true", flat["topic_recommendation_enabled"])
	}
}

// TestAffinityEnabled KV 亲和：顶层优先，旧位置回退
func TestAffinityEnabled(t *testing.T) {
	if affinityEnabled(map[string]any{}) {
		t.Error("空配置应为 false")
	}
	raw := map[string]any{"kv_cache_affinity_config": map[string]any{"enable_kv_cache_affinity": true}}
	if !affinityEnabled(raw) {
		t.Error("顶层开启应为 true")
	}
	raw = map[string]any{"react": map[string]any{"kv_cache_affinity_config": map[string]any{"enable_kv_cache_affinity": true}}}
	if !affinityEnabled(raw) {
		t.Error("react 旧位置开启应为 true")
	}
}

// TestDotenvValues .env 解析：注释跳过、引号剥离、值含等号
func TestDotenvValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# comment\nKEY1=v1\nKEY2=\"quoted\"\nKEY3=a=b\n\n"), 0o644)
	values := dotenvValues(p)
	if values["KEY1"] != "v1" || values["KEY2"] != "quoted" || values["KEY3"] != "a=b" {
		t.Errorf("values = %v", values)
	}
	if len(dotenvValues(filepath.Join(t.TempDir(), "missing"))) != 0 {
		t.Error("缺失文件应回空表")
	}
}

// TestProjectBrowserDecision 投影函数（swarm/common browser_settings.go）
func TestProjectBrowserDecision(t *testing.T) {
	out := common.ProjectBrowserDecision(map[string]any{"provider": "bogus", "mode": "shadow"}, map[string]string{"TYPESAFE_API_KEY": " "})
	if out["decision_provider"] != "typesafe" {
		t.Errorf("provider = %v, want typesafe（非法回落）", out["decision_provider"])
	}
	if out["decision_mode"] != "shadow" {
		t.Errorf("mode = %v, want shadow", out["decision_mode"])
	}
	if out["jev_key_configured"] != false {
		t.Errorf("jev_key_configured = %v, want false（空白值）", out["jev_key_configured"])
	}
	out = common.ProjectBrowserDecision(map[string]any{}, map[string]string{"TYPESAFE_API_KEY": "sk"})
	if out["decision_model"] != "jev-1.13.0" || out["decision_api_base"] != "https://api.typesafe.ai/v1" {
		t.Errorf("defaults = %v", out)
	}
}

// fmtAny any → 字符串比较。
func fmtAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}
	return fmt.Sprintf("%v", v)
}

// TestFlattenExternalCli 外部 CLI 展平（含自定义 agents 键）
func TestFlattenExternalCli(t *testing.T) {
	raw := map[string]any{"modes": map[string]any{"team": map[string]any{
		"jiuwen_team": map[string]any{"external_cli_agents": []any{"claude"}},
	}}}
	flat := flattenExternalCli(raw)
	if _, ok := flat["external_cli_agent_claude_enabled"]; !ok {
		t.Errorf("缺 external_cli_agent_claude_enabled 键: %v", flat)
	}
	if _, ok := flat["external_cli_custom_agents"]; !ok {
		t.Errorf("缺 external_cli_custom_agents 键")
	}
}

// TestResolveEnvVars_Recursive dict/list 递归展开
func TestResolveEnvVars_Recursive(t *testing.T) {
	t.Setenv("REC_VAR", "deep")
	in := map[string]any{
		"a": map[string]any{"b": "${REC_VAR}"},
		"c": []any{"${REC_VAR}", 1},
		"d": 42,
	}
	out := resolveEnvVars(in).(map[string]any)
	if out["a"].(map[string]any)["b"] != "deep" {
		t.Errorf("a.b = %v", out["a"])
	}
	list := out["c"].([]any)
	if list[0] != "deep" || list[1] != 1 {
		t.Errorf("c = %v", list)
	}
	if out["d"] != 42 {
		t.Errorf("d = %v（非字符串原样）", out["d"])
	}
}

// TestHandleConfigRequest_MemoryForbiddenNotDict 非映射回空对象
func TestHandleConfigRequest_MemoryForbiddenNotDict(t *testing.T) {
	useTestConfigDir(t, "", "memory:\n  forbidden_memory_definition: bad")
	payload, err := HandleConfigRequest("memory_forbidden.get", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if payload == nil || len(payload) != 0 {
		t.Errorf("payload = %v, want 空 map", payload)
	}
}

// TestResolveBrowserPath_PlatformDict chrome_path 为平台映射字典
func TestResolveBrowserPath_PlatformDict(t *testing.T) {
	browser := map[string]any{
		"chrome_path": map[string]any{"linux": "/usr/bin/chrome", "default": "/fallback"},
	}
	if got := resolveBrowserPath(browser); got != "/usr/bin/chrome" {
		t.Errorf("linux = %q", got)
	}
	browser = map[string]any{"chrome_path": map[string]any{"default": "/fallback"}}
	if got := resolveBrowserPath(browser); got != "/fallback" {
		t.Errorf("default = %q", got)
	}
	if got := resolveBrowserPath(map[string]any{"chrome_path": map[string]any{}}); got != "" {
		t.Errorf("无匹配 = %q, want 空", got)
	}
	if got := resolveBrowserPath(map[string]any{"chrome_path": 42}); got != "" {
		t.Errorf("非字符串非字典 = %q, want 空", got)
	}
}

// TestGetPanel_A2UIEnabled 环境变量开启 a2ui
func TestGetPanel_A2UIEnabled(t *testing.T) {
	useTestConfigDir(t, "", "")
	t.Setenv("JIUWENSWARM_A2UI_ENABLED", "true")
	panel := GetPanel()
	if panel["a2ui_enabled"] != "true" {
		t.Errorf("a2ui_enabled = %v, want true", panel["a2ui_enabled"])
	}
}

// TestGetPanel_BudgetAbsent 预算缺失不写 swarmflow_budget 键
func TestGetPanel_BudgetAbsent(t *testing.T) {
	useTestConfigDir(t, "", "modes:\n  team:\n    jiuwen_team:\n      enable_swarmflow: true")
	panel := GetPanel()
	if _, exists := panel["swarmflow_budget"]; exists {
		t.Errorf("swarmflow_budget = %v, want 键不存在（budget nil）", panel["swarmflow_budget"])
	}
	if panel["swarmflow_enabled"] != "true" {
		t.Errorf("swarmflow_enabled = %v", panel["swarmflow_enabled"])
	}
}

// TestBoolText 布尔文本三态边界
func TestBoolText(t *testing.T) {
	if boolText(true, false) != true || boolText(false, true) != false {
		t.Error("bool 输入应原样")
	}
	if boolText(nil, true) != true || boolText(nil, false) != false {
		t.Error("nil 回默认")
	}
	if boolText("YES", false) != true || boolText("0", true) != false || boolText("bogus", false) != false {
		t.Error("字符串判定不符")
	}
}

// TestParsePositiveInt 整数解析
func TestParsePositiveInt(t *testing.T) {
	if !parsePositiveInt(" 5 ") || parsePositiveInt("0") || parsePositiveInt("-1") || parsePositiveInt("x") {
		t.Error("parsePositiveInt 边界不符")
	}
}

// TestHandleConfigRequest_ConfigGet 分派回面板
func TestHandleConfigRequest_ConfigGet(t *testing.T) {
	useTestConfigDir(t, "", "")
	payload, err := HandleConfigRequest("config.get", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if _, exists := payload["app_version"]; !exists {
		t.Errorf("payload 缺 app_version")
	}
}

// TestFlattenModesTeam_TeammateAndAgentIndex teammate 分支与 agent 索引展平
func TestFlattenModesTeam_TeammateAndAgentIndex(t *testing.T) {
	raw := map[string]any{
		"modes": map[string]any{
			"team": map[string]any{
				"jiuwen_team": map[string]any{
					"teammate": map[string]any{"agent_key": "tm_key"},
					"agents": map[string]any{
						"teammate": map[string]any{"model": map[string]any{"model": "m-1"}},
						"m1":       map[string]any{"skills": []any{"s1", "s2"}, "max_iterations": 5},
					},
					"predefined_members": []any{
						map[string]any{"member_name": "m1", "agent_key": "", "display_name": "M1", "persona": "pp", "prompt_hint": "hh"},
					},
				},
			},
		},
	}
	flat := flattenModesTeam(raw)
	if flat["team_0_teammate_agent_key"] != "tm_key" {
		t.Errorf("teammate_agent_key = %q, want tm_key", flat["team_0_teammate_agent_key"])
	}
	// Python 插入序：teammate 先登记 → agent_name_0 = tm_key
	if flat["agent_name_0"] != "tm_key" || flat["agent_model_0"] != "m-1" {
		t.Errorf("agent index 0 = %v/%v（应为插入序 tm_key）", flat["agent_name_0"], flat["agent_model_0"])
	}
	// 插入序 index 1 = jiuwen_team_m1（来自 predefined_members），其 spec 有 skills
	if flat["agent_name_1"] != "jiuwen_team_m1" {
		t.Errorf("agent_name_1 = %q, want jiuwen_team_m1", flat["agent_name_1"])
	}
	if !strings.Contains(flat["agent_skills_1"], "s1") {
		t.Errorf("agent_skills_1 = %q", flat["agent_skills_1"])
	}
	if flat["agent_max_iterations_1"] != "5" {
		t.Errorf("agent_max_iterations_1 = %q, want 5", flat["agent_max_iterations_1"])
	}
	if flat["agent_completion_timeout_1"] != "600" {
		t.Errorf("agent_completion_timeout_1 默认 = %q, want 600", flat["agent_completion_timeout_1"])
	}
	if flat["agent_skills_0"] != "" {
		t.Errorf("agent_skills_0（teammate spec 无 skills）= %q, want 空", flat["agent_skills_0"])
	}
}

// TestFlattenModesTeam_TenTeamCap 团队超过 10 截断（Python team_idx >= 10）
func TestFlattenModesTeam_TenTeamCap(t *testing.T) {
	team := map[string]any{}
	teams := map[string]any{}
	for i := 0; i < 12; i++ {
		teams[fmt.Sprintf("t%02d", i)] = map[string]any{"team_name": fmt.Sprintf("T%d", i)}
	}
	_ = team
	raw := map[string]any{"modes": map[string]any{"team": teams}}
	flat := flattenModesTeam(raw)
	// Python enumerate + team_idx >= 10 continue：仅前 10 个（idx 0-9）展平
	if _, exists := flat["team_10_name"]; exists {
		t.Error("第 11 个团队不应展平（上限 10）")
	}
	if _, exists := flat["team_11_name"]; exists {
		t.Error("第 12 个团队不应展平（上限 10）")
	}
	if flat["team_9_name"] != "T9" {
		t.Errorf("team_9_name = %q, want T9", flat["team_9_name"])
	}
}

// TestFlattenModesTeam_ModelRequestConfigFallback 三层模型名回退
func TestFlattenModesTeam_ModelRequestConfigFallback(t *testing.T) {
	mk := func(model any) map[string]any {
		return map[string]any{"modes": map[string]any{"team": map[string]any{
			"jiuwen_team": map[string]any{"agents": map[string]any{
				"leader": map[string]any{"model": model},
			}},
		}}}
	}
	flat := flattenModesTeam(mk(map[string]any{"model_request_config": map[string]any{"model": "req-m"}}))
	if flat["agent_model_0"] != "req-m" {
		t.Errorf("request_config 层 = %q", flat["agent_model_0"])
	}
	flat = flattenModesTeam(mk(map[string]any{"model_client_config": map[string]any{"model_name": "cli-m"}}))
	if flat["agent_model_0"] != "cli-m" {
		t.Errorf("client_config 层 = %q", flat["agent_model_0"])
	}
	flat = flattenModesTeam(mk("not-a-map"))
	if flat["agent_model_0"] != "" {
		t.Errorf("model 非 map = %q, want 空", flat["agent_model_0"])
	}
}

// TestGetPanel_A2UIBadVersion a2ui 版本非法回 false（Python :460-466 except）
func TestGetPanel_A2UIBadVersion(t *testing.T) {
	useTestConfigDir(t, "", "")
	t.Setenv("JIUWENSWARM_A2UI_PROTOCOL_VERSION", "9.9")
	panel := GetPanel()
	if panel["a2ui_enabled"] != "false" {
		t.Errorf("a2ui_enabled = %v, want false（版本非法）", panel["a2ui_enabled"])
	}
}

// TestFlattenSymphony_FlowFallback flow.enabled 缺失回 evolution.enabled
func TestFlattenSymphony_FlowFallback(t *testing.T) {
	flat := flattenSymphony(map[string]any{"symphony": map[string]any{
		"evolution": map[string]any{"enabled": true},
	}})
	if flat["symphony_evolution_enabled"] != "true" {
		t.Errorf("symphony_evolution_enabled = %q, want true（flow 缺失回 evolution）", flat["symphony_evolution_enabled"])
	}
	if flat["skill_retrieval_max_results"] != "10" {
		t.Errorf("max_results 默认 = %q, want 10", flat["skill_retrieval_max_results"])
	}
	if flat["skill_retrieval_max_output_chars"] != "12000" {
		t.Errorf("max_output_chars 默认 = %q", flat["skill_retrieval_max_output_chars"])
	}
	if flat["skill_retrieval_max_list_entries"] != "40" {
		t.Errorf("max_list_entries 默认 = %q", flat["skill_retrieval_max_list_entries"])
	}
	if flat["skill_retrieval_incremental_notice_max_chars"] != "4000" {
		t.Errorf("incremental 默认 = %q", flat["skill_retrieval_incremental_notice_max_chars"])
	}
}
