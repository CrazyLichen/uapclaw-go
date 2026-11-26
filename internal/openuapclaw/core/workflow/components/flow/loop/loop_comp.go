package loop

import (
	"context"
	"fmt"
	"strings"

	"errors"
	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessionconstants "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	sessionutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
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
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
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
		// 编码适配：checkpoint JSON 往返 int→float64——复用 utils.PyInt（Python
		// int() 构造语义）单点（topic_audit D-6 先例收敛，禁包内私抄）
		if n, err := utils.PyInt(v); err == nil {
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
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
		return c.invoke(sess)
	}
	return c, nil
}

// GenerateValue 取变量值（loop_comp.py:291-296 一比一，静态方法收 session →
// Go 收具体 *session.NodeSessionFacade 直调 GetGlobalState 面；2026-11-22 用户
// 拍板：不为测试方便加窄接口，测试一律走真实会话链路）。
func GenerateValue(sess *session.NodeSessionFacade, value any) any {
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
		// 编码适配：checkpoint JSON 往返 int→float64——复用 utils.PyInt 单点
		// （topic_audit D-6 先例收敛，禁包内私抄）；非数值回退 0 ≙ Python None
		// 之外类型比较失败的宽容路径维持不变
		if n, err := utils.PyInt(v); err == nil {
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
	// 偏差 D-20-6：Python 该 staged 写经节点 PostCommit/commit_cmp 时序在条件求值前
	// 已对外可见（upstream test_loop_number 断言 12 轮 distinct 实证）；Go CommitState
	// staged 更新对 Get 不可见，需显式提交对齐可观测行为
	if finishIndex == index {
		_ = st.Update(map[string]any{constants.Index: index + 1})
		ws.SetOutputs(map[string]any{constants.Index: index + 1})
		if ws2, ok := st.(state.WorkflowState); ok {
			ws2.CommitCmp()
		}
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

// ──────────────────────────── LoopGroup ────────────────────────────

// LoopGroup 循环体子工作流（loop_comp.py:46 LoopGroup(BaseWorkflow, Executable) →
// Go 组合：嵌入 BaseWorkflow（构图面）+ graphexeAlias（可执行基座）+ 覆写四钩子）。
type LoopGroup struct {
	workflow.BaseWorkflow
	graphexeAlias
	// breakComponents 中断组件收集（Python self._break_components）
	breakComponents []*LoopBreakComponent
	// startNodeIDs 起始节点记录（Python self._start_nodes）
	startNodeIDs []string
	// endNodeIDs 结束节点记录（Python self._end_nodes）
	endNodeIDs []string
	// nodeIDs 全部节点记录（Python self._node_ids）
	nodeIDs []string
}

// NewLoopGroup 构造（Python __init__: super().__init__() + 空集；graph/config nil 兜底
// 由 NewBaseWorkflow 承载）。
func NewLoopGroup() *LoopGroup {
	return &LoopGroup{BaseWorkflow: *workflow.NewBaseWorkflow(nil, nil)}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AddWorkflowComp 添加循环体组件（loop_comp.py:57-85 一比一）：
// 禁嵌套 LoopComponent（101030）→ super 转发 → LoopBreakComponent 收集 →
// drawable.set_break_node（:83-84，8.13 已回填）→ node_ids 记录。
func (g *LoopGroup) AddWorkflowComp(
	compID string,
	workflowComp components.ComponentComposable,
	waitForAll *bool,
	inputsSchema, outputsSchema any,
	streamInputsSchema, streamOutputsSchema any,
	compAbility []components.ComponentAbility,
	maxRetries int,
	timeout float64,
	exceptionConfig *workflow.ExceptionConfig,
	name *string,
) error {
	// Python: if isinstance(workflow_comp, LoopComponent): raise 101030
	if lc, ok := workflowComp.(*LoopComponent); ok {
		_ = lc
		return exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
			exception.WithParam("reason", "cannot add 'LoopComponent' to a loop group."))
	}
	// Python: super().add_workflow_comp(...)
	if err := g.BaseWorkflow.AddWorkflowComp(compID, workflowComp, waitForAll, inputsSchema, outputsSchema,
		streamInputsSchema, streamOutputsSchema, compAbility, maxRetries, timeout, exceptionConfig, name); err != nil {
		return err
	}
	// Python: if isinstance(workflow_comp, LoopBreakComponent): self._break_components.append(...)
	if bc, ok := workflowComp.(*LoopBreakComponent); ok {
		g.breakComponents = append(g.breakComponents, bc)
	}
	// Python loop_comp.py:83-84：drawable 非空且组件为 LoopBreakComponent 时登记中断节点
	if g.Drawable() != nil {
		if bc, ok := workflowComp.(*LoopBreakComponent); ok {
			_ = g.Drawable().SetBreakNode(compID)
			_ = bc
		}
	}
	// Python: self._node_ids.append(comp_id)
	g.nodeIDs = append(g.nodeIDs, compID)
	return nil
}

// StartNodes 批量登记起始（loop_comp.py:87-91 一比一）。
func (g *LoopGroup) StartNodes(nodes []string) error {
	for _, node := range nodes {
		if err := g.StartComp(node); err != nil {
			return err
		}
	}
	g.startNodeIDs = nodes
	return nil
}

// StartComp 登记起始（loop_comp.py:93-98 一比一：BaseWorkflow API 直调也记录）。
func (g *LoopGroup) StartComp(startCompID string) error {
	if err := g.BaseWorkflow.StartComp(startCompID); err != nil {
		return err
	}
	for _, id := range g.startNodeIDs {
		if id == startCompID {
			return nil
		}
	}
	g.startNodeIDs = append(g.startNodeIDs, startCompID)
	return nil
}

// EndNodes 批量登记结束（loop_comp.py:100-108 一比一）。
func (g *LoopGroup) EndNodes(nodes any) error {
	if s, ok := nodes.(string); ok {
		if err := g.EndComp(s); err != nil {
			return err
		}
		g.endNodeIDs = append(g.endNodeIDs, s)
		return nil
	}
	if list, ok := nodes.([]string); ok {
		for _, node := range list {
			if err := g.EndComp(node); err != nil {
				return err
			}
		}
		g.endNodeIDs = list
		return nil
	}
	return fmt.Errorf("end_nodes expects str or list[str], got %T", nodes)
}

// EndComp 登记结束（loop_comp.py:110-115 一比一：BaseWorkflow API 直调也记录）。
func (g *LoopGroup) EndComp(endCompID string) error {
	if err := g.BaseWorkflow.EndComp(endCompID); err != nil {
		return err
	}
	for _, id := range g.endNodeIDs {
		if id == endCompID {
			return nil
		}
	}
	g.endNodeIDs = append(g.endNodeIDs, endCompID)
	return nil
}

// OnInvoke 执行循环体子工作流（loop_comp.py:117-130 一比一）。
func (g *LoopGroup) OnInvoke(ctx context.Context, inputs map[string]any, sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	// Python: self.auto_complete_abilities()
	g.AutoCompleteAbilities()
	// Python: actor_manager = ActorManager(self._workflow_spec, self._stream_actor, sub_graph=True, session=session)
	am, err := stream_actor.NewActorManager(ctx, g.Spec(), g.StreamGraph(), true, actorSessionOf(sess))
	if err != nil {
		return nil, err
	}
	// Python: loop_session = SubWorkflowSession(session.parent(), self._workflow_config.card.id, actor_manager)
	// parent() 一步到位（NodeSession.Parent() 返回父 NodeSession，勿二次解包）
	parent := parentSessionOf(sess)
	loopSession := session.NewSubWorkflowSessionFacade(parent, g.Config().Card.ID, am)
	if loopSession == nil {
		return nil, exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
			exception.WithParam("reason", "SubWorkflowSession requires parent to be a NodeSession"))
	}
	// Python: loop_session.config().add_workflow_config(workflow_id=..., workflow_config=...)
	loopSession.Config().AddWorkflowConfig(g.Config().Card.ID, g.Config())
	// Python: self.compiled_graph = self.compile(loop_session, context=kwargs.get("context"))
	compiled, err := g.Compile(loopSession, kwargs["context"])
	if err != nil {
		return nil, err
	}
	// Python: await self.compiled_graph.invoke(inputs, loop_session)
	if _, err := compiled.Invoke(ctx, inputs, loopSession); err != nil {
		return nil, err
	}
	// Python 125-129: finished_stream_nodes 清理（移除本循环体节点后写回并提交）
	g.cleanupFinishedStreamNodes(sess)
	return nil, nil // Python: return None
}

// SkipTrace 跳过埋点（loop_comp.py:132-133）。
func (g *LoopGroup) SkipTrace() bool { return true }

// GraphInvoker 子图递归入口（loop_comp.py:135-136）。
func (g *LoopGroup) GraphInvoker() bool { return true }

// BreakComponents 访问器（loop_comp.py:138-140 property break_components）。
func (g *LoopGroup) BreakComponents() []*LoopBreakComponent { return g.breakComponents }

// CheckValidate 构造期校验（loop_comp.py:142-156 一比一三段，101030 reason 逐字）。
func (g *LoopGroup) CheckValidate() error {
	// Python: if not self._start_nodes: raise "missing start_nodes in loop group"
	if len(g.startNodeIDs) == 0 {
		return exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
			exception.WithParam("reason", "missing start_nodes in loop group"))
	}
	// Python: if not self._end_nodes: raise "missing end_nodes in loop group"
	if len(g.endNodeIDs) == 0 {
		return exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
			exception.WithParam("reason", "missing end_nodes in loop group"))
	}
	// Python: nodes = self._graph.get_nodes(); if len(nodes) == 0: raise "loop group is empty..."
	if len(g.Graph().GetNodes()) == 0 {
		return exception.BuildError(exception.StatusComponentLoopGroupParamInvalid,
			exception.WithParam("reason", "loop group is empty (contains no nodes)"))
	}
	return nil
}

