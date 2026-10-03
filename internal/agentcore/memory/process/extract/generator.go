package extract

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/search"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// semanticValidationMatch 语义校验匹配结果，记录 ID 和内容。
//
// 对齐 Python: _semantic_validation 返回的 list[tuple[str, str]] 中的每个元组
type semanticValidationMatch struct {
	// ID 匹配的记忆 ID
	ID string
	// Mem 匹配的记忆内容
	Mem string
}

// Generator 记忆生成编排器，串联 MemoryAnalyzer → LongTermMemoryExtractor → 语义校验 → 记忆单元输出。
//
// Python: openjiuwen/core/memory/process/extract/generation.py (Generator)
type Generator struct {
	// dataIdGenerator 记忆 ID 生成器
	dataIdGenerator *mem_model.DataIdManager
	// searchManager 搜索管理器（可选，处理 UPDATE/DELETE 指令时使用）
	searchManager *search.SearchManager
}

// GenAllMemoryParams 记忆生成编排器的全部参数。
//
// Python 的 gen_all_memory 使用 **kwargs 接收参数，
// Go 无法用 **kwargs，将所有参数收进此结构体，方法签名更清晰。
//
// Python: Generator.gen_all_memory(**kwargs)
type GenAllMemoryParams struct {
	// Messages 当前轮次消息列表
	Messages []llmschema.BaseMessage
	// HistoryMessages 历史消息列表
	HistoryMessages []llmschema.BaseMessage
	// BaseModel 基础聊天模型
	BaseModel *llm.Model
	// MemoryConfig Agent 记忆配置
	MemoryConfig *config.AgentMemoryConfig
	// EngineConfig 记忆引擎配置（可选，提取 forbiddenVariables 和 summaryMaxToken）
	EngineConfig *config.MemoryEngineConfig
	// ScopeConfig 记忆作用域配置（可选）
	ScopeConfig *config.MemoryScopeConfig
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// ForbiddenVariables 禁用变量名列表（逗号分隔，对齐 Python forbidden_variables）
	ForbiddenVariables string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
	// SummaryMaxToken 单轮历史摘要最大 token 数
	SummaryMaxToken int
	// SemanticStore 语义存储（用于搜索旧记忆做语义验证）
	// TODO(#7.27): 回填为具体类型——当前 Go 版 SearchManager.Search 不再接收 semantic_store 参数（已内置），
	// 此字段仅传递到 MemoryOperationParams 供 7.27 回填时使用。
	SemanticStore any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// categoryToClass 记忆类别到 MemoryType 的映射。
// Python: category_to_class
var categoryToClass = map[string]mem_model.MemoryType{
	"user_profile":    mem_model.MemoryTypeUserProfile,
	"semantic_memory": mem_model.MemoryTypeSemanticMemory,
	"episodic_memory": mem_model.MemoryTypeEpisodicMemory,
}

// operationStrToEnum 操作字符串到 OperationType 的映射。
// Python: operation_str_to_enum = {op.value: op for op in OperationType}
var operationStrToEnum = map[string]mem_model.OperationType{
	mem_model.OperationTypeAdd.String():    mem_model.OperationTypeAdd,
	mem_model.OperationTypeUpdate.String(): mem_model.OperationTypeUpdate,
	mem_model.OperationTypeDelete.String(): mem_model.OperationTypeDelete,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGenerator 创建记忆生成编排器。
//
// Python: Generator.__init__(data_id_generator, search_manager)
func NewGenerator(dataIdGenerator *mem_model.DataIdManager, searchManager *search.SearchManager) *Generator {
	return &Generator{
		dataIdGenerator: dataIdGenerator,
		searchManager:   searchManager,
	}
}

// GenAllMemory 编排全部记忆生成流程。
//
// 流程：
//  1. 验证必填参数（messages, memoryConfig, userID, scopeID, baseModel）
//  2. 构建 ExtractMemoryParams
//  3. 调 Analyze（包级函数）
//  4. 调 processExtractedData 处理变量
//  5. 按 enable 标志判断是否继续
//  6. 调 processSummaryData 生成摘要单元
//  7. 按 has_key_information 判断是否继续
//  8. 调 categoriesToMemoryUnit（含异常捕获）
//  9. 按 fragment_enable 过滤
//  10. 返回 map[string][]mem_model.MemoryUnit
//
// Python: Generator.gen_all_memory
func (g *Generator) GenAllMemory(
	ctx context.Context,
	params *GenAllMemoryParams,
) (map[string][]mem_model.MemoryUnit, error) {
	// 步骤 1：验证必填参数（对齐 Python: if not all([messages, config, user_id, scope_id, model])）
	if len(params.Messages) == 0 || params.MemoryConfig == nil || params.UserID == "" || params.ScopeID == "" || params.BaseModel == nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_PROCESS").
			Str("user_id", params.UserID).
			Str("scope_id", params.ScopeID).
			Msg("Messages, config, user_id, scope_id, model are required parameters")
		return map[string][]mem_model.MemoryUnit{}, nil
	}

	// 步骤 2：构建 ExtractMemoryParams（对齐 Python: extract_memory_params = ExtractMemoryParams(...)）
	extractMemoryParams := &ExtractMemoryParams{
		UserID:          params.UserID,
		ScopeID:         params.ScopeID,
		Messages:        params.Messages,
		HistoryMessages: params.HistoryMessages,
		BaseChatModel:   params.BaseModel,
	}

	allMemoryResults := map[string][]mem_model.MemoryUnit{}

	// 步骤 3：调 Analyze（对齐 Python: memory_analyze_res = await MemoryAnalyzer.analyze(...)）
	memoryAnalyzeRes, err := Analyze(
		ctx,
		params.Messages,
		params.HistoryMessages,
		params.BaseModel,
		params.MemoryConfig,
		params.SummaryMaxToken,
		params.ScopeConfig,
		params.ForbiddenVariables,
		3,
	)
	if err != nil {
		return nil, fmt.Errorf("记忆分析失败: %w", err)
	}
	// 分析结果为 nil 时（messages 为空），不应走到这里（前面已检查），但防御性处理
	if memoryAnalyzeRes == nil {
		return allMemoryResults, nil
	}

	// 步骤 4：调 processExtractedData 处理变量（对齐 Python: variable_units = self._process_extracted_data(...)）
	variableUnits := g.processExtractedData(params.UserID, memoryAnalyzeRes.Variables)
	for _, unit := range variableUnits {
		memType := unit.MemType.String()
		if _, ok := allMemoryResults[memType]; !ok {
			allMemoryResults[memType] = []mem_model.MemoryUnit{}
		}
		allMemoryResults[memType] = append(allMemoryResults[memType], unit)
	}

	// 步骤 5：按 enable 标志判断是否继续（对齐 Python: if not config.enable_long_term_mem）
	if !params.MemoryConfig.EnableLongTermMem {
		logger.Info(logComponent).
			Str("event_type", "MEMORY_PROCESS").
			Str("user_id", params.UserID).
			Str("scope_id", params.ScopeID).
			Msg("未启用长期记忆")
		return allMemoryResults, nil
	}

	// 步骤 6：调 processSummaryData 生成摘要单元（对齐 Python: if config.enable_summary_memory: summary_unit = ...）
	if params.MemoryConfig.EnableSummaryMemory {
		summaryUnit := g.processSummaryData(params.UserID, params.MessageMemID, memoryAnalyzeRes.Summary, params.Timestamp)
		summaryType := summaryUnit.MemType.String()
		if _, ok := allMemoryResults[summaryType]; !ok {
			allMemoryResults[summaryType] = []mem_model.MemoryUnit{}
		}
		allMemoryResults[summaryType] = append(allMemoryResults[summaryType], summaryUnit)
	}

	// 步骤 7：按 has_key_information 判断是否继续（对齐 Python: if not memory_analyze_res.has_key_information）
	if !memoryAnalyzeRes.HasKeyInformation {
		return allMemoryResults, nil
	}

	// fragment_enable 映射（对齐 Python: fragment_enable = {MemoryType.XXX.value: config.enable_xxx}）
	fragmentEnable := map[string]bool{
		mem_model.MemoryTypeUserProfile.String():    params.MemoryConfig.EnableUserProfile,
		mem_model.MemoryTypeSemanticMemory.String(): params.MemoryConfig.EnableSemanticMemory,
		mem_model.MemoryTypeEpisodicMemory.String(): params.MemoryConfig.EnableEpisodicMemory,
	}

	// 步骤 8：调 categoriesToMemoryUnit（对齐 Python: try: merged_units = await self._categories_to_memory_unit(...)）
	mergedUnits, err := g.categoriesToMemoryUnit(ctx, extractMemoryParams, params.MessageMemID, params.Timestamp, params.ScopeConfig, params.SemanticStore)
	if err != nil {
		// 对齐 Python: except AttributeError/ValueError/BaseException → 记日志并返回已有结果
		logger.Warn(logComponent).
			Str("event_type", "MEMORY_PROCESS").
			Str("user_id", params.UserID).
			Str("scope_id", params.ScopeID).
			Str("exception", err.Error()).
			Msg("获取冲突信息时发生异常")
		return allMemoryResults, nil
	}

	// 步骤 9：按 fragment_enable 过滤（对齐 Python: if fragment_enable.get(mem_type, False)）
	for _, unit := range mergedUnits {
		memType := unit.GetMemType().String()
		if fragmentEnable[memType] {
			if _, ok := allMemoryResults[memType]; !ok {
				allMemoryResults[memType] = []mem_model.MemoryUnit{}
			}
			allMemoryResults[memType] = append(allMemoryResults[memType], unit)
		}
	}

	logger.Info(logComponent).
		Str("event_type", "MEMORY_PROCESS").
		Str("user_id", params.UserID).
		Str("scope_id", params.ScopeID).
		Msg("记忆单元生成成功")

	return allMemoryResults, nil
}

// categoriesToMemoryUnit 调用长期记忆提取器，分离指令式记忆和碎片记忆。
//
// Python: Generator._categories_to_memory_unit
func (g *Generator) categoriesToMemoryUnit(
	ctx context.Context,
	extractMemoryParams *ExtractMemoryParams,
	messageMemID string,
	timestamp string,
	scopeConfig *config.MemoryScopeConfig,
	semanticStore any,
) ([]mem_model.MemoryUnit, error) {
	var memoryUnits []mem_model.MemoryUnit

	// 调 ExtractLongTermMemory（对齐 Python: memory_dict = await LongTermMemoryExtractor.extract_long_term_memory(...)）
	memoryDict, err := ExtractLongTermMemory(ctx, extractMemoryParams, timestamp, scopeConfig, 3)
	if err != nil {
		return nil, err
	}

	// 处理指令式记忆（对齐 Python: if memory_dict.get("has_explict_instruct", False)）
	if hasExplicit, _ := memoryDict["has_explict_instruct"].(bool); hasExplicit {
		instructMemories, _ := memoryDict["instruct_memories"].([]any)
		if len(instructMemories) > 0 {
			memoryOperationParams := &MemoryOperationParams{
				UserID:        extractMemoryParams.UserID,
				ScopeID:       extractMemoryParams.ScopeID,
				MessageMemID:  messageMemID,
				Timestamp:     timestamp,
				BaseModel:     extractMemoryParams.BaseChatModel,
				SemanticStore: semanticStore,
			}
			instructUnits := g.handleMemoryWithInstruct(ctx, memoryOperationParams, instructMemories)
			for _, u := range instructUnits {
				memoryUnits = append(memoryUnits, u)
			}
		}
	}

	// 调 getFragmentMemoryUnit（对齐 Python: memory_units.extend(await self._get_fragment_memory_unit(...))）
	fragmentUnits, err := g.getFragmentMemoryUnit(extractMemoryParams.UserID, messageMemID, memoryDict, timestamp)
	if err != nil {
		return nil, err
	}
	for _, u := range fragmentUnits {
		memoryUnits = append(memoryUnits, u)
	}

	return memoryUnits, nil
}

// processExtractedData 将 VariableResult 列表转换为 VariableUnit 列表。
//
// 对齐 Python: Generator._process_extracted_data（@staticmethod）
// 注意：Python VariableUnit 无 mem_id 字段，但 Go 的 VariableUnit 嵌入了 BaseMemoryUnit（有 MemID），
// 下游写入依赖 MemID，因此需要为每个 VariableUnit 生成唯一 ID。
func (g *Generator) processExtractedData(userID string, variableResults []VariableResult) []*mem_model.VariableUnit {
	variableUnits := make([]*mem_model.VariableUnit, 0, len(variableResults))
	for _, tmpData := range variableResults {
		// 对齐 Python: if not tmp_data.variable_value: continue
		if tmpData.VariableValue == "" {
			continue
		}
		memID := g.dataIdGenerator.GenerateNextID(userID)
		variableUnits = append(variableUnits, &mem_model.VariableUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{
				MemType: mem_model.MemoryTypeVariable,
				MemID:   memID,
			},
			VariableName: tmpData.VariableKey,
			VariableMem:  tmpData.VariableValue,
		})
	}
	return variableUnits
}

