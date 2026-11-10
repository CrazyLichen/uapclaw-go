//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
	_ "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev/stages"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevPipelineSuite 覆盖技能开发流水线的编排层：阶段推进、挂起点、恢复、
// 检查点，以及 PLAN/GENERATE/IMPROVE/EVALUATE/DESC_OPTIMIZE 接入阶段 Agent 后的
// 真实内容流转（SKILL.md 版本变化、描述被优化）。
//
// 模型由按 prompt 关键字分派的 mock 客户端提供，保证多阶段、多轮次可确定复现。
type SkillDevPipelineSuite struct {
	isuite.BaseIntegrationSuite
	// tmpDir 每测试隔离根目录
	tmpDir string
	// taskID 测试任务标识
	taskID string
	// calls 各分支调用计数（用于断言阶段真的调用了模型）
	calls map[string]int
}

// pipelineMockClient 按 prompt 关键字分派的模型客户端。
type pipelineMockClient struct {
	// replyFor 根据用户 prompt 返回模型输出
	replyFor func(prompt string) string
	// calls 分派计数
	calls map[string]int
}

// Invoke 依据最后一条用户消息分派响应。
func (m *pipelineMockClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	prompt := lastUserPrompt(messages)
	return llmschema.NewAssistantMessage(m.replyFor(prompt)), nil
}

// Stream 未实现。
func (m *pipelineMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateImage 未实现。
func (m *pipelineMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateSpeech 未实现。
func (m *pipelineMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateVideo 未实现。
func (m *pipelineMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// TranscribeAudio 未实现。
func (m *pipelineMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release 无需释放。
func (m *pipelineMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持。
func (m *pipelineMockClient) SupportsKVCacheRelease() bool { return false }

// lastUserPrompt 取最后一条用户消息文本（兼容 dict / 对象 / 纯文本三种入参）。
func lastUserPrompt(messages model_clients.MessagesParam) string {
	prompt := ""
	for _, dict := range messages.Dicts() {
		if role, _ := dict["role"].(string); role != "user" {
			continue
		}
		if raw, ok := dict["content"]; ok && raw != nil {
			if v := fmt.Sprintf("%v", raw); v != "" {
				prompt = v
			}
		}
	}
	if prompt != "" {
		return prompt
	}

	for _, msg := range messages.Messages() {
		if msg.GetRole() != llmschema.RoleTypeUser {
			continue
		}
		if content := msg.GetContent(); content.IsText() && content.Text() != "" {
			prompt = content.Text()
		}
	}
	if prompt != "" {
		return prompt
	}
	return messages.Text()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillDevPipelineSuite 运行技能开发流水线集成测试。
func TestSkillDevPipelineSuite(t *testing.T) {
	suite.Run(t, new(SkillDevPipelineSuite))
}

// TestPipeline_挂起恢复与检查点 INIT/PLAN 执行后在 PLAN_CONFIRM 挂起并落检查点，
// 恢复后经 GENERATE/VALIDATE/TEST_DESIGN/TEST_RUN/EVALUATE 到 REVIEW 挂起，
// 且 SKILL.md 由模型真实生成（可通过校验）。
func (s *SkillDevPipelineSuite) TestPipeline_挂起恢复与检查点() {
	ctx := s.Ctx
	pipeline := s.newPipeline()

	events := s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Run(ctx)
	})
	s.Require().NotEmpty(events, "应产生阶段事件")
	s.Equal(skilldev.SkillDevStagePlanConfirm, pipeline.State.Stage, "应在 PLAN_CONFIRM 挂起")
	s.Equal("plan_confirm", s.firstConfirmType(events), "第一个挂起点应请求计划确认")
	s.FileExists(s.stateFile(), "挂起时应写入检查点 state.json")

	events = s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"plan": pipeline.State.Plan})
	})
	s.Require().NotEmpty(events)
	s.True(s.hasStageChanged(events, skilldev.SkillDevStageGenerate), "应执行过 GENERATE")
	s.True(s.hasStageChanged(events, skilldev.SkillDevStageEvaluate), "应执行过 EVALUATE")
	s.Equal(skilldev.SkillDevStageReview, pipeline.State.Stage, "应在 REVIEW 挂起")
	s.Equal("review", s.firstConfirmType(events))
	s.Contains(s.readSkillMD(), "description: 演示技能", "SKILL.md 应是模型生成内容")
	s.Contains(s.readSkillMD(), "# Demo V1", "GENERATE 应写入第一版正文")

	state := s.loadState()
	s.Equal(skilldev.SkillDevStageReview, state.Stage, "检查点阶段应为 REVIEW")
	s.Equal(0, state.Iteration, "首轮迭代计数应为 0")
}

// TestPipeline_改进一轮后内容变化 REVIEW 恢复（improve）后 IMPROVE 改写 SKILL.md，
// 迭代计数 +1，并再次进入 REVIEW 等待审阅。
func (s *SkillDevPipelineSuite) TestPipeline_改进一轮后内容变化() {
	ctx := s.Ctx
	pipeline := s.newPipeline()

	s.runToReview(ctx, pipeline)
	s.Contains(s.readSkillMD(), "# Demo V1")

	s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"action": "improve", "feedback": map[string]any{"comment": "补充失败处理"}})
	})

	s.Equal(1, pipeline.State.Iteration, "IMPROVE 应使迭代计数 +1")
	s.Equal(skilldev.SkillDevStageReview, pipeline.State.Stage, "改进后应再次进入 REVIEW")
	skillMD := s.readSkillMD()
	s.Contains(skillMD, "# Demo V2", "SKILL.md 应被改进内容覆盖")
	s.Contains(skillMD, "补充失败处理", "改进内容应体现反馈")
	s.Equal(1, s.calls["improve"], "应真实调用一次改进 Agent")
}

