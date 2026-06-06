package prompt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultPrefix 默认占位符前缀。
	defaultPrefix = "{{"
	// defaultSuffix 默认占位符后缀。
	defaultSuffix = "}}"
	// innerVarName 内部变量名，对应 Python: "__inner__"。
	innerVarName = "__inner__"
	// defaultVarName 默认变量名，对应 Python: "default"。
	defaultVarName = "default"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextableVariable 字符串模板变量，处理 {{placeholder}} 占位符替换。
//
// 对应 Python: openjiuwen/core/foundation/prompt/assemble/variables/textable.py (TextableVariable)
//
// 语法特性：
//   - 变量替换：{{var}}，前后缀可配置（如 ${var}$、{var}）
//   - 嵌套属性：{{user.name}} 逐层 map/struct 解析
//   - 不支持条件逻辑、循环或表达式计算
type TextableVariable struct {
	baseVariable
	text         string   // 原始模板文本
	prefix       string   // 占位符前缀
	suffix       string   // 占位符后缀
	placeholders []string // 完整占位符路径列表（去重），如 ["user.name", "domain"]
}

// ──────────────────────────── 构造选项 ────────────────────────────

// TextableOption TextableVariable 构造选项函数。
type TextableOption func(*TextableVariable)

// WithPrefix 设置占位符前缀。
func WithPrefix(prefix string) TextableOption {
	return func(v *TextableVariable) { v.prefix = prefix }
}

// WithSuffix 设置占位符后缀。
func WithSuffix(suffix string) TextableOption {
	return func(v *TextableVariable) { v.suffix = suffix }
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTextableVariable 创建字符串模板变量。
//
// 参数：
//   - text: 模板文本，包含占位符，如 "Hello {{name}}"
//   - name: 变量名，默认 "default"，内部使用时为 "__inner__"
//   - opts: 可选配置（WithPrefix/WithSuffix）
//
// 对应 Python: TextableVariable(text=..., name=..., prefix=..., suffix=...)
func NewTextableVariable(text, name string, opts ...TextableOption) (*TextableVariable, error) {
	v := &TextableVariable{
		text:   text,
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

	// 正则提取占位符
	// 对应 Python: re.compile(re.escape(prefix) + r"([^{}]*?)" + re.escape(suffix))
	pattern, err := regexp.Compile(regexp.QuoteMeta(v.prefix) + `([^{}]*?)` + regexp.QuoteMeta(v.suffix))
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusPromptAssemblerVariableInitFailed,
			exception.WithMsg(fmt.Sprintf("invalid placeholder pattern: %s", err.Error())),
		)
	}

	// 扫描占位符，去重
	seen := make(map[string]struct{})
	placeholders := make([]string, 0)
	for _, match := range pattern.FindAllStringSubmatch(v.text, -1) {
		placeholder := strings.TrimSpace(match[1])
		if len(placeholder) == 0 {
			return nil, exception.NewBaseError(
				exception.StatusPromptAssemblerVariableInitFailed,
				exception.WithMsg("placeholders cannot be empty string"),
			)
		}
		if _, ok := seen[placeholder]; !ok {
			seen[placeholder] = struct{}{}
			placeholders = append(placeholders, placeholder)
		}
	}

	inputKeys := extractInputKeys(placeholders)

	v.placeholders = placeholders
	v.baseVariable = baseVariable{
		name:      name,
		inputKeys: inputKeys,
		value:     "",
	}

	return v, nil
}

// Text 返回原始模板文本。
func (v *TextableVariable) Text() string {
	return v.text
}

// Placeholders 返回完整占位符路径列表。
func (v *TextableVariable) Placeholders() []string {
	return v.placeholders
}

// Eval 求值：覆盖 baseVariable.Eval，确保调用自身的 Update。
// 对应 Python: Variable.eval()
func (v *TextableVariable) Eval(kwargs map[string]any) any {
	return evalBase(&v.baseVariable, v, kwargs)
}

// Update 根据传入的键值对替换占位符，更新 value。
//
// 对应 Python: TextableVariable.update()
//
// 逻辑：
//  1. 遍历 placeholders，对每个占位符逐层解析嵌套路径
//  2. 非 str/int/float/bool 类型调用 fmt.Sprintf("%v", value) 转换，并记录日志
//  3. strings.ReplaceAll 替换到 formattedText
func (v *TextableVariable) Update(kwargs map[string]any) {
	formattedText := v.text

	for _, placeholder := range v.placeholders {
		value, err := resolveNestedValue(placeholder, kwargs)
		if err != nil {
			// 解析失败时，保留原始占位符不替换（与 Python 端行为一致：部分填充场景）
			continue
		}

		// 类型检查：非 str/int/float/bool 记录日志并转为字符串
		valueStr := formatValue(value)

		placeholderStr := v.prefix + placeholder + v.suffix
		formattedText = strings.ReplaceAll(formattedText, placeholderStr, valueStr)
	}

	v.value = formattedText
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatValue 将值转换为字符串，非 str/int/float/bool 类型记录日志。
// 对应 Python: isinstance(value, (str, int, float, bool)) 检查 + str() 转换 + prompt_logger.info
func formatValue(value any) string {
	switch val := value.(type) {
	case string:
		return val
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%v", val)
	case float32:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		// 非 str/int/float/bool 类型，记录日志并转为字符串
		// 对应 Python: prompt_logger.info("Converting non-string value using str()...")
		logger.Info(logger.ComponentAgentCore).
			Str("placeholder", fmt.Sprintf("%v", value)).
			Msg("Converting non-string value using str(). Please check if the style is describe.")
		return fmt.Sprintf("%v", val)
	}
}
