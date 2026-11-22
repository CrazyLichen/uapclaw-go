package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	sessionutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/utils"
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
type EmptyExecutable struct{}

// PostLoopBody 后置计数件（loop_comp.py:316-333 一比一）。finishIndex 实例字段
// （拍板 5：Python 就是实例字段 + resume 修正语义；字段不跨 invoke 存活正是
// _condition_invoke 修正逻辑存在的原因，不收进 state）。
type PostLoopBody struct {
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
