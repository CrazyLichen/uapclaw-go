package wsorigin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// testEnvPrefix 测试用环境变量前缀，避免与真实环境变量冲突
const (
	testEnableEnv  = "TEST_WS_ORIGIN_ENABLE"
	testHostsEnv   = "TEST_WS_ORIGIN_HOSTS"
)

// helper: 设置环境变量并在测试结束后恢复
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	orig := os.Getenv(key)
	os.Setenv(key, value)
	t.Cleanup(func() { os.Setenv(key, orig) })
}

// helper: 清除环境变量并在测试结束后恢复
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	orig := os.Getenv(key)
	os.Unsetenv(key)
	t.Cleanup(func() { os.Setenv(key, orig) })
}

// helper: 创建测试用校验器
func newTestChecker() *OriginChecker {
	return NewOriginCheckerWithEnv(testEnableEnv, testHostsEnv)
}

// ─── OriginChecker.IsEnabled 测试 ───

func TestOriginChecker_IsEnabled_值为1(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	checker := newTestChecker()
	if !checker.IsEnabled() {
		t.Error("环境变量为 '1' 时应启用校验")
	}
}

func TestOriginChecker_IsEnabled_未设置(t *testing.T) {
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()
	if checker.IsEnabled() {
		t.Error("环境变量未设置时不应启用校验")
	}
}

func TestOriginChecker_IsEnabled_值为0(t *testing.T) {
	setEnv(t, testEnableEnv, "0")
	checker := newTestChecker()
	if checker.IsEnabled() {
		t.Error("环境变量为 '0' 时不应启用校验")
	}
}

func TestOriginChecker_IsEnabled_值为其他(t *testing.T) {
	setEnv(t, testEnableEnv, "yes")
	checker := newTestChecker()
	if checker.IsEnabled() {
		t.Error("环境变量为 'yes' 时不应启用校验")
	}
}

func TestOriginChecker_IsEnabled_值含空格(t *testing.T) {
	setEnv(t, testEnableEnv, " 1 ")
	checker := newTestChecker()
	if !checker.IsEnabled() {
		t.Error("环境变量为 ' 1 '（含前后空格）时应启用校验")
	}
}

// ─── OriginChecker.GetAllowedHosts 测试 ───

func TestOriginChecker_GetAllowedHosts_未设置(t *testing.T) {
	unsetEnv(t, testHostsEnv)
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()
	if len(hosts) != 0 {
		t.Errorf("环境变量未设置时应返回空集合，实际 %d 项", len(hosts))
	}
}

func TestOriginChecker_GetAllowedHosts_逗号分隔(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com,localhost,test.org")
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()

	expected := map[string]bool{
		"example.com": true,
		"localhost":   true,
		"test.org":    true,
	}
	if len(hosts) != len(expected) {
		t.Errorf("期望 %d 项，实际 %d 项", len(expected), len(hosts))
	}
	for k := range expected {
		if !hosts[k] {
			t.Errorf("缺少主机名 %q", k)
		}
	}
}

func TestOriginChecker_GetAllowedHosts_含空格(t *testing.T) {
	setEnv(t, testHostsEnv, " example.com , localhost , ")
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()

	if !hosts["example.com"] {
		t.Error("应包含 'example.com'")
	}
	if !hosts["localhost"] {
		t.Error("应包含 'localhost'")
	}
	if len(hosts) != 2 {
		t.Errorf("期望 2 项（空项被忽略），实际 %d 项", len(hosts))
	}
}

func TestOriginChecker_GetAllowedHosts_大小写转小写(t *testing.T) {
	setEnv(t, testHostsEnv, "Example.COM,LOCALHOST")
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()

	if !hosts["example.com"] {
		t.Error("大写主机名应被转为小写 'example.com'")
	}
	if !hosts["localhost"] {
		t.Error("大写主机名应被转为小写 'localhost'")
	}
}

func TestOriginChecker_GetAllowedHosts_含none关键词(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com,none")
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()

	if !hosts["none"] {
		t.Error("应包含 'none' 关键词")
	}
}

func TestOriginChecker_GetAllowedHosts_仅逗号(t *testing.T) {
	setEnv(t, testHostsEnv, ",,,")
	checker := newTestChecker()
	hosts := checker.GetAllowedHosts()
	if len(hosts) != 0 {
		t.Errorf("仅逗号应返回空集合，实际 %d 项", len(hosts))
	}
}

// ─── OriginChecker.IsAllowed 测试 ───

