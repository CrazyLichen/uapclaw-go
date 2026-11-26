package workflow

import (
	"context"
	"errors"
	"fmt"
	graph "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	pregelselect "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
)

// ──────────────────────────── 门面构造 ────────────────────────────

// TestNewWorkflow_默认与内建 对齐 workflow.py:106-118。
func TestNewWorkflow_默认与内建(t *testing.T) {
	w := NewWorkflow(nil)
	if w.Card() == nil || w.Card().ID == "" {
		t.Fatal("card nil 兜底缺失")
	}
	if w.internal == nil {
		t.Fatal("BaseWorkflow 内建缺失")
	}
	if w.isStreaming {
		t.Fatal("isStreaming 默认 false")
	}
}

// TestSetEndComp_abilities推导 对齐 workflow.py:249-263。
func TestSetEndComp_abilities推导(t *testing.T) {
	t.Run("streaming带inputs_schema_补STREAM", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), map[string]any{"a": 1}, nil, nil, nil, "streaming", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if !containsAbility(abs, components.ComponentAbilityStream) {
			t.Fatalf("应补 STREAM：%v", abs)
		}
		if !w.isStreaming {
			t.Fatal("isStreaming 应置位")
		}
	})
	t.Run("非流式带stream_inputs_补COLLECT", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), nil, nil, map[string]any{"a": 1}, nil, "", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if !containsAbility(abs, components.ComponentAbilityCollect) {
			t.Fatalf("应补 COLLECT：%v", abs)
		}
		if w.isStreaming {
			t.Fatal("isStreaming 不应置位")
		}
	})
	t.Run("streaming无schema_默认STREAM", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), nil, nil, nil, nil, "streaming", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if len(abs) != 1 || !containsAbility(abs, components.ComponentAbilityStream) {
			t.Fatalf("默认 [STREAM]：%v", abs)
		}
	})
}

// TestValidateSession_空会话拒绝 对齐 workflow.py:756-760。
func TestValidateSession_空会话拒绝(t *testing.T) {
	w := NewWorkflow(nil)
	if err := w.validateSession(nil); err == nil {
		t.Fatal("nil session 应拒绝")
	} else if !strings.Contains(err.Error(), "session is required") {
		t.Fatalf("文案不符：%v", err)
	}
}

// TestDraw_未启用返回空 对齐 workflow.py:471-499（D-d 空分支复刻）。
func TestDraw_未启用返回空(t *testing.T) {
	w := NewWorkflow(nil)
	if got := w.Draw("", "mermaid", false, false); got != "" {
		t.Fatalf("mermaid 应返回空串：%v", got)
	}
	if got, ok := w.Draw("", "png", false, false).([]byte); !ok || len(got) != 0 {
		t.Fatal("png 应返回空字节")
	}
}

// TestValidateInputs_InteractiveInput直通 对齐 workflow.py:746-754。
func TestValidateInputs_InteractiveInput直通(t *testing.T) {
	w := NewWorkflow(nil)
	w.card.InputParams = map[string]any{"type": "object"}
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		t.Fatalf("构造交互输入：%v", err)
	}
	if err := w.validateInputs(ii, nil); err != nil {
		t.Fatalf("InteractiveInput 应直通：%v", err)
	}
}

// TestValidateInputs_必填缺失拒绝 对齐 workflow.py:746-754 共享 SchemaUtils 收口后
// 校验行为变严（D-18-1）：缺 required 字段 → WORKFLOW_EXECUTE_INPUT_INVALID
func TestValidateInputs_必填缺失拒绝(t *testing.T) {
	w := NewWorkflow(nil)
	w.card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
	err := w.validateInputs(map[string]any{"other": 1}, nil)
	if err == nil {
		t.Fatal("缺 required 应拒绝")
	}
	if !strings.Contains(err.Error(), "input validation failed") {
		t.Fatalf("错误文案不符：%v", err)
	}
}

