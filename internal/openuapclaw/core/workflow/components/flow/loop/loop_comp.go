package loop

import (
	"context"
	"fmt"
	"strings"

	"errors"
	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	sessionutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	conditionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
	callbackpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow/loop/callback"
)

// ──────────────────────────── 常量 ────────────────────────────

// Broken 循环中断 state 键（loop_comp.py:38 BROKEN = "_broken" 逐字）
const Broken = "_broken"

// FirstInLoopStateKey 首轮标记 state 键（loop_comp.py:39 FIRST_IN_LOOP = "_first_in_loop"
// 逐字；Python 侧常量定义后零消费，复刻留档）
const FirstInLoopStateKey = "_first_in_loop"

// ConditionNodeID 条件节点 ID（loop_comp.py:41）
const ConditionNodeID = "condition"

// BodyNodeID 循环体节点 ID（loop_comp.py:42）
const BodyNodeID = "body"

// PostBodyNodeID 后置计数节点 ID（loop_comp.py:43）
const PostBodyNodeID = "post_body"

// LoopType 循环类型（loop_comp.py:477-481 str Enum 四值）
const (
	// LoopTypeArray 数组循环
	LoopTypeArray = "array"
	// LoopTypeNumber 数字循环
	LoopTypeNumber = "number"
	// LoopTypeAlwaysTrue 无限循环（至 break/中断）
	LoopTypeAlwaysTrue = "always_true"
	// LoopTypeExpression 表达式循环
	LoopTypeExpression = "expression"
)

// state 键（Python constant.py:20-22 经 common/constants 承载）
var (
	finishKey = constants.FinishIndex
	loopIDKey = constants.LoopID
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopInput 循环输入（loop_comp.py:484-489 pydantic → Go 结构体，偏差 D-20-5：
// Optional Field 默认值语义由 ValidateLoopInput 承载）。
type LoopInput struct {
	// LoopType 循环类型（loop_type，Optional 默认 ""）
	LoopType string `json:"loop_type"`
	// LoopNumber 循环次数（loop_number，Optional 默认 0；nil ≙ None）
	LoopNumber *int `json:"loop_number"`
	// LoopArray 数组映射（loop_array，default_factory dict）
	LoopArray map[string]any `json:"loop_array"`
	// BoolExpression 布尔表达式（bool_expression，str|bool Union → any 槽，D-20-4 同源）
	BoolExpression any `json:"bool_expression"`
	// IntermediateVar 中间变量映射（intermediate_var，default_factory dict）
	IntermediateVar map[string]any `json:"intermediate_var"`
}

// LoopController 循环控制契约（loop_comp.py:230-237 ABC 一比一）。
type LoopController interface {
	// BreakLoop 触发中断（Python break_loop 抽象方法）
	BreakLoop()
	// IsBroken 是否已中断（Python is_broken 抽象方法）
	IsBroken() bool
}

// LoopBreakComponent 循环中断组件（loop_comp.py:240-256；Python 继承 WorkflowComponent
// → Go 嵌 8.17 WorkflowComponent 槽位，InvokeFn 承载 invoke）。
type LoopBreakComponent struct {
	components.WorkflowComponent
	// loopController 循环控制器（Python self._loop_controller，构造期 nil）
	loopController LoopController
}

// EmptyExecutable 空可执行体（loop_comp.py:308-313 一比一：condition 节点占位，
// 路由语义由 AdvancedLoopComponent 自身承载）。
type EmptyExecutable struct {
	graphexeAlias
}

// PostLoopBody 后置计数件（loop_comp.py:316-333 一比一）。finishIndex 实例字段
// （拍板 5：Python 就是实例字段 + resume 修正语义；字段不跨 invoke 存活正是
// _condition_invoke 修正逻辑存在的原因，不收进 state）。
type PostLoopBody struct {
	graphexeAlias
	finishIndex int
}

// LoopSetVariableComponent 循环设变量组件（loop_comp.py:259-305；死代码复刻拍板 1：
// Python 仓内零调用方，仅 __init__.py 导出，仍一比一）。
type LoopSetVariableComponent struct {
	components.WorkflowComponent
	// variableMapping 变量映射（Python self._variable_mapping）
	variableMapping map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateLoopInput 校验循环输入（Python LoopInput.model_validate 语义对齐，D-20-5）。
func ValidateLoopInput(raw any) (*LoopInput, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("loop inputs must be a dict, but got %T", raw)
	}
	li := &LoopInput{LoopArray: map[string]any{}, IntermediateVar: map[string]any{}}
	if v, ok := m["loop_type"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return nil, fmt.Errorf("loop_type must be a str, but got %T", v)
		}
		li.LoopType = s
	}
	if v, ok := m["loop_number"]; ok && v != nil {
		n, isNum := v.(int)
		if !isNum {
			return nil, fmt.Errorf("loop_number must be an int, but got %T", v)
		}
		li.LoopNumber = &n
	}
	if v, ok := m["loop_array"]; ok && v != nil {
		d, isMap := v.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("loop_array must be a dict, but got %T", v)
		}
		li.LoopArray = d
	}
	if v, ok := m["bool_expression"]; ok && v != nil {
		switch v.(type) {
		case string, bool:
			li.BoolExpression = v
		default:
			return nil, fmt.Errorf("bool_expression must be str or bool, but got %T", v)
		}
	}
	if v, ok := m["intermediate_var"]; ok && v != nil {
		d, isMap := v.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("intermediate_var must be a dict, but got %T", v)
		}
		li.IntermediateVar = d
	}
	return li, nil
}

