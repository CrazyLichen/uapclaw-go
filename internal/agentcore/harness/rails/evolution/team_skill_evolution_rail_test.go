package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestTeamSkillEvolutionRail_Priority(t *testing.T) {
	// Priority 必须返回 80，对齐 Python: TeamSkillEvolutionRail.priority = 80
	r := &TeamSkillEvolutionRail{}
	assert.Equal(t, 80, r.Priority())
}

func TestTeamSkillEvolutionRail_AllowEvolutionTrigger(t *testing.T) {
	// autoScan=true + passiveEvolutionPending=true → 允许触发
	r := &TeamSkillEvolutionRail{
		autoScan:                true,
		passiveEvolutionPending: true,
		EvolutionRail:           &EvolutionRail{},
	}
	assert.True(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, nil))

	// autoScan=false → 始终不允许
	r.autoScan = false
	assert.False(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, nil))

	// autoScan=true + passiveEvolutionPending=false + hostCompletionPending 匹配
	r.autoScan = true
	r.passiveEvolutionPending = false
	sessionID := "sess-1"
	r.hostCompletionPendingSessionID = &sessionID
	// 无 Builder 时 currentBuilderSessionID 返回空串 → 不匹配
	assert.False(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
}

func TestTeamSkillEvolutionRail_AutoScan(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true}
	assert.True(t, r.AutoScan())

	r.SetAutoScan(false)
	assert.False(t, r.AutoScan())
}

func TestTeamSkillEvolutionRail_AutoSave(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoSave: true}
	assert.True(t, r.AutoSave())

	r.SetAutoSave(false)
	assert.False(t, r.AutoSave())
}

func TestTeamSkillEvolutionRail_ClearProcessedSignals(t *testing.T) {
	r := &TeamSkillEvolutionRail{
		processedSignalKeys: map[[4]string]bool{
			{"a", "b", "c", "d"}: true,
		},
	}
	assert.Len(t, r.ProcessedSignalKeys(), 1)

	r.ClearProcessedSignals()
	assert.Len(t, r.ProcessedSignalKeys(), 0)
}