// ──────────────────────────── LoopComponent ────────────────────────────

// LoopComponent 循环组件装配厂（loop_comp.py:159 LoopComponent(WorkflowComponent) →
// Go 嵌 8.17 WorkflowComponent 槽位，InvokeFn 承载 invoke）。
type LoopComponent struct {
	components.WorkflowComponent
	// loopGroup 循环体（Python self._loop_group，构造期持有）
	loopGroup *LoopGroup
	// outputSchema 输出格式（Python self._output_schema，构造期持有）
	outputSchema map[string]any
}

// NewLoopComponent 构造（loop_comp.py:160-164 一比一：存引用 + check_validate）。
func NewLoopComponent(loopGroup *LoopGroup, outputSchema map[string]any) (*LoopComponent, error) {
	if err := loopGroup.CheckValidate(); err != nil {
		return nil, err
	}
	c := &LoopComponent{loopGroup: loopGroup, outputSchema: outputSchema}
	c.InvokeFn = c.invoke
	// 槽注入（基座 ToExecutable/AddComponent 已改槽优先，D-17-1 先例贯彻）：
	// Python to_executable 动态分派返回 self → 注入外层自身，保住 GraphInvoker
	// 钩子与 Vertex 信封路径（原成对 override 由基座单点收口，8.14b D-14b 同缝）。
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return c }
	return c, nil
}

