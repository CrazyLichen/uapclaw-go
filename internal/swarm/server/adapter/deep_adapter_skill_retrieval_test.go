package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mcpstate "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/mcp"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

func enableSkillCfg() map[string]any {
	return map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}}
}

func writeTestSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: the " + name + " skill\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSkillRetrievalProfilePath_形态与净化
func TestSkillRetrievalProfilePath_形态与净化(t *testing.T) {
	d := &DeepAdapter{}
	path := d.skillRetrievalProfilePath("sess/1")
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".skill-retrieval.json") {
		t.Fatalf("suffix wrong: %s", base)
	}
	// 斜杠被净化
	if strings.Contains(strings.SplitN(base, ".", 2)[0], "/") {
		t.Fatalf("session key not sanitized: %s", base)
	}
	// 空键回落 default
	if !strings.HasPrefix(d.skillRetrievalProfilePath(""), "default") && !strings.Contains(d.skillRetrievalProfilePath(""), "default") {
		t.Fatalf("empty sid should fall back to default: %s", d.skillRetrievalProfilePath(""))
	}
}

// TestSkillRetrievalProfileChecksum_往返与敏感
func TestSkillRetrievalProfileChecksum_往返与敏感(t *testing.T) {
	profile := map[string]any{"schema_version": 1, "session_id": "s1", "enabled": false}
	// 带checksum键剔除后计算
	profile["profile_checksum"] = "irrelevant"
	sum := skillRetrievalProfileChecksum(profile)
	if len(sum) != 64 {
		t.Fatalf("want 64 hex got %s", sum)
	}
	delete(profile, "profile_checksum")
	if skillRetrievalProfileChecksum(profile) != sum {
		t.Fatal("checksum key must not affect sum")
	}
	profile["enabled"] = true
	if skillRetrievalProfileChecksum(profile) == sum {
		t.Fatal("value change must alter checksum")
	}
}

// TestLoadSkillRetrievalSessionProfile_无文件不降级
func TestLoadSkillRetrievalSessionProfile_无文件不降级(t *testing.T) {
	d := &DeepAdapter{}
	payload, found := d.loadSkillRetrievalSessionProfile("no-such-session")
	if found || payload != nil {
		t.Fatal("missing file → (nil,false) not degraded dict")
	}
}

