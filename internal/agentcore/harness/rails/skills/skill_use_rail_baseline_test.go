package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
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

		require.NoError(t, r.ensureSessionBaseline(sess))

		raw, err := sess.GetState(state.StringKey(skillSessionStateKey))
		require.NoError(t, err)
		require.NotNil(t, raw, "首次调用必须落盘基线")

		// 第二次：rail 侧技能集合变化，基线保持不变（快照语义）
		r.skills = []*skillpkg.Skill{testSkill("beta")}
		require.NoError(t, r.ensureSessionBaseline(sess))

		baseline := r.loadSessionBaseline(sess)
		require.Len(t, baseline, 1)
		assert.Equal(t, "alpha", baseline[0].Name)
	})

	t.Run("nil会话不panic", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		assert.NoError(t, r.ensureSessionBaseline(nil))
	})
}

// TestClearSessionBaseline 清基线行为
func TestClearSessionBaseline(t *testing.T) {
	t.Run("nil会话不panic", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		assert.NoError(t, r.ClearSessionBaseline(nil))
	})

	t.Run("清除后视图回落到当前集合", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		require.NoError(t, r.ensureSessionBaseline(sess))

		require.NoError(t, r.ClearSessionBaseline(sess))
		assert.Equal(t, []*skillpkg.Skill{r.skills[0]}, r.GetSkillsForSession(sess))
	})
}

// TestGetSkillsForSession 会话视图合并
func TestGetSkillsForSession(t *testing.T) {
	t.Run("基线并集新发现", func(t *testing.T) {
		r := newRailWithSkills(testSkill("alpha"))
		sess := newSkillFakeSession("s1")
		require.NoError(t, r.ensureSessionBaseline(sess))

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