// GraphInvoker 子图递归入口（loop_comp.py:222-223）。
func (c *LoopComponent) GraphInvoker() bool { return true }

// ToExecutable/AddComponent 无需 override：基座已改槽优先，构造期 ToExecutableFn
// 注入外层自身即完成 Python to_executable 动态分派语义（原成对 override 删除，
// 规则 8 收口）。

// LoopGroup 访问器（loop_comp.py:225-227 property loop_group）。
func (c *LoopComponent) LoopGroup() *LoopGroup { return c.loopGroup }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invoke 循环执行（loop_comp.py:166-220 十步一比一；D-20-1：getattr(session, "_inner")
// → InnerNodeSession()）。
func (c *LoopComponent) invoke(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
	// Python 167-220 整体 try：GraphInterrupt/BaseError 透传，其余包装 101040。
	// Go 错误分类在 defer 内承载（等价 except 链）。
	result, err := c.invokeInner(ctx, inputs, sess)
	if err == nil {
		return result, nil
	}
	// Python: except GraphInterrupt: raise / except BaseError: raise
	var gi *pregel.GraphInterrupt
	if errors.As(err, &gi) {
		return nil, err
	}
	var be *exception.BaseError
	if errors.As(err, &be) {
		return nil, err
	}
	// Python: except Exception as e: raise build_error(101040, cause=e, comp=..., reason=str(e))
	return nil, exception.BuildError(exception.StatusComponentLoopExecutionError,
		exception.WithParam("reason", err.Error()),
		exception.WithParam("comp", sess.GetComponentDescription()))
}

