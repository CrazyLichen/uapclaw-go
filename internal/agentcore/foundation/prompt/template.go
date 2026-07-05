package prompt

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptTemplate 可插值替换的 Prompt 模板，支持字符串和消息列表两种内容格式。
//
// 对应 Python: openjiuwen/core/foundation/prompt/template.py (PromptTemplate)
//
// 支持的操作：
//   - Format(keywords) — 非原地修改，返回新实例，替换占位符
//   - ToMessages() — 将内容转为消息列表
//   - 部分填充 — 未传入的 key 保留原始占位符，可链式 Format
type PromptTemplate struct {
	// Name 模板名称
	Name string
	// Content 模板内容，支持 string 或 []schema.BaseMessage
	Content any
	// PlaceholderPrefix 占位符前缀，默认 "{{"
	PlaceholderPrefix string
	// PlaceholderSuffix 占位符后缀，默认 "}}"
	PlaceholderSuffix string
}

// TemplateOption PromptTemplate 构造选项函数。
type TemplateOption func(*PromptTemplate)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTemplatePrefix 设置占位符前缀。
func WithTemplatePrefix(prefix string) TemplateOption {
	return func(t *PromptTemplate) { t.PlaceholderPrefix = prefix }
}

// WithTemplateSuffix 设置占位符后缀。
func WithTemplateSuffix(suffix string) TemplateOption {
	return func(t *PromptTemplate) { t.PlaceholderSuffix = suffix }
}

// NewPromptTemplate 创建 PromptTemplate 实例。
//
// 参数：
//   - name: 模板名称
//   - content: 模板内容，string 或 []*schema.BaseMessage
//   - opts: 可选配置
func NewPromptTemplate(name string, content any, opts ...TemplateOption) *PromptTemplate {
	t := &PromptTemplate{
		Name:              name,
		Content:           content,
		PlaceholderPrefix: defaultPrefix,
		PlaceholderSuffix: defaultSuffix,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// Format 替换模板中的占位符，返回新的 PromptTemplate 实例。
//
// 对应 Python: PromptTemplate.format(keywords)
//
// 逻辑：
//  1. keywords 为空 → 返回深拷贝
//  2. 创建 PromptAssembler，过滤 keywords 中匹配 inputKeys 的键
//  3. 调 Assemble 渲染
//  4. 返回新 PromptTemplate
func (t *PromptTemplate) Format(keywords map[string]any) (*PromptTemplate, error) {
	// keywords 为空 → 返回深拷贝
	if len(keywords) == 0 {
		return t.deepCopy()
	}

	// 深拷贝 content 后创建 Assembler
	contentCopy, err := t.deepCopyContent()
	if err != nil {
		return nil, err
	}

	assembler, err := NewPromptAssembler(contentCopy,
		WithAssemblerPrefix(t.PlaceholderPrefix),
		WithAssemblerSuffix(t.PlaceholderSuffix),
	)
	if err != nil {
		return nil, err
	}

	// 过滤 keywords 中匹配 inputKeys 的键
	inputKeys := assembler.InputKeys()
	validKeywords := make(map[string]any)
	for _, key := range inputKeys {
		if v, ok := keywords[key]; ok {
			validKeywords[key] = v
		}
	}

	// 执行装配
	content, err := assembler.Assemble(validKeywords)
	if err != nil {
		return nil, err
	}

	return &PromptTemplate{
		Name:              t.Name,
		Content:           content,
		PlaceholderPrefix: t.PlaceholderPrefix,
		PlaceholderSuffix: t.PlaceholderSuffix,
	}, nil
}

// ToMessages 将模板内容转为消息列表。
//
// 对应 Python: PromptTemplate.to_messages()
//
// 逻辑：
//   - 空内容 → []
//   - string → [UserMessage]
//   - []schema.BaseMessage → 深拷贝后返回
func (t *PromptTemplate) ToMessages() ([]schema.BaseMessage, error) {
	if isNilOrEmpty(t.Content) {
		return nil, nil
	}

	switch content := t.Content.(type) {
	case string:
		um := schema.NewUserMessage(content)
		return []schema.BaseMessage{um}, nil

	case []schema.BaseMessage:
		// 验证类型
		for _, msg := range content {
			if msg == nil {
				return nil, exception.NewBaseError(
					exception.StatusPromptTemplateInvalid,
					exception.WithMsg("prompt template type must be in str or list[BaseMessage]"),
				)
			}
		}
		// 深拷贝
		return deepCopyMessages(content)

	default:
		return nil, exception.NewBaseError(
			exception.StatusPromptTemplateInvalid,
			exception.WithMsg(fmt.Sprintf("prompt template type must be in str or list[BaseMessage], got %T", t.Content)),
		)
	}
}

// InputKeys 返回模板中所有占位符键名。
func (t *PromptTemplate) InputKeys() ([]string, error) {
	contentCopy, err := t.deepCopyContent()
	if err != nil {
		return nil, err
	}

	assembler, err := NewPromptAssembler(contentCopy,
		WithAssemblerPrefix(t.PlaceholderPrefix),
		WithAssemblerSuffix(t.PlaceholderSuffix),
	)
	if err != nil {
		return nil, err
	}

	return assembler.InputKeys(), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopy 返回 PromptTemplate 的深拷贝。
func (t *PromptTemplate) deepCopy() (*PromptTemplate, error) {
	contentCopy, err := t.deepCopyContent()
	if err != nil {
		return nil, err
	}
	return &PromptTemplate{
		Name:              t.Name,
		Content:           contentCopy,
		PlaceholderPrefix: t.PlaceholderPrefix,
		PlaceholderSuffix: t.PlaceholderSuffix,
	}, nil
}

// deepCopyContent 深拷贝 Content 字段。
func (t *PromptTemplate) deepCopyContent() (any, error) {
	switch content := t.Content.(type) {
	case string:
		// string 是值类型，直接返回
		return content, nil
	case []schema.BaseMessage:
		return deepCopyMessages(content)
	default:
		return t.Content, nil
	}
}