func TestOriginChecker_IsAllowed_空Origin且白名单无none(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()
	if checker.IsAllowed("") {
		t.Error("Origin 为空且白名单无 'none' 时应拒绝")
	}
}

func TestOriginChecker_IsAllowed_空Origin且白名单有none(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com,none")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()
	if !checker.IsAllowed("") {
		t.Error("Origin 为空且白名单含 'none' 时应放行")
	}
}

func TestOriginChecker_IsAllowed_hostname匹配(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com,localhost")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"https匹配", "https://example.com", true},
		{"http匹配", "http://example.com", true},
		{"带端口匹配", "https://example.com:8080", true},
		{"localhost匹配", "http://localhost:3000", true},
		{"不匹配", "https://evil.com", false},
		{"子域名不匹配", "https://sub.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checker.IsAllowed(tt.origin)
			if got != tt.want {
				t.Errorf("IsAllowed(%q) = %v，期望 %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestOriginChecker_IsAllowed_大小写不敏感(t *testing.T) {
	setEnv(t, testHostsEnv, "Example.COM")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()

	if !checker.IsAllowed("https://example.com") {
		t.Error("白名单大小写不敏感，'Example.COM' 应匹配 'example.com'")
	}
	if !checker.IsAllowed("https://EXAMPLE.COM") {
		t.Error("白名单大小写不敏感，'Example.COM' 应匹配 'EXAMPLE.COM'")
	}
}

func TestOriginChecker_IsAllowed_URL解析失败(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()

	// url.Parse 对大多数字符串不会返回错误，
	// 但可以构造 scheme 缺少冒号的特殊 case
	// 实际测试中使用不含 :// 的值，hostname 应为空
	got := checker.IsAllowed("not-a-valid-origin")
	// url.Parse("not-a-valid-origin") 的 hostname 为空 → 应拒绝
	if got {
		t.Error("无法解析出 hostname 的 Origin 应被拒绝")
	}
}

func TestOriginChecker_IsAllowed_仅scheme(t *testing.T) {
	setEnv(t, testHostsEnv, "example.com")
	unsetEnv(t, testEnableEnv)
	checker := newTestChecker()

	// "https://" 解析后 hostname 为空
	got := checker.IsAllowed("https://")
	if got {
		t.Error("hostname 为空的 Origin 应被拒绝")
	}
}

// ─── OriginChecker.ForbiddenResponse 测试 ───

func TestOriginChecker_ForbiddenResponse(t *testing.T) {
	checker := newTestChecker()
	code, headers, body := checker.ForbiddenResponse()

	if code != 403 {
		t.Errorf("状态码应为 403，实际 %d", code)
	}

	ct := headers["Content-Type"]
	if len(ct) == 0 || ct[0] != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type 应为 'text/plain; charset=utf-8'，实际 %v", ct)
	}

	if body != "Forbidden: Origin not allowed\n" {
		t.Errorf("响应体不匹配: %q", body)
	}
}

// ─── GorillaCheckOrigin 测试 ───

func TestGorillaCheckOrigin_未启用时放行(t *testing.T) {
	unsetEnv(t, testEnableEnv)
	unsetEnv(t, testHostsEnv)

	checkFn := GorillaCheckOriginWithChecker(newTestChecker())

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Origin", "https://evil.com")

	if !checkFn(r) {
		t.Error("未启用校验时应放行所有请求")
	}
}

func TestGorillaCheckOrigin_启用且匹配(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	checkFn := GorillaCheckOriginWithChecker(newTestChecker())

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Origin", "https://example.com")

	if !checkFn(r) {
		t.Error("Origin 在白名单中时应放行")
	}
}

func TestGorillaCheckOrigin_启用且不匹配(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	checkFn := GorillaCheckOriginWithChecker(newTestChecker())

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Origin", "https://evil.com")

	if checkFn(r) {
		t.Error("Origin 不在白名单中时应拒绝")
	}
}

func TestGorillaCheckOrigin_启用且无Origin头(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	checkFn := GorillaCheckOriginWithChecker(newTestChecker())

	r := httptest.NewRequest("GET", "/ws", nil)
	// 不设置 Origin 头

	if checkFn(r) {
		t.Error("无 Origin 头且白名单无 'none' 时应拒绝")
	}
}

func TestGorillaCheckOrigin_启用且无Origin但白名单有none(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com,none")

	checkFn := GorillaCheckOriginWithChecker(newTestChecker())

	r := httptest.NewRequest("GET", "/ws", nil)

	if !checkFn(r) {
		t.Error("无 Origin 头但白名单含 'none' 时应放行")
	}
}

func TestGorillaCheckOrigin_默认校验器(t *testing.T) {
	// GorillaCheckOrigin() 使用默认环境变量名
	checkFn := GorillaCheckOrigin()
	if checkFn == nil {
		t.Error("GorillaCheckOrigin() 不应返回 nil")
	}
}

// ─── HTTPMiddleware 测试 ───

func TestHTTPMiddleware_非WS请求放行(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddlewareWithChecker(newTestChecker())
	handler := middleware(next)

	// 普通 HTTP 请求（无 Upgrade 头）
	r := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !called {
		t.Error("非 WebSocket 请求应传递给下一个 handler")
	}
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}
}

func TestHTTPMiddleware_未启用时放行(t *testing.T) {
	unsetEnv(t, testEnableEnv)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddlewareWithChecker(newTestChecker())
	handler := middleware(next)

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !called {
		t.Error("未启用校验时应传递给下一个 handler")
	}
}

func TestHTTPMiddleware_校验通过放行(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddlewareWithChecker(newTestChecker())
	handler := middleware(next)

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !called {
		t.Error("校验通过时应传递给下一个 handler")
	}
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}
}

