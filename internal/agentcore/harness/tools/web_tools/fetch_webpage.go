package web_tools

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FetchWebpageInput fetch_webpage 工具的输入参数
// 对齐 Python: WebFetchWebpageTool.invoke inputs (web_tools.py L1598-1603)
type FetchWebpageInput struct {
	// URL 要抓取的网页 URL
	URL string `json:"url"`
	// MaxChars 返回内容最大字符数
	MaxChars int `json:"max_chars"`
	// TimeoutSeconds 请求超时时间
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// charsetHeaderRe Content-Type 中 charset 正则
	// 对齐 Python: _CHARSET_HEADER_RE (web_tools.py L33)
	charsetHeaderRe = regexp.MustCompile(`(?i)charset=([^\s;]+)`)
	// charsetMetaRe HTML meta charset 正则
	// 对齐 Python: _CHARSET_META_RE (web_tools.py L34-37)
	charsetMetaRe = regexp.MustCompile(`(?i)<meta[^>]+charset=["']?\s*([A-Za-z0-9._-]+)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebFetchWebpageTool 创建网页抓取工具
// 对齐 Python: WebFetchWebpageTool.__init__ + invoke (web_tools.py L1384-1632)
func NewWebFetchWebpageTool(language, agentID string) tool.Tool {
	card, _ := hprompts.BuildToolCard("fetch_webpage", "WebFetchWebpageTool", language, nil, agentID)

	fn := func(ctx context.Context, input FetchWebpageInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: WebFetchWebpageTool.invoke (web_tools.py L1596-1627)
		rawURL := normalizeURL(strings.TrimSpace(input.URL))
		maxChars := input.MaxChars
		timeoutSeconds := input.TimeoutSeconds

		if rawURL == "" {
			return map[string]any{"result": "[ERROR]: url cannot be empty."}, nil
		}

		if maxChars <= 0 {
			maxChars = fetchWebpageDefaultMaxChars
		}
		if timeoutSeconds <= 0 {
			timeoutSeconds = fetchWebpageDefaultTimeoutSeconds
		}

		// 对齐 Python: L1608-1612 — 环境变量上限
		maxCharsCap := 200000
		if envVal := os.Getenv(fetchWebpageMaxCharsEnv); envVal != "" {
			if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
				maxCharsCap = parsed
			}
		}
		timeoutCap := 600
		if envVal := os.Getenv(fetchWebpageMaxTimeoutEnv); envVal != "" {
			if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
				timeoutCap = parsed
			}
		}
		if maxChars != 0 {
			if maxChars < 500 {
				maxChars = 500
			}
			if maxChars > maxCharsCap {
				maxChars = maxCharsCap
			}
		}
		if timeoutSeconds < 5 {
			timeoutSeconds = 5
		}
		if timeoutSeconds > timeoutCap {
			timeoutSeconds = timeoutCap
		}

		// 对齐 Python: L1614-1617 — 抓取网页
		data, err := fetchWebpage(rawURL, timeoutSeconds)
		if err != nil {
			logger.Error(logComponent).Str("url", rawURL).Err(err).Msg("获取网页失败")
			return map[string]any{"result": fmt.Sprintf("[ERROR]: failed to fetch webpage: %s", err)}, nil
		}

		// 对齐 Python: L1619-1627 — 格式化输出
		var lines []string
		lines = append(lines, fmt.Sprintf("URL: %s", data["url"]))
		lines = append(lines, fmt.Sprintf("Status: %v", data["status_code"]))
		if title, ok := data["title"].(string); ok && title != "" {
			lines = append(lines, fmt.Sprintf("Title: %s", title))
		}
		lines = append(lines, "Content:")
		content := ""
		if c, ok := data["content"].(string); ok {
			content = c
		}
		if maxChars > 0 {
			content = clipText(content, maxChars)
		}
		if content == "" {
			content = "[empty]"
		}
		lines = append(lines, content)

		return map[string]any{"result": strings.Join(lines, "\n")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchWebpage 抓取网页内容（带 Jina 降级）
// 对齐 Python: WebFetchWebpageTool._fetch_webpage_sync() (web_tools.py L1555-1576)
func fetchWebpage(rawURL string, timeoutSeconds int) (map[string]any, error) {
	resp, err := httpRequest("GET", rawURL,
		withHeaders(map[string]string{"User-Agent": userAgent}),
		withTimeout(timeoutSeconds),
	)
	if err != nil {
		return nil, fmt.Errorf("获取网页失败: %w", err)
	}

	// 对齐 Python: L1558-1559 — 401/403/429 降级到 Jina
	if resp.statusCode == 401 || resp.statusCode == 403 || resp.statusCode == 429 {
		return fetchViaJinaReader(rawURL, timeoutSeconds)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		return nil, err
	}

	text := decodeResponseText(resp)
	contentType := strings.ToLower(resp.headers.Get("Content-Type"))
	title := ""

	// 对齐 Python: L1566-1569
	if strings.Contains(contentType, "html") {
		title, text = extractMainTextFromHTML(text)
	} else {
		text = multiSpaceRe.ReplaceAllString(text, " ")
		text = strings.TrimSpace(text)
	}

	return map[string]any{
		"url":         resp.finalURL,
		"status_code": resp.statusCode,
		"title":       title,
		"content":     text,
	}, nil
}

// fetchViaJinaReader 通过 Jina Reader 代理抓取
// 对齐 Python: WebFetchWebpageTool._fetch_via_jina_reader_sync() (web_tools.py L1470-1480)
func fetchViaJinaReader(rawURL string, timeoutSeconds int) (map[string]any, error) {
	readerURL := fmt.Sprintf("https://r.jina.ai/%s", rawURL)
	resp, err := httpRequest("GET", readerURL,
		withHeaders(map[string]string{"User-Agent": userAgent}),
		withTimeout(timeoutSeconds),
	)
	if err != nil {
		return nil, fmt.Errorf("Jina Reader 获取失败: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		return nil, err
	}

	return map[string]any{
		"url":         rawURL,
		"status_code": resp.statusCode,
		"title":       "",
		"content":     strings.TrimSpace(decodeResponseText(resp)),
	}, nil
}

// decodeResponseText 多编码检测解码响应文本
// 对齐 Python: WebFetchWebpageTool._decode_response_text() (web_tools.py L1422-1467)
func decodeResponseText(resp *httpResponse) string {
	raw := resp.body
	if len(raw) == 0 {
		return ""
	}

	// 对齐 Python: L1428-1429
	declared := strings.ToLower(extractDeclaredCharset(resp))

	var candidates []string
	// 对齐 Python: L1433-1434 — 声明编码优先（排除 iso-8859-1 等）
	if declared != "" && declared != "iso-8859-1" && declared != "latin-1" && declared != "latin1" {
		candidates = append(candidates, declared)
	}
	// 对齐 Python: apparent_encoding 自动检测
	if _, detectedName, ok := charset.DetermineEncoding(raw, ""); ok {
		detectedName = strings.ToLower(detectedName)
		if detectedName != "" && detectedName != declared && detectedName != "utf-8" {
			candidates = append(candidates, detectedName)
		}
	}
	// 对齐 Python: L1435-1448
	candidates = append(candidates,
		"utf-8-sig", "utf-8",
		"gbk", "gb18030", "big5", "shift_jis", "cp1252", "iso-8859-1",
	)

	type decodedCandidate struct {
		score float64
		text  string
	}
	var decodedCandidates []decodedCandidate
	seen := map[string]bool{}
	for _, enc := range candidates {
		enc = strings.TrimSpace(strings.ToLower(enc))
		if enc == "" || seen[enc] {
			continue
		}
		seen[enc] = true
		decoded, err := decodeBytes(raw, enc)
		if err != nil {
			continue
		}
		decodedCandidates = append(decodedCandidates, decodedCandidate{
			score: scoreDecodedText(decoded),
			text:  decoded,
		})
	}

	// 对齐 Python: L1463-1465 — 按分数排序取最佳
	if len(decodedCandidates) > 0 {
		best := decodedCandidates[0]
		for _, c := range decodedCandidates[1:] {
			if c.score > best.score {
				best = c
			}
		}
		return best.text
	}

	// 对齐 Python: L1467 — 降级
	return string(raw)
}

// extractDeclaredCharset 从响应头或 HTML meta 标签中提取 charset
// 对齐 Python: WebFetchWebpageTool._extract_declared_charset() (web_tools.py L1391-1405)
func extractDeclaredCharset(resp *httpResponse) string {
	// 对齐 Python: L1393-1396 — Content-Type 头
	contentType := resp.headers.Get("Content-Type")
	if match := charsetHeaderRe.FindStringSubmatch(contentType); len(match) > 1 {
		return strings.Trim(match[1], ` "'`)
	}

	// 对齐 Python: L1398-1404 — HTML meta 标签
	headBytes := resp.body
	if len(headBytes) > 4096 {
		headBytes = headBytes[:4096]
	}
	if match := charsetMetaRe.FindSubmatch(headBytes); len(match) > 1 {
		return strings.TrimSpace(string(match[1]))
	}
	return ""
}

