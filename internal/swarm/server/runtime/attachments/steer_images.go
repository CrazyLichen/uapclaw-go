package attachments

import (
	"bytes"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/attachmentsafety"
	sessionlifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxSteerImageBytes steer 图片字节上限。
	// Python: steer_images.py:20 MAX_STEER_IMAGE_BYTES = 10MiB。
	MaxSteerImageBytes = 10 * 1024 * 1024

	// MaxSteerImagePixels steer 图片解码像素上限（防解压炸弹）。
	// Python: steer_images.py:21 MAX_STEER_IMAGE_PIXELS = 40_000_000。
	MaxSteerImagePixels = 40_000_000
)

// steerImageFields steer 引用的合法字段集（多余字段拒绝——引用不带内联数据）。
// Python: runtime/session_input.py:10 _STEER_IMAGE_FIELDS。
var steerImageFields = map[string]struct{}{
	"type": {}, "path": {}, "filename": {}, "mime_type": {}, "size_bytes": {},
}

// steerImageMimeTypes steer 引用受支持的 MIME 集。
// Python: runtime/session_input.py:11 _STEER_IMAGE_MIME_TYPES。
var steerImageMimeTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/webp": {}, "image/gif": {},
}

// imageFormatMimeTypes 真实解码格式→MIME。
// Python: steer_images.py:22 _IMAGE_FORMAT_MIME_TYPES（WEBP 由 Go image 包
// 不内置支持，经 golang.org/x/image/webp 注册——见 init）。
var imageFormatMimeTypes = map[string]string{
	// Go image 包注册的格式名为小写
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"webp": "image/webp",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateSteerImageReferences 校验 steer 引用的线上形状（不触盘）。
// Python: runtime/session_input.py:62-82 validate_steer_image_references：
//   - 携带 images/image_files/files/attachments/audio_files/video_files 任一 → 拒绝
//     （steer 只收上传图片引用，其他附件走排队任务）
//   - media_items 缺省 → 空列表
//   - 非 list / 条目带字段集外字段 / type != image / path-filename-mime_type
//     非非空字符串 / MIME 不在白名单 → 各自文案拒绝
func ValidateSteerImageReferences(params map[string]any) ([]map[string]any, error) {
	for _, key := range []string{"images", "image_files", "files", "attachments", "audio_files", "video_files"} {
		if v, ok := params[key]; ok && v != nil && v != false && v != "" {
			return nil, errors.New("steering accepts uploaded image references only; send other attachments as a queued task")
		}
	}
	raw, exists := params["media_items"]
	if !exists || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("steer media_items must be a list of uploaded image references")
	}
	out := make([]map[string]any, 0, len(items))
	for _, entry := range items {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New("steer images require uploaded file references without inline data")
		}
		for key := range item {
			if _, allowed := steerImageFields[key]; !allowed {
				return nil, errors.New("steer images require uploaded file references without inline data")
			}
		}
		if item["type"] != "image" {
			return nil, errors.New("steering accepts uploaded images only")
		}
		for _, key := range []string{"path", "filename", "mime_type"} {
			v, ok := item[key].(string)
			if !ok || strings.TrimSpace(v) == "" {
				return nil, fmt.Errorf("steer image %s must be a non-empty string", key)
			}
		}
		mt := item["mime_type"].(string)
		if _, ok := steerImageMimeTypes[mt]; !ok {
			return nil, errors.New("unsupported steer image MIME type")
		}
		out = append(out, item)
	}
	return out, nil
}

// ResolveSteerImageReferences 校验并规范化 steer 图片引用（触盘）。
// 只接受本会话 uploads 目录下的普通文件；客户端文件名与 MIME 均校验；
// 尺寸以磁盘为准；解码探测真实格式并校验声明一致性（防伪造 mime）。
// Python: steer_images.py:106-130 resolve_steer_image_references。
func ResolveSteerImageReferences(params map[string]any, sessionID string) ([]map[string]any, error) {
	references, err := ValidateSteerImageReferences(params)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("image steering requires a current session")
	}
	sessionsDir := sessionlifecycle.GetAgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, attachmentsafety.SafeSessionDirname(sessionID))
	uploadDir := filepath.Join(sessionDir, "uploads")
	if err := checkDirStat(sessionDir); err != nil {
		return nil, steerReadError(err)
	}
	if err := checkDirStat(uploadDir); err != nil {
		return nil, steerReadError(err)
	}
	images := make([]map[string]any, 0, len(references))
	for _, item := range references {
		resolved, err := resolveSteerImage(item, uploadDir)
		if err != nil {
			return nil, err
		}
		images = append(images, resolved)
	}
	// 目录在校验期间被替换（inode 变化）→ 回滚重传
	for _, dir := range []string{sessionDir, uploadDir} {
		if err := checkDirStat(dir); err != nil {
			return nil, errors.New("session uploads changed during validation; upload images again")
		}
	}
	return images, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSteerImage 校验单个引用并返回规范化记录。
