package harnessobservability

import (
	"sync"

	"go.opentelemetry.io/otel/trace"

	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
)

// span_context.go 运行根 span 的 session 键注册表。
// Python: harness/observability/span_context.py（全文 177 行）。
// 单 agent 在 session-setup supervisor 任务里执行，请求的 ContextVar 传不进去，
// 那里的查询什么都看不到——本注册表按 session id 键控、无视任务边界可读，
// 包装共享 get_root_span 使所有父查找都能回退到它。
// 按键而非单槽的原因：会话重叠（单进程同时服务多个 chat），单槽会互相打架
// （先结束者清槽 → 在跑的运行中途丢 agent 层 span；后开者占槽 → 别的运行
// 的 span 落错迹线）。

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// runRootMu 注册表锁。
	// Python: dict 直读写（单线程 asyncio）；Go 并发需锁（偏差登记 spec §4）。
	runRootMu sync.RWMutex
	// runRoots 当前在跑的运行的根 span，按 session id 键。
	// Python: _ROOT_SPANS: dict[str, Any]（span_context.py:29）。
	runRoots = map[string]trace.Span{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterRunRootSpan 注册 span 为 session 所属运行的根。
// sessionID 为空是合法键（无 session 的运行）。
// Python: register_run_root_span（span_context.py:63-74）。
func RegisterRunRootSpan(span trace.Span, sessionID string) {
	runRootMu.Lock()
	runRoots[sessionID] = span
	runRootMu.Unlock()
}

// UnregisterRunRootSpan 仅当 span 仍拥有该注册项才清除。
// Python: unregister_run_root_span（span_context.py:76-96）——会话重叠，
// 误清会弄瞎仍在跑的运行（其子 agent 会中途丢 span）。
func UnregisterRunRootSpan(span trace.Span, sessionID string) {
	runRootMu.Lock()
	if runRoots[sessionID] == span {
		delete(runRoots, sessionID)
	}
	runRootMu.Unlock()
}

// ResolveRunRootSpan 返回调用方所属运行的根 span（按 session id 解析），
// 或 nil。解析只按 session id：session id 在 agent 执行期间被绑定，ContextVar
// 传不到的任务里也可读——这正是兜底要用的地方。绝不收养"唯一的活根"：
// 那只是碰巧在跑的 session，结束运行的迟到回调会落进别的迹线。
// 缺根好过错根。Python: resolve_run_root_span（span_context.py:98-124）。
func ResolveRunRootSpan(sessionID string) trace.Span {
	requested := sessionID
	if requested == "" {
		requested = obsCurrentSessionID()
	}
	runRootMu.RLock()
	span := runRoots[requested]
	runRootMu.RUnlock()
	if span != nil && span.IsRecording() {
		return span
	}
	return nil
}

// ResetRunRootSpans 清空全部注册根（shutdown 与测试隔离用）。
// Python: reset_run_root_spans（span_context.py:99-102）。
func ResetRunRootSpans() {
	runRootMu.Lock()
	runRoots = map[string]trace.Span{}
	runRootMu.Unlock()
}

// obsCurrentSessionID 读环境 session id（共享观测层的当前 session）。
// Python: current_session_id（span_context.py:127-149）双源——team run 绑自己的
// ContextVar，单 agent run 开根时发布到共享观测态；哪个被设了哪个答。
func obsCurrentSessionID() string {
	return obs.GetCurrentSessionID()
}
