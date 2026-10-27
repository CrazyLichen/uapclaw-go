// Web 端文件下载令牌管理器（10.6.24b）。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/web_file_download.py（187 行）：
// HMAC-SHA256 无状态令牌，密钥三级来源（env/共享文件/自动生成），
// AgentServer 与 Gateway 跨进程无需共享内存即可安全校验。

package tools

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WebFileDownloadManager Web 端文件下载令牌管理器。
// 使用 HMAC-SHA256 签名保证令牌不可伪造，
// 密钥通过环境变量或共享密钥文件在 AgentServer / Gateway 间共享。
// Python: WebFileDownloadManager (web_file_download.py L66-71)
type WebFileDownloadManager struct {
	// secret HMAC 签名密钥
	secret string
}

// webFileDownloadPayload 令牌载荷（键序对齐 Python dict 插入序 path→exp→sid，
// 结构体序列化保证字节级一致，跨语言可互验）。
type webFileDownloadPayload struct {
	// Path 文件绝对路径
	Path string `json:"path"`
	// Exp 过期 Unix 秒
	Exp int64 `json:"exp"`
	// Sid 会话标识（仅溯源，验证时不校验）
	Sid string `json:"sid"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// webFileDownloadDefaultExpiresSeconds 默认过期秒数
	// Python: _DEFAULT_EXPIRES_SECONDS = 600
	webFileDownloadDefaultExpiresSeconds = 600

	// WebFileDownloadSecretEnvKey 密钥环境变量（品牌映射 JIUWENSWARM_→UAPCLAWSWARM_）
	// Python: _SECRET_ENV_KEY = "JIUWENSWARM_FILE_DOWNLOAD_SECRET"
	WebFileDownloadSecretEnvKey = "UAPCLAWSWARM_FILE_DOWNLOAD_SECRET"

	// webFileDownloadSecretFileName 密钥文件名
	// Python: _SECRET_FILE_NAME = ".file_download_secret"
	webFileDownloadSecretFileName = ".file_download_secret"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// webFileDownloadManagerOnce 单例懒加载
	webFileDownloadManagerOnce sync.Once

	// webFileDownloadManagerInst 单例实例
	webFileDownloadManagerInst *WebFileDownloadManager

	// webFileDownloadSecretLoader 密钥加载函数（测试注入点，默认三级来源）
	webFileDownloadSecretLoader = loadOrCreateWebFileDownloadSecret
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetWebFileDownloadManager 返回单例（首次调用加载/生成密钥）。
// Python: WebFileDownloadManager.get_instance() (web_file_download.py L78-82)
func GetWebFileDownloadManager() *WebFileDownloadManager {
	webFileDownloadManagerOnce.Do(func() {
		webFileDownloadManagerInst = &WebFileDownloadManager{
			secret: webFileDownloadSecretLoader(),
		}
	})
	return webFileDownloadManagerInst
}

// ResetWebFileDownloadManager 重置单例（仅测试用）。
// Python: WebFileDownloadManager.reset_instance() (web_file_download.py L84-86)
func ResetWebFileDownloadManager() {
	webFileDownloadManagerOnce = sync.Once{}
	webFileDownloadManagerInst = nil
}

// GenerateToken 生成下载令牌。
// 格式: Base64URL(payload_json) + "." + Hex(HMAC-SHA256)；
// payload 为 {path, exp, sid}——sid 仅溯源，验证时不校验。
// Python: generate_token (web_file_download.py L88-108)
func (m *WebFileDownloadManager) GenerateToken(filePath, sessionID string, expiresIn int) string {
	payload := webFileDownloadPayload{
		Path: filePath,
		Exp:  time.Now().Unix() + int64(expiresIn),
		Sid:  sessionID,
	}
	// Python: json.dumps(payload, separators=(",", ":"), ensure_ascii=False)
	// ——紧凑分隔符+不转义非 ASCII；Go 用 SetEscapeHTML(false) 同步关闭 <>& 转义
	payloadJSON, err := marshalCompactNoEscape(payload)
	if err != nil {
		// 结构体序列化结构性不可能失败，防御性返回空串
		return ""
	}
	payloadB64 := base64.URLEncoding.EncodeToString(payloadJSON)
	sig := m.sign(payloadB64)
	return payloadB64 + "." + sig
}

// ValidateToken 验证令牌，返回 payload；验签失败/过期/畸形返回 nil。
// Python: validate_token (web_file_download.py L110-134)
func (m *WebFileDownloadManager) ValidateToken(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil
	}
	payloadB64, signature := parts[0], parts[1]
	expectedSig := m.sign(payloadB64)
	// Python: hmac.compare_digest → Go hmac.Equal（常量时间比较）
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		logger.Warn(logComponent).Msg("[WebFileDownload] token signature mismatch")
		return nil
	}
	raw, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("[WebFileDownload] token base64 decode failed")
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		// 覆盖 Python isinstance(payload, dict) 非 dict 分支：
		// Go 非对象 JSON 无法 Unmarshal 进 map → 此处拒绝
		logger.Debug(logComponent).Err(err).Msg("[WebFileDownload] token payload parse failed")
		return nil
	}
	// Python: payload.get("exp", 0) < time.time()（缺省 0 → 必过期）
	if exp, _ := payload["exp"].(float64); exp < float64(time.Now().Unix()) {
		logger.Info(logComponent).Str("path", fmt.Sprint(payload["path"])).
			Msg("[WebFileDownload] token expired")
		return nil
	}
	return payload
}

// GenerateDownloadURL 构造下载 URL。
// Python: generate_download_url (web_file_download.py L136-138)
func GenerateDownloadURL(token string) string {
	return "/file-api/download?token=" + token
}

// GenerateFileDownloadToken 包级便捷函数（单例路径）。
// Python: generate_file_download_token (web_file_download.py L141-148)
func GenerateFileDownloadToken(filePath, sessionID string, expiresIn int) string {
	return GetWebFileDownloadManager().GenerateToken(filePath, sessionID, expiresIn)
}

// ValidateFileDownloadToken 包级便捷函数（单例路径）。
// Python: validate_file_download_token (web_file_download.py L151-152)
func ValidateFileDownloadToken(token string) map[string]any {
	return GetWebFileDownloadManager().ValidateToken(token)
}

// BuildFileDownloadInfo 构造单文件下载信息
// （name/size/mime_type/download_url/download_token/expires_at）。
// Python: build_file_download_info (web_file_download.py L155-186)
func BuildFileDownloadInfo(filePath, fileName, sessionID string, expiresIn int) map[string]any {
	token := GenerateFileDownloadToken(filePath, sessionID, expiresIn)
	downloadURL := GenerateDownloadURL(token)

	// Python: os.path.getsize 抛 OSError → pass → file_size=0
	fileSize := int64(0)
	if fi, err := os.Stat(filePath); err == nil {
		fileSize = fi.Size()
	}

	// Python: mimetypes.guess_type(file_name)，未识别回退 octet-stream
	mimeType := "application/octet-stream"
	if guessed := mime.TypeByExtension(filepath.Ext(fileName)); guessed != "" {
		mimeType = guessed
	}

	return map[string]any{
		"name":           fileName,
		"size":           fileSize,
		"mime_type":      mimeType,
		"download_url":   downloadURL,
		"download_token": token,
		"expires_at":     time.Now().Unix() + int64(expiresIn),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sign 对 payloadB64 计算 HMAC-SHA256 hex 签名。
// Python: hmac.new(secret, payload_b64, sha256).hexdigest()
func (m *WebFileDownloadManager) sign(payloadB64 string) string {
	mac := hmac.New(sha256.New, []byte(m.secret))
	mac.Write([]byte(payloadB64))
	return hex.EncodeToString(mac.Sum(nil))
}

// marshalCompactNoEscape 紧凑 JSON 序列化，不转义 HTML 字符
// （对齐 Python separators=(",", ":") + ensure_ascii=False 字节输出）。
func marshalCompactNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder 追加换行符，去掉以对齐 json.dumps
	b := buf.Bytes()
	return b[:len(b)-1], nil
}

// getWebFileDownloadSecretFilePath 密钥文件路径。
// Python: _get_secret_file_path (web_file_download.py L34-38)
// env 映射：JIUWENSWARM_WORKSPACE → Go path.WorkspaceDir()（UAPCLAW_DATA_DIR → ~/.uapclaw/）
func getWebFileDownloadSecretFilePath() string {
	return filepath.Join(path.WorkspaceDir(), "config", webFileDownloadSecretFileName)
}

// loadOrCreateWebFileDownloadSecret 三级密钥来源：
// 1. 环境变量（≥32 字符）2. 共享密钥文件（≥32）3. 生成 64 hex 写文件 0600（失败回退内存）。
// Python: _load_or_create_secret (web_file_download.py L41-63)
func loadOrCreateWebFileDownloadSecret() string {
	if secret := os.Getenv(WebFileDownloadSecretEnvKey); len(secret) >= 32 {
		return secret
	}

	secretFile := getWebFileDownloadSecretFilePath()
	if existing, err := os.ReadFile(secretFile); err == nil {
		trimmed := strings.TrimSpace(string(existing))
		if len(trimmed) >= 32 {
			return trimmed
		}
	} else {
		logger.Debug(logComponent).Err(err).
			Msg("[WebFileDownload] read secret file failed, will regenerate")
	}

	// 生成 64 字符 hex（对齐 secrets.token_hex(32)）
	newSecretBytes := make([]byte, 32)
	if _, err := rand.Read(newSecretBytes); err != nil {
		// crypto/rand 失败属系统性异常，回退零值定长 hex（防御性，实际不可达）
		logger.Warn(logComponent).Err(err).
			Msg("[WebFileDownload] generate secret failed, using zero fallback")
		return hex.EncodeToString(newSecretBytes)
	}
	newSecret := hex.EncodeToString(newSecretBytes)
	if err := os.MkdirAll(filepath.Dir(secretFile), 0o755); err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("[WebFileDownload] write secret file failed, using memory secret (lost on restart)")
		return newSecret
	}
	if err := os.WriteFile(secretFile, []byte(newSecret), 0o600); err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("[WebFileDownload] write secret file failed, using memory secret (lost on restart)")
	}
	return newSecret
}
