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
	"sync"

	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"regexp"
	"strings"
)

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