// processSummaryData 生成摘要记忆单元。
//
// 对齐 Python: Generator._process_summary_data
func (g *Generator) processSummaryData(userID, messageMemID, summary, timestamp string) *mem_model.SummaryUnit {
	memID := g.dataIdGenerator.GenerateNextID(userID)
	return &mem_model.SummaryUnit{
		BaseMemoryUnit: mem_model.BaseMemoryUnit{
			MemType: mem_model.MemoryTypeSummary,
			MemID:   memID,
		},
		Summary:      summary,
		MessageMemID: messageMemID,
		Timestamp:    timestamp,
	}
}

// getFragmentMemoryUnit 从提取的记忆字典中生成碎片记忆单元。
//
// 对齐 Python: Generator._get_fragment_memory_unit
func (g *Generator) getFragmentMemoryUnit(
	userID string,
	messageMemID string,
	memoryDict map[string]any,
	timestamp string,
) ([]*mem_model.FragmentMemoryUnit, error) {
	fragmentMemUnits := make([]*mem_model.FragmentMemoryUnit, 0)

	// 对齐 Python: for fragment_type, fragment_memories in memory_dict.items():
	for fragmentType, fragmentMemories := range memoryDict {
		memType, ok := categoryToClass[fragmentType]
		if !ok {
			// 对齐 Python: mem_type = category_to_class.get(fragment_type, None); if not mem_type: continue
			continue
		}

		fragmentList, ok := fragmentMemories.([]any)
		if !ok {
			continue
		}

		for _, content := range fragmentList {
			// 对齐 Python: if not isinstance(mem_content, str): content = mem_content.get("content"); ...
			memContent := ""
			switch v := content.(type) {
			case string:
				memContent = v
			case map[string]any:
				// 尝试 .content 字段
				if c, ok := v["content"].(string); ok && c != "" {
					memContent = c
				} else {
					memContent = fmt.Sprint(v)
				}
			default:
				memContent = fmt.Sprint(v)
			}

			memID := g.dataIdGenerator.GenerateNextID(userID)
			fragmentMemUnits = append(fragmentMemUnits, &mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: memType,
					MemID:   memID,
				},
				Content:       memContent,
				MessageMemID:  messageMemID,
				Timestamp:     timestamp,
				OperationType: mem_model.OperationTypeAdd,
			})
		}
	}

	return fragmentMemUnits, nil
}