// Python: steer_images.py:68-104 _resolve_image。
func resolveSteerImage(item map[string]any, uploadDir string) (map[string]any, error) {
	rawPath, _ := item["path"].(string)
	filename, _ := item["filename"].(string)
	declaredMime, _ := item["mime_type"].(string)

	// Python 先查原始路径 parts 再 resolve（:71）——先 Clean 会把 ".." 洗掉漏判
	if strings.Contains(rawPath, "..") {
		return nil, errors.New("steer image path is outside the current session uploads")
	}
	clean := filepath.Clean(rawPath)
	if !filepath.IsAbs(clean) ||
		filepath.Dir(clean) != filepath.Clean(uploadDir) {
		return nil, errors.New("steer image path is outside the current session uploads")
	}
	if filepath.Base(clean) != filename {
		return nil, errors.New("steer image filename does not match its uploaded path")
	}
	if filepath.Base(clean) != attachmentsafety.SanitizeUploadFilename(filepath.Base(clean), "image") {
		return nil, errors.New("steer image path is not an uploaded filename")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, steerReadError(err)
	}
	if isSymlink(info) {
		return nil, errors.New("steer image paths must not contain symlinks or junctions")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("steer image path must identify a regular file")
	}
	if info.Size() <= 0 || info.Size() > MaxSteerImageBytes {
		return nil, errors.New("steer image must be non-empty and at most 10 MiB")
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, steerReadError(err)
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !sameInode(info, opened) || !opened.Mode().IsRegular() {
		return nil, errors.New("steer image changed during validation; upload it again")
	}
	data := make([]byte, MaxSteerImageBytes+1)
	n, err := f.Read(data)
	if err != nil && n == 0 {
		return nil, steerReadError(err)
	}
	data = data[:n]
	if len(data) == 0 || len(data) > MaxSteerImageBytes {
		return nil, errors.New("steer image must be non-empty and at most 10 MiB")
	}
	// 解码探测真实格式与像素规模（替代 Python PIL：container verify + load 截断检测
	// 由 image.DecodeConfig/Decode 两段承担；解压炸弹用像素上限承担）
	mimeType, err := probeImageMime(data)
	if err != nil {
		return nil, err
	}
	suffix := strings.ToLower(filepath.Ext(clean))
	validSuffix := suffix == attachmentsafety.ImageSuffixForMime(mimeType) ||
		(mimeType == "image/jpeg" && (suffix == ".jpeg" || suffix == ".jfif"))
	if mimeType != declaredMime || !validSuffix {
		return nil, errors.New("steer image MIME type and filename must match its actual image format")
	}
	// 校验期间文件被替换（尺寸/inode/_mtime 变化）→ 回滚重传
	current, err := os.Lstat(clean)
	if err != nil || !sameInode(info, current) || current.Size() != int64(len(data)) ||
		!current.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("steer image changed during validation; upload it again")
	}
	return map[string]any{
		"type":       "image",
		"path":       clean,
		"filename":   filepath.Base(clean),
		"mime_type":  mimeType,
		"size_bytes": len(data),
	}, nil
}

// probeImageMime 解码探测真实 MIME 与像素规模。
// Python: steer_images.py:42-66 _image_mime_type（PIL verify+load 两段 →
// Go DecodeConfig 像素门 + Decode 完整解码两段；WEBP 经 x/image 注册）。
func probeImageMime(data []byte) (string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("steer image is invalid or exceeds the safe decoded size")
	}
	mimeType, ok := imageFormatMimeTypes[format]
	if !ok {
		return "", errors.New("unsupported steer image format")
	}
	if cfg.Width*cfg.Height > MaxSteerImagePixels {
		return "", fmt.Errorf("steer image exceeds %d pixels", MaxSteerImagePixels)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", errors.New("steer image is invalid or exceeds the safe decoded size")
	}
	return mimeType, nil
}

// checkDirStat 目录存在且为目录且非 symlink。
// Python: steer_images.py:30-40 _checked_stat(directory=True)。
func checkDirStat(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if isSymlink(fi) {
		return errors.New("steer image paths must not contain symlinks or junctions")
	}
	if !fi.IsDir() {
		return errors.New("session uploads path is not a directory")
	}
	return nil
}

// steerReadError 触盘类错误统一文案。
// Python: steer_images.py:124-126 OSError/RuntimeError → 统一 ValueError 文案。
func steerReadError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("steer image upload is missing or cannot be read; upload it again")
}

// sameInode 比较 dev/ino（防打开期间替换）；平台不支持时退化为路径存在性。
func sameInode(a, b os.FileInfo) bool {
	type inodePair struct{ dev, ino uint64 }
	pairOf := func(fi os.FileInfo) (inodePair, bool) {
		type sys interface{ Sys() any }
		if st, ok := fi.Sys().(*syscallStatT); ok {
			return inodePair{st.Dev, st.Ino}, true
		}
		return inodePair{}, false
	}
	pa, oka := pairOf(a)
	pb, okb := pairOf(b)
	if !oka || !okb {
		return true
	}
	return pa == pb
}
