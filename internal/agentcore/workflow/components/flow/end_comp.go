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
	"fmt"
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
