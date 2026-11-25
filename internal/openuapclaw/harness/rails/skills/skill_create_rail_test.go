package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillCreateRail_首轮窗口阈值入队 对齐 _should_enqueue_creation_follow_up
// 首次提示分支：窗口有效工具调用 ≥10 → prompt_eligible → follow-up 入队一次。
func TestSkillCreateRail_首轮窗口阈值入队(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, effectiveToolNames(10))
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Len(t, ctl.followUps, 1)
	assert.Contains(t, ctl.followUps[0], "<auto_skill_creation_followup>")
	assert.True(t, rail.followUpSent)
}

// TestSkillCreateRail_同轮不重复提示 对齐 follow_up_sent 短路：
// 同一 invoke 内第二次迭代（轨迹无新增有效量）不再入队。
func TestSkillCreateRail_同轮不重复提示(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, effectiveToolNames(10))
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Len(t, ctl.followUps, 1)
}

// TestSkillCreateRail_复提示快照 对齐 prompted_snapshot 判定：
// before_invoke 重置后新增 4 次有效调用（≥ REPROMPT_TOOL_CALL_THRESHOLD）→ 再次入队；
// 新增量不足（1 次）→ 不入队。
func TestSkillCreateRail_复提示快照(t *testing.T) {
	builder := mustNewTestBuilder("sess-1", "test")
	appendSteps(t, builder, effectiveToolNames(10))
	rail := NewSkillCreateRail(t.TempDir())
	rail.SetBuilder(builder)
	ctl := &fakeController{}
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{ctl: ctl}, nil, nil)

	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Len(t, ctl.followUps, 1)
	require.NoError(t, rail.OnBeforeInvoke(context.Background(), cbc))
	// 新增量 1 次（< 4）：复提示不触发
	appendSteps(t, builder, []string{"read_file"})
	rail.currentTrajectory = nil // 强制重捕获
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Len(t, ctl.followUps, 1)
	// 新增量累计 4 次：复提示触发
	appendSteps(t, builder, []string{"write_file", "bash", "code"})
	rail.currentTrajectory = nil
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Len(t, ctl.followUps, 2)
}

// TestSkillCreateRail_已用SkillTool不再提示 对齐 cover 信号短路
// （skill_create_rail.py:186-192）：窗口内出现 skill_tool → 置 seen 标记并跳过。
func TestSkillCreateRail_已用SkillTool不再提示(t *testing.T) {
	names := append(effectiveToolNames(10), "skill_tool")
	rail, cbc, ctl := newSkillCreateRailForTest(t, names)
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	require.Empty(t, ctl.followUps)
	assert.True(t, rail.skillToolSeenThisInvoke)
}

// TestSkillCreateRail_执行上下文抑制 对齐 _follow_up_blocked_by_context：
// run_kind=background / is_follow_up → 抑制且不入队。
func TestSkillCreateRail_执行上下文抑制(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runKnd agentinterfaces.RunKind
		follow bool
	}{
		{"background", agentinterfaces.RunKind("background"), false},
		{"heartbeat", agentinterfaces.RunKindHeartbeat, false},
		{"cron", agentinterfaces.RunKindCron, false},
		{"followUp", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rail, _, ctl := newSkillCreateRailForTest(t, effectiveToolNames(10))
			inputs := &agentinterfaces.TaskIterationInputs{RunKind: tc.runKnd, IsFollowUp: tc.follow}
			cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{ctl: ctl}, inputs, nil)
			require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
			assert.Empty(t, ctl.followUps)
			assert.False(t, rail.followUpSent)
		})
	}
}

// TestSkillCreateRail_普通回复已提议创建 对齐 _assistant_output_proposes_skill_creation：
// output 含创建动作词 + skill 词 → 跳过入队（但记录 prompted totals 并刷新水位线）。
func TestSkillCreateRail_普通回复已提议创建(t *testing.T) {
	rail, _, ctl := newSkillCreateRailForTest(t, effectiveToolNames(10))
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": "我建议创建一个 skill 来沉淀该流程"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{ctl: ctl}, inputs, nil)
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	assert.Empty(t, ctl.followUps)
	// 复提示快照已记录（skill_create_rail.py:131-133）
	assert.NotEmpty(t, rail.lastPromptedToolTotals)
}

// TestSkillCreateRail_提取助手输出多形态 对齐 _extract_assistant_output
// （skill_create_rail.py:261-277）：dict.content 形态与字符串形态。
func TestSkillCreateRail_提取助手输出多形态(t *testing.T) {
	cases := []map[string]any{
		{"output": "创建 skill"},
		{"message": map[string]any{"content": "创建 skill"}},
	}
	for _, result := range cases {
		inputs := &agentinterfaces.TaskIterationInputs{Result: result}
		cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
		assert.Equal(t, "创建 skill", extractAssistantOutput(cbc))
	}
}

