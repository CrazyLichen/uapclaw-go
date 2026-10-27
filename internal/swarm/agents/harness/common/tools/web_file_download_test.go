package tools

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// newTestManagerWithSecret 测试辅助：重置单例并注入固定密钥 loader。
// 单例 + loader 注入保证确定性且零文件系统副作用（不触碰真实 ~/.uapclaw）。
func newTestManagerWithSecret(t *testing.T, secret string) *WebFileDownloadManager {
	t.Helper()
	ResetWebFileDownloadManager()
	origLoader := webFileDownloadSecretLoader
	webFileDownloadSecretLoader = func() string { return secret }
	t.Cleanup(func() { webFileDownloadSecretLoader = origLoader })
	return GetWebFileDownloadManager()
}

// isolateUserHomeForSecret 隔离用户主目录（密钥文件落点指向临时目录）。
// 项目既有模式：env 负责指向（UAPCLAW_HOME），SetUserHome("") 仅重置缓存——
// 直接 SetUserHome(tmp) 会被 Once 重执行时的 os.UserHomeDir() 覆盖回真实主目录。
func isolateUserHomeForSecret(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv(path.EnvHome, tmp)
	t.Setenv(path.EnvDataDir, "") // 防外部 UAPCLAW_DATA_DIR 干扰落点断言
	path.SetUserHome("")
	t.Cleanup(func() { path.SetUserHome("") })
	return tmp
}

// ──────────────────── 令牌生成/验签测试 ────────────────────

// TestWebFileDownloadTokenRoundTrip 令牌往返：生成→验证还原 payload。
// Python: generate_token / validate_token (web_file_download.py L88-134)
func TestWebFileDownloadTokenRoundTrip(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m.GenerateToken("/tmp/report.pdf", "sess-1", 600)
	if token == "" || !strings.Contains(token, ".") {
		t.Fatalf("token 格式错误: %q", token)
	}
	payload := m.ValidateToken(token)
	if payload == nil {
		t.Fatal("验签失败")
	}
	if payload["path"] != "/tmp/report.pdf" || payload["sid"] != "sess-1" {
		t.Errorf("payload 还原错误: %v", payload)
	}
	exp, _ := payload["exp"].(float64)
	if exp <= float64(time.Now().Unix()) {
		t.Errorf("exp 应在未来: %v", exp)
	}
}

// TestWebFileDownloadTokenTampered 篡改签名拒绝。
// Python: hmac.compare_digest 校验失败返回 None
func TestWebFileDownloadTokenTampered(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m.GenerateToken("/tmp/a.pdf", "", 600)
	parts := strings.SplitN(token, ".", 2)
	tampered := parts[0] + "." + strings.Repeat("0", len(parts[1]))
	if m.ValidateToken(tampered) != nil {
		t.Error("篡改签名应拒绝")
	}
}

// TestWebFileDownloadTokenExpired 过期拒绝。
// Python: payload.get("exp", 0) < time.time() → None
func TestWebFileDownloadTokenExpired(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m.GenerateToken("/tmp/a.pdf", "", -10) // 已过期
	if m.ValidateToken(token) != nil {
		t.Error("过期令牌应拒绝")
	}
}

// TestWebFileDownloadTokenWrongSecret 错密钥拒绝。
func TestWebFileDownloadTokenWrongSecret(t *testing.T) {
	m1 := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m1.GenerateToken("/tmp/a.pdf", "", 600)
	m2 := &WebFileDownloadManager{secret: "ffffffffffffffffffffffffffffffff"}
	if m2.ValidateToken(token) != nil {
		t.Error("错密钥应拒绝")
	}
}

// TestWebFileDownloadTokenMalformed 多段/空 token 拒绝。
// Python: token.split(".") 长度非 2 → None
func TestWebFileDownloadTokenMalformed(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	for _, tok := range []string{"", "abc", "a.b.c", "!!.-"} {
		if m.ValidateToken(tok) != nil {
			t.Errorf("畸形 token %q 应拒绝", tok)
		}
	}
}