// invokeInner invoke 主体（Python try 块内步骤一比一）。
func (c *LoopComponent) invokeInner(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
	// Python 168-169: if not isinstance(inputs, dict): raise KeyError
	// （Go InvokeFn 入参恒 map，形状校验以 nil 防御承载）
	if inputs == nil {
		return nil, fmt.Errorf("inputs must be a dict, but got NoneType")
	}
	// Python 171-172: if INPUTS_KEY not in inputs: raise KeyError —— 8.2 D5 兑现
	raw, ok := inputs[graph.InputsKey]
	if !ok {
		return nil, fmt.Errorf("missing required key %s", graph.InputsKey)
	}
	// Python 174: loop_input = LoopInput.model_validate(inputs.get(INPUTS_KEY))
	loopInput, err := ValidateLoopInput(raw)
	if err != nil {
		return nil, err
	}
	// Python 175-200: 四态选 condition
	var cond conditionpkg.Condition
	switch loopInput.LoopType {
	case LoopTypeArray:
		// Python 177: condition = ArrayConditionInSession(loop_input.loop_array)
		ac, acErr := conditionpkg.NewArrayConditionInSession(loopInput.LoopArray)
		if acErr != nil {
			return nil, acErr
		}
		cond = ac
	case LoopTypeNumber:
		// Python 179: max_loop_limit = session.get_env(LOOP_NUMBER_MAX_LIMIT_KEY) or DEFAULT
		maxLimit := sessionconstants.LoopNumberMaxLimitDefault
		if v := sess.GetEnv(sessionconstants.LoopNumberMaxLimitKey); v != nil {
			if n, isInt := v.(int); isInt {
				maxLimit = n
			}
		}
		// Python 180-183: int(max_loop_limit) 失败回默认（Go 类型断言失败即回默认，同语义）
		// Python 185-186: if loop_input.loop_number is None: raise ValueError
		if loopInput.LoopNumber == nil {
			return nil, fmt.Errorf("loop_number variable not found or is None")
		}
		// Python 188-189: if loop_input.loop_number > max_loop_limit: raise ValueError
		if *loopInput.LoopNumber > maxLimit {
			return nil, fmt.Errorf("loop_number exceeds maximum limit %d", maxLimit)
		}
		cond = conditionpkg.NewNumberConditionInSession(*loopInput.LoopNumber)
	case LoopTypeAlwaysTrue:
		// Python 192-193: condition = AlwaysTrue()
		cond = conditionpkg.NewAlwaysTrue()
	case LoopTypeExpression:
		// Python 194-198: bool 字面量 → FuncCondition；否则 ExpressionCondition
		if b, isBool := loopInput.BoolExpression.(bool); isBool {
			cond = conditionpkg.NewFuncCondition(func() bool { return b })
		} else {
			expr, _ := loopInput.BoolExpression.(string)
			ec, ecErr := conditionpkg.NewExpressionCondition(expr)
			if ecErr != nil {
				return nil, ecErr
			}
			cond = ec
		}
	default:
		// Python 199-200: raise ValueError(f"invalid loop type '{...}' for LoopComponent")
		return nil, fmt.Errorf("invalid loop type '%s' for LoopComponent", loopInput.LoopType)
	}
	// Python 201-204: output_callback + 可选 intermediate_var callback
	cbs := []callbackpkg.LoopCallback{callbackpkg.NewOutputCallback(c.outputSchema, "", "").LoopCallback}
	if len(loopInput.IntermediateVar) > 0 {
		cbs = append(cbs, callbackpkg.NewIntermediateLoopVarCallback(loopInput.IntermediateVar, "").LoopCallback)
	}
	// Python 206-207: loop_component = AdvancedLoopComponent(loop_group, condition,
	//                break_components, callbacks)
	alc, err := NewAdvancedLoopComponent(c.loopGroup, cond, c.loopGroup.BreakComponents(), cbs)
	if err != nil {
		return nil, err
	}
	// Python 208-209: return await loop_component.on_invoke({INPUTS_KEY: {}, CONFIG_KEY: ...},
	//                getattr(session, "_inner")) —— D-20-1：_inner → InnerNodeSession()
	return alc.OnInvoke(ctx, map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: inputs[graph.ConfigKey]},
		sess.InnerNodeSession(), nil)
}

