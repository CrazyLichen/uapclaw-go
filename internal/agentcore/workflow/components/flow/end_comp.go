// Package flow 流程组件：Start/End 及后续 Branch/Loop 流程控制组件。
//
// 文件目录：
//
//	flow/
//	├── doc.go           # 包文档
//	├── start_comp.go    # Start 入口组件（原样透传）
//	└── end_comp.go      # End 出口组件（模板分段渲染状态机）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/flow/
package flow

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	graphexe "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonutils "github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// logComponent 组件级日志组件常量（规则 3.2：agentcore/* 用 ComponentAgentCore）
const logComponent = logger.ComponentAgentCore

// renderTemplateVarRe {{var}} 段匹配（end_comp.py:486 re.split 捕获组
// r'(\{\{[^{}]+\}\})'；RE2 兼容此模式，8.12 RE2 先例）
var renderTemplateVarRe = regexp.MustCompile(`(\{\{[^{}]+\}\})`)

// ──────────────────────────── 导出函数 ────────────────────────────

// TemplateUtilsRenderTemplate 模板整体渲染。
// Python: TemplateUtils.render_template（end_comp.py:472-482）：
// "{{"→"$"、"}}"删除后 string.Template.safe_substitute；
// Go 直接按 {{var}} 正则替换，语义等价。
// 偏差 D-17-4：safe_substitute 缺键保留原样 → 手写等价（替换函数缺键时
// 返回原文）；不等价点（$ 字面量在 Python 版会参与替换）回收条件=上游模板语义变更。
func TemplateUtilsRenderTemplate(template string, inputs map[string]any) string {
	return renderTemplateVarRe.ReplaceAllStringFunc(template, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if v, ok := inputs[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return m // 缺键保留原样（safe_substitute 语义）
	})
}

// TemplateUtilsRenderTemplateToList 模板分段：捕获组切分 + 滤除空段。
// Python: TemplateUtils.render_template_to_list（end_comp.py:484-486
// list(filter(None, re.split(...)))）。
func TemplateUtilsRenderTemplateToList(template string) []string {
	parts := renderTemplateVarRe.Split(template, -1)
	segs := renderTemplateVarRe.FindAllString(template, -1)
	// Split 与 FindAllString 交替对齐：[文本, 变量, 文本, 变量, ..., 尾文本]
	var out []string
	for i, txt := range parts {
		if txt != "" {
			out = append(out, txt)
		}
		if i < len(segs) {
			out = append(out, segs[i])
		}
	}
	return out
}

// ──────────────────────────── templateFrame ────────────────────────────

// templateFrame 模板渲染流帧（Python render_stream 产出的 {"data","index"} dict）
type templateFrame struct {
	// Data 帧数据（静态文本串或变量值/流帧）
	Data any
	// Index 帧序号
	Index int
}

// ──────────────────────────── TemplateProcessor ────────────────────────────

// TemplateProcessor 模板分段渲染状态机。
// Python: end_comp.py:250-444 (TemplateProcessor)。
// 并发原语映射（偏差 D-17-2）：asyncio.Lock → sync.Mutex；
// asyncio.Condition + anyio.fail_after → notify channel 广播（close+换新）
// + timer select（sync.Cond 无超时等待能力）。
type TemplateProcessor struct {
	// template 原始模板串
	template string
	// segments 分段列表（变量段已剥壳）
	segments []string
	// variablesPositions 变量段位置集合
	variablesPositions map[int]bool
	// currentPosition 当前推进位置
	currentPosition int
	// chunkIndex 帧序号
	chunkIndex int
	// dataSourceCount 数据源数（mix 模式=2）
	dataSourceCount int
	// count 已完成的数据源数
	count int
	// mu 互斥锁（Python _lock）
	mu sync.Mutex
	// notify 条件广播通道：notifyAll 时 close 并换新（Python _condition）
	notify chan struct{}
}

