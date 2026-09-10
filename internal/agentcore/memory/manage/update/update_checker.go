package update

import (
	"context"
	"fmt"
	"strings"

	"github.com/wk8/go-ordered-map/v2"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryActionItem 记忆动作项，表示一条记忆的 ADD 或 DELETE 动作。
//
// Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryActionItem)
type MemoryActionItem struct {
	// ID 记忆 ID
	ID string
	// Content 记忆内容
	Content string
	// Status 动作状态
	Status MemoryStatus
}

// MemCheckItem 记忆检查结果项。
//
// Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemCheckItem)
type MemCheckItem struct {
	// InfoID 记忆 ID
	InfoID string
	// InfoText 记忆内容
	InfoText string
	// Result 检查结果
	Result CheckResult
	// RelatedInfos 关联的旧记忆
	RelatedInfos map[string]string
}

// MemUpdateChecker 记忆冲突检查器。
//
// 使用 LLM 驱动的提示词模板分析新旧记忆之间的冗余和冲突关系。
// Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
//
// Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemUpdateChecker)
type MemUpdateChecker struct{}

// checkConfig Check 配置。
type checkConfig struct {
	// model LLM 模型（对齐 Python: base_chat_model）
	model *llm.Model
	// retries 重试次数（对齐 Python: retries=3）
	retries int
}

// CheckOption Check 可选参数。
type CheckOption func(*checkConfig)

// ──────────────────────────── 枚举 ────────────────────────────

// CheckResult 记忆检查结果枚举。
//
// Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (CheckResult)
type CheckResult int

const (
	// CheckResultRedundant 冗余
	CheckResultRedundant CheckResult = iota
	// CheckResultConflicting 冲突
	CheckResultConflicting
	// CheckResultNone 共存
	CheckResultNone
)

// MemoryStatus 记忆动作状态枚举。
//
// Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryStatus)
type MemoryStatus int

const (
	// MemoryStatusAdd 添加
	MemoryStatusAdd MemoryStatus = iota
	// MemoryStatusDelete 删除
	MemoryStatusDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithModel 设置 LLM 模型（对齐 Python: base_chat_model）。
func WithModel(m *llm.Model) CheckOption {
	return func(c *checkConfig) { c.model = m }
}

// WithRetries 设置重试次数（对齐 Python: retries）。
func WithRetries(n int) CheckOption {
	return func(c *checkConfig) { c.retries = n }
}

// Check 检查新记忆与旧记忆的冗余/冲突。
//
// Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
//
// 流程：
//  1. 无 LLM 模型时直接返回所有新记忆为 ADD
//  2. 格式化输入 → 加载提示词模板 → LLM 调用 → JSON 解析（最多 retries 次）
//  3. 映射结果：REDUNDANT→跳过 / CONFLICTING→新ADD+旧DELETE / NONE→新ADD
//  4. 解析失败 fallback：所有新记忆 ADD
func (c *MemUpdateChecker) Check(ctx context.Context, newMemories *orderedmap.OrderedMap[string, string], oldMemories *orderedmap.OrderedMap[string, string], opts ...CheckOption) ([]*MemoryActionItem, error) {
	cfg := &checkConfig{retries: 3}
	for _, opt := range opts {
		opt(cfg)
	}

	// 无 LLM 模型 → 直接返回所有新记忆为 ADD（对齐 Python: if not base_chat_model）
	if cfg.model == nil {
		logger.Debug(logComponent).
			Int("new_count", newMemories.Len()).
			Int("old_count", oldMemories.Len()).
			Msg("无 LLM 模型，跳过记忆冲突检查")
		return allAddItems(newMemories), nil
	}

	// 检查新旧记忆 ID 重复（对齐 Python: duplicate_ids = set(new) & set(old)）
	duplicateIDs := checkDuplicateIDs(newMemories, oldMemories)
	if len(duplicateIDs) > 0 {
		logger.Debug(logComponent).
			Int("duplicate_count", len(duplicateIDs)).
			Msg("发现重复记忆 ID")
	}

	// 步骤 1：格式化输入（对齐 Python: _format_input）
	newInfoStr, oldInfoStr := formatInput(newMemories, oldMemories)

	// 步骤 2：加载提示词模板并替换变量（对齐 Python: PromptApplier.apply）
	userPrompt, err := prompts.DefaultApplier().Apply("memory_update_check", map[string]any{
		"new_information": newInfoStr,
		"old_information": oldInfoStr,
	})
	if err != nil {
		return allAddItems(newMemories), fmt.Errorf("加载记忆冲突检查提示词模板失败: %w", err)
	}

	// 步骤 3：构造消息（对齐 Python: messages = [{"role": "user", "content": user_prompt}]）
	formatted := prompt.NewPromptTemplate("memory_update_check_user", userPrompt)
	messages, err := formatted.ToMessages()
	if err != nil {
		return allAddItems(newMemories), fmt.Errorf("构造冲突检查消息失败: %w", err)
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	logger.Debug(logComponent).Msg("开始记忆冲突检查")

	// 步骤 4：LLM 调用 + JSON 解析（对齐 Python: for attempt in range(retries)）
	parser := output_parsers.NewJsonOutputParser()
	var checkItems []*MemCheckItem

	for attempt := 0; attempt < cfg.retries; attempt++ {
		response, invokeErr := cfg.model.Invoke(ctx, msgsParam,
			model_clients.WithInvokeOutputParser(parser))
		if invokeErr != nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Err(invokeErr).
					Msg("记忆冲突检查 LLM 调用失败，重试中")
				continue
			}
			logger.Error(logComponent).Err(invokeErr).Msg("记忆冲突检查 LLM 调用全部失败")
			return allAddItems(newMemories), nil
		}

		parsedResult := response.ParserContent
		if parsedResult == nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Msg("记忆冲突检查解析结果为 nil，重试中")
				continue
			}
			logger.Error(logComponent).Msg("记忆冲突检查解析结果为 nil，全部重试失败")
			return allAddItems(newMemories), nil
		}

		items, parseErr := parseCheckItems(parsedResult)
		if parseErr != nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Err(parseErr).
					Msg("记忆冲突检查解析错误，重试中")
				continue
			}
			logger.Error(logComponent).Err(parseErr).Msg("记忆冲突检查重试全部失败")
			return allAddItems(newMemories), nil
		}

		checkItems = items
		logger.Debug(logComponent).
			Int("result_count", len(checkItems)).
			Msg("记忆冲突检查 LLM 返回成功")
		break
	}

	// 步骤 5：映射结果为动作项（对齐 Python: check → action_items 逻辑）
	actionItems := mapCheckItemsToActionItems(checkItems, newMemories, oldMemories)

	logger.Debug(logComponent).
		Int("action_count", len(actionItems)).
		Msg("记忆冲突检查完成")

	return actionItems, nil
}

