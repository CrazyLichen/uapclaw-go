package anthropic

import (
	"encoding/json"
	"sort"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeReplayBlock 只保留 Anthropic 消息输入 schema 接受的字段；其余返回 nil（丢弃）。
// 四型白名单：thinking（thinking+signature）/ redacted_thinking（data 非空）/ text（text+citations）/
// tool_use（id+name+input）。Python: anthropic_model_client.py:145-176 _sanitize_replay_block
func SanitizeReplayBlock(block any) map[string]any {
	data := toPlainData(block)
	if data == nil {
		return nil
	}
	blockType, _ := data["type"].(string)
	switch blockType {
	case "thinking":
		return map[string]any{
			"type":      "thinking",
			"thinking":  strArg(data, "thinking"),
			"signature": strArg(data, "signature"),
		}
	case "redacted_thinking":
		opaqueData := data["data"]
		if opaqueData == nil || opaqueData == "" {
			return nil
		}
		return map[string]any{"type": "redacted_thinking", "data": opaqueData}
	case "text":
		result := map[string]any{"type": "text", "text": strArg(data, "text")}
		if data["citations"] != nil {
			result["citations"] = deepCopyValue(data["citations"])
		}
		return result
	case "tool_use":
		input := data["input"]
		if input == nil {
			input = map[string]any{}
		}
		return map[string]any{
			"type":  "tool_use",
			"id":    strArg(data, "id"),
			"name":  strArg(data, "name"),
			"input": deepCopyValue(input),
		}
	}
	return nil
}

// PreservedContentBlocks 从消息（含 metadata）取回放块列表并逐块 sanitize。
// Python: anthropic_model_client.py:178-192 _preserved_content_blocks
func PreservedContentBlocks(message map[string]any) []map[string]any {
	rawBlocks, exists := message[anthropicInternalContentBlocksKey]
	if !exists {
		if metadata, ok := message["metadata"].(map[string]any); ok {
			rawBlocks = metadata[AnthropicContentBlocksMetadataKey]
		}
	}
	list, ok := rawBlocks.([]any)
	if !ok {
		return nil
	}
	blocks := make([]map[string]any, 0, len(list))
	for _, block := range list {
		if sanitized := SanitizeReplayBlock(block); sanitized != nil {
			blocks = append(blocks, sanitized)
		}
	}
	return blocks
}

// CopyPreservedBlocksToConvertedMessages 把 provider 私有 metadata 恢复进
// 通用转换器剥掉的消息（按位置 zip 回填旁路键）。
// Python: anthropic_model_client.py:194-208 _copy_preserved_blocks_to_converted_messages
func CopyPreservedBlocksToConvertedMessages(sourceMessages []map[string]any, convertedMessages []map[string]any) {
	if len(sourceMessages) == 0 || len(sourceMessages) != len(convertedMessages) {
		return
	}
	for i, source := range sourceMessages {
		metadata, ok := source["metadata"].(map[string]any)
		if !ok {
			continue
		}
		rawBlocks, ok := metadata[AnthropicContentBlocksMetadataKey].([]any)
		if !ok {
			continue
		}
		convertedMessages[i][anthropicInternalContentBlocksKey] = deepCopyValue(rawBlocks)
	}
}

// StreamBlocksMetadata 把流式累积块（按 index 升序）sanitize 后打包进 metadata。
// Python: anthropic_model_client.py:210-219 _stream_blocks_metadata
func StreamBlocksMetadata(blockAcc map[int]map[string]any) map[string]any {
	if len(blockAcc) == 0 {
		return nil
	}
	indices := make([]int, 0, len(blockAcc))
	for idx := range blockAcc {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	blocks := make([]map[string]any, 0, len(indices))
	for _, idx := range indices {
		if sanitized := SanitizeReplayBlock(blockAcc[idx]); sanitized != nil {
			blocks = append(blocks, sanitized)
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	return map[string]any{AnthropicContentBlocksMetadataKey: blocks}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toPlainData 把任意 JSON 形值归一为 map（非 map 返回 nil）。
// Python: anthropic_model_client.py:134-143 _to_plain_data（Go 侧入参已是 JSON 解码结果，
// 无 SDK 模型对象，只剩 Mapping 分支）。
func toPlainData(value any) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		// 支持 struct 序列化兜底（如测试直接传 Go struct）
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		m = map[string]any{}
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
	}
	return m
}

// deepCopyValue 经 JSON round-trip 深拷贝（对齐 Python copy.deepcopy 语义）。
func deepCopyValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return value
	}
	return out
}
