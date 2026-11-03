//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
	_ "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev/stages"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevPipelineSuite 覆盖技能开发流水线的编排层：阶段推进、挂起点、恢复与检查点。
//
// 重要事实（决定了覆盖边界）：各 LLM 阶段的内部实现仍是占位
// （skilldev/context.go:CreateStageAgent 返回「尚未接入 openjiuwen，待实现」，
// plan/generate/evaluate/improve/desc_optimize 均返回占位数据）。
// 因此本套件只断言**确定性的编排行为**（阶段序列、挂起、恢复、持久化、产物文件存在），
// 不断言技能内容质量；等 create_stage_agent 接通后再补多轮迭代的内容级断言。
type SkillDevPipelineSuite struct {
	isuite.BaseIntegrationSuite
	// tmpDir 每测试隔离根目录
	tmpDir string
	// taskID 测试任务标识
	taskID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillDevPipelineSuite 运行技能开发流水线编排测试。
func TestSkillDevPipelineSuite(t *testing.T) {
	suite.Run(t, new(SkillDevPipelineSuite))
}

// TestPipeline_挂起恢复与检查点 INIT/PLAN 执行后在 PLAN_CONFIRM 挂起并落检查点，
// 恢复后继续推进到 GENERATE/VALIDATE。
func (s *SkillDevPipelineSuite) TestPipeline_挂起恢复与检查点() {
	ctx := s.Ctx
	_, pipeline := s.newPipeline()

	// 第一段：跑到 PLAN_CONFIRM 挂起
	events := s.drain(ctx, pipeline, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Run(ctx)
	})
	s.Require().NotEmpty(events, "应产生阶段事件")
	s.Equal(skilldev.SkillDevStagePlanConfirm, pipeline.State.Stage, "应在 PLAN_CONFIRM 挂起")
	s.Equal("plan_confirm", s.firstConfirmType(events), "第一个挂起点应请求计划确认")
	s.FileExists(s.stateFile(), "挂起时应写入检查点 state.json")

	// 第二轮：确认计划 → 继续执行（当前生成器为占位，末段由迭代上限收敛，故用宽松收集）
	events = s.drainLenient(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"plan": pipeline.State.Plan})
	})
	s.Require().NotEmpty(events)
	s.True(s.hasStageChanged(events, skilldev.SkillDevStageGenerate), "应执行过 GENERATE 阶段")
	s.True(s.hasStageChanged(events, skilldev.SkillDevStageValidate), "应执行过 VALIDATE 阶段")
	s.FileExists(filepath.Join(s.workspaceDir(), "skill", "SKILL.md"), "GENERATE 阶段应产出 SKILL.md 占位文件")

	// 检查点内容可反序列化
	state := s.loadState()
	s.Equal(s.taskID, state.TaskID)
	s.NotEqual(skilldev.SkillDevStageInit, state.Stage, "检查点阶段应已推进")
}

// TestPipeline_生成器占位时由迭代上限兜底 当前 GENERATE 产出占位 SKILL.md，
// VALIDATE 必然失败并回退 GENERATE，流水线靠 maxPipelineIterations 兜底退出，
// 不会挂死。等 skilldev.CreateStageAgent 接通后本用例应改为「推进到 REVIEW 挂起」。
func (s *SkillDevPipelineSuite) TestPipeline_生成器占位时由迭代上限兜底() {
	ctx := s.Ctx
	_, pipeline := s.newPipeline()

	// 推进到 PLAN_CONFIRM
	s.drain(ctx, pipeline, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Run(ctx)
	})
	s.Require().Equal(skilldev.SkillDevStagePlanConfirm, pipeline.State.Stage)

	// 恢复后收集事件直到 channel 关闭（不因 error 事件提前失败）
	ch, err := pipeline.Resume(ctx, map[string]any{"plan": pipeline.State.Plan})
	s.Require().NoError(err)

	deadline := time.After(30 * time.Second)
	sawValidateFail := false
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			if evt.EventType == skilldev.SkillDevEventTypeValidateResult {
				if valid, _ := evt.Payload["valid"].(bool); !valid {
					sawValidateFail = true
				}
			}
		case <-deadline:
			s.FailNow("流水线未在超时内退出（可能挂死）")
		case <-ctx.Done():
			s.FailNow("上下文已取消")
		}
	}
done:
	s.True(sawValidateFail, "VALIDATE 应对占位 SKILL.md 报告校验失败")
	s.Equal(skilldev.SkillDevStageError, pipeline.State.Stage, "占位生成器下流水线应以 ERROR 收敛")
	s.Require().NotNil(pipeline.State.Error)
	s.Contains(*pipeline.State.Error, "最大迭代次数", "应由迭代上限兜底而非无限循环")
}

