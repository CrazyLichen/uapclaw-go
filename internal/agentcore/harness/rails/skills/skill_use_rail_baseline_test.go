package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	ptools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
)

// ──────────────────────────── 结构体 ────────────────────────────

// skillFakeSession 测试用会话门面（仅实现基线所需能力，其余空实现）
type skillFakeSession struct {
	// id 会话 id
	id string
	// stateMap 状态表
	stateMap map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newSkillFakeSession 创建测试会话
func newSkillFakeSession(id string) *skillFakeSession {
	return &skillFakeSession{id: id, stateMap: map[string]any{}}
}

// GetSessionID 返回会话 id
func (f *skillFakeSession) GetSessionID() string { return f.id }

// UpdateState 合并写入状态
func (f *skillFakeSession) UpdateState(data map[string]any) {
	for k, v := range data {
		f.stateMap[k] = v
	}
}

// GetState 读取状态
func (f *skillFakeSession) GetState(key state.StateKey) (any, error) {
	return f.stateMap[key.String()], nil
}

// DumpState 导出状态
func (f *skillFakeSession) DumpState() map[string]any { return f.stateMap }

// WriteStream 空实现
func (f *skillFakeSession) WriteStream(context.Context, any) error { return nil }

// WriteCustomStream 空实现
func (f *skillFakeSession) WriteCustomStream(context.Context, any) error { return nil }

// GetEnv 返回 nil
func (f *skillFakeSession) GetEnv(string, ...any) any { return nil }

// Interact 空实现
func (f *skillFakeSession) Interact(context.Context, any) error { return nil }

// ClearSession 空实现
func (f *skillFakeSession) ClearSession(context.Context) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRailWithSkills 构造带固定技能集合的 rail
func newRailWithSkills(skills ...*skillpkg.Skill) *SkillUseRail {
	r := NewSkillUseRail([]string{"/tmp/skills"})
	r.skills = skills
	return r
}

// testSkill 构造测试技能
func testSkill(name string) *skillpkg.Skill {
	return &skillpkg.Skill{Name: name, Description: name + " desc", Directory: "/tmp/skills/" + name}
}

// TestEnsureSessionBaseline 首次落盘且二次不覆盖
func TestEnsureSessionBaseline(t *testing.T) {
	t.Run("首次落盘且二次不覆盖", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")

		r.ensureSessionBaseline(sess)

		raw, err := sess.GetState(state.StringKey(skillSessionStateKey))
		require.NoError(t, err)
		require.NotNil(t, raw, "首次调用必须落盘基线")

		// 第二次：rail 侧技能集合变化，基线保持不变（快照语义）
		r.skills = []*skillpkg.Skill{testSkill("beta")}
		r.ensureSessionBaseline(sess)

		baseline := r.loadSessionBaseline(sess)
		require.Len(t, baseline, 1)
		assert.Equal(t, "alpha", baseline[0].Name)
	})

	t.Run("nil会话不panic", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		assert.NotPanics(t, func() { r.ensureSessionBaseline(nil) })
	})
}

// TestClearSessionBaseline 清基线行为
func TestClearSessionBaseline(t *testing.T) {
	t.Run("nil会话不panic", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		assert.NotPanics(t, func() { r.ClearSessionBaseline(nil) })
	})

	t.Run("清除后视图回落到当前集合", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		r.ensureSessionBaseline(sess)

		r.ClearSessionBaseline(sess)
		assert.Equal(t, []*skillpkg.Skill{r.skills[0]}, r.GetSkillsForSession(sess))
	})
}

// TestGetSkillsForSession 会话视图合并
func TestGetSkillsForSession(t *testing.T) {
	t.Run("基线并集新发现", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		r.ensureSessionBaseline(sess)

		// 会话中途新增 beta（应作为 runtime addition 可见）
		r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}
		names := []string{}
		for _, s := range r.GetSkillsForSession(sess) {
			names = append(names, s.Name)
		}
		assert.Equal(t, []string{"alpha", "beta"}, names)
	})

	t.Run("无state时返回当前集合", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		assert.Equal(t, []*skillpkg.Skill{r.skills[0]}, r.GetSkillsForSession(sess))
	})
}