// processProactiveMemoryData 从主动记忆列表中生成碎片记忆单元（仅处理 ADD 操作）。
//
// 对齐 Python: Generator._process_proactive_memory_data
func (g *Generator) processProactiveMemoryData(
	userID string,
	messageMemID string,
	memoryList []any,
	timestamp string,
) ([]*mem_model.FragmentMemoryUnit, error) {
	fragmentMemUnits := make([]*mem_model.FragmentMemoryUnit, 0)

	for _, item := range memoryList {
		// 对齐 Python: if not isinstance(mem_dict, dict): continue
		memDict, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// 对齐 Python: mem_instruct = str(mem_dict.get("mem_instruct", "")).lower()
		memInstruct := strings.ToLower(fmt.Sprint(memDict["mem_instruct"]))
		operationType, ok := operationStrToEnum[memInstruct]
		if !ok {
			continue
		}

		// 对齐 Python: if operation_type != OperationType.ADD: continue
		if operationType != mem_model.OperationTypeAdd {
			continue
		}

		// 对齐 Python: fragment_type = mem_dict.get("mem_type"); mem_type = category_to_class.get(fragment_type, None)
		fragmentType := fmt.Sprint(memDict["mem_type"])
		memType, ok := categoryToClass[fragmentType]
		if !ok {
			continue
		}

		// 对齐 Python: mem_content = mem_dict.get("mem_content"); if not mem_content: continue
		memContentRaw, ok := memDict["mem_content"]
		if !ok || memContentRaw == nil {
			continue
		}

		// 对齐 Python: if not isinstance(mem_content, str): content = mem_content.get("mem_content"); ...
		memContent := ""
		switch v := memContentRaw.(type) {
		case string:
			memContent = v
		case map[string]any:
			// 尝试 .mem_content 字段
			if c, ok := v["mem_content"].(string); ok && c != "" {
				memContent = c
			} else {
				memContent = fmt.Sprint(v)
			}
		default:
			memContent = fmt.Sprint(v)
		}

		memID := g.dataIdGenerator.GenerateNextID(userID)
		fragmentMemUnits = append(fragmentMemUnits, &mem_model.FragmentMemoryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{
				MemType: memType,
				MemID:   memID,
			},
			Content:       memContent,
			MessageMemID:  messageMemID,
			Timestamp:     timestamp,
			OperationType: operationType,
		})
	}

	return fragmentMemUnits, nil
}