func TestTeamSkillEvolutionRail_OnBeforeInvoke(t *testing.T) {
	// OnBeforeInvoke 重置 passiveEvolutionPending
	r := &TeamSkillEvolutionRail{passiveEvolutionPending: true}
	err := r.OnBeforeInvoke(context.TODO(), nil)
	assert.NoError(t, err)
	assert.False(t, r.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_OnAfterModelCall(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	err := r.OnAfterModelCall(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_OnAfterInvoke(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	err := r.OnAfterInvoke(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_OnAfterTaskIteration(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	err := r.OnAfterTaskIteration(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_OnAfterEvolutionTriggered(t *testing.T) {
	// 匹配 sessionID → 清除 hostCompletionPendingSessionID
	sessionID := "sess-1"
	r := &TeamSkillEvolutionRail{hostCompletionPendingSessionID: &sessionID}
	traj := &trajectory.Trajectory{SessionID: "sess-1"}
	err := r.OnAfterEvolutionTriggered(context.TODO(), traj, nil)
	assert.NoError(t, err)
	assert.Nil(t, r.hostCompletionPendingSessionID)

	// 不匹配 → 保留
	sessionID2 := "sess-2"
	r.hostCompletionPendingSessionID = &sessionID2
	err = r.OnAfterEvolutionTriggered(context.TODO(), traj, nil)
	assert.NoError(t, err)
	assert.NotNil(t, r.hostCompletionPendingSessionID)
}

func TestTeamSkillEvolutionRail_GetEvolutionTotalTimeoutSecs(t *testing.T) {
	r := &TeamSkillEvolutionRail{evolutionTotalTimeoutSec: 720.0}
	assert.Equal(t, 720.0, r.GetEvolutionTotalTimeoutSecs())
}

func TestTeamSkillEvolutionRail_EvolutionConfig(t *testing.T) {
	r := &TeamSkillEvolutionRail{
		evalInterval:             5,
		evolutionTotalTimeoutSec: 720.0,
	}
	config := r.EvolutionConfig()
	assert.Equal(t, 5, config["eval_interval"])
	assert.Equal(t, 720.0, config["evolution_total_timeout_secs"])
	// 未初始化 EvolutionRail 时，max_concurrent_evolution 为 0
	assert.Equal(t, 0, config["max_concurrent_evolution"])

	// 手动构造带 EvolutionRail 的场景验证 cap
	r2 := &TeamSkillEvolutionRail{
		evalInterval:             5,
		evolutionTotalTimeoutSec: 720.0,
	}
	r2.EvolutionRail = &EvolutionRail{evolutionSem: make(chan struct{}, 3)}
	config2 := r2.EvolutionConfig()
	assert.Equal(t, 3, config2["max_concurrent_evolution"])
}

func TestTeamSkillEvolutionRail_MarkPassiveEvolutionPending(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	assert.False(t, r.passiveEvolutionPending)

	r.markPassiveEvolutionPending()
	assert.True(t, r.passiveEvolutionPending)

	// 幂等：再次调用不应改变
	r.markPassiveEvolutionPending()
	assert.True(t, r.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_ConsumePresentedEntries(t *testing.T) {
	// 无 tracker → 返回 nil
	r := &TeamSkillEvolutionRail{}
	assert.Nil(t, r.consumePresentedEntries("sess-1"))
}

func TestTeamSkillEvolutionRail_EvaluatePresentedEntries(t *testing.T) {
	// 无 tracker → 不 panic
	r := &TeamSkillEvolutionRail{}
	r.evaluatePresentedEntries(context.TODO(), nil)

	r.evaluatePresentedEntries(context.TODO(), []experience.PresentedRecordEntry{})
}

func TestTeamSkillEvolutionRail_BuildTrajectory(t *testing.T) {
	// 无 Builder（基类已初始化但 builder 为 nil）→ 返回 nil
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	assert.Nil(t, r.buildTrajectory())
}

func TestTeamSkillEvolutionRail_CurrentBuilderSessionID(t *testing.T) {
	// 无 Builder → 返回空串
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	assert.Equal(t, "", r.currentBuilderSessionID())
}

// ─── 模块级函数测试 ───

func TestIsCompletedTeamTaskView(t *testing.T) {
	// nil 输入
	assert.False(t, isCompletedTeamTaskView(nil))

	// 包含 "completed" 且无非终态
	assert.True(t, isCompletedTeamTaskView("status: completed"))

	// 不包含 "completed"
	assert.False(t, isCompletedTeamTaskView("status: in_progress"))

	// 包含 "completed" 但也包含非终态
	assert.False(t, isCompletedTeamTaskView("completed and pending"))

	// 包含 "completed" 且包含 "blocked"
	assert.False(t, isCompletedTeamTaskView("completed, but blocked"))

	// 纯 completed
	assert.True(t, isCompletedTeamTaskView(map[string]any{"status": "completed"}))
}

func TestInferTeamSkillFromTrajectory(t *testing.T) {
	knownSkills := []string{"team-planner", "team-coder"}

	// nil 轨迹
	assert.Equal(t, "", inferTeamSkillFromTrajectory(nil, knownSkills))

	// 空轨迹
	assert.Equal(t, "", inferTeamSkillFromTrajectory(&trajectory.Trajectory{}, knownSkills))

	// 空已知技能
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{Kind: trajectory.StepKindTool, Detail: &trajectory.ToolCallDetail{ToolName: "skill_tool"}},
		},
	}
	assert.Equal(t, "", inferTeamSkillFromTrajectory(traj, []string{}))

	// 有 skill_tool 调用但 args 不包含技能名
	assert.Equal(t, "", inferTeamSkillFromTrajectory(traj, knownSkills))

	// 有 skill_tool 调用且 args 包含技能名
	trajWithSkill := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind:   trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{ToolName: "skill_tool", CallArgs: map[string]any{"skill_name": "team-planner"}},
			},
		},
	}
	result := inferTeamSkillFromTrajectory(trajWithSkill, knownSkills)
	assert.Equal(t, "team-planner", result)
}

func TestTeamSkillKinds(t *testing.T) {
	// 验证团队技能类型集合
	assert.True(t, teamSkillKinds["team-skill"])
	assert.True(t, teamSkillKinds["swarm-skill"])
	assert.False(t, teamSkillKinds["skill"])
}

func TestTeamTaskNonTerminalStates(t *testing.T) {
	// 验证团队任务非终态集合
	assert.Contains(t, teamTaskNonTerminalStates, "pending")
	assert.Contains(t, teamTaskNonTerminalStates, "claimed")
	assert.Contains(t, teamTaskNonTerminalStates, "in_progress")
	assert.Contains(t, teamTaskNonTerminalStates, "blocked")
}

func TestTeamAppendUniqueSignal_去重(t *testing.T) {
	r := &TeamSkillEvolutionRail{}

	sig1 := signal.MakeTeamUserIntentSignal("test-skill", "improve accuracy")
	sig2 := signal.MakeTeamUserIntentSignal("test-skill", "improve accuracy")
	sig3 := signal.MakeTeamUserIntentSignal("test-skill", "add logging")

	var signals []*signal.EvolutionSignal
	r.teamAppendUniqueSignal(&signals, sig1)
	assert.Len(t, signals, 1)

	// 重复信号应被忽略
	r.teamAppendUniqueSignal(&signals, sig2)
	assert.Len(t, signals, 1)

	// 不同信号应被追加
	r.teamAppendUniqueSignal(&signals, sig3)
	assert.Len(t, signals, 2)
}

func TestTeamAppendUniqueSignal_边界(t *testing.T) {
	r := &TeamSkillEvolutionRail{}

	// nil 信号
	var signals []*signal.EvolutionSignal
	r.teamAppendUniqueSignal(&signals, nil)
	assert.Len(t, signals, 0)

	// nil 切片指针
	r.teamAppendUniqueSignal(nil, signal.MakeTeamUserIntentSignal("s", "i"))
	// 不 panic 即通过
}

func TestTeamExtractPresentedRecordIDs(t *testing.T) {
	// 正常场景：包含多个 record ID
	content := "## [rec_001] Some experience\n### [rec_002] Another experience\n#### [rec_003] Third"
	ids := teamExtractPresentedRecordIDs(content)
	assert.Equal(t, []string{"rec_001", "rec_002", "rec_003"}, ids)

	// 去重场景
	contentDedup := "## [rec_001] First\n## [rec_001] Duplicate\n## [rec_002] Second"
	idsDedup := teamExtractPresentedRecordIDs(contentDedup)
	assert.Equal(t, []string{"rec_001", "rec_002"}, idsDedup)

	// 空内容
	idsEmpty := teamExtractPresentedRecordIDs("")
	assert.Nil(t, idsEmpty)

	// 无匹配
	idsNoMatch := teamExtractPresentedRecordIDs("no headings here")
	assert.Nil(t, idsNoMatch)
}

func TestTeamSkillDefaultEvolutionTimeoutSecs(t *testing.T) {
	// Python: _DEFAULT_TEAM_EVOLUTION_TOTAL_TIMEOUT_SECS = 720.0
	assert.Equal(t, 720.0, teamSkillDefaultEvolutionTimeoutSecs)
}

func TestTeamSkillDefaultEvalInterval(t *testing.T) {
	assert.Equal(t, 5, teamSkillDefaultEvalInterval)
}

func TestTeamSkillMDRE_匹配(t *testing.T) {
	// 匹配 /path/team-planner/SKILL.md
	matches := teamSkillMDRE.FindStringSubmatch("/skills/team-planner/SKILL.md")
	assert.Len(t, matches, 2)
	assert.Equal(t, "team-planner", matches[1])

	// 不匹配
	noMatch := teamSkillMDRE.FindStringSubmatch("/skills/team-planner/README.md")
	assert.Nil(t, noMatch)
}

func TestTeamExperienceRecordHeadingRE_匹配(t *testing.T) {
	matches := teamExperienceRecordHeadingRE.FindStringSubmatch("## [ev_abc123] Some record")
	assert.Len(t, matches, 2)
	assert.Equal(t, "ev_abc123", matches[1])

	// 无匹配
	noMatch := teamExperienceRecordHeadingRE.FindStringSubmatch("no heading here")
	assert.Nil(t, noMatch)
}

func TestTeamSkillEvolutionRail_SnapshotForEvolution_禁用AutoScan(t *testing.T) {
	// autoScan=false → 返回 nil
	r := &TeamSkillEvolutionRail{autoScan: false}
	snapshot := r.SnapshotForEvolution(context.TODO(), nil, nil)
	assert.Nil(t, snapshot)
}

func TestTeamSkillEvolutionRail_RecordPresentedExperiences_无Tracker(t *testing.T) {
	// 无 tracker → 不 panic
	r := &TeamSkillEvolutionRail{}
	r.RecordPresentedExperiences(context.TODO(), "test-skill", "snippet", "sess-1", nil)
}

func TestTeamSkillEvolutionRail_RecordPresentedExperiences_有RecordIDs(t *testing.T) {
	// 有 recordIDs → 走 RecordPresentedRecords 路径
	// 无 tracker → 不 panic
	r := &TeamSkillEvolutionRail{}
	r.RecordPresentedExperiences(context.TODO(), "test-skill", "snippet", "sess-1", []string{"rec_001"})
}

func TestTeamExtractToolContent_各种输入(t *testing.T) {
	// map 类型结果含 skill_content
	inputs := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"skill_content": "hello world"},
	}
	assert.Equal(t, "hello world", teamExtractToolContent(inputs))

	// map 类型结果含 content
	inputs2 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"content": "fallback content"},
	}
	assert.Equal(t, "fallback content", teamExtractToolContent(inputs2))

	// .data 中间层含 skill_content（对齐 Python: getattr(result, "data", None)）
	inputs5 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"data": map[string]any{"skill_content": "from data layer"}},
	}
	assert.Equal(t, "from data layer", teamExtractToolContent(inputs5))

	// .data 中间层含 content
	inputs6 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"data": map[string]any{"content": "data content"}},
	}
	assert.Equal(t, "data content", teamExtractToolContent(inputs6))

	// .data 优先于顶层字段
	inputs7 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{
			"data":           map[string]any{"skill_content": "data wins"},
			"skill_content":  "top level",
		},
	}
	assert.Equal(t, "data wins", teamExtractToolContent(inputs7))

	// .data 不是 dict 时回退到顶层
	inputs8 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{
			"data":          "not a dict",
			"skill_content": "top level fallback",
		},
	}
	assert.Equal(t, "top level fallback", teamExtractToolContent(inputs8))

	// string 类型结果
	inputs3 := &agentinterfaces.ToolCallInputs{
		ToolResult: "direct string result",
	}
	assert.Equal(t, "direct string result", teamExtractToolContent(inputs3))

	// nil 结果
	inputs4 := &agentinterfaces.ToolCallInputs{}
	assert.Equal(t, "", teamExtractToolContent(inputs4))
}