// String 实现 fmt.Stringer 接口，对齐 Python CheckResult.value
func (cr CheckResult) String() string {
	switch cr {
	case CheckResultRedundant:
		return "redundant"
	case CheckResultConflicting:
		return "conflicting"
	case CheckResultNone:
		return "none"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python MemoryStatus.value
func (ms MemoryStatus) String() string {
	switch ms {
	case MemoryStatusAdd:
		return "add"
	case MemoryStatusDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatInput 格式化新旧记忆字典为提示词输入文本。
//
// Python: _format_input(new_memories, old_memories)
// 新记忆按插入顺序反序（[::-1]），旧记忆按插入顺序（不排序）。
func formatInput(newMemories *orderedmap.OrderedMap[string, string], oldMemories *orderedmap.OrderedMap[string, string]) (string, string) {
	// 新记忆：按插入顺序反序（对齐 Python: new_info_lines[::-1]）
	var newLines []string
	if newMemories != nil {
		newLines = make([]string, 0, newMemories.Len())
		for pair := newMemories.Oldest(); pair != nil; pair = pair.Next() {
			newLines = append(newLines, fmt.Sprintf("%s: %s", pair.Key, pair.Value))
		}
		// 反转
		for i, j := 0, len(newLines)-1; i < j; i, j = i+1, j-1 {
			newLines[i], newLines[j] = newLines[j], newLines[i]
		}
	}

	// 旧记忆：按插入顺序（对齐 Python: 不排序）
	var oldLines []string
	if oldMemories != nil {
		oldLines = make([]string, 0, oldMemories.Len())
		for pair := oldMemories.Oldest(); pair != nil; pair = pair.Next() {
			oldLines = append(oldLines, fmt.Sprintf("%s: %s", pair.Key, pair.Value))
		}
	}

	return strings.Join(newLines, "\n"), strings.Join(oldLines, "\n")
}

// mapCheckItemsToActionItems 将 LLM 检查结果映射为动作项列表。
//
// Python: check() 方法中的 action_items 映射逻辑。
// REDUNDANT → 跳过 / CONFLICTING → 新ADD+旧DELETE / NONE → 新ADD
// 使用 processedNewIds 追踪已处理的新记忆 ID（对齐 Python: processed_new_ids）。
func mapCheckItemsToActionItems(checkItems []*MemCheckItem, newMemories *orderedmap.OrderedMap[string, string], oldMemories *orderedmap.OrderedMap[string, string]) []*MemoryActionItem {
	var actionItems []*MemoryActionItem
	// Python: processed_new_ids = set()
	processedNewIds := make(map[string]bool)

	for _, item := range checkItems {
		// Python: processed_new_ids.add(new_id)
		processedNewIds[item.InfoID] = true

		switch item.Result {
		case CheckResultRedundant:
			// 冗余 → 跳过（对齐 Python: if check_item.result == CheckResult.REDUNDANT）
			logger.Debug(logComponent).
				Str("mem_id", item.InfoID).
				Msg("记忆冗余，跳过")
			continue

		case CheckResultConflicting:
			// 冲突 → 新记忆 ADD + 关联旧记忆 DELETE
			newContent, ok := newMemories.Get(item.InfoID)
			if !ok {
				newContent = item.InfoText
			}
			actionItems = append(actionItems, &MemoryActionItem{
				ID:      item.InfoID,
				Content: newContent,
				Status:  MemoryStatusAdd,
			})
			// Python: for old_id in item.related_infos: if old_id in old_memories
			for oldID, oldContent := range item.RelatedInfos {
				if _, exists := oldMemories.Get(oldID); !exists {
					continue
				}
				actionItems = append(actionItems, &MemoryActionItem{
					ID:      oldID,
					Content: oldContent,
					Status:  MemoryStatusDelete,
				})
			}

		case CheckResultNone:
			// 共存 → 新记忆 ADD
			newContent, ok := newMemories.Get(item.InfoID)
			if !ok {
				newContent = item.InfoText
			}
			actionItems = append(actionItems, &MemoryActionItem{
				ID:      item.InfoID,
				Content: newContent,
				Status:  MemoryStatusAdd,
			})
		}
	}

	return actionItems
}

// parseCheckItems 从 LLM 解析后的 any 结果中提取 MemCheckItem 列表。
//
// Python: parsed_result → MemCheckItem.model_validate(item)
// 支持单对象（map）和数组（slice）两种格式。
func parseCheckItems(parsed any) ([]*MemCheckItem, error) {
	var items []map[string]any

	switch v := parsed.(type) {
	case map[string]any:
		items = []map[string]any{v}
	case []any:
		for _, elem := range v {
			if m, ok := elem.(map[string]any); ok {
				items = append(items, m)
			}
		}
	default:
		return nil, fmt.Errorf("解析结果类型不支持: %T", parsed)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("解析结果为空")
	}

	var result []*MemCheckItem
	for _, item := range items {
		infoID, _ := item["info_id"].(string)
		infoText, _ := item["info_text"].(string)
		resultStr, _ := item["result"].(string)
		checkResult := parseCheckResult(resultStr)

		relatedInfos := make(map[string]string)
		if ri, ok := item["related_infos"].(map[string]any); ok {
			for k, v := range ri {
				if vs, ok := v.(string); ok {
					relatedInfos[k] = vs
				}
			}
		}

		result = append(result, &MemCheckItem{
			InfoID:       infoID,
			InfoText:     infoText,
			Result:       checkResult,
			RelatedInfos: relatedInfos,
		})
	}

	return result, nil
}

// parseCheckResult 从字符串解析 CheckResult 枚举。
func parseCheckResult(s string) CheckResult {
	switch s {
	case "redundant":
		return CheckResultRedundant
	case "conflicting":
		return CheckResultConflicting
	case "none":
		return CheckResultNone
	default:
		return CheckResultNone
	}
}

// allAddItems 返回所有新记忆为 ADD 动作项（fallback 行为）。
func allAddItems(newMemories *orderedmap.OrderedMap[string, string]) []*MemoryActionItem {
	if newMemories == nil {
		return nil
	}
	result := make([]*MemoryActionItem, 0, newMemories.Len())
	for pair := newMemories.Oldest(); pair != nil; pair = pair.Next() {
		result = append(result, &MemoryActionItem{
			ID:      pair.Key,
			Content: pair.Value,
			Status:  MemoryStatusAdd,
		})
	}
	return result
}

// checkDuplicateIDs 检查新旧记忆 ID 重复。
func checkDuplicateIDs(newMemories *orderedmap.OrderedMap[string, string], oldMemories *orderedmap.OrderedMap[string, string]) []string {
	if newMemories == nil || oldMemories == nil {
		return nil
	}
	var duplicates []string
	for pair := newMemories.Oldest(); pair != nil; pair = pair.Next() {
		if _, ok := oldMemories.Get(pair.Key); ok {
			duplicates = append(duplicates, pair.Key)
		}
	}
	return duplicates
}
