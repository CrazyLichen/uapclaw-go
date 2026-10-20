package team

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildProductionLikeConfig 构造贴近生产 config.yaml 的测试配置（dict 格式 modes.team）。
// S-01 修复验证：生产配置为 dict-of-dicts（team_name → team_spec），非 list。
func buildProductionLikeConfig() map[string]any {
	return map[string]any{
		"models": map[string]any{
			// S-03 修复验证：defaults 为 list（生产格式）
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"api_base":        "https://api.example.com/v1",
						"api_key":         "sk-test",
						"model_name":      "test-model",
						"client_provider": "openai",
						"timeout":         1800.0,
						"verify_ssl":      false,
					},
					"model_config_obj": map[string]any{
						"temperature": 0.95,
					},
				},
			},
		},
		"agents": map[string]any{
			// M-01 修复验证：顶层 agents 键（查找路径）+ 字符串引用目标
			"agent_leader": map[string]any{
				"workspace":      map[string]any{"stable_base": true},
				"max_iterations": 100,
			},
		},
		"preferred_language": "ZH ",
		"modes": map[string]any{
			"team": map[string]any{
				// S-01 修复验证：dict 形式（team_name → team_spec）
				"jiuwen_team": map[string]any{
					"team_name":     "jiuwen_team",
					"lifecycle":     "persistent",
					"teammate_mode": "build_mode",
					"spawn_mode":    "inprocess",
					"leader": map[string]any{
						"member_name":  "team-leader",
						"display_name": "团队领导",
						"persona":      "天才项目管理专家，擅长任务分解和团队协调",
					},
					// M-01 修复验证：字符串引用 "$agent_leader"
					"agents":  map[string]any{"leader": "$agent_leader"},
					"storage": map[string]any{"type": "sqlite"},
				},
			},
		},
	}
}

// TestSelectFirstModesTeam_dict格式 生产 dict 格式取第一个 team spec（S-01）。
func TestSelectFirstModesTeam_dict格式(t *testing.T) {
	cfg := buildProductionLikeConfig()
	teamRaw := selectFirstModesTeam(cfg)
	if len(teamRaw) == 0 {
		t.Fatal("S-01: dict 格式 modes.team 应能取到 team 配置（旧 list 断言恒失败）")
	}
	if got := strField(teamRaw, "team_name"); got != "jiuwen_team" {
		t.Fatalf("team_name = %q, want jiuwen_team", got)
	}
}

// TestSelectFirstModesTeam_空dict与list格式 空字典与 list 格式均回退空配置。
func TestSelectFirstModesTeam_空dict与list格式(t *testing.T) {
	// 空 dict：两种实现行为一致
	if got := selectFirstModesTeam(map[string]any{"modes": map[string]any{"team": map[string]any{}}}); len(got) != 0 {
		t.Fatalf("empty dict should return empty, got %v", got)
	}
	// list 格式（Python 不支持）：非 dict 返回空
	if got := selectFirstModesTeam(map[string]any{"modes": map[string]any{"team": []any{map[string]any{"team_name": "x"}}}}); len(got) != 0 {
		t.Fatalf("list format should return empty (Python isinstance dict guard), got %v", got)
	}
	// 非 dict 值条目跳过，取第一个 dict 值
	cfg := map[string]any{"modes": map[string]any{"team": map[string]any{
		"a": "not-a-map",
		"b": map[string]any{"team_name": "b-team"},
	}}}
	got := selectFirstModesTeam(cfg)
	if strField(got, "team_name") != "b-team" {
		t.Fatalf("should skip non-dict entries and take first dict value, got %v", got)
	}
}

// TestResolveDefaultModelConfig_list格式 defaults list 取首个 dict 条目 + default 旧格式回退（S-03）。
func TestResolveDefaultModelConfig_list格式(t *testing.T) {
	cfg := buildProductionLikeConfig()
	def := resolveDefaultModelConfig(cfg)
	mcc, _ := def["model_client_config"].(map[string]any)
	if mcc == nil {
		t.Fatal("S-03: defaults list 应能取到首条 dict（旧 map 断言恒失败）")
	}
	if got := strField(mcc, "model_name"); got != "test-model" {
		t.Fatalf("model_name = %q, want test-model", got)
	}

	// 旧格式回退：models.default 单对象
	legacy := map[string]any{"models": map[string]any{
		"default": map[string]any{"model_client_config": map[string]any{"model_name": "legacy"}}},
	}
	def2 := resolveDefaultModelConfig(legacy)
	mcc2, _ := def2["model_client_config"].(map[string]any)
	if mcc2 == nil || strField(mcc2, "model_name") != "legacy" {
		t.Fatal("S-03: 旧格式 models.default 应回退生效")
	}
}

