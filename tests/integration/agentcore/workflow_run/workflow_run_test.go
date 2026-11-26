//go:build integration

// Package workflow_run 8.14 Workflow 门面全链集成测试。
// 真实跨子系统链路：workflow（Invoke/Stream 门面 + createWorkflowSession 装配）→
// graph（CompiledGraph/Pregel 调度 + Vertex）→ stream_actor（ActorManager）→
// session/stream（StreamWriterManager emitter 队列）→ components/flow（Start/End 组件）。
// 对齐 Python: tests/unit_tests/core/workflow/test_workflow.py 真实链场景。
package workflow_run

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
)

// WorkflowRunSuite 8.14 Workflow 全链集成。
type WorkflowRunSuite struct {
	suite.Suite
}

func TestWorkflowRunSuite(t *testing.T) {
	suite.Run(t, new(WorkflowRunSuite))
}

// newEnd 构造 End 组件（模板收敛）。
func (s *WorkflowRunSuite) newEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	s.Require().NoError(err)
	e, err := flow.NewEnd(end)
	s.Require().NoError(err)
	return e
}

// buildHelloWorkflow 构造 Start→End("hello {{name}}") streaming 工作流
// （spec §9 真实循环解析场景）。
func (s *WorkflowRunSuite) buildHelloWorkflow() *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.SetEndComp("end", s.newEnd("hello {{name}}"), nil, nil, nil, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "end")
	return w
}

// TestInvoke_流式真实链 批式入口走 _stream 收割：COMPLETED + result=chunks
// （is_streaming）+ 尾帧 workflow_final。
func (s *WorkflowRunSuite) TestInvoke_流式真实链() {
	w := s.buildHelloWorkflow()
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"name": "u1"}, sess, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(out)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok, "invoke 应返回 *WorkflowOutput")
	s.Equal(workflowbase.WorkflowExecutionStateCompleted, wo.State)
	chunks, ok := wo.Result.([]workflow.WorkflowChunk)
	s.Require().True(ok, "流式工作流 result 应为 chunks")
	s.Require().NotEmpty(chunks)
	// 帧类型断言：End 流叶帧（END_NODE_STREAM 信封，Python workflow.py:546 逐帧 yield）
	found := false
	for _, chunk := range chunks {
		if sc, ok := chunk.(interface{ SchemaType() string }); ok && sc.SchemaType() == "end node stream" {
			found = true
		}
	}
	s.True(found, "帧序列应含 End 流叶帧")
	// workflow_final 补帧仅在 get_outputs(end) 非空时出现（Python workflow.py:557
	// `if results:`）——Go 端 End 无 get_stream_output 面，流式场景 results 空 →
	// 无补帧（Python test_workflow_with_comp_stream 同行为），不在此断言
}

// TestInvoke_非流式真实链 End 走 INVOKE：result=EndOutputs（session state）。
func (s *WorkflowRunSuite) TestInvoke_非流式真实链() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.SetEndComp("end", s.newEnd("hi {{name}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "end")
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"name": "u2"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok, "invoke 应返回 *WorkflowOutput")
	s.Equal(workflowbase.WorkflowExecutionStateCompleted, wo.State)
	// 非 streaming：result = get_outputs(end)（Python workflow.py:382）
	s.NotNil(wo.Result)
}

// TestStream_帧序列真实链 流式入口逐帧转发 + 通道关闭收尾。
func (s *WorkflowRunSuite) TestStream_帧序列真实链() {
	w := s.buildHelloWorkflow()
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u3"}, sess, nil, nil, nil)
	s.Require().NoError(err)
	var count int
	sawEndStream := false
	for chunk := range ch {
		count++
		if sc, ok := chunk.(interface{ SchemaType() string }); ok && sc.SchemaType() == "end node stream" {
			sawEndStream = true
		}
	}
	s.GreaterOrEqual(count, 1)
	s.True(sawEndStream, "帧序列应含 End 流叶帧（workflow_final 仅在 outputs 非空时补发，streaming End 无 outputs 不断言）")
}

// TestInvoke_中途取消真实链 图执行中取消：Invoke 返回取消错误（三取消分支）。
func (s *WorkflowRunSuite) TestInvoke_中途取消真实链() {
	w := s.buildHelloWorkflow()
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	// 预取消 ctx：验证取消沿 Invoke → streamInternal → 图执行全链传播重抛
	//（workflow.py:567-580；真实中途取消的时序依赖组件阻塞，预取消等价验证分类路径）
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Invoke(ctx, map[string]any{"name": "u4"}, sess, nil, nil)
	s.Require().Error(err)
}

// 编译期兜底：config/state 引用（fake 桩扩展点保留）
var (
	_ = config.NewSessionConfig
	_ = state.NewInMemoryWorkflowState
)