// ─── Options 测试 ───

func TestWithTeamSkillAutoScan(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillAutoScan(false)(r)
	assert.False(t, r.autoScan)
}

func TestWithTeamSkillAutoSave(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillAutoSave(true)(r)
	assert.True(t, r.autoSave)
}

func TestWithTeamSkillEvalInterval(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillEvalInterval(10)(r)
	assert.Equal(t, 10, r.evalInterval)
}

func TestWithTeamSkillEvolutionTimeout(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillEvolutionTimeout(600.0)(r)
	assert.Equal(t, 600.0, r.evolutionTotalTimeoutSec)
}

func TestWithTeamSkillTeamID(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillTeamID("team-1")(r)
	assert.Equal(t, "team-1", r.teamID)
}

func TestWithTeamSkillTrajectoriesDir(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	WithTeamSkillTrajectoriesDir("/tmp/debug")(r)
	assert.Equal(t, "/tmp/debug", r.trajectoriesDir)
}

func TestWithTeamSkillLLMPolicies(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	p := llm_resilience.LLMInvokePolicy{MaxAttempts: 5}

	WithTeamSkillUserRequestLLMPolicy(p)(r)
	assert.Equal(t, 5, r.userRequestLLMPolicy.MaxAttempts)

	WithTeamSkillTrajectoryIssueLLMPolicy(p)(r)
	assert.Equal(t, 5, r.trajectoryIssueLLMPolicy.MaxAttempts)

	WithTeamSkillRecordLLMPolicy(p)(r)
	assert.Equal(t, 5, r.recordLLMPolicy.MaxAttempts)

	WithTeamSkillEvaluateLLMPolicy(p)(r)
	assert.Equal(t, 5, r.evaluateLLMPolicy.MaxAttempts)

	WithTeamSkillSimplifyLLMPolicy(p)(r)
	assert.Equal(t, 5, r.simplifyLLMPolicy.MaxAttempts)
}