// NewLoopBreakComponent 构造（Python __init__: controller = None；invoke 四步对齐
// loop_comp.py:248-256：controller 未初始化 → 101042，否则 BreakLoop() 返回 {}）。
func NewLoopBreakComponent() *LoopBreakComponent {
	c := &LoopBreakComponent{}
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		// Python: if self._loop_controller is None: raise 101042
		if c.loopController == nil {
			return nil, exception.BuildError(exception.StatusComponentLoopBreakExecutionError,
				exception.WithParam("reason", "failed to initialize loop controller"),
				exception.WithParam("comp", sess.GetComponentDescription()))
		}
		// Python: self._loop_controller.break_loop(); return {}
		c.loopController.BreakLoop()
		return map[string]any{}, nil
	}
	return c
}

// SetController 注入控制器（loop_comp.py:245-246 一比一）。
func (c *LoopBreakComponent) SetController(ctrl LoopController) { c.loopController = ctrl }

// NewEmptyExecutable 构造。
func NewEmptyExecutable() *EmptyExecutable { return &EmptyExecutable{} }

// OnInvoke 空执行（loop_comp.py:309-310：pass）。
func (e *EmptyExecutable) OnInvoke(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// SkipTrace 跳过埋点（loop_comp.py:312-313）。
func (e *EmptyExecutable) SkipTrace() bool { return true }

// NewPostLoopBody 构造（Python __init__: self._finish_index = -1）。
func NewPostLoopBody() *PostLoopBody { return &PostLoopBody{finishIndex: -1} }

// OnInvoke 后置计数（loop_comp.py:320-327 一比一）：
// state 读 FINISH_INDEX → 非 nil 同步实例字段 → +1 → update → commit → 返回 nil。
func (p *PostLoopBody) OnInvoke(_ context.Context, _ map[string]any, sess session.BaseSession, _ map[string]any) (map[string]any, error) {
	ws, ok := sess.State().(state.WorkflowState)
	if !ok {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is not support commit state"))
	}
	// Python: finish_index = session.state().get(FINISH_INDEX)
	if v := sess.State().Get(state.StringKey(finishKey)); v != nil {
		if n, isInt := v.(int); isInt {
			p.finishIndex = n
		}
	}
	// Python: self._finish_index += 1
	p.finishIndex++
	// Python: session.state().update({FINISH_INDEX: self._finish_index})
	if err := sess.State().Update(map[string]any{finishKey: p.finishIndex}); err != nil {
		return nil, err
	}
	// Python: session.state().commit()
	ws.Commit()
	return nil, nil
}

// SkipTrace 跳过埋点（loop_comp.py:329-330）。
func (p *PostLoopBody) SkipTrace() bool { return true }

// GetFinishIndex 读计数（loop_comp.py:332-333）。
func (p *PostLoopBody) GetFinishIndex() int { return p.finishIndex }

// SetFinishIndex 写计数（loop_comp.py:335-336）。
func (p *PostLoopBody) SetFinishIndex(v int) { p.finishIndex = v }

// NewLoopSetVariableComponent 构造（loop_comp.py:259-268 一比一：mapping 空 →
// 101031，error_msg 逐字）。
func NewLoopSetVariableComponent(variableMapping map[string]any) (*LoopSetVariableComponent, error) {
	if len(variableMapping) == 0 {
		return nil, exception.BuildError(exception.StatusComponentLoopSetVarParamInvalid,
			exception.WithParam("reason", "variable_mapping is None or empty"))
	}
	c := &LoopSetVariableComponent{variableMapping: variableMapping}
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		return c.invoke(sess)
	}
	return c, nil
}

