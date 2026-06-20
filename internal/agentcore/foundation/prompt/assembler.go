package prompt

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptAssembler 模板装配器，负责编排变量求值和模板渲染。
//
// 对应 Python: openjiuwen/core/foundation/prompt/assemble/assembler.py (PromptAssembler)
//
// 职责：
//   - 解析模板内容，为每个片段创建 Variable 对象
//   - 验证外部传入的变量定义
//   - 执行变量求值和模板渲染
type PromptAssembler struct {
	templateContent any                 // string 或 []schema.BaseMessage
	prefix          string              // 占位符前缀
	suffix          string              // 占位符后缀
	formatters      []Variable          // 与 templateContent 片段一一对应，nil 表示跳过
	variables       map[string]Variable // 变量注册表
	inputKeys       []string            // 所有需要外部传入的键名（去重保序）
}

// ──────────────────────────── 构造选项 ────────────────────────────

// AssemblerOption PromptAssembler 构造选项函数。
type AssemblerOption func(*PromptAssembler)

// WithAssemblerPrefix 设置占位符前缀。
func WithAssemblerPrefix(prefix string) AssemblerOption {
	return func(a *PromptAssembler) { a.prefix = prefix }
}

// WithAssemblerSuffix 设置占位符后缀。
func WithAssemblerSuffix(suffix string) AssemblerOption {
	return func(a *PromptAssembler) { a.suffix = suffix }
}