func TestWithTeamSkillMemberRole_基类已初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	WithTeamSkillMemberRole("leader")(r)
	assert.Equal(t, "leader", r.EvolutionRail.defaultMemberRole)
}

func TestWithTeamSkillMemberRole_基类未初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	// 不应 panic
	WithTeamSkillMemberRole("leader")(r)
}

func TestWithTeamSkillAsyncEvolution_基类已初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	WithTeamSkillAsyncEvolution(false)(r)
	assert.False(t, r.EvolutionRail.asyncEvolution)
}

func TestWithTeamSkillAsyncEvolution_基类未初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	// 不应 panic
	WithTeamSkillAsyncEvolution(false)(r)
}

func TestWithTeamSkillMaxConcurrentEvolution_基类已初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	WithTeamSkillMaxConcurrentEvolution(3)(r)
	assert.Equal(t, 3, cap(r.EvolutionRail.evolutionSem))
}

func TestWithTeamSkillMaxConcurrentEvolution_基类未初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	// 不应 panic
	WithTeamSkillMaxConcurrentEvolution(3)(r)
}

func TestWithTeamSkillDisabledSkills_基类已初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	WithTeamSkillDisabledSkills([]string{"skill-a", "skill-b"})(r)
	assert.True(t, r.EvolutionRail.disabledSkills["skill-a"])
	assert.True(t, r.EvolutionRail.disabledSkills["skill-b"])
}