// cleanupFinishedStreamNodes finished_stream_nodes 清理（loop_comp.py:125-129 一比一）。
func (g *LoopGroup) cleanupFinishedStreamNodes(sess session.BaseSession) {
	ws, ok := sess.State().(state.WorkflowState)
	if !ok {
		return
	}
	// Python: finished_stream_nodes = session.state().get_workflow_state("finished_stream_nodes") or []
	raw := ws.GetWorkflowState(state.StringKey("finished_stream_nodes"))
	list, _ := raw.([]any)
	// Python: for node_id in self._node_ids: if node_id in finished: remove
	cleaned := make([]any, 0, len(list))
	for _, item := range list {
		s, _ := item.(string)
		removed := false
		for _, id := range g.nodeIDs {
			if s == id {
				removed = true
				break
			}
		}
		if !removed {
			cleaned = append(cleaned, item)
		}
	}
	// Python: session.state().update_and_commit_workflow_state({"finished_stream_nodes": cleaned})
	ws.UpdateAndCommitWorkflowState(map[string]any{"finished_stream_nodes": cleaned})
}

// actorSessionOf ActorSession 能力断言（Python ActorManager(session=session) 消费
// get_env/workflow_state 面；Go 以接口断言承载，不满足时 nil ≙ Python 鸭子缺省）。
func actorSessionOf(sess session.BaseSession) stream_actor.ActorSession {
	if as, ok := sess.(stream_actor.ActorSession); ok {
		return as
	}
	return nil
}

// ──────────────────────────── 8.13 D-13-5 接口实现 ────────────────────────────

// LoopDrawableSubgraph 返回循环体子图（Python drawable.py:59-60
// `component.loop_group.drawable.get_graph()`；8.13 D-13-5 接口实现，
// LoopSubgraphSource 编译期断言见下）
func (c *LoopComponent) LoopDrawableSubgraph() *visualization.DrawableGraph {
	return c.loopGroup.Drawable().GetGraph()
}

// LoopDrawableSubgraph AdvancedLoop 取循环体子图（Python drawable.py:59-60
// `component.body.drawable.get_graph()`；body 非 LoopGroup 时 nil）
func (a *AdvancedLoopComponent) LoopDrawableSubgraph() *visualization.DrawableGraph {
	if lg, ok := a.body.(*LoopGroup); ok {
		return lg.Drawable().GetGraph()
	}
	return nil
}

// 编译期断言：实现 visualization 循环子图源接口
var (
	_ visualization.LoopSubgraphSource = (*LoopComponent)(nil)
	_ visualization.LoopSubgraphSource = (*AdvancedLoopComponent)(nil)
)