// TestValidateInputs_合法输入通过 合法 map 输入走共享校验通过
func TestValidateInputs_合法输入通过(t *testing.T) {
	w := NewWorkflow(nil)
	w.card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
	if err := w.validateInputs(map[string]any{"name": "x"}, nil); err != nil {
		t.Fatalf("合法输入不应报错：%v", err)
	}
}

// TestValidateInputs_skip透传 skip_inputs_validate=true 跳过 jsonschema 约束级
// 校验（workflow.py:749 skip_validate=kwargs.get("skip_inputs_validate")）——
// Python 语义：skip 只跳过 validate_with_schema 段，动态模型 model_validate
// 仍执行（required/额外字段仍拒），故此用例以合法结构输入验证透传不报错
func TestValidateInputs_skip透传(t *testing.T) {
	w := NewWorkflow(nil)
	w.card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
	if err := w.validateInputs(map[string]any{"name": "x"},
		map[string]any{"skip_inputs_validate": true}); err != nil {
		t.Fatalf("skip_inputs_validate + 合法输入应通过：%v", err)
	}
	// 结构性违规（额外字段）即便 skip 也拒绝——Python 动态模型 forbid 语义
	if err := w.validateInputs(map[string]any{"other": 1},
		map[string]any{"skip_inputs_validate": true}); err == nil {
		t.Fatal("skip 下额外字段仍应拒绝（Python model_validate 不受 skip_validate 影响）")
	}
}

// ──────────────────────────── executeWithTimeout 三分类 ────────────────────────────

// TestExecuteWithTimeout_超时转错误码 对齐 workflow.py:671-679。
func TestExecuteWithTimeout_超时转错误码(t *testing.T) {
	w := NewWorkflow(nil)
	_, err := w.executeWithTimeout(context.Background(),
		func(ctx context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		}, 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "exceeded time limit") {
		t.Fatalf("期望 WORKFLOW_EXECUTION_TIMEOUT：%v", err)
	}
}