// TestPersistLoadSkillRetrievalSessionProfile_往返
func TestPersistLoadSkillRetrievalSessionProfile_往返(t *testing.T) {
	// 重定向 ConfigDir 到临时目录（sidecar 落盘隔离）
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := &DeepAdapter{isSessionScoped: true, parentSessionID: "sess-x"}
	root := t.TempDir()
	writeTestSkill(t, root, "alpha")
	tk, err := swarmtools.NewSkillRetrievalToolkit(swarmtools.ToolkitOptions{
		SkillDirectories: func() []string { return []string{root} },
		ConfigBase:       enableSkillCfg(),
		SessionScope:     "sess-x",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.skillRetrievalToolkit = tk
	enabledTrue := true
	d.skillRetrievalSessionEnabled = &enabledTrue
	d.persistSkillRetrievalSessionProfile()

	path := d.skillRetrievalProfilePath("sess-x")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	// 0o600 权限
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm want 0600 got %o", info.Mode().Perm())
	}
	payload, found := d.loadSkillRetrievalSessionProfile("sess-x")
	if !found || payload == nil {
		t.Fatal("roundtrip should find valid payload")
	}
	if _, hasErr := payload["profile_recovery_error"]; hasErr {
		t.Fatalf("valid profile degraded: %v", payload)
	}
	if payload["session_id"] != "sess-x" {
		t.Fatalf("session_id wrong: %v", payload["session_id"])
	}
}

// TestLoadSkillRetrievalSessionProfile_五重校验逐项
func TestLoadSkillRetrievalSessionProfile_五重校验逐项(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := &DeepAdapter{isSessionScoped: true, parentSessionID: "sid-1"}
	d.persistSkillRetrievalSessionProfile()
	path := d.skillRetrievalProfilePath("sid-1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)

	expectReason := func(mutated map[string]any, reason string) {
		t.Helper()
		data, _ := json.Marshal(mutated)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		got, _ := d.loadSkillRetrievalSessionProfile("sid-1")
		if got == nil {
			t.Fatalf("case %s: expected degraded dict", reason)
		}
		if got["profile_recovery_error"] != reason {
			t.Fatalf("case %s: got %v", reason, got["profile_recovery_error"])
		}
	}
	// 错 schema_version
	bad := map[string]any{}
	for k, v := range payload {
		bad[k] = v
	}
	bad["schema_version"] = 2
	expectReason(bad, "invalid_profile_schema")
	// checksum 破坏
	bad2 := map[string]any{}
	for k, v := range payload {
		bad2[k] = v
	}
	bad2["profile_checksum"] = strings.Repeat("0", 64)
	expectReason(bad2, "profile_checksum_mismatch")
	// session_id 换
	bad3 := map[string]any{}
	for k, v := range payload {
		bad3[k] = v
	}
	bad3["session_id"] = "other"
	// Python 校验序：schema → checksum → session_id；改 sid 后须同步重算
	// checksum 才能到达 session_id 校验位
	bad3["profile_checksum"] = skillRetrievalProfileChecksum(bad3)
	expectReason(bad3, "session_id_mismatch")
	// 不可读 payload（enabled 改为破坏 canonical 的值）
	bad4 := map[string]any{}
	for k, v := range payload {
		bad4[k] = v
	}
	bad4["settings"] = map[string]any{"max_results": "oops"}
	// 同步重算 checksum，确保到达 payload 校验位（Python :2536-2539）
	bad4["profile_checksum"] = skillRetrievalProfileChecksum(bad4)
	expectReason(bad4, "invalid_profile_payload")
}

// TestPersistSkillRetrievalSessionProfile_全局关闭冻结legacy
func TestPersistSkillRetrievalSessionProfile_全局关闭冻结legacy(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := &DeepAdapter{isSessionScoped: true, parentSessionID: "sid-off"}
	enabledFalse := false
	d.skillRetrievalSessionEnabled = &enabledFalse
	d.persistSkillRetrievalSessionProfile()
	path := d.skillRetrievalProfilePath("sid-off")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	if payload["enabled"] != false {
		t.Fatal("legacy freeze should record enabled=false")
	}
	// 无 toolkit 无恢复 profile：已写过 → 二次 persist 不覆盖（enabled=false 保真）
	d.restoredSkillRetrievalProfile = nil
	d.persistSkillRetrievalSessionProfile()
	raw2, _ := os.ReadFile(path)
	var payload2 map[string]any
	_ = json.Unmarshal(raw2, &payload2)
	if payload2["enabled"] != false {
		t.Fatal("second persist must not override legacy freeze")
	}
}

// TestDiscardTransientSkillRetrievalSessionProfile_恢复来源保留
func TestDiscardTransientSkillRetrievalSessionProfile_恢复来源保留(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := &DeepAdapter{isSessionScoped: true, parentSessionID: "sid-d"}
	d.persistSkillRetrievalSessionProfile()
	path := d.skillRetrievalProfilePath("sid-d")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	// 乐观来源 → 删
	d.DiscardTransientSkillRetrievalSessionProfile()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("transient sidecar should be discarded")
	}
	// 恢复来源 → 保留（restore 意味 sidecar 先前已在盘上；persist 对
	// toolkit 缺席+已恢复 profile 的组合是 no-op，Python :2549-2553）
	d2 := &DeepAdapter{isSessionScoped: true, parentSessionID: "sid-d"}
	d.persistSkillRetrievalSessionProfile() // d 先落一份
	d2.restoredSkillRetrievalProfile = map[string]any{"schema_version": 1, "enabled": false}
	d2.DiscardTransientSkillRetrievalSessionProfile()
	if _, err := os.Stat(d2.skillRetrievalProfilePath("sid-d")); err != nil {
		t.Fatal("restored sidecar must survive discard")
	}
}

// TestGetOrCreateSkillRetrievalToolkit_懒创建幂等
func TestGetOrCreateSkillRetrievalToolkit_懒创建幂等(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := &DeepAdapter{}
	root := t.TempDir()
	writeTestSkill(t, root, "alpha")
	tk1 := d.getOrCreateSkillRetrievalToolkit(enableSkillCfg())
	if tk1 == nil {
		t.Fatal("toolkit should be created")
	}
	tk2 := d.getOrCreateSkillRetrievalToolkit(enableSkillCfg())
	if tk1 != tk2 {
		t.Fatal("second call must return cached instance")
	}
}

// TestRestoreSkillRetrievalSession_不可变输入
func TestRestoreSkillRetrievalSession_不可变输入(t *testing.T) {
	d := &DeepAdapter{}
	d.RestoreSkillRetrievalSession(map[string]any{"model_name": "m1", "context_window_tokens": 4096, "requested_model_name": "req-m"}, []string{"mcp-a"}, "req-b")
	if d.skillRetrievalContextModelName != "m1" {
		t.Fatalf("context model got %s", d.skillRetrievalContextModelName)
	}
	if d.skillRetrievalContextWindowTokens != 4096 {
		t.Fatalf("window got %d", d.skillRetrievalContextWindowTokens)
	}
	// 锁定后不被覆盖
	d.setInitialSkillRetrievalModel("another")
	if d.initialSkillRetrievalModelName != "req-m" {
		t.Fatalf("initial model locked got %s", d.initialSkillRetrievalModelName)
	}
	// 无 profile → 用请求名
	d2 := &DeepAdapter{}
	d2.RestoreSkillRetrievalSession(nil, nil, "req-b")
	if d2.initialSkillRetrievalModelName != "req-b" {
		t.Fatalf("fallback model got %s", d2.initialSkillRetrievalModelName)
	}
	if d2.skillRetrievalContextWindowTokens != 0 {
		t.Fatal("no profile → window 0")
	}
}

// TestSkillScanDirs_MCP会话分支 对齐 Python _skill_scan_dirs :10200-10236
func TestSkillScanDirs_MCP会话分支(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	writeTestSkill(t, root, "alpha")
	// state.json：mcp-a connected / mcp-b registered
	// 捆绑技能目录由 mcp 状态存储拥有：<workspace>/mcp/skills/<name>/
	bundledA := filepath.Join(mcpstate.McpRoot(), "skills", "mcp-a")
	writeTestSkill(t, bundledA, "bundled-skill")
	sm := skillruntime.NewSkillManager(root)
	d := NewDeepAdapter()
	d.SetSkillManager(sm)
	d.skillManager = sm
	// mcp/state.json：mcp-a connected / mcp-b registered
	mcpstate.SaveState(map[string]any{"version": 1, "mcp": map[string]any{
		"mcp-a": map[string]any{"state": "connected"},
		"mcp-b": map[string]any{"state": "registered"},
	}, "mounts": map[string]any{}})

	// 根 adapter：恒单根（防 cli/skill 技能泄漏进 rewind/cron/admin）
	// 注：绑定包目录为空时 roots = [agent skills dir]（workspace 派生），
	// MCP 分支不参与。
	roots := d.skillScanDirs()
	for _, r := range roots {
		assert.NotEqual(t, bundledA, r, "root adapter must not scan MCP dirs")
	}

	// session-scoped + 未选中 → 不含
	d.SetSessionScopedForTest("sess-m")
	roots = d.skillScanDirs()
	for _, r := range roots {
		assert.NotEqual(t, bundledA, r, "unselected MCP must stay hidden")
	}

	// 选中 mcp-a → 出现
	d.SetSessionSelectedMcp([]string{"mcp-a"})
	roots = d.skillScanDirs()
	found := false
	for _, r := range roots {
		if strings.HasPrefix(r, bundledA) {
			found = true
		}
	}
	assert.True(t, found, "selected MCP bundled dir should be scanned: %v", roots)

	// 构造期 pending 名单（Python _pending_skill_scan_mcp_names）也放行
	d2 := NewDeepAdapter()
	d2.SetSessionScopedForTest("sess-m")
	d2.SetSkillManager(sm)
	d2.skillManager = sm
	d2.pendingSkillScanMCPNames = []string{"mcp-a"}
	roots = d2.skillScanDirs()
	found = false
	for _, r := range roots {
		if strings.HasPrefix(r, bundledA) {
			found = true
		}
	}
	assert.True(t, found, "pending scan names should pass cold-first-chat window")
}

// TestSkillRetrievalProfilePath_RuntimeStateDerivation D-16-4-8 回收：
// sidecar 路径 = workspace.RuntimeStatePath(sid) 改缀（Python :2474-2483 逐字）
func TestSkillRetrievalProfilePath_RuntimeStateDerivation(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	d := NewDeepAdapter()
	path := d.SkillRetrievalProfilePath("sess/1")
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	// 目录 = <config>/runtime_state
	assert.True(t, strings.HasSuffix(dir, filepath.Join("runtime_state")), "dir=%s", dir)
	// stem = 净化后的 sid + ".yaml" 去后缀 → "<sid>.<digest>.skill-retrieval.json"
	assert.Regexp(t, `^sess_1\.[0-9a-f]{16}\.skill-retrieval\.json$`, base)
}

// TestLiveDisabledReload_外部写者后检索现查生效 D-16-4-7 回收集成面
func TestLiveDisabledReload_外部写者后检索现查生效(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	shared := t.TempDir()
	writeTestSkill(t, shared, "alpha")
	sm := skillruntime.NewSkillManager(shared)
	d := NewDeepAdapter()
	d.SetSkillManager(sm)
	assert.Empty(t, d.liveSkillRetrievalDisabledSkills())
	// 外部写者（独立实例，共享同一 skills_state.json 路径
	// = <shared>/skills/skills_state.json）禁用 alpha 并落盘
	writer := skillruntime.NewSkillManager(shared)
	sm2state := map[string]any{
		"skill_configs": map[string]any{
			"alpha": map[string]any{"enabled": false},
		},
	}
	stateData, _ := json.Marshal(sm2state)
	require.NoError(t, os.MkdirAll(filepath.Join(shared, "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(shared, "skills", "skills_state.json"), stateData, 0o644))
	_ = writer
	// live provider 内部 reload → 现查看到
	assert.Equal(t, []string{"alpha"}, d.liveSkillRetrievalDisabledSkills())
}
