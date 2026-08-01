package lite

// ──────────────────────────── 常量 ────────────────────────────

// ValidTypes 合法的记忆类型
var ValidTypes = []string{"user", "feedback", "project", "reference"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseFrontmatter 解析 --- frontmatter。
// ⤵️ 回填: 7.5 — 当前返回 nil
func ParseFrontmatter(content string) map[string]string { return nil }

// ValidateFrontmatter 验证 name/description/type 字段。
// ⤵️ 回填: 7.5 — 当前返回 false
func ValidateFrontmatter(fm map[string]string) (bool, string) { return false, "" }

// EnrichFrontmatter 自动填充 created_at/updated_at。
// ⤵️ 回填: 7.5 — 当前返回 nil
func EnrichFrontmatter(fm map[string]string, isEdit bool) map[string]string { return nil }

// RebuildContentWithFrontmatter 用更新后的 frontmatter 重建文件内容。
// ⤵️ 回填: 7.5 — 当前返回空字符串
func RebuildContentWithFrontmatter(content string, fm map[string]string) string { return "" }
