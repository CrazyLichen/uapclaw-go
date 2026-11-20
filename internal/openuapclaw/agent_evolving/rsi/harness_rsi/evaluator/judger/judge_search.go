package judger

// 冻结裁判证据上的有界字面量搜索工具，无 shell 执行。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/judge_search.py（130 行）。

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JudgeSearchTool 冻结证据的字面量搜索工具。
// Python: judge_search.py JudgeSearchTool(Tool)——字面量搜索避免外部可执行文件
// 与无界正则求值。
type JudgeSearchTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// workspace 冻结工作区绝对路径
	workspace string
}

// judgeSearchLimits 搜索边界常量（Python 类属性）。
const (
	judgeSearchMaxFileBytes  = 1024 * 1024
	judgeSearchMaxTotalBytes = 16 * 1024 * 1024
	judgeSearchMaxEntries    = 4096
)

// judgeSearchDescription grep 卡片描述（Python 逐字）。
const judgeSearchDescription = "Search a literal substring in frozen UTF-8 evidence without executing commands. " +
	"Returns bounded file/line matches. An incomplete scan or no match is not proof of absence. " +
	"Use read_evidence for full text and large files. Regex syntax is treated literally."

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJudgeSearchTool 构造搜索工具。
// Python: JudgeSearchTool.__init__（ToolCard id=f"{agent_id}.grep"）。
func NewJudgeSearchTool(workspace, agentID string) *JudgeSearchTool {
	return &JudgeSearchTool{
		card: &tool.ToolCard{
			BaseCard: schema.BaseCard{
				ID:          agentID + ".grep",
				Name:        "grep",
				Description: judgeSearchDescription,
			},
			InputParams: []*schema.Param{
				schema.NewStringParam("pattern", "", true),
				schema.NewStringParam("path", "", false),
				schema.NewStringParam("glob", "", false),
				schema.NewBooleanParam("ignore_case", "", false, false),
				schema.NewNumberParam("max_results", "", false),
			},
		},
		workspace: mustAbs(workspace),
	}
}

// Card 返回工具卡片。
func (t *JudgeSearchTool) Card() *tool.ToolCard { return t.card }

