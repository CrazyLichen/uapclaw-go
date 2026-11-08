package prompts

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
	"sync"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptTemplate 提示词模板，支持单花括号 {var} 和双花括号 {{var}} 占位符渲染。
// Python: PromptTemplate (openjiuwen/agent_teams/prompts/loader.py)
type PromptTemplate struct {
	// content 模板原始内容
	content string
}

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed cn/*.md en/*.md system_prompt.md
var promptsFS embed.FS

// templateCache 模板缓存（对齐 Python @cache 装饰器）
var templateCache sync.Map

// defaultLanguage 默认提示词语言
// Python: load_template(name, language=_DEFAULT_LANGUAGE="cn")
const defaultLanguage = "cn"

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadTemplate 加载语言相关模板。
// 模板缺失时返回 error（对齐 Python: Path.read_text 抛 FileNotFoundError），
// 调用方必须逐层上抛，不得以空串静默降级。
// Python: load_template(name, language) -> PromptTemplate
// 从 prompts/<lang>/<name>.md 加载
func LoadTemplate(name, language string) (*PromptTemplate, error) {
	// Python: load_template(name, language="cn") —— 语言参数默认 "cn"
	if language == "" {
		language = defaultLanguage
	}
	key := language + "/" + name
	if cached, ok := templateCache.Load(key); ok {
		return cached.(*PromptTemplate), nil
	}
	data, err := promptsFS.ReadFile(language + "/" + name + ".md")
	if err != nil {
		return nil, fmt.Errorf("加载模板 %s/%s.md 失败: %w", language, name, err)
	}
	tpl := &PromptTemplate{content: string(data)}
	templateCache.Store(key, tpl)
	return tpl, nil
}

// LoadSharedTemplate 加载语言无关模板。
// 模板缺失时返回 error（对齐 Python: Path.read_text 抛 FileNotFoundError）。
// Python: load_shared_template(name) -> PromptTemplate
// 从 prompts/<name>.md 加载
func LoadSharedTemplate(name string) (*PromptTemplate, error) {
	if cached, ok := templateCache.Load(name); ok {
		return cached.(*PromptTemplate), nil
	}
	data, err := promptsFS.ReadFile(name + ".md")
	if err != nil {
		return nil, fmt.Errorf("加载共享模板 %s.md 失败: %w", name, err)
	}
	tpl := &PromptTemplate{content: string(data)}
	templateCache.Store(name, tpl)
	return tpl, nil
}

// renderPlaceholderPattern 双花括号占位符正则，对齐上游
// TextableVariable 的 `re.escape(prefix) + r"([^{}]*?)" + re.escape(suffix)`
// （assemble/variables/textable.py L30）：占位符名内部不允许出现花括号。
//
// Python: TextableVariable.__init__ 的 pattern (textable.py L30)
var renderPlaceholderPattern = regexp.MustCompile(`\{\{([^{}]*?)\}\}`)

// Render 渲染模板，只替换双花括号 {{var}}。
//
// 单花括号 {var} 视为字面文本，原样保留——对齐上游 PromptAssembler 的
// placeholder_prefix="{{" / placeholder_suffix="}}"（template.py L33-34）与
// TextableVariable 的正则 `\{\{([^{}]*?)\}\}`。上游从不用单花括号做占位符：
// team_plan_mode.md 里那两个 {enter_plan_mode_status} / {plan_file_info}
// 在 Python 侧同样不被替换。
//
// **先切段、再只替换占位符段**：上游把模板切成「字面段 / 占位符段」，
// update() 时只替换占位符段、字面段原样拼回，所以替换值里的 {{...}}
// 不会被再解析一次。全局 ReplaceAll 做不到这点（替换进去的文本会被后续
// key 再匹配），故这里照抄切段结构。
//
// 未在 vars 中出现的 key 保留占位符原文（上游 prompt_assemble 的
// all_kwargs 回填行为），因此调度模板的两次 Render 可以照抄上游。
//
// Python: TextableVariable.update (textable.py L63-90) +
// PromptTemplate.format (template.py L58-75)
func (t *PromptTemplate) Render(vars map[string]string) string {
	if t == nil {
		return ""
	}
	if vars == nil || len(vars) == 0 {
		return t.content
	}
	matches := renderPlaceholderPattern.FindAllStringSubmatchIndex(t.content, -1)
	if len(matches) == 0 {
		return t.content
	}
	var b strings.Builder
	b.Grow(len(t.content))
	cursor := 0
	for _, m := range matches {
		// 占位符名内部不含花括号，故整段是纯 ASCII
		name := strings.TrimSpace(t.content[m[2]:m[3]])
		if name == "" {
			// 空占位符（{{}}）上游当字面文本保留并告警，这里同样不前进 cursor
			continue
		}
		// 字面段原样写入，绝不解析其中内容
		b.WriteString(t.content[cursor:m[0]])
		if v, ok := vars[name]; ok {
			b.WriteString(v)
		} else {
			// 未提供的 key 保留占位符原文
			b.WriteString(t.content[m[0]:m[1]])
		}
		cursor = m[1]
	}
	b.WriteString(t.content[cursor:])
	return b.String()
}

// Content 返回模板原始内容。
func (t *PromptTemplate) Content() string {
	if t == nil {
		return ""
	}
	return t.content
}

// ToSection 将模板转换为 PromptSection。
// 便捷方法，用于纯文本模板（无占位符）直接构建 section。
func (t *PromptTemplate) ToSection(name string, language string, priority int) saprompt.PromptSection {
	content := t.Render(nil)
	return saprompt.NewPromptSection(name, map[string]string{language: content}, priority)
}