// TestLoadSessionState schema 版本校验
func TestLoadSessionState(t *testing.T) {
	t.Run("版本不符则忽略", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		sess.stateMap[skillSessionStateKey] = map[string]any{
			"schema_version":  999,
			"baseline_skills": []any{map[string]any{"name": "alpha", "directory": "/tmp/skills/alpha"}},
		}

		_, ok := r.loadSessionState(sess)
		assert.False(t, ok, "版本不符必须整份忽略")
		assert.Empty(t, r.loadSessionBaseline(sess))
	})

	t.Run("float64版本兼容落盘回读", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		sess.stateMap[skillSessionStateKey] = map[string]any{
			"schema_version":  float64(1),
			"baseline_skills": []any{map[string]any{"name": "alpha", "directory": "/tmp/skills/alpha"}},
		}

		_, ok := r.loadSessionState(sess)
		assert.True(t, ok)
	})
}

// TestLoadSessionBaseline 非法条目跳过
func TestLoadSessionBaseline(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	sess := newSkillFakeSession("s1")
	sess.stateMap[skillSessionStateKey] = map[string]any{
		"schema_version": 1,
		"baseline_skills": []any{
			map[string]any{"name": "alpha", "description": "d", "directory": "/tmp/skills/alpha"},
			map[string]any{"name": "", "directory": "/tmp/skills/x"}, // 缺 name
			map[string]any{"name": "gamma"},                          // 缺 directory
			"not-a-map",                                              // 类型非法
		},
	}

	baseline := r.loadSessionBaseline(sess)
	require.Len(t, baseline, 1)
	assert.Equal(t, "alpha", baseline[0].Name)
}

// 编译期断言：假会话满足门面
var _ sessioninterfaces.SessionFacade = (*skillFakeSession)(nil)

// newCBC 构造带会话的回调上下文（agent 为 nil，附件管理器由 attachmentManagerFor 注入）。
func newCBC(t *testing.T, sess sessioninterfaces.SessionFacade) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	return agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
}

// TestSyncSkillPromptAndAttachment 提示词节取自会话基线
func TestSyncSkillPromptAndAttachment(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	r.skillsDir = []string{t.TempDir()} // 空目录 → 签名恒为空，跳过增量刷新
	r.skillMode = SkillModeAll
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

	sess := newSkillFakeSession("s1")
	r.ensureSessionBaseline(sess)

	// 基线落盘后新增 beta：提示词节仍只应包含 alpha
	r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}
	require.NoError(t, r.syncSkillPromptAndAttachment(context.Background(), newCBC(t, sess)))

	section := r.systemPromptBuilder.(*mockSystemPromptBuilder).GetSection(sections.SectionSkills)
	require.NotNil(t, section)
	assert.Contains(t, section.Content["cn"], "alpha")
	assert.NotContains(t, section.Content["cn"], "beta", "系统提示词必须取自基线")
}

// TestUpdateRuntimeSkillAttachment 运行时变更附件
func TestUpdateRuntimeSkillAttachment(t *testing.T) {
	t.Run("新增技能写入附件", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		r.skillMode = SkillModeAll
		r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

		sess := newSkillFakeSession("s1")
		r.ensureSessionBaseline(sess)
		r.skills = []*skillpkg.Skill{testSkill("alpha"), testSkill("beta")}

		mgr := prompts.NewManager()
		r.attachmentManager = mgr

		require.NoError(t, r.updateRuntimeSkillAttachment(newCBC(t, sess), r.loadSessionBaseline(sess)))

		att := mgr.Find("s1", runtimeSkillAttachmentSection, runtimeSkillAttachmentSource)
		require.NotNil(t, att)
		assert.Contains(t, att.Content, "beta")
		assert.NotContains(t, att.Content, "alpha", "基线内技能不进变更附件")
	})

	t.Run("无变化清除附件", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		r.skillMode = SkillModeAll
		r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

		sess := newSkillFakeSession("s1")
		r.ensureSessionBaseline(sess)

		mgr := prompts.NewManager()
		r.attachmentManager = mgr
		_, err := mgr.AddSection("s1", runtimeSkillAttachmentSection, "stale", prompts.AttachmentSkill, runtimeSkillAttachmentSource)
		require.NoError(t, err)

		require.NoError(t, r.updateRuntimeSkillAttachment(newCBC(t, sess), r.loadSessionBaseline(sess)))
		assert.Nil(t, mgr.Find("s1", runtimeSkillAttachmentSection, runtimeSkillAttachmentSource))
	})

	t.Run("无会话时不panic", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		mgr := prompts.NewManager()
		r.attachmentManager = mgr
		assert.NoError(t, r.updateRuntimeSkillAttachment(nil, nil))
	})
}

