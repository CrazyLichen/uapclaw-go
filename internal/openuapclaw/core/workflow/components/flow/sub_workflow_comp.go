package flow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubWorkflowStreamState 子工作流组件的流式结果缓存状态
// （Python workflow_comp.py:22-63 SubWorkflowStreamState 一比一）。
// 并发约定：Accumulate 仅由 stream 生产方调用、BuildFinalResult 由通道耗尽后的
// Vertex postStream / invoke 短路检查调用，通道序自带 happens-before，不加锁
// （spec 决策 #3；定义复用+并发 Invoke 的缓存混淆为已知限制 D-14b-3）。
type SubWorkflowStreamState struct {
	// accumulatedOutputs 累积的流帧（Python _accumulated_outputs）
	accumulatedOutputs []any
}

// SubWorkflowOption 子工作流组件构造选项（Python keyword-only 参数的 Go 惯用法承载，D-14b-4）
type SubWorkflowOption func(*SubWorkflowComponent)

// SubWorkflowComponent 子工作流嵌套组件（Python workflow_comp.py:66-96 一比一）。
// 嵌入 WorkflowComponent：执行面经 InvokeFn/StreamFn 槽、包装序由基类
// OnInvoke/OnStream 承担；GraphInvoker/ComponentType 经方法提升遮蔽 override
// （深度 1 > 嵌入基座的深度 2，≙ Python 方法覆写）。
type SubWorkflowComponent struct {
	components.WorkflowComponent
	// subWorkflow 内嵌子工作流（Python _sub_workflow）
	subWorkflow *workflow.Workflow
	// cacheStream 是否缓存流式结果（Python _cache_stream）
	cacheStream bool
	// streamState 流式累积缓存（Python _stream_state；组件实例字段，
	// 生存期跨单次调用——中断恢复读到部分缓存即 Quirk B 的机制，D-14b-2）
	streamState SubWorkflowStreamState

	// subWorkflowInvokeFn 子工作流批式调用缝（生产=闭包转 subWorkflow.Invoke；测试可注入 fake）
	subWorkflowInvokeFn func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (any, error)
	// subWorkflowStreamFn 子工作流流式调用缝（生产=闭包转 subWorkflow.Stream）
	subWorkflowStreamFn func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, streamModes []stream.StreamMode,
		kwargs map[string]any) (<-chan any, error)
}

// ──────────────────────────── 常量 ────────────────────────────

// SubWorkflowComponentType 子工作流组件类型标签（Python SUB_WORKFLOW_COMPONENT）
const SubWorkflowComponentType = "sub_workflow"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubWorkflowComponent 构造子工作流组件（Python __init__ :66-75）。
// subWorkflow 为 nil → StatusComponentSubWorkflowParamInvalid 复刻 build_error 文案。
func NewSubWorkflowComponent(subWorkflow *workflow.Workflow, opts ...SubWorkflowOption) (*SubWorkflowComponent, error) {
	return newSubWorkflowComponent(subWorkflow, opts...)
}

// WithCacheStream 是否缓存流式结果供批式取用（Python cache_stream=False keyword-only）
func WithCacheStream(enabled bool) SubWorkflowOption {
	return func(c *SubWorkflowComponent) { c.cacheStream = enabled }
}

// GraphInvoker 子图递归入口标识（Python :87-88，恒 true；
// _workflow.py:109 据此给 Vertex 递 InnerPregelConfig 形成 ns 嵌套）
func (c *SubWorkflowComponent) GraphInvoker() bool { return true }

// ToExecutable/AddComponent 无需 override：基座已改槽优先，构造期 ToExecutableFn
// 注入外层自身即完成 Python to_executable 动态分派语义（原成对 override 及其
// "静态解析丢失钩子"注释由基座单点收口删除，规则 8；2026-11-22 8.20 收口）。

// ComponentType 组件类型标签（Python :90-91）
func (c *SubWorkflowComponent) ComponentType() string { return SubWorkflowComponentType }

// SubWorkflow 暴露内嵌子工作流（Python sub_workflow property）
func (c *SubWorkflowComponent) SubWorkflow() *workflow.Workflow { return c.subWorkflow }