// globalStateGetter get_global_state 面（Python session.get_global_state；
// NodeSessionFacade 天然满足，测试以最小替身承载）。
type globalStateGetter interface {
	GetGlobalState(key state.StateKey) (any, error)
}

// GenerateValue 取变量值（loop_comp.py:291-296 一比一）：引用路径 → get_global_state
// 解引用；否则原值。
func GenerateValue(sess globalStateGetter, value any) any {
	if s, ok := value.(string); ok && sessionutils.IsRefPath(s) {
		// Python: session.get_global_state(extract_origin_key(value))
		v, err := sess.GetGlobalState(state.StringKey(sessionutils.ExtractOriginKey(s)))
		if err != nil {
			return nil
		}
		return v
	}
	return value
}

// GenerateOutput 逆序嵌套组装输出（loop_comp.py:298-305 一比一）。
func GenerateOutput(keys []string, value any) any {
	output := value
	for i := len(keys) - 1; i >= 0; i-- {
		output = map[string]any{keys[i]: output}
	}
	return output
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invoke 设变量执行（loop_comp.py:270-289 一比一；D-20-1：_inner.parent() →
// InnerNodeSession().Parent()）。
func (c *LoopSetVariableComponent) invoke(sess *session.NodeSessionFacade) (map[string]any, error) {
	// Python: root_session = getattr(session, "_inner").parent()
	// 偏差 D-20-1：_inner → InnerNodeSession()；.parent() → ParentProvider 断言
	inner := sess.InnerNodeSession()
	root := inner
	if pp, ok := inner.(sessioninterfaces.ParentProvider); ok {
		root = pp.Parent()
	}
	for left, right := range c.variableMapping {
		// Python: left_ref_str = extract_origin_key(left); keys = split(NESTED_PATH_SPLIT)
		keys := splitRefPath(sessionutils.ExtractOriginKey(left))
		// Python: if len(keys) == 0: raise 101043
		if len(keys) == 0 {
			return nil, exception.BuildError(exception.StatusComponentLoopSetVarExecutionError,
				exception.WithParam("comp", sess.GetComponentDescription()),
				exception.WithParam("reason", fmt.Sprintf("key[%v] not supported format", left)))
		}
		// Python: node_session = NodeSession(root_session, node_id)
		// （node_type=None/skip_trace=False 默认 → Go ""/false）
		nodeSession := session.NewWorkflowVertexSession(root, keys[0], "", false)
		// Python: node_session.state().set_outputs(generate_output(keys[1:], generate_value(session, right)))
		if ws, wsOk := nodeSession.State().(state.WorkflowState); wsOk {
			out := GenerateOutput(keys[1:], GenerateValue(sess, right))
			if m, isMap := out.(map[string]any); isMap {
				ws.SetOutputs(m)
			}
		}
	}
	return nil, nil // Python: return None
}

// splitRefPath 引用路径切段（Python split(NESTED_PATH_SPLIT)）。
func splitRefPath(s string) []string {
	return strings.Split(s, sessionutils.NestedPathSplit)
}

// ──────────────────────────── AdvancedLoopComponent ────────────────────────────

// parentIDProvider parent_id 面（Python session.parent_id()；无 Provider 接口，
// 结构断言承载，tracer/workflow.go:26 同款登记）
type parentIDProvider interface {
	ParentID() string
}

// AdvancedLoopComponent 循环执行体（loop_comp.py:339 五重继承 ComponentComposable/
// LoopController/Executable/AtomicNode → Go 组合：嵌 AtomicNode + 实现
// graph.GeneralExecutor + pregel.Selector + LoopController）。
type AdvancedLoopComponent struct {
	graph.AtomicNode
	// body 循环体（Python self._body，LoopGroup）
	body graph.GeneralExecutor
	// postBody 后置计数（Python self._post_body）
	postBody *PostLoopBody
	// condition 循环条件（Python self._condition）
	condition conditionpkg.Condition
	// callbacks 四阶段回调（Python self._callbacks）
	callbacks []callbackpkg.LoopCallback
	// g 循环小图（Python self._graph）
	g *graph.PregelGraph
	// inLoop 继续循环路由目标（Python self._in_loop = [BODY_NODE_ID]）
	inLoop []string
	// outLoop 跳出路由目标（Python self._out_loop = [END]）
	outLoop []string
	// nodeID 循环节点 ID（Python self._node_id）
	nodeID string
	// nodeSession 循环节点会话（Python self._node_session）
	nodeSession session.BaseSession
}

// NewAdvancedLoopComponent 构造（loop_comp.py:341-380 一比一）。
// condition 四态归一：nil→AlwaysTrue / Condition 直用 / func()→FuncCondition /
// str→ExpressionCondition（偏差 D-20-4 同源：Union → any 分派）。
func NewAdvancedLoopComponent(body graph.GeneralExecutor, cond any,
	breakNodes []*LoopBreakComponent, cbs []callbackpkg.LoopCallback) (*AdvancedLoopComponent, error) {
	a := &AdvancedLoopComponent{
		body:     body,
		postBody: NewPostLoopBody(),
		inLoop:   []string{BodyNodeID},
		outLoop:  []string{pregel.End},
	}
	// Python: if condition is None → AlwaysTrue; Condition 直用;
	// Callable → FuncCondition; str → ExpressionCondition
	switch c := cond.(type) {
	case nil:
		a.condition = conditionpkg.NewAlwaysTrue()
	case conditionpkg.Condition:
		a.condition = c
	case func() bool:
		a.condition = conditionpkg.NewFuncCondition(c)
	case string:
		ec, err := conditionpkg.NewExpressionCondition(c)
		if err != nil {
			return nil, err
		}
		a.condition = ec
	default:
		return nil, fmt.Errorf("invalid condition type %T for AdvancedLoopComponent", cond)
	}
	// Python: if break_nodes: for bn: bn.set_controller(self)
	for _, bn := range breakNodes {
		bn.SetController(a)
	}
	// Python: if callbacks: for cb: self.register_callback(cb)
	a.callbacks = append(a.callbacks, cbs...)

	// Python: self._graph = PregelGraph(); add_node×3; add_edge×3;
	// add_conditional_edges(CONDITION, self)
	a.g = graph.NewPregelGraph()
	a.g.AddNode(BodyNodeID, body)
	a.g.AddNode(ConditionNodeID, NewEmptyExecutable())
	a.g.AddNode(PostBodyNodeID, a.postBody)
	a.g.AddEdge([]string{pregel.Start}, ConditionNodeID)
	a.g.AddEdge([]string{BodyNodeID}, PostBodyNodeID)
	a.g.AddEdge([]string{PostBodyNodeID}, ConditionNodeID)
	a.g.AddConditionalEdges(ConditionNodeID, a)

	// Python: __call__ → atomic_invoke 走 AtomicNode 链（构造回调槽）
	a.AtomicNode = graph.NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		// Python _atomic_invoke（loop_comp.py:388-400）
		sess, _ := kwargs["session"].(session.BaseSession)
		outputs, err := a.conditionInvoke(sess)
		if err != nil {
			// Python: if isinstance(e, BaseError): raise; else build_error 101041
			return nil, a.wrapConditionError(err)
		}
		return outputs, nil
	})
	return a, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Select 条件节点路由（Python __call__(session) → atomic_invoke → []string；