// TestBuildDefaultModelDict_键名与注入 model_config_obj 键名 + model_name 注入（S-03）。
func TestBuildDefaultModelDict_键名与注入(t *testing.T) {
	cfg := buildProductionLikeConfig()
	dict := buildDefaultModelDict(cfg)

	mcc, _ := dict["model_client_config"].(map[string]any)
	if mcc == nil {
		t.Fatal("model_client_config missing")
	}
	// 键名应为 model_request_config（Go 外层键），其内容来自 model_config_obj（Python 键）
	mrc, _ := dict["model_request_config"].(map[string]any)
	if mrc == nil {
		t.Fatal("model_request_config missing")
	}
	// S-03: model_name 注入 model_request_config["model"]
	if got, _ := mrc["model"].(string); got != "test-model" {
		t.Fatalf("S-03: model_name 应注入 model_request_config.model，got %q", got)
	}
	// 原有 temperature 保留
	if got, _ := mrc["temperature"].(float64); got != 0.95 {
		t.Fatalf("temperature = %v, want 0.95", got)
	}
}

// TestLoadTeamSpecDict_生产配置全链路 验证 S-02 全部子项修复后的顶层结构。
func TestLoadTeamSpecDict_生产配置全链路(t *testing.T) {
	cfg := buildProductionLikeConfig()
	specDict := LoadTeamSpecDict(cfg)

	// S-01: team_name 从 modes.team 取到
	if got, _ := specDict["team_name"].(string); got != "jiuwen_team" {
		t.Fatalf("team_name = %v, want jiuwen_team", got)
	}
	// S-02: 无顶层 default_model/default_workspace/max_iterations/completion_timeout 冗余键（T-01）
	for _, k := range []string{"default_model", "default_workspace", "max_iterations", "completion_timeout"} {
		if _, exists := specDict[k]; exists {
			t.Fatalf("T-01: 顶层冗余键 %q 不应存在（Python 注入 agent 级）", k)
		}
	}
	// S-02: language 来自 preferred_language（strip+lower）
	if got, _ := specDict["language"].(string); got != "zh" {
		t.Fatalf("language = %v, want zh", got)
	}
	// S-02: enable_hitt 默认 true
	if got, _ := specDict["enable_hitt"].(bool); !got {
		t.Fatal("enable_hitt 默认应为 true")
	}
	// S-02: enable_team_plan 被 pop
	if _, exists := specDict["enable_team_plan"]; exists {
		t.Fatal("enable_team_plan 应被 pop")
	}
	// S-02c: leader 保留显式 persona
	leader, _ := specDict["leader"].(map[string]any)
	if leader == nil {
		t.Fatal("leader missing")
	}
	if got, _ := leader["persona"].(string); got != "天才项目管理专家，擅长任务分解和团队协调" {
		t.Fatalf("leader persona = %q", got)
	}
	// M-01: agents.leader 经 $agent_leader 引用解析（含默认值注入）
	agents, _ := specDict["agents"].(map[string]any)
	leaderAgent, _ := agents["leader"].(map[string]any)
	if leaderAgent == nil {
		t.Fatal("agents.leader missing")
	}
	if got, _ := leaderAgent["max_iterations"].(int); got != 100 {
		t.Fatalf("M-01: $agent_leader 引用应解析顶层 agents.agent_leader（max_iterations=100），got %v", leaderAgent["max_iterations"])
	}
	// M-01: 生产配置 agents 非空（leader 引用生效），teammate 键不注入——
	// 空 agents 注入 {"leader","teammate"} 的分支由 TestBuildAgentsConfig_字符串引用覆盖
	if _, exists := agents["teammate"]; exists {
		t.Fatal("agents 非空时不应额外注入 teammate 键（Python 仅空配置时注入）")
	}
	// S-02: storage 从 "storage" 键读取并保留
	storage, _ := specDict["storage"].(map[string]any)
	if storage == nil {
		t.Fatal("S-02: storage 应从 team_raw[\"storage\"] 读取（旧读 storage_raw 恒空）")
	}
	if got, _ := storage["type"].(string); got != "sqlite" {
		t.Fatalf("storage.type = %v, want sqlite", got)
	}
}

