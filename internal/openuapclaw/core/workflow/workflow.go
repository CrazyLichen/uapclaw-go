package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	schemautils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/common/utils/schemautils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	session "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/ability"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Workflow 工作流门面：编排组件图、会话管理、流式输出
// （Python workflow.py Workflow 一比一；_WorkflowMeta 元类装饰等价在构造函数承载）。
type Workflow struct {
	card        *WorkflowCard
	internal    *BaseWorkflow
	endCompID   string
	isStreaming bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflow 构造（Python workflow.py:106-118 __init__ + :73-95 _WorkflowMeta）。
// card nil 兜底 WorkflowCard(id=uuid4().hex)；kwargs 经选项透传。
// 元类装饰等价：invoke/stream 的 before/transform_io/after 链由方法体内
// TransformWorkflowIO 族承载（偏差 D-17-5 先例），构造期注册四事件对。
func NewWorkflow(card *WorkflowCard) *Workflow {
	if card == nil {
		card = NewWorkflowCard()
	}
	cfg, _ := NewWorkflowConfig(card)
	w := &Workflow{
		card:     card,
		internal: NewBaseWorkflow(cfg, nil),
	}
	// Python _WorkflowMeta.__call__（:74-95）：注册 invoke/stream 的事件对
	fw := callback.GetCallbackFramework()
	fw.RegisterWorkflowTransformIO(callback.WorkflowInvokeInput, callback.WorkflowInvokeOutput, nil, nil)
	fw.RegisterWorkflowTransformIO(callback.WorkflowStreamInput, callback.WorkflowStreamOutput, nil, nil)
	return w
}

// Card 元数据访问器（workflow.py:131-134）。
func (w *Workflow) Card() *WorkflowCard { return w.card }

// SetStartComp 起始组件（workflow.py:136-167；链式返回 Self）。
func (w *Workflow) SetStartComp(startCompID string, component components.ComponentComposable,
	inputsSchema, outputsSchema any, name *string) *Workflow {
	wf := false
	_ = w.internal.AddWorkflowComp(startCompID, component, &wf,
		inputsSchema, outputsSchema, nil, nil, nil, 0, -1.0, nil, name)
	_ = w.internal.StartComp(startCompID)
	return w
}

// AddWorkflowComp 组件（workflow.py:169-217；形参集一比一）。
func (w *Workflow) AddWorkflowComp(compID string, workflowComp components.ComponentComposable,
	waitForAll *bool, inputsSchema, outputsSchema, streamInputsSchema, streamOutputsSchema any,
	compAbility []components.ComponentAbility, maxRetries int, timeout float64,
	exceptionConfig *ExceptionConfig, name *string) *Workflow {
	_ = w.internal.AddWorkflowComp(compID, workflowComp, waitForAll, inputsSchema, outputsSchema,
		streamInputsSchema, streamOutputsSchema, compAbility, maxRetries, timeout, exceptionConfig, name)
	return w
}

// SetEndComp 终组件（workflow.py:219-277；response_mode→abilities 推导逐行复刻）。
func (w *Workflow) SetEndComp(endCompID string, component components.ComponentComposable,
	inputsSchema, outputsSchema, streamInputsSchema, streamOutputsSchema any,
	responseMode string, name *string) *Workflow {
	compAbility := []components.ComponentAbility{}
	if responseMode == "streaming" {
		w.isStreaming = true
		if inputsSchema != nil {
			compAbility = append(compAbility, components.ComponentAbilityStream)
		}
		if streamInputsSchema != nil {
			compAbility = append(compAbility, components.ComponentAbilityTransform)
		}
		if len(compAbility) == 0 {
			compAbility = []components.ComponentAbility{components.ComponentAbilityStream}
		}
	} else {
		compAbility = []components.ComponentAbility{components.ComponentAbilityInvoke}
		if streamInputsSchema != nil {
			compAbility = append(compAbility, components.ComponentAbilityCollect)
		}
	}
	// Python :262-263 wait_for_all = True if (COLLECT in ab or TRANSFORM in ab) else False
	waitForAll := false
	for _, ab := range compAbility {
		if ab == components.ComponentAbilityCollect || ab == components.ComponentAbilityTransform {
			waitForAll = true
			break
		}
	}
	_ = w.internal.AddWorkflowComp(endCompID, component, &waitForAll,
		inputsSchema, outputsSchema, streamInputsSchema, streamOutputsSchema,
		compAbility, 0, -1.0, nil, name)
	_ = w.internal.EndComp(endCompID)
	w.endCompID = endCompID
	return w
}

// AddConnection 常规边（workflow.py:279-293 add_connection）。
// 偏差 D-14-6：Python src_comp_id: str|list[str] → Go []string（单源包一元素）。
func (w *Workflow) AddConnection(srcCompIDs []string, targetCompID string) *Workflow {
	_ = w.internal.AddConnection(srcCompIDs, targetCompID)
	return w
}

// AddStreamConnection 流式边（workflow.py:295-309）。
func (w *Workflow) AddStreamConnection(srcCompID, targetCompID string) *Workflow {
	_ = w.internal.AddStreamConnection(srcCompID, targetCompID)
	return w
}

// AddConditionalConnection 条件边（workflow.py:311-326；src: str、router: Router 标注类型直传）。
func (w *Workflow) AddConditionalConnection(srcCompID string, router graph.Router) *Workflow {
	_ = w.internal.AddConditionalConnection(srcCompID, router)
	return w
}

// Invoke 批式执行（workflow.py:328-408 一比一；kwargs 携带 is_sub/skip_inputs_validate）。
// inputs 运行时闭集（D-17-1 收窄注记）：map[string]any（常规）|
// *interaction.InteractiveInput（交互恢复，workflow.py:747 isinstance 探测同形）；
// 两形态均在 validateInputs/图执行内显式分发，any 仅作闭集外层承载。
// Invoke 批式执行（workflow.py:328-408 一比一；kwargs 携带 is_sub/skip_inputs_validate）。
// inputs 运行时闭集（D-17-1 收窄注记）：map[string]any（常规）|
// *interaction.InteractiveInput（交互恢复，workflow.py:747 isinstance 探测同形）；
// 两形态均在 validateInputs/图执行内显式分发，any 仅作闭集外层承载。
// 返回值：顶层 = *WorkflowOutput（Python invoke 返回 WorkflowOutput）；
// is_sub = 裸 dict（Python _sub_invoke 返回 get_outputs 原始输出，workflow.py:626）。
// 修复 R-14b-2：原 is_sub 分支把裸 dict 强转 *WorkflowOutput 失败后返回 nil——
// SubWorkflowComponent（8.14b）为首个真实消费者，父侧 sub 节点输出恒空。
func (w *Workflow) Invoke(ctx context.Context, inputs any, sess sessioninterfaces.SessionFacade,
	modelContext any, kwargs map[string]any) (any, error) {
	if kwBool(kwargs, "is_sub") {
		return w.subInvoke(ctx, inputs, sess, modelContext, kwargs)
	}
	if err := w.validateSession(sess); err != nil {
		return nil, err
	}
	if err := w.validateInputs(inputs, kwargs); err != nil {
		return nil, err
	}
	// 元类装饰序（_WorkflowMeta）：emit_before(INVOKE_INPUT) → 主体 →
	// transform_io(INVOKE_OUTPUT) → emit_after 由框架在事件触发时承载
	fw := callback.GetCallbackFramework()
	inputs = fw.TransformWorkflowIOInput(ctx, callback.WorkflowInvokeInput, inputs)
	logWorkflowStart("invoke", w.card, inputs)
	ws := w.createWorkflowSession(sess, []stream.StreamMode{stream.StreamModeOutput}, false)

	result, err := w.executeWithTimeout(ctx, func(ctx context.Context) (any, error) {
		var chunks []WorkflowChunk
		// Python workflow.py:367 self._stream(inputs, workflow_session, context=context)
		ch, chErr := w.streamInternal(ctx, inputs, ws, modelContext, kwargs)
		for chunk := range ch {
			// 顶层工作流帧恒 Schema（writer 产出 OutputSchema）；any 化后断言收集
			// 保持 Result 类型闭集（is_sub 的 dict 帧只出现在嵌套路径，不经此处）
			if sc, ok := chunk.(stream.Schema); ok {
				chunks = append(chunks, sc)
			}
		}
		bgErr, ok := <-chErr
		if ok && bgErr != nil {
			return nil, bgErr // Python except BaseError: raise（workflow.py:399-408）
		}
		// workflow.py:370-377：扫 INTERACTION chunk → INPUT_REQUIRED
		// 8.24 修正（基座缺陷）：Python 扫描比较的是常量 INTERACTION="__interaction__"
		// （constant.py:13），非 WorkflowChunkType 枚举值 "interaction"——原用枚举值
		// 比对恒不命中，交互挂起被误判 COMPLETED（集成测试 workflow_questioner 实证）
		for _, chunk := range chunks {
			if sc, ok := chunk.(stream.Schema); ok &&
				sc.SchemaType() == interaction.InteractionType {
				return &WorkflowOutput{Result: chunks, State: WorkflowExecutionStateInputRequired}, nil
			}
		}
		var result any
		if w.isStreaming { // workflow.py:379-383 双结果形态
			result = chunks
		} else {
			result = workflowGetOutputs(ws, w.endCompID)
		}
		return &WorkflowOutput{Result: result, State: WorkflowExecutionStateCompleted}, nil
	}, envDuration(ws.Config(), constants.WorkflowExecuteTimeoutEnvKey))
	if err != nil {
		logWorkflowError("invoke", w.card, err, ws.SessionID())
		return nil, err
	}
	out, _ := result.(*WorkflowOutput)
	// 元类 transform_io(INVOKE_OUTPUT)（workflow.py:80-84）
	out = transformWorkflowOutput(ctx, fw, callback.WorkflowInvokeOutput, out)
	logWorkflowEnd("invoke", w.card, ws.SessionID())
	return out, nil
}

// Stream 流式执行门面（workflow.py:410-469；A' 签名：启动错误走第二返回值）。
// inputs 闭集同 Invoke（map[string]any | *interaction.InteractiveInput）。
// 返回帧元素 any（D-14b-6 延伸）：Python stream 返回 AsyncIterator 无元素约束，
// is_sub 分流的子图帧为队列原样（dict/Schema 混流）
func (w *Workflow) Stream(ctx context.Context, inputs any, sess sessioninterfaces.SessionFacade,
	modelContext any, streamModes []stream.StreamMode, kwargs map[string]any) (<-chan any, error) {
	if kwBool(kwargs, "is_sub") {
		return w.subStream(ctx, inputs, sess, modelContext, kwargs)
	}
	if err := w.validateSession(sess); err != nil {
		return nil, err
	}
	if err := w.validateInputs(inputs, kwargs); err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	inputs = fw.TransformWorkflowIOInput(ctx, callback.WorkflowStreamInput, inputs)
	logWorkflowStart("stream", w.card, inputs)
	ws := w.createWorkflowSession(sess, streamModes, false)
	out, _ := w.streamInternal(ctx, inputs, ws, modelContext, kwargs)
	return out, nil
}

// Draw mermaid 生成（workflow.py:471-499；偏差 D-d：_drawable 恒 nil，
// 复刻 Python `_drawable is None` 分支返回空串/空字节；8.13 实施时回收）。
// 偏差 D-14-7：expand_subgraph: int|bool（False/True 或展开层级）→ Go 收窄 bool
// （层级语义当前零消费方，8.13 落地若需层级改 int）。
func (w *Workflow) Draw(title, outputFormat string, expandSubgraph bool, enableAnimation bool) any {
	_ = title
	_ = expandSubgraph
	switch outputFormat {
	case "png":
		return []byte(nil) // ≙ Python return b""
	case "svg":
		return []byte(nil)
	default:
		return "" // ≙ Python return ""
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// streamInternal 内部流循环（workflow.py:501-592 步骤一比一；
// 主循环中转禁直透——spec §10：转发层承载 workflow_final 补帧/取消事件/逐帧日志）。
// context 形参对齐 Python _stream(inputs, session, context)（D-18-7 修正：原漏形参）。
func (w *Workflow) streamInternal(ctx context.Context, inputs any,
	ws sessioninterfaces.InnerSession, modelContext any, kwargs map[string]any) (<-chan any, <-chan error) {
	out := make(chan any)
	errCh := make(chan error, 1)
	go func() { // 转发协程（spec §10 注记 3；Python _stream 协程）
		defer close(errCh) // 成功路径零写入：关闭承载「无错误」语义
		defer close(out)
		// workflow.py:506-512 trace start + WORKFLOW_STARTED
		tracer.TracerWorkflowUtils{}.TraceWorkflowStart(ctx, w.tracerSession(ws), inputs)
		triggerEvent(ctx, callback.WorkflowStarted, map[string]any{
			"workflow_id": w.card.ID, "workflow_name": w.card.Name, "inputs": inputs})

		// workflow.py:514-522 超时钳制（0<timeout≤frame/first → frame/first=timeout）
		timeout := envDuration(ws.Config(), constants.WorkflowExecuteTimeoutEnvKey)
		frameTimeout := envDuration(ws.Config(), constants.WorkflowStreamFrameTimeoutEnvKey)
		if timeout > 0 && timeout <= frameTimeout {
			frameTimeout = timeout
		}
		ws.Config().SetEnvs(map[string]any{constants.WorkflowStreamFrameTimeoutEnvKey: frameTimeout.Seconds()})
		firstFrameTimeout := envDuration(ws.Config(), constants.WorkflowStreamFirstFrameTimeoutEnvKey)
		if timeout > 0 && timeout <= firstFrameTimeout {
			firstFrameTimeout = timeout
		}
		ws.Config().SetEnvs(map[string]any{constants.WorkflowStreamFirstFrameTimeoutEnvKey: firstFrameTimeout.Seconds()})

		// workflow.py:524-537 后台图执行（stream_process；spawn_background_task group="workflow"）
		task, terr := utils.CreateBackgroundTask(ctx, func(ctx context.Context) error {
			// workflow.py:525 self._internal.compile(session, context)
			compiled, err := w.internal.Compile(ws, modelContext)
			if err != nil {
				return err
			}
			defer func() {
				// workflow.py:528-532 finally：trace done + emitter close
				//（asyncio.shield → defer 清理不可取消，偏差 D-c）
				outputs := workflowGetOutputs(ws, w.endCompID)
				tracer.TracerWorkflowUtils{}.TraceWorkflowDone(ctx, w.tracerSession(ws), outputs)
				_ = ws.StreamWriterManager().StreamEmitter().Close(ctx)
			}()
			_, err = compiled.Invoke(ctx, map[string]any{graph.InputsKey: inputs}, ws)
			return err
		}, "workflow_stream_process:"+w.card.ID, "workflow")
		if terr != nil {
			triggerEvent(ctx, callback.WorkflowError, map[string]any{"error": terr})
			w.cleanup(ws)
			errCh <- terr
			return
		}

		// workflow.py:540-548 主循环消费帧（逐帧日志 + chunk_idx 计数）
		emit := ws.StreamWriterManager().StreamOutput(
			stream.WithFirstFrameTimeout(firstFrameTimeout),
			stream.WithFrameTimeout(frameTimeout))
		idx := 0
	drain:
		for {
			select {
			case <-ctx.Done(): // 主循环取消（workflow.py:567-580）
				triggerEvent(ctx, callback.WorkflowCancelled, map[string]any{
					"workflow_id": w.card.ID, "workflow_name": w.card.Name})
				loggerWarn("Canecel stream output task", w.card) // Python 拼写笔误照复刻
				if !taskDone(task) {
					_ = task.Stop(time.Second)
				}
				w.cleanup(ws)
				errCh <- ctx.Err()
				return
			case chunk, ok := <-emit:
				if !ok {
					break drain
				}
				loggerDebugChunk(idx, w.card)
				out <- chunk
				idx++
			}
		}
		// workflow.py:550-558 收尾：task.wait → WORKFLOW_FINISHED → 补 workflow_final
		if err := task.Wait(); err != nil {
			if errors.Is(err, context.Canceled) { // Wait 处取消（workflow.py:559-566）
				loggerWarn("Workflow stream output be cancelled", w.card)
				w.cleanup(ws)
				errCh <- err
				return
			}
			triggerEvent(ctx, callback.WorkflowError, map[string]any{
				"workflow_id": w.card.ID, "workflow_name": w.card.Name, "error": err})
			w.cleanup(ws)
			errCh <- err // BaseError/图执行错误回传（Python except BaseError: raise）
			return
		}
		results := workflowGetOutputs(ws, w.endCompID)
		triggerEvent(ctx, callback.WorkflowFinished, map[string]any{
			"workflow_id": w.card.ID, "workflow_name": w.card.Name, "outputs": results})
		if results != nil && !isEmptyMap(results) {
			out <- &stream.OutputSchema{ // workflow.py:557-558 workflow_final 补帧
				Type: "workflow_final", Index: 0, Payload: results,
			}
		}
		w.cleanup(ws) // workflow.py:588-592 finally：cancel 未完任务 + shield close + reset
	}()
	return out, errCh
}

// subInvoke 子工作流批式（workflow.py:594-626 一比一）。
func (w *Workflow) subInvoke(ctx context.Context, inputs any, sess sessioninterfaces.SessionFacade,
	modelContext any, kwargs map[string]any) (any, error) {
	subWS := w.createWorkflowSession(sess, nil, true)
	// Python finally :624-626 shield close + reset
	defer w.cleanup(subWS)
	compiled, err := w.internal.Compile(subWS, modelContext)
	if err != nil {
		return nil, err
	}
	// Python workflow.py:600 CONFIG_KEY: kwargs.get(CONFIG_KEY)——父图 InnerPregelConfig
	// 透传使子图 isMain=False，跳过 pre_workflow_execute 校验，由引擎自 graph_store
	// 自动恢复快照（恢复链路关键；Go 曾漏传导致子图 pre 拒绝恢复重入）
	if _, err = compiled.Invoke(ctx, map[string]any{graph.InputsKey: inputs, graph.ConfigKey: kwargs[graph.ConfigKey]}, subWS); err != nil {
		return nil, err
	}
	if w.isStreaming { // workflow.py:602-617 按 end 组件流能力数收 sub_workflow_stream
		messages := w.drainSubWorkflowStreamWithTimeout(subWS, envDuration(sessConfig(sess), constants.WorkflowExecuteTimeoutEnvKey))
		if len(messages) > 0 {
			return map[string]any{"stream": messages}, nil
		}
	}
	// workflow.py:619-623 NodeSession(sub_session, end_comp_id).state().get_outputs(output_key)
	// —— Python 以 subWS 为父建节点级视图（parent_id=subWS.executable_id），
	// get_outputs 据此前缀下钻子树（io_state 点路径键展开后，子图节点输出位于
	// subWS.executable_id 子树下，如 "composite.end"）；直接按根解引用读不到。
	if wfs, ok := subWS.State().(state.WorkflowState); ok {
		parentID := ""
		if p, ok2 := subWS.(sessioninterfaces.ExecutableIDProvider); ok2 {
			parentID = p.ExecutableID() // Python create_parent_id(sub_workflow_session)
		}
		execID := w.endCompID
		if parentID != "" {
			execID = parentID + "." + w.endCompID
		}
		ns := wfs.CreateNodeState(execID, parentID)
		if nws, ok3 := ns.(state.WorkflowState); ok3 {
			return nws.GetOutputs(w.endCompID), nil
		}
		return workflowGetOutputs(subWS, w.endCompID), nil
	}
	return workflowGetOutputs(subWS, w.endCompID), nil
}

// subStream 子工作流流式（workflow.py:628-651 一比一）。
// 帧元素 any（D-14b-6 延伸）：Python _sub_stream yield 的 messages 为队列原样帧
// （dict/OutputSchema 混流），WorkflowChunk 接口约束会迫使 drain 包装 dict 帧，
// 破坏 accumulate 的 isinstance-dict 合并语义（缓存恒空，与 Python 分叉）
func (w *Workflow) subStream(ctx context.Context, inputs any, sess sessioninterfaces.SessionFacade,
	modelContext any, kwargs map[string]any) (<-chan any, error) {
	subWS := w.createWorkflowSession(sess, nil, true)
	out := make(chan any)
	go func() {
		defer close(out)
		defer w.cleanup(subWS)
		compiled, err := w.internal.Compile(subWS, modelContext)
		if err != nil {
			return
		}
		// 同 subInvoke：CONFIG_KEY 透传（Python :633）
		if _, err = compiled.Invoke(ctx, map[string]any{graph.InputsKey: inputs, graph.ConfigKey: kwargs[graph.ConfigKey]}, subWS); err != nil {
			return
		}
		if !w.isStreaming { // Python :634 流式分支才收帧
			return
		}
		streamTimeout := envDuration(subWS.Config(), constants.WorkflowExecuteTimeoutEnvKey)
		for _, frame := range w.drainSubWorkflowStreamWithTimeout(subWS, streamTimeout) {
			out <- frame
		}
	}()
	return out, nil
}

// drainSubWorkflowStream 收子图流帧（workflow.py:607-617/640-648）：
// 按 end 组件 STREAM/TRANSFORM 能力数循环 Receive，END_ 哨兵减计数、nil continue。
// drainSubWorkflowStreamWithTimeout 带超时的收帧实现。
// 偏差 D-14-3：Python 按 END_ 哨兵减能力计数（workflow.py:607-617），Go 侧 8.12
// 已消除 END_FRAME（Emitter.Close → 队列关闭承载），故循环改为收帧直至队列关闭。
func (w *Workflow) drainSubWorkflowStreamWithTimeout(subWS sessioninterfaces.InnerSession,
	streamTimeout time.Duration) []any {
	subStreamQ, err := subWS.ActorManager().SubWorkflowStream()
	if err != nil {
		return nil
	}
	// workflow.py:602-606 按 end 组件 STREAM/TRANSFORM 能力数定哨兵计数
	var streamAbilityCount int
	if spec := w.internal.Config().Spec; spec != nil {
		if cc, ok := spec.CompConfigs[w.endCompID]; ok {
			for _, a := range cc.Abilities {
				if a == ability.ComponentAbilityStream || a == ability.ComponentAbilityTransform {
					streamAbilityCount++
				}
			}
		}
	}
	// workflow.py:607-617 一比一：frame==None → continue；frame==END_FRAME → 计数减；
	// 其余入 messages；计数归零退出（哨兵消费语义，撤销 D-14-3 close 等价）
	var messages []any
	for streamAbilityCount > 0 {
		frame, rerr := subStreamQ.Receive(context.Background(), streamTimeout)
		if rerr != nil {
			continue // Python frame is None → continue
		}
		if s, ok := frame.(string); ok && s == constants.StreamEmitterEndFrame {
			streamAbilityCount--
			continue
		}
		// Python workflow.py:617 messages.append(frame) 原样直通（dict/Schema 混流）——
		// 包装 OutputSchema 信封会破坏下游 accumulate 的 isinstance-dict 合并语义
		messages = append(messages, frame)
	}
	return messages
}

// executeWithTimeout 超时执行（workflow.py:653-694 五分支一比一）。
func (w *Workflow) executeWithTimeout(ctx context.Context, fn func(context.Context) (any, error),
	timeout time.Duration) (any, error) {
	var taskResult any
	task, terr := utils.CreateBackgroundTask(ctx, func(ctx context.Context) error {
		var err error
		taskResult, err = fn(ctx)
		return err
	}, "workflow_exec_timeout:"+w.card.ID, "workflow")
	if terr != nil {
		return nil, terr
	}
	waitCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 { // Python wait_for(..., timeout if (timeout and timeout>0) else None)
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	type result struct {
		val any
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		err := task.Wait()
		resCh <- result{err: err} // Python task.wait() 返回值经闭包结果承载（fn 内部已存）
	}()
	select {
	case <-waitCtx.Done():
		if !taskDone(task) {
			_ = task.Stop(time.Second) // finally：未完则取消（workflow.py:692-694）
		}
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			// workflow.py:671-679 TimeoutError → WORKFLOW_EXECUTION_TIMEOUT
			loggerErrorTimeout(w.card, timeout)
			return nil, exception.BuildError(exception.StatusWorkflowExecutionTimeout,
				exception.WithParam("timeout", timeout.String()),
				exception.WithParam("workflow", w.card.ID+":"+w.card.Version))
		}
		// workflow.py:663-670 CancelledError → error 日志 + 重抛
		loggerErrorCancelled(w.card)
		return nil, waitCtx.Err()
	case res := <-resCh:
		if !taskDone(task) {
			_ = task.Stop(time.Second)
		}
		if res.err == nil {
			return taskResult, nil
		}
		var re *pregel.RecursionError
		if errors.As(res.err, &re) { // workflow.py:680-689 RecursionError → EXECUTION_ERROR
			loggerErrorRecursion(w.card, res.err)
			return nil, exception.BuildError(exception.StatusWorkflowExecutionError,
				exception.WithParam("workflow", w.card.ID+":"+w.card.Version),
				exception.WithParam("reason", res.err.Error()))
		}
		return nil, res.err // BaseError 直通（workflow.py:690-691）
	}
}

// cleanup 收尾（workflow.py:588-592 finally 一比一；D-c defer 承载 shield）。
func (w *Workflow) cleanup(ws sessioninterfaces.InnerSession) {
	_ = ws.Close()
	_ = w.internal.Reset(context.Background())
}

// createWorkflowSession 会话装配（workflow.py:707-744 双分支一比一）。
func (w *Workflow) createWorkflowSession(sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode,
	isSub bool) sessioninterfaces.InnerSession {
	if !isSub {
		if cardSetter, ok := sess.(interface{ SetWorkflowCard(*WorkflowCard) }); ok {
			cardSetter.SetWorkflowCard(w.card) // workflow.py:709
		}
		var parent sessioninterfaces.InnerSession
		if p, ok := sess.(interface {
			GetParent() sessioninterfaces.InnerSession
		}); ok {
			parent = p.GetParent()
		}
		workflowSession := session.NewWorkflowSessionFacade(w.card.ID, sess.GetSessionID(), innerToInnerSession(parent))
		w.internal.AutoCompleteAbilities() // workflow.py:715
		workflowSession.Config().AddWorkflowConfig(w.card.ID, w.internal.Config())
		// workflow.py:718-720 ActorManager(sub_graph=false)
		am, err := stream_actor.NewActorManager(context.Background(), w.internal.Config().Spec,
			w.internal.streamActor, false, &actorSessionAdapter{ws: workflowSession})
		if err == nil {
			workflowSession.SetActorManager(am)
		}
		// workflow.py:721-722 StreamWriterManager 内部构造（非注入）
		workflowSession.SetStreamWriterManager(stream.NewStreamWriterManager(
			stream.NewStreamEmitter(), streamModes...))
		// workflow.py:723-731 Tracer 创建/update_trace_id
		traceID := sessTraceID(sess)
		if workflowSession.Tracer() == nil {
			tr := tracer.NewTracer()
			// 偏差 D-14-4：Go Tracer 无 SetTraceID 构造入参/UpdateTraceID 面
			//（Python Tracer(session_id, trace_id) + update_trace_id），trace_id
			// 取 NewTracer 自动 UUID；上游补面时回收
			_ = traceID
			tr.Init(workflowSession.StreamWriterManager())
			workflowSession.SetTracer(tr)
		}
		return workflowSession
	}
	// 子分支（workflow.py:732-744）：inner + sub_graph=true
	w.internal.AutoCompleteAbilities()
	inner := sessInner(sess)
	if inner == nil {
		return nil
	}
	innerSess := innerToInnerSession(inner)
	am, err := stream_actor.NewActorManager(context.Background(), w.internal.Config().Spec,
		w.internal.streamActor, true, &actorSessionAdapter{ws: workflowSessionOf(innerSess)})
	if err != nil {
		return nil
	}
	subWS := session.NewSubWorkflowSessionFacade(innerSess, w.card.ID, am)
	// Python :742-743 sub_workflow_session.config().add_workflow_config(...)
	if cfgUser, ok := subWS.(interface {
		Config() config.SessionConfig
	}); ok {
		cfgUser.Config().AddWorkflowConfig(w.card.ID, w.internal.Config())
	}
	return subWS
}

// validateSession 会话必填（workflow.py:756-760）。
func (w *Workflow) validateSession(sess sessioninterfaces.SessionFacade) error {
	if sess == nil {
		return exception.BuildError(exception.StatusWorkflowExecuteSessionInvalid,
			exception.WithParam("reason", "session is required for workflow execution"),
			exception.WithParam("workflow", w.card.ID+":"+w.card.Version))
	}
	return nil
}

// validateInputs 输入 schema 校验（workflow.py:746-754；D-5 信封层判定回收点）。
func (w *Workflow) validateInputs(inputs any, kwargs map[string]any) error {
	if w.card.InputParams == nil {
		return nil
	}
	if _, isII := inputs.(*interaction.InteractiveInput); isII {
		return nil // ≙ not isinstance(inputs, InteractiveInput) 取反
	}
	// Python SchemaUtils.format_with_schema(inputs, card.input_params,
	// skip_validate=kwargs.get("skip_inputs_validate"))（workflow.py:748-749）
	// ——共享实现 core/common/utils/schemautils（D-18-1 收口：降级等价面删除，
	// 校验行为向 Python 对齐变严）。Python 侧格式化结果仅在函数内局部复用，
	// _validate_inputs 对外只承载校验失败错误，Go 同步丢弃格式化产物。
	skipValidate := false
	if sv, ok := kwargs["skip_inputs_validate"].(bool); ok {
		skipValidate = sv
	}
	m, isMap := inputs.(map[string]any)
	if !isMap {
		// Python 非 dict 输入过 object 型 schema 必然校验失败（动态模型
		// model_validate 抛 ValidationError → SCHEMA_FORMAT_INVALID）
		return w.workflowInputInvalid(inputs, fmt.Errorf("input is not a JSON object"))
	}
	opts := []schemautils.FormatOption{}
	if skipValidate {
		opts = append(opts, schemautils.WithFormatSkipValidate(true))
	}
	if _, err := (schemautils.SchemaUtils{}).FormatWithSchemaMap(m, w.card.InputParams, opts...); err != nil {
		return w.workflowInputInvalid(inputs, err)
	}
	return nil
}

// workflowInputInvalid 输入校验失败统一错误（workflow.py:750-753 build_error，
// reason 携带底层校验错误字符串对齐 str(e)）
func (w *Workflow) workflowInputInvalid(inputs any, cause error) error {
	return exception.BuildError(exception.StatusWorkflowExecuteInputInvalid,
		exception.WithParam("inputs", fmt.Sprintf("%v", inputs)),
		exception.WithParam("reason", fmt.Sprintf("input validation failed against schema: %v", cause)),
		exception.WithParam("workflow", w.card.ID+":"+w.card.Version))
}

// taskGroupScope 任务组作用域（workflow.py:119-129；偏差 D-b：anyio 语义 →
// 有活跃 task group 复用、否则 WithTaskGroup 包裹；utils 尚无 task group 查询面，
// 以直接执行承载，回收条件=utils.TaskGroup 上下文落地）。
func (w *Workflow) taskGroupScope(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// ──────────────────────────── 小助手 ────────────────────────────

// innerToInnerSession 能力断言：inner 提供者 → InnerSession。
func innerToInnerSession(p sessioninterfaces.InnerSession) sessioninterfaces.InnerSession { return p }

// workflowSessionOf InnerSession → 门面断言（ActorSession 适配器用）。
func workflowSessionOf(p sessioninterfaces.InnerSession) *session.WorkflowSession {
	if f, ok := p.(*session.WorkflowSession); ok {
		return f
	}
	return nil
}

func loggerDebug(msg string, card *WorkflowCard, fields map[string]any) {
	l := logger.Debug(logComponent).Str("workflow_id", card.ID).Str("workflow_name", card.Name)
	for k, v := range fields {
		l = l.Str(k, fmt.Sprintf("%v", v))
	}
	l.Msg(msg)
}

func loggerWarn(msg string, card *WorkflowCard) {
	logger.Warn(logComponent).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Str("event_type", "WORKFLOW_EXECUTE_ERROR").Msg(msg)
}

func loggerErrorFields(msg string, card *WorkflowCard, fields map[string]any, err error) {
	l := logger.Error(logComponent).Err(err).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Str("event_type", "WORKFLOW_EXECUTE_ERROR")
	for k, v := range fields {
		l = l.Str(k, fmt.Sprintf("%v", v))
	}
	l.Msg(msg)
}

func loggerErrorTimeout(card *WorkflowCard, timeout time.Duration) {
	logger.Error(logComponent).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Str("event_type", "WORKFLOW_EXECUTE_ERROR").
		Str("exception", fmt.Sprintf("Workflow execution timeout %v s", timeout.Seconds())).
		Msgf("Workflow execution timeout %v s", timeout.Seconds())
}

func loggerErrorCancelled(card *WorkflowCard) {
	logger.Error(logComponent).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Str("event_type", "WORKFLOW_EXECUTE_ERROR").Msg("Workflow execution cancelled")
}

func loggerErrorRecursion(card *WorkflowCard, err error) {
	logger.Error(logComponent).Err(err).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Str("event_type", "WORKFLOW_EXECUTE_ERROR").Msg("Workflow execution recursion limit reached")
}

func loggerDebugChunk(idx int, card *WorkflowCard) {
	logger.Debug(logComponent).Str("workflow_id", card.ID).Str("workflow_name", card.Name).
		Int("chunk_idx", idx).Str("event_type", "WORKFLOW_OUTPUT_CHUNK").
		Msgf("Output workflow chunk[%d]", idx)
}

// sessConfig 会话配置提取（SessionFacade 无 Config → 能力断言）。
func sessConfig(sess sessioninterfaces.SessionFacade) config.SessionConfig {
	if c, ok := sess.(interface{ Config() config.SessionConfig }); ok {
		return c.Config()
	}
	return nil
}

func kwBool(kwargs map[string]any, key string) bool {
	if kwargs == nil {
		return false
	}
	if v, ok := kwargs[key].(bool); ok {
		return v
	}
	return false
}

func taskDone(task *utils.BackgroundTask) bool {
	select {
	case <-task.Done():
		return true
	default:
		return false
	}
}

func taskDoneChannel(task *utils.BackgroundTask) <-chan struct{} {
	return task.Done()
}

func isEmptyMap(v any) bool {
	if m, ok := v.(map[string]any); ok {
		return len(m) == 0
	}
	return false
}

func transformWorkflowOutput(ctx context.Context, fw *callback.CallbackFramework,
	event callback.WorkflowEventType, out *WorkflowOutput) *WorkflowOutput {
	if out == nil {
		return nil
	}
	if t := fw.TransformWorkflowIOOutput(ctx, event, out); t != nil {
		if o, ok := t.(*WorkflowOutput); ok {
			return o
		}
	}
	return out
}

func triggerEvent(ctx context.Context, event callback.WorkflowEventType, data map[string]any) {
	_ = callback.Trigger(ctx, string(event), data)
}

func envDuration(cfg config.SessionConfig, key string) time.Duration {
	if cfg == nil {
		return 0
	}
	v := cfg.GetEnv(key)
	switch d := v.(type) {
	case float64:
		if d > 0 {
			return time.Duration(d * float64(time.Second))
		}
	case int:
		if d > 0 {
			return time.Duration(d) * time.Second
		}
	}
	return 0
}

// sessTraceID 会话 trace id（Python hasattr(session,"get_trace_id") 探测）。
func sessTraceID(sess sessioninterfaces.SessionFacade) string {
	if p, ok := sess.(interface{ GetTraceID() string }); ok {
		return p.GetTraceID()
	}
	return ""
}

// innerSessionProvider 子会话 inner 提取（Python getattr(session, "_inner")）。
type innerSessionProvider interface {
	Inner() any
}

func sessInner(sess sessioninterfaces.SessionFacade) sessioninterfaces.InnerSession {
	if p, ok := sess.(interface {
		InnerNodeSession() sessioninterfaces.InnerSession
	}); ok {
		return p.InnerNodeSession()
	}
	return nil
}

// workflowGetOutputs 经 WorkflowState 断言取 end 组件输出（Python state().get_outputs）。
func workflowGetOutputs(ws sessioninterfaces.InnerSession, nodeID string) any {
	if ws == nil || ws.State() == nil {
		return nil
	}
	if wfs, ok := ws.State().(state.WorkflowState); ok {
		return wfs.GetOutputs(nodeID)
	}
	return nil
}

// tracerSession 构造 tracer.WorkflowNodeSession 视图（workflow 级 trace：
// ExecutableID=card.ID、ParentID 空）。
func (w *Workflow) tracerSession(ws sessioninterfaces.InnerSession) tracer.WorkflowNodeSession {
	return &workflowTracerSession{ws: ws, workflowID: w.card.ID}
}

// workflowTracerSession tracer.WorkflowNodeSession 适配器。
type workflowTracerSession struct {
	ws         sessioninterfaces.InnerSession
	workflowID string
}

func (t *workflowTracerSession) Tracer() *tracer.Tracer { return t.ws.Tracer() }
func (t *workflowTracerSession) ExecutableID() string   { return t.workflowID }
func (t *workflowTracerSession) ParentID() string       { return "" }
func (t *workflowTracerSession) WorkflowID() string     { return t.workflowID }
func (t *workflowTracerSession) NodeID() string         { return t.workflowID }
func (t *workflowTracerSession) NodeType() string       { return "workflow" }
func (t *workflowTracerSession) State() state.SessionState {
	return t.ws.State()
}
func (t *workflowTracerSession) Config() config.SessionConfig {
	return t.ws.Config()
}

// actorSessionAdapter stream_actor.ActorSession 适配器（门面 → 流调度会话面）。
type actorSessionAdapter struct{ ws *session.WorkflowSession }

func (a *actorSessionAdapter) GetEnvFloat(key string) (float64, bool) {
	if a.ws == nil || a.ws.Config() == nil {
		return 0, false
	}
	v := a.ws.Config().GetEnv(key)
	if f, ok := v.(float64); ok {
		return f, true
	}
	return 0, false
}

func (a *actorSessionAdapter) GetWorkflowStateValue(key string) any {
	if a.ws == nil || a.ws.State() == nil {
		return nil
	}
	if wfs, ok := a.ws.State().(state.WorkflowState); ok {
		return wfs.GetWorkflowState(state.StringKey(key))
	}
	return nil
}

func (a *actorSessionAdapter) UpdateAndCommitWorkflowState(data map[string]any) {
	if a.ws == nil || a.ws.State() == nil {
		return
	}
	if wfs, ok := a.ws.State().(state.WorkflowState); ok {
		wfs.UpdateAndCommitWorkflowState(data)
	}
}

// 日志助手（规则 3：Msg 英文化、结构化字段、logComponent 组件标识）。
func logWorkflowStart(op string, card *WorkflowCard, inputs any) {
	loggerDebug(op+" start", card, map[string]any{"inputs": fmt.Sprintf("%v", inputs)})
}

func logWorkflowEnd(op string, card *WorkflowCard, sessionID string) {
	loggerDebug(op+" end", card, map[string]any{"session_id": sessionID})
}

func logWorkflowError(op string, card *WorkflowCard, err error, sessionID string) {
	loggerErrorFields(op+" failed", card, map[string]any{"session_id": sessionID}, err)
}

// 编译期兜底：确认 components 面引用（防误删）
var _ components.ComponentComposable = (components.ComponentComposable)(nil)

// os 引用兜底（Draw 空分支复刻不触发 env 读取，保留 import 面给 NewBaseWorkflow）
var _ = os.Getenv
