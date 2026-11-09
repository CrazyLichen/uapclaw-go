package harness

// ──────────────────────────── 结构体 ────────────────────────────

// controlCmd 控制命令。
// Python: ControlEvent = _CmdSend/_CmdAbort/_CmdPause/_CmdResume/
// _CmdRoundFinished/_CmdStop (agent_teams/harness/control.py)。
// Go 用接口 + 各自结构体表达，每条命令携带应答通道，等价 Python 的 ack: asyncio.Future。
type controlCmd interface {
	// ack 返回应答通道
	ack() chan error
}

// cmdSend 投递一条输入。
// Python: _CmdSend（包裹 InboxMessage + ack）
type cmdSend struct {
	// msg 待处理输入
	msg InboxMessage
	// reply 应答通道
	reply chan error
}

// ack 实现 controlCmd。
func (c *cmdSend) ack() chan error { return c.reply }

// cmdAbort 中断。
// Python: _CmdAbort
type cmdAbort struct {
	// immediate true 立即取消调度任务，false 让本轮自然收尾
	immediate bool
	// reply 应答通道
	reply chan error
}

// ack 实现 controlCmd。
func (c *cmdAbort) ack() chan error { return c.reply }

// cmdPause 暂停（25.1c 前恒返回不支持）。
// Python: _CmdPause
type cmdPause struct {
	// reply 应答通道
	reply chan error
}

// ack 实现 controlCmd。
func (c *cmdPause) ack() chan error { return c.reply }

// cmdResume 继续（25.1c 前恒返回不支持）。
// Python: _CmdResume（ack + query）
type cmdResume struct {
	// query 冷恢复载荷；暖恢复为 nil
	query any
	// reply 应答通道
	reply chan error
}

// ack 实现 controlCmd。
func (c *cmdResume) ack() chan error { return c.reply }

// cmdRoundFinished 一轮结束（由轮次协程投递）。
// Python: _CmdRoundFinished（round_id + error + result，无 ack）
type cmdRoundFinished struct {
	// roundID 轮次序号
	roundID int
	// result 本轮结果（wait_round_completion 的返回；取消/错误时为 nil）
	result map[string]any
	// err 本轮错误（成功为 nil）
	err error
}

// ack 实现 controlCmd；轮末命令无应答方。
func (c *cmdRoundFinished) ack() chan error { return nil }

// cmdStop 停机。
// Python: _CmdStop
type cmdStop struct {
	// reply 应答通道
	reply chan error
}

// ack 实现 controlCmd。
func (c *cmdStop) ack() chan error { return c.reply }

// ──────────────────────────── 非导出函数 ────────────────────────────

// ack 非阻塞回写应答；通道满则丢弃，保证 supervisor 崩溃路径不卡死。
// Python: NativeHarness._ack（控制命令的 Future 由 supervisor 结算）
func ack(ch chan error, err error) {
	if ch == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}