// 实现 graph.Router ≙ pregel.Selector，state 恒 nil（8.19 BranchRouter 惯例））。
func (a *AdvancedLoopComponent) Select(ctx context.Context, _ any) ([]string, error) {
	res, err := a.AtomicInvoke(ctx, map[string]any{"session": a.nodeSession})
	if err != nil {
		return nil, err
	}
	targets, _ := res.([]string)
	return targets, nil
}

// OnInvoke 执行循环（loop_comp.py:448-467 一比一；D-20-1：node_id/executable_id 走
// ExecutableIDProvider 断言、parent_id 走 parentIDProvider 断言）。
func (a *AdvancedLoopComponent) OnInvoke(ctx context.Context, inputs map[string]any, sess session.BaseSession, _ map[string]any) (map[string]any, error) {
	// Python: loop_session = session; self._node_id = loop_session.node_id()
	a.nodeID = executableIDOf(sess)
	// Python: self._node_session = NodeSession(loop_session, self._node_id)
	a.nodeSession = session.NewWorkflowVertexSession(sess, a.nodeID, "", false)
	loopSess := sess

	// Python: loop_session.state().set_outputs({LOOP_ID: self._node_id})
	ws, ok := loopSess.State().(state.WorkflowState)
	if !ok {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is not support commit state"))
	}
	ws.SetOutputs(map[string]any{loopIDKey: a.nodeID})

	// Python 454-458：io_state 取 state（按 parent_id 取子命名空间）→ 删除本节点残留
	// → set_outputs(清理后全量)（Go 等价：GetState io 快照 → 删 → SetOutputs）
	ioSnapshot := a.ioStateSnapshot(ws, sess)
	ws.SetOutputs(ioSnapshot)
	// Python: loop_session.state().commit()
	ws.Commit()

	// Python: if loop_session.tracer() is not None:
	//             tracer.register_workflow_span_manager(loop_session.executable_id())
	if tr := loopSess.Tracer(); tr != nil {
		tr.RegisterWorkflowSpanManager(executableIDOf(loopSess))
	}
	// Python: compiled = self._graph.compile(loop_session, **kwargs)
	compiled := a.g.Compile(loopSess, nil)
	// Python: await compiled.invoke(inputs, loop_session)
	if _, err := compiled.Invoke(ctx, inputs, loopSess); err != nil {
		return nil, err
	}
	// Python: result = self._node_session.state().get_outputs(self._node_id)
	result := a.nodeSession.State().(state.WorkflowState).GetOutputs(a.nodeID)
	// Python: loop_session.state()._io_state.update_by_id(self._node_id, {self._node_id: None})
	if ub, ok := ws.(interface {
		UpdateByID(nodeID string, data map[string]any) error
	}); ok {
		if err := ub.UpdateByID(a.nodeID, map[string]any{a.nodeID: nil}); err != nil {
			return nil, err
		}
	}
	out, _ := result.(map[string]any)
	return out, nil
}

