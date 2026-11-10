package runtime

import (
	"errors"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeModelDescriptor 安全模型事实，其 selection_key 映射到 Adapter 全局索引。
// Python: model_catalog.py:30-54 RuntimeModelDescriptor
type RuntimeModelDescriptor struct {
	// SelectionKey 选择键（`model_name#global_index`）
	SelectionKey string
	// DisplayName 展示名（alias 优先，否则 model_name）
	DisplayName string
	// ModelName 模型名
	ModelName string
	// Provider 提供商标识
	Provider string
	// ReasoningLevel 推理等级（false 归一为 "off"）
	ReasoningLevel string
	// IsDefault 是否默认（agentos 来源一律为假）
	IsDefault bool
	// IsAgentOS 是否 agentos 来源
	IsAgentOS bool
	// IsCurrent 是否当前选中
	IsCurrent bool
}

// ModelCatalogResult 已配置的聊天模型加上生效的请求选择。
// Python: model_catalog.py:57-71 ModelCatalogResult
type ModelCatalogResult struct {
	// Models 模型列表（含 IsCurrent 标记）
	Models []RuntimeModelDescriptor
	// CurrentSelection 当前选择键
	CurrentSelection string
	// CurrentDisplayName 当前选择展示名
	CurrentDisplayName string
}

// ModelSelectionResult 一次已校验的、请求作用域的模型选择。
// Python: model_catalog.py:74-80 ModelSelectionResult
type ModelSelectionResult struct {
	// Model 选中的模型
	Model RuntimeModelDescriptor
	// SessionID 会话标识
	SessionID string
	// Persisted 是否已持久化
	Persisted bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrModelSelectionRequired 模型选择缺失（Python code=BAD_REQUEST）
	// Python: model_catalog.py:188 raise ModelCatalogError("model selection is required", code="BAD_REQUEST")
	ErrModelSelectionRequired = errors.New("model selection is required")
	// ErrModelMultimodalOnly 视频/音频/视觉为多模态专用档（Python code=BAD_REQUEST）
	// Python: model_catalog.py:190-193
	ErrModelMultimodalOnly = errors.New("video, audio, and vision are multimodal-only model profiles")
	// ErrModelAmbiguous 多个模型匹配（Python code=BAD_REQUEST）
	// Python: model_catalog.py:202-205
	ErrModelAmbiguous = errors.New("multiple models match; use a selection key from the catalog")
	// ErrModelNotFound 模型不存在（Python code=NOT_FOUND）
	// Python: model_catalog.py:206
	ErrModelNotFound = errors.New("model not found")
)

// reservedMultimodalNames 多模态专用档名，不作为聊天模型出现在目录中。
// Python: model_catalog.py:11 _RESERVED_MULTIMODAL_NAMES
var reservedMultimodalNames = map[string]struct{}{
	"video": {}, "audio": {}, "vision": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildModelCatalog 构建安全目录，同时保留原始全局列表索引。
// Python: model_catalog.py:145-178 build_model_catalog
//
// entries 可能是一次性迭代器：恰好消费一次；被跳过的条目不改变后续
// selection key 里的全局下标。
func BuildModelCatalog(entries []any, currentSelection string) ModelCatalogResult {
	models := make([]RuntimeModelDescriptor, 0, len(entries))
	for globalIndex, entry := range entries {
		// Python:157-158 if not isinstance(entry, Mapping): continue
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		descriptor, ok := modelDescriptor(globalIndex, mapping)
		if ok {
			models = append(models, descriptor)
		}
	}
	// Python:163-164 if not models: return ModelCatalogResult(models=())
	if len(models) == 0 {
		return ModelCatalogResult{Models: []RuntimeModelDescriptor{}}
	}

	// Python:166-167 requested 非空才尝试解析
	requested := safeText(currentSelection)
	var current *RuntimeModelDescriptor
	if requested != "" {
		current = resolveFromCatalog(models, requested)
	}
	// Python:168-169 解析不到则取默认或首项
	if current == nil {
		fallback := -1
		for index := range models {
			if models[index].IsDefault {
				fallback = index
				break
			}
		}
		if fallback < 0 {
			fallback = 0
		}
		current = &models[fallback]
	}

	// Python:170-173 replace(item, is_current=item.selection_key == current.selection_key)
	marked := make([]RuntimeModelDescriptor, 0, len(models))
	for _, item := range models {
		item.IsCurrent = item.SelectionKey == current.SelectionKey
		marked = append(marked, item)
	}
	return ModelCatalogResult{
		Models:             marked,
		CurrentSelection:   current.SelectionKey,
		CurrentDisplayName: current.DisplayName,
	}
}

// ResolveModelSelection 解析精确键或无歧义的人类可读模型引用。
// Python: model_catalog.py:181-207 resolve_model_selection
func ResolveModelSelection(catalog ModelCatalogResult, requested string) (RuntimeModelDescriptor, error) {
	// Python:186-188 空选择
	target := safeText(requested)
	if target == "" {
		return RuntimeModelDescriptor{}, ErrModelSelectionRequired
	}
	// Python:189-193 多模态专用档
	if _, reserved := reservedMultimodalNames[strings.ToLower(target)]; reserved {
		return RuntimeModelDescriptor{}, ErrModelMultimodalOnly
	}
	// Python:194 目录解析
	selected := resolveFromCatalog(catalog.Models, target)
	if selected == nil {
		// Python:196-205 歧义别名
		ambiguous := 0
		for _, item := range catalog.Models {
			if item.DisplayName == target && item.DisplayName != item.ModelName {
				ambiguous++
			}
		}
		if ambiguous > 1 {
			return RuntimeModelDescriptor{}, ErrModelAmbiguous
		}
		return RuntimeModelDescriptor{}, ErrModelNotFound
	}
	// Python:207 replace(selected, is_current=True)
	out := *selected
	out.IsCurrent = true
	return out, nil
}

// BuildModelCatalogFromEntries 把映射列表转为 []any 后构建（便利包装）。
// Python: model_catalog.py:145 entries: Iterable[Mapping[str, Any] | object]
func BuildModelCatalogFromEntries(entries []map[string]any, currentSelection string) ModelCatalogResult {
	converted := make([]any, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, entry)
	}
	return BuildModelCatalog(converted, currentSelection)
}

// RuntimeModelDescriptorToDict 返回可序列化的安全投影（不含连接密钥）。
// Python: model_catalog.py:43-54 to_dict
func RuntimeModelDescriptorToDict(descriptor RuntimeModelDescriptor) map[string]any {
	return map[string]any{
		"selection_key":   descriptor.SelectionKey,
		"display_name":    descriptor.DisplayName,
		"model_name":      descriptor.ModelName,
		"provider":        descriptor.Provider,
		"reasoning_level": descriptor.ReasoningLevel,
		"is_default":      descriptor.IsDefault,
		"is_agentos":      descriptor.IsAgentOS,
		"is_current":      descriptor.IsCurrent,
	}
}

// ModelCatalogResultToDict 返回不含模型连接密钥的目录序列化。
// Python: model_catalog.py:65-71 to_dict
func ModelCatalogResultToDict(result ModelCatalogResult) map[string]any {
	models := make([]map[string]any, 0, len(result.Models))
	for _, item := range result.Models {
		models = append(models, RuntimeModelDescriptorToDict(item))
	}
	return map[string]any{
		"models":               models,
		"current_selection":    result.CurrentSelection,
		"current_display_name": result.CurrentDisplayName,
	}
}

// ResolveConfiguredModelEntry 用 Adapter 的全局索引语法解析原始条目。
// Python: model_catalog.py:210-261 _resolve_configured_model_entry
//
// 注意：本函数可能返回凭据，与安全描述符刻意分离，绝不可作为目录输出序列化。
func ResolveConfiguredModelEntry(entries []any, requested string) (map[string]any, bool) {
	target := safeText(requested)
	if target == "" {
		return nil, false
	}

	// Python:224-241 带 "#" 的全局索引语法
	if strings.Contains(target, "#") {
		index := strings.LastIndex(target, "#")
		bareName := target[:index]
		rawIndex := target[index+1:]
		if bareName == "" {
			return nil, false
		}
		globalIndex, err := strconv.Atoi(rawIndex)
		if err != nil {
			return nil, false
		}
		if globalIndex < 0 || globalIndex >= len(entries) {
			return nil, false
		}
		entry, ok := entries[globalIndex].(map[string]any)
		if !ok {
			return nil, false
		}
		clientConfig, ok := entry["model_client_config"].(map[string]any)
		if !ok {
			return nil, false
		}
		if safeTextFromAny(clientConfig["model_name"]) != bareName {
			return nil, false
		}
		return entry, true
	}

	// Python:243-255 按 model_name 匹配，优先 is_default
	named := make([]map[string]any, 0)
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		clientConfig, ok := mapping["model_client_config"].(map[string]any)
		if !ok {
			continue
		}
		if safeTextFromAny(clientConfig["model_name"]) == target {
			named = append(named, mapping)
		}
	}
	if len(named) > 0 {
		for _, entry := range named {
			if isDefault, _ := entry["is_default"].(bool); isDefault {
				return entry, true
			}
		}
		return named[0], true
	}

	// Python:256-261 别名匹配（唯一才返回）
	aliases := make([]map[string]any, 0)
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if safeTextFromAny(mapping["alias"]) == target {
			aliases = append(aliases, mapping)
		}
	}
	if len(aliases) == 1 {
		return aliases[0], true
	}
	return nil, false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeText 字符串去空白，非字符串返回空串。