func TestHTTPMiddleware_校验拒绝403(t *testing.T) {
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddlewareWithChecker(newTestChecker())
	handler := middleware(next)

	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if called {
		t.Error("校验拒绝时不应调用下一个 handler")
	}
	if w.Code != 403 {
		t.Errorf("期望状态码 403，实际 %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type 不匹配: %s", w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	if body != "Forbidden: Origin not allowed\n" {
		t.Errorf("响应体不匹配: %q", body)
	}
}

func TestHTTPMiddleware_默认校验器(t *testing.T) {
	middleware := HTTPMiddleware()
	if middleware == nil {
		t.Error("HTTPMiddleware() 不应返回 nil")
	}
}

// ─── isWebSocketUpgrade 测试 ───

func TestIsWebSocketUpgrade_标准头(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	if !isWebSocketUpgrade(r) {
		t.Error("Upgrade: websocket 应识别为 WebSocket 请求")
	}
}

func TestIsWebSocketUpgrade_大写头(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("Upgrade", "WebSocket")
	if !isWebSocketUpgrade(r) {
		t.Error("Upgrade: WebSocket（大写）应识别为 WebSocket 请求")
	}
}

func TestIsWebSocketUpgrade_非WS请求(t *testing.T) {
	r := httptest.NewRequest("GET", "/api", nil)
	if isWebSocketUpgrade(r) {
		t.Error("普通 HTTP 请求不应识别为 WebSocket 请求")
	}
}

// ─── NewOriginCheckerWithEnv 测试 ───

func TestNewOriginCheckerWithEnv_自定义环境变量(t *testing.T) {
	customEnable := "CUSTOM_WS_ENABLE"
	customHosts := "CUSTOM_WS_HOSTS"
	setEnv(t, customEnable, "1")
	setEnv(t, customHosts, "custom.com")

	checker := NewOriginCheckerWithEnv(customEnable, customHosts)

	if !checker.IsEnabled() {
		t.Error("使用自定义环境变量名时应启用校验")
	}
	if !checker.IsAllowed("https://custom.com") {
		t.Error("使用自定义环境变量名时应匹配白名单")
	}
}

// ─── 热生效测试 ───

func TestOriginChecker_热生效(t *testing.T) {
	// 初始状态：启用校验，白名单含 example.com
	setEnv(t, testEnableEnv, "1")
	setEnv(t, testHostsEnv, "example.com")

	checker := newTestChecker()

	// 校验 example.com 通过
	if !checker.IsAllowed("https://example.com") {
		t.Error("初始状态应放行 example.com")
	}
	// 校验 evil.com 拒绝
	if checker.IsAllowed("https://evil.com") {
		t.Error("初始状态应拒绝 evil.com")
	}

	// 运行时修改环境变量：将白名单改为 evil.com
	setEnv(t, testHostsEnv, "evil.com")

	// 重新校验应反映新配置
	if checker.IsAllowed("https://example.com") {
		t.Error("修改白名单后应拒绝 example.com")
	}
	if !checker.IsAllowed("https://evil.com") {
		t.Error("修改白名单后应放行 evil.com")
	}

	// 关闭校验
	unsetEnv(t, testEnableEnv)
	if checker.IsEnabled() {
		t.Error("关闭校验后 IsEnabled 应返回 false")
	}
}
