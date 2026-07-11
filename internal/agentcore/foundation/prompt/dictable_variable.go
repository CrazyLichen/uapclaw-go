package prompt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DictableVariable 字典/列表模板变量，递归处理多模态内容中的占位符。
//
// 对应 Python: openjiuwen/core/foundation/prompt/assemble/variables/dictable.py (DictableVariable)
//
// 与 TextableVariable 共享占位符语法和嵌套解析逻辑，区别在于：
//   - TextableVariable 处理纯字符串模板
//   - DictableVariable 递归遍历 dict/list 结构中所有字符串值，扫描并替换占位符
//
// 用途：处理 BaseMessage.content 为 []ContentPart（多模态消息）时的占位符替换。
type DictableVariable struct {
	baseVariable
	data         any            // map[string]any 或 []any
	prefix       string         // 占位符前缀
	suffix       string         // 占位符后缀
	pattern      *regexp.Regexp // 预编译正则
	placeholders []string       // 完整占位符路径列表（去重）
}

// ──── 构造选项 ────

// DictableOption DictableVariable 构造选项函数。
type DictableOption func(*DictableVariable)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDictablePrefix 设置占位符前缀。
func WithDictablePrefix(prefix string) DictableOption {
	return func(v *DictableVariable) { v.prefix = prefix }
}

// WithDictableSuffix 设置占位符后缀。
func WithDictableSuffix(suffix string) DictableOption {
	return func(v *DictableVariable) { v.suffix = suffix }
}

// NewDictableVariable 创建字典/列表模板变量。
//
// 参数：
//   - data: 字典或列表数据，如 map[string]any{"text": "Hello {{name}}"} 或 []any{...}
//   - name: 变量名
//   - opts: 可选配置（WithDictablePrefix/WithDictableSuffix）
//
// 对应 Python: DictableVariable(data=..., name=..., prefix=..., suffix=...)
func NewDictableVariable(data any, name string, opts ...DictableOption) (*DictableVariable, error) {
	v := &DictableVariable{
		data:   data,
		prefix: defaultPrefix,
		suffix: defaultSuffix,
	}
	if name == "" {
		name = defaultVarName
	}

	// 应用选项
	for _, opt := range opts {
		opt(v)
	}

	// 预编译正则
	pattern, err := regexp.Compile(regexp.QuoteMeta(v.prefix) + `([^{}]*?)` + regexp.QuoteMeta(v.suffix))
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusPromptAssemblerVariableInitFailed,
			exception.WithMsg(fmt.Sprintf("invalid placeholder pattern: %s", err.Error())),
		)
	}
	v.pattern = pattern

	// 递归扫描占位符
	seen := make(map[string]struct{})
	placeholders := make([]string, 0)
	scanPlaceholders(v.data, v.pattern, seen, &placeholders)

	inputKeys := extractInputKeys(placeholders)

	v.placeholders = placeholders
	v.baseVariable = baseVariable{
		name:      name,
		inputKeys: inputKeys,
		value:     nil,
	}

	return v, nil
}

// Data 返回原始数据。
func (v *DictableVariable) Data() any {
	return v.data
}

// Placeholders 返回完整占位符路径列表。
func (dv *DictableVariable) Placeholders() []string {
	return dv.placeholders
}

// Eval 求值：覆盖 baseVariable.Eval，确保调用自身的 Update。
// 对应 Python: Variable.eval()
func (dv *DictableVariable) Eval(kwargs map[string]any) any {
	return evalBase(&dv.baseVariable, dv, kwargs)
}

// Update 递归替换占位符，更新 value。
//
// 对应 Python: DictableVariable.update()
//
// 逻辑：
//  1. 深拷贝 data
//  2. 递归替换所有字符串值中的占位符
//  3. 赋值 value
func (dv *DictableVariable) Update(kwargs map[string]any) {
	dataCopy := deepCopyAny(dv.data)
	dv.value = dv.recursiveFormat(dataCopy, kwargs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// scanPlaceholders 递归扫描 obj 中所有字符串值，提取占位符。
// 对应 Python: DictableVariable._scan_placeholders()
func scanPlaceholders(obj any, pattern *regexp.Regexp, seen map[string]struct{}, placeholders *[]string) {
	switch val := obj.(type) {
	case string:
		for _, match := range pattern.FindAllStringSubmatch(val, -1) {
			placeholder := strings.TrimSpace(match[1])
			if len(placeholder) == 0 {
				// 对应 Python: 空占位符抛异常，但此处无法返回 error，
				// 因此在 NewDictableVariable 中先验证数据中的占位符。
				// 这里如果遇到空占位符，直接跳过（不应出现，构造时已校验）
				continue
			}
			if _, ok := seen[placeholder]; !ok {
				seen[placeholder] = struct{}{}
				*placeholders = append(*placeholders, placeholder)
			}
		}
	case []any:
		for _, item := range val {
			scanPlaceholders(item, pattern, seen, placeholders)
		}
	case map[string]any:
		for _, v := range val {
			scanPlaceholders(v, pattern, seen, placeholders)
		}
	}
}

// recursiveFormat 递归替换 obj 中的占位符。
// 对应 Python: DictableVariable._recursive_format()
func (dv *DictableVariable) recursiveFormat(obj any, kwargs map[string]any) any {
	switch val := obj.(type) {
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = dv.recursiveFormat(item, kwargs)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, mapVal := range val {
			result[k] = dv.recursiveFormat(mapVal, kwargs)
		}
		return result
	case string:
		return dv.formatString(val, kwargs)
	default:
		// 非字符串、非 dict、非 list 的值原样返回
		return val
	}
}

// formatString 替换字符串中的占位符。
func (dv *DictableVariable) formatString(s string, kwargs map[string]any) string {
	formattedText := s
	for _, placeholder := range dv.placeholders {
		placeholderStr := dv.prefix + placeholder + dv.suffix
		if !strings.Contains(formattedText, placeholderStr) {
			continue
		}

		value, err := resolveNestedValue(placeholder, kwargs)
		if err != nil {
			// 解析失败，保留原占位符
			continue
		}

		valueStr := formatValue(value)
		formattedText = strings.ReplaceAll(formattedText, placeholderStr, valueStr)
	}
	return formattedText
}

// deepCopyAny 深拷贝 any 类型的值。
// 对于 map[string]any 和 []any 递归拷贝，其他类型直接返回（Go 中 string/int/float/bool 是值类型）。
func deepCopyAny(obj any) any {
	switch val := obj.(type) {
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = deepCopyAny(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = deepCopyAny(v)
		}
		return result
	default:
		// string/int/float/bool/nil 等值类型直接返回
		return val
	}
}