// semanticValidation 对每条候选记忆逐条串行 LLM 调用，验证与旧记忆的语义一致性。
//
// 对齐 Python: Generator._semantic_validation
// 返回语义匹配的 (ID, Mem) 对列表。
func (g *Generator) semanticValidation(
	ctx context.Context,
	obtainedMems []*storeindex.MemorySearchResult,
	oldMem string,
	baseChatModel *llm.Model,
) []semanticValidationMatch {
	retIDs := make([]semanticValidationMatch, 0)

	for _, obtainedMem := range obtainedMems {
		// 对齐 Python: prompt_content = PromptApplier().apply("semantic_validation", {...})
		obtainedMemContent := obtainedMem.Doc.Text
		obtainedMemID := obtainedMem.Doc.ID

		promptContent, err := prompts.DefaultApplier().Apply("semantic_validation", map[string]any{
			"obtained_mem": obtainedMemContent,
			"old_mem":      oldMem,
		})
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", err.Error()).
				Msg("语义校验提示词加载失败")
			continue
		}

		// 对齐 Python: model_input = [{"role": "user", "content": prompt_content}]
		modelMessages, err := prompt.NewPromptTemplate("semantic_validation_user", promptContent).ToMessages()
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", err.Error()).
				Msg("语义校验消息构造失败")
			continue
		}
		msgsParam := model_clients.NewMessagesParam(modelMessages...)

		response, invokeErr := baseChatModel.Invoke(ctx, msgsParam)
		if invokeErr != nil {
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", invokeErr.Error()).
				Msg("语义校验 LLM 调用失败")
			continue
		}

		// 对齐 Python: if "CORRECT" in response.content.upper() and "WRONG" not in response.content.upper()
		upperContent := strings.ToUpper(response.Content.Text())
		if strings.Contains(upperContent, "CORRECT") && !strings.Contains(upperContent, "WRONG") {
			logger.Debug(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("old_mem", oldMem).
				Str("obtained_mem", obtainedMemContent).
				Str("result", "CORRECT").
				Msg("语义校验结果")
			retIDs = append(retIDs, semanticValidationMatch{
				ID:  obtainedMemID,
				Mem: obtainedMemContent,
			})
		} else {
			logger.Debug(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("old_mem", oldMem).
				Str("obtained_mem", obtainedMemContent).
				Str("result", "WRONG").
				Msg("语义校验结果")
		}
	}

	return retIDs
}