// NewTemplateProcessor 构造：分段解析 + 变量段剥壳。
// Python: __init__（end_comp.py:251-267）。
func NewTemplateProcessor(template string) *TemplateProcessor {
	responseList := TemplateUtilsRenderTemplateToList(template)
	p := &TemplateProcessor{
		template:           template,
		segments:           responseList,
		variablesPositions: make(map[int]bool),
		notify:             make(chan struct{}),
	}
	for pos, res := range responseList {
		if strings.HasPrefix(res, "{{") && strings.HasSuffix(res, "}}") {
			p.variablesPositions[pos] = true
			p.segments[pos] = res[2 : len(res)-2]
		}
	}
	return p
}

// SetDataSourceCount 设置数据源数并清零计数。
// Python: set_data_source_count（end_comp.py:269-271）。
func (p *TemplateProcessor) SetDataSourceCount(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dataSourceCount = n
	p.count = 0
}

// CurrentPosition 当前位置（Python current_position，end_comp.py:273-274）。
func (p *TemplateProcessor) CurrentPosition() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentPosition
}

// Render 模板整体渲染（Python render，end_comp.py:301-302）。
func (p *TemplateProcessor) Render(inputs map[string]any) string {
	return TemplateUtilsRenderTemplate(p.template, inputs)
}

// ──────────────────────────── TemplateProcessor 非导出方法 ────────────────────────────

// needRender 是否存在有值的变量段。
// Python: _need_render（end_comp.py:279-287）。
func (p *TemplateProcessor) needRender(inputs map[string]any) bool {
	for pos, seg := range p.segments {
		if p.variablesPositions[pos] {
			if utils.GetValueByNestedPath(seg, inputs) != nil {
				return true
			}
		}
	}
	return false
}

// getSegment 取位置段文本（越界返回空串）。
// Python: _get_segment（end_comp.py:289-292）。
func (p *TemplateProcessor) getSegment(pos int) string {
	if pos >= len(p.segments) {
		return ""
	}
	return p.segments[pos]
}

// shouldRender 当前位置是否变量段。Python: should_render（end_comp.py:294-295）。
func (p *TemplateProcessor) shouldRender() bool {
	return p.variablesPositions[p.currentPosition]
}

// advancePosition 推进位置。Python: advance_position（end_comp.py:297-299）。
func (p *TemplateProcessor) advancePosition() int {
	p.currentPosition++
	return p.currentPosition
}

// reset 归零位置/帧序/计数。Python: reset（end_comp.py:304-308）。
func (p *TemplateProcessor) reset() {
	if p.currentPosition != 0 {
		p.currentPosition = 0
	}
	p.chunkIndex = 0
	p.count = 0
}

// isFinished 是否推进完毕。Python: is_finished（end_comp.py:443-444）。
func (p *TemplateProcessor) isFinished() bool {
	return p.currentPosition >= len(p.segments)
}

// notifyAll 广播唤醒等待方：close 现通道并换新（须持 mu）。
// Python: _condition.notify_all（end_comp.py:440-441）。
func (p *TemplateProcessor) notifyAll() {
	close(p.notify)
	p.notify = make(chan struct{})
}

// ──────────────────────────── TemplateProcessor 流式方法 ────────────────────────────

// RenderStream 模板流式渲染入口：主循环产出帧；结束后耗尽输入中残留流并按
// 数据源计数归零。Python: render_stream（end_comp.py:356-366）。
func (p *TemplateProcessor) RenderStream(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, timeout float64) <-chan graphexe.StreamChunk[templateFrame] {
	out := make(chan graphexe.StreamChunk[templateFrame])
	go func() {
		defer close(out)
		defer func() {
			// finally：耗尽输入中所有迭代器（end_comp.py:361-362）
			consumeAllIterators(inputs)
			p.mu.Lock()
			p.count++
			done := p.count == p.dataSourceCount
			if done {
				p.reset()
			}
			p.mu.Unlock()
		}()
		p.renderStreamLoop(ctx, inputs, sess, timeout, out)
	}()
	return out
}