// TestPipeline_打包与描述优化 REVIEW 选择打包后进入 DESC_OPTIMIZE_CONFIRM，
// 选择优化后描述被改写并进入 COMPLETED。
func (s *SkillDevPipelineSuite) TestPipeline_打包与描述优化() {
	ctx := s.Ctx
	pipeline := s.newPipeline()

	s.runToReview(ctx, pipeline)

	events := s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"action": "package"})
	})
	s.True(s.hasStageChanged(events, skilldev.SkillDevStagePackage), "应执行过 PACKAGE")
	s.Equal(skilldev.SkillDevStageDescOptimizeConfirm, pipeline.State.Stage, "应在 DESC_OPTIMIZE_CONFIRM 挂起")

	s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"action": "optimize"})
	})
	s.Equal(skilldev.SkillDevStageCompleted, pipeline.State.Stage, "描述优化后应进入 COMPLETED")
	s.Contains(s.readSkillMD(), "用户提到表格、导出、清洗数据时使用", "description 应被优化结果替换")
	s.GreaterOrEqual(s.calls["desc_gen"], 1, "应调用触发查询生成 Agent")
	s.GreaterOrEqual(s.calls["desc_improve"], 1, "应调用描述改进 Agent")
}

// TestPipeline_非挂起点Resume报错 非挂起阶段调用 Resume 应返回错误（对齐 Python 语义）。
func (s *SkillDevPipelineSuite) TestPipeline_非挂起点Resume报错() {
	ctx := s.Ctx
	pipeline := s.newPipeline()

	pipeline.State.Stage = skilldev.SkillDevStageGenerate
	_, err := pipeline.Resume(ctx, map[string]any{})
	s.Require().Error(err, "非挂起阶段 Resume 应失败")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每用例独立任务与目录。
func (s *SkillDevPipelineSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.taskID = "task-itest-001"
	s.calls = map[string]int{}
}

// newPipeline 构造带分派 mock 模型的流水线（真实磁盘 StateStore / WorkspaceProvider）。
func (s *SkillDevPipelineSuite) newPipeline() *skilldev.SkillDevPipeline {
	s.T().Helper()

	client := &pipelineMockClient{replyFor: s.dispatch, calls: s.calls}
	provider := fmt.Sprintf("skilldev_pipeline_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})
	s.T().Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	deps := &skilldev.SkillDevDeps{
		ModelName: "mock-model",
		ModelClientConfig: map[string]any{
			"client_provider": provider,
			"client_id":       provider + "_id",
			"api_key":         "test-key",
			"api_base":        "http://127.0.0.1:1",
		},
		StateStore:        skilldev.NewStateStore(filepath.Join(s.tmpDir, "tasks")),
		WorkspaceProvider: skilldev.NewWorkspaceProvider(filepath.Join(s.tmpDir, "workspaces")),
		SysOpConfig: sysop.NewSysOperationCard(
			sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
		),
	}
	state := skilldev.NewSkillDevState(s.taskID)
	state.Input = map[string]any{"query": "生成一个处理 CSV 的技能"}
	return skilldev.NewSkillDevPipeline(s.taskID, state, deps)
}

// dispatch 按 prompt 关键字返回固定模型输出。
func (s *SkillDevPipelineSuite) dispatch(prompt string) string {
	switch {
	case strings.Contains(prompt, "触发测试查询 JSON 数组"):
		s.calls["desc_gen"]++
		return `[{"query": "帮我处理这个 CSV 文件", "should_trigger": true},
{"query": "把这张表格导出成 JSON", "should_trigger": true},
{"query": "清洗这批数据并统计", "should_trigger": true},
{"query": "帮我写一个排序算法", "should_trigger": false},
{"query": "画一个饼图", "should_trigger": false},
{"query": "给我讲讲 HTTP 协议", "should_trigger": false}]`
	case strings.Contains(prompt, "JSON 布尔数组"):
		s.calls["desc_eval"]++
		// 首轮（train+test）全部判定失败 → 触发描述改进；改进后判定全部正确 → 收敛
		if s.calls["desc_eval"] <= 2 {
			return answerArray(prompt, false)
		}
		return answerArray(prompt, true)
	case strings.Contains(prompt, "请输出新的 description"):
		s.calls["desc_improve"]++
		return "<new_description>处理 CSV 与 JSON 数据文件，用户提到表格、导出、清洗数据时使用</new_description>"
	case strings.Contains(prompt, "benchmark 数据"):
		s.calls["analyst"]++
		return `["技能在基础场景通过", "样本量偏小"]`
	case strings.Contains(prompt, "配置："):
		s.calls["grader"]++
		return `{"expectations":[{"text":"输出包含结果","passed":true,"evidence":"transcript 中出现结果"}],"pass_rate":1.0}`
	case strings.Contains(prompt, "根据反馈改进当前技能"):
		s.calls["improve"]++
		return "---\nname: demo-skill\ndescription: 演示技能\n---\n# Demo V2\n补充失败处理：遇到解析错误时输出诊断信息。\n"
	case strings.Contains(prompt, "本次只生成文件"):
		s.calls["generate"]++
		return "---\nname: demo-skill\ndescription: 演示技能\n---\n# Demo V1\n读取 CSV 并输出统计结果。\n"
	default:
		s.calls["plan"]++
		return `{"skill_name":"demo-skill","description":"演示技能","purpose":"处理 CSV",
"directory_structure":{"SKILL.md":"主指令文件"},"test_strategy":{"approach":"基础场景"}}`
	}
}

// answerArray 按 prompt 中的查询数量返回等长布尔数组。
func answerArray(prompt string, value bool) string {
	start := strings.LastIndex(prompt, "[")
	end := strings.LastIndex(prompt, "]")
	if start < 0 || end <= start {
		return fmt.Sprintf("[%t]", value)
	}
	var queries []string
	if err := json.Unmarshal([]byte(prompt[start:end+1]), &queries); err != nil {
		return fmt.Sprintf("[%t]", value)
	}
	answers := make([]string, len(queries))
	for i := range answers {
		answers[i] = fmt.Sprintf("%t", value)
	}
	return "[" + strings.Join(answers, ",") + "]"
}

// runToReview 推进到 REVIEW 挂起点（两次 Resume）。
func (s *SkillDevPipelineSuite) runToReview(ctx context.Context, pipeline *skilldev.SkillDevPipeline) {
	s.T().Helper()

	s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Run(ctx)
	})
	s.Require().Equal(skilldev.SkillDevStagePlanConfirm, pipeline.State.Stage)
	s.drain(ctx, func() (<-chan skilldev.SkillDevEvent, error) {
		return pipeline.Resume(ctx, map[string]any{"plan": pipeline.State.Plan})
	})
	s.Require().Equal(skilldev.SkillDevStageReview, pipeline.State.Stage)
}

// drain 启动流水线并收集全部事件（带超时保护，error 事件直接失败）。
func (s *SkillDevPipelineSuite) drain(
	ctx context.Context,
	start func() (<-chan skilldev.SkillDevEvent, error),
) []skilldev.SkillDevEvent {
	s.T().Helper()

	ch, err := start()
	s.Require().NoError(err)
	s.Require().NotNil(ch)

	deadline := time.After(60 * time.Second)
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

// readSkillMD 读取工作区内生成的 SKILL.md。
func (s *SkillDevPipelineSuite) readSkillMD() string {
	s.T().Helper()
	data, err := os.ReadFile(filepath.Join(s.workspaceDir(), "skill", "SKILL.md"))
	s.Require().NoError(err)
	return string(data)
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

// SupportsKVCacheAffinity 测试替身：不支持 KV 亲和。
func (m *pipelineMockClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 测试替身：返回空亲和参数。
func (m *pipelineMockClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}
