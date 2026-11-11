// Package interrupt 的审查器 UI 元数据投影。
//
// 本文件对齐 Python interrupt_helpers.py 的 _reviewer_ui_metadata_from_interaction
// （L1563-1598）与 _extract_interaction_metadata（L1554-1561）。
//
// # 数据来源的三层合并
//
// 审批卡片上要显示「为什么需要审批」——这段信息由审查器产出，散在三个位置：
//  1. interaction["metadata"]：审批交互自带的元数据
//  2. value["metadata"]：中断值对象上的元数据（审查器写入）
//  3. value 上的同名顶层字段：少数审查器直接写在 value 顶层
//
// 三层按 1 → 2 → 3 的顺序合并（后者覆盖前者），再经白名单过滤 + 脱敏 + 截断。
//
// # 为什么必须白名单
//
// metadata 里混着内部键（cache 标记、追踪 id）。直接透传到前端既泄露内部实现，
// 也可能带出未经脱敏的路径或命令片段。白名单只放 13 个面向用户的键。
package interrupt

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/redaction"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// reviewerUIMaxListItems 列表类字段最多保留条数
	//
	// Python: _AUTO_REVIEWER_UI_MAX_LIST_ITEMS = 8
	reviewerUIMaxListItems = 8
	// reviewerUIMaxTextLength 文本类字段最大长度
	//
	// Python: _AUTO_REVIEWER_UI_MAX_TEXT_LENGTH = 512
	reviewerUIMaxTextLength = 512
	// reviewerUITruncatedMarker 列表超限时的尾巴标记
	//
	// Python: items.append("[TRUNCATED]")
	reviewerUITruncatedMarker = "[TRUNCATED]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// reviewerUIMetadataKeys 投影到前端的元数据键白名单，顺序即输出顺序。
	//
	// Python: _AUTO_REVIEWER_UI_METADATA_KEYS (interrupt_helpers.py L69-83)
	reviewerUIMetadataKeys = []string{
		"action_summary",
		"contract_gate_missing_evidence",
		"decision_source",
		"evidence_summary",
		"fallback_reason",
		"final_reviewer_status",
		"manual_reason_code",
		"manual_reason_summary",
		"remaining_forbidden_actions",
		"reviewer_status",
		"risk_level",
		"user_authorization",
		"user_review_hint",
	}

	// reviewerUIListMetadataKeys 其中按列表处理的键。
	//
	// Python: _AUTO_REVIEWER_UI_LIST_METADATA_KEYS (L84-86)
	reviewerUIListMetadataKeys = map[string]bool{
		"contract_gate_missing_evidence": true,
		"remaining_forbidden_actions":    true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractInteractionMetadata 取 interaction 自带的 metadata。
//
// Python: _extract_interaction_metadata(interaction) (interrupt_helpers.py L1554-1561)
func extractInteractionMetadata(interaction map[string]any) map[string]any {
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

// reviewerUIMetadataFromInteraction 从审批交互中投影出前端可见的审查器元数据。
//
// Python: _reviewer_ui_metadata_from_interaction(interaction, value_obj) (L1563-1598)
//
// 三步：
//  1. 合并三层来源（interaction.metadata → value.metadata → value 顶层同名键）
//  2. 按白名单逐个取值，空值（nil / ""）跳过
//  3. 列表键：只保留非空字符串、脱敏、最多 8 条，超限追加 [TRUNCATED]
//     文本键：只保留非空字符串、脱敏、截断到 512
func reviewerUIMetadataFromInteraction(
	interaction map[string]any,
	valueObj any,
) map[string]any {
	metadata := extractInteractionMetadata(interaction)
	for key, value := range extractInterruptMetadata(valueObj) {
		metadata[key] = value
	}
	for _, key := range reviewerUIMetadataKeys {
		if value := readValueField(valueObj, key); value != "" {
			metadata[key] = value
		}
	}

	projected := make(map[string]any, len(reviewerUIMetadataKeys))
	for _, key := range reviewerUIMetadataKeys {
		value := metadata[key]
		if isEmptyMetadataValue(value) {
			continue
		}

		if reviewerUIListMetadataKeys[key] {
			items, ok := value.([]any)
			if !ok {
				continue
			}
			limit := len(items)
			if limit > reviewerUIMaxListItems {
				limit = reviewerUIMaxListItems
			}
			redacted := make([]any, 0, limit+1)
			for _, item := range items[:limit] {
				text, isString := item.(string)
				if !isString || strings.TrimSpace(text) == "" {
					continue
				}
				redacted = append(redacted, redaction.RedactSecretValues(
					text,
					redaction.WithRedactTextMaxLength(reviewerUIMaxTextLength),
				))
			}
			if len(items) > reviewerUIMaxListItems {
				redacted = append(redacted, reviewerUITruncatedMarker)
			}
			if len(redacted) > 0 {
				projected[key] = redacted
			}
			continue
		}

		text, isString := value.(string)
		if !isString || strings.TrimSpace(text) == "" {
			continue
		}
		projected[key] = redaction.RedactSecretValues(
			text,
			redaction.WithRedactTextMaxLength(reviewerUIMaxTextLength),
		)
	}
	return projected
}

// isEmptyMetadataValue 判断值是否为空（nil 或空字符串）。
//
// Python: `if value in (None, ""): continue`
func isEmptyMetadataValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}
