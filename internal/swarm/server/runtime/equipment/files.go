package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxPreviewFileBytes 预览文件大小上限 1MiB。
// Python: _MAX_PREVIEW_FILE_BYTES = 1 * 1024 * 1024。
const maxPreviewFileBytes = 1 * 1024 * 1024

// previewSensitiveKeyPattern 敏感键（JSON 键/文本行键）。
// Python: _PREVIEW_SENSITIVE_KEY_PATTERN (:73-77)。
var previewSensitiveKeyPattern = regexp.MustCompile(`(?i)(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|authorization|auth[_-]?(?:code|token)|credential|private[_-]?key|user[_-]?id|project[_-]?id|amap[_-]?key|map[_-]?ak)`)

// previewPrivateKeyPattern PEM 私钥块。
// Python: _PREVIEW_PRIVATE_KEY_PATTERN (:78-80)。
var previewPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?-----END [^-\r\n]*PRIVATE KEY-----`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ListPackageFiles 包内文件树（agent_template/agent_group 两 kind 同构）。
// Python: list_agent_template_files (:3691-3694) / list_agent_group_files (:3655-3662)
// （经 _agent_template_preview_dir/_resolve_agent_group_definition_dir，本地半区）。
func ListPackageFiles(kindLabel, name string) ([]map[string]any, error) {
	pkgDir, err := previewDirFor(kindLabel, name)
	if err != nil {
		return nil, err
	}
	return buildFileTree(pkgDir, pkgDir), nil
}

// ReadPackageFile 读一个可预览文件（脱敏链）。
// Python: read_agent_template_file (:3696-3699) / read_agent_group_file (:3664-3670)
// → _read_previewable_file (:3570-3602)。
func ReadPackageFile(kindLabel, name, relPath string) (map[string]any, error) {
	pkgDir, err := previewDirFor(kindLabel, name)
	if err != nil {
		return nil, err
	}
	return readPreviewableFile(pkgDir, relPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// previewDirFor 预览用的包目录解析（含未安装货架项）。
// Python: _agent_template_preview_dir (:3305-3315)——经 resolve_equipment_runtime_id
// （Hub 记录反查，D-1 下 record 恒 nil → 原名直通）。
func previewDirFor(kindLabel, name string) (string, error) {
	packageID := name
	if record := NewHubInstallStateStore(kindForLabel(kindLabel)).Get(name); record != nil {
		packageID = record.PackageID
	}
	var pkgDir string
	var source string
	if kindLabel == "agent_group" {
		pkgDir, source, _ = resolveShowPackageDir(packageID, "agent_group")
	} else {
		pkgDir, source, _ = resolveShowPackageDir(packageID, "agent_template")
	}
	if pkgDir == "" {
		logger.Warn(logger.ComponentAgentServer).Str("package", packageID).
			Msg("[equipment] preview package not found")
		_ = source
		return "", &PackageConflictError{Message: kindLabel + " package not found: " + packageID}
	}
	return pkgDir, nil
}

// readPreviewableFile 单文件预览（类型门卫 + 大小门卫 + 脱敏）。
// Python: _read_previewable_file (:3570-3602)。
func readPreviewableFile(pkgDir, relPath string) (map[string]any, error) {
	rel := strings.TrimSpace(strings.ReplaceAll(relPath, "\\", "/"))
	if !isPreviewableFile(rel) {
		return nil, &PackageConflictError{Message: "file not previewable: " + rel}
	}
	fullPath, err := rejectPreviewPathSymlink(pkgDir, rel)
	if err != nil {
		return nil, err
	}
	info, statErr := os.Stat(fullPath)
	if statErr != nil {
		return nil, &PackageConflictError{Message: "file not found: " + rel}
	}
	size := info.Size()
	if strings.ToLower(filepath.Ext(rel)) == ".pdf" {
		// Python: build_file_download_info（web_file_download 半区，15.16f/资产账本）——
		// Go 侧 web_file_download.go 已有对应物（D-23.8-5 批）；此处返回路径形态对齐。
		return map[string]any{
			"path":    rel,
			"content": nil,
		}, nil
	}
	if size > maxPreviewFileBytes {
		return nil, &PackageConflictError{
			Message: rel + " (" + itoaStr(size) + " bytes)"}
	}
	data, readErr := os.ReadFile(fullPath)
	content := ""
	if readErr == nil {
		content = string(data)
	} else {
		content = "[二进制文件，大小 " + itoaStr(size) + " bytes]"
	}
	return map[string]any{
		"path":    rel,
		"content": redactPreviewContent(content, filepath.Ext(rel)),
	}, nil
}

// redactPreviewContent 凭据与 PII 脱敏。
// Python: _redact_preview_content (:190-228)：非 JSON 走文本行脱敏；
// JSON 解析成功走结构化脱敏。
func redactPreviewContent(content, suffix string) string {
	redactText := func(text string) string {
		text = previewPrivateKeyPattern.ReplaceAllString(text, "******")
		var out strings.Builder
		for _, line := range splitKeepEndings(text) {
			body := strings.TrimRight(line, "\r\n")
			ending := line[len(body):]
			idxColon := strings.Index(body, ":")
			idxEq := strings.Index(body, "=")
			idx := -1
			if idxColon >= 0 && (idxEq < 0 || idxColon < idxEq) {
				idx = idxColon
			} else if idxEq >= 0 {
				idx = idxEq
			}
			if idx >= 0 && previewSensitiveKeyPattern.MatchString(body[:idx]) {
				out.WriteString(body[:idx+1] + " ******" + ending)
				continue
			}
			out.WriteString(maskSensitiveLine(line))
		}
		return out.String()
	}
	if strings.ToLower(suffix) != ".json" {
		return redactText(content)
	}
	var value any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return redactText(content)
	}
	redacted := redactJSON(value)
	out, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return redactText(content)
	}
	return string(out)
}

// redactJSON JSON 结构化脱敏。
func redactJSON(item any) any {
	switch v := item.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			if previewSensitiveKeyPattern.MatchString(key) {
				out[key] = "******"
			} else {
				out[key] = redactJSON(child)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			out = append(out, redactJSON(child))
		}
		return out
	case string:
		return maskSensitiveLine(v)
	}
	return item
}

// splitKeepEndings 按行切分并保留行尾（对齐 Python splitlines(keepends=True)）。
func splitKeepEndings(text string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// maskSensitiveLine 行级脱敏（复用 common/utils MaskSensitive——
// Python 复用 mask_sensitive，同一真相源不重复实现正则）。
func maskSensitiveLine(line string) string {
	return utils.MaskSensitive(line)
}

// itoaStr int64 → 字符串。
func itoaStr(v int64) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}