// TestWebFileDownloadTokenChinesePath 中文路径不转义（ensure_ascii=False 对齐）。
func TestWebFileDownloadTokenChinesePath(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m.GenerateToken("/tmp/报告 最终版.pdf", "s", 600)
	payload := m.ValidateToken(token)
	if payload == nil || payload["path"] != "/tmp/报告 最终版.pdf" {
		t.Errorf("中文路径往返失败: %v", payload)
	}
}

// TestWebFileDownloadTokenPayloadKeyOrder 载荷键序对齐 Python dict 插入序（path,exp,sid）。
// Python: json.dumps 按 dict 插入序输出——Go 用结构体保序序列化保证字节级一致。
func TestWebFileDownloadTokenPayloadKeyOrder(t *testing.T) {
	m := newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	token := m.GenerateToken("/tmp/a.pdf", "s", 600)
	payloadB64 := strings.SplitN(token, ".", 2)[0]
	raw, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("b64 解码失败: %v", err)
	}
	if !strings.HasPrefix(string(raw), `{"path":"/tmp/a.pdf","exp":`) {
		t.Errorf("载荷键序应 path→exp→sid: %s", string(raw))
	}
	if !strings.HasSuffix(string(raw), `"sid":"s"}`) {
		t.Errorf("载荷应以 sid 结尾: %s", string(raw))
	}
}

// TestGenerateDownloadURL URL 前缀。
// Python: generate_download_url (web_file_download.py L136-138)
func TestGenerateDownloadURL(t *testing.T) {
	if got := GenerateDownloadURL("tok"); got != "/file-api/download?token=tok" {
		t.Errorf("download url: %q", got)
	}
}

// ──────────────────── 密钥三级来源测试 ────────────────────

// TestWebFileDownloadSecretFromEnv env 密钥优先（<32 拒绝后走文件/生成）。
// Python: _load_or_create_secret L42-43
func TestWebFileDownloadSecretFromEnv(t *testing.T) {
	isolateUserHomeForSecret(t)

	// 短 env 被拒绝 → 走文件（隔离目录内新建）→ 64 hex
	t.Setenv(WebFileDownloadSecretEnvKey, "short")
	ResetWebFileDownloadManager()
	m := GetWebFileDownloadManager()
	if m == nil || m.secret == "" {
		t.Fatal("manager 构造失败")
	}
	if len(m.secret) != 64 {
		t.Errorf("短 env 后应生成 64 hex 密钥: %q", m.secret)
	}

	// 合法 env（≥32）直接优先返回
	t.Setenv(WebFileDownloadSecretEnvKey, strings.Repeat("k", 40))
	ResetWebFileDownloadManager()
	m2 := GetWebFileDownloadManager()
	if m2.secret != strings.Repeat("k", 40) {
		t.Errorf("env 密钥应优先: %q", m2.secret)
	}
}

// TestWebFileDownloadSecretFilePersist 密钥文件持久化（跨实例复用）。
// Python: _load_or_create_secret L46-51 + L55-59
func TestWebFileDownloadSecretFilePersist(t *testing.T) {
	tmp := isolateUserHomeForSecret(t)
	t.Setenv(WebFileDownloadSecretEnvKey, "")

	ResetWebFileDownloadManager()
	m1 := GetWebFileDownloadManager()

	secretFile := filepath.Join(tmp, ".uapclaw", "config", webFileDownloadSecretFileName)
	if _, err := os.Stat(secretFile); err != nil {
		t.Fatalf("密钥文件应已创建: %v", err)
	}
	// 权限检查（非 Windows）
	if fi, _ := os.Stat(secretFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("权限应为 0600, 实际 %v", fi.Mode().Perm())
	}

	ResetWebFileDownloadManager()
	m2 := GetWebFileDownloadManager()
	if m1.secret != m2.secret {
		t.Error("重置后应从文件读到同一密钥")
	}
}

