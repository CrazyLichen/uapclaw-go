package web_tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// clipDebugTextMaxChars 调试文本截断最大字符数
	clipDebugTextMaxChars = 30000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// clipDebugText 截断调试文本，保留头尾
// 对齐 Python: _clip_debug_text() (web_tools.py L318-324)
func clipDebugText(value string, maxChars ...int) string {
	limit := clipDebugTextMaxChars
	if len(maxChars) > 0 && maxChars[0] > 0 {
		limit = maxChars[0]
	}
	if len(value) <= limit {
		return value
	}
	headLen := limit / 2
	tailLen := limit - headLen
	return value[:headLen] + fmt.Sprintf("\n...[truncated %d chars]...\n", len(value)-limit) + value[len(value)-tailLen:]
}

// writeDebugPayload 写调试数据到文件
// 对齐 Python: _write_debug_payload() (web_tools.py L413-428)
func writeDebugPayload(runID, engine, stage string, payload map[string]any) {
	if !isDebugEnabled() {
		return
	}
	debugDir := getDebugDir()
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		return
	}
	filename := fmt.Sprintf("%s_%s_%s.json", runID, engine, stage)
	path := filepath.Join(debugDir, filename)

	// 对齐 Python: L422-424 — 截断 raw_html
	normalized := make(map[string]any)
	for k, v := range payload {
		if k == "raw_html" {
			if s, ok := v.(string); ok {
				normalized[k] = clipDebugText(s)
			} else {
				normalized[k] = v
			}
		} else {
			normalized[k] = v
		}
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// newDebugRunID 生成调试运行 ID
// 对齐 Python: debug_run_id = datetime.now(timezone.utc).strftime(...) (web_tools.py L904)
func newDebugRunID() string {
	return time.Now().UTC().Format("20060102_150405_000000")
}

// extractDebugHTMLNeighborhood 提取 HTML 标记附近的内容
// 对齐 Python: _extract_debug_html_neighborhood() (web_tools.py L336-347)
func extractDebugHTMLNeighborhood(htmlStr, marker string, window ...int) map[string]any {
	w := 4000
	if len(window) > 0 {
		w = window[0]
	}
	position := strings.Index(htmlStr, marker)
	if position < 0 {
		return map[string]any{"marker": marker, "position": -1, "fragment": ""}
	}
	start := position - w
	if start < 0 {
		start = 0
	}
	end := position + len(marker) + w
	if end > len(htmlStr) {
		end = len(htmlStr)
	}
	return map[string]any{
		"marker":   marker,
		"position": position,
		"fragment": clipDebugText(htmlStr[start:end], w*2+len(marker)),
	}
}

// summarizeRows 构建搜索结果摘要
// 对齐 Python: _summarize_rows() (web_tools.py L564-584)
func summarizeRows(rows []searchRow, limit ...int) map[string]any {
	maxRows := 10
	if len(limit) > 0 {
		maxRows = limit[0]
	}
	bySource := map[string]int{}
	var topTitles []map[string]string
	for _, row := range rows {
		source := row.Source
		if source == "" {
			source = "unknown"
		}
		bySource[source]++
	}
	for i, row := range rows {
		if i >= maxRows {
			break
		}
		title := row.Title
		if len(title) > 200 {
			title = title[:200]
		}
		origin := row.Origin
		if len(origin) > 120 {
			origin = origin[:120]
		}
		rowURL := row.URL
		if len(rowURL) > 240 {
			rowURL = rowURL[:240]
		}
		topTitles = append(topTitles, map[string]string{
			"title":  title,
			"origin": origin,
			"source": row.Source,
			"url":    rowURL,
		})
	}
	return map[string]any{
		"count":      len(rows),
		"by_source":  bySource,
		"top_titles": topTitles,
	}
}
