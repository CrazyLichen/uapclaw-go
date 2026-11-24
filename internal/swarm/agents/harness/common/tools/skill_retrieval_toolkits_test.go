package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
)

// writeSkillDir 在临时目录下生成一个最小技能（SKILL.md frontmatter）。
func writeSkillDir(t *testing.T, root, name, description string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\nbody text\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dirsOf(paths ...string) SkillDirectoriesProvider {
	return func() []string { return paths }
}

func staticDisabled(names ...string) DisabledSkillsProvider {
	return func() []string { return names }
}

// TestNewSkillRetrievalToolkit_小清单small全量注入
func TestNewSkillRetrievalToolkit_小清单small全量注入(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first skill")
	writeSkillDir(t, root, "beta", "second skill")
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.CandidateScale() != "small" {
		t.Fatalf("want small got %s", tk.CandidateScale())
	}
	if tk.EffectiveStrategy() != "small_full" {
		t.Fatalf("want small_full got %s", tk.EffectiveStrategy())
	}
	snapshot := tk.FrozenPromptSnapshot()
	if snapshot.Mode != "small" || len(snapshot.Entries) != 2 {
		t.Fatalf("want small with 2 entries, got mode=%s entries=%d", snapshot.Mode, len(snapshot.Entries))
	}
	if tk.IndexEnabled() != true {
		t.Fatal("index_enabled should follow retrieval switch")
	}
}

// TestNewSkillRetrievalToolkit_禁用与关闭开关
func TestNewSkillRetrievalToolkit_禁用与关闭开关(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first")
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		DisabledSkills:   staticDisabled("alpha"),
		ConfigBase:       map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.FrozenPromptSnapshot().Entries) != 0 {
		t.Fatal("disabled skill should be excluded")
	}
	if tk.IndexEnabled() {
		t.Fatal("switch off should disable index")
	}
}

// TestNewSkillRetrievalToolkit_大会话large_flat降级
func TestNewSkillRetrievalToolkit_大会话large_flat降级(t *testing.T) {
	root := t.TempDir()
	// 大量技能 + 极小上下文窗口 → large
	cfg := map[string]any{
		"symphony": map[string]any{"skill_retrieval": map[string]any{
			"enabled":   true,
			"discovery": map[string]any{"context_window_tokens": 1},
		}},
	}
	for i := 0; i < 3; i++ {
		writeSkillDir(t, root, string(rune('a'+i))+"-skill", "desc")
	}
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{SkillDirectories: dirsOf(root), ConfigBase: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if tk.CandidateScale() != "large" {
		t.Fatalf("want large got %s", tk.CandidateScale())
	}
	if tk.EffectiveStrategy() != "large_flat" {
		t.Fatalf("want large_flat got %s", tk.EffectiveStrategy())
	}
	if tk.FrozenPromptSnapshot().Mode != "large-flat" {
		t.Fatalf("snapshot mode want large-flat got %s", tk.FrozenPromptSnapshot().Mode)
	}
}

// TestNewSkillRetrievalToolkit_frozenProfile恢复跳过重推
func TestNewSkillRetrievalToolkit_frozenProfile恢复跳过重推(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first skill")
	base, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := base.SessionProfile()
	data, _ := json.Marshal(profile)
	var restored map[string]any
	_ = json.Unmarshal(data, &restored)

	// 新增技能后仍以 frozen profile 为准（恢复基线非真相源）
	writeSkillDir(t, root, "gamma", "new skill")
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
		FrozenProfile:    restored,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := tk.FrozenPromptSnapshot(); len(got.Entries) != len(base.FrozenPromptSnapshot().Entries) {
		t.Fatalf("frozen snapshot should skip rescan: got %d entries", len(got.Entries))
	}
}

// TestNewSkillRetrievalToolkit_非法profile回落
func TestNewSkillRetrievalToolkit_非法profile回落(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first")
	profile := map[string]any{"schema_version": 1, "enabled": true, "settings": map[string]any{"context_window_tokens": "not-a-number"}}
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
		FrozenProfile:    profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 非法 profile 被忽略（走 live 扫描，alpha 可见）
	if len(tk.FrozenPromptSnapshot().Entries) != 1 {
		t.Fatalf("invalid profile should fall back to live scan, got %d", len(tk.FrozenPromptSnapshot().Entries))
	}
}

// TestIsValidSkillRetrievalSessionProfile_逐项失败路径
func TestIsValidSkillRetrievalSessionProfile_逐项失败路径(t *testing.T) {
	valid := map[string]any{
		"schema_version": 1,
		"enabled":        false,
	}
	if !IsValidSkillRetrievalSessionProfile(valid) {
		t.Fatal("enabled=false 短路放行")
	}
	cases := []map[string]any{
		{"schema_version": 2},
		{"schema_version": 1, "enabled": true, "index_enabled": "yes"},
		{"schema_version": 1, "index_enabled": true},
		{"schema_version": 1, "index_enabled": true, "selection_cards": map[string]any{}, "settings": map[string]any{"max_results": -1}},
	}
	for i, c := range cases {
		if IsValidSkillRetrievalSessionProfile(c) {
			t.Fatalf("case %d should be invalid", i)
		}
	}
	// index_snapshot 缺失但 pinned revision 非空 → 无效
	bad := map[string]any{
		"schema_version": 1, "index_enabled": true,
		"selection_cards": map[string]any{},
		"settings": map[string]any{"candidate_budget_ratio": 0.01, "context_window_tokens": 200000,
			"max_output_chars": 12000, "max_results": 10, "max_list_entries": 40,
			"incremental_notice_max_chars": 4000, "use_existing_index": false,
			"prompt_preferred_skills": []any{}},
		"prompt_snapshot": map[string]any{"mode": "small", "index_state": "missing",
			"total_count": 0, "estimated_candidate_tokens": 0, "candidate_budget_tokens": 2000,
			"entries": []any{}, "branches": []any{}},
		"pinned_index_revision": "abc",
	}
	if IsValidSkillRetrievalSessionProfile(bad) {
		t.Fatal("pinned revision without index_snapshot must be invalid")
	}
	bad["index_snapshot"] = map[string]any{"nodes": []any{map[string]any{"cid": "w1", "type": "leaf", "worker_id": "w1"}}, "record_hashes": map[string]any{"w1": "h1"}, "fingerprint": "abc"}
	if !IsValidSkillRetrievalSessionProfile(bad) {
		t.Fatal("fingerprint matching revision should validate")
	}
}

// TestSessionProfile往返_键全量
func TestSessionProfile往返_键全量(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first")
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := tk.SessionProfile()
	for _, key := range []string{"schema_version", "session_id", "index_enabled", "candidate_scale",
		"estimated_candidate_tokens", "candidate_budget_tokens", "effective_strategy", "layout",
		"index_state", "pinned_index_revision", "searchable_count", "settings",
		"selection_cards", "prompt_snapshot", "index_snapshot"} {
		if _, ok := profile[key]; !ok {
			t.Fatalf("missing key %s", key)
		}
	}
	if profile["schema_version"].(int) != 1 {
		t.Fatal("schema_version must be 1")
	}
	if layout := profile["layout"].(string); layout != "flat" {
		t.Fatalf("want flat got %s", layout)
	}
}

// TestSkillRetrievalArtifactRoot_三级解析
func TestSkillRetrievalArtifactRoot_三级解析(t *testing.T) {
	got := SkillRetrievalArtifactRoot(map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"artifact_root": "/tmp/rt1"}}})
	if got != "/tmp/rt1" {
		t.Fatalf("config root want /tmp/rt1 got %s", got)
	}
	t.Setenv("SYMPHONY_SKILL_RETRIEVAL_ROOT", "/tmp/rt2")
	got = SkillRetrievalArtifactRoot(map[string]any{})
	if got != "/tmp/rt2" {
		t.Fatalf("env root want /tmp/rt2 got %s", got)
	}
	t.Setenv("SYMPHONY_SKILL_RETRIEVAL_ROOT", "")
	got = SkillRetrievalArtifactRoot(nil)
	if got == "" || filepath.Base(got) != "skill_retrieval" {
		t.Fatalf("workspace fallback got %s", got)
	}
}

