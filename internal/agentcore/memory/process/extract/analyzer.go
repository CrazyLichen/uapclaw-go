package extract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VariableResult 变量提取结果，记录单个变量的键值对。
//
// Python: openjiuwen/core/memory/process/extract/memory_analyzer.py (VariableResult)
type VariableResult struct {
	// VariableKey 变量键名
	VariableKey string `json:"variable_key"`
	// VariableValue 变量值
	VariableValue string `json:"variable_value"`
}

// MemoryAnalyzerResult 记忆分析结果，包含关键信息标志、变量列表和摘要。
//
// Python: openjiuwen/core/memory/process/extract/memory_analyzer.py (MemoryAnalyzerResult)
type MemoryAnalyzerResult struct {
	// HasKeyInformation 是否含有关键信息
	HasKeyInformation bool `json:"has_key_information"`
	// Variables 提取的变量列表
	Variables []VariableResult `json:"variables"`
	// Summary 对话摘要
	Summary string `json:"summary"`
}

// MemoryAnalyzer 记忆分析器，通过 LLM 从对话中提取变量和摘要。
//
// 无状态分析器，所有方法均为静态（对齐 Python: __init__: pass, @staticmethod）。
//
// Python: openjiuwen/core/memory/process/extract/memory_analyzer.py (MemoryAnalyzer)
type MemoryAnalyzer struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Analyze 分析对话消息，提取变量和摘要。
//
// 流程：
//  1. messages 为空时返回 nil（对齐 Python: if len(messages) == 0: return None）
//  2. 格式化 history_messages 和 messages 为字符串
//  3. 从 memory_config.mem_variables 构建 variables_description 和 variables_output_format JSON
//  4. forbidden_variables 空字符串映射为 "None"
//  5. 加载 memory_analysis_prompt 模板
//  6. 调用 LLM + JsonOutputParser 解析，最多重试 retries 次
//  7. !enable_long_term_mem || !enable_summary_memory 时清空 summary
//  8. 全部重试失败返回空 MemoryAnalyzerResult
//
// Python: MemoryAnalyzer.analyze (@staticmethod)
func (MemoryAnalyzer) Analyze(
	ctx context.Context,
	messages []llmschema.BaseMessage,
	historyMessages []llmschema.BaseMessage,
	baseChatModel *llm.Model,
	memoryConfig *config.AgentMemoryConfig,
	summaryMaxToken int,
	scopeConfig *config.MemoryScopeConfig,
	forbiddenVariables string,
	retries int,
) (*MemoryAnalyzerResult, error) {
	// 步骤 1：messages 为空时返回 nil（对齐 Python: if len(messages) == 0: return None）
	if len(messages) == 0 {
		logger.Warn(logComponent).
			Str("event_type", "MEMORY_PROCESS").
			Int("messages_len", len(messages)).
			Msg("没有消息可供分析")
		return nil, nil
	}

	// 步骤 2：格式化 history_messages 和 messages（对齐 Python: history / conversation 字符串拼接）
	// Python: for msg in history_messages: history += f"{msg.role}: {msg.content}\n"
	history := ""
	for _, msg := range historyMessages {
		history += fmt.Sprintf("%s: %s\n", msg.GetRole().String(), msg.GetContent().Text())
	}
	// Python: for msg in messages: conversation += f"{msg.role}: {msg.content}\n"
	conversation := ""
	for _, msg := range messages {
		conversation += fmt.Sprintf("%s: %s\n", msg.GetRole().String(), msg.GetContent().Text())
	}

	// 步骤 3：构建 variables_description 和 variables_output_format JSON
	// Python: variables_description = [] / variables_output_format = []
	// Python: for param in memory_config.mem_variables: ...
	variablesDescription := make([]map[string]string, 0, len(memoryConfig.MemVariables))
	variablesOutputFormat := make([]map[string]string, 0, len(memoryConfig.MemVariables))
	for _, param := range memoryConfig.MemVariables {
		variablesDescription = append(variablesDescription, map[string]string{
			"variable_key":   param.Name,
			"variable_value": param.Description,
		})
		variablesOutputFormat = append(variablesOutputFormat, map[string]string{
			"variable_key":   param.Name,
			"variable_value": "",
		})
	}
	variablesDescriptionJSON, _ := json.Marshal(variablesDescription)
	variablesOutputFormatJSON, _ := json.Marshal(variablesOutputFormat)

	// Python: has_variable = (len(memory_config.mem_variables) > 0)
	hasVariable := len(memoryConfig.MemVariables) > 0

	// 步骤 4：forbidden_variables 空字符串映射为 "None"（对齐 Python: forbidden_variables = "None" if forbidden_variables == "" else forbidden_variables）
	fbVariables := forbiddenVariables
	if fbVariables == "" {
		fbVariables = "None"
	}

	// 步骤 5：加载提示词模板（对齐 Python: PromptApplier().apply("memory_analysis_prompt", {...})）
	// Python: user_profile_definition = scope_config.user_profile_definition or "" if scope_config else ""
	userProfileDef := ""
	semanticMemoryDef := ""
	episodicMemoryDef := ""
	if scopeConfig != nil {
		userProfileDef = scopeConfig.UserProfileDefinition
		semanticMemoryDef = scopeConfig.SemanticMemoryDefinition
		episodicMemoryDef = scopeConfig.EpisodicMemoryDefinition
	}

	promptContent, err := prompts.DefaultApplier().Apply("memory_analysis_prompt", map[string]any{
		"history":                    history,
		"conversation":               conversation,
		"has_variable":               hasVariable,
		"variables_define_template":  string(variablesDescriptionJSON),
		"variables_output_template":  string(variablesOutputFormatJSON),
		"forbidden_variables":        fbVariables,
		"max_message_token":          summaryMaxToken,
		"user_profile_definition":    userProfileDef,
		"semantic_memory_definition": semanticMemoryDef,
		"episodic_memory_definition": episodicMemoryDef,
	})
	if err != nil {
		return nil, fmt.Errorf("加载记忆分析提示词模板失败: %w", err)
	}

	// 步骤 6：构造消息（对齐 Python: model_input = [{"role": "user", "content": prompt_content}]）
	formatted := prompt.NewPromptTemplate("memory_analysis_prompt_user", promptContent)
	modelMessages, err := formatted.ToMessages()
	if err != nil {
		return nil, fmt.Errorf("构造记忆分析消息失败: %w", err)
	}
	msgsParam := model_clients.NewMessagesParam(modelMessages...)

	// 步骤 7：LLM 调用 + JSON 解析（对齐 Python: for attempt in range(retries)）
	parser := output_parsers.NewJsonOutputParser()

	for attempt := 0; attempt < retries; attempt++ {
		response, invokeErr := baseChatModel.Invoke(ctx, msgsParam,
			model_clients.WithInvokeOutputParser(parser))
		if invokeErr != nil {
			// 对齐 Python: invoke 异常向上传播（不在 except JSONDecodeError 内）
			return nil, fmt.Errorf("记忆分析 LLM 调用失败: %w", invokeErr)
		}

		parsedResult := response.ParserContent
		if parsedResult == nil {
			if attempt < retries-1 {
				continue
			}
			// 对齐 Python: 全部重试失败
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", "JSON解析结果为nil").
				Msg("记忆分析模型输出格式错误")
			return &MemoryAnalyzerResult{}, nil
		}

		resultMap, ok := parsedResult.(map[string]any)
		if !ok {
			if attempt < retries-1 {
				continue
			}
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", fmt.Sprintf("JSON解析结果类型非map[string]any，实际: %T", parsedResult)).
				Msg("记忆分析模型输出格式错误")
			return &MemoryAnalyzerResult{}, nil
		}

		// 步骤 8：解析结果为 MemoryAnalyzerResult（对齐 Python: MemoryAnalyzerResult.model_validate(res)）
		analyzeResult, parseErr := mapToMemoryAnalyzerResult(resultMap)
		if parseErr != nil {
			if attempt < retries-1 {
				continue
			}
			// 对齐 Python: except json.JSONDecodeError → 全部重试失败返回空 MemoryAnalyzerResult
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", parseErr.Error()).
				Msg("记忆分析模型输出格式错误")
			return &MemoryAnalyzerResult{}, nil
		}

		// 步骤 9：enable 检查（对齐 Python: if not memory_config.enable_long_term_mem or not memory_config.enable_summary_memory: analyze_result.summary = ""）
		if !memoryConfig.EnableLongTermMem || !memoryConfig.EnableSummaryMemory {
			analyzeResult.Summary = ""
		}

		return analyzeResult, nil
	}

	// 对齐 Python: return MemoryAnalyzerResult()（全部重试失败返回空结果）
	return &MemoryAnalyzerResult{}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mapToMemoryAnalyzerResult 将解析后的 map 转换为 MemoryAnalyzerResult。
//
// 对齐 Python: MemoryAnalyzerResult.model_validate(res)
func mapToMemoryAnalyzerResult(m map[string]any) (*MemoryAnalyzerResult, error) {
	result := &MemoryAnalyzerResult{}

	// has_key_information
	if v, ok := m["has_key_information"]; ok {
		switch val := v.(type) {
		case bool:
			result.HasKeyInformation = val
		default:
			// 尝试兼容 LLM 返回的字符串 "true"/"false"
			result.HasKeyInformation = fmt.Sprintf("%v", v) == "true"
		}
	}

	// summary
	if v, ok := m["summary"]; ok {
		if s, ok := v.(string); ok {
			result.Summary = s
		}
	}

	// variables
	if v, ok := m["variables"]; ok {
		varList, ok := v.([]any)
		if !ok {
			return result, nil
		}
		result.Variables = make([]VariableResult, 0, len(varList))
		for _, item := range varList {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			vr := VariableResult{}
			if key, ok := itemMap["variable_key"].(string); ok {
				vr.VariableKey = key
			}
			if val, ok := itemMap["variable_value"].(string); ok {
				vr.VariableValue = val
			}
			result.Variables = append(result.Variables, vr)
		}
	}

	return result, nil
}

