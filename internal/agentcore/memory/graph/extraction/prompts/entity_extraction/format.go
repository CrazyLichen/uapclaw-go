package entity_extraction

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatNewEntities 格式化新提取的实体列表
//
// Python: format_new_entities(entities, language)
func FormatNewEntities(entities []map[string]any, language string) string {
	if len(entities) == 0 {
		return ""
	}
	tmpl := registry.DisplayEntity[language]
	var lines []string
	for i, ent := range entities {
		line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", i+1))
		line = strings.ReplaceAll(line, "{name}", fmt.Sprintf("%v", ent["name"]))
		content, _ := ent["content"].(string)
		if content == "" {
			content = fmt.Sprintf("%v", ent["name"])
		}
		line = strings.ReplaceAll(line, "{content}", content)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
