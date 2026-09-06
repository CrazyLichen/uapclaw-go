package locales

import (
	"embed"
	"strings"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// descKey 工具描述 key 后缀。
// 对齐 Python: _desc
const descKey = "_desc"

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed descs/**/*.md
var descFS embed.FS

// descCache 工具描述缓存（lang+"/"+tool → content）。
// 对齐 Python: @cache _load_desc
var descCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// Translator 翻译器闭包。
// 对齐 Python: Translator = Callable[..., str]
//
// 调用方式:
//   t("workspace_meta")           → 加载 descs/{lang}/workspace_meta.md 作为 _desc
//   t("workspace_meta", "action") → 查 STRINGS["workspace_meta.action"]
type Translator func(tool string, key ...string) string

// MakeTranslator 创建绑定到指定语言的翻译器闭包。
// 对齐 Python: make_translator(lang)
func MakeTranslator(lang atschema.Language) Translator {
	return func(tool string, key ...string) string {
		// 无 key 或 key 为 "_desc" → 加载 Markdown 描述
		if len(key) == 0 || key[0] == descKey {
			return loadToolDesc(tool, string(lang))
		}
		// 其他 key → 查 STRINGS 映射
		dictKey := tool + "." + key[0]
		table, ok := atschema.STRINGS[lang]
		if !ok {
			table = atschema.STRINGS[atschema.LanguageCN]
		}
		if val, ok := table[dictKey]; ok {
			return val
		}
		// 回退到默认语言
		if table = atschema.STRINGS[atschema.LanguageCN]; table != nil {
			if val, ok := table[dictKey]; ok {
				return val
			}
		}
		// key 缺失：返回 key 本身（对齐 Python KeyError 可恢复行为）
		return dictKey
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadToolDesc 从嵌入的 Markdown 文件中加载工具描述。
// 对齐 Python: _load_desc(tool, lang)
// 文件路径格式：descs/{lang}/{tool}.md
func loadToolDesc(tool, lang string) string {
	cacheKey := lang + "/" + tool
	if cached, ok := descCache.Load(cacheKey); ok {
		return cached.(string)
	}
	// 尝试读取 descs/{lang}/{tool}.md
	path := "descs/" + lang + "/" + tool + ".md"
	data, err := descFS.ReadFile(path)
	if err != nil {
		// 回退到中文
		if lang != string(atschema.LanguageCN) {
			path = "descs/" + string(atschema.LanguageCN) + "/" + tool + ".md"
			data, err = descFS.ReadFile(path)
		}
		if err != nil {
			descCache.Store(cacheKey, "")
			return ""
		}
	}
	content := strings.TrimSpace(string(data))
	descCache.Store(cacheKey, content)
	return content
}
