package attachments

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/attachmentsafety"
	sessionlifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxDocumentBytes 单个文档字节上限（base64 膨胀前）。
	// Python: document_attachments.py:31 _MAX_DOCUMENT_BYTES = 100MiB。
	MaxDocumentBytes = 100 * 1024 * 1024

	// MaxDocumentCount 单次文档上传条数上限（与图片 _MAX_IMAGE_COUNT 对齐）。
	// Python: document_attachments.py:33 _MAX_DOCUMENT_COUNT = 8（企业版 20，不移植）。
	MaxDocumentCount = 8
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrMissingDocSource 本地路径与 base64 内容均缺失。
// Python: :259 "Missing local path or base64 content for document upload"。
var ErrMissingDocSource = errors.New("Missing local path or base64 content for document upload")

// ──────────────────────────── 导出函数 ────────────────────────────

// PersistAndParseDocuments 校验文档条目并落盘 base64 内容，回填 chat 参数。
// 接受 params["documents"] 或 media_items 中 type=="document"（或无 type 但
// 文件名不在黑名单）的条目：本地 path 条目原位校验引用；base64 条目落盘会话
// uploads 并回填服务端 path。返回 params 并就地修改：
//   - media_items：保留混入的图片记录 + 文档记录
//   - files.uploaded_documents：五字段轻量元数据
//   - document_errors：被拒条目回显（超限不静默丢弃）
//   - forbidden_formats：黑名单列表兜底
//
// Python: document_attachments.py:58-161 persist_and_parse_documents
// （企业版 max_count=20 与 OBS 分支不移植，D-27.8-4）。
func PersistAndParseDocuments(params map[string]any, sessionID string) map[string]any {
	rawItems := collectDocumentItems(params)
	if len(rawItems) == 0 {
		delete(params, "documents")
		return params
	}

	stored := make([]map[string]any, 0, len(rawItems))
	var errorsList []any
	for index, item := range rawItems {
		if index >= MaxDocumentCount {
			// 超出上限的文档不静默丢弃，回显错误让前端可知。
			errorsList = append(errorsList, map[string]any{
				"index":    index,
				"filename": itemFilename(item),
				"error":    fmt.Sprintf("exceeds max document count (%d)", MaxDocumentCount),
			})
			continue
		}
		storedItem, err := resolveDocumentItem(item, index, sessionID)
		if err != nil {
			if storedItem == nil {
				errorsList = append(errorsList, map[string]any{
					"index":    index,
					"filename": itemFilename(item),
					"error":    err.Error(),
				})
				continue
			}
		}
		if storedItem != nil {
			stored = append(stored, storedItem)
		}
	}

	if len(stored) > 0 {
		// 混入的既有图片记录原样保留，仅替换文档段
		var keptImages []any
		if existing, ok := params["media_items"].([]any); ok {
			for _, entry := range existing {
				if m, ok := entry.(map[string]any); ok && m["type"] == "image" && m["path"] != nil {
					keptImages = append(keptImages, entry)
				}
			}
		}
		storedAny := make([]any, len(stored))
		for i, s := range stored {
			storedAny[i] = s
		}
		params["media_items"] = append(keptImages, storedAny...)
		// 剥离原始条目的 base64，防止 document.persist 响应整文件回显压爆内部帧
		stripDocumentBase64(params)
		files, ok := params["files"].(map[string]any)
		if !ok {
			files = map[string]any{}
		}
		summaries := make([]map[string]any, 0, len(stored))
		for _, item := range stored {
			summaries = append(summaries, map[string]any{
				"filename":      item["filename"],
				"path":          item["path"],
				"original_path": item["original_path"],
				"mime_type":     item["mime_type"],
				"size_bytes":    item["size_bytes"],
			})
		}
		files["uploaded_documents"] = summaries
		params["files"] = files
	} else {
		delete(params, "documents")
	}

	if len(errorsList) > 0 {
		params["document_errors"] = errorsList
	}
	params["forbidden_formats"] = attachmentsafety.ForbiddenFormats()
	return params
}

// ForbiddenFormats 黑名单列表转发。
// Python: attachment_safety.py:146-148（经 document_attachments re-export）。
func ForbiddenFormats() []string { return attachmentsafety.ForbiddenFormats() }

// IsSupportedDocument 文件名是否允许上传（未命中黑名单）。
// Python: attachment_safety.py:150-152（经 document_attachments re-export）。
func IsSupportedDocument(filename string) bool { return attachmentsafety.IsSupportedDocument(filename) }

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectDocumentItems 收集 documents 与 media_items 中的文档条目。
// Python: document_attachments.py:164-186 _collect_document_items。
func collectDocumentItems(params map[string]any) []map[string]any {
	var items []map[string]any
	if documents, ok := params["documents"].([]any); ok {
		for _, raw := range documents {
			if m, ok := raw.(map[string]any); ok {
				items = append(items, m)
			}
		}
	}
	if mediaItems, ok := params["media_items"].([]any); ok {
		for _, raw := range mediaItems {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			itemType, _ := m["type"].(string)
			if itemType == "document" ||
				(itemType == "" && attachmentsafety.IsSupportedDocument(itemFilename(m))) {
				items = append(items, m)
			}
		}
	}
	return items
}