// TestSkillCreateRail_followUp文案 对齐 _build_follow_up_prompt（:240-247）：
// tag 包裹 + 中英分支（新版文案，v1 文案已删除）。
func TestSkillCreateRail_followUp文案(t *testing.T) {
	cn := NewSkillCreateRail(t.TempDir()).buildFollowUpPrompt()
	assert.True(t, strings.HasPrefix(cn, "<auto_skill_creation_followup>\n"))
	assert.True(t, strings.HasSuffix(cn, "\n</auto_skill_creation_followup>"))
	assert.Contains(t, cn, "这是运行时插入的 Skill 创建自检")
	en := NewSkillCreateRail(t.TempDir(), WithSkillCreateLanguage("en")).buildFollowUpPrompt()
	assert.Contains(t, en, "This runtime-inserted Skill creation self-check")
}

// TestSkillCreateRail_BeforeModelCall注入与Uninit移除 对齐 before_model_call/uninit
// （skill_create_rail.py:97-104/86-95）：注入 skill_creation_guidance 段，uninit 移除。
func TestSkillCreateRail_BeforeModelCall注入与Uninit移除(t *testing.T) {
	rail := NewSkillCreateRail(t.TempDir())
	spb := newMockSystemPromptBuilder("cn")
	agent := newMockBaseAgent(spb, nil)
	require.NoError(t, rail.Init(context.Background(), agent))
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	require.NoError(t, rail.BeforeModelCall(context.Background(), cbc))
	assert.True(t, spb.HasSection(sectionsNameForAssert()))
	require.NoError(t, rail.Uninit(agent))
	assert.False(t, spb.HasSection(sectionsNameForAssert()))
	assert.Nil(t, rail.systemPromptBuilder)
}

// TestSkillCreateRail_无Controller时不入队 拿不到 LoopController 时丢弃且不置标记。
func TestSkillCreateRail_无Controller时不入队(t *testing.T) {
	rail, cbc, _ := newSkillCreateRailForTestNoAgent(t)
	require.NoError(t, rail.OnAfterTaskIteration(context.Background(), cbc))
	assert.False(t, rail.followUpSent)
}

// TestSkillCreateRail_Priority 优先级对齐 Python 的 85。
func TestSkillCreateRail_Priority(t *testing.T) {
	assert.Equal(t, 85, NewSkillCreateRail(t.TempDir()).Priority())
}

// TestSkillCreateRail_GetCallbacks含BeforeModelCall 对齐 GetCallbacks 覆盖
// （ProgressiveToolRail 先例）：注册表中含 before_model_call 事件。
func TestSkillCreateRail_GetCallbacks含BeforeModelCall(t *testing.T) {
	rail := NewSkillCreateRail(t.TempDir())
	_, ok := rail.GetCallbacks()[agentinterfaces.CallbackBeforeModelCall]
	assert.True(t, ok)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sectionsNameForAssert 返回引导段名（避免测试文件直接依赖 sections 包别名冲突）。
func sectionsNameForAssert() string { return "skill_creation_guidance" }

// effectiveToolNames 返回 n 次有效任务工具调用名序列（去重 5 种，均为非排除工具）。
func effectiveToolNames(n int) []string {
	base := []string{"read_file", "write_file", "bash", "code", "edit_file"}
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		names = append(names, base[i%len(base)])
	}
	return names
}

// appendSteps 向 builder 追加工具 step。
func appendSteps(t *testing.T, builder *trajectory.TrajectoryBuilder, names []string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, builder.RecordTestStep(trajectory.TestStep{Kind: "tool", ToolName: name}))
	}
}

// newSkillCreateRailForTest 构造带 step 的 rail 与回调上下文。
func newSkillCreateRailForTest(t *testing.T, toolNames []string) (*SkillCreateRail, *agentinterfaces.AgentCallbackContext, *fakeController) {
	t.Helper()
	rail := NewSkillCreateRail(t.TempDir())
	builder := mustNewTestBuilder("sess-1", "test")
	appendSteps(t, builder, toolNames)
	rail.SetBuilder(builder)
	ctl := &fakeController{}
	agent := &fakeLoopAgent{ctl: ctl}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	return rail, cbc, ctl
}

// newSkillCreateRailForTestNoAgent 构造无 LoopController 的回调上下文。
func newSkillCreateRailForTestNoAgent(t *testing.T) (*SkillCreateRail, *agentinterfaces.AgentCallbackContext, *fakeController) {
	t.Helper()
	rail := NewSkillCreateRail(t.TempDir())
	builder := mustNewTestBuilder("sess-1", "test")
	appendSteps(t, builder, effectiveToolNames(10))
	rail.SetBuilder(builder)
	ctl := &fakeController{}
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{ctl: nil}, nil, nil)
	return rail, cbc, ctl
}