// TestBuildRuntimeSkillChangeContent 附件文案
func TestBuildRuntimeSkillChangeContent(t *testing.T) {
	t.Run("无变化返回空串", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		r.systemPromptBuilder = newMockSystemPromptBuilder("en")
		content := r.buildRuntimeSkillChangeContent(nil, nil, []*skillpkg.Skill{testSkill("alpha")})
		assert.Equal(t, "", content)
	})

	t.Run("英文包含移除段", func(t *testing.T) {
		r := newRailWithSkills()
		r.systemPromptBuilder = newMockSystemPromptBuilder("en")
		content := r.buildRuntimeSkillChangeContent(
			[]*skillpkg.Skill{testSkill("beta")},
			[]*skillpkg.Skill{testSkill("alpha")},
			[]*skillpkg.Skill{testSkill("alpha")},
		)
		assert.Contains(t, content, "Skill environment status update")
		assert.Contains(t, content, "Newly available skills:")
		assert.Contains(t, content, "Unavailable skills (removed from the environment):")
		assert.Contains(t, content, "alpha")
	})

	t.Run("中文包含演进经验段", func(t *testing.T) {
		r := newRailWithSkills()
		r.systemPromptBuilder = newMockSystemPromptBuilder("cn")
		r.evolutionTexts["alpha"] = "  经验正文  "
		content := r.buildRuntimeSkillChangeContent(nil, nil, []*skillpkg.Skill{testSkill("alpha")})
		assert.Contains(t, content, "Skill 环境状态更新")
		assert.Contains(t, content, "Skill 演进经验参考：")
		assert.Contains(t, content, "经验正文")
	})
}

// TestSkillUseRailPriority 与上游一致（95，晚于文件系统 rail）
// Python: SkillUseRail.priority = 95 (skill_use_rail.py:57-61)
func TestSkillUseRailPriority(t *testing.T) {
	r := NewSkillUseRail([]string{t.TempDir()})
	assert.Equal(t, 95, r.Priority())
}

// TestInit_已存在的工具不再覆盖注册 他人拥有的能力不抢注册
// Python: skill_use_rail.py:377-386
func TestInit_已存在的工具不再覆盖注册(t *testing.T) {
	ownerCard, err := ptools.BuildToolCard("read_file", "ReadFileTool", "cn", nil, "agent")
	require.NoError(t, err)

	am := newMockAbilityManager()
	am.abilities["read_file"] = ownerCard

	agent := newMockBaseAgent(newMockSystemPromptBuilder("cn"), am)
	r := NewSkillUseRail([]string{t.TempDir()}, WithIncludeTools(true))
	require.NoError(t, r.Init(context.Background(), agent))

	_, owned := r.ownedToolNames["read_file"]
	assert.False(t, owned, "他人拥有的工具不得被本 rail 认领")
	assert.Same(t, ownerCard, am.abilities["read_file"], "原持有者实例不应被替换")
}

// TestSchemaVersion 版本号归一化
func TestSchemaVersion(t *testing.T) {
	assert.Equal(t, 1, schemaVersion(1))
	assert.Equal(t, 1, schemaVersion(int64(1)))
	assert.Equal(t, 1, schemaVersion(float64(1.0)))
	assert.Equal(t, -1, schemaVersion("1"))
	assert.Equal(t, -1, schemaVersion(nil))
}