// GetStreamOutput 批式侧取缓存结果（Python get_stream_output :81-85；
// Vertex postStream 接口断言 vertex.go:1487 自动接通写 SetOutputs）
func (c *SubWorkflowComponent) GetStreamOutput() map[string]any {
	if !c.cacheStream {
		return nil
	}
	merged, err := c.streamState.BuildFinalResult()
	if err != nil {
		// Python build_final_result 对异类型抛 TypeError 会中断 postStream；
		// Go 防御收敛为 nil（异常路径不顶替正常执行链），
		// 合并错误在 invoke 短路检查路径可传播给调用方。
		return nil
	}
	return merged
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSubWorkflowComponent 内部构造（测试缝经 subWorkflowInvokeFn/subWorkflowStreamFn 注入）
func newSubWorkflowComponent(subWorkflow *workflow.Workflow, opts ...SubWorkflowOption) (*SubWorkflowComponent, error) {
	if subWorkflow == nil {
		return nil, exception.BuildError(exception.StatusComponentSubWorkflowParamInvalid,
			exception.WithParam("reason", "sub_workflow is None"))
	}
	c := &SubWorkflowComponent{subWorkflow: subWorkflow}
	for _, opt := range opts {
		opt(c)
	}
	// 槽注入（基座 ToExecutable/AddComponent 已改槽优先，D-17-1 先例贯彻）：
	// Python to_executable 动态分派返回 self → 注入外层自身（原成对 override
	// 由基座单点收口，规则 8）。
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return c }
	// 生产调用缝（Python invoke :79-83 直接调 self._sub_workflow.invoke 的等价承载）
	c.subWorkflowInvokeFn = func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (any, error) {
		return subWorkflow.Invoke(ctx, inputs, sess, modelContext, kwargs)
	}
	// 帧元素 any（D-14b-6 延伸）：Python stream 返回队列原样帧（dict/Schema 混流）
	c.subWorkflowStreamFn = func(ctx context.Context, inputs any,
		sess sessioninterfaces.SessionFacade, modelContext any, streamModes []stream.StreamMode,
		kwargs map[string]any) (<-chan any, error) {
		return subWorkflow.Stream(ctx, inputs, sess, modelContext, streamModes, kwargs)
	}
	// InvokeFn 槽绑定（Python invoke :78-84；Quirk A 空缓存短路逐字复刻，D-14b-1 已知行为）
	c.InvokeFn = func(ctx context.Context, inputs map[string]any,
		sess *session.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
		return c.invokeSubWorkflow(ctx, inputs, sess)
	}
	// StreamFn 槽绑定（Python stream :76-81：转调子工作流流式 + cache_stream 逐帧累积；
	// 帧信封 {"frame": <stream.Schema>} 为 Go map 槽承载惯例，End 同款，D-17-1 延伸）
	c.StreamFn = func(ctx context.Context, inputs map[string]any,
		sess *session.NodeSessionFacade, _ iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
		subCh, err := c.subWorkflowStreamFn(ctx, inputs[graph.InputsKey], sess, nil,
			nil, map[string]any{graph.ConfigKey: inputs[graph.ConfigKey], "is_sub": true})
		if err != nil {
			return nil, err
		}
		out := make(chan graphexe.StreamChunk[any])
		go func() {
			defer close(out)
			for frame := range subCh {
				// Python stream :77-78 accumulate(value) / yield value 均为队列原样帧——
				// dict 帧进缓存（isinstance-dict 合并），Schema 帧/信封帧被过滤；
				// 转发原样直通：子图 end-sub 分支已还原 payload dict（流边路径提取可用，
				// D-14b-6 历史：{"frame": schema} 信封会使路径提取 nil 且 END 帧
				// sourcePathMap 空不投 EndFrame → StreamReader 永不关 → transform 挂死）
				if c.cacheStream {
					c.streamState.Accumulate(frame)
				}
				out <- graphexe.StreamChunk[any]{Val: frame}
			}
		}()
		return out, nil
	}
	return c, nil
}