// stripDocumentBase64 就地剥离原始条目的 base64 内容。
// Python: document_attachments.py:189-207 _strip_document_base64。
func stripDocumentBase64(params map[string]any) {
	strip := func(raw any) {
		m, ok := raw.(map[string]any)
		if !ok {
			return
		}
		delete(m, "base64Data")
		delete(m, "base64_data")
	}
	if documents, ok := params["documents"].([]any); ok {
		for _, raw := range documents {
			strip(raw)
		}
	}
	if mediaItems, ok := params["media_items"].([]any); ok {
		for _, raw := range mediaItems {
			if m, ok := raw.(map[string]any); ok && m["type"] == "document" {
				strip(raw)
			}
		}
	}
}

// resolveDocumentItem 解析单个文档条目：_persisted 透传 / base64 落盘 / 本地 path 校验。
// Python: document_attachments.py:210-280 _resolve_document_item。
func resolveDocumentItem(item map[string]any, index int, sessionID string) (map[string]any, error) {
	filenameHint := itemFilename(item)
	mimeType := strings.ToLower(strings.TrimSpace(mimeOfType(item)))

	// _persisted：Gateway 分流已写盘，直接透传（Python :218-237）
	if persisted, ok := item["_persisted"]; ok && persisted == true {
		path, _ := item["path"].(string)
		if strings.TrimSpace(path) == "" {
			return nil, nil
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return nil, nil
		}
		mt := mimeType
		if mt == "" {
			mt = "application/octet-stream"
		}
		return map[string]any{
			"type":          "document",
			"filename":      filepath.Base(path),
			"mime_type":     mt,
			"path":          path,
			"original_path": path,
			"size_bytes":    fi.Size(),
		}, nil
	}

	// 浏览器上传：base64 内容落盘（Python :240-245）
	rawBase64 := firstNonEmptyString(item["base64Data"], item["base64_data"])
	if strings.TrimSpace(rawBase64) != "" {
		return storeDocumentItem(item, rawBase64, filenameHint, mimeType, index, sessionID)
	}

	// 本地 path：校验后原位引用（Python :247-280）
	rawPath := strings.TrimSpace(firstNonEmptyString(item["path"], item["original_path"], item["originalPath"]))
	if rawPath == "" {
		return nil, ErrMissingDocSource
	}
	path := resolveClean(rawPath)
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("File not found: %s", path)
	}
	filename := filenameHint
	if filename == "" {
		filename = filepath.Base(path)
	}
	if filename == "" {
		filename = fmt.Sprintf("document-%d", index+1)
	}
	if attachmentsafety.IsForbiddenDocument(filename, "") || attachmentsafety.IsForbiddenDocument(filepath.Base(path), "") {
		return nil, fmt.Errorf(
			"Forbidden document type: filename=%q path=%q. Forbidden: %v",
			filename, filepath.Base(path), attachmentsafety.ForbiddenFormats())
	}
	mt := mimeType
	if mt == "" {
		mt = "application/octet-stream"
	}
	return map[string]any{
		"type":          "document",
		"filename":      filepath.Base(filename),
		"mime_type":     mt,
		"path":          path,
		"original_path": path,
		"size_bytes":    fi.Size(),
	}, nil
}

// storeDocumentItem 解码浏览器 base64 文档并落盘会话 uploads。
// 与 media_attachments.storeImageItem 同构：先归一化文件名再做黑名单校验，
// 防 "evil.exe "（尾空格）绕过后落盘成 "evil.exe"。
// Python: document_attachments.py:283-337 _store_document_item。
func storeDocumentItem(item map[string]any, base64Data, filenameHint, mimeType string, index int, sessionID string) (map[string]any, error) {
	_ = item
	data, err := base64.StdEncoding.DecodeString(attachmentsafety.StripDataURIPrefix(base64Data))
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("Invalid or empty base64 content for document %q", filenameHint)
	}
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("Document too large: %d bytes exceeds %d bytes", len(data), MaxDocumentBytes)
	}
	filename := filenameHint
	if filename == "" {
		filename = fmt.Sprintf("document-%d", index+1)
	}
	safeName := SafeUploadFilename(filename, fmt.Sprintf("document-%d", index+1))
	if attachmentsafety.IsForbiddenDocument(safeName, "") {
		return nil, fmt.Errorf(
			"Forbidden document type: filename=%q. Forbidden: %v",
			safeName, attachmentsafety.ForbiddenFormats())
	}
	uploadDir := filepath.Join(sessionlifecycle.GetAgentSessionsDir(),
		attachmentsafety.SafeSessionDirname(sessionID), "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}
	path, err := AtomicWriteUnique(filepath.Join(uploadDir, safeName), data)
	if err != nil {
		return nil, err
	}
	mt := mimeType
	if mt == "" {
		mt = "application/octet-stream"
	}
	return map[string]any{
		"type":          "document",
		"filename":      filepath.Base(path),
		"mime_type":     mt,
		"path":          path,
		"original_path": path,
		"size_bytes":    len(data),
	}, nil
}

// itemFilename 取条目的 filename 或 name 提示名。
func itemFilename(item map[string]any) string {
	return firstNonEmptyString(item["filename"], item["name"])
}

// resolveClean 展开 ~ 后取绝对路径（Python Path.expanduser().resolve(strict=False) 的等价）。
func resolveClean(raw string) string {
	if strings.HasPrefix(raw, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return filepath.Clean(raw)
	}
	return filepath.Clean(abs)
}
