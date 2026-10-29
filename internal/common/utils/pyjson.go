// pyjson.go 提供 Python json.dumps 语义对齐的序列化助手。
//
// Python json.dumps(ensure_ascii=False) 的两个关键行为：
//  1. 非 ASCII 字符原样输出（Go json.Marshal 默认同样原样，无差异）
//  2. HTML 字符 < > & 原样输出（Go json.Marshal 默认转义为 \u003c 等
//     ——SetEscapeHTML(false) 对齐；提示词/历史写回等场景 LLM 会看到
//     字面 \u003c 转义串而非 < 本身）
//
// 注意：json.Encoder.Encode 会在产物尾部追加换行符（Python json.dumps
// 无），助手统一去掉以对齐字节输出。
//
// 对应 Python 代码：json.dumps(...)（各调用点 ensure_ascii=False 形态）
package utils

import (
	"bytes"
	"encoding/json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MarshalNoEscape JSON 序列化（紧凑、不转义 HTML 字符），
// 对齐 Python json.dumps(v, ensure_ascii=False)。
// 分隔符差异：Go 默认 ","/":"（无空格），Python 默认 ", "/": "——
// 消费方为 LLM 提示词/文件写回等非字节级比较场景，语义等价。
func MarshalNoEscape(v any) ([]byte, error) {
	return marshalNoEscape(v, "")
}

// MarshalIndentNoEscape JSON 序列化（2 空格缩进、不转义 HTML 字符），
// 对齐 Python json.dumps(v, ensure_ascii=False, indent=2)。
// 尾换行去掉（Python json.dumps 无尾换行；Encode 有）。
func MarshalIndentNoEscape(v any) ([]byte, error) {
	return marshalNoEscape(v, "  ")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalNoEscape 统一实现：prefix 非空时启用缩进。
func marshalNoEscape(v any, indentPrefix string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indentPrefix != "" {
		enc.SetIndent("", indentPrefix)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode 追加尾换行，去掉以对齐 Python json.dumps 字节输出
	b := buf.Bytes()
	return bytes.TrimSuffix(b, []byte("\n")), nil
}
