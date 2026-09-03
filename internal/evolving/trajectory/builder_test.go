package trajectory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── NewTrajectoryBuilder 测试 ────────────────────────────

// TestNewTrajectoryBuilder 创建基本构建器
func TestNewTrajectoryBuilder(t *testing.T) {
	b := NewTrajectoryBuilder("conv_123", "online")
	assert.Equal(t, "conv_123", b.sessionID)
	assert.Equal(t, "online", b.source)
	assert.Empty(t, b.steps)
	assert.Equal(t, 0, b.cost["input_tokens"])
	assert.Equal(t, 0, b.cost["output_tokens"])
}

// TestNewTrajectoryBuilder_带选项 创建带选项的构建器
func TestNewTrajectoryBuilder_带选项(t *testing.T) {
	maxSteps := 5
	b := NewTrajectoryBuilder("sess_1", "offline",
		WithCaseID("case_1"),
		WithMemberID("member_1"),
		WithMeta(map[string]any{"key": "value"}),
		WithMaxSteps(5),
	)
	assert.Equal(t, "case_1", b.caseID)
	assert.Equal(t, "member_1", b.memberID)
	assert.Equal(t, &maxSteps, b.maxSteps)
	// member_id 在 Build() 中通过 member_id 写入 meta，对齐 Python build() 逻辑
}

// TestWithMaxSteps_无效值 maxSteps < 1 时忽略
func TestWithMaxSteps_无效值(t *testing.T) {
	// maxSteps < 1 时忽略
	b := NewTrajectoryBuilder("sess", "online", WithMaxSteps(0))
	assert.Nil(t, b.maxSteps)

	b = NewTrajectoryBuilder("sess", "online", WithMaxSteps(-1))
	assert.Nil(t, b.maxSteps)
}

// ──────────────────────────── RecordStep 测试 ────────────────────────────

// TestRecordStep_基本累积 验证步骤基本累积
func TestRecordStep_基本累积(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	step := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}
	b.RecordStep(step)
	assert.Len(t, b.steps, 1)
	assert.Equal(t, step, b.steps[0])
}

// TestRecordStep_MaxSteps滑窗截断 验证最大步骤数滑窗截断
func TestRecordStep_MaxSteps滑窗截断(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online", WithMaxSteps(2))
	s1 := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}
	s2 := &TrajectoryStep{Kind: StepKindLLM, Meta: map[string]any{}}
	s3 := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}

	b.RecordStep(s1)
	b.RecordStep(s2)
	assert.Len(t, b.steps, 2)

	b.RecordStep(s3)
	assert.Len(t, b.steps, 2)
	assert.Equal(t, s2, b.steps[0]) // 保留最后 2 个
	assert.Equal(t, s3, b.steps[1])
}

// TestRecordStep_LLMCost累积 验证 LLM 步骤累积 cost
func TestRecordStep_LLMCost累积(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	step := &TrajectoryStep{
		Kind: StepKindLLM,
		Detail: &LLMCallDetail{
			Usage: map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 50,
			},
		},
		Meta: map[string]any{},
	}
	b.RecordStep(step)
	assert.Equal(t, 100, b.cost["input_tokens"])
	assert.Equal(t, 50, b.cost["output_tokens"])

	// 非 LLM 步骤不累积 cost
	toolStep := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}
	b.RecordStep(toolStep)
	assert.Equal(t, 100, b.cost["input_tokens"])
	assert.Equal(t, 50, b.cost["output_tokens"])
}

// TestRecordStep_记录首个步骤开始时间 验证首个有 StartTimeMs 的步骤记录开始时间
func TestRecordStep_记录首个步骤开始时间(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")

	// 首个步骤 StartTimeMs 为 0，不记录
	s1 := &TrajectoryStep{Kind: StepKindTool, StartTimeMs: 0, Meta: map[string]any{}}
	b.RecordStep(s1)
	assert.Nil(t, b.startTimeMs)

	// 第二个步骤有 StartTimeMs
	s2 := &TrajectoryStep{Kind: StepKindTool, StartTimeMs: 1000, Meta: map[string]any{}}
	b.RecordStep(s2)
	assert.NotNil(t, b.startTimeMs)
	assert.Equal(t, 1000, *b.startTimeMs)

	// 后续步骤不再更新 startTimeMs
	s3 := &TrajectoryStep{Kind: StepKindTool, StartTimeMs: 2000, Meta: map[string]any{}}
	b.RecordStep(s3)
	assert.Equal(t, 1000, *b.startTimeMs)
}

// TestRecordStep_LLMCost累积_float64验证 验证 float64 类型的 token 使用量
func TestRecordStep_LLMCost累积_float64验证(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	step := &TrajectoryStep{
		Kind: StepKindLLM,
		Detail: &LLMCallDetail{
			Usage: map[string]any{
				"prompt_tokens":     float64(100),
				"completion_tokens": float64(50),
			},
		},
		Meta: map[string]any{},
	}
	b.RecordStep(step)
	assert.Equal(t, 100, b.cost["input_tokens"])
	assert.Equal(t, 50, b.cost["output_tokens"])
}