// TestWebFileDownloadSecretShortFileRejected 文件内密钥 <32 拒绝重新生成。
// Python: existing and len(existing) >= 32 才返回（L50-51）
func TestWebFileDownloadSecretShortFileRejected(t *testing.T) {
	tmp := isolateUserHomeForSecret(t)
	t.Setenv(WebFileDownloadSecretEnvKey, "")

	// 预置过短密钥文件
	shortFile := filepath.Join(tmp, ".uapclaw", "config", webFileDownloadSecretFileName)
	if err := os.MkdirAll(filepath.Dir(shortFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shortFile, []byte("too-short"), 0o600); err != nil {
		t.Fatal(err)
	}

	ResetWebFileDownloadManager()
	m := GetWebFileDownloadManager()
	if m.secret == "too-short" {
		t.Fatal("短密钥文件应被拒绝")
	}
	if len(m.secret) != 64 {
		t.Errorf("应重新生成 64 hex: %q", m.secret)
	}
	// 短密钥应被新密钥覆盖
	content, err := os.ReadFile(shortFile)
	if err != nil || string(content) != m.secret {
		t.Errorf("密钥文件应被覆盖为新密钥: %q err=%v", string(content), err)
	}
}

// TestWebFileDownloadSecretWriteFail 写失败回退内存密钥。
// Python: L60-61 except → warning + 返回内存密钥
func TestWebFileDownloadSecretWriteFail(t *testing.T) {
	// 主目录指向普通文件 → MkdirAll 因 ENOTDIR 失败（root 也无法绕过；
	// t.Setenv 拒绝 NUL 字节，不能用路径注入方式触发）
	blocker := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(path.EnvHome, blocker)
	t.Setenv(path.EnvDataDir, "")
	path.SetUserHome("")
	t.Cleanup(func() { path.SetUserHome("") })
	t.Setenv(WebFileDownloadSecretEnvKey, "")

	ResetWebFileDownloadManager()
	m := GetWebFileDownloadManager()
	if len(m.secret) != 64 {
		t.Errorf("回退内存密钥应为 64 hex 字符: %q", m.secret)
	}
}

// ──────────────────── build_file_download_info 测试 ────────────────────

// TestBuildFileDownloadInfo 全字段断言。
// Python: build_file_download_info (web_file_download.py L155-186)
func TestBuildFileDownloadInfo(t *testing.T) {
	// loader 注入固定密钥——单例路径确定性，无文件系统副作用
	newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")

	tmp := t.TempDir()
	fp := filepath.Join(tmp, "data.csv")
	if err := os.WriteFile(fp, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := BuildFileDownloadInfo(fp, "data.csv", "sess", 120)
	if info["name"] != "data.csv" {
		t.Errorf("name: %v", info["name"])
	}
	if info["size"] != int64(8) {
		t.Errorf("size: %v", info["size"])
	}
	if mt, _ := info["mime_type"].(string); !strings.HasPrefix(mt, "text/csv") {
		t.Errorf("mime_type: %v", info["mime_type"])
	}
	url, _ := info["download_url"].(string)
	if !strings.HasPrefix(url, "/file-api/download?token=") {
		t.Errorf("download_url: %v", url)
	}
	if info["expires_at"].(int64) <= time.Now().Unix() {
		t.Errorf("expires_at 应在未来: %v", info["expires_at"])
	}
}

// TestBuildFileDownloadInfo_文件不存在 size 兜底 0 不报错。
// Python: os.path.getsize 抛 OSError → pass → file_size=0
func TestBuildFileDownloadInfo_文件不存在(t *testing.T) {
	newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")

	info := BuildFileDownloadInfo("/nonexistent/x.bin", "x.bin", "sess", 60)
	if info["size"] != int64(0) {
		t.Errorf("size 应兜底 0: %v", info["size"])
	}
	if info["mime_type"] != "application/octet-stream" {
		t.Errorf("未知扩展名 mime 兜底: %v", info["mime_type"])
	}
}