func TestWithTeamSkillDisabledSkills_基类未初始化(t *testing.T) {
	r := &TeamSkillEvolutionRail{}
	// 不应 panic
	WithTeamSkillDisabledSkills([]string{"skill-a"})(r)
}

func TestTeamSkillEvolutionRail_RunEvolution_DeferRecover(t *testing.T) {
	// RunEvolution 中的 defer recover 应捕获 panic
	r := &TeamSkillEvolutionRail{
		autoScan:      true,
		EvolutionRail: &EvolutionRail{},
	}
	// 使用一个会 panic 的场景——这里通过构造特殊的 trajectory 验证不传播
	// 实际测试：autoScan=true 但无 store → 不会 panic，只是正常完成
	err := r.RunEvolution(context.TODO(), &trajectory.Trajectory{}, nil)
	assert.NoError(t, err)
}

// ─── OnAfterToolCall 测试 ───

func TestTeamSkillEvolutionRail_OnAfterToolCall_非ToolCallInputs(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true, EvolutionRail: &EvolutionRail{}}
	// 空的 AgentCallbackContext，Inputs() 返回 nil
	cbc := &agentinterfaces.AgentCallbackContext{}
	err := r.OnAfterToolCall(context.TODO(), cbc)
	assert.NoError(t, err)
}

func TestTeamSkillEvolutionRail_OnAfterToolCall_非ViewTask(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true, EvolutionRail: &EvolutionRail{}}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "read_file"})
	err := r.OnAfterToolCall(context.TODO(), cbc)
	assert.NoError(t, err)
	// 不应标记 passiveEvolutionPending
	assert.False(t, r.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_OnAfterToolCall_ViewTask未完成(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true, EvolutionRail: &EvolutionRail{}}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: "status: in_progress",
	})
	err := r.OnAfterToolCall(context.TODO(), cbc)
	assert.NoError(t, err)
	assert.False(t, r.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_OnAfterToolCall_ViewTask已完成(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true, EvolutionRail: &EvolutionRail{}}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: "status: completed, all done",
	})
	err := r.OnAfterToolCall(context.TODO(), cbc)
	assert.NoError(t, err)
	assert.True(t, r.passiveEvolutionPending)
}

func TestTeamSkillEvolutionRail_OnAfterToolCall_禁用AutoScan(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: false, EvolutionRail: &EvolutionRail{}}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: "status: completed",
	})
	err := r.OnAfterToolCall(context.TODO(), cbc)
	assert.NoError(t, err)
	assert.False(t, r.passiveEvolutionPending)
}

// ─── NotifyTeamCompleted 测试 ───

func TestTeamSkillEvolutionRail_NotifyTeamCompleted_禁用AutoScan(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: false, EvolutionRail: &EvolutionRail{}}
	ok, err := r.NotifyTeamCompleted(context.TODO())
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestTeamSkillEvolutionRail_NotifyTeamCompleted_无SessionID(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: true, EvolutionRail: &EvolutionRail{}}
	ok, err := r.NotifyTeamCompleted(context.TODO())
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestTeamSkillEvolutionRail_NotifyTeamCompleted_正常(t *testing.T) {
	r := &TeamSkillEvolutionRail{
		autoScan:      true,
		EvolutionRail: &EvolutionRail{},
	}
	// 设置一个 hostCompletionPendingSessionID 来模拟 builder 有 sessionID
	sessionID := "sess-1"
	r.hostCompletionPendingSessionID = &sessionID
	// 由于无 Builder，currentBuilderSessionID 返回空串
	ok, err := r.NotifyTeamCompleted(context.TODO())
	assert.NoError(t, err)
	assert.False(t, ok) // 无 sessionID 可用
}

// ─── RunEvolution 禁用路径 ───

func TestTeamSkillEvolutionRail_RunEvolution_禁用AutoScan(t *testing.T) {
	r := &TeamSkillEvolutionRail{autoScan: false, EvolutionRail: &EvolutionRail{}}
	err := r.RunEvolution(context.TODO(), &trajectory.Trajectory{}, nil)
	assert.NoError(t, err)
}

// ─── 辅助 ───
