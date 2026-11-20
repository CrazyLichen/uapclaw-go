package locales

import (
	"embed"
	"strings"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// Translator 翻译器闭包。
// Python: Translator = Callable[..., str]
//
// 调用方式:
//
//	t("workspace_meta")           → 加载 descs/{lang}/workspace_meta.md 作为 _desc
//	t("workspace_meta", "action") → 查 STRINGS["workspace_meta.action"]
type Translator func(tool string, key ...string) string

// ──────────────────────────── 常量 ────────────────────────────

// descKey 工具描述 key 后缀。
// Python: _desc
const descKey = "_desc"

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed descs
var descFS embed.FS

// descCache 工具描述缓存（lang+"/"+tool → content）。
// Python: @cache _load_desc
var descCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeTranslator 创建绑定到指定语言的翻译器闭包。
// Python: make_translator(lang)
func MakeTranslator(lang atschema.Language) Translator {
	return func(tool string, key ...string) string {
		// 无 key 或 key 为 "_desc" → 加载 Markdown 描述并渲染
		// （Python make_translator 闭包：_desc 分支必经 _render_desc——
		// slot 填充 + omit 塌缩；Go 侧 omit 经显式 RenderDesc 传参，
		// 闭包形态无 omit（空集））。e15 审查修复：原直接返回 md 原文，
		// slot 未填充（集成测试暴露 create_task/build_team/update_task/
		// send_message 四工具描述残留 {{）。
		if len(key) == 0 || key[0] == descKey {
			return RenderDesc(tool, lang, map[string]bool{})
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
// Python: _load_desc(tool, lang)
// 文件路径格式：descs/{lang}/{tool}.md
func loadToolDesc(tool, lang string) string {
	cacheKey := lang + "/" + tool
	if cached, ok := descCache.Load(cacheKey); ok {
		return cached.(string)
	}
	// 递归索引（Python _desc_index：desc_key 平铺、fragments 排除）——
	// desc 文件按领域子目录归档（member/expert/...），单层路径读不到。
	p, ok := descIndexOf(lang)[tool]
	if !ok {
		descCache.Store(cacheKey, "")
		return ""
	}
	data, err := descFS.ReadFile(p)
	if err != nil {
		descCache.Store(cacheKey, "")
		return ""
	}
	content := strings.TrimSpace(string(data))
	descCache.Store(cacheKey, content)
	return content
}