// GetCallTimeout 沿用默认超时。
func (t *JudgeSearchTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// Invoke 执行字面量搜索（Python _search + invoke 错误包装）。
func (t *JudgeSearchTool) Invoke(ctx context.Context, inputs map[string]any,
	opts ...tool.ToolOption) (map[string]any, error) {
	payload, err := t.search(inputs)
	if err != nil {
		return nil, err
	}
	data, err := marshalJSONCompact(payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": string(data)}, nil
}

// Stream 单块流。
func (t *JudgeSearchTool) Stream(ctx context.Context, inputs map[string]any,
	opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	ch := make(chan tool.StreamChunk, 1)
	go func() {
		defer close(ch)
		result, err := t.Invoke(ctx, inputs, opts...)
		if err != nil {
			ch <- tool.StreamChunk{Error: err}
			return
		}
		ch <- tool.StreamChunk{Data: result}
	}()
	return ch, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// search 搜索主逻辑。Python: judge_search.py _search。扫描顺序：入口栈
// （Python list.pop() 后进先出）→ 条目上限 → symlink/逃逸跳过 → 目录展开 →
// glob 过滤 → 字节预算 → 二进制跳过 → UTF-8 解码（劣化 replace）→ 逐行匹配 →
// 输出字节与结果数上限。
func (t *JudgeSearchTool) search(inputs map[string]any) (map[string]any, error) {
	pattern, _ := inputs["pattern"].(string)
	if len(pattern) < 1 || len(pattern) > 512 {
		return nil, fmtErrorf("pattern must be a literal string of 1 to 512 characters")
	}
	limit := 50
	if raw, present := inputs["max_results"]; present && raw != nil {
		v, ok := toInt(raw)
		if !ok || v < 1 || v > 100 {
			return nil, fmtErrorf("max_results must be between 1 and 100")
		}
		limit = v
	}
	startValue, _ := inputs["path"].(string)
	if startValue == "" {
		startValue = "."
	}
	start := mustAbs(filepath.Join(t.workspace, startValue))
	rel, err := filepath.Rel(t.workspace, start)
	if err != nil || strings.HasPrefix(rel, "..") || !pathExists(start) {
		return nil, fmtErrorf("Search path must exist inside the frozen workspace")
	}
	ignoreCase, _ := inputs["ignore_case"].(bool)
	needle := pattern
	if ignoreCase {
		needle = caseFold(pattern)
	}
	glob := "*"
	if g, present := inputs["glob"]; present {
		if s, ok := g.(string); ok && s != "" {
			glob = s
		}
	}
	var matches []map[string]any
	limitations := map[string]bool{}
	pending := []string{start}
	entries, total := 0, 0
	outputLimitHit := false
	for len(pending) > 0 {
		path := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries++
		if entries > judgeSearchMaxEntries {
			limitations["entry_limit"] = true
			break
		}
		if isSymlink(path) || !withinWorkspace(t.workspace, path) {
			limitations["symlink_skipped"] = true
			continue
		}
		info, statErr := osStat(path)
		if statErr != nil {
			limitations["unreadable_path"] = true
			continue
		}
		if info.IsDir() {
			children, readErr := osReadDir(path)
			if readErr != nil {
				limitations["unreadable_path"] = true
				continue
			}
			// 不跟随目录链接；发现与读取同样受界（Python scandir 循环）。
			for _, child := range children {
				if entries+len(pending) >= judgeSearchMaxEntries {
					limitations["entry_limit"] = true
					break
				}
				pending = append(pending, filepath.Join(path, child))
			}
			continue
		}
		if !info.Mode().IsRegular() {
			limitations["non_regular_file"] = true
			continue
		}
		relative := filepath.ToSlash(relPath(t.workspace, path))
		if !globMatch(glob, relative) {
			continue
		}
		remaining := judgeSearchMaxTotalBytes - total
		if remaining <= 0 {
			limitations["total_byte_limit"] = true
			break
		}
		sizeLimit := judgeSearchMaxFileBytes
		if remaining < sizeLimit {
			sizeLimit = remaining
		}
		raw, readErr := osReadFileBounded(path, sizeLimit+1)
		if readErr != nil {
			limitations["unreadable_path"] = true
			continue
		}
		total += len(raw)
		if len(raw) > sizeLimit {
			limitations["file_byte_limit"] = true
			raw = raw[:sizeLimit]
		}
		if containsNullByte(raw) {
			limitations["binary_file_skipped"] = true
			continue
		}
		text := decodeUTF8Sigil(raw)
		lineNo := 0
		for _, line := range strings.Split(text, "\n") {
			lineNo++
			if lineNo > 1 && strings.HasSuffix(text[:0], "") {
				// Python enumerate(splitlines(), 1)：split 已按 \n 切分，行号即序号。
				_ = lineNo
			}
			haystack := line
			if ignoreCase {
				haystack = caseFold(line)
			}
			index := strings.Index(haystack, needle)
			if index < 0 {
				continue
			}
			excerptStart := index - 80
			if excerptStart < 0 {
				excerptStart = 0
			}
			excerptEnd := index + 240
			if excerptEnd > len(line) {
				excerptEnd = len(line)
			}
			excerpt := line[excerptStart:excerptEnd]
			match := map[string]any{
				"path": relative, "line": lineNo, "text": excerpt,
				"line_truncated": len(excerpt) != len(line),
			}
			// 保持实际结果在工具字节信封内（TOOL_BYTES-1500）。
			probe, _ := jsonMarshalCompactAny(matchesToMapSlice(matches, match))
			if len(probe) > ToolBytes-1500 {
				limitations["output_byte_limit"] = true
				outputLimitHit = true
				break
			}
			matches = append(matches, match)
			if len(matches) >= limit {
				limitations["result_limit"] = true
				outputLimitHit = true
				break
			}
		}
		if limitations["output_byte_limit"] || limitations["result_limit"] {
			break
		}
	}
	_ = outputLimitHit
	scanComplete := len(limitations) == 0
	limitedList := sortedKeys(limitations)
	return map[string]any{
		"matches":       toAnySlice(matches),
		"scan_complete": scanComplete,
		"limitations":   toAnyStrings(limitedList),
		"instruction": "Search misses and omitted evidence are not proof of absence. " +
			"Read relevant paths with read_evidence for complete content.",
	}, nil
}

// caseFold 大小写折叠（Python str.casefold 语义近似：小写化 + 去变音）。
func caseFold(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		return r
	}, s)
}

// matchesToMapSlice 追加一条匹配后返回副本（probe 序列化用）。
func matchesToMapSlice(matches []map[string]any, extra map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(matches)+1)
	out = append(out, matches...)
	out = append(out, extra)
	return out
}

// sortedKeys 排序输出 limitation 键（Python sorted(limitations)）。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
