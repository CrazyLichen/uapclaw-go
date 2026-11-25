// Package tools 提供 harness 工具层公共契约：ToolOutput 统一返回形态与字段渲染。
//
// Python 真相源：openjiuwen/harness/tools/base_tool.py（ToolOutput re-export 自
// core.foundation.tool.schema + render_fields）。演进工具面（agent_evolving/tools）
// 以本包 ToolOutput 为统一返回契约；D-15.21-4 的 map 暂代形态随 15.24b 回收。
//
// 文件目录：
//
//	tools/
//	├── doc.go              # 包文档
//	└── base_tool.go       # ToolOutput 三字段契约 + RenderFields 渲染
//
// 对应 Python 代码：openjiuwen/harness/tools/base_tool.py
package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolOutput 演进工具统一返回契约（Python ToolOutput(success, data, error) 三字段）。
// 裁决 2026-12-24 方案 A：独立 struct；15.21 D-15.21-4 map 暂代形态随本批回收。
type ToolOutput struct {
	// Success 是否成功
	Success bool `json:"success"`
	// Data 结构化结果负载（partial 提交时携带 status/counts/retry_request_id）
	Data map[string]any `json:"data,omitempty"`
	// Error 失败信息（partial 提交时为通用错误，明细在 Data）
	Error string `json:"error,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMap 转为 foundation/tool.Invoke 返回的 map 形态。
// Data 原样透传（禁止 json.Marshal→Unmarshal 往返：int 会漂移为 float64）。
func (o ToolOutput) ToMap() map[string]any {
	m := map[string]any{"success": o.Success}
	if o.Data != nil {
		m["data"] = o.Data
	}
	if o.Error != "" {
		m["error"] = o.Error
	}
	return m
}

// RenderFields 将扁平记录渲染为 "key: value" 行序列（模型可见文本）。
// Python: render_fields (harness/tools/base_tool.py:10-30)——None/空串跳过，
// 字符串原样，其余 JSON 编码（Python default=str 兜底语义）。
// 偏差：Python dict 保插入序，Go map 无序——按 key 字典序输出保证确定性。
func RenderFields(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value := fields[key]
		if value == nil || value == "" {
			continue
		}
		if text, ok := value.(string); ok {
			pairs = append(pairs, key+": "+text)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			encoded = []byte(fmt.Sprintf("%v", value))
		}
		pairs = append(pairs, key+": "+string(encoded))
	}
	return strings.Join(pairs, "\n")
}