// renderStreamLoop 主推进循环。
// Python: _render_stream（end_comp.py:368-441）。
// 三态 None 处理：mix 等待（count<data_source_count）/ 单源跳过（!has_any_value，
// debug 日志）/ 有值等待；静态段直出；channel 叶子逐帧转发。
func (p *TemplateProcessor) renderStreamLoop(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, timeout float64, out chan<- graphexe.StreamChunk[templateFrame]) {
	hasAnyValue := p.needRender(inputs)
	shouldWait := false
	for {
		if shouldWait {
			// Python: async with self._condition + anyio.fail_after(wait_timeout)
			p.mu.Lock()
			waitCh := p.notify
			p.mu.Unlock()
			var waitDur time.Duration
			if timeout > 0 {
				waitDur = time.Duration(timeout * float64(time.Second))
			} else {
				waitDur = 0 // 零/负超时 ≙ anyio.fail_after(None)：无超时等待
			}
			var timer *time.Timer
			var timeoutCh <-chan time.Time
			if waitDur > 0 {
				timer = time.NewTimer(waitDur)
				timeoutCh = timer.C
			}
			select {
			case <-waitCh:
				if timer != nil {
					timer.Stop()
				}
			case <-timeoutCh:
				// TimeoutError → warning + advance（end_comp.py:380-388）
				logger.Warn(logComponent).
					Float64("timeout", timeout).
					Str("component_id", compIDOf(sess)).
					Msg("Template render stream timeout")
				p.mu.Lock()
				p.advancePosition()
				p.mu.Unlock()
			case <-ctx.Done():
				// asyncio.CancelledError → warning + break（end_comp.py:389-397）
				logger.Warn(logComponent).
					Str("component_id", compIDOf(sess)).
					Msg("Template render stream cancelled")
				return
			}
			shouldWait = false
		}
		p.mu.Lock()
		if p.isFinished() {
			p.mu.Unlock()
			return
		}
		segment := p.getSegment(p.currentPosition)
		if !p.shouldRender() {
			// 静态文本段直出（end_comp.py:404-409）
			f := templateFrame{Data: segment, Index: p.chunkIndex}
			p.chunkIndex++
			p.advancePosition()
			p.mu.Unlock()
			out <- graphexe.StreamChunk[templateFrame]{Val: f}
			continue
		}
		value := utils.GetValueByNestedPath(segment, inputs)
		p.mu.Unlock()
		if value == nil {
			p.mu.Lock()
			if p.count < p.dataSourceCount {
				shouldWait = true // mix 模式等待另一数据源（end_comp.py:415-417）
				p.mu.Unlock()
				continue
			}
			if !hasAnyValue {
				// 单源跳过 None 值位置（end_comp.py:419-428）
				logger.Debug(logComponent).
					Str("segment", segment).
					Str("component_id", compIDOf(sess)).
					Msg("Template segment skip none value position")
				p.advancePosition()
				p.mu.Unlock()
				continue
			}
			shouldWait = true // 有值场景等待（end_comp.py:429-430）
			p.mu.Unlock()
			continue
		}
		if ch, ok := value.(<-chan any); ok {
			// 流叶子逐帧转发（end_comp.py:432-435）
			for frame := range ch {
				p.mu.Lock()
				f := templateFrame{Data: frame, Index: p.chunkIndex}
				p.chunkIndex++
				p.mu.Unlock()
				out <- graphexe.StreamChunk[templateFrame]{Val: f}
			}
		} else {
			p.mu.Lock()
			f := templateFrame{Data: value, Index: p.chunkIndex}
			p.chunkIndex++
			p.mu.Unlock()
			out <- graphexe.StreamChunk[templateFrame]{Val: f}
		}
		p.mu.Lock()
		p.advancePosition()
		p.notifyAll()
		p.mu.Unlock()
	}
}