// scoreDecodedText 对解码后的文本评分（避免乱码）
// 对齐 Python: WebFetchWebpageTool._score_decoded_text() (web_tools.py L1408-1419)
func scoreDecodedText(value string) float64 {
	if value == "" {
		return -1e9
	}
	score := 0.0
	// 对齐 Python: L1413 — 替换字符惩罚
	replacementCount := strings.Count(value, "\ufffd")
	score -= float64(replacementCount) * 8
	// 对齐 Python: L1414-1415 — 乱码标记惩罚
	for _, marker := range mojibakeMarkers {
		score -= float64(strings.Count(value, marker)) * 3
	}
	// 对齐 Python: L1416 — CJK 字符奖励
	cjkCount := 0
	for _, ch := range value {
		if ch >= '\u4e00' && ch <= '\u9fff' {
			cjkCount++
		}
	}
	score += float64(cjkCount) * 0.15
	// 对齐 Python: L1417 — 英文字母数字奖励
	alnumCount := 0
	for _, ch := range value {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			alnumCount++
		}
	}
	score += float64(alnumCount) * 0.02
	// 对齐 Python: L1418 — 控制字符惩罚
	ctrlCount := 0
	for _, ch := range value {
		if (ch >= 0x00 && ch <= 0x08) || ch == 0x0b || ch == 0x0c || (ch >= 0x0e && ch <= 0x1f) {
			ctrlCount++
		}
	}
	score -= float64(ctrlCount) * 5
	return score
}

