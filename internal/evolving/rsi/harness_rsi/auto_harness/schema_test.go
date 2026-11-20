package autoharness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestConfigYAML 写一份临时配置 YAML 内容
func newTestConfigYAML(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	return path
}

// TestTaskStatus_枚举值 验证状态成员字符串
func TestTaskStatus_枚举值(t *testing.T) {
	cases := map[TaskStatus]string{
		TaskStatusPending:  "pending",
		TaskStatusRunning:  "running",
		TaskStatusSuccess:  "success",
		TaskStatusFailed:   "failed",
		TaskStatusTimeout:  "timeout",
		TaskStatusReverted: "reverted",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Fatalf("TaskStatus %q != %q", status, want)
		}
	}
}

// TestExperienceType_枚举值 验证类型成员字符串
func TestExperienceType_枚举值(t *testing.T) {
	if string(ExperienceTypeOptimization) != "optimization" ||
		string(ExperienceTypeFailure) != "failure" ||
		string(ExperienceTypeInsight) != "insight" {
		t.Fatal("ExperienceType 成员值漂移")
	}
}

// TestStageSlot_枚举值 验证阶段成员字符串
func TestStageSlot_枚举值(t *testing.T) {
	for _, s := range []StageSlot{
		StageSlotAssess, StageSlotPlan, StageSlotImplement, StageSlotVerify,
		StageSlotActivate, StageSlotCommit, StageSlotPublish, StageSlotLearnings,
	} {
		if len(s) == 0 {
			t.Fatal("StageSlot 成员缺失")
		}
	}
	if string(StageSlotAssess) != "assess" || string(StageSlotPublish) != "publish" {
		t.Fatal("StageSlot 成员值漂移")
	}
}

