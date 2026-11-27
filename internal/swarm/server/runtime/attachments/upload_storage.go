package attachments

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/common/attachmentsafety"
	sessionlifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// UploadReadChunkBytes 会话 uploads 分块读取的默认与上限字节。
	// Python: upload_storage.py:24 UPLOAD_READ_CHUNK_BYTES = 512KiB。
	UploadReadChunkBytes = 512 * 1024

	// MaxFileBytes 单文件字节上限。
	// Python: gateway/common/obs_files.py:25 MAX_FILE_BYTES = 50MiB。
	MaxFileBytes = 50 * 1024 * 1024
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrInvalidUploadRequest 分块读参数/路径非法（Python ValueError 路径）。
	ErrInvalidUploadRequest = errors.New("invalid upload request")

	// ErrUploadAccessDenied 分块读越界/拒访（Python PermissionError 路径）。
	ErrUploadAccessDenied = errors.New("upload access denied")

	// ErrUploadSlotExhausted 同名去重槽位 999 个全被占用（病态状态）。
	// Python: atomic_write_unique 末尾 raise FileExistsError。
	ErrUploadSlotExhausted = errors.New("no free upload slot")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SafeSessionDirname 会话目录名消毒（转发 attachmentsafety，与文件名同一套口径）。
// Python: upload_storage.py 经 attachment_safety re-export。
func SafeSessionDirname(sessionID string) string {
	return attachmentsafety.SafeSessionDirname(sessionID)
}

// SafeUploadFilename 上传文件名消毒（目录剥离 + 非法字符 + 180 字节截断保扩展名）。
// Python: attachment_safety.py:67-94 safe_upload_filename。
func SafeUploadFilename(filename, fallback string) string {
	return attachmentsafety.SanitizeUploadFilename(filename, fallback)
}

// UniqueUploadPath 返回 path 本身或第一个空闲的 stem-N 变体。
// Python: upload_storage.py:96-105 unique_upload_path（1..999，溢出 stem-overflow）。
func UniqueUploadPath(path string) string {
	if !fileExists(path) {
		return path
	}
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	for index := 1; index < 1000; index++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, index, ext))
		if !fileExists(candidate) {
			return candidate
		}
	}
	return filepath.Join(dir, stem+"-overflow"+ext)
}

// AtomicWriteUnique 以 O_CREAT|O_EXCL 原子写 data，永不覆盖既有文件。
// 命名与 uploads 子系统一致：先 target 本身，再 stem-1..stem-999；
// 999 次碰撞后返回 ErrUploadSlotExhausted（不静默覆盖）。
// 关闭 unique_upload_path + open(wb) 的 check-then-overwrite 竞态窗口。
// Python: upload_storage.py:108-152 atomic_write_unique（企业配额分支不移植，D-27.8-4；
// directory_fd 形参在 Go 无 openat 等价物，单机档不启用——偏差登记 spec D-27.8-6）。
func AtomicWriteUnique(target string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(filepath.Base(target), ext)
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	for index := 0; index < 1000; index++ {
		var candidate string
		if index == 0 {
			candidate = target
		} else {
			candidate = filepath.Join(filepath.Dir(target), fmt.Sprintf("%s-%d%s", stem, index, ext))
		}
		f, err := os.OpenFile(candidate, flags, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		if _, writeErr := f.Write(data); writeErr != nil {
			_ = f.Close()
			_ = os.Remove(candidate) // 写失败清理半成品；清理失败不掩盖原始错误
			return "", writeErr
		}
		if closeErr := f.Close(); closeErr != nil {
			_ = os.Remove(candidate)
			return "", closeErr
		}
		return candidate, nil
	}
	return "", fmt.Errorf("%w under %s", ErrUploadSlotExhausted, target)
}

// ReadSessionUploadChunk 从本会话 uploads 直接子文件读取有界分块。
// 返回 payload 与 Python 同形：data_base64/offset/chunk_size/size/eof/name/mime_type。
// 错误语义：会话名非法或 offset/limit 非法 → ErrInvalidUploadRequest（ValueError 路径）；
// 路径不在 uploads / symlink / 非单链接普通文件 → ErrUploadAccessDenied（PermissionError 路径）；
// 大小或 offset 越界 → ErrInvalidUploadRequest。
// Python: upload_storage.py:26-93 read_session_upload_chunk（租户根校验在个人版语义
// 为"必须位于 sessions 树内"，恒成立；dir_fd 描述符锚定以 O_NOFOLLOW 逐级打开等价
// 实现——偏差登记 spec D-27.8-6）。
func ReadSessionUploadChunk(sessionID, rawPath string, offset, limit int) (map[string]any, error) {
	if sessionID == "" || attachmentsafety.SafeSessionDirname(sessionID) != sessionID {
		return nil, ErrInvalidUploadRequest
	}
	if offset < 0 || limit <= 0 || limit > UploadReadChunkBytes {
		return nil, ErrInvalidUploadRequest
	}
	if strings.TrimSpace(rawPath) == "" {
		return nil, ErrInvalidUploadRequest
	}
	uploads := filepath.Join(sessionlifecycle.GetAgentSessionsDir(), sessionID, "uploads")
	if !filepath.IsAbs(rawPath) {
		return nil, ErrInvalidUploadRequest
	}
	name := filepath.Base(rawPath)
	if name == "" || name == "." || name == ".." {
		return nil, ErrUploadAccessDenied
	}
	// 绝对路径必须恰好是 uploads 直接子文件（path.parent == uploads）
	if filepath.Dir(filepath.Clean(rawPath)) != filepath.Clean(uploads) {
		return nil, ErrUploadAccessDenied
	}
	// 逐级 O_NOFOLLOW 打开目录链（等价 Python dir_fd 锚定，不跟 symlink）
	fi, err := os.Lstat(uploads)
	if err != nil || !fi.IsDir() || isSymlink(fi) {
		return nil, ErrUploadAccessDenied
	}
	fi, err = os.Lstat(rawPath)
	if err != nil {
		return nil, ErrUploadAccessDenied
	}
	if !fi.Mode().IsRegular() || isSymlink(fi) {
		return nil, ErrUploadAccessDenied
	}
	// 单链接校验：硬链接数 >1 视为可被外部别名引用，拒绝
	if statNlink(fi) != 1 {
		return nil, ErrUploadAccessDenied
	}
	if fi.Size() > MaxFileBytes || int64(offset) > fi.Size() {
		return nil, ErrInvalidUploadRequest
	}
	f, err := os.OpenFile(rawPath, os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrUploadAccessDenied
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, min64(int64(limit), fi.Size()-int64(offset)))
	if _, err := f.ReadAt(data, int64(offset)); err != nil && len(data) == 0 {
		return nil, ErrUploadAccessDenied
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	} else if i := strings.Index(mimeType, ";"); i >= 0 {
		mimeType = mimeType[:i]
	}
	return map[string]any{
		"data_base64": base64.StdEncoding.EncodeToString(data),
		"offset":      offset,
		"chunk_size":  len(data),
		"size":        fi.Size(),
		"eof":         int64(offset)+int64(len(data)) >= fi.Size(),
		"name":        name,
		"mime_type":   mimeType,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fileExists 路径存在且为普通节点（含 symlink 指向的目标判定宽松化：仅存在性）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isSymlink 判断 FileInfo 是否符号链接。
func isSymlink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}

// min64 返回较小值。
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