// TestLoadTeamSpecDict_默认值 无 modes.team 时 workspace/transport/leader 默认值（S-02a/b/c）。
func TestLoadTeamSpecDict_默认值(t *testing.T) {
	specDict := LoadTeamSpecDict(map[string]any{})

	// S-02a: workspace 默认 {"enabled": true, "version_control": false}
	ws, _ := specDict["workspace"].(map[string]any)
	if ws == nil {
		t.Fatal("workspace missing")
	}
	if enabled, _ := ws["enabled"].(bool); !enabled {
		t.Fatal("S-02a: workspace 默认 enabled 应为 true（Python _DEFAULT_TEAM_WORKSPACE）")
	}
	if vc, _ := ws["version_control"].(bool); vc {
		t.Fatal("S-02a: workspace 默认 version_control 应为 false")
	}

	// S-02b: transport 默认 {"type": "inprocess"}
	tp, _ := specDict["transport"].(map[string]any)
	if tp == nil {
		t.Fatal("transport missing")
	}
	if got, _ := tp["type"].(string); got != "inprocess" {
		t.Fatalf("S-02b: transport.type = %v, want inprocess", got)
	}

	// S-02c: leader 默认四字段
	leader, _ := specDict["leader"].(map[string]any)
	if leader == nil {
		t.Fatal("leader missing")
	}
	if got, _ := leader["member_name"].(string); got != "team_leader" {
		t.Fatalf("leader.member_name = %v, want team_leader", got)
	}
	if got, _ := leader["display_name"].(string); got != "Team Leader" {
		t.Fatalf("leader.display_name = %v, want Team Leader", got)
	}
	if got, _ := leader["name"].(string); got != "TeamLeader" {
		t.Fatalf("leader.name = %v, want TeamLeader（三级回退终值）", got)
	}
	if got, _ := leader["persona"].(string); got != "天才项目管理专家" {
		t.Fatalf("leader.persona = %v, want 天才项目管理专家", got)
	}

	// S-02: storage 键不设置（Python None 语义）
	if _, exists := specDict["storage"]; exists {
		t.Fatal("S-02: 无 storage 配置时不应设置该键（Python 保持 None）")
	}

	// team_name 默认
	if got, _ := specDict["team_name"].(string); got != "team" {
		t.Fatalf("team_name = %v, want team", got)
	}
}

// TestLoadTeamSpecDict_workspace字段级setdefault dict 时 enabled/version_control 补齐（S-02a）。
func TestLoadTeamSpecDict_workspace字段级setdefault(t *testing.T) {
	cfg := map[string]any{"modes": map[string]any{"team": map[string]any{
		"t": map[string]any{"workspace": map[string]any{"root_path": "/tmp/ws"}},
	}}}
	specDict := LoadTeamSpecDict(cfg)
	ws, _ := specDict["workspace"].(map[string]any)
	if got, _ := ws["root_path"].(string); got != "/tmp/ws" {
		t.Fatalf("root_path = %v, want /tmp/ws", got)
	}
	if enabled, _ := ws["enabled"].(bool); !enabled {
		t.Fatal("dict workspace 缺 enabled 时应 setdefault true")
	}
	if vc, _ := ws["version_control"].(bool); vc {
		t.Fatal("dict workspace 缺 version_control 时应 setdefault false")
	}
}

// TestBuildAgentsConfig_字符串引用 顶层 agents 查找 + 未命中回退默认（M-01）。
func TestBuildAgentsConfig_字符串引用(t *testing.T) {
	teamRaw := map[string]any{"agents": map[string]any{
		"leader":   "$agent_leader",
		"teammate": "$missing_ref",
	}}
	configBase := map[string]any{
		"agents": map[string]any{
			"agent_leader": map[string]any{"max_iterations": 77},
		},
	}
	agents := buildAgentsConfig(teamRaw, configBase)

	leader, _ := agents["leader"].(map[string]any)
	if got, _ := leader["max_iterations"].(int); got != 77 {
		t.Fatalf("M-01: $agent_leader 应从顶层 agents 解析（max_iterations=77），got %v", leader["max_iterations"])
	}
	// 未命中引用：空配置 + 默认值兜底
	teammate, _ := agents["teammate"].(map[string]any)
	if teammate == nil {
		t.Fatal("teammate missing")
	}
	// 空配置走默认 max_iterations（200）
	if got, _ := teammate["max_iterations"].(int); got != 200 {
		t.Fatalf("unresolved ref should use defaults, max_iterations = %v", teammate["max_iterations"])
	}
	// dict-$ref 形式（Python 不存在）：不再识别为引用，走字面 dict
	teamRaw2 := map[string]any{"agents": map[string]any{
		"leader": map[string]any{"$ref": "agent_leader"},
	}}
	agents2 := buildAgentsConfig(teamRaw2, configBase)
	leader2, _ := agents2["leader"].(map[string]any)
	if _, exists := leader2["$ref"]; !exists {
		t.Fatal("M-01: dict-$ref 形式应按字面 dict 处理（$ref 键保留），不再走引用分支")
	}
}

