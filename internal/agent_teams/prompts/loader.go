package prompts

import (
	"embed"
	"fmt"
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

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

// Render 渲染模板，替换占位符。
// 同时支持双花括号 {{var}}（system_prompt.md）和单花括号 {var}（team_plan_mode.md）格式。
// Python: PromptTemplate.format(**kwargs) / str.format()
func (t *PromptTemplate) Render(vars map[string]string) string {
	if t == nil {
		return ""
	}
	result := t.content
	if vars == nil {
		return result
	}
	// 先替换双花括号 {{var}}，再替换单花括号 {var}
	// 双花括号优先级高于单花括号
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
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
