package splitter

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// detectChinese 检测文本主要为中文还是英文（基于字符分布）。
//
// 中文字符（CJK 统一表意 0x4E00-0x9FFF）占比 >= threshold 判 zh；
// 未达标时用标点启发（？> ? 且 ！> ! 判 zh）。
//
// Python 的 numpy UTF-32 向量化与 5000 万字符分块统计在 Go 侧省略
// （numpy 特有内存优化，rune 单循环足够快）；计满阈值即早退为语义
// 等价增强（Python 分块路径的早退行为，统一路径最终判定结果一致）。
//
// Python: splitter/splitter.py SentenceSplitter._detect_chinese
func detectChinese(text string, threshold float64) string {
	if text == "" {
		return "en"
	}
	runes := []rune(text)
	total := len(runes)
	thresholdVal := int(threshold * float64(total))
	chineseCount := 0
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
			// 语义早退：已计满阈值
			if chineseCount >= thresholdVal {
				return "zh"
			}
		}
	}
	isChinese := chineseCount >= thresholdVal
	if !isChinese {
		usingChinesePuncs := strings.Count(text, "？") > strings.Count(text, "?") &&
			strings.Count(text, "！") > strings.Count(text, "!")
		isChinese = usingChinesePuncs
	}
	if isChinese {
		return "zh"
	}
	return "en"
}
