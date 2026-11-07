//go:build integration

package harness_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AdmitUserMessageSuite 覆盖 spec C9/C10/C11/C12/C13：输入批次载体全链路。
//
// Python: ReActAgent._admit_user_message（react_agent.py L917-955）
type AdmitUserMessageSuite struct {
	isuite.AgentSuite
}

// capturePartsRail 记录 on_user_message 看到的 parts 并可选前插/清空。
type capturePartsRail struct {
	agentinterfaces.BaseRail
	// seen 每次触发的 parts 快照
	seen [][]string
	// source 每次触发的 source
	source []string
	// prepend 非空时前插该文本
	prepend string
	// dropAll 为真时清空 parts
	dropAll bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAdmitUserMessageSuite 运行 AdmitUserMessageSuite。
func TestAdmitUserMessageSuite(t *testing.T) { suite.Run(t, new(AdmitUserMessageSuite)) }

// GetCallbacks 声明 OnUserMessage。
func (r *capturePartsRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackOnUserMessage, r.onUserMessage),
	)
}

// TestIT_多输入一轮_parts到达rails 验证 parts 与 rail 前插落到历史。
//
// spec C9/C13：3 个 part 到达 rail，前插 X → 历史里一条 "X\na\nb\nc"。
func (s *AdmitUserMessageSuite) TestIT_多输入一轮_parts到达rails() {
	ctx := context.Background()
	rail := &capturePartsRail{prepend: "X"}

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, sess := s.createPartsAgent(ctx, rail, "it-parts-multi")
	s.Require().NoError(sess.PreRun(ctx))

	_, err := agent.Invoke(ctx, map[string]any{
		"query":        "a\nb\nc",
		"_input_parts": []string{"a", "b", "c"},
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(ctx)

	s.Require().NotEmpty(rail.seen, "on_user_message 必须被触发")
	s.Equal([]string{"a", "b", "c"}, rail.seen[0], "rails 必须看到 3 个未 join 的 part")
	s.Equal("query", rail.source[0])

	// 历史里的 UserMessage 应为 X\na\nb\nc
	s.Contains(s.modelSaw(0), "X\na\nb\nc", "前插必须落在 join 之前")
}

// TestIT_rails丢空输入不写历史 验证清空 parts 后不写历史且 inputs 已还原。
//
// spec C10/C11。
func (s *AdmitUserMessageSuite) TestIT_rails丢空输入不写历史() {
	ctx := context.Background()
	rail := &capturePartsRail{dropAll: true}

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, sess := s.createPartsAgent(ctx, rail, "it-parts-drop")
	s.Require().NoError(sess.PreRun(ctx))

	_, err := agent.Invoke(ctx, map[string]any{
		"query":        "a\nb",
		"_input_parts": []string{"a", "b"},
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(ctx)

	s.NotContains(s.modelSaw(0), "a\nb", "parts 被丢空后不得写入拼接消息")
}

// TestIT_输入parts_真实InputEvent多帧 验证多帧 InputEvent 的三段完整到达 rails。
//
// spec C12/C13 的集成侧：真实 modules.TaskManager 里流转一个携带 3 帧文本的
// InputEvent，提取后经真实 DeepAgent 链路送达 rails，断言 rails 看到 3 段。
//
// 注：extractInputParts 是 task_loop 包的未导出函数，跨包不可调；其
// "只认文本帧 / 过滤空串 / >1 条才返回" 的语义由 task_loop 包的单元用例
// TestExtractInputParts_多帧才返回、TestExtractInputParts_非文本帧被忽略
// 与 TestTaskLoopExecutor_改写query则不带parts 覆盖。本用例负责的是
// "多帧数据在真实组件间流转后仍完整"这一集成事实。
func (s *AdmitUserMessageSuite) TestIT_输入parts_真实InputEvent多帧() {
	ctx := context.Background()

	// 真实 TaskManager 里登记一个携带多帧 InputEvent 的任务
	tm := modules.NewTaskManager(&config.ControllerConfig{})
	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "第一段"},
			&cschema.TextDataFrame{Text: "第二段"},
			&cschema.TextDataFrame{Text: "第三段"},
		},
	}
	task := &cschema.Task{
		TaskID:      "it-parts-task",
		Description: "第一段\n第二段\n第三段",
		Status:      cschema.TaskSubmitted,
		Inputs:      []cschema.Event{evt},
		Metadata:    map[string]any{},
	}
	s.Require().NoError(tm.AddTask(ctx, task))

	tasks, err := tm.GetTask(ctx, &modules.TaskFilter{TaskID: "it-parts-task"})
	s.Require().NoError(err)
	s.Require().Len(tasks, 1)

	// 复刻 executor 步骤 3 的提取
	var got *cschema.InputEvent
	for _, e := range tasks[0].Inputs {
		if ie, ok := e.(*cschema.InputEvent); ok {
			got = ie
			break
		}
	}
	s.Require().NotNil(got, "任务里必须能取回 InputEvent")

	// 走真实 agent 链路验证三帧可达 rails
	rail := &capturePartsRail{}
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	agent, sess := s.createPartsAgent(ctx, rail, "it-parts-event")
	s.Require().NoError(sess.PreRun(ctx))

	parts := inputPartsOf(got)
	s.Require().Len(parts, 3, "三个文本帧必须被提取")

	_, err = agent.Invoke(ctx, map[string]any{
		"query":        strings.Join(parts, "\n"),
		"_input_parts": parts,
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(ctx)

	s.Require().NotEmpty(rail.seen)
	s.Equal([]string{"第一段", "第二段", "第三段"}, rail.seen[0],
		"executor 提取的三段必须完整到达 rails")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onUserMessage 记录 parts 并可选前插/清空。
func (r *capturePartsRail) onUserMessage(_ context.Context, railCtx any) error {
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	in, ok := cbc.Inputs().(*agentinterfaces.UserMessageInputs)
	if !ok {
		return nil
	}
	r.seen = append(r.seen, append([]string(nil), in.Parts...))
	r.source = append(r.source, in.Source)
	if r.dropAll {
		in.Parts = nil
		return nil
	}
	if r.prepend != "" {
		in.Parts = append([]string{r.prepend}, in.Parts...)
	}
	return nil
}

// createPartsAgent 构造挂了 parts rail 的 DeepAgent。
func (s *AdmitUserMessageSuite) createPartsAgent(
	ctx context.Context,
	rail *capturePartsRail,
	sessionName string,
) (*harness.DeepAgent, *session.Session) {
	dummyTool := testhelpers.NewBlockingTool("dummy_tool")
	dummyTool.Release()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations:     3,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{dummyTool.Tool()},
	})
	s.Require().NoError(err)
	// 必须在 EnsureInitialized 之后注册：桥接事件需要内层 ReActAgent 已创建
	ctx, err = agent.EnsureInitialized(ctx)
	s.Require().NoError(err)
	s.Require().NoError(agent.RegisterRail(ctx, rail))

	return agent, s.NewTestSession(sessionName)
}

// modelSaw 把第 i 次模型调用收到的消息拼成文本。
func (s *AdmitUserMessageSuite) modelSaw(i int) string {
	call := s.MockLLM.GetInvokeCall(i)
	if call == nil {
		return ""
	}
	var sb strings.Builder
	for _, m := range call.Messages.Messages() {
		if m == nil {
			continue
		}
		sb.WriteString(m.GetContent().Text())
		sb.WriteString("\n")
	}
	return sb.String()
}

// inputPartsOf 复刻 executor 的 extractInputParts 语义（测试侧实现，避免依赖未导出函数）。
//
// Python: task_loop_event_executor.py L431-448 —— 只认文本帧、过滤空串、>1 条才返回。
func inputPartsOf(evt *cschema.InputEvent) []string {
	if evt == nil {
		return nil
	}
	texts := make([]string, 0, len(evt.InputData))
	for _, df := range evt.InputData {
		if td, ok := df.(*cschema.TextDataFrame); ok && td.Text != "" {
			texts = append(texts, td.Text)
		}
	}
	if len(texts) > 1 {
		return texts
	}
	return nil
}