// SkipTrace 跳过埋点（loop_comp.py 无覆写 → Executable 默认 false；Python
// AdvancedLoopComponent 未定义 skip_trace → 基类默认 False）。

// IsBroken 是否已中断（loop_comp.py:439-443 一比一）。
func (a *AdvancedLoopComponent) IsBroken() bool {
	v := a.nodeSession.State().Get(state.StringKey(Broken))
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// BreakLoop 触发中断（loop_comp.py:445-446 一比一）。
func (a *AdvancedLoopComponent) BreakLoop() {
	_ = a.nodeSession.State().Update(map[string]any{Broken: true})
}

// Body 访问器（loop_comp.py:472-474）。
func (a *AdvancedLoopComponent) Body() graph.GeneralExecutor { return a.body }

// ──────────────────────────── 非导出函数 ────────────────────────────

// conditionInvoke 条件求值与路由（loop_comp.py:402-437 一比一）。
func (a *AdvancedLoopComponent) conditionInvoke(sess session.BaseSession) ([]string, error) {
	st := sess.State()
	ws, _ := st.(state.WorkflowState)
	// Python: index = session.state().get(INDEX)
	index := 0
	if v := st.Get(state.StringKey(constants.Index)); v != nil {
		if n, ok := v.(int); ok {
			index = n
		}
	} else {
		// Python: index is None → {BROKEN:False, INDEX:0} update + set_outputs + commit
		_ = st.Update(map[string]any{Broken: false, constants.Index: 0})
		ws.SetOutputs(map[string]any{constants.Index: 0})
		ws.Commit()
		index = 0
	}

	// Python: finish_index = self._post_body.get_finish_index()
	finishIndex := a.postBody.GetFinishIndex()
	// Python: if finish_index + 1 < index or finish_index > index:  # resume from checkpoint
	//             finish_index = index - 1
	if finishIndex+1 < index || finishIndex > index {
		finishIndex = index - 1
	}
	// Python: if finish_index == index: {INDEX: index+1} update + set_outputs（无 commit）
	if finishIndex == index {
		_ = st.Update(map[string]any{constants.Index: index + 1})
		ws.SetOutputs(map[string]any{constants.Index: index + 1})
	}

	// Python: continue_loop = False if self.is_broken() else self._condition(session=session)
	continueLoop := false
	if !a.IsBroken() {
		flag, err := a.condition.Call(context.Background(), sess)
		if err != nil {
			return nil, err
		}
		continueLoop = flag
	}
	// Python: for callback in self._callbacks: 四时机分发
	for i := range a.callbacks {
		cb := &a.callbacks[i]
		lt := index + 1
		if finishIndex < 0 {
			if err := cb.Call(context.Background(), callbackpkg.StageFirstLoop, sess, nil); err != nil {
				return nil, err
			}
		} else if finishIndex == index {
			if err := cb.Call(context.Background(), callbackpkg.StageEndRound, sess, &lt); err != nil {
				return nil, err
			}
		}
		if continueLoop {
			if err := cb.Call(context.Background(), callbackpkg.StageStartRound, sess, nil); err != nil {
				return nil, err
			}
		} else {
			if err := cb.Call(context.Background(), callbackpkg.StageOutLoop, sess, nil); err != nil {
				return nil, err
			}
		}
	}
	// Python: if not continue_loop: 复位（INDEX=None/BROKEN=False/post_body=-1/父 POST_BODY=None）
	if !continueLoop {
		_ = st.Update(map[string]any{constants.Index: nil, Broken: false})
		a.postBody.SetFinishIndex(-1)
		if parent := parentSessionOf(sess); parent != nil {
			_ = parent.State().Update(map[string]any{PostBodyNodeID: nil})
		}
		ws.SetOutputs(map[string]any{constants.Index: nil})
	}
	// Python: return self._in_loop if continue_loop else self._out_loop
	if continueLoop {
		return a.inLoop, nil
	}
	return a.outLoop, nil
}

// wrapConditionError 非 BaseError 包装 101041（loop_comp.py:392-400 一比一）。
func (a *AdvancedLoopComponent) wrapConditionError(err error) error {
	var se *exception.BaseError
	if errors.As(err, &se) {
		return err // Python: isinstance(e, BaseError): raise
	}
	return exception.BuildError(exception.StatusComponentLoopConditionExecutionError,
		exception.WithParam("reason", err.Error()),
		exception.WithParam("comp", a.nodeID))
}

// ioStateSnapshot io_state 快照清理（loop_comp.py:454-458 Go 等价）。
func (a *AdvancedLoopComponent) ioStateSnapshot(ws state.WorkflowState, sess session.BaseSession) map[string]any {
	gs, ok := ws.(interface{ GetState() map[string]any })
	if !ok {
		return nil
	}
	st := gs.GetState()
	raw, _ := st[state.IOStateKey].(map[string]any)
	if raw == nil {
		return nil
	}
	io := raw
	if pp, ok := sess.(parentIDProvider); ok {
		if pid := pp.ParentID(); pid != "" {
			if sub, ok := raw[pid].(map[string]any); ok {
				io = sub
			}
		}
	}
	// Python: if state and self._node_id in state: del state[self._node_id]
	if _, exists := io[a.nodeID]; exists {
		copied := make(map[string]any, len(io))
		for k, v := range io {
			if k == a.nodeID {
				continue
			}
			copied[k] = v
		}
		return copied
	}
	return io
}

// executableIDOf 取节点 ID（Python node_id()/executable_id() → ExecutableIDProvider 断言）。
func executableIDOf(sess session.BaseSession) string {
	if ep, ok := sess.(sessioninterfaces.ExecutableIDProvider); ok {
		return ep.ExecutableID()
	}
	return ""
}

// parentSessionOf 取父会话（Python session.parent() → ParentProvider 断言）。
func parentSessionOf(sess session.BaseSession) session.BaseSession {
	if pp, ok := sess.(sessioninterfaces.ParentProvider); ok {
		return pp.Parent()
	}
	return nil
}

// nodeStateOf 节点状态断言（Python state() 返回 CommitState；Go 断言 WorkflowState，
// 失败返回 nil 由调用方 Get/Update 的 nil-safe 使用承载——Python 鸭子类型等价）。
func nodeStateOf(sess session.BaseSession) state.WorkflowState {
	if sess == nil {
		return nil
	}
	ws, _ := sess.State().(state.WorkflowState)
	return ws
}

// graphexeAlias GeneralExecutor 基座别名（executable.BaseExecutable[dict,dict]）
type graphexeAlias = graphexe.BaseExecutable[map[string]any, map[string]any]