// TestRecordStep_LLMCost累积_int64验证 验证 int64 类型的 token 使用量
func TestRecordStep_LLMCost累积_int64验证(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	step := &TrajectoryStep{
		Kind: StepKindLLM,
		Detail: &LLMCallDetail{
			Usage: map[string]any{
				"prompt_tokens":     int64(200),
				"completion_tokens": int64(100),
			},
		},
		Meta: map[string]any{},
	}
	b.RecordStep(step)
	assert.Equal(t, 200, b.cost["input_tokens"])
	assert.Equal(t, 100, b.cost["output_tokens"])
}

// ──────────────────────────── Build 测试 ────────────────────────────

// TestBuild_基本组装 验证基本组装逻辑
func TestBuild_基本组装(t *testing.T) {
	b := NewTrajectoryBuilder("sess_1", "offline", WithCaseID("case_1"))
	s1 := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}
	b.RecordStep(s1)

	traj := b.Build()
	assert.NotEmpty(t, traj.ExecutionID)
	assert.Equal(t, "sess_1", traj.SessionID)
	assert.Equal(t, "offline", traj.Source)
	assert.Equal(t, "case_1", traj.CaseID)
	assert.Len(t, traj.Steps, 1)
	assert.Equal(t, s1, traj.Steps[0])
}

// TestBuild_Cost为零时为nil 验证 cost 为零时设为 nil
func TestBuild_Cost为零时为nil(t *testing.T) {
	// 对齐 Python: cost=self.cost if self.cost["input_tokens"] > 0 else None
	b := NewTrajectoryBuilder("sess", "online")
	s1 := &TrajectoryStep{Kind: StepKindTool, Meta: map[string]any{}}
	b.RecordStep(s1)

	traj := b.Build()
	assert.Nil(t, traj.Cost)
}

// TestBuild_Cost非零时保留 验证 cost 非零时保留
func TestBuild_Cost非零时保留(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	s1 := &TrajectoryStep{
		Kind: StepKindLLM,
		Detail: &LLMCallDetail{
			Usage: map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 50,
			},
		},
		Meta: map[string]any{},
	}
	b.RecordStep(s1)

	traj := b.Build()
	assert.NotNil(t, traj.Cost)
	assert.Equal(t, 100, traj.Cost["input_tokens"])
	assert.Equal(t, 50, traj.Cost["output_tokens"])
}

// TestBuild_MemberID写入Meta 验证 MemberID 写入 Meta
func TestBuild_MemberID写入Meta(t *testing.T) {
	// 对齐 Python: if self.member_id: meta["member_id"] = self.member_id
	b := NewTrajectoryBuilder("sess", "online", WithMemberID("leader"))
	traj := b.Build()
	assert.Equal(t, "leader", traj.Meta["member_id"])
}

// TestBuild_Meta合并 验证 Meta 合并逻辑
func TestBuild_Meta合并(t *testing.T) {
	// 对齐 Python: meta=dict(self.meta)
	b := NewTrajectoryBuilder("sess", "online",
		WithMeta(map[string]any{"custom_key": "custom_value"}),
		WithMemberID("member_1"),
	)
	traj := b.Build()
	assert.Equal(t, "custom_value", traj.Meta["custom_key"])
	assert.Equal(t, "member_1", traj.Meta["member_id"])
}

// TestBuild_无MemberID 验证无 memberID 时 meta 中无 member_id
func TestBuild_无MemberID(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online")
	traj := b.Build()
	assert.NotContains(t, traj.Meta, "member_id")
}

// TestBuild_MemberID已在Meta 验证 Build 时不覆盖 meta 中已存在的 member_id
func TestBuild_MemberID已在Meta(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online",
		WithMeta(map[string]any{"member_id": "original"}),
		WithMemberID("new_member"),
	)
	traj := b.Build()
	// setdefault 语义：member_id 已存在于 meta 中，不覆盖
	assert.Equal(t, "original", traj.Meta["member_id"])
}

// ──────────────────────────── WithMemberID 测试 ────────────────────────────

// TestWithMemberID_空字符串 验证空字符串不设置 member_id
func TestWithMemberID_空字符串(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online", WithMemberID(""))
	assert.Equal(t, "", b.memberID)
	// 空 meta + 空字符串 memberID → meta 中无 member_id
	assert.NotContains(t, b.meta, "member_id")
}

// TestWithMemberID_Meta已有MemberID 验证不覆盖已存在的 member_id
func TestWithMemberID_Meta已有MemberID(t *testing.T) {
	b := NewTrajectoryBuilder("sess", "online",
		WithMeta(map[string]any{"member_id": "existing"}),
		WithMemberID("new_member"),
	)
	// WithMeta 先执行，设置 member_id=existing；WithMemberID 发现已存在，不覆盖
	assert.Equal(t, "existing", b.meta["member_id"])
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestToIntFromAny 验证各种类型安全转换为 int
func TestToIntFromAny(t *testing.T) {
	assert.Equal(t, 10, toIntFromAny(10))
	assert.Equal(t, 20, toIntFromAny(int64(20)))
	assert.Equal(t, 30, toIntFromAny(float64(30)))
	assert.Equal(t, 0, toIntFromAny("not a number"))
	assert.Equal(t, 0, toIntFromAny(nil))
}