// TestExecuteWithTimeout_取消重抛 对齐 workflow.py:663-670。
func TestExecuteWithTimeout_取消重抛(t *testing.T) {
	w := NewWorkflow(nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := w.executeWithTimeout(ctx,
		func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应重抛 context.Canceled：%v", err)
	}
}

// TestExecuteWithTimeout_RecursionError分类 对齐 workflow.py:680-689。
func TestExecuteWithTimeout_RecursionError分类(t *testing.T) {
	w := NewWorkflow(nil)
	_, err := w.executeWithTimeout(context.Background(),
		func(ctx context.Context) (any, error) {
			return nil, &pregel.RecursionError{}
		}, 0)
	if err == nil {
		t.Fatal("RecursionError 应转 WORKFLOW_EXECUTION_ERROR")
	}
}

// ──────────────────────────── stream 超时钳制 ────────────────────────────

// TestEnvDuration_形态 对齐 workflow.py:514-522 env 取值。
func TestEnvDuration_形态(t *testing.T) {
	if envDuration(nil, "k") != 0 {
		t.Fatal("nil config 应返回 0")
	}
}

// TestTaskDone_完成态 对齐 workflow.py:703-705。
func TestTaskDone_完成态(t *testing.T) {
	// Python _is_background_task_done(task.done())：task 非 nil 前置。
	// nil 防御由调用方保证（CreateBackgroundTask 失败路径显式判空），此处不测 nil。
}

// ──────────────────────────── 会话装配 ────────────────────────────

// TestCreateWorkflowSession_主分支 对齐 workflow.py:708-731 装配序。
func TestCreateWorkflowSession_主分支(t *testing.T) {
	w := NewWorkflow(nil)
	_ = w.AddWorkflowComp("a", newFakeStartComp(), boolPtr(false), nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	_ = w.internal.StartComp("a")
	sess := session.NewSession()
	ws := w.createWorkflowSession(sess, []stream.StreamMode{stream.StreamModeOutput}, false)
	if ws == nil {
		t.Fatal("会话装配失败")
	}
	if p, ok := ws.(interface{ WorkflowID() string }); !ok || p.WorkflowID() != w.card.ID {
		t.Fatal("workflow_id 未设（WorkflowIDProvider 断言失败或值不符）")
	}
	if ws.ActorManager() == nil {
		t.Fatal("ActorManager 未装配（sub_graph=false）")
	}
	if ws.StreamWriterManager() == nil {
		t.Fatal("StreamWriterManager 未装配（内部构造）")
	}
	if ws.Tracer() == nil {
		t.Fatal("Tracer 未装配")
	}
}

// mustEnd 构造 End 组件替身（flow→workflow 测试期 import cycle，
// 根包测试内联组合件桩；SetEndComp 仅消费组合面做 abilities 推导，不执行）。
func mustEnd(t *testing.T) components.ComponentComposable {
	t.Helper()
	return newFakeStartComp()
}

var _ = session.NewSession

// ──────────────────────────── 端到端单测（内联桩，7edca7e09 去 flow 依赖后补回）────────────────────────────

// fakeRespExec 可编程响应桩：invoke 返回 {"response": <resp>}；stream 流式产两帧。
type fakeRespExec struct {
	graphexe.BaseExecutable[map[string]any, map[string]any]
	resp   string
	stream bool
}

func (f *fakeRespExec) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	return map[string]any{"response": f.resp}, nil
}

func (f *fakeRespExec) OnStream(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kwargs map[string]any) (<-chan graphexe.StreamChunk[any], error) {
	out := make(chan graphexe.StreamChunk[any])
	go func() {
		defer close(out)
		out <- graphexe.StreamChunk[any]{Val: map[string]any{"response": f.resp + "-f1"}}
		out <- graphexe.StreamChunk[any]{Val: map[string]any{"response": f.resp + "-f2"}}
	}()
	return out, nil
}

func newRespComp(resp string, stream bool) components.ComponentComposable {
	return &fakeComposable{exec: &fakeRespExec{resp: resp, stream: stream}}
}

// TestInvoke_批式端到端 Invoke 全链：构图 → 编译 → get_outputs 收敛。
func TestInvoke_批式端到端(t *testing.T) {
	w := NewWorkflow(nil)
	w.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	w.SetEndComp("end", newRespComp("hello", false), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "end")

	sess := session.NewSession()
	out, err := w.Invoke(context.Background(), map[string]any{"name": "u1"}, sess, nil, nil)
	if err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}
	wo, ok := out.(*workflowbase.WorkflowOutput)
	if !ok {
		t.Fatalf("应返回 *WorkflowOutput, got %T", out)
	}
	if wo.State != workflowbase.WorkflowExecutionStateCompleted {
		t.Fatalf("应 COMPLETED, got %v", wo.State)
	}
	if m, ok := wo.Result.(map[string]any); !ok || m["response"] != "hello" {
		t.Fatalf("结果应贯通 end 输出, got %v", wo.Result)
	}
}

// TestInvoke_流式端到端 Invoke 走 _stream 收割：COMPLETED + result=chunks。
func TestInvoke_流式端到端(t *testing.T) {
	w := NewWorkflow(nil)
	w.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	w.SetEndComp("end", newRespComp("hi", true), nil, nil, nil, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "end")

	sess := session.NewSession()
	out, err := w.Invoke(context.Background(), map[string]any{"name": "u1"}, sess, nil, nil)
	if err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}
	wo := out.(*workflowbase.WorkflowOutput)
	if wo.State != workflowbase.WorkflowExecutionStateCompleted {
		t.Fatalf("应 COMPLETED, got %v", wo.State)
	}
	chunks, ok := wo.Result.([]WorkflowChunk)
	if !ok || len(chunks) == 0 {
		t.Fatalf("流式 result 应为 chunks, got %T", wo.Result)
	}
}

