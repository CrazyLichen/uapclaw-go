package pregel

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 对齐 Python openjiuwen/core/graph/pregel/task.py（174 行）：
// TaskExecutorPool 单 step 任务池 + NodeTask 单节点执行体。
// Python 的 store.base.PendingNode 延迟导入（task.py:23/37）破环，由偏差 D6 承接
// （类型定义迁入本包 pending.go）。

// （PregelLoop 传给 pool 的是 inner_config——见 engine.py:40 审查修正）

// ──────────────────────────── 结构体 ────────────────────────────

// nodeResult 单个节点任务的交付物。
// Python run() 的返回类型 Union[List[Message], GraphInterrupt] 由本结构承载：
// 成功时 msgs 非空、err=nil；中断时 msgs=nil、err=*GraphInterrupt（经 recover 降级，
// task.py:172-174）；普通错误时 err 为原样错误（向 wait_all 传播以取消兄弟任务）。
type nodeResult struct {
	// msgs 成功任务的路由消息
	msgs []Message
	// err 失败任务的上抛错误（含中断）
	err error
}

// nodeTask 单节点执行任务（Python NodeTask，task.py:117-174）。
// 字段与 Python __init__ 一一对应（node/config/messages/version）；
// ctx/cancel/done/finished 为方案 B 并发模型的承载件（偏差 D2/D5）：
// asyncio.Task.cancel 由 per-task cancel ctx 承担。
type nodeTask struct {
	// node 执行目标节点
	node *PregelNode
	// config 外部配置的透传载体。Python 的 config 是 TypedDict，可动态携带
	// parent_ns 键（task.py:141 从 create_inner_config 结果里读 PARENT_NS）；
	// Go 结构体无动态键，故用 InnerPregelConfig 承载同样的数据形状，
	// 归一化（CreateInnerConfig）在 execute 内做——对齐 task.py:140 的调用时机。
	config *InnerPregelConfig
	// messages 本次执行路由产出的消息（Python task.py:122）
	messages []Message
	// version 节点版本号，参与 ns 拼接（Python task.py:123）
	version int
	// ctx 本任务的可取消执行上下文（asyncio Task.cancel 的注入点）
	ctx context.Context
	// cancel 取消函数，由 pool 在 FIRST_EXCEPTION / CancelAll 路径调用
	cancel context.CancelFunc
	// done 结果交付通道（容量 1；完成后必发一次，无人读也不阻塞）
	done chan nodeResult
	// finished 标记 execute 已完成（CancelAll 判未完成任务用）
	finished atomic.Bool
}