// WithAssemblerVariable 添加自定义变量定义。
func WithAssemblerVariable(name string, variable Variable) AssemblerOption {
	return func(a *PromptAssembler) { a.variables[name] = variable }
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPromptAssembler 创建模板装配器。
//
// 参数：
//   - content: 模板内容，string 或 []schema.BaseMessage
//   - opts: 可选配置
//
// 对应 Python: PromptAssembler(prompt_template_content=..., placeholder_prefix=..., placeholder_suffix=..., **variables)
func NewPromptAssembler(content any, opts ...AssemblerOption) (*PromptAssembler, error) {
	a := &PromptAssembler{
		templateContent: content,
		prefix:          defaultPrefix,
		suffix:          defaultSuffix,
		variables:       make(map[string]Variable),
	}

	// 应用选项
	for _, opt := range opts {
		opt(a)
	}

	// 创建 formatter 列表
	if err := a.buildFormatterList(); err != nil {
		return nil, err
	}

	// 验证和补全变量
	if err := a.verifyAndCompleteVariables(); err != nil {
		return nil, err
	}

	return a, nil
}

// InputKeys 返回所有需要外部传入的键名。
// 对应 Python: PromptAssembler.input_keys (property)
func (a *PromptAssembler) InputKeys() []string {
	return a.inputKeys
}

// Assemble 执行模板装配：更新变量值，渲染模板，返回结果。
//
// 对应 Python: PromptAssembler.prompt_assemble()
//
// 逻辑：
//  1. 过滤 nil 值和无关 key
//  2. 未传入的 key 用 prefix+key+suffix 字符串填充
//  3. 更新所有 Variable
//  4. 将 Variable 求值结果回填到 templateContent
func (a *PromptAssembler) Assemble(kwargs map[string]any) (any, error) {
	if kwargs == nil {
		kwargs = make(map[string]any)
	}

	// 过滤 nil 值和无关 key
	filtered := make(map[string]any)
	for k, v := range kwargs {
		if v != nil {
			if a.isInputKey(k) {
				filtered[k] = v
			}
		}
	}

	// 未传入的 key 用 prefix+key+suffix 填充
	allKwargs := make(map[string]any)
	for _, k := range a.inputKeys {
		if v, ok := filtered[k]; ok {
			allKwargs[k] = v
		} else {
			allKwargs[k] = a.prefix + k + a.suffix
		}
	}

	// 更新所有 Variable
	if err := a.updateVariables(allKwargs); err != nil {
		return nil, err
	}

	// 渲染模板
	return a.format(), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildFormatterList 根据内容类型创建 Variable 列表。
// 对应 Python: PromptAssembler._get_formatter_list()
func (a *PromptAssembler) buildFormatterList() error {
	switch content := a.templateContent.(type) {
	case string:
		tv, err := NewTextableVariable(content, innerVarName, WithPrefix(a.prefix), WithSuffix(a.suffix))
		if err != nil {
			return err
		}
		a.formatters = []Variable{tv}

	case []schema.BaseMessage:
		a.formatters = make([]Variable, len(content))
		for i, msg := range content {
			if msg == nil {
				a.formatters[i] = nil
				continue
			}
			if msg.GetContent().IsText() {
				tv, err := NewTextableVariable(msg.GetContent().Text(), innerVarName,
					WithPrefix(a.prefix), WithSuffix(a.suffix))
				if err != nil {
					return err
				}
				a.formatters[i] = tv
			} else {
				// 多模态内容：ContentPart → map[string]any
				data := contentPartsToAny(msg.GetContent().Parts())
				if len(data) == 0 {
					a.formatters[i] = nil
					continue
				}
				dv, err := NewDictableVariable(data, innerVarName,
					WithDictablePrefix(a.prefix), WithDictableSuffix(a.suffix))
				if err != nil {
					return err
				}
				a.formatters[i] = dv
			}
		}

	default:
		return exception.NewBaseError(
			exception.StatusPromptAssemblerTemplateParamError,
			exception.WithMsg(fmt.Sprintf("unsupported template content type: %T", a.templateContent)),
		)
	}
	return nil
}

// verifyAndCompleteVariables 验证外部变量定义并补全未提供的变量。
// 对应 Python: PromptAssembler._get_variables_with_verify()
func (a *PromptAssembler) verifyAndCompleteVariables() error {
	// 收集 formatter 的 inputKeys，用于验证传入变量名的合法性
	formatterKeySet := make(map[string]struct{})
	for _, f := range a.formatters {
		if f == nil {
			continue
		}
		for _, k := range f.InputKeys() {
			formatterKeySet[k] = struct{}{}
		}
	}

	// 验证传入的变量：变量名必须在 formatter 的 inputKeys 中
	for name, variable := range a.variables {
		if _, ok := formatterKeySet[name]; !ok {
			return exception.NewBaseError(
				exception.StatusPromptAssemblerVariableInitFailed,
				exception.WithMsg(fmt.Sprintf("variable %s is not defined in the promptTemplate", name)),
			)
		}
		if variable == nil {
			return exception.NewBaseError(
				exception.StatusPromptAssemblerVariableInitFailed,
				exception.WithMsg(fmt.Sprintf("variable %s must be instantiated as a `variable` object", name)),
			)
		}
	}

	// 为未提供的 formatter inputKey 创建补全变量
	for key := range formatterKeySet {
		if _, ok := a.variables[key]; !ok {
			placeholderStr := a.prefix + key + a.suffix
			tv, err := NewTextableVariable(placeholderStr, key,
				WithPrefix(a.prefix), WithSuffix(a.suffix))
			if err != nil {
				return err
			}
			a.variables[key] = tv
		}
	}

	// inputKeys 从所有 variables 的 inputKeys 收集（包含嵌套依赖）
	// 对应 Python: PromptAssembler.input_keys property
	keySet := make(map[string]struct{})
	var allKeys []string
	for _, variable := range a.variables {
		for _, k := range variable.InputKeys() {
			if _, ok := keySet[k]; !ok {
				keySet[k] = struct{}{}
				allKeys = append(allKeys, k)
			}
		}
	}
	a.inputKeys = allKeys

	return nil
}

// isInputKey 检查 key 是否在 inputKeys 中。
func (a *PromptAssembler) isInputKey(key string) bool {
	for _, k := range a.inputKeys {
		if k == key {
			return true
		}
	}
	return false
}

// updateVariables 更新所有变量。
// 对应 Python: PromptAssembler._update()
func (a *PromptAssembler) updateVariables(kwargs map[string]any) error {
	// 检查 missing keys
	kwargSet := make(map[string]struct{}, len(kwargs))
	for k := range kwargs {
		kwargSet[k] = struct{}{}
	}
	for _, k := range a.inputKeys {
		if _, ok := kwargSet[k]; !ok {
			return exception.NewBaseError(
				exception.StatusPromptAssemblerTemplateParamError,
				exception.WithMsg(fmt.Sprintf("missing keys for updating the prompt assembler: [%s]", k)),
			)
		}
	}

	// 检查 unexpected keys
	for k := range kwargs {
		if !a.isInputKey(k) {
			return exception.NewBaseError(
				exception.StatusPromptAssemblerTemplateParamError,
				exception.WithMsg(fmt.Sprintf("unexpected keys for updating the prompt assembler: [%s]", k)),
			)
		}
	}

	// 对每个 variable 按 inputKeys 过滤 kwargs 后调 Eval
	for _, variable := range a.variables {
		filtered := prepareInputs(variable.InputKeys(), kwargs)
		variable.Eval(filtered)
	}

	return nil
}

// format 将 Variable 求值结果回填到 templateContent。
// 对应 Python: PromptAssembler._format()
func (a *PromptAssembler) format() any {
	// 构建格式化参数：变量名 → 变量值
	formatKwargs := make(map[string]any, len(a.variables))
	for name, variable := range a.variables {
		formatKwargs[name] = variable.Value()
	}

	switch content := a.templateContent.(type) {
	case string:
		for _, formatter := range a.formatters {
			if formatter == nil {
				continue
			}
			result := formatter.Eval(formatKwargs)
			if s, ok := result.(string); ok {
				a.templateContent = s
				return s
			}
		}
		return a.templateContent

	case []schema.BaseMessage:
		for idx, formatter := range a.formatters {
			if formatter == nil {
				continue
			}
			result := formatter.Eval(formatKwargs)
			// 将结果回填到对应 Message 的 content
			a.assignMessageContent(content[idx], result)
		}
		return content

	default:
		return a.templateContent
	}
}

// assignMessageContent 将 Variable 求值结果回填到 Message 的 content。
func (a *PromptAssembler) assignMessageContent(msg schema.BaseMessage, result any) {
	switch val := result.(type) {
	case string:
		msg.SetContent(schema.NewTextContent(val))
	case []any:
		// 将 []any 转回 []ContentPart
		parts := anyToContentParts(val)
		msg.SetContent(schema.NewMultiModalContent(parts...))
	case map[string]any:
		// 单个 dict 作为多模态内容的一个 part
		parts := anyToContentParts([]any{val})
		msg.SetContent(schema.NewMultiModalContent(parts...))
	default:
		// 其他类型，转为字符串
		msg.SetContent(schema.NewTextContent(fmt.Sprintf("%v", val)))
	}
}

// contentPartsToAny 将 []ContentPart 转为 []any（每项转为 map[string]any），
// 以便 DictableVariable 递归处理。
func contentPartsToAny(parts []schema.ContentPart) []any {
	result := make([]any, 0, len(parts))
	for _, part := range parts {
		m := map[string]any{
			"type": part.Type,
		}
		if part.Text != "" {
			m["text"] = part.Text
		}
		if part.ImageURL != nil {
			urlMap := map[string]any{
				"url": part.ImageURL.URL,
			}
			if part.ImageURL.Detail != "" {
				urlMap["detail"] = part.ImageURL.Detail
			}
			m["image_url"] = urlMap
		}
		result = append(result, m)
	}
	return result
}

// anyToContentParts 将 []any 转回 []ContentPart。
// 与 contentPartsToAny 互逆。
func anyToContentParts(items []any) []schema.ContentPart {
	parts := make([]schema.ContentPart, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		part := schema.ContentPart{}
		if t, ok := m["type"].(string); ok {
			part.Type = t
		}
		if txt, ok := m["text"].(string); ok {
			part.Text = txt
		}
		if imgURL, ok := m["image_url"].(map[string]any); ok {
			part.ImageURL = &schema.ImageURL{}
			if url, ok := imgURL["url"].(string); ok {
				part.ImageURL.URL = url
			}
			if detail, ok := imgURL["detail"].(string); ok {
				part.ImageURL.Detail = detail
			}
		}
		parts = append(parts, part)
	}
	return parts
}

// deepCopyMessages 深拷贝 []schema.BaseMessage 列表。
// 使用 JSON 序列化/反序列化实现深拷贝。
func deepCopyMessages(msgs []schema.BaseMessage) ([]schema.BaseMessage, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	result := make([]schema.BaseMessage, 0, len(msgs))
	for _, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			return nil, exception.NewBaseError(
				exception.StatusPromptTemplateRuntimeError,
				exception.WithMsg(fmt.Sprintf("failed to deep copy messages: %s", err.Error())),
			)
		}
		var dm schema.DefaultMessage
		if err := json.Unmarshal(data, &dm); err != nil {
			return nil, exception.NewBaseError(
				exception.StatusPromptTemplateRuntimeError,
				exception.WithMsg(fmt.Sprintf("failed to deep copy messages: %s", err.Error())),
			)
		}
		result = append(result, &dm)
	}
	return result, nil
}

// isNilOrEmpty 检查 any 是否为 nil 或空字符串。
func isNilOrEmpty(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok && s == "" {
		return true
	}
	return false
}
