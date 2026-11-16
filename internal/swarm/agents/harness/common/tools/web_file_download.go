// Web 端文件下载令牌管理器（10.6.24b + 2026-11-16 全量对齐批）。
// 对齐 Python jiuwenswarm/common/file_download_token.py（410 行，10.6.24b 后上游迁移）：
// HMAC-SHA256 无状态令牌，密钥三级来源（env/共享文件/自动生成），
// AgentServer 与 Gateway 跨进程无需共享内存即可安全校验。
//
// 协议（Python docstring L8-14）：
// - 令牌格式: Base64URL(payload_json) + "." + Hex(HMAC-SHA256)
// - 普通下载 payload: path, sid；可选 exp（省略则不过期，send_file 交付产物）
// - 受管资产令牌额外绑定: kind=verified_asset_v1, asset_id, size, digest, name
// - 密钥来源: env 或自动生成并写入共享文件

package tools

import (
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
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	verifieddownloadassets "github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WebFileDownloadManager Web 端文件下载令牌管理器。
// 使用 HMAC-SHA256 签名保证令牌不可伪造，
// 密钥通过环境变量或共享密钥文件在 AgentServer / Gateway 间共享。
// Python: WebFileDownloadManager (file_download_token.py L85-101)
type WebFileDownloadManager struct {
	// secret HMAC 签名密钥（Python _secret）
	secret string
	// assetOwner 受管资产宿主（Python _asset_owner L101；nil → 验证时回退共享单例）
	assetOwner *verifieddownloadassets.VerifiedDownloadAssetOwner
}

// webFileDownloadPayload 普通下载令牌载荷（键序对齐 Python generate_token 插入序
// path→sid→exp→download_http_base，结构体序列化保证字节级一致）。
// exp/download_http_base 为可选（Python L162-171 条件写入）。
type webFileDownloadPayload struct {
	// Path 文件绝对路径
	Path string `json:"path"`
	// Sid 会话标识（传入 session_id 的验证方校验一致性）
	Sid string `json:"sid"`
	// Exp 过期 Unix 秒（0 = 不写入，持久交付；Python expires_in=None）
	Exp int64 `json:"exp,omitempty"`
	// DownloadHTTPBase AgentOS HTTP bridge 基址（Python agent_http_base_key="download_http_base"）
	DownloadHTTPBase string `json:"download_http_base,omitempty"`
}

// webFileVerifiedAssetPayload 受管资产令牌载荷（键序对齐 Python generate_verified_asset_token
// L128-137：kind→asset_id→path→exp→size→digest→name→sid）。
type webFileVerifiedAssetPayload struct {
	// Kind 令牌种类（Python _VERIFIED_ASSET_TOKEN_KIND）
	Kind string `json:"kind"`
	// AssetID 资产标识
	AssetID string `json:"asset_id"`
	// Path 密封副本路径
	Path string `json:"path"`
	// Exp 过期（秒，资产 TTL）
	Exp float64 `json:"exp"`
	// Size 字节数
	Size int64 `json:"size"`
	// Digest 内容摘要
	Digest string `json:"digest"`
	// Name 交付文件名
	Name string `json:"name"`
	// Sid 会话标识
	Sid string `json:"sid"`
}

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

	// WebFileDownloadHTTPBaseEnvKey 下载 HTTP bridge 基址 env
	// Python: _DOWNLOAD_HTTP_BASE_ENV_KEY（品牌映射）
	WebFileDownloadHTTPBaseEnvKey = "UAPCLAWSWARM_AGENT_DOWNLOAD_HTTP_BASE"

	// WebFileDownloadLegacyHTTPBaseEnvKey 旧 HTTP bridge 基址 env
	// Python: _LEGACY_HTTP_BASE_ENV_KEY（品牌映射）
	WebFileDownloadLegacyHTTPBaseEnvKey = "UAPCLAWSWARM_AGENT_HTTP_BASE"

	// VerifiedAssetTokenKind 受管资产令牌种类
	// Python: _VERIFIED_ASSET_TOKEN_KIND = "verified_asset_v1"
	VerifiedAssetTokenKind = "verified_asset_v1"
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
// Python: WebFileDownloadManager.get_instance() (file_download_token.py L103-107)
func GetWebFileDownloadManager() *WebFileDownloadManager {
	webFileDownloadManagerOnce.Do(func() {
		webFileDownloadManagerInst = &WebFileDownloadManager{
			secret: webFileDownloadSecretLoader(),
		}
	})
	return webFileDownloadManagerInst
}

// ResetWebFileDownloadManager 重置单例（仅测试用）。
// Python: WebFileDownloadManager.reset_instance() (file_download_token.py L109-111)
func ResetWebFileDownloadManager() {
	webFileDownloadManagerOnce = sync.Once{}
	webFileDownloadManagerInst = nil
}

// SetWebFileDownloadAssetOwner 注入受管资产宿主（Python __init__ asset_owner kwarg）。
func SetWebFileDownloadAssetOwner(owner *verifieddownloadassets.VerifiedDownloadAssetOwner) {
	GetWebFileDownloadManager().assetOwner = owner
}

// GenerateToken 生成下载令牌。
// 格式: Base64URL(payload_json) + "." + Hex(HMAC-SHA256)；
// payload 为 {path, sid[, exp][, download_http_base]}——expiresIn=nil 不过期
// （Python expires_in=None，send_file 交付产物）。
// Python: generate_token (file_download_token.py L149-171)
func (m *WebFileDownloadManager) GenerateToken(filePath, sessionID string, expiresIn *int) string {
	payload := webFileDownloadPayload{
		Path: filePath,
		Sid:  sessionID,
	}
	// Python L162-164: expires_in=None 不写入 exp，校验侧视为不过期
	if expiresIn != nil {
		payload.Exp = time.Now().Unix() + int64(*expiresIn)
	}
	// Python L165-170: AgentOS HTTP bridge 基址随 token 签发（env 读取在包级便捷函数）
	// ——本方法不读 env，由 GenerateFileDownloadToken 传入
	return m.signPayloadStruct(payload)
}

// GenerateTokenWithBase 带 HTTP bridge 基址的令牌生成（Python generate_token
// agent_http_base/agent_http_base_key 参数）。
func (m *WebFileDownloadManager) GenerateTokenWithBase(filePath, sessionID string, expiresIn *int, agentHTTPBase, agentHTTPBaseKey string) string {
	payload := webFileDownloadPayload{
		Path: filePath,
		Sid:  sessionID,
	}
	if expiresIn != nil {
		payload.Exp = time.Now().Unix() + int64(*expiresIn)
	}
	base := strings.TrimSpace(agentHTTPBase)
	if base != "" && agentHTTPBaseKey == "download_http_base" {
		payload.DownloadHTTPBase = strings.TrimSuffix(base, "/")
	}
	return m.signPayloadStruct(payload)
}

// GenerateVerifiedAssetToken 生成绑定受管资产注册的令牌。
// Python: generate_verified_asset_token (file_download_token.py L119-147)
// enterprise tenant_key 分支不移植（偏差 D-x1）。
func (m *WebFileDownloadManager) GenerateVerifiedAssetToken(
	asset *verifieddownloadassets.VerifiedDownloadAsset,
	fileName string,
	sessionID string,
) string {
	payload := webFileVerifiedAssetPayload{
		Kind:    VerifiedAssetTokenKind,
		AssetID: asset.AssetID,
		Path:    toPosixPath(asset.SealedPath),
		Exp:     asset.ExpiresAt,
		Size:    asset.SizeBytes,
		Digest:  asset.ContentDigest,
		Name:    filepath.Base(fileName),
		Sid:     sessionID,
	}
	return m.signPayloadStruct(payload)
}

// ValidateToken 验证令牌，返回 payload；验签失败/过期/畸形返回 nil。
// Python: validate_token (file_download_token.py L189-272)
// sessionID 非空时校验 payload.sid 一致（Python session_id 参数）；
// checkExpiry=false 跳过 exp 检查（交付产物路由语义，Python check_expiry 参数）。
func (m *WebFileDownloadManager) ValidateToken(token string, sessionID string, checkExpiry bool) map[string]any {
	// ① 拆分 + 验签（Python L192-205）
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil
	}
	payloadB64, signature := parts[0], parts[1]
	expectedSig := m.sign(payloadB64)
	// Python: hmac.compare_digest → Go hmac.Equal（常量时间比较）
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		logger.Warn(logComponent).Msg("[WebFileDownload] 令牌签名校验失败")
		return nil
	}
	// ② 载荷解析（Python L206-210）
	raw, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("[WebFileDownload] token base64 decode failed")
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Debug(logComponent).Err(err).Msg("[WebFileDownload] token payload parse failed")
		return nil
	}
	// ③ 过期校验：exp 存在则严格校验；不存在则要求 legacy 白名单 schema（Python L211-224）
	if checkExpiry {
		if expVal, hasExp := payload["exp"]; hasExp {
			exp, ok := expVal.(float64)
			if !ok || int64(exp) < time.Now().Unix() {
				logger.Info(logComponent).Str("path", fmt.Sprint(payload["path"])).
					Msg("[WebFileDownload] 令牌已过期")
				return nil
			}
		} else if !isLegacyNoExpirationPayload(payload) {
			return nil
		}
	}
	// ④ sid 一致性校验（传入 session_id 时，Python L225-229）
	if strings.TrimSpace(sessionID) != "" {
		if fmt.Sprint(payload["sid"]) != strings.TrimSpace(sessionID) {
			return nil
		}
	}
	// ⑤ kind 门卫（Python L230-233）
	kind, _ := payload["kind"].(string)
	if kind != "" && kind != VerifiedAssetTokenKind {
		return nil
	}
	// ⑥ 受管资产令牌 → is_active 校验（Python L234-267）
	if kind == VerifiedAssetTokenKind {
		exp, _ := payload["exp"].(float64)
		size, _ := payload["size"].(float64)
		owner := m.assetOwner
		if owner == nil {
			// Python L236-240: fallback get_verified_download_asset_owner()
			owner = verifieddownloadassets.GetVerifiedDownloadAssetOwner()
		}
		if !owner.IsActive(verifieddownloadassets.IsActiveClaim{
			AssetID:       fmt.Sprint(payload["asset_id"]),
			SealedPath:    fmt.Sprint(payload["path"]),
			ExpiresAt:     exp,
			SizeBytes:     int64(size),
			ContentDigest: fmt.Sprint(payload["digest"]),
		}) {
			return nil
		}
	}
	return payload
}