// TaskExecutorPool 单 step 的节点任务池（Python TaskExecutorPool，task.py:19-114）。
// 非并发安全约定：engine 单线程逐 step 调用 Submit→WaitAll（对齐 Python 事件循环
// 串行语义）；runningTasks 的 Submit/WaitAll/CancelAll 交叉访问用 mu 兜底，
// succeedMessages/failed 与 Python 一样仅由 wait_all/cancel_all/clear 触碰，不加锁。
type TaskExecutorPool struct {
	// config 执行配置。Python 侧 PregelLoop.init 把 **inner_config**（create_inner_config
	// 结果，含 PARENT_NS/NS/SESSION_ID）传给 TaskExecutorPool（engine.py:40），
	// task.py:141 的 PARENT_NS 正是从它读出——Go 用 *InnerPregelConfig 一比一承接，
	// ParentNs 的注入通道由此贯通（审查修正：原 *PregelConfig 会使子图 ns 拼接分支
	// 经 pool 路径永不可达）。
	config *InnerPregelConfig
	// succeedMessages 成功任务的路由消息（Python succeed_messages）
	succeedMessages []Message
	// failed 失败节点记账（Python failed）
	failed map[string]PendingNode
	// runningTasks 在途任务→节点反查表（Python running_tasks，偏差 D2）
	runningTasks map[*nodeTask]*PregelNode
	// mu 保护 runningTasks 的交叉访问
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量（规则 3）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskExecutorPool 构造任务池（Python task.py:20-25）。
// 入参即 Python PregelLoop.init 传入的 inner_config（engine.py:40）。
func NewTaskExecutorPool(config *InnerPregelConfig) *TaskExecutorPool {
	return &TaskExecutorPool{
		config:       config,
		failed:       make(map[string]PendingNode),
		runningTasks: make(map[*nodeTask]*PregelNode),
	}
}

// Submit 提交节点执行任务（Python task.py:27-30）。
// D5：ctx 形参承载 asyncio create_task 的取消句柄；每任务派生可取消子 ctx。
func (p *TaskExecutorPool) Submit(ctx context.Context, node *PregelNode, version int) {
	// Python task.py:29 把 self.config（inner_config）原样传给 NodeTask；归一化在 run 内做
	t := newNodeTask(node, p.config, version, ctx)
	p.mu.Lock()
	p.runningTasks[t] = node
	p.mu.Unlock()
	go t.run()
}

// WaitAll 等待全部在途任务收场，FIRST_EXCEPTION 语义（Python task.py:40-90）。
// 1) 任一任务出错（含中断）即取消其余在途任务（对应 asyncio.wait FIRST_EXCEPTION
// 提前返回 + pending.cancel()，task.py:70-72）；2) 被取消任务记 context.Canceled
// （偏差 D3），结果带异常先打 warning（task.py:74-80）；3) 普通异常 > 中断 优先上抛
// （task.py:86-90）。
// 取消是协作式的：NodeFunc 必须响应 ctx（对齐 Python 协程在 await 点注入
// CancelledError；无 await 的 fn 忽略取消则本轮等待至其自然完成——Python 同性质）。
func (p *TaskExecutorPool) WaitAll() error {
	p.mu.Lock()
	total := len(p.runningTasks)
	p.mu.Unlock()
	if total == 0 {
		return nil
	}

	var (
		firstErr     error
		interruptErr *GraphInterrupt
	)
	received := make(map[*nodeTask]nodeResult, total)
	cancelled := make(map[*nodeTask]bool, total)

	for len(received) < total {
		// 就绪通道快照：每轮重建 select 集（对应 asyncio.wait 的 tasks 列表）
		p.mu.Lock()
		pending := make([]*nodeTask, 0, len(p.runningTasks))
		for t := range p.runningTasks {
			if _, ok := received[t]; !ok {
				pending = append(pending, t)
			}
		}
		p.mu.Unlock()

		cases := make([]reflect.SelectCase, len(pending))
		for i, t := range pending {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.done)}
		}
		chosen, val, _ := reflect.Select(cases)
		t := pending[chosen]
		res := val.Interface().(nodeResult)
		received[t] = res
		p.mu.Lock()
		node := p.runningTasks[t]
		delete(p.runningTasks, t) // Python task.py:53 pop(t)
		p.mu.Unlock()

		if cancelled[t] {
			// 取消收尾分支（Python task.py:74-80 的 gather 段）：仅真正被 cancel() 的任务
			if res.err != nil {
				logger.Warn(logComponent).
					Str("event_type", "graph_vertex_call_error").
					Str("error", res.err.Error()).
					Msg("task cancelled with exception")
			}
			continue
		}

		if res.err != nil {
			p.commitFailure(node, res.err)
			var gi *GraphInterrupt
			if errors.As(res.err, &gi) {
				if interruptErr == nil {
					interruptErr = gi
				}
			} else if firstErr == nil {
				firstErr = res.err
			}
			// FIRST_EXCEPTION：尚有在途 → 进入取消路径（Python task.py:70-72）。
			// 只取消未完成任务（Python 的 pending 集 = 唤醒时未 done 者）；
			// 已完成者的结果在后续接收中走正常分支，参与优先级上抛。
			if len(received) < total {
				for _, ot := range pending {
					if ot != t && !ot.finished.Load() {
						ot.cancel()
						cancelled[ot] = true
					}
				}
			}
		} else {
			p.succeedMessages = append(p.succeedMessages, res.msgs...)
		}
	}

	// 取消记账（Python task.py:81-84）：无论结果如何都记 CancelledError 等价物
	for t := range cancelled {
		p.commitFailure(t.node, context.Canceled)
	}

	// 优先级：普通异常 > 中断（Python task.py:86-90）
	if firstErr != nil {
		return firstErr
	}
	if interruptErr != nil {
		return interruptErr
	}
	return nil
}

// Clear 清空三容器（Python task.py:92-95）。
func (p *TaskExecutorPool) Clear() {
	p.succeedMessages = nil
	p.failed = make(map[string]PendingNode)
	p.mu.Lock()
	p.runningTasks = make(map[*nodeTask]*PregelNode)
	p.mu.Unlock()
}

