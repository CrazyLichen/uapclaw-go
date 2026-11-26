// recall_compressed_context 工具双语元数据。
// 对应 Python 代码：openjiuwen/harness/prompts/tools/compression_recall.py（68 行）
package tools

// ──────────────────────────── 常量 ────────────────────────────

// compressionRecallDescriptions 双语描述。
// Python: compression_recall.py:9 (_DESCRIPTIONS)
var compressionRecallDescriptions = map[string]string{
	"cn": "在当前 session 的压缩归档中检索。先匹配最相关的问答轮次（可能跨多个轮次），再在返回预算内" +
		"返回相关原文片段。memory_id 来自 [[COMPRESSION_RECALL: id=...]] 标记；省略 memory_id 时" +
		"跨本 session 的全部压缩归档搜索，结果附带每个片段所属的归档。",
	"en": "Search compressed memory archives of the current session. It first selects the most relevant turns " +
		"(possibly across multiple turns), then returns relevant source chunks within a return budget. " +
		"memory_id comes from a [[COMPRESSION_RECALL: id=...]] marker; omit it to search across all " +
		"compression archives of the session, with each chunk annotated by its source archive.",
}

// compressionRecallParamDescriptions 双语参数描述。
// Python: compression_recall.py:19 (_input_params descriptions)
var compressionRecallParamDescriptions = map[string]map[string]string{
	"cn": {
		"memory_id": "压缩摘要中 COMPRESSION_RECALL 标记所给出的记忆 ID；省略则跨全部归档搜索。",
		"query":     "用于匹配原始问答轮次和内容片段的检索文本。",
	},
	"en": {
		"memory_id": "Memory ID from the COMPRESSION_RECALL marker; omit to search across all archives.",
		"query":     "Text used to match the original turn and its source chunks.",
	},
}

// ──────────────────────────── 结构体 ────────────────────────────

// CompressionRecallMetadataProvider recall 工具元数据提供者。
// Python: compression_recall.py:37 (CompressionRecallMetadataProvider)
type CompressionRecallMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetName 返回工具名称。
// Python: compression_recall.py:50 (get_name)
func (p *CompressionRecallMetadataProvider) GetName() string { return "recall_compressed_context" }

// GetDescription 返回双语描述。
// Python: compression_recall.py:53 (get_description)
func (p *CompressionRecallMetadataProvider) GetDescription(language string) string {
	if d, ok := compressionRecallDescriptions[language]; ok {
		return d
	}
	return compressionRecallDescriptions["cn"]
}

// GetInputParams 返回参数 Schema。
// Python: compression_recall.py:56 (get_input_params)
func (p *CompressionRecallMetadataProvider) GetInputParams(language string) map[string]any {
	descriptions, ok := compressionRecallParamDescriptions[language]
	if !ok {
		descriptions = compressionRecallParamDescriptions["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"memory_id": map[string]any{"type": "string", "description": descriptions["memory_id"]},
			"query":     map[string]any{"type": "string", "description": descriptions["query"]},
		},
		"required": []any{"query"},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&CompressionRecallMetadataProvider{}) }
