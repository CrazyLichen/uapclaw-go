package permissions

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolLocatorStateAbsent 交互里没有 tool_invocation_key
	//
	// Python: return "absent", None
	ToolLocatorStateAbsent = "absent"
	// ToolLocatorStateInvalid 有 key 但无法在队列里定位到 pending 卡片
	//
	// Python: return "invalid", None
	ToolLocatorStateInvalid = "invalid"
	// ToolLocatorStateLive key 命中队列里仍 pending 的卡片
	//
	// Python: return "live", card
	ToolLocatorStateLive = "live"
	// toolInvocationKeyMetadataField 队列身份指纹在元数据里的键
	//
	// Python: "tool_invocation_key"
	toolInvocationKeyMetadataField = "tool_invocation_key"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolInvocationLocatorFromInteraction 在队列里定位一次审批对应的卡片。
//
// Python: _tool_invocation_locator_from_interaction(interaction, value_obj, *,
// root_permission_queue) (interrupt_helpers.py L1599-1621)
//
// 三态返回：
//   - absent：交互里根本没有 tool_invocation_key（不是队列产出的中断）
//   - invalid：有 key 但队列不存在 / key 解析失败 / 卡片不存在或已非 pending
//   - live：命中队列里仍 pending 的那一张
//
// 为什么必须是 pending：非 pending 说明这次审批已经被消费或作废，
// 再渲染给用户会造成「批准一个已失效请求」。
func ToolInvocationLocatorFromInteraction(
	interaction map[string]any,
	valueObj any,
	queue *RootPermissionQueue,
) (string, *RootPermissionCard) {
	metadata := interactionMetadataForLocator(interaction)
	for key, value := range interruptMetadataForLocator(valueObj) {
		metadata[key] = value
	}

	if _, exists := metadata[toolInvocationKeyMetadataField]; !exists {
		return ToolLocatorStateAbsent, nil
	}
	if queue == nil {
		return ToolLocatorStateInvalid, nil
	}

	candidate, ok := metadata[toolInvocationKeyMetadataField].(map[string]any)
	if !ok {
		return ToolLocatorStateInvalid, nil
	}
	key, err := ToolInvocationKeyV1FromWire(candidate)
	if err != nil {
		return ToolLocatorStateInvalid, nil
	}

	card, exists := queue.Get(key)
	if !exists || card.State != RootPermissionCardStatePending {
		return ToolLocatorStateInvalid, nil
	}
	return ToolLocatorStateLive, &card
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// interactionMetadataForLocator 取 interaction 的 metadata 副本。
//
// Python: _extract_interaction_metadata(interaction)
func interactionMetadataForLocator(interaction map[string]any) map[string]any {
	metadata, ok := interaction["metadata"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}

// interruptMetadataForLocator 取 value 上的 metadata。
//
// Python: _extract_interrupt_metadata(value_obj)
func interruptMetadataForLocator(valueObj any) map[string]any {
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	metadata, ok := mapping["metadata"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return metadata
}
