package harness_providers

import (
	"strings"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// 本文件对应 Python openjiuwen/harness_providers/inputs.py（34 行）：
// 把协议输入渲染成大多数 SDK 接受的一段纯文本。

// ──────────────────────────── 导出函数 ────────────────────────────

// HarnessInputText 把 HarnessInput 渲染成一个提示词字符串。
// Python: inputs.py:14-27 harness_input_text
//
// 三分支（顺序不可调）：
//  1. 字符串直通
//  2. 非空的 {"type":"text","text":...} 块列表 → 空行拼接
//  3. 其余 JSON 值 → 紧凑序列化
//
// 第 3 分支的意义：结构化输入不得静默退化成 str(dict)。
func HarnessInputText(content harnessprotocol.HarnessInput) string {
	value := harnessprotocol.JsonValueToBuiltin(content.Content)
	if text, ok := value.(string); ok {
		return text
	}
	if list, ok := value.([]any); ok && len(list) > 0 && allTextBlocks(list) {
		parts := make([]string, 0, len(list))
		for _, block := range list {
			mapping := block.(map[string]any)
			parts = append(parts, mapping["text"].(string))
		}
		return strings.Join(parts, "\n\n")
	}
	// Python: json.dumps(value, ensure_ascii=False, separators=(",", ":"))
	// utils.MarshalNoEscape 关闭 HTML 转义且无缩进、无尾换行，逐字对应。
	encoded, err := utils.MarshalNoEscape(value)
	if err != nil {
		// 序列化结构性不可能失败（值已通过 FreezeJsonValue），防御性返回空串
		return ""
	}
	return string(encoded)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// allTextBlocks 每个块都是 {"type":"text","text":<string>}。
// Python: inputs.py:30-31 _is_text_block（配合 all() 判定）
func allTextBlocks(list []any) bool {
	for _, block := range list {
		mapping, ok := block.(map[string]any)
		if !ok {
			return false
		}
		if mapping["type"] != "text" {
			return false
		}
		if _, ok := mapping["text"].(string); !ok {
			return false
		}
	}
	return true
}
