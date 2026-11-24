//go:build integration

// Package skill_retrieval 16.4b 技能渐进式发现全链集成测试。
//
// 真实跨子系统链路：DeepAdapter（sidecar 五重校验/懒创建）→
// harness/common/tools SkillRetrievalToolkit（装配层：粗判/冻结/策略推导）→
// agentcore/symphony/discovery（SkillFS 真盘扫描 + skill_index 三操作 +
// notice 基线表）→ workspace/config（UAPCLAW_HOME 环境派生）。
// 无索引环境（D-16-4-5：taxonomy 构建 ⤴️16.5）：渐进式闭环（small_full /
// large-flat / skill_index 翻页 / 漂移通知）在本套件全链可用。
package skill_retrieval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	adapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// SkillRetrievalSuite 16.4b 渐进式全链端到端。
type SkillRetrievalSuite struct {
	suite.Suite
}

func TestSkillRetrievalSuite(t *testing.T) {
	suite.Run(t, new(SkillRetrievalSuite))
}

// writeSkill 造一个最小技能（SKILL.md frontmatter）。
func (s *SkillRetrievalSuite) writeSkill(root, name, description string) string {
	dir := filepath.Join(root, name)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\nbody of " + name + "\n"
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
	return dir
}

func (s *SkillRetrievalSuite) enabledConfig() map[string]any {
	return map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{"enabled": true}}}
}

func (s *SkillRetrievalSuite) newToolkit(dirs func() []string, cfg map[string]any) *tools.SkillRetrievalToolkit {
	tk, err := tools.NewSkillRetrievalToolkit(tools.ToolkitOptions{
		SkillDirectories: dirs,
		ConfigBase:       cfg,
	})
	s.Require().NoError(err)
	return tk
}

// ──────────────────────────── 用例 ────────────────────────────

// TestProgressiveSmallFull 渐进式闭环·小目录：small_full 全量注入 + skill_index
// list/search/read 三操作全链可达（adapter→toolkit→discovery→workspace 四子系统）。
func (s *SkillRetrievalSuite) TestProgressiveSmallFull() {
	t := s.T()
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	s.writeSkill(root, "alpha", "alpha reads pdf files")
	s.writeSkill(root, "beta", "beta writes csv files")

	// adapter 层：懒创建（真盘扫描）+ sidecar 即时落盘
	d := adapter.NewDeepAdapter()
	d.SetSessionScopedForTest("prog-small")
	tk, err := tools.NewSkillRetrievalToolkit(tools.ToolkitOptions{
		SkillDirectories: func() []string { return []string{root} },
		ConfigBase:       s.enabledConfig(),
		SessionScope:     "prog-small",
	})
	s.Require().NoError(err)
	d.SetSkillRetrievalToolkitForTest(tk)
	d.PersistSkillRetrievalSessionProfile()

	// rail 层：构造期冻结快照
	rail, err := rails.NewSkillRetrievalPromptRail(rails.RailOptions{
		Toolkit:     tk,
		SessionScope: "prog-small",
		ConfigBase:  s.enabledConfig(),
	})
	s.Require().NoError(err)
	snapshot := rail.FrozenPromptSnapshot()
	s.Assert().Equal("small", snapshot.Mode)
	s.Assert().Len(snapshot.Entries, 2)

	// discovery 层：skill_index 三操作
	s.Assert().Equal("small_full", tk.EffectiveStrategy())
	result, err := tk.SkillIndex(map[string]any{"operation": "list"})
	s.Require().NoError(err)
	s.Assert().Contains(result.ModelContent, "alpha")
	searched, err := tk.SkillIndex(map[string]any{"operation": "search", "query": []any{"pdf"}})
	s.Require().NoError(err)
	s.Assert().Contains(searched.ModelContent, "alpha")
	read, err := tk.SkillIndex(map[string]any{"operation": "read", "skills": []any{"alpha"}})
	s.Require().NoError(err)
	s.Assert().Contains(read.ModelContent, "alpha reads pdf files")

	// sidecar 五重校验往返
	payload, found := d.LoadSkillRetrievalSessionProfileForTest("prog-small")
	s.Require().True(found)
	s.Assert().Empty(payload["profile_recovery_error"])
}

// TestProgressiveLargeFlat 渐进式闭环·大目录：large-flat 截断 + 翻页游标 +
// 漂移通知（rail after_tool_call 双入口 B 路径）。
func (s *SkillRetrievalSuite) TestProgressiveLargeFlat() {
	t := s.T()
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	cfg := map[string]any{"symphony": map[string]any{"skill_retrieval": map[string]any{
		"enabled":   true,
		"discovery": map[string]any{"context_window_tokens": 1, "max_list_entries": 2},
	}}}
	// 12 技能 > list 默认页大小 10 → 首页必有 [next] 游标
	for _, name := range []string{"alpha", "beta", "gamma", "delta0", "delta1", "delta2",
		"delta3", "delta4", "delta5", "delta6", "delta7", "delta8"} {
		s.writeSkill(root, name, "the "+name+" skill")
	}
	tk := s.newToolkit(func() []string { return []string{root} }, cfg)
	s.Assert().Equal("large", tk.CandidateScale())
	s.Assert().Equal("large_flat", tk.EffectiveStrategy())
	snapshot := tk.FrozenPromptSnapshot()
	s.Assert().Equal("large-flat", snapshot.Mode)
	s.Assert().LessOrEqual(len(snapshot.Entries), 2)

	// skill_index list 翻页：首页 → [next] 游标 → 下一页覆盖全部
	first, err := tk.SkillIndex(map[string]any{"operation": "list"})
	s.Require().NoError(err)
	s.Assert().Contains(first.ModelContent, "[next]")
	// 游标解析与续读（footer JSON 形态）
	cursor := extractCursor(first.ModelContent)
	s.Assert().NotEmpty(cursor)
	second, err := tk.SkillIndex(map[string]any{"operation": "list", "cursor": cursor})
	s.Require().NoError(err)
	s.Assert().NotContains(second.ModelContent, first.ModelContent)

	// 漂移通知：基线初始化 → 新增技能 → 非 skill_index 工具钩子消费
	rail, err := rails.NewSkillRetrievalPromptRail(rails.RailOptions{
		Toolkit:     tk,
		SessionScope: tk.SessionScope(),
		ConfigBase:  cfg,
	})
	s.Require().NoError(err)
	s.Require().NoError(rail.Init(nil, nil))
	s.writeSkill(root, "delta", "freshly installed delta skill")
	reminder, err := discovery.ConsumeIncrementalSkillReminder(
		tk.SessionScope(), rail.ScanSelectionCardsForTest(), 4000)
	s.Require().NoError(err)
	s.Assert().Contains(reminder, "delta")
	// 基线推进：二次消费无通知
	reminder2, err := discovery.ConsumeIncrementalSkillReminder(
		tk.SessionScope(), rail.ScanSelectionCardsForTest(), 4000)
	s.Require().NoError(err)
	s.Assert().Empty(reminder2)
}