// decodeBytes 使用指定编码解码字节
func decodeBytes(data []byte, encoding string) (string, error) {
	enc := getEncoder(encoding)
	if enc == nil {
		// 未知编码或 UTF-8，降级为 UTF-8
		text := string(data)
		// 去除 UTF-8 BOM（\xEF\xBB\xBF），对齐 Python utf-8-sig 行为
		text = strings.TrimPrefix(text, "\xEF\xBB\xBF")
		return text, nil
	}
	decoded, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return "", err // 对齐 Python: strict 模式，解码失败时抛出异常
	}
	text := string(decoded)
	// 去除 UTF-8 BOM（\xEF\xBB\xBF），对齐 Python utf-8-sig 行为
	text = strings.TrimPrefix(text, "\xEF\xBB\xBF")
	return text, nil
}

// getEncoder 根据编码名称获取编码器
func getEncoder(name string) encoding.Encoding {
	switch strings.ToLower(name) {
	case "utf-8", "utf8":
		return nil // UTF-8 无需转换
	case "utf-8-sig":
		return nil // UTF-8 无需编码转换，BOM 在 decodeBytes 中去除
	case "gbk":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "big5":
		return traditionalchinese.Big5
	case "shift_jis":
		return japanese.ShiftJIS
	case "cp1252":
		return charmap.Windows1252
	case "iso-8859-1", "latin-1", "latin1":
		return charmap.ISO8859_1
	default:
		return nil
	}
}