// TestNormalizePipelinePreference_别名与兜底 验证别名表与非法值兜底
func TestNormalizePipelinePreference_别名与兜底(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, PipelinePreferenceAuto},
		{"", PipelinePreferenceAuto},
		{"AUTO", PipelinePreferenceAuto},
		{" meta ", MetaEvolvePipeline},
		{MetaEvolvePipeline, MetaEvolvePipeline},
		{"extended", ExtendedEvolvePipeline},
		{"pr_pipeline", MetaEvolvePipeline},
		{"extended_harness_pipeline", ExtendedEvolvePipeline},
		{"bogus", PipelinePreferenceAuto},
	}
	for _, c := range cases {
		if got := NormalizePipelinePreference(c.in); got != c.want {
			t.Fatalf("NormalizePipelinePreference(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGapPriority 验证 priority = impact x feasibility
func TestGapPriority(t *testing.T) {
	g := Gap{Impact: 0.8, Feasibility: 0.5}
	if g.Priority() != 0.4 {
		t.Fatalf("Priority = %v, want 0.4", g.Priority())
	}
}

// TestExperience_默认ID与时间戳 验证 12 位 hex ID 与时间戳生成
func TestExperience_默认ID与时间戳(t *testing.T) {
	id := newShortID()
	if len(id) != 12 {
		t.Fatalf("newShortID 长度 = %d, want 12", len(id))
	}
	e := Experience{ID: id, Timestamp: float64(1740000000)}
	if e.ID != id || e.Timestamp != 1740000000 {
		t.Fatal("Experience 默认字段赋值失败")
	}
	if e2 := newShortID(); e2 == id {
		t.Fatal("newShortID 应随机")
	}
}

// TestAutoHarnessConfigFromDict_各段落 验证嵌套 YAML 字典的逐段映射
func TestAutoHarnessConfigFromDict_各段落(t *testing.T) {
	data := map[string]any{
		"data_dir":     "/tmp/ah",
		"local_repo":   "/repo",
		"repo_url":     "https://gitcode.com/foo/bar.git",
		"skills_dirs":  []any{"/s1", "/s2"},
		"language":     "en",
		"pipeline":     "pr_pipeline",
		"workspace":    "/old-ws",
		"git": map[string]any{
			"remote":        "fork",
			"base_branch":   "main",
			"user_name":     "n",
			"user_email":    "e",
			"fork_owner":    "me",
			"upstream_owner": "up",
			"upstream_repo": "repo-x",
		},
		"gitcode": map[string]any{
			"username":         "u",
			"access_token_env": "MY_TOKEN_ENV",
			"access_token":     "tok",
		},
		"budget": map[string]any{
			"session_secs":         1234.5,
			"cost_limit_usd":       20,
			"task_timeout_secs":    100,
			"model_timeout_secs":   200,
			"max_tasks_per_session": 7,
		},
		"ci_gate": map[string]any{
			"config_path":       "/ci.yaml",
			"python_executable": "/py",
			"install_command":   "uv sync",
		},
		"fix_loop": map[string]any{
			"phase1_max_retries": 3,
			"phase2_max_retries": 4,
		},
		"agent": map[string]any{
			"implement": "55",
			"plan":      42,
			"bad":       "not-a-number",
		},
		"extensions": map[string]any{
			"stage_registrars":    []any{"m:f"},
			"pipeline_registrars": []any{"p:f"},
		},
	}
	cfg := AutoHarnessConfigFromDict(data)

	if cfg.DataDir != "/tmp/ah" || cfg.LocalRepo != "/repo" || cfg.RepoURL != "https://gitcode.com/foo/bar.git" {
		t.Fatalf("顶层字段: %+v", cfg)
	}
	if len(cfg.SkillsDirs) != 2 || cfg.Language != "en" {
		t.Fatalf("skills_dirs/language: %+v", cfg)
	}
	if cfg.PipelinePreference != MetaEvolvePipeline {
		t.Fatalf("pipeline 别名解析 = %q", cfg.PipelinePreference)
	}
	if cfg.Workspace != "/old-ws" {
		t.Fatalf("兼容旧字段 workspace = %q", cfg.Workspace)
	}
	if cfg.GitRemote != "fork" || cfg.GitBaseBranch != "main" || cfg.ForkOwner != "me" ||
		cfg.UpstreamOwner != "up" || cfg.UpstreamRepo != "repo-x" {
		t.Fatalf("git 段: %+v", cfg)
	}
	if cfg.GitcodeUsername != "u" || cfg.GitcodeTokenEnv != "MY_TOKEN_ENV" || cfg.GitcodeToken != "tok" {
		t.Fatalf("gitcode 段: %+v", cfg)
	}
	if cfg.SessionBudgetSecs != 1234.5 || cfg.CostLimitUSD != 20 || cfg.MaxTasksPerSession != 7 {
		t.Fatalf("budget 段: %+v", cfg)
	}
	if cfg.CIGateConfig != "/ci.yaml" || cfg.CIGateInstallCommand != "uv sync" {
		t.Fatalf("ci_gate 段: %+v", cfg)
	}
	if cfg.FixPhase1MaxRetries != 3 || cfg.FixPhase2MaxRetries != 4 {
		t.Fatalf("fix_loop 段: %+v", cfg)
	}
	if cfg.AgentIterations["implement"] != 55 || cfg.AgentIterations["plan"] != 42 {
		t.Fatalf("agent 段字符串/数值转换: %+v", cfg.AgentIterations)
	}
	if _, ok := cfg.AgentIterations["bad"]; ok {
		t.Fatal("非法 agent 迭代值应被跳过")
	}
	if len(cfg.StageRegistrars) != 1 || len(cfg.PipelineRegistrars) != 1 {
		t.Fatalf("extensions 段: %+v", cfg)
	}
}

// TestAutoHarnessConfig_默认值 验证字段默认值与 Python dataclass 一致
func TestAutoHarnessConfig_默认值(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	if cfg.RepoURL != defaultRepoURL || cfg.Language != "cn" {
		t.Fatalf("路径/语言默认值: %+v", cfg)
	}
	if cfg.SessionBudgetSecs != 900000.0 || cfg.CostLimitUSD != 10.0 ||
		cfg.TaskTimeoutSecs != 300000.0 || cfg.MaxTasksPerSession != 10 ||
		cfg.SelfDrivenSlots != 1 || cfg.ExtensionVerifyConcurrency != 4 {
		t.Fatalf("预算默认值: %+v", cfg)
	}
	if cfg.GitBaseBranch != "develop" || cfg.UpstreamOwner != "openJiuwen" || cfg.UpstreamRepo != "agent-core" {
		t.Fatalf("git 默认值: %+v", cfg)
	}
	if cfg.GitcodeTokenEnv != "GITCODE_ACCESS_TOKEN" {
		t.Fatalf("gitcode env 默认值: %+v", cfg)
	}
	if cfg.FixPhase1MaxRetries != 10 || cfg.FixPhase2MaxRetries != 9 {
		t.Fatalf("fix_loop 默认值: %+v", cfg)
	}
	if len(cfg.CommunitySkillRepos) != 2 || len(cfg.AgentIterations) != 10 {
		t.Fatalf("默认列表/映射: %+v", cfg)
	}
	if cfg.AgentIterations["explore_subagent"] != 20 || cfg.AgentIterations["merge_ext"] != 8 {
		t.Fatalf("agent 默认迭代数: %+v", cfg.AgentIterations)
	}
}

// TestResolveRepoName 验证仓库目录名解析（upstream 优先、URL stem 兜底）
func TestResolveRepoName(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	if got := cfg.ResolveRepoName(); got != "agent-core" {
		t.Fatalf("ResolveRepoName = %q", got)
	}
	cfg2 := newDefaultAutoHarnessConfig()
	cfg2.UpstreamRepo = ""
	cfg2.RepoURL = "https://gitcode.com/openJiuwen/myrepo.git/"
	if got := cfg2.ResolveRepoName(); got != "myrepo" {
		t.Fatalf("URL stem = %q", got)
	}
	cfg3 := newDefaultAutoHarnessConfig()
	cfg3.UpstreamRepo = ""
	cfg3.RepoURL = ""
	if got := cfg3.ResolveRepoName(); got != "repository" {
		t.Fatalf("空兜底 = %q", got)
	}
}

// TestResolveGitcodeToken_配置优先env兜底
func TestResolveGitcodeToken_配置优先env兜底(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.GitcodeToken = "direct"
	if got := cfg.ResolveGitcodeToken(); got != "direct" {
		t.Fatalf("配置 token = %q", got)
	}
	cfg.GitcodeToken = ""
	cfg.GitcodeTokenEnv = "AH_TEST_TOKEN"
	if got := cfg.ResolveGitcodeToken(); got != "" {
		t.Fatalf("未设 env 应为空: %q", got)
	}
	t.Setenv("AH_TEST_TOKEN", "env-token")
	if got := cfg.ResolveGitcodeToken(); got != "env-token" {
		t.Fatalf("env token = %q", got)
	}
}

// TestResolveGitcodeUsername 验证 username→fork_owner 兜底链
func TestResolveGitcodeUsername(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.GitcodeUsername = "u1"
	if got := cfg.ResolveGitcodeUsername(); got != "u1" {
		t.Fatalf("username = %q", got)
	}
	cfg.GitcodeUsername = ""
	cfg.ForkOwner = "f1"
	if got := cfg.ResolveGitcodeUsername(); got != "f1" {
		t.Fatalf("fork_owner = %q", got)
	}
	cfg.ForkOwner = ""
	if got := cfg.ResolveGitcodeUsername(); got != "" {
		t.Fatalf("空兜底 = %q", got)
	}
}

// TestBuildPaths 验证派生路径
func TestBuildPaths(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.DataDir = "/dd"
	paths := cfg.BuildPaths()
	if paths.DataDir != "/dd" ||
		paths.ExperienceDir != "/dd/experience" ||
		paths.WorktreesDir != "/dd/worktrees" ||
		paths.RunsDir != "/dd/runs" ||
		paths.CacheRepoDir != "/dd/repo/agent-core" ||
		paths.RuntimeExtensionsDir != "/dd/runtime_extensions" {
		t.Fatalf("BuildPaths = %+v", paths)
	}
	// 无 data_dir 的回落形态
	cfg2 := newDefaultAutoHarnessConfig()
	paths2 := cfg2.BuildPaths()
	if paths2.WorktreesDir != ".auto_harness/worktrees/" ||
		paths2.CacheRepoDir != ".auto_harness/repo/agent-core" {
		t.Fatalf("无 data_dir 回落 = %+v", paths2)
	}
}

// TestResolvedExperienceDir_配置优先 验证 experience_dir 配置优先
func TestResolvedExperienceDir_配置优先(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.ExperienceDir = "/custom"
	if got := cfg.ResolvedExperienceDir(); got != "/custom" {
		t.Fatalf("配置优先 = %q", got)
	}
	cfg.ExperienceDir = ""
	cfg.DataDir = ""
	if got := cfg.ResolvedExperienceDir(); got != ".auto_harness/experience/" {
		t.Fatalf("回落 = %q", got)
	}
}

// TestResolveAgentIterations_缺失回默认
func TestResolveAgentIterations_缺失回默认(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	if got := cfg.ResolveAgentIterations("nonexistent", 9); got != 9 {
		t.Fatalf("缺失回默认 = %d", got)
	}
	if got := cfg.ResolveAgentIterations("implement", 1); got != 30 {
		t.Fatalf("命中默认表 = %d", got)
	}
}

// TestResolveImmutableFiles_配置优先默认兜底
func TestResolveImmutableFiles_配置优先默认兜底(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.ImmutableFiles = []string{"a.md"}
	got := cfg.ResolveImmutableFiles()
	if len(got) != 1 || got[0] != "a.md" {
		t.Fatalf("配置优先 = %v", got)
	}
	cfg.ImmutableFiles = nil
	got = cfg.ResolveImmutableFiles()
	if len(got) != 3 || !strings.Contains(got[0], "identity.md") {
		t.Fatalf("内置默认 = %v", got)
	}
}

// TestBuildProjectProfile 验证画像构建
func TestBuildProjectProfile(t *testing.T) {
	cfg := newDefaultAutoHarnessConfig()
	cfg.RepoURL = defaultRepoURL
	cfg.GitBaseBranch = "develop-auto-harness"
	profile := cfg.BuildProjectProfile()
	if profile.RepoURL != defaultRepoURL ||
		profile.RepoSlug != "openJiuwen/agent-core" ||
		profile.Platform != "gitcode" ||
		profile.DefaultBaseBranch != "develop-auto-harness" ||
		profile.DefaultCIProfile != "default" ||
		len(profile.ImmutableFiles) != 3 ||
		len(profile.HighImpactPrefixes) != 1 {
		t.Fatalf("ProjectProfile = %+v", profile)
	}
	// base_branch 为空回落 develop
	cfg.GitBaseBranch = ""
	if p := cfg.BuildProjectProfile(); p.DefaultBaseBranch != "develop" {
		t.Fatalf("空 base_branch 回落 = %q", p.DefaultBaseBranch)
	}
}

// TestLoadAutoHarnessConfig_缺失时bootstrap 验证缺文件→复制模板→默认配置
func TestLoadAutoHarnessConfig_缺失时bootstrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")
	cfg := LoadAutoHarnessConfig(path, "")
	if !cfg.ConfigBootstrapped {
		t.Fatal("ConfigBootstrapped 应为 true")
	}
	if cfg.ConfigPath != path {
		t.Fatalf("ConfigPath = %q", cfg.ConfigPath)
	}
	if cfg.DataDir != filepath.Dir(path) {
		t.Fatalf("DataDir 应回落到配置目录 = %q", cfg.DataDir)
	}
	if !isRegularFile(path) {
		t.Fatal("模板应被复制到配置路径")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "gitcode:") {
		t.Fatal("复制的内容应为出厂模板")
	}
}

// TestLoadAutoHarnessConfig_正常加载 验证 YAML 文件加载与 data_dir 默认
func TestLoadAutoHarnessConfig_正常加载(t *testing.T) {
	dir := t.TempDir()
	path := newTestConfigYAML(t, dir, "language: en\nbudget:\n  cost_limit_usd: 3.5\n")
	cfg := LoadAutoHarnessConfig(path, "")
	if cfg.ConfigBootstrapped {
		t.Fatal("已存在的文件不应标记 bootstrap")
	}
	if cfg.Language != "en" || cfg.CostLimitUSD != 3.5 {
		t.Fatalf("加载结果: language=%q cost=%v", cfg.Language, cfg.CostLimitUSD)
	}
	if cfg.DataDir != dir {
		t.Fatalf("DataDir 默认为配置目录 = %q", cfg.DataDir)
	}
}

// TestLoadAutoHarnessConfig_损坏回落默认 验证非法 YAML 回落默认配置
func TestLoadAutoHarnessConfig_损坏回落默认(t *testing.T) {
	dir := t.TempDir()
	path := newTestConfigYAML(t, dir, "language: [unclosed")
	cfg := LoadAutoHarnessConfig(path, "")
	if cfg.Language != "cn" {
		t.Fatalf("损坏文件应回落默认 language = %q", cfg.Language)
	}
	if cfg.DataDir != dir {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
}

// TestIsPlaceholderLocalRepo 验证模板示例值判定
func TestIsPlaceholderLocalRepo(t *testing.T) {
	if !IsPlaceholderLocalRepo("/home/user/code/agent-core") ||
		!IsPlaceholderLocalRepo(" /home/user/repo ") ||
		IsPlaceholderLocalRepo("/real/path") {
		t.Fatal("IsPlaceholderLocalRepo 判定漂移")
	}
}

// TestDetectLocalRepo_命中与未命中 验证 hint/cwd 候选探测
func TestDetectLocalRepo_命中与未命中(t *testing.T) {
	dir := t.TempDir()
	// 造一个像仓库根的目录
	repo := filepath.Join(dir, "agent-core")
	os.MkdirAll(filepath.Join(repo, "openjiuwen"), 0o755)
	os.WriteFile(filepath.Join(repo, "pyproject.toml"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(repo, ".git"), []byte(""), 0o644)

	if got := detectLocalRepo(dir); got != repo {
		t.Fatalf("hint 下探测 = %q, want %q", got, repo)
	}
	empty := t.TempDir()
	if got := detectLocalRepo(empty); got != "" {
		t.Fatalf("空目录不应命中: %q", got)
	}
}

// TestRepoURLStem 验证 URL 末段去扩展名
func TestRepoURLStem(t *testing.T) {
	cases := map[string]string{
		"https://gitcode.com/openJiuwen/agent-core.git": "agent-core",
		"https://gitcode.com/openJiuwen/agent-core.git/": "agent-core",
		"agent-core": "agent-core",
	}
	for in, want := range cases {
		if got := repoURLStem(in); got != want {
			t.Fatalf("repoURLStem(%q) = %q, want %q", in, got, want)
		}
	}
}
