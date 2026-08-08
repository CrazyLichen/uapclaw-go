package multimodal

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeVideoURL 规范化视频路径为 URL 或 data:URI。
//
// 对齐 Python: _normalize_video_url(video_path)
// HTTP URL → 保持原样；本地文件 → base64 → data:URI
func NormalizeVideoURL(videoPath string) (string, error) {
	value := strings.TrimSpace(videoPath)
	if value == "" {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalVideoConfigInvalid,
			exception.WithMsg("video_path cannot be empty"),
		)
	}

	// HTTP URL → 直接返回（对齐 Python: if value.startswith(("http://", "https://"))）
	if isHTTPURL(value) {
		return value, nil
	}

	// 本地文件 → base64 编码 → data:URI（对齐 Python: base64.b64encode → data URI）
	if _, err := os.Stat(value); err != nil {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalVideoInvokeFailed,
			exception.WithMsg(fmt.Sprintf("video file does not exist: %s", value)),
		)
	}

	data, err := os.ReadFile(value)
	if err != nil {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalVideoInvokeFailed,
			exception.WithMsg(fmt.Sprintf("读取视频文件失败: %s", err.Error())),
		)
	}

	mimeType := guessVideoMIMEType(value, data)
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// guessVideoMIMEType 推断视频 MIME 类型
// 优先使用 http.DetectContentType 内容检测，降级到扩展名推断
func guessVideoMIMEType(filePath string, data []byte) string {
	// 先尝试从内容检测
	if len(data) >= 512 {
		detected := http.DetectContentType(data[:512])
		if strings.HasPrefix(detected, "video/") {
			return detected
		}
	}

	// 降级到扩展名推断
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/avi"
	case ".mov":
		return "video/quicktime"
	case ".mkv":
		return "video/x-matroska"
	case ".flv":
		return "video/x-flv"
	case ".wmv":
		return "video/x-ms-wmv"
	default:
		return "video/mp4"
	}
}

// clampInt 范围裁剪整数（对齐 Python: max(min, min(val, max)))
func clampInt(val, min, max, defaultVal int) int {
	if val == 0 {
		return defaultVal
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// clampFloat 范围裁剪浮点数（对齐 Python: max(min, min(val, max)))
func clampFloat(val, min, max, defaultVal float64) float64 {
	if val == 0 {
		return defaultVal
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
