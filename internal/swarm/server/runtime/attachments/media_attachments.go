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
	// MaxImageBytes 单张图片字节上限。
	// Python: media_attachments.py:31 _MAX_IMAGE_BYTES = 10MiB。
	MaxImageBytes = 10 * 1024 * 1024

	// MaxImageCount 单次上传图片条数上限。
	// Python: media_attachments.py:32 _MAX_IMAGE_COUNT = 8。
	MaxImageCount = 8
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrMediaPathRequired media.discard：path 必填。
// Python: media_attachments.py:65 "path is required"。
var ErrMediaPathRequired = errors.New("path is required")

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeChatMediaAttachments 校验浏览器 media_items、落盘图片并回填 chat 参数。
// 图片持久化到当前会话 uploads 目录并转为结构化文件记录，下游多模态 rail 从
// path 加载，避免长 base64 进正文上下文。
// 处理后 media_items 全无效时删除该 key；有效时回填 stored 记录并向
// params["files"]["uploaded_images"] 追加四字段摘要。
// Python: media_attachments.py:95-147 normalize_chat_media_attachments（就地修改 params）。
func NormalizeChatMediaAttachments(params map[string]any, sessionID string) {
	rawItems, ok := params["media_items"].([]any)
	if !ok || len(rawItems) == 0 {
		return
	}

	stored := make([]map[string]any, 0, len(rawItems))
	for index, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if item["type"] != "image" {
			continue
		}
		if len(stored) >= MaxImageCount {
			break
		}
		if storedItem := storeImageItem(item, sessionID, index); storedItem != nil {
			stored = append(stored, storedItem)
		}
	}

	if len(stored) == 0 {
		delete(params, "media_items")
		return
	}

	storedAny := make([]any, len(stored))
	for i, s := range stored {
		storedAny[i] = s
	}
	params["media_items"] = storedAny
	files, ok := params["files"].(map[string]any)
	if !ok {
		files = map[string]any{}
	}
	summaries := make([]map[string]any, 0, len(stored))
	for _, item := range stored {
		summaries = append(summaries, map[string]any{
			"filename":   item["filename"],
			"path":       item["path"],
			"mime_type":  item["mime_type"],
			"size_bytes": item["size_bytes"],
		})
	}
	files["uploaded_images"] = summaries
	params["files"] = files
}

// DiscardSessionUpload 删除当前会话 uploads 目录里的一个普通文件。
// 只接受该目录的直接子文件；symlink、目录、其他会话与用户原图路径一律拒绝。
// 文件已不存在返回 {"deleted": false}——重复删除是成功。
// Python: media_attachments.py:55-88 discard_session_upload。
func DiscardSessionUpload(sessionID, rawPath string) (map[string]any, error) {
	if strings.TrimSpace(rawPath) == "" {
		return nil, ErrMediaPathRequired
	}
	if !filepath.IsAbs(rawPath) {
		return nil, errors.New("path must be absolute")
	}
	name := filepath.Base(rawPath)
	if name == "" || name == "." || name == ".." {
		return nil, errors.New("invalid filename")
	}
	uploadDir := filepath.Join(sessionlifecycle.GetAgentSessionsDir(),
		attachmentsafety.SafeSessionDirname(sessionID), "uploads")
	// 直接子文件判定（resolve 后 parent 相等；单机非 Windows 直接字符串比较）
	if filepath.Dir(filepath.Clean(rawPath)) != filepath.Clean(uploadDir) {
		return nil, errors.New("path is outside session uploads")
	}
	target := filepath.Join(uploadDir, name)
	fi, err := os.Lstat(target)
	if err != nil || isSymlink(fi) {
		// symlink 与不存在统一走下方 exists 分支——symlink 单独拒绝
		if err == nil && isSymlink(fi) {
			return nil, errors.New("refusing to delete symlink")
		}
		return map[string]any{"deleted": false}, nil
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("path is not a file")
	}
	if err := os.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"deleted": false}, nil
		}
		return nil, err
	}
	return map[string]any{"deleted": true}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// storeImageItem 校验并落盘单张图片，返回 stored 记录；无效项返回 nil（静默跳过）。
// Gateway 侧大图分流已落盘的 _persisted 项直接透传记录，不重复解码/写盘，
// path 不存在视为无效丢弃。
// Python: media_attachments.py:150-202 _store_image_item。
func storeImageItem(item map[string]any, sessionID string, index int) map[string]any {
	mimeType := strings.ToLower(strings.TrimSpace(mimeOfType(item)))
	suffix := attachmentsafety.ImageSuffixForMime(mimeType)
	if suffix == "" {
		return nil
	}

	// _persisted：Gateway 分流已写盘，直接透传落盘记录
	if persisted, ok := item["_persisted"]; ok && persisted == true {
		path, _ := item["path"].(string)
		if strings.TrimSpace(path) == "" {
			return nil
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		return map[string]any{
			"type":       "image",
			"filename":   filepath.Base(path),
			"mime_type":  mimeType,
			"path":       path,
			"size_bytes": fi.Size(),
		}
	}

	rawBase64 := firstNonEmptyString(item["base64Data"], item["base64_data"])
	if strings.TrimSpace(rawBase64) == "" {
		return nil
	}
	payload := attachmentsafety.StripDataURIPrefix(rawBase64)
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Python: b64decode(validate=True) 的 binascii.Error 被 suppress 吞掉 → 项无效
		return nil
	}
	if len(data) == 0 || len(data) > MaxImageBytes {
		return nil
	}

	uploadDir := filepath.Join(sessionlifecycle.GetAgentSessionsDir(),
		attachmentsafety.SafeSessionDirname(sessionID), "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil
	}
	fallback := fmt.Sprintf("image-%d%s", index+1, suffix)
	filename := attachmentsafety.EnsureImageUploadFilename(
		SafeUploadFilename(firstNonEmptyString(item["filename"], fallback), fallback), suffix)
	path, err := AtomicWriteUnique(filepath.Join(uploadDir, filename), data)
	if err != nil {
		return nil
	}
	return map[string]any{
		"type":       "image",
		"filename":   filepath.Base(path),
		"mime_type":  mimeType,
		"path":       path,
		"size_bytes": len(data),
	}
}

// mimeOfType 取 item 的 mimeType 或 mime_type（snake/camel 双形，Python 同款取法）。
func mimeOfType(item map[string]any) string {
	for _, key := range []string{"mimeType", "mime_type"} {
		if v, ok := item[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyString 依序取第一个非空字符串，全空返回 ""。
func firstNonEmptyString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