// Python: model_catalog.py:83-84 _safe_text
func safeText(value string) string { return strings.TrimSpace(value) }

// safeTextFromAny 从 any 取字符串并去空白。
// Python: model_catalog.py:83-84 _safe_text
func safeTextFromAny(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// reasoningLevel 推理等级归一：false → "off"，其余取安全文本。
// Python: model_catalog.py:87-90 _reasoning_level
func reasoningLevel(value any) string {
	if flag, ok := value.(bool); ok && !flag {
		return "off"
	}
	return safeTextFromAny(value)
}

// modelDescriptor 由条目构造描述符；不合格条目返回 ok=false。
// Python: model_catalog.py:93-122 _descriptor
func modelDescriptor(globalIndex int, entry map[string]any) (RuntimeModelDescriptor, bool) {
	// Python:96-98 必须有 model_client_config 映射
	clientConfig, ok := entry["model_client_config"].(map[string]any)
	if !ok {
		return RuntimeModelDescriptor{}, false
	}
	modelName := safeTextFromAny(clientConfig["model_name"])
	alias := safeTextFromAny(entry["alias"])
	// Python:101 display_name = alias or model_name
	displayName := alias
	if displayName == "" {
		displayName = modelName
	}
	// Python:102-107 模型名为空或多模态专用档则跳过
	if modelName == "" {
		return RuntimeModelDescriptor{}, false
	}
	if _, reserved := reservedMultimodalNames[strings.ToLower(modelName)]; reserved {
		return RuntimeModelDescriptor{}, false
	}
	if _, reserved := reservedMultimodalNames[strings.ToLower(displayName)]; reserved {
		return RuntimeModelDescriptor{}, false
	}

	// Python:109-111 model_config_obj 可缺省；_source == "agentos"
	modelConfig, ok := entry["model_config_obj"].(map[string]any)
	if !ok {
		modelConfig = map[string]any{}
	}
	isAgentOS, _ := modelConfig["_source"].(string)
	agentOS := isAgentOS == "agentos"

	// Python:120 is_default = bool(entry.get("is_default")) and not is_agentos
	rawDefault, _ := entry["is_default"].(bool)
	isDefault := rawDefault && !agentOS

	return RuntimeModelDescriptor{
		// Python:112-115 selection_key=f"{model_name}#{global_index}"
		// Agent Adapter 把后缀解释为 get_default_models() 完整序列中的位置，
		// 而不是同名出现次数。
		SelectionKey:   modelName + "#" + strconv.Itoa(globalIndex),
		DisplayName:    displayName,
		ModelName:      modelName,
		Provider:       safeTextFromAny(clientConfig["client_provider"]),
		ReasoningLevel: reasoningLevel(modelConfig["reasoning_level"]),
		IsDefault:      isDefault,
		IsAgentOS:      agentOS,
	}, true
}

// resolveFromCatalog 按 selection_key → model_name → 唯一 alias 顺序解析。
// Python: model_catalog.py:125-142 _resolve_from_catalog
func resolveFromCatalog(models []RuntimeModelDescriptor, requested string) *RuntimeModelDescriptor {
	// Python:129-131 精确 selection_key
	for index := range models {
		if models[index].SelectionKey == requested {
			return &models[index]
		}
	}

	// Python:133-135 按 model_name，优先 is_default
	named := make([]int, 0)
	for index := range models {
		if models[index].ModelName == requested {
			named = append(named, index)
		}
	}
	if len(named) > 0 {
		for _, index := range named {
			if models[index].IsDefault {
				return &models[index]
			}
		}
		return &models[named[0]]
	}

	// Python:137-142 别名匹配（唯一才返回）
	aliases := make([]int, 0)
	for index := range models {
		if models[index].DisplayName == requested &&
			models[index].DisplayName != models[index].ModelName {
			aliases = append(aliases, index)
		}
	}
	if len(aliases) == 1 {
		return &models[aliases[0]]
	}
	return nil
}