// TestWithSkillBudget 预算选项
func TestWithSkillBudget(t *testing.T) {
	r := NewSkillUseRail([]string{t.TempDir()}, WithSkillBudget(5, 1000))
	assert.Equal(t, 5, r.maxSkills)
	assert.Equal(t, 1000, r.maxTotalChars)
}

// TestLanguage 语言回退
func TestLanguage(t *testing.T) {
	r := newRailWithSkills()
	assert.Equal(t, "cn", r.language())
	r.systemPromptBuilder = newMockSystemPromptBuilder("en")
	assert.Equal(t, "en", r.language())
}

// TestGetCallbacks 回调映射包含两个 hook
func TestGetCallbacks(t *testing.T) {
	r := newRailWithSkills()
	cbs := r.GetCallbacks()
	assert.Contains(t, cbs, agentinterfaces.CallbackBeforeInvoke)
	assert.Contains(t, cbs, agentinterfaces.CallbackBeforeModelCall)
}

// TestSaveSessionBaseline_跳过nil技能 空技能不落盘
func TestSaveSessionBaseline_跳过nil技能(t *testing.T) {
	r := newRailWithSkills()
	sess := newSkillFakeSession("s1")
	r.saveSessionBaseline(sess, []*skillpkg.Skill{nil})
	_, ok := r.loadSessionState(sess)
	assert.True(t, ok, "即使技能为空也应写入合法 state")
	assert.Empty(t, r.loadSessionBaseline(sess))
}

// TestBeforeModelCall_NilBuilder 未注入 builder 时安全返回
func TestBeforeModelCall_NilBuilder(t *testing.T) {
	r := newRailWithSkills(testSkill("alpha"))
	assert.NoError(t, r.BeforeModelCall(context.Background(), nil))
}

// TestBuildRuntimeSkillChangeContent_仅移除 只有移除时也应产出内容
func TestBuildRuntimeSkillChangeContent_仅移除(t *testing.T) {
	r := newRailWithSkills()
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")
	content := r.buildRuntimeSkillChangeContent(nil, []*skillpkg.Skill{testSkill("alpha")}, nil)
	assert.Contains(t, content, "已移除、当前不可用的 Skill：")
	assert.Contains(t, content, "- alpha")
}

// TestBeforeModelCall_提取查询并注入 每次模型调用都刷新基线视图
func TestBeforeModelCall_提取查询并注入(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "alpha", "SKILL.md"), []byte("# alpha"), 0o644))

	r := NewSkillUseRail([]string{root}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	r.systemPromptBuilder = newMockSystemPromptBuilder("cn")

	sess := newSkillFakeSession("s1")
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ModelCallInputs{Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("帮我处理 pdf")}}, sess)

	require.NoError(t, r.BeforeModelCall(context.Background(), cbc))
	assert.Equal(t, "帮我处理 pdf", r.currentQuery)
	section := r.systemPromptBuilder.(*mockSystemPromptBuilder).GetSection(sections.SectionSkills)
	require.NotNil(t, section)
	assert.Contains(t, section.Content["cn"], "alpha")
}

// TestSkillsSignatureEquals 签名比较
func TestSkillsSignatureEquals(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "alpha", "SKILL.md"), []byte("# alpha"), 0o644))

	r := NewSkillUseRail([]string{root}, WithSkillMode(SkillModeAll), WithIncludeTools(false))
	assert.False(t, r.skillsSignatureEquals(), "首次无签名应判为变化")

	r.refreshSkillPrompt(context.Background())
	assert.True(t, r.skillsSignatureEquals())

	require.NoError(t, os.MkdirAll(filepath.Join(root, "beta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "beta", "SKILL.md"), []byte("# beta"), 0o644))
	assert.False(t, r.skillsSignatureEquals(), "新增技能目录应判为变化")
}

// TeardownTools 测试替身空实现（对齐 AbilityManagerInterface 扩张）。
func (f *mockAbilityManager) TeardownTools() {}
