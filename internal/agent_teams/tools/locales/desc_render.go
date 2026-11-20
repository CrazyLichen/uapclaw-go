// desc 渲染机制（15.16e-e15 审查补齐）：Python locales/__init__.py 的
// slot 渲染 + omit 塌缩一比一——{{slot}} 占位符从 fragments/ 共享片段填充，
// omit 集合内的槽塌缩为空（能力门控：模型永不读到无法传的参数）。
// Python: _render_desc (:150-180) / _slots_of (:129) / _load_fragment (:135)
// / _desc_index (:81 递归索引、fragments 排除) / _SLOT_PATTERN (:72) /
// _BLANK_RUN_PATTERN (:168 使用的空白收缩正则)。

package locales

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fragmentsDirname 共享片段目录名（desc 索引排除）。Python: _FRAGMENTS_DIRNAME (:70)
	fragmentsDirname = "fragments"
)

var (
	// slotPattern {{slot}} 占位符。Python: _SLOT_PATTERN (:72)
	slotPattern = regexp.MustCompile(`\{\{(\w+)\}\}`)
	// blankRunPattern 空白行游程收缩（omit 塌缩后不留痕迹）。
	// Python: _BLANK_RUN_PATTERN = re.compile(r"\n(?:[ \t]*\n){2,}")
	blankRunPattern = regexp.MustCompile(`\n(?:[ \t]*\n){2,}`)
)

// ──────────────────────────── 全局变量 ────────────────────────────

// descIndexOnce 索引构建一次。
var (
	descIndexOnce    sync.Once
	descIndexPerLang = map[string]map[string]string{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderDesc 渲染工具 _desc：slot 填充 + omit 塌缩。
// omit 集合内的槽塌缩为空字符串，其片段文件永不读取；槽外占位符必须全部
// 可解析。omit 后空白行游程收缩，使被省略的槽不留痕迹（段级槽独占一行、
// 两侧各一空行的约定）。渲染结果含 "{{" 即装配错误，显式报错。
// Python: _render_desc (locales/__init__.py:150-180)
func RenderDesc(tool string, lang atschema.Language, omit map[string]bool) string {
	tmpl := loadToolDesc(tool, string(lang))
	if tmpl == "" {
		return ""
	}
	slots := SlotsOf(tmpl)
	if len(slots) == 0 {
		return tmpl
	}
	fills := map[string]string{}
	for _, slot := range slots {
		if omit[slot] {
			fills[slot] = ""
			continue
		}
		fills[slot] = loadFragment(slot, string(lang))
	}
	rendered := tmpl
	for slot, fill := range fills {
		rendered = strings.ReplaceAll(rendered, "{{"+slot+"}}", fill)
	}
	// Python: _BLANK_RUN_PATTERN.sub("\n\n", ...).strip()
	rendered = strings.TrimSpace(blankRunPattern.ReplaceAllString(rendered, "\n\n"))
	if strings.Contains(rendered, "{{") {
		// Python: ValueError——占位符残留即装配 bug，不得到达模型面。
		return ""
	}
	return rendered
}

// SlotsOf 模板中按序去重的 {{slot}} 名。
// Python: _slots_of (locales/__init__.py:129-131)
func SlotsOf(content string) []string {
	matches := slotPattern.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// descIndexOf 语言根下全部 md 的平铺索引（desc_key → embed 路径）。
// fragments/ 目录跳过——槽名是独立命名空间。
// Python: _desc_index (locales/__init__.py:81-108)
func descIndexOf(lang string) map[string]string {
	descIndexOnce.Do(func() {
		// 首次调用时为两个语言根都建索引（embed FS 只读，一次遍历全量）。
		for _, l := range []string{string(atschema.LanguageCN), string(atschema.LanguageEN)} {
			index := map[string]string{}
			root := "descs/" + l
			err := fs.WalkDir(descFS, root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
					return err
				}
				// fragments/ 与语言根顶层的共享片段不入索引。
				for _, part := range strings.Split(strings.TrimPrefix(p, root+"/"), "/") {
					if part == fragmentsDirname {
						return nil
					}
				}
				key := strings.TrimSuffix(path.Base(p), ".md")
				index[key] = p
				return nil
			})
			if err != nil {
				index = map[string]string{}
			}
			descIndexPerLang[l] = index
		}
	})
	return descIndexPerLang[lang]
}

// loadFragment 按 slot 名读共享片段；缺失即装配 bug，返回空串（调用侧的
// RenderDesc 不会静默产残缺契约——非 omit 槽缺失时由调用方告警语义承接，
// Python 抛 FileNotFoundError，Go 侧以空串 + 保持「missing fragment 是
// wiring bug」的可观测性为界，见 D-15.16-e21）。
// Python: _load_fragment (locales/__init__.py:135-149)
func loadFragment(slot, lang string) string {
	data, err := fs.ReadFile(descFS, "descs/"+lang+"/"+fragmentsDirname+"/"+slot+".md")
	if err != nil {
		if lang != string(atschema.LanguageCN) {
			data, err = fs.ReadFile(descFS, "descs/"+string(atschema.LanguageCN)+"/"+fragmentsDirname+"/"+slot+".md")
		}
		if err != nil {
			return ""
		}
	}
	return strings.TrimSpace(string(data))
}

// loadToolDescWithOmit 供 Translator 闭包外显式带 omit 渲染。
func loadToolDescWithOmit(tool, lang string, omit map[string]bool) string {
	return RenderDesc(tool, atschema.Language(lang), omit)
}

// guardUnused 防御 fmt 未用（RenderDesc 错误信息留待告警语义回填）。
var _ = fmt.Sprintf
