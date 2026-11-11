package processcli

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandSignals 一次机器调用的信号归属：执行取消一次，清理期屏蔽后续信号。
// Python: machine_signals.py:27-76 CommandSignals
//
// 同步输入/导入期收到信号 → Python raise KeyboardInterrupt；
// 异步执行期 → cancel 持有该任务的 task，不碰任意 Runtime 任务。
// 清理留在该任务内以保有 async-generator context token 归属。
// 重复的优雅信号绝不升级为放弃 Runtime.close()。
type CommandSignals struct {
	// Interrupted 是否收到过中断信号
	Interrupted bool
	// Cleaning 是否已进入清理期
	Cleaning bool
	// mu 保护 Interrupted / Cleaning / settled / cancel
	mu sync.Mutex
	// settled 终态是否已冻结（finalize 之后不再响应）
	settled bool
	// cancel 执行期上下文取消函数（nil = 尚未进入执行态）
	cancel context.CancelFunc
	// cancelled 是否已触发过取消（供测试与幂等判定）
	cancelled atomic.Bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// activeSignals 当前生效的信号归属（等价 Python 的 _ACTIVE ContextVar）。
	// Python 用 ContextVar 做任务局部；Go 侧机器入口是单次进程主路径，
	// 用包级守卫，语义对齐「已有则复用」。
	activeSignalsMu sync.Mutex
	// activeSignals 当前 owner
	activeSignals *CommandSignals
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCommandSignals 构造信号归属。
// Python: machine_signals.py:36-40 CommandSignals.__init__
func NewCommandSignals() *CommandSignals {
	return &CommandSignals{}
}

// Cancelled 是否已触发取消（清理期不触发）。
func (s *CommandSignals) Cancelled() bool {
	return s.cancelled.Load()
}

// Interrupt 信号回调本体：不持有任何 Runtime 或 transport 实现。
// Python: machine_signals.py:42-52 interrupt
//
// 偏差 D-2：Python 在未进入异步执行态时 raise KeyboardInterrupt；
// Go 的监听 goroutine 不能 panic 到别处，改为 os.Exit(130)（见 handleSignal）。
func (s *CommandSignals) Interrupt(signum int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Python: :44-45 if self._settled or self.interrupted: return
	if s.settled || s.Interrupted {
		return
	}
	s.Interrupted = true
	// Python: :47-48 if self.cleaning: return
	if s.Cleaning {
		return
	}
	// Python: :49-52 有活跃任务则 cancel，否则 KeyboardInterrupt
	if s.cancel != nil {
		s.cancelled.Store(true)
		s.cancel()
		return
	}
	// 偏差 D-2：未进入执行态 → 直接终态退出
	os.Exit(130)
}

// Finalize 发布前冻结结果；保留第一个失败。
// Python: machine_signals.py:64-76 finalize
func (s *CommandSignals) Finalize(result *protocol.OneShotRunResult) *protocol.OneShotRunResult {
	s.mu.Lock()
	interrupted := s.Interrupted
	s.settled = true
	s.mu.Unlock()

	// Python: :67-76 if self.interrupted and result.error is None: replace(...)
	if interrupted && result.Error == nil {
		// Python: replace(result, status=CANCELLED, exit_code=130, error=...)
		// replace 生成新对象，避免就地改写调用方持有的值
		cancelled := *result
		cancelled.Status = string(protocol.StatusCancelled)
		cancelled.ExitCode = 130
		cancelled.Error = &protocol.RuntimeErrorInfo{
			Code:    "CANCELLED",
			Message: "Command interrupted.",
		}
		return &cancelled
	}
	return result
}

// CommandSignalsContext 为机器入口安装信号处理，返回恢复函数。
// Python: machine_signals.py:79-98 command_signals
//
// 「只装一次」：已有 owner 时直接复用并原样返回（不改 handler）。
func CommandSignalsContext() (owner *CommandSignals, restore func()) {
	activeSignalsMu.Lock()
	defer activeSignalsMu.Unlock()

	// Python: :82-85 existing is not None → yield existing; return
	if activeSignals != nil {
		return activeSignals, func() {}
	}
	owner = NewCommandSignals()
	activeSignals = owner

	ch := make(chan os.Signal, 1)
	// Python: :90-93 for name in ("SIGINT","SIGTERM","SIGBREAK")
	signal.Notify(ch, signalSet()...)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range ch {
			if sig == nil {
				return
			}
			owner.Interrupt(signalNumber(sig))
		}
	}()

	restore = func() {
		// Python: :96-97 恢复每个先前的 handler
		signal.Stop(ch)
		close(ch)
		<-done
		activeSignalsMu.Lock()
		activeSignals = nil
		activeSignalsMu.Unlock()
	}
	return owner, restore
}

// DeferCommandSignals 让信号取消不再打断协作式资源释放。
// Python: machine_signals.py:101-105 defer_command_signals
func DeferCommandSignals(s *CommandSignals) {
	if s == nil {
		activeSignalsMu.Lock()
		s = activeSignals
		activeSignalsMu.Unlock()
	}
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Cleaning = true
	s.mu.Unlock()
}

// BindCancel 把执行期取消函数绑定到信号归属。
// Python: machine_signals.py:54-62 run（绑定当前 task）
//
// 返回的解除函数对应 Python run() 的 finally：置 cleaning 并清空绑定。
func (s *CommandSignals) BindCancel(cancel context.CancelFunc) (unbind func()) {
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.Cleaning = true
		s.cancel = nil
		s.mu.Unlock()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// signalSet 待监听的信号集合。
// Python: machine_signals.py:90-93
//
//	for name in ("SIGINT", "SIGTERM", "SIGBREAK"):
//	    signum = getattr(signal, name, None)
//	    if signum is not None: previous[signum] = signal.signal(signum, owner.interrupt)
//
// 上游用 getattr 做存在性判断，不存在的名字跳过。
// Go 标准库在两个平台都定义了 SIGINT 与 SIGTERM，但都没有 SIGBREAK
// （Windows 只有虚拟的 SIGTERM/SIGINT），故按同一语义只保留这两个。
func signalSet() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}

// signalNumber 取信号的数值表示（用于日志与断言）。
func signalNumber(sig os.Signal) int {
	if sysSig, ok := sig.(syscall.Signal); ok {
		return int(sysSig)
	}
	return 0
}