// TestPipeline_非挂起点Resume报错 非挂起阶段调用 Resume 应返回错误（对齐 Python 语义）。
func (s *SkillDevPipelineSuite) TestPipeline_非挂起点Resume报错() {
	ctx := s.Ctx
	_, pipeline := s.newPipeline()

	pipeline.State.Stage = skilldev.SkillDevStageGenerate
	_, err := pipeline.Resume(ctx, map[string]any{})
	s.Require().Error(err, "非挂起阶段 Resume 应失败")
}

// ──────────────────────────── 非导函数 ────────────────────────────

// SetupTest 每用例独立任务与目录。
func (s *SkillDevPipelineSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.taskID = "task-itest-001"
}

// newPipeline 构造带真实磁盘 StateStore/WorkspaceProvider 的流水线。
func (s *SkillDevPipelineSuite) newPipeline() (*skilldev.SkillDevDeps, *skilldev.SkillDevPipeline) {
	deps := &skilldev.SkillDevDeps{
		ModelName:         "mock-model",
		StateStore:        skilldev.NewStateStore(filepath.Join(s.tmpDir, "tasks")),
		WorkspaceProvider: skilldev.NewWorkspaceProvider(filepath.Join(s.tmpDir, "workspaces")),
	}
	state := skilldev.NewSkillDevState(s.taskID)
	state.Input = map[string]any{"query": "生成一个处理 CSV 的技能"}
	return deps, skilldev.NewSkillDevPipeline(s.taskID, state, deps)
}

// drain 启动流水线并收集全部事件（带超时保护）。
func (s *SkillDevPipelineSuite) drain(
	ctx context.Context,
	_ *skilldev.SkillDevPipeline,
	start func() (<-chan skilldev.SkillDevEvent, error),
) []skilldev.SkillDevEvent {
	s.T().Helper()

	ch, err := start()
	s.Require().NoError(err)
	s.Require().NotNil(ch)

	deadline := time.After(30 * time.Second)
	var events []skilldev.SkillDevEvent
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
			s.NotEqual(skilldev.SkillDevEventTypeError, evt.EventType, "流水线不应产生 error 事件: %v", evt.Payload)
		case <-deadline:
			s.FailNow("流水线事件收集超时")
			return events
		case <-ctx.Done():
			s.FailNow("上下文已取消")
			return events
		}
	}
}

// drainLenient 收集事件但不因 error 事件失败（用于当前占位阶段的收敛路径）。
func (s *SkillDevPipelineSuite) drainLenient(
	ctx context.Context,
	start func() (<-chan skilldev.SkillDevEvent, error),
) []skilldev.SkillDevEvent {
	s.T().Helper()

	ch, err := start()
	s.Require().NoError(err)
	s.Require().NotNil(ch)

	deadline := time.After(30 * time.Second)
	var events []skilldev.SkillDevEvent
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
		case <-deadline:
			s.FailNow("流水线事件收集超时")
			return events
		case <-ctx.Done():
			s.FailNow("上下文已取消")
			return events
		}
	}
}

// firstConfirmType 返回第一个确认请求事件的 confirm_type。
func (s *SkillDevPipelineSuite) firstConfirmType(events []skilldev.SkillDevEvent) string {
	for _, evt := range events {
		if evt.EventType != skilldev.SkillDevEventTypeConfirmRequest {
			continue
		}
		value, _ := evt.Payload["confirm_type"].(string)
		return value
	}
	return ""
}

// hasStageChanged 判断事件流中出现过指定阶段的 stage_changed。
func (s *SkillDevPipelineSuite) hasStageChanged(events []skilldev.SkillDevEvent, stage skilldev.SkillDevStage) bool {
	for _, evt := range events {
		if evt.EventType != skilldev.SkillDevEventTypeStageChanged {
			continue
		}
		if value, _ := evt.Payload["stage"].(string); value == string(stage) {
			return true
		}
	}
	return false
}

// stateFile 返回检查点文件路径。
func (s *SkillDevPipelineSuite) stateFile() string {
	return filepath.Join(s.tmpDir, "tasks", s.taskID, "state.json")
}

// workspaceDir 返回任务工作区根目录。
func (s *SkillDevPipelineSuite) workspaceDir() string {
	return filepath.Join(s.tmpDir, "workspaces", s.taskID)
}

// loadState 读取并反序列化检查点。
func (s *SkillDevPipelineSuite) loadState() *skilldev.SkillDevState {
	s.T().Helper()
	data, err := os.ReadFile(s.stateFile())
	s.Require().NoError(err)
	var raw map[string]any
	s.Require().NoError(json.Unmarshal(data, &raw))
	return skilldev.FromCheckpointDict(raw)
}