// TestBuildModelDiscoverySettings_窗口覆盖
func TestBuildModelDiscoverySettings_窗口覆盖(t *testing.T) {
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{"context_window_tokens": 4096}}}
	settings := BuildModelDiscoverySettings(cfg, "m1", nil, nil)
	if settings.ContextWindowTokens != 4096 {
		t.Fatalf("want 4096 got %d", settings.ContextWindowTokens)
	}
	// 无模型信息 → BuildDiscoverySettings 默认
	settings = BuildModelDiscoverySettings(map[string]any{}, "", nil, nil)
	if settings.ContextWindowTokens != 200000 {
		t.Fatalf("default want 200000 got %d", settings.ContextWindowTokens)
	}
}

// TestResolveSkillRetrievalStrategy_四态
func TestResolveSkillRetrievalStrategy_四态(t *testing.T) {
	cases := []struct {
		enabled  bool
		scale    string
		layout   string
		state    string
		expected string
	}{
		{false, "", "", "", "legacy"},
		{true, "small", "", "", "small_full"},
		{true, "large", "flat", "missing", "large_flat"},
		{true, "large", "tree", "fresh", "indexed"},
		{true, "large", "tree", "stale", "indexed_stale"},
	}
	for _, c := range cases {
		if got := ResolveSkillRetrievalStrategy(c.enabled, c.scale, c.layout, c.state); got != c.expected {
			t.Fatalf("want %s got %s", c.expected, got)
		}
	}
}

// TestSkillSourcesFromManager_投影
func TestSkillSourcesFromManager_投影(t *testing.T) {
	sources := SkillSourcesFromManager(nil)
	if len(sources) != 0 {
		t.Fatalf("nil manager want empty map got %v", sources)
	}
}

// TestSkillIndex_工具转发三操作可达
func TestSkillIndex_工具转发三操作可达(t *testing.T) {
	root := t.TempDir()
	writeSkillDir(t, root, "alpha", "first skill about pdf")
	tk, err := NewSkillRetrievalToolkit(ToolkitOptions{
		SkillDirectories: dirsOf(root),
		ConfigBase:       map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := tk.GetTools()
	if len(tools) != 1 || tools[0].Name != discovery.SkillIndexToolName {
		t.Fatalf("want single skill_index tool got %d", len(tools))
	}
	result, err := tk.SkillIndex(map[string]any{"operation": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModelContent == "" {
		t.Fatal("list should produce model content")
	}
}
