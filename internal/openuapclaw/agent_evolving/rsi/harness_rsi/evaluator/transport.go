// transport.go 可重试的传输层失败判定，与 harness 执行失败相区分。
// Python: openjiuwen/rsi/harness_rsi/evaluator/transport.py（37 行全量对齐）。

package evaluator

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

// transientErrorMarkers 瞬态传输失败特征子串（Python _TRANSIENT_ERROR_MARKERS 元组逐字对齐）。
var transientErrorMarkers = []string{
	"connection error",
	"connection reset",
	"connection refused",
	"connection aborted",
	"connectionerror",
	"remoteprotocolerror",
	"peer closed connection",
	"incomplete chunked read",
	"server disconnected",
	"remotedisconnected",
	"timed out",
	"timeout",
	"temporarily unavailable",
	"temporary failure in name resolution",
	"could not resolve host",
	"service unavailable",
	"rate limit",
	"too many requests",
	"http 429",
	"http 502",
	"http 503",
	"http 504",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TransientTransportError 当错误描述可重试的传输层失败时返回原错误，否则返回空串。
// Python: transport.py transient_transport_error。
func TransientTransportError(errText string) string {
	lowered := strings.ToLower(errText)
	for _, marker := range transientErrorMarkers {
		if strings.Contains(lowered, marker) {
			return errText
		}
	}
	return ""
}