// consumeAllIterators 递归收集并耗尽 inputs 中所有 channel 叶子。
// Python: TemplateProcessor._consume_all_iterators（end_comp.py:310-354）；
// 偏差 D-17-3：AsyncGenerator/Generator → Go channel（含反射兜底任意 chan 种类）；
// Python 逐迭代器耗尽异常仅 debug 日志，Go channel 无错误元素故省略。
func consumeAllIterators(data any) {
	switch v := data.(type) {
	case map[string]any:
		for _, val := range v {
			consumeAllIterators(val)
		}
	case []any:
		for _, val := range v {
			consumeAllIterators(val)
		}
	case <-chan any:
		for range v {
		}
	case chan any:
		for range v {
		}
	default:
		rv := reflect.ValueOf(data)
		if rv.Kind() == reflect.Chan {
			for {
				if _, ok := rv.Recv(); !ok {
					break
				}
			}
		}
	}
}

// compIDOf 组件 ID 安全取值（sess 为 nil 时返回空串）。
func compIDOf(sess *session.NodeSessionFacade) string {
	if sess == nil {
		return ""
	}
	return sess.GetComponentID()
}

// ──────────────────────────── EndConfig / End ────────────────────────────

// EndConfig End 组件配置。
// Python: end_comp.py:26-35 (EndConfig pydantic)；alias responseTemplate、
// min_length=1（构造校验拒绝空模板，拍板 α 拒绝语义同 8.15）。
type EndConfig struct {
	// ResponseTemplate 最终输出格式化模板，渲染结果 {"response": "..."}
	ResponseTemplate string `json:"responseTemplate"`
}

// NewEndConfig 构造校验（min_length=1：空模板拒绝）。
// Python: pydantic Field(min_length=1) 校验 + build_error(COMPONENT_END_PARAM_INVALID)。
func NewEndConfig(responseTemplate string) (*EndConfig, error) {
	if responseTemplate == "" {
		return nil, exception.BuildError(exception.StatusComponentEndParamInvalid,
			exception.WithParam("reason", "response_template must not be empty"))
	}
	return &EndConfig{ResponseTemplate: responseTemplate}, nil
}

// End 工作流出口组件：模板格式化最终输出。
// Python: end_comp.py:38-51 (End)。
type End struct {
	components.WorkflowComponent
	// conf 配置（可 nil）
	conf *EndConfig
	// template 模板处理器（conf 非 nil 时建立）
	template *TemplateProcessor
	// batchTemplate 批渲染处理器（_render 懒建立）
	batchTemplate *TemplateBatchProcessor
	// btMu batchTemplate 懒建立/清空的并发保护（Python asyncio 单线程事件循环
	// 天然串行，Go 多 goroutine 并发调用 render 需显式锁；偏差 D-17-2 补充）
	btMu sync.Mutex
	// mix 混合模式（模板+裸流并存）
	mix bool
}