// invokeSubWorkflow 批式调用逻辑（Python invoke :78-84 步骤一比一）：
// cache_stream 开 → get_stream_output 恒非 nil（空累积返回 {} 非 None）→
// 直接回 {'output': cached} 短路，不重跑子工作流——Python workflow_comp.py:81-84
// 真实行为逐字复刻（含空缓存 `{}` 短路效应：cache_stream 开启时批入腿不执行
// 子工作流；用户拍板 2026-11-21「对齐按上游真实实现」，撤销此前修复决定，
// D-14b-1 由偏差改登记为已知行为复刻）；cache_stream 关 → 恒转调。
func (c *SubWorkflowComponent) invokeSubWorkflow(ctx context.Context, inputs map[string]any,
	sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	if c.cacheStream {
		merged, err := c.streamState.BuildFinalResult()
		if err != nil {
			// Python build_final_result 异类型 + 抛 TypeError 直接上抛（:81 调用点无捕获）
			return nil, err
		}
		return map[string]any{"output": merged}, nil
	}
	cfg := inputs[graph.ConfigKey]
	out, err := c.subWorkflowInvokeFn(ctx, inputs[graph.InputsKey], sess, nil,
		map[string]any{graph.ConfigKey: cfg, "is_sub": true})
	if err != nil {
		return nil, err
	}
	// is_sub 路径 Python _sub_invoke 返回裸 dict（workflow.py:626），Go 直传 any
	// （R-14b-2 修复：原经 *WorkflowOutput JSON 往返丢失原始键形态）
	if m, ok := out.(map[string]any); ok {
		return m, nil
	}
	// 非 dict 形态（如流式 {"stream": [...]}/其它）经 JSON 往返收敛 map（D-17-1 延伸）
	raw, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m, nil
}

// ──────────────────────────── SubWorkflowStreamState 方法 ────────────────────────────

// Accumulate 累积流帧（Python accumulate）
func (s *SubWorkflowStreamState) Accumulate(output any) {
	s.accumulatedOutputs = append(s.accumulatedOutputs, output)
}

// BuildFinalResult 合并累积帧为最终结果（Python build_final_result :41-63）：
// 只认 map 帧；嵌套取 output 键回退自身；同 key 相加——string 拼接、数字求和、
// 其余类型（含 map+map）对齐 Python TypeError 返回 error（spec 决策 #2）。
func (s *SubWorkflowStreamState) BuildFinalResult() (map[string]any, error) {
	if len(s.accumulatedOutputs) == 0 {
		return map[string]any{}, nil
	}
	merged := map[string]any{}
	for _, output := range s.accumulatedOutputs {
		m, ok := output.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := m["output"]
		if !ok {
			inner = m
		}
		innerMap, ok := inner.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range innerMap {
			if old, exists := merged[key]; exists {
				sum, err := addValues(old, value)
				if err != nil {
					return nil, err
				}
				merged[key] = sum
			} else {
				merged[key] = value
			}
		}
	}
	return merged, nil
}

// Clear 清空状态（Python clear）
func (s *SubWorkflowStreamState) Clear() {
	s.accumulatedOutputs = nil
}

// addValues 复刻 Python merged[key] + value 的类型分派：
// string+string 拼接、数值求和（int/float 提升）、其余报错对齐 TypeError。
func addValues(a, b any) (any, error) {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as + bs, nil
	}
	af, aIsNum := toFloat64(a)
	bf, bIsNum := toFloat64(b)
	if aIsNum && bIsNum {
		return af + bf, nil
	}
	return nil, fmt.Errorf("unsupported operand type(s) for +: '%T' and '%T'", a, b)
}

// toFloat64 数值类型统一提升（int/int64/float64/float32；bool 经 int 语义天然覆盖 Python bool+bool=1+1）
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	default:
		return 0, false
	}
}

// ──────────────────────────── 8.13 D-13-5 接口实现 ────────────────────────────

// SubWorkflowDrawableSubgraph 返回子工作流子图（Python drawable.py:76
// `component.sub_workflow._internal.drawable.get_graph()`；8.13 D-13-5 接口实现）
func (c *SubWorkflowComponent) SubWorkflowDrawableSubgraph() *visualization.DrawableGraph {
	// env 为进程级开关：外层 drawable 开启则内层同开启，此守卫仅测试注入 nil 场景可达
	if c.subWorkflow == nil || c.subWorkflow.Internal().Drawable() == nil {
		return nil
	}
	return c.subWorkflow.Internal().Drawable().GetGraph()
}

// 编译期断言：实现 visualization 子图源接口
var _ visualization.SubgraphSource = (*SubWorkflowComponent)(nil)
