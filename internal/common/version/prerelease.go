package version

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// prereleasePattern 预发布版本标记的正则表达式
// 支持标记：alpha, beta, rc, dev, pre, a, b
// 匹配示例：0.2.0-beta1, 0.2.0.alpha.1, 0.2.0rc2, 0.2.0.dev0
var prereleasePattern = regexp.MustCompile(
	`\d[.\-_]?(?:alpha|beta|rc|dev|pre|a|b)(?:\.?\d+)?(?:\b|$)`,
)

// baseVersionPattern 基础版本号的正则表达式
// 匹配纯数字点分版本号，如 0.2.0
var baseVersionPattern = regexp.MustCompile(
	`^(\d+(?:\.\d+)*)` +
		`(?:[.\-_]?(?:alpha|beta|rc|dev|pre|a|b)(?:[.\-_]?\d+)?)*$`,
)

// numericPattern 版本号中的数字提取正则
var numericPattern = regexp.MustCompile(`\d+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsPrereleaseVersion 判断版本号是否为预发布版本。
//
// 支持的预发布标记：alpha, beta, rc, dev, pre, a, b
// 示例：
//   - 0.2.0-beta1  → true
//   - 0.2.0.alpha  → true
//   - 0.2.0rc2     → true
//   - 0.2.0.dev0   → true
//   - 0.2.0        → false
func IsPrereleaseVersion(version string) bool {
	normalized := normalizeVersion(version)
	return prereleasePattern.MatchString(normalized)
}

// StripPrereleaseSuffix 去除版本号中的预发布后缀。
//
// 示例：
//   - 0.2.0-beta1  → 0.2.0
//   - 0.2.0.alpha.1 → 0.2.0
//   - 0.2.0rc2     → 0.2.0
//   - 0.2.0        → 0.2.0（无预发布后缀，原样返回）
func StripPrereleaseSuffix(version string) string {
	normalized := normalizeVersion(version)
	m := baseVersionPattern.FindStringSubmatch(normalized)
	if len(m) >= 2 {
		return m[1]
	}
	return normalized
}

// IsNewerVersion 判断 candidate 是否比 current 版本更新。
//
// 比较规则：
//  1. 先比较基础版本号（去除预发布后缀后的数字部分）
//  2. 基础版本号相同时，稳定版 > 预发布版（如 0.2.0 > 0.2.0-beta1）
//  3. 两个预发布版之间，按完整数字段逐段比较（如 0.2.0-beta2 > 0.2.0-beta1）
func IsNewerVersion(candidate, current string) bool {
	candidateBase := baseVersionKey(candidate)
	currentBase := baseVersionKey(current)

	// 填充到相同长度以便比较
	maxLen := max(len(candidateBase), len(currentBase))
	candidatePadded := padSlice(candidateBase, maxLen)
	currentPadded := padSlice(currentBase, maxLen)

	// 基础版本号比较
	for i := range maxLen {
		if candidatePadded[i] > currentPadded[i] {
			return true
		}
		if candidatePadded[i] < currentPadded[i] {
			return false
		}
	}

	// 基础版本号相同，看预发布 vs 稳定版
	currentIsPre := IsPrereleaseVersion(current)
	candidateIsPre := IsPrereleaseVersion(candidate)

	if currentIsPre && !candidateIsPre {
		// 当前是预发布，候选是稳定版 → 候选更新
		return true
	}
	if candidateIsPre && !currentIsPre {
		// 候选是预发布，当前是稳定版 → 候选更旧
		return false
	}

	// 两者同为预发布或同为稳定版，按完整数字段比较
	if candidateIsPre && currentIsPre {
		candidateFull := versionKey(candidate)
		currentFull := versionKey(current)
		maxFull := max(len(candidateFull), len(currentFull))
		candidateFullPadded := padSlice(candidateFull, maxFull)
		currentFullPadded := padSlice(currentFull, maxFull)
		for i := range maxFull {
			if candidateFullPadded[i] > currentFullPadded[i] {
				return true
			}
			if candidateFullPadded[i] < currentFullPadded[i] {
				return false
			}
		}
	}

	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeVersion 规范化版本号：去除前后空白，去除 v/V 前缀
func normalizeVersion(version string) string {
	return strings.TrimLeft(strings.TrimSpace(version), "vV")
}

// baseVersionKey 提取基础版本号的数字元组（去除预发布后缀）
// 如 0.2.0-beta1 → (0, 2, 0)，0.2.0 → (0, 2, 0)
func baseVersionKey(version string) []int {
	base := StripPrereleaseSuffix(version)
	return extractNumbers(base)
}

// versionKey 提取完整版本号的数字元组
// 如 0.2.0-beta1 → (0, 2, 0, 1)，0.2.0 → (0, 2, 0)
func versionKey(version string) []int {
	normalized := normalizeVersion(version)
	return extractNumbers(normalized)
}

// extractNumbers 从字符串中提取所有连续数字，转为 int 切片
func extractNumbers(s string) []int {
	matches := numericPattern.FindAllString(s, -1)
	result := make([]int, 0, len(matches))
	for _, m := range matches {
		var n int
		for _, ch := range m {
			n = n*10 + int(ch-'0')
		}
		result = append(result, n)
	}
	if len(result) == 0 {
		return []int{0}
	}
	return result
}

// padSlice 将 int 切片填充到指定长度（用 0 补齐）
func padSlice(s []int, length int) []int {
	if len(s) >= length {
		return s
	}
	padded := make([]int, length)
	copy(padded, s)
	return padded
}