// NewEnd 构造：conf 非 nil → 校验 + 建 TemplateProcessor，失败抛
// StatusComponentEndParamInvalid（reason 语义对齐 f"conf is invalid, {str(e)}"）。
// Python: __init__（end_comp.py:39-51）。
// InvokeFn/StreamFn/CollectFn/TransformFn 四槽在后续任务逐段绑定
// （Python 类方法覆写 → Go 函数槽赋值）。
func NewEnd(conf *EndConfig) (*End, error) {
	e := &End{}
	if conf != nil {
		if conf.ResponseTemplate == "" {
			return nil, exception.BuildError(exception.StatusComponentEndParamInvalid,
				exception.WithParam("reason", "conf is invalid, response_template must not be empty"))
		}
		e.conf = conf
		e.template = NewTemplateProcessor(conf.ResponseTemplate)
	}
	// StreamFn 绑定（Python stream，end_comp.py:91-142）
	e.StreamFn = func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (<-chan graphexe.StreamChunk[map[string]any], error) {
		logger.Debug(logComponent).
			Str("component_id", sess.GetComponentID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component stream started")
		if inputs == nil {
			inputs = map[string]any{}
		}
		out := make(chan graphexe.StreamChunk[map[string]any])
		frameCount := 0
		go func() {
			defer close(out)
			defer func() {
				logger.Debug(logComponent).
					Str("component_id", sess.GetComponentID()).
					Str("session_id", sess.GetSessionID()).
					Bool("has_template", e.template != nil).
					Bool("mix", e.mix).
					Int("chunk_num", frameCount).
					Msg("End component stream succeed")
			}()
			if e.template != nil {
				// 模板路径：逐帧包 OutputSchema{EndNodeStream}（end_comp.py:106-114）
				timeout := envTimeoutToFloat(sess.GetEnv(constants.EndCompTemplateRenderPositionTimeoutKey))
				for chunk := range e.template.RenderStream(ctx, inputs, sess, timeout) {
					if chunk.Err != nil {
						logger.Error(logComponent).
							Err(chunk.Err).
							Str("component_id", sess.GetComponentID()).
							Str("session_id", sess.GetSessionID()).
							Int("chunk_num", frameCount).
							Bool("has_template", e.template != nil).
							Bool("mix", e.mix).
							Msg("End component stream failed")
						out <- graphexe.StreamChunk[map[string]any]{Err: chunk.Err}
						return
					}
					frameCount++
					out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{
						"frame": stream.OutputSchema{
							Type:    constants.EndNodeStream,
							Index:   chunk.Val.Index,
							Payload: map[string]any{"response": chunk.Val.Data},
						},
					}}
				}
				return
			}
			// 无模板：dict 逐 key 吐 {"output": {k:v}}（end_comp.py:116-119）
			for key, value := range inputs {
				frameCount++
				out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{
					"output": map[string]any{key: value},
				}}
			}
		}()
		return out, nil
	}

	// TransformFn 绑定（Python transform，end_comp.py:144-179）
	e.TransformFn = func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (<-chan graphexe.StreamChunk[map[string]any], error) {
		logger.Debug(logComponent).
			Str("component_id", sess.GetExecutableID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component transform started")
		out := make(chan graphexe.StreamChunk[map[string]any])
		frameCount := 0
		go func() {
			defer close(out)
			defer func() {
				logger.Debug(logComponent).
					Str("component_id", sess.GetExecutableID()).
					Str("session_id", sess.GetSessionID()).
					Bool("has_template", e.template != nil).
					Bool("mix", e.mix).
					Int("chunk_num", frameCount).
					Msg("End component transform succeed")
			}()
			if e.template != nil {
				// 模板路径：流入合并后走渲染流（end_comp.py:154-162）
				merged := flattenChannelInputs(inputs)
				timeout := envTimeoutToFloat(sess.GetEnv(constants.EndCompTemplateRenderPositionTimeoutKey))
				for chunk := range e.template.RenderStream(ctx, merged, sess, timeout) {
					if chunk.Err != nil {
						out <- graphexe.StreamChunk[map[string]any]{Err: chunk.Err}
						return
					}
					frameCount++
					out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{
						"frame": stream.OutputSchema{
							Type:    constants.EndNodeStream,
							Index:   chunk.Val.Index,
							Payload: map[string]any{"response": chunk.Val.Data},
						},
					}}
				}
				return
			}
			// 无模板：逐帧叶子抽取 → {"output": {path: value}}（end_comp.py:163-171）
			for frame := range inputs {
				for _, leaf := range commonutils.ExtractLeafNodes(frame) {
					frameCount++
					out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{
						"output": map[string]any{commonutils.FormatPath(leaf.Path): leaf.Value},
					}}
				}
			}
		}()
		return out, nil
	}

	// CollectFn 绑定（Python collect，end_comp.py:181-212）
	e.CollectFn = func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		logger.Debug(logComponent).
			Str("component_id", sess.GetExecutableID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component collect started")
		var result map[string]any
		if e.template != nil {
			// 模板路径：流入合并 → 批渲染（end_comp.py:191-192）
			merged := flattenChannelInputs(inputs)
			timeout := envTimeoutToFloat(sess.GetEnv(constants.EndCompTemplateBranchRenderTimeoutKey))
			r, rerr := e.render(ctx, merged, timeout, sess)
			if rerr != nil {
				return nil, rerr
			}
			result = r
		} else {
			// 无模板：逐帧叶子抽取 → chunks 聚合（end_comp.py:193-202）
			var chunks []any
			for frame := range inputs {
				for _, leaf := range commonutils.ExtractLeafNodes(frame) {
					chunks = append(chunks, map[string]any{commonutils.FormatPath(leaf.Path): leaf.Value})
				}
			}
			result = map[string]any{"output": chunks}
		}
		logger.Debug(logComponent).
			Str("component_id", sess.GetExecutableID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component collect succeed")
		return result, nil
	}

	// InvokeFn 绑定（Python invoke，end_comp.py:59-89）
	e.InvokeFn = func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		logger.Debug(logComponent).
			Str("component_id", sess.GetComponentID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component invoke started")
		var result map[string]any
		if e.template != nil {
			if inputs == nil {
				inputs = map[string]any{}
			}
			r, rerr := e.render(ctx, inputs, envTimeoutToFloat(sess.GetEnv(constants.EndCompTemplateBranchRenderTimeoutKey)), sess)
			if rerr != nil {
				return nil, rerr
			}
			result = r
		} else {
			// 无模板：过滤 None 值 → {"output": ...} 信封（end_comp.py:75-79）
			var output any
			if inputs != nil {
				filtered := make(map[string]any, len(inputs))
				for k, v := range inputs {
					if v != nil {
						filtered[k] = v
					}
				}
				if len(filtered) > 0 {
					output = filtered
				}
			}
			if output != nil {
				result = map[string]any{"output": output}
			}
		}
		logger.Debug(logComponent).
			Str("component_id", sess.GetComponentID()).
			Str("session_id", sess.GetSessionID()).
			Bool("has_template", e.template != nil).
			Bool("mix", e.mix).
			Msg("End component invoke succeed")
		return result, nil
	}
	return e, nil
}