// GenerateDownloadURL 构造下载 URL（token 必带；userID 非空追加 query）。
// Python: generate_download_url (file_download_token.py L279-285)
func GenerateDownloadURL(token string, userID string) string {
	url := "/file-api/download?token=" + token
	if normalized := strings.TrimSpace(userID); normalized != "" {
		url += "&user_id=" + normalized
	}
	return url
}

// GenerateFileDownloadToken 包级便捷函数（单例路径，读 HTTP bridge env）。
// Python: generate_file_download_token (file_download_token.py L254-269)
func GenerateFileDownloadToken(filePath, sessionID string, expiresIn *int) string {
	return GetWebFileDownloadManager().GenerateTokenWithBase(
		filePath, sessionID, expiresIn,
		firstNonEmptyEnv(WebFileDownloadHTTPBaseEnvKey, WebFileDownloadLegacyHTTPBaseEnvKey),
		"download_http_base",
	)
}

// ValidateFileDownloadToken 包级便捷函数（单例路径）。
// Python: validate_file_download_token (file_download_token.py L300-303)
func ValidateFileDownloadToken(token string, sessionID string, checkExpiry bool) map[string]any {
	return GetWebFileDownloadManager().ValidateToken(token, sessionID, checkExpiry)
}

// BuildFileDownloadInfo 构造单文件下载信息
// （name/size/mime_type/download_url/download_token——新版无 expires_at）。
// 默认签发不过期令牌（expiresIn=nil），与 send_file_to_user 产物语义一致。
// Python: build_file_download_info (file_download_token.py L326-358)
func BuildFileDownloadInfo(filePath, fileName, sessionID string, expiresIn *int, userID string) map[string]any {
	token := GenerateFileDownloadToken(filePath, sessionID, expiresIn)
	downloadURL := GenerateDownloadURL(token, userID)

	// Python: os.path.getsize 抛 OSError → pass → file_size=0
	fileSize := int64(0)
	if fi, err := os.Stat(filePath); err == nil {
		fileSize = fi.Size()
	}

	// Python: mimetypes.guess_type(file_name)，未识别回退 octet-stream
	mimeType := guessMimeType(fileName)

	return map[string]any{
		"name":           fileName,
		"size":           fileSize,
		"mime_type":      mimeType,
		"download_url":   downloadURL,
		"download_token": token,
	}
}