// CancelAll 取消全部在途任务并清空 runningTasks（Python task.py:97-114）。
// 不上抛：被取消任务结果带异常仅打 warning（task.py:104-109）；
// 已完成任务不入 to_cancel、不记账（Python 只 pop 被取消的）。
func (p *TaskExecutorPool) CancelAll() {
	p.mu.Lock()
	toCancel := make([]*nodeTask, 0, len(p.runningTasks))
	for t := range p.runningTasks {
		if !t.finished.Load() {
			t.cancel()
			toCancel = append(toCancel, t)
		}
	}
	p.mu.Unlock()

	for _, t := range toCancel {
		res := <-t.done // 对齐 asyncio.gather(return_exceptions=True) 的等待（task.py:103）
		if res.err != nil {
			logger.Warn(logComponent).
				Str("event_type", "graph_vertex_call_error").
				Str("error", res.err.Error()).
				Msg("running task with exception")
		}
		p.commitFailure(t.node, context.Canceled)
	}
	p.mu.Lock()
	p.runningTasks = make(map[*nodeTask]*PregelNode)
	p.mu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newNodeTask 构造任务（对应 Python NodeTask.__init__，task.py:119-123）。
// base 为 pool Submit 传入的执行上下文，本处派生可取消子 ctx。
func newNodeTask(node *PregelNode, config *InnerPregelConfig, version int, base context.Context) *nodeTask {
	ctx, cancel := context.WithCancel(base)
	return &nodeTask{
		node:    node,
		config:  config,
		version: version,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan nodeResult, 1),
	}
}

// run 执行任务并交付结果（对应 Python NodeTask.run，task.py:125-174）。
// Python 返回值 Union[List[Message], GraphInterrupt] 由 nodeResult{msgs, err} 承载。
func (t *nodeTask) run() {
	msgs, err := t.execute()
	t.finished.Store(true)
	t.done <- nodeResult{msgs: msgs, err: err}
}

// execute 执行节点函数并收集路由消息（按 Python task.py:125-174 逐步骤）。
// 中断经 recover 降级：NodeFunc 纪律（node.go:19-23）——图级中断是 panic，
// 本处是唯一 recover 点，只认 *GraphInterrupt，其余 panic 原样重抛。
func (t *nodeTask) execute() (msgs []Message, err error) {
	defer func() {
		// 对齐 task.py:172-174 except GraphInterrupt: return e
		if r := recover(); r != nil {
			gi, ok := r.(*GraphInterrupt)
			if !ok {
				panic(r)
			}
			msgs, err = nil, gi
		}
	}()

	// Python task.py:140：create_inner_config（补递归上限默认、拷贝入参）。
	// TypedDict 动态键 parent_ns 的 Go 等价：从 InnerPregelConfig 透传 ParentNs。
	// 偏差 D1：Go 固定签名无条件注入；task.py:143 ParentNs 非空才拼接、双写。
	inner := CreateInnerConfig(t.config.PregelConfig)
	inner.ParentNs = t.config.ParentNs
	if inner.ParentNs != "" {
		newFullNs := inner.ParentNs + ":" + t.node.name + ":" + strconv.Itoa(t.version)
		inner.Ns = newFullNs
		inner.ParentNs = newFullNs
	}

	// Python task.py:151-154 sync/coroutine 双路 → 偏差 D4 单路直调；
	// state 恒 nil（Python task.py:149 字面量 None）。
	if err := t.node.fn(t.ctx, NodeInput{Config: inner}); err != nil {
		return nil, err
	}

	// Python task.py:157-160：依次 dispatch 收集消息。
	t.messages = make([]Message, 0, len(t.node.routers))
	for _, r := range t.node.routers {
		out, derr := r.Dispatch(t.ctx, t.node.name)
		if derr != nil {
			return nil, derr
		}
		t.messages = append(t.messages, out...)
	}
	msgs = t.messages

	// Python task.py:162-168：逐条 EDGE_TRAVERSED 回调。
	// 锚点：graph_id 读外部配置的 Ns（Python self.config.get(NS)），不是拼过的 inner.Ns。
	if len(msgs) > 0 {
		for _, m := range msgs {
			callback.Trigger(t.ctx, string(callback.EdgeTraversed), map[string]any{
				"source_node": m.Sender,
				"target_node": m.Target,
				"graph_id":    t.config.Ns,
			})
		}
	}
	return msgs, nil
}

// commitFailure 失败记账，同名首记不覆盖（Python task.py:32-38）。
func (p *TaskExecutorPool) commitFailure(node *PregelNode, err error) {
	name := node.name
	if _, ok := p.failed[name]; ok {
		return
	}
	status := TaskStatusError
	var gi *GraphInterrupt
	if errors.As(err, &gi) {
		status = TaskStatusInterrupt
	}
	p.failed[name] = PendingNode{NodeName: name, Status: status, Exception: []error{err}}
}