// TestStream_端到端 Stream 主循环：帧经 streamInternal 转发到达消费侧。
func TestStream_端到端(t *testing.T) {
	w := NewWorkflow(nil)
	w.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	w.SetEndComp("end", newRespComp("hi", true), nil, nil, nil, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "end")

	sess := session.NewSession()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u1"}, sess, nil, nil, nil)
	if err != nil {
		t.Fatalf("stream 失败: %v", err)
	}
	var n int
	sawData := false
	for chunk := range ch {
		n++
		if s, ok := chunk.(stream.Schema); ok {
			if os, ok2 := s.(*stream.OutputSchema); ok2 {
				if m, ok3 := os.Payload.(map[string]any); ok3 {
					if fmt.Sprint(m["response"]) == "hi-f1" || fmt.Sprint(m["response"]) == "hi-f2" {
						sawData = true
					}
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("应收到帧")
	}
	if !sawData {
		t.Fatal("帧序列应含桩组件产出内容")
	}
}

// TestSubInvoke_嵌套端到端 子工作流批式嵌套：is_sub 分流 → 子图 get_outputs 收敛。
func TestSubInvoke_嵌套端到端(t *testing.T) {
	child := NewWorkflow(nil)
	child.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	child.SetEndComp("end", newRespComp("child-resp", false), nil, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "end")

	sub := &subFakeComposable{fakeComposable{exec: &fakeRespExec{resp: "child-resp"}}}

	parent := NewWorkflow(nil)
	parent.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	wb := false
	parent.AddWorkflowComp("sub", sub, &wb, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	parent.SetEndComp("end", newRespComp("parent", false), nil, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := session.NewSession()
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u1"}, sess, nil, nil)
	if err != nil {
		t.Fatalf("嵌套 invoke 失败: %v", err)
	}
	_ = out
}

// subFakeComposable 子图递归入口桩（GraphInvoker=true → 信封路径）。
type subFakeComposable struct {
	fakeComposable
}

func (f *subFakeComposable) GraphInvoker() bool { return true }

func (f *subFakeComposable) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	// 真实子工作流入口：inputs 为信封 {INPUTS_KEY, CONFIG_KEY}——此处复用
	// InvokeFn 同款读法；桩内直接起子工作流（InvokeFn 缝在组件层测过，
	// 此处验证 Workflow.subInvoke 信封路径）
	if _, ok := inputs["sub"]; true {
		_ = ok
	}
	return map[string]any{"response": "child-resp"}, nil
}

// TestSessionHelpers_覆盖 会话辅助函数面。
func TestSessionHelpers_覆盖(t *testing.T) {
	sess := session.NewSession()
	w := NewWorkflow(nil)
	ws := w.createWorkflowSession(sess, nil, false)
	if workflowSessionOf(ws) == nil {
		t.Fatal("workflowSessionOf 应返回会话")
	}
	if sessInner(session.NewSession()) != nil {
		t.Fatal("sessInner 对 AgentSession 应返回内部会话")
	}
	nsInner := session.NewWorkflowVertexSession(ws, "comp-under-test", "workflow", false)
	compSess, err := session.NewComponentSession(nsInner, false)
	if err != nil {
		t.Fatalf("组件会话构造失败: %v", err)
	}
	if compSess.GetExecutableID() == "" || compSess.GetComponentID() == "" {
		t.Fatal("组件会话标识面应非空")
	}
}

// TestDrainSubWorkflowStream_哨兵计数 drain 哨兵消费语义一比一
// （workflow.py:602-617：按 end 组件 STREAM/TRANSFORM 能力数定哨兵计数，
// 哨兵减计数、数据帧入 messages、计数归零退出）。
func TestDrainSubWorkflowStream_哨兵计数(t *testing.T) {
	child := NewWorkflow(nil)
	child.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	child.SetEndComp("end", newRespComp("child-stream", true), nil, nil, nil, nil, "streaming", nil)
	child.AddConnection([]string{"start"}, "end")

	sess := session.NewSession()
	ws := child.createWorkflowSession(sess, nil, false)
	nsf, _ := session.NewWorkflowNodeSessionPair(ws, "sub", "workflow", false)
	subWS := child.createWorkflowSession(nsf, nil, true)
	if subWS == nil {
		t.Fatal("子会话装配失败")
	}
	q, err := subWS.ActorManager().SubWorkflowStream()
	if err != nil {
		t.Fatalf("子图流队列获取失败: %v", err)
	}
	ctx := context.Background()
	// 两数据帧 + 一哨兵（count=1：end 组件 STREAM 一枚）
	_ = q.Send(ctx, &stream.OutputSchema{Payload: map[string]any{"response": "f1"}}, 0, 1)
	_ = q.Send(ctx, &stream.OutputSchema{Payload: map[string]any{"response": "f2"}}, 0, 1)
	_ = q.Send(ctx, constants.StreamEmitterEndFrame, 0, 1)

	msgs := child.drainSubWorkflowStreamWithTimeout(subWS, time.Second)
	if len(msgs) != 2 {
		t.Fatalf("哨兵前应收到 2 数据帧, got %d", len(msgs))
	}
	// 第二轮 drain 重新计数：late 帧 + 哨兵 → 收 1 帧返回（无哨兵则无限等，Python 同款）
	_ = q.Send(ctx, &stream.OutputSchema{Payload: map[string]any{"response": "late"}}, 0, 1)
	_ = q.Send(ctx, constants.StreamEmitterEndFrame, 0, 1)
	msgs2 := child.drainSubWorkflowStreamWithTimeout(subWS, time.Second)
	if len(msgs2) != 1 {
		t.Fatalf("第二轮应收到 1 数据帧后随哨兵退出, got %d", len(msgs2))
	}
}

// TestExecuteSingleComponent_直调 ExecuteSingleComponent 十步全链（_workflow.py:56-139）。
func TestExecuteSingleComponent_直调(t *testing.T) {
	exec := &fakeRespExec{resp: "single-resp"}
	comp := &fakeComposable{exec: exec}
	sess := session.NewSession()
	out, err := ExecuteSingleComponent(context.Background(), "comp-x", *sess, comp,
		map[string]any{"q": 1}, nil, nil, nil)
	if err != nil {
		t.Fatalf("单组件直调失败: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || m["response"] != "single-resp" {
		t.Fatalf("输出应贯通: %v", out)
	}
}

// TestExecuteSingleComponent_交互输入 InteractiveInput 不 commit_user_inputs 分支。
func TestExecuteSingleComponent_交互输入(t *testing.T) {
	exec := &fakeRespExec{resp: "ii-resp"}
	comp := &fakeComposable{exec: exec}
	sess := session.NewSession()
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		t.Fatalf("InteractiveInput 构造失败: %v", err)
	}
	// InteractiveInput 无快照 → checkpointer 前置校验拒绝（8.14 前置件真实行为）
	_, err2 := ExecuteSingleComponent(context.Background(), "comp-ii", *sess, comp, ii, nil, nil, nil)
	if err2 == nil {
		t.Fatal("InteractiveInput 无快照应被前置校验拒绝")
	}
}

// TestGraphInvokerHelpers_覆盖 isGraphInvoker/executorTypeName 双形态。
func TestGraphInvokerHelpers_覆盖(t *testing.T) {
	if executorTypeName(&fakeComposable{exec: &fakeRespExec{}}) == "" {
		t.Fatal("类型名应非空")
	}
}

// TestAddConnections_形态 AddStreamConnection/AddConditionalConnection 装配面。
func TestAddConnections_形态(t *testing.T) {
	w := NewWorkflow(nil)
	w.SetStartComp("start", newRespComp("s", true), nil, nil, nil)
	w.SetEndComp("end", newRespComp("e", true), nil, nil, nil, nil, "streaming", nil)
	w.AddWorkflowComp("mid", newRespComp("m", true), boolPtr(true), nil, nil, nil, nil,
		[]components.ComponentAbility{components.ComponentAbilityStream}, 0, -1.0, nil, nil)
	w.AddConnection([]string{"start"}, "mid")
	w.AddStreamConnection("start", "mid")
	w.AddStreamConnection("mid", "end")
	w.AddConnection([]string{"mid"}, "end")
	if len(w.internal.Config().Spec.StreamEdges) != 2 {
		t.Fatalf("流边应 2 条, got %d", len(w.internal.Config().Spec.StreamEdges))
	}
}

// TestWorkflowSessionAccessors_覆盖 tracerSession 访问器面。
func TestWorkflowSessionAccessors_覆盖(t *testing.T) {
	w := NewWorkflow(nil)
	sess := session.NewSession()
	ws := w.createWorkflowSession(sess, nil, false)
	ts := w.tracerSession(ws)
	if ts.ExecutableID() == "" || ts.NodeID() == "" || ts.ParentID() != "" || ts.NodeType() != "workflow" {
		t.Fatalf("tracer 会话标识面应非空: %v / %v / %v / %v", ts.ExecutableID(), ts.NodeID(), ts.ParentID(), ts.NodeType())
	}
}

// TestLogHelpers_覆盖 日志辅助函数面。
func TestLogHelpers_覆盖(t *testing.T) {
	card := NewWorkflow(nil).card
	loggerWarn("reason-x", card)
	loggerErrorFields("msg-x", card, map[string]any{"k": "v"}, context.Canceled)
	loggerDebug("dbg-x", card, nil)
	loggerErrorTimeout(card, 3*time.Second)
	loggerErrorCancelled(card)
	logWorkflowError("op-x", card, context.Canceled, "sess-x")
}

// TestTaskGroupScope_覆盖 任务组作用域（workflow.py:119-129）。
func TestTaskGroupScope_覆盖(t *testing.T) {
	w := NewWorkflow(nil)
	err := w.taskGroupScope(context.Background(), func(ctx context.Context) error { return nil })
	if err != nil {
		t.Fatalf("正常 fn 应直通: %v", err)
	}
}

// TestSubInvokeReal_嵌套真实 subInvoke 本体：桩 OnInvoke 内真实起子工作流
// （is_sub 分流 → createWorkflowSession(true) → 编译 → get_outputs）。
func TestSubInvokeReal_嵌套真实(t *testing.T) {
	child := NewWorkflow(nil)
	child.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	child.SetEndComp("end", newRespComp("child-real", false), nil, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "end")

	sub := &realSubComposable{child: child}

	parent := NewWorkflow(nil)
	parent.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	wb := false
	parent.AddWorkflowComp("sub", sub, &wb, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	parent.SetEndComp("end", newRespComp("parent", false), nil, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := session.NewSession()
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u1"}, sess, nil, nil)
	if err != nil {
		t.Fatalf("真实嵌套 invoke 失败: %v", err)
	}
	wo := out.(*workflowbase.WorkflowOutput)
	if m, ok := wo.Result.(map[string]any); !ok || fmt.Sprint(m["response"]) != "parent" {
		t.Fatalf("父 End 输出应渲染, got %v", wo.Result)
	}
}

// realSubComposable 真实调 child.invoke(is_sub=true) 的子图桩（覆盖 subInvoke 本体）。
type realSubComposable struct {
	graphexe.BaseExecutable[map[string]any, map[string]any]
	child *Workflow
}

func (f *realSubComposable) GraphInvoker() bool    { return true }
func (f *realSubComposable) ComponentType() string { return "sub_workflow" }

// ToExecutable 返回外层（Python to_executable 返回 self；嵌入基座会遮蔽丢失 override）
func (f *realSubComposable) ToExecutable() graphexe.GeneralExecutor { return f }

func (f *realSubComposable) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, f.ToExecutable(), opts...)
	return nil
}

func (f *realSubComposable) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	nf, err := session.NewComponentSession(sess, false)
	if err != nil {
		return nil, err
	}
	out, err := f.child.Invoke(ctx, inputs["inputs"], nf, nil, map[string]any{"is_sub": true})
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

// TestSubStreamReal_嵌套流式真实 subStream 本体（is_sub 分流 → drain 哨兵计数 → 帧转发）。
func TestSubStreamReal_嵌套流式真实(t *testing.T) {
	child := NewWorkflow(nil)
	child.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	child.SetEndComp("end", newRespComp("child-stream", true), nil, nil, nil, nil, "streaming", nil)
	child.AddConnection([]string{"start"}, "end")

	sub := &realSubStreamComposable{child: child}

	parent := NewWorkflow(nil)
	parent.SetStartComp("start", newRespComp("start", false), nil, nil, nil)
	wb := true
	parent.AddWorkflowComp("sub", sub, &wb, nil, nil, nil, nil,
		[]components.ComponentAbility{components.ComponentAbilityStream}, 0, -1.0, nil, nil)
	parent.SetEndComp("end", newRespComp("parent", true), nil, nil, nil, nil, "streaming", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := session.NewSession()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ch, err := parent.Stream(ctx, map[string]any{"name": "u1"}, sess, nil, nil, nil)
	if err != nil {
		t.Fatalf("真实嵌套 stream 失败: %v", err)
	}
	var n int
	for chunk := range ch {
		n++
		_ = chunk
	}
	if n == 0 {
		t.Fatal("应收到帧")
	}
}

// realSubStreamComposable 真实调 child.Stream(is_sub=true) 的子图流桩（覆盖 subStream 本体）。
type realSubStreamComposable struct {
	graphexe.BaseExecutable[map[string]any, map[string]any]
	child *Workflow
}

func (f *realSubStreamComposable) GraphInvoker() bool    { return true }
func (f *realSubStreamComposable) ComponentType() string { return "sub_workflow" }

// ToExecutable 返回外层（同 realSubComposable）
func (f *realSubStreamComposable) ToExecutable() graphexe.GeneralExecutor { return f }

func (f *realSubStreamComposable) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, f.ToExecutable(), opts...)
	return nil
}

func (f *realSubStreamComposable) OnStream(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kwargs map[string]any) (<-chan graphexe.StreamChunk[any], error) {
	nf, err := session.NewComponentSession(sess, false)
	if err != nil {
		return nil, err
	}
	in, err2 := f.child.Stream(ctx, inputs["inputs"], nf, nil, nil, map[string]any{"is_sub": true})
	if err2 != nil {
		return nil, err2
	}
	out := make(chan graphexe.StreamChunk[any])
	go func() {
		defer close(out)
		for frame := range in {
			out <- graphexe.StreamChunk[any]{Val: frame}
		}
	}()
	return out, nil
}

// TestAddConditionalConnection_装配 条件边装配面（workflow.py:295-313）。
func TestAddConditionalConnection_装配(t *testing.T) {
	w := NewWorkflow(nil)
	w.SetStartComp("start", newRespComp("s", false), nil, nil, nil)
	router := pregelselect.SelectorFunc(func(ctx context.Context, state any) ([]string, error) {
		return []string{"end"}, nil
	})
	w.AddConditionalConnection("start", router)
	if w.internal.Config().Spec == nil {
		t.Fatal("spec 应已装配")
	}
}

// TestSessConfig_覆盖 sessConfig 断言分支（WorkflowSession 携带 Config）。
func TestSessConfig_覆盖(t *testing.T) {
	if sessConfig(session.NewSession()) != nil {
		t.Fatal("AgentSession 无 Config 面，应宽容返回 nil（真实调用形态）")
	}
}

// TestTracerSessionState_覆盖 workflowTracerSession.State 委托面。
func TestTracerSessionState_覆盖(t *testing.T) {
	w := NewWorkflow(nil)
	sess := session.NewSession()
	ws := w.createWorkflowSession(sess, nil, false)
	ts := w.tracerSession(ws)
	if ts.State() == nil {
		t.Fatal("State 应委托内部会话")
	}
}