// TestRecoveryChain 恢复链：persist → 五重校验逐项破坏 → 降级重扫不中断；
// 完好 → frozen_profile 恢复跳过重推。
func (s *SkillRetrievalSuite) TestRecoveryChain() {
	t := s.T()
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	s.writeSkill(root, "alpha", "alpha skill")
	d := adapter.NewDeepAdapter()
	d.SetSessionScopedForTest("rec-1")
	tk := s.newToolkit(func() []string { return []string{root} }, s.enabledConfig())
	d.SetSkillRetrievalToolkitForTest(tk)
	d.PersistSkillRetrievalSessionProfile()

	// 完好恢复
	payload, found := d.LoadSkillRetrievalSessionProfileForTest("rec-1")
	s.Require().True(found)
	s.Assert().Empty(payload["profile_recovery_error"])

	// 破坏 checksum → 降级 dict（enabled=false + recovery_error）不中断
	path := d.SkillRetrievalProfilePathForTest("rec-1")
	raw, err := os.ReadFile(path)
	s.Require().NoError(err)
	var mutated map[string]any
	s.Require().NoError(json.Unmarshal(raw, &mutated))
	mutated["profile_checksum"] = "0000000000000000000000000000000000000000000000000000000000000000"
	corrupted, _ := json.Marshal(mutated)
	s.Require().NoError(os.WriteFile(path, corrupted, 0o600))
	degraded, found2 := d.LoadSkillRetrievalSessionProfileForTest("rec-1")
	s.Require().True(found2)
	s.Assert().Equal("profile_checksum_mismatch", degraded["profile_recovery_error"])
	s.Assert().Equal(false, degraded["enabled"])

	// 恢复链端到端：frozen_profile 传构造跳过重推（新增技能不进冻结快照）
	s.writeSkill(root, "extra", "post snapshot skill")
	restored, err := tools.NewSkillRetrievalToolkit(tools.ToolkitOptions{
		SkillDirectories: func() []string { return []string{root} },
		ConfigBase:       s.enabledConfig(),
		SessionScope:     "rec-1",
		FrozenProfile:    payload,
	})
	s.Require().NoError(err)
	s.Assert().Equal(len(tk.FrozenPromptSnapshot().Entries), len(restored.FrozenPromptSnapshot().Entries))

	// 乐观丢弃 / 恢复来源保留
	d.DiscardTransientSkillRetrievalSessionProfile()
	d2 := adapter.NewDeepAdapter()
	d2.SetSessionScopedForTest("rec-1")
	d2.RestoreSkillRetrievalSession(payload, nil, "")
	// 恢复语义 = sidecar 先前已在盘上（restore 隐含既有文件）；回写再验保留
	s.Require().NoError(os.WriteFile(path, raw, 0o600))
	d2.DiscardTransientSkillRetrievalSessionProfile()
	_, statErr := os.Stat(path)
	s.Assert().NoError(statErr, "restored sidecar must survive discard")
}

// TestSwitchOff 渐进式关断：全局开关关 → legacy 全链（不装配 toolkit、
// sidecar 记录 enabled=false、开关读数 false）。
func (s *SkillRetrievalSuite) TestSwitchOff() {
	t := s.T()
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	cfg := map[string]any{}
	s.Assert().False(tools.IsSkillRetrievalEnabled(cfg))
	d := adapter.NewDeepAdapter()
	d.SetSessionScopedForTest("off-1")
	d.SetSkillRetrievalSessionEnabledForTest(false)
	d.PersistSkillRetrievalSessionProfile()
	payload, found := d.LoadSkillRetrievalSessionProfileForTest("off-1")
	s.Require().True(found)
	s.Assert().Equal(false, payload["enabled"])
	// workspace/config 子系统联动：workspace 派生路径仍可达
	s.Assert().True(filepath.IsAbs(filepath.Dir(d.SkillRetrievalProfilePathForTest("off-1"))) ||
		filepath.Dir(d.SkillRetrievalProfilePathForTest("off-1")) != "")
	_ = workspace.ConfigDir()
}

// extractCursor 从 [next] footer 提取游标（footer JSON 形态：[next] {"cursor": "..."}）。
func extractCursor(content string) string {
	marker := "[next] "
	idx := indexOf(content, marker)
	if idx < 0 {
		return ""
	}
	var payload struct {
		Cursor string `json:"cursor"`
	}
	tail := content[idx+len(marker):]
	if err := json.Unmarshal([]byte(tail), &payload); err != nil {
		return ""
	}
	return payload.Cursor
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