// render 批渲染：懒建 batchTemplate → 渲染 → notifyAll → 清空。
// Python: _render（end_comp.py:214-245）；mix 分支见 renderMix。
func (e *End) render(ctx context.Context, inputs map[string]any, timeout float64, sess *session.NodeSessionFacade) (map[string]any, error) {
	e.btMu.Lock()
	if e.batchTemplate == nil {
		e.batchTemplate = &TemplateBatchProcessor{template: e.template, inputs: inputs, notify: make(chan struct{})}
		if e.mix {
			bt := e.batchTemplate
			btCh := bt.notify
			e.btMu.Unlock()
			return e.renderMix(ctx, bt, btCh, timeout, sess)
		}
	}
	bt := e.batchTemplate
	e.btMu.Unlock()
	// 非首拍：合并自身 inputs 渲染（Python end_comp.py:239-241 inputs | inputs）
	answer, err := bt.Render(ctx, inputs, sess)
	if err != nil {
		return nil, err
	}
	bt.mu.Lock()
	bt.notifyAll()
	bt.mu.Unlock()
	e.btMu.Lock()
	e.batchTemplate = nil
	e.btMu.Unlock()
	return map[string]any{"response": answer}, nil
}

// renderMix mix 模式首拍条件等待：渲染完成被唤醒或超时降级。
// Python: _render mix 分支（end_comp.py:219-238）。
func (e *End) renderMix(ctx context.Context, bt *TemplateBatchProcessor, btCh chan struct{}, timeout float64, sess *session.NodeSessionFacade) (map[string]any, error) {
	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(time.Duration(timeout * float64(time.Second)))
		timeoutCh = timer.C
	}
	select {
	case <-btCh: // 首拍渲染完成唤醒（notify_all，end_comp.py:220-223）
		if timer != nil {
			timer.Stop()
		}
	case <-timeoutCh:
		// 渲染超时 error 日志（end_comp.py:225-231）
		logger.Error(logComponent).
			Float64("timeout", timeout).
			Str("component_id", compIDOf(sess)).
			Msg("End component render timeout")
		if !bt.IsRendered() {
			// 未渲染过 → 同步渲染兜底（end_comp.py:232-236）
			answer, aerr := bt.Render(ctx, nil, sess)
			e.btMu.Lock()
			e.batchTemplate = nil
			e.btMu.Unlock()
			if aerr != nil {
				return nil, aerr
			}
			return map[string]any{"response": answer}, nil
		}
		return nil, nil // 已渲染过 → 返回 None（end_comp.py:236）
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	e.btMu.Lock()
	e.batchTemplate = nil
	e.btMu.Unlock()
	return nil, nil // mix 首拍（渲染完成后）：返回 None（end_comp.py:237-238）
}

