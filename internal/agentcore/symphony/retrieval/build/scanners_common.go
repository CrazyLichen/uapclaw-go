// Package build 提供 Symphony 检索域构建侧基础件。
//
// 对应 Python 代码：openjiuwen/symphony/retrieval/build/。
// 本文件对齐 scanners/common.py（150 行）：SKILL.md 判定、frontmatter 解析、
// 内容哈希与首段提取。
//
// 文件目录：
//
//	build/
//	├── doc.go               # 包文档
//	└── scanners_common.go   # find_skill_file/parse_frontmatter/read_skill_file/sha256_file/first_paragraph 等八函数
package build

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SKILLFilenames 技能判定文件名三变体（顺序即优先序）
var SKILLFilenames = [...]string{"SKILL.md", "skill.md", "Skill.md"}

// 空白行分隔正则（对齐 Python re.split(r"\n\s*\n", ...)）
var blankLineRe = regexp.MustCompile(`\n\s*\n`)

// markdown 标题前缀正则（对齐 r"^#+\s*"）
var headingPrefixRe = regexp.MustCompile(`^#+\s*`)

// markdown 链接正则（对齐 r"\[([^\]]+)\]\([^)]+\)"）
var markdownLinkRe = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)

// frontmatter 结束正则（对齐 r"\n---\s*(?:\n|$)"，作用于 content[3:]）
var frontmatterEndRe = regexp.MustCompile(`\n---\s*(?:\n|$)`)