// handleMemoryWithInstruct 处理指令式记忆，分离 UPDATE 和 DELETE 操作。
//
// 对齐 Python: Generator._handle_memory_with_instruct
func (g *Generator) handleMemoryWithInstruct(
	ctx context.Context,
	memoryOperationParams *MemoryOperationParams,
	memoryList []any,
) []*mem_model.FragmentMemoryUnit {
	updateMemories := make([]map[string]any, 0)
	deleteMemories := make([]map[string]any, 0)

	// 对齐 Python: for mem_dict in memory_list: ...
	for _, item := range memoryList {
		memDict, ok := item.(map[string]any)
		if !ok {
			continue
		}
		memInstruct := strings.ToLower(fmt.Sprint(memDict["mem_instruct"]))
		opType, ok := operationStrToEnum[memInstruct]
		if !ok {
			continue
		}
		switch opType {
		case mem_model.OperationTypeUpdate:
			updateMemories = append(updateMemories, memDict)
		case mem_model.OperationTypeDelete:
			deleteMemories = append(deleteMemories, memDict)
		}
	}

	retMemories := make([]*mem_model.FragmentMemoryUnit, 0)

	// 对齐 Python: ret_memories.extend(await self._process_memory_operations(..., operation_type=OperationType.UPDATE))
	updateUnits := g.processMemoryOperations(ctx, memoryOperationParams, updateMemories, mem_model.OperationTypeUpdate)
	retMemories = append(retMemories, updateUnits...)

	// 对齐 Python: ret_memories.extend(await self._process_memory_operations(..., operation_type=OperationType.DELETE))
	deleteUnits := g.processMemoryOperations(ctx, memoryOperationParams, deleteMemories, mem_model.OperationTypeDelete)
	retMemories = append(retMemories, deleteUnits...)

	return retMemories
}