// BuildVerifiedAssetDownloadInfo 构造以 staged 资产背书的下载信息。
// Python: build_verified_asset_download_info (file_download_token.py L362-390)
func BuildVerifiedAssetDownloadInfo(
	asset *verifieddownloadassets.VerifiedDownloadAsset,
	fileName string,
	sessionID string,
	userID string,
) map[string]any {
	m := GetWebFileDownloadManager()
	token := m.GenerateVerifiedAssetToken(asset, fileName, sessionID)
	return map[string]any{
		"name":           filepath.Base(fileName),
		"size":           asset.SizeBytes,
		"mime_type":      guessMimeType(fileName),
		"download_url":   GenerateDownloadURL(token, userID),
		"download_token": token,
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

// signPayloadStruct 结构体保序序列化 + 签名（Python _sign_payload L113-117）。
func (m *WebFileDownloadManager) signPayloadStruct(payload any) string {
	// Python: json.dumps(payload, separators=(",", ":"), ensure_ascii=False)
	// ——紧凑分隔符+不转义非 ASCII；utils.MarshalNoEscape 同步关闭 <>& 转义
	payloadJSON, err := utils.MarshalNoEscape(payload)
	if err != nil {
		// 结构体序列化结构性不可能失败，防御性返回空串
		return ""
	}
	payloadB64 := base64.URLEncoding.EncodeToString(payloadJSON)
	sig := m.sign(payloadB64)
	return payloadB64 + "." + sig
}

// signTokenPayload 任意 map 载荷签名（测试注入非法 kind/白名单外 schema 用）。
func (m *WebFileDownloadManager) signTokenPayload(payload map[string]any) string {
	payloadJSON, err := utils.MarshalNoEscape(payload)
	if err != nil {
		return ""
	}
	payloadB64 := base64.URLEncoding.EncodeToString(payloadJSON)
	return payloadB64 + "." + m.sign(payloadB64)
}

// isLegacyNoExpirationPayload 无 exp 令牌白名单：
// 恰为 {path,sid} 或 {path,sid,download_http_base} 且值全为非空字符串。
// Python: _is_legacy_no_expiration_payload (file_download_token.py L288-296)
func isLegacyNoExpirationPayload(payload map[string]any) bool {
	keys := make(map[string]struct{}, len(payload))
	for k := range payload {
		keys[k] = struct{}{}
	}
	isLegacy := len(keys) == 2 && hasKeys(keys, "path", "sid")
	isRouted := len(keys) == 3 && hasKeys(keys, "path", "sid", "download_http_base")
	if !isLegacy && !isRouted {
		return false
	}
	for _, key := range []string{"path", "sid", "download_http_base"} {
		if v, ok := payload[key]; ok {
			s, isStr := v.(string)
			if !isStr || s == "" {
				return false
			}
		}
	}
	return true
}

// hasKeys keys 集合恰含给定成员。
func hasKeys(keys map[string]struct{}, want ...string) bool {
	if len(keys) != len(want) {
		return false
	}
	for _, w := range want {
		if _, ok := keys[w]; !ok {
			return false
		}
	}
	return true
}

// guessMimeType 扩展名猜测，未识别回退 octet-stream（Python mimetypes.guess_type）。
func guessMimeType(fileName string) string {
	if guessed := mime.TypeByExtension(filepath.Ext(fileName)); guessed != "" {
		return guessed
	}
	return "application/octet-stream"
}

// firstNonEmptyEnv 返回首个非空环境变量值（Python L264-265 双 env 回退）。
func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// toPosixPath 路径转 posix 斜杠（Python as_posix）。
func toPosixPath(p string) string {
	return filepath.ToSlash(p)
}

// getWebFileDownloadSecretFilePath 密钥文件路径。
// Python: _get_secret_file_path (file_download_token.py L53-57)
// env 映射：JIUWENSWARM_WORKSPACE → Go path.WorkspaceDir()（UAPCLAW_DATA_DIR → ~/.uapclaw/）
func getWebFileDownloadSecretFilePath() string {
	return filepath.Join(path.WorkspaceDir(), "config", webFileDownloadSecretFileName)
}

// loadOrCreateWebFileDownloadSecret 三级密钥来源：
// 1. 环境变量（≥32 字符）2. 共享密钥文件（≥32）3. 生成 64 hex 写文件 0600（失败回退内存）。
// Python: _load_or_create_secret (file_download_token.py L60-82)
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