// envTimeoutToFloat env 超时值 → float64 秒（vertex.go anyToFloat64 同款语义，
// 本包独立实现避免引 graph 根包）。
func envTimeoutToFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// SetMix 进入混合模式：模板数据源计数置 2 并复位。
// Python: set_mix（end_comp.py:53-57）。
func (e *End) SetMix() {
	e.mix = true
	if e.template != nil {
		e.template.SetDataSourceCount(2)
		e.template.mu.Lock()
		e.template.reset()
		e.template.mu.Unlock()
	}
}

// ──────────────────────────── TemplateBatchProcessor ────────────────────────────

// TemplateBatchProcessor 批渲染处理器。
// Python: end_comp.py:447-467 (TemplateBatchProcessor)。
type TemplateBatchProcessor struct {
	// template 模板处理器
	template *TemplateProcessor
	// inputs 首拍输入
	inputs map[string]any
	// isRendered 是否已渲染过
	isRendered bool
	// mu 互斥锁（condition 配套）
	mu sync.Mutex
	// notify 条件广播通道（notifyAll 时 close+换新）
	notify chan struct{}
}

// IsRendered 是否已渲染过。Python: is_rendered（end_comp.py:454-455）。
func (b *TemplateBatchProcessor) IsRendered() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.isRendered
}

// notifyAll 广播唤醒等待方（Python: condition.notify_all，end_comp.py:240-241）。
func (b *TemplateBatchProcessor) notifyAll() {
	close(b.notify)
	b.notify = make(chan struct{})
}

// Render 合并 inputs（Python self._inputs | inputs：调用方 inputs 优先）并逐帧拼接。
// Python: render（end_comp.py:457-467）。
func (b *TemplateBatchProcessor) Render(ctx context.Context, extra map[string]any, sess *session.NodeSessionFacade) (string, error) {
	b.mu.Lock()
	b.isRendered = true
	b.mu.Unlock()
	merged := make(map[string]any, len(b.inputs)+len(extra))
	for k, v := range b.inputs {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	var sb strings.Builder
	for chunk := range b.template.RenderStream(ctx, merged, sess, 0) {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		sb.WriteString(fmt.Sprintf("%v", chunk.Val.Data))
	}
	return sb.String(), nil
}

// flattenChannelInputs 将流入的多帧合并为单 dict（同名后帧覆盖前帧）。
// Python 流入侧收的是 dict + AsyncGenerator 叶子（偏差 D-17-3），Go 统一为
// 帧 channel 后的等价聚合；流叶子在 RenderStream/consumeAllIterators 内消化。
func flattenChannelInputs(inputs <-chan map[string]any) map[string]any {
	merged := map[string]any{}
	for frame := range inputs {
		for k, v := range frame {
			merged[k] = v
		}
	}
	return merged
}