// processMemoryOperations 处理记忆更新或删除操作（含语义校验）。
//
// 对齐 Python: Generator._process_memory_operations
func (g *Generator) processMemoryOperations(
	ctx context.Context,
	memoryOperationParams *MemoryOperationParams,
	memoryDicts []map[string]any,
	operationType mem_model.OperationType,
) []*mem_model.FragmentMemoryUnit {
	retMemories := make([]*mem_model.FragmentMemoryUnit, 0)

	// 防御性检查：searchManager 为 nil 时无法搜索，直接返回空
	if g.searchManager == nil {
		logger.Warn(logComponent).
			Str("event_type", "MEMORY_PROCESS").
			Msg("searchManager 为 nil，跳过记忆操作处理")
		return retMemories
	}

	for _, memDict := range memoryDicts {
		// 对齐 Python: old_mem = mem_dict.get("old_mem"); if not old_mem: continue
		oldMem, _ := memDict["old_mem"].(string)
		if oldMem == "" {
			continue
		}

		// 对齐 Python: params = SearchParams(query=old_mem, scope_id=..., top_k=1, user_id=...)
		params := &search.SearchParams{
			Query:   oldMem,
			ScopeID: memoryOperationParams.ScopeID,
			TopK:    1,
			UserID:  memoryOperationParams.UserID,
			SearchType: []string{
				mem_model.MemoryTypeUserProfile.String(),
				mem_model.MemoryTypeEpisodicMemory.String(),
				mem_model.MemoryTypeSemanticMemory.String(),
			},
		}

		// 对齐 Python: search_data = await self.search_manager.search(params, semantic_store=...)
		searchData, err := g.searchManager.Search(ctx, params)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", err.Error()).
				Msg("搜索旧记忆失败")
			continue
		}

		// 对齐 Python: search_data = sorted(search_data, key=lambda x: x.get("score", 0.0), reverse=True)
		// Go 版 SearchManager.Search 返回 []*storeindex.MemorySearchResult，按 Score 降序排序
		sortSearchResultsByScore(searchData)

		// 对齐 Python: obtained_mem = [search_data[0]] if search_data else []
		var obtainedMem []*storeindex.MemorySearchResult
		if len(searchData) > 0 {
			obtainedMem = []*storeindex.MemorySearchResult{searchData[0]}
		}
		if len(obtainedMem) == 0 {
			continue
		}

		// 对齐 Python: mem_ids = await self._semantic_validation(obtained_mems=obtained_mem, old_mem=old_mem, base_chat_model=...)
		memIDs := g.semanticValidation(ctx, obtainedMem, oldMem, memoryOperationParams.BaseModel)
		for _, match := range memIDs {
			// 对齐 Python: mem_type_str = str(mem_dict.get("mem_type")).lower()
			memTypeStr := strings.ToLower(fmt.Sprint(memDict["mem_type"]))
			memType, ok := categoryToClass[memTypeStr]
			if !ok {
				continue
			}

			retMemories = append(retMemories, &mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: memType,
					MemID:   match.ID,
				},
				Content:       fmt.Sprint(memDict["mem_content"]),
				MessageMemID:  memoryOperationParams.MessageMemID,
				Timestamp:     memoryOperationParams.Timestamp,
				OperationType: operationType,
			})
		}
	}

	return retMemories
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortSearchResultsByScore 按分数降序排序搜索结果。
// 对齐 Python: sorted(search_data, key=lambda x: x.get("score", 0.0), reverse=True)
func sortSearchResultsByScore(results []*storeindex.MemorySearchResult) {
	if len(results) <= 1 {
		return
	}
	// 简单插入排序（搜索结果通常很少）
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