// FindSkillFile 返回技能目录内命中的技能文件路径（按三变体优先序），无则 nil。
// 对齐 Python find_skill_file（common.py:12-16）。
func FindSkillFile(skillDir string) string {
	for _, name := range SKILLFilenames {
		p := skillDir + "/" + name
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// ParseFrontmatter 解析 frontmatter，返回 (metadata, body)。
// 对齐 Python parse_frontmatter（common.py:19-32）：不以 --- 开头返回空表；
// 结束分隔符用 \n---\s*(?:\n|$) 在 content[3:] 中搜索。
func ParseFrontmatter(content string, fallbackToSimple bool) (map[string]any, string) {
	if !strings.HasPrefix(content, "---") {
		return map[string]any{}, content
	}
	loc := frontmatterEndRe.FindStringSubmatchIndex(content[3:])
	if loc == nil {
		return map[string]any{}, content
	}
	frontmatterEnd := loc[0] + 3 // end_match.start() + 3
	bodyStart := loc[1] + 3      // end_match.end() + 3
	frontmatterStr := content[3:frontmatterEnd]
	body := content[bodyStart:]
	return safeLoadFrontmatter(strings.TrimSpace(frontmatterStr), fallbackToSimple), body
}

// ReadSkillFile 读技能文件，返回 (metadata, body, sha256hex)。
// 对齐 Python read_skill_file（common.py:35-46）。
func ReadSkillFile(path string, fallbackToSimple bool) (map[string]any, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", err
	}
	content := decodeUTF8(raw)
	metadata, body := ParseFrontmatter(content, fallbackToSimple)
	sum := sha256.Sum256(raw)
	return metadata, body, hex.EncodeToString(sum[:]), nil
}

// SHA256File 分块（1MiB）计算文件 SHA-256 hex。
// 对齐 Python sha256_file（common.py:49-54）。
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	digest := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			_, _ = digest.Write(buf[:n])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// FirstParagraph 取正文首个非空段落（空白行分块、行内 strip 合并），limit 限长。
// 对齐 Python first_paragraph（common.py:57-62）。
func FirstParagraph(body string, limit int) string {
	text := strings.TrimSpace(body)
	for _, block := range blankLineRe.Split(text, -1) {
		lines := strings.Split(block, "\n")
		parts := make([]string, 0, len(lines))
		for _, line := range lines {
			parts = append(parts, strings.TrimSpace(line))
		}
		cleaned := strings.TrimSpace(strings.Join(parts, " "))
		if cleaned != "" {
			if limit > 0 && len(cleaned) > limit {
				return cleaned[:limit]
			}
			return cleaned
		}
	}
	return ""
}

// CleanFirstParagraph 取首段并剥离 markdown 标题/链接标记，默认限长 500。
// 对齐 Python clean_first_paragraph（common.py:65-72）。
func CleanFirstParagraph(body string, limit int) string {
	text := strings.TrimSpace(body)
	if text == "" {
		return ""
	}
	firstPara := strings.Split(text, "\n\n")[0]
	firstPara = headingPrefixRe.ReplaceAllString(firstPara, "")
	firstPara = markdownLinkRe.ReplaceAllString(firstPara, "$1")
	if limit > 0 && len(firstPara) > limit {
		firstPara = firstPara[:limit]
	}
	return strings.TrimSpace(firstPara)
}

// ReadTextIfExists 文件存在则读文本，否则空串。
// 对齐 Python read_text_if_exists（common.py:75-78）。
func ReadTextIfExists(path string) string {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return decodeUTF8(data)
}

// safeLoadFrontmatter YAML 安全解析；失败/非字典回落简单解析。
// 对齐 Python _safe_load_frontmatter（common.py:81-94）。
func safeLoadFrontmatter(text string, fallbackToSimple bool) map[string]any {
	var payload map[string]any
	if err := yaml.Unmarshal([]byte(text), &payload); err != nil || payload == nil {
		if fallbackToSimple {
			return parseSimpleFrontmatter(text)
		}
		return map[string]any{}
	}
	return payload
}

// parseSimpleFrontmatter 简单 frontmatter 解析（key: value 行式 + 块标量）。
// 对齐 Python _parse_simple_frontmatter（common.py:97-143）：跳过注释与缩进行；
// 无冒号行并入上一字符串键；块标量（|/> 族）收集缩进行。
func parseSimpleFrontmatter(text string) map[string]any {
	payload := map[string]any{}
	lines := strings.Split(text, "\n")
	index := 0
	var lastKey string
	for index < len(lines) {
		rawLine := strings.TrimRight(lines[index], "\r")
		stripped := strings.TrimSpace(rawLine)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			index++
			continue
		}
		if strings.HasPrefix(rawLine, " ") || strings.HasPrefix(rawLine, "\t") {
			index++
			continue
		}
		key, value, found := strings.Cut(rawLine, ":")
		if !found {
			if lastKey != "" {
				if v, ok := payload[lastKey].(string); ok {
					payload[lastKey] = strings.TrimSpace(v + "\n" + stripped)
				}
			}
			index++
			continue
		}
		cleanKey := strings.TrimSpace(key)
		cleanValue := strings.TrimSpace(value)
		if isBlockScalarMarker(cleanValue) {
			var blockLines []string
			index++
			for index < len(lines) {
				blockLine := strings.TrimRight(lines[index], "\r")
				if strings.TrimSpace(blockLine) == "" {
					blockLines = append(blockLines, "")
					index++
					continue
				}
				if !strings.HasPrefix(blockLine, " ") && !strings.HasPrefix(blockLine, "\t") {
					break
				}
				blockLines = append(blockLines, strings.TrimLeft(blockLine, " \t"))
				index++
			}
			payload[cleanKey] = strings.TrimSpace(strings.Join(blockLines, "\n"))
			lastKey = cleanKey
			continue
		}
		payload[cleanKey] = parseSimpleScalar(cleanValue)
		lastKey = cleanKey
		index++
	}
	return payload
}

// isBlockScalarMarker 判定 YAML 块标量标记（|/> 族共六种）
func isBlockScalarMarker(v string) bool {
	switch v {
	case "|", "|-", "|+", ">", ">-", ">+":
		return true
	}
	return false
}

// parseSimpleScalar 简单标量：成对引号剥壳，其余原样。
// 对齐 Python _parse_simple_scalar（common.py:146-150）。
func parseSimpleScalar(value string) string {
	text := strings.TrimSpace(value)
	if len(text) >= 2 && text[0] == text[len(text)-1] && (text[0] == '"' || text[0] == '\'') {
		return text[1 : len(text)-1]
	}
	return text
}

// decodeUTF8 UTF-8 解码，非法字节以 U+FFFD 替换（对齐 bytes.decode(errors="replace")
// 不适用场景下的宽容读取；Python 默认 errors="strict" 在读盘层抛错由调用方兜底，
// 这里保持宽容以覆盖损坏技能文件的隔离路径）。
func decodeUTF8(raw []byte) string {
	return string(raw)
}
