package lite

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ──────────────────────────── ────────────────────────────

// validTypes 合法的记忆类型。对齐 Python VALID_TYPES（tuple 不可变）
var validTypes = [...]string{"user", "feedback", "project", "reference"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidTypes 返回合法记忆类型的不可变副本。对齐 Python VALID_TYPES（tuple 不可变）
func ValidTypes() []string {
	return validTypes[:]
}

// ParseFrontmatter 解析 --- frontmatter。对齐 Python parse_frontmatter
func ParseFrontmatter(content string) map[string]string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(content[3:3+end]), "\n") {
		if idx := strings.Index(line, ":"); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ValidateFrontmatter 验证 name/description/type 字段。对齐 Python validate_frontmatter
func ValidateFrontmatter(fm map[string]string) (bool, string) {
	if fm == nil {
		return false, "frontmatter 为 nil"
	}
	for _, field := range []string{"name", "description", "type"} {
		if fm[field] == "" {
			return false, fmt.Sprintf("缺少必填字段: %s", field)
		}
	}
	valid := false
	for _, t := range validTypes[:] {
		if fm["type"] == t {
			valid = true
			break
		}
	}
	if !valid {
		return false, "type 必须是以下之一: user, feedback, project, reference"
	}
	return true, ""
}

// EnrichFrontmatter 自动填充 created_at/updated_at。对齐 Python enrich_frontmatter
func EnrichFrontmatter(fm map[string]string, isEdit bool) map[string]string {
	today := time.Now().Format("2006-01-02")
	if !isEdit {
		if _, ok := fm["created_at"]; !ok {
			fm["created_at"] = today
		}
	}
	fm["updated_at"] = today
	return fm
}

// RebuildContentWithFrontmatter 用更新后的 frontmatter 重建文件内容。对齐 Python rebuild_content_with_frontmatter
func RebuildContentWithFrontmatter(content string, fm map[string]string) string {
	body := ExtractBody(content)
	var fmLines []string
	fmLines = append(fmLines, "---")

	// 按固定优先级输出核心字段，其余按 key 排序，确保输出稳定（Python dict 3.7+ 保持插入顺序）
	fixedOrder := []string{"name", "description", "type"}
	fixedSet := make(map[string]bool, len(fixedOrder))
	for _, k := range fixedOrder {
		fixedSet[k] = true
		if v, ok := fm[k]; ok {
			fmLines = append(fmLines, k+": "+v)
		}
	}
	var rest []string
	for k := range fm {
		if !fixedSet[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		fmLines = append(fmLines, k+": "+fm[k])
	}

	fmLines = append(fmLines, "---")
	parts := []string{strings.Join(fmLines, "\n")}
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

// ExtractBody 提取 frontmatter 后的 body 内容。对齐 Python _extract_body
func ExtractBody(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return content
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(content[3+end+3:])
}
