//go:build integration

package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	skilltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/skills"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// itAgent 集成测试用 agent：只覆盖 SkillUseRail.Init 与附件注入用到的方法，
// 其余方法由嵌入接口提供（不会被调用）。
type itAgent struct {
	agentinterfaces.BaseAgent
	// builder 系统提示词构建器
	builder saprompt.SystemPromptBuilderInterface
	// ability 能力管理器
	ability agentinterfaces.AbilityManagerInterface
	// attachMgr 附件管理器
	attachMgr *prompts.Manager
	// id agent 标识（保证 ResourceMgr 键唯一）
	id string
}

// itSession 集成测试用会话门面。
type itSession struct {
	// id 会话 id
	id string
	// stateMap 状态表
	stateMap map[string]any
}

// SkillBaselineSuite 技能会话基线集成套件
type SkillBaselineSuite struct {
	suite.Suite
	// skillsRoot 技能根目录
	skillsRoot string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillBaselineSuite 套件入口
func TestSkillBaselineSuite(t *testing.T) {
	suite.Run(t, new(SkillBaselineSuite))
}

// SetupTest 每个用例独立技能目录与全局注册表
func (s *SkillBaselineSuite) SetupTest() {
	s.skillsRoot = s.T().TempDir()
}

// SystemPromptBuilder 返回系统提示词构建器
func (a *itAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return a.builder }

// AbilityManager 返回能力管理器
func (a *itAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return a.ability }

// Card 返回 agent 身份卡片
func (a *itAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: cschema.BaseCard{ID: a.id}}
}

// PromptAttachments 返回附件管理器（SkillUseRail.Init 通过类型断言取用）
func (a *itAgent) PromptAttachments() *prompts.Manager { return a.attachMgr }

// GetSessionID 返回会话 id
func (sess *itSession) GetSessionID() string { return sess.id }

// UpdateState 合并写入状态
func (sess *itSession) UpdateState(data map[string]any) {
	for k, v := range data {
		sess.stateMap[k] = v
	}
}

// GetState 读取状态
func (sess *itSession) GetState(key state.StateKey) (any, error) {
	return sess.stateMap[key.String()], nil
}

// DumpState 导出状态
func (sess *itSession) DumpState() map[string]any { return sess.stateMap }

// WriteStream 空实现
func (sess *itSession) WriteStream(context.Context, any) error { return nil }

// WriteCustomStream 空实现
func (sess *itSession) WriteCustomStream(context.Context, any) error { return nil }

// GetEnv 返回 nil
func (sess *itSession) GetEnv(string, ...any) any { return nil }

// Interact 空实现
func (sess *itSession) Interact(context.Context, any) error { return nil }

// ClearSession 空实现
func (sess *itSession) ClearSession(context.Context) {}

// TestBaselineLifecycle 基线生命周期：落盘 → 并集视图 → 变更附件 → 清基线重拍
func (s *SkillBaselineSuite) TestBaselineLifecycle() {
	ctx := context.Background()
	writeSkill(s.T(), s.skillsRoot, "alpha")

	rail := skillrails.NewSkillUseRail([]string{s.skillsRoot}, skillrails.WithSkillMode(skillrails.SkillModeAll))
	rail.SetSysOperation(sysop.NewLocalSysOperation(&sysop.SysOperationCard{}))
	agent := &itAgent{
		builder:   saprompt.NewSystemPromptBuilder(),
		ability:   ability.NewAbilityManager(nil),
		attachMgr: prompts.NewManager(),
		id:        "it-skill-baseline-" + filepath.Base(s.skillsRoot),
	}
	require.NoError(s.T(), rail.Init(ctx, agent))

	sess := &itSession{id: "sess-1", stateMap: map[string]any{}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, sess)

	// ① 首次 BeforeInvoke：基线落盘为 {alpha}，skill_tool 经会话视图可读
	require.NoError(s.T(), rail.BeforeInvoke(ctx, cbc))
	assert.Equal(s.T(), []string{"alpha"}, namesOf(rail.GetSkillsForSession(sess)))

	skillTool := skilltools.NewSkillTool(
		rail.SysOperation(),
		func(view sessioninterfaces.SessionFacade) []*skillpkg.Skill { return rail.GetSkillsForSession(view) },
		"cn", agent.id, true,
	)
	res, err := skillTool.Invoke(ctx, map[string]any{"skill_name": "alpha"}, tool.WithToolSession(sess))
	require.NoError(s.T(), err)
	require.Equal(s.T(), true, res["success"], "invoke error: %v", res["error"])
	data, ok := res["data"].(map[string]any)
	require.True(s.T(), ok)
	content, _ := data["content"].(string)
	assert.Contains(s.T(), content, "alpha")
	assert.Contains(s.T(), content, "## Directory layout")

	// ② 会话中途新增 beta：视图 = 基线 ∪ 新增，并产生运行时变更附件
	writeSkill(s.T(), s.skillsRoot, "beta")
	require.NoError(s.T(), rail.BeforeInvoke(ctx, cbc))
	assert.Equal(s.T(), []string{"alpha", "beta"}, namesOf(rail.GetSkillsForSession(sess)))
	att := agent.attachMgr.Find(sess.id, "skills.runtime_changes", "skill_use_rail")
	require.NotNil(s.T(), att, "新增技能必须写入运行时附件")
	assert.Contains(s.T(), att.Content, "beta")

	// ③ 清基线后重拍：移除 alpha 后视图只剩 beta（禁用/卸载即时生效）
	require.NoError(s.T(), os.RemoveAll(filepath.Join(s.skillsRoot, "alpha")))
	require.NoError(s.T(), rail.ClearSessionBaseline(sess))
	require.NoError(s.T(), rail.BeforeInvoke(ctx, cbc))
	assert.Equal(s.T(), []string{"beta"}, namesOf(rail.GetSkillsForSession(sess)))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeSkill 在技能根下写一个最小技能
func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644))
}

// namesOf 提取技能名序列
func namesOf(items []*skillpkg.Skill) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