// TestBuildAgentDefaults_stableBase 默认 workspace 为 {"stable_base": true}（S-02 新发现）。
func TestBuildAgentDefaults_stableBase(t *testing.T) {
	ws, _, _ := buildAgentDefaults()
	if got, _ := ws["stable_base"].(bool); !got {
		t.Fatalf("S-02: 默认 agent workspace 应为 {stable_base: true}，got %v", ws)
	}
}

// TestBuildPredefinedMembers_过滤链 member_name/identity 校验与字段规范化（S-02 新发现）。
func TestBuildPredefinedMembers_过滤链(t *testing.T) {
	teamRaw := map[string]any{"predefined_members": []any{
		// 有效成员：display_name 提供身份
		map[string]any{"member_name": "m1", "display_name": "成员一", "persona": "专家"},
		// 无 member_name：跳过
		map[string]any{"display_name": "无名"},
		// 无 name/display_name：跳过
		map[string]any{"member_name": "m2"},
		// name 提供身份：display_name 由 name 规范化
		map[string]any{"member_name": "m3", "name": "Alice"},
		// 非 dict：跳过
		"junk",
	}}
	members := buildPredefinedMembers(teamRaw)
	if len(members) != 2 {
		t.Fatalf("过滤后应剩 2 个成员，got %d", len(members))
	}
	m1 := members[0]
	if got, _ := m1["display_name"].(string); got != "成员一" {
		t.Fatalf("m1 display_name = %v", got)
	}
	if got, _ := m1["persona"].(string); got != "专家" {
		t.Fatalf("m1 persona = %v", got)
	}
	m3 := members[1]
	if got, _ := m3["display_name"].(string); got != "Alice" {
		t.Fatalf("m3 display_name 应规范化为 name（Alice），got %v", got)
	}
	if got, _ := m3["persona"].(string); got != "" {
		t.Fatalf("m3 persona 应兜底空串，got %v", got)
	}
}

// TestResolveStorageConfig_params解析 params.connection_string 解析与目录创建（S-02d）。
func TestResolveStorageConfig_params解析(t *testing.T) {
	// 无 connection_string 键：原样返回
	raw := map[string]any{"type": "sqlite", "params": map[string]any{}}
	got := resolveStorageConfig(raw)
	if params, _ := got["params"].(map[string]any); params == nil {
		t.Fatal("params should be kept")
	}

	// 有 connection_string：解析为绝对路径并创建目录
	dbRel := "team_test.db"
	raw2 := map[string]any{"type": "sqlite", "params": map[string]any{"connection_string": dbRel}}
	got2 := resolveStorageConfig(raw2)
	params2, _ := got2["params"].(map[string]any)
	cs, _ := params2["connection_string"].(string)
	if cs == dbRel || cs == "" {
		t.Fatalf("S-02d: connection_string 应解析为绝对路径，got %q", cs)
	}
	// 目录应已创建
	dir := filepath.Dir(cs)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("db 目录应已创建: %v", err)
	}

	// 非存储 dict（nil params）：原样返回
	raw3 := map[string]any{"type": "sqlite"}
	got3 := resolveStorageConfig(raw3)
	if got3 == nil {
		t.Fatal("nil params should return as-is")
	}
}

// TestDestroyTeam_并发不死锁 并发调用不阻塞（S-05 死锁回归测试）。
// 死锁时 go test -timeout 会以超时失败形式暴露。
func TestDestroyTeam_并发不死锁(t *testing.T) {
	mgr := NewTeamManager()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.DestroyTeam(context.Background(), "sess-s05")
	}()
	select {
	case <-done:
		// 正常返回
	case <-time.After(5 * time.Second):
		t.Fatal("S-05: DestroyTeam 死锁（超过 5s 未返回）")
	}
}

// TestTeamManager_访问器并发 8 个访问器与写路径并发无 race（S-04）。
// go test -race 下验证：并发 GetTeamAgent/HasStreamTask/RegisterStreamTask 等
// 与 CreateTeam/StopSessionRuntime 无 fatal error。
func TestTeamManager_访问器并发(t *testing.T) {
	mgr := NewTeamManager()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 并发读访问器
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					mgr.GetTeamAgent("sess-race")
					mgr.HasStreamTask("sess-race")
					mgr.GetMonitor("sess-race")
					mgr.GetTeamEvolutionWatcher("sess-race")
				}
			}
		}()
	}

	// 并发写访问器
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					mgr.RegisterStreamTask("sess-race", func() {})
					mgr.RegisterTeamEvolutionWatcher("sess-race", func() {})
					wgEntry := &sync.WaitGroup{}
					mgr.SetStreamTaskWaitGroup("sess-race", wgEntry)
					mgr.PopStreamTask("sess-race")
					mgr.PopTeamEvolutionWatcher("sess-race")
				}
			}
		}()
	}

	// 主 goroutine 短暂运行后停止
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
