package agent

import (
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// MemberRuntime：团队成员「大脑缝」接口，一比一移植 Python
// openjiuwen/agent_teams/agent/member_runtime.py:40-171 的
// “MemberRuntime“ Protocol（2026-11-03 上游快照，文件全文 192 行）。
//
// StreamController / coordination / configurator 只经本面驱动成员的大脑。
// 默认实现是 TeamHarness（DeepAgent 背书）；外部 CLI 成员由
// externalcli.ExternalCliRuntime / ReinvokeCliRuntime / ExternalHarnessMemberRuntime
// 实现同一表面。Python 侧仅声明实例上实际被访问的成员；rail/记忆钩子在
// 外部 runtime 上实现为 no-op（configurator 对此类成员不调用它们）。
//
// 承载方式差异（spec 15.13 §5 登记）：Python 以 “MemberRuntimeLike =
// CliRuntimeBase | ExternalHarnessMemberRuntime“ 联合别名承载同一鸭子面；
// Go 无联合类型，以本接口承载——外部第三方 provider 运行时未来也须满足。
//
// Python: MemberRuntime (member_runtime.py:40)
type MemberRuntime interface {
	// ───── lifecycle ─────

	// Start 启动一个运行周期的轮次生命周期。DeepAgent 背书的 runtime 会从
	// teamSession 派生子 agent 会话（共享团队会话 id 与持久化 DeepAgentState）；
	// SDK 背书的外部 runtime 同样可派生成员 AgentSession 承载后端原生 resume
	// 状态；纯子进程 CLI runtime 可忽略 teamSession（Go 侧工厂闭包在构造期
	// 已捕获团队会话，D-15.13-g）。
	//
	// Python: start (member_runtime.py:45)
	Start(teamSession any) error
	// Stop 停止 runtime、取消在飞工作并关闭 outputs。
	//
	// Python: stop (member_runtime.py:56)
	Stop()
	// State 返回当前生命周期相位（HarnessState）。
	//
	// Python: state property (member_runtime.py:61)
	State() harnessprotocol.HarnessState
	// SessionID 返回当前会话 id；start 前 Python 返回 None，Go 以 nil 承载。
	//
	// Python: session_id property (member_runtime.py:66)
	SessionID() *string

	// ───── interaction ─────

	// Outputs 返回队列背书的输出块迭代面（Go 以 channel 承载 async 迭代器，
	// D-15.13-b/j）。
	//
	// Python: outputs (member_runtime.py:72)
	Outputs() <-chan stream.OutputSchema
	// Send 提交输入；immediate=True 中途 steer 在飞轮次。content 可为
	// InteractiveInput 以应答挂起中断；返回 runtime 自定义的确认（DeepAgent
	// runtime 为序列 id；CLI runtime 为 nil）。
	//
	// Python: send (member_runtime.py:76)
	Send(content any, immediate bool) (any, error)
	// Abort 中止在飞轮次：graceful（false）或 hard+rollback（true）。
	//
	// Python: abort (member_runtime.py:85)
	Abort(immediate bool)
	// Pause 暂停在飞轮次；下一次 send 重启它。
	//
	// Python: pause (member_runtime.py:89)
	Pause()
	// Resume 恢复保留的热轮次或冷 checkpoint 还原。Python 判
	// ``query is not None``，Go 以 nil 判定。
	//
	// Python: resume (member_runtime.py:93)
	Resume(query any) error
	// Subscribe 注册可选的相位/轮次回调；两者均可为 nil（只注册非 nil 回调）。
	// onState 接收 old/new（相位）与 session_id；onRound 接收 kind
	// （started/finished/aborted/paused/failed）、round_id 与 result。
	//
	// Python: subscribe (member_runtime.py:97)
	Subscribe(
		onState func(old, new harnessprotocol.HarnessState, sessionID string),
		onRound func(kind string, roundID int, turnID string, result *harnessprotocol.TurnResult),
	)

	// ───── interrupt-resume helpers ─────

	// HasPendingInterrupt 返回 runtime 是否在等待中断应答。
	//
	// Python: has_pending_interrupt (member_runtime.py:114)
	HasPendingInterrupt() bool
	// IsPendingInterruptResumeValid 返回 userInput 是否可解挂起中断。
	//
	// Python: is_pending_interrupt_resume_valid (member_runtime.py:118)
	IsPendingInterruptResumeValid(userInput any) bool
	// InitCwdForRound 初始化每轮工作目录（如有）。
	//
	// Python: init_cwd_for_round (member_runtime.py:122)
	InitCwdForRound()

	// ───── rail / memory hooks（默认 DeepAgent 路径） ─────

	// FindRails 返回已挂载的 railType 类 rail（不支持时空切片）。
	//
	// Python: find_rails (member_runtime.py:128)
	FindRails(railType any) []any
	// RegisterRail 在运行中的 agent 上注册附加 rail。
	//
	// Python: register_rail (member_runtime.py:132)
	RegisterRail(rail any) error
	// UnregisterRail 注销先前注册的 rail。
	//
	// Python: unregister_rail (member_runtime.py:136)
	UnregisterRail(rail any) error
	// RegisterMemberTools 在 agent 上注册团队记忆工具箱。
	//
	// Python: register_member_tools (member_runtime.py:140)
	RegisterMemberTools(memoryManager any)
	// InjectMemberMemory 把已加载记忆注入 agent 系统提示词。
	//
	// Python: inject_member_memory (member_runtime.py:144)
	InjectMemberMemory(memoryManager any, query string)
	// SetBackgroundTaskController 挂接外部后台任务控制器（pause/resume 面）。
	// 驱动 leader 的后台 swarmflow 运行；不启动后台工具的非 DeepAgent runtime
	// 实现为 no-op。
	//
	// Python: set_background_task_controller (member_runtime.py:148)
	SetBackgroundTaskController(controller any)
	// BackgroundTaskController 上述挂接的读侧；未挂接时 Python 返回 None、
	// Go 返回 nil。
	//
	// Python: background_task_controller property (member_runtime.py:157)
	BackgroundTaskController() any

	// ───── config snapshots ─────

	// Workspace 返回绑定到 runtime 的 workspace（无则 nil）。
	//
	// Python: workspace property (member_runtime.py:164)
	Workspace() any
	// SysOperation 返回绑定到 runtime 的 sys_operation（无则 nil）。
	//
	// Python: sys_operation property (member_runtime.py:169)
	SysOperation() any
}

// TaskNoticeAwareRuntime 能立刻向成员告知任务编辑的 runtime 面（F_129）。
// 与 MemberRuntime、名册 protocol 分开保留：没有它的 runtime 只是不会收到
// 早期任务通知，其余行为不变。Python 侧 async 返回 None；Go 以 error 承载
// 失败路径（对齐 externalcli 实现形状）。
//
// Python: TaskNoticeAwareRuntime (member_runtime.py:174)
type TaskNoticeAwareRuntime interface {
	// AnnounceTaskRevisions 立即投递成员待发的任务编辑通知（如有）。
	// 由成员的任务通知 tracker 决定说什么，已最新时为 no-op。
	//
	// Python: announce_task_revisions (member_runtime.py:183)
	AnnounceTaskRevisions() error
}
