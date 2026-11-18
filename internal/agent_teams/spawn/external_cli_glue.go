// 本文件承载 external_cli_spawn 的 Go 侧胶合类型（卡片视图 / 原子引用 /
// 进程句柄构造）。Python 无对应物：它们是 D-15.16-b1 注入缝的承载件。
//
// Python 侧无对应实现（注入缝胶合，规则 9.6 登记 D-15.16-b1）。
package spawn

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalCliAgentCard 外部成员 TeamAgent 卡片视图（由注入侧转成具体
// agentschema.AgentCard 构造成员壳）。
type ExternalCliAgentCard struct {
	// ID card id（team_name_member_name）
	ID string
	// Name 成员名
	Name string
	// Description 描述
	Description string
}

// externalCliSpawnRefMu teammateBackendRef 的互斥锁。
var externalCliSpawnRefMu sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInProcessExternalHandle 构造外部成员的进程内句柄：成员任务已在
// ExternalCliSpawn 的 goroutine 中运行，本句柄承载 process_id、agent 引用与
// 取消信号（Python InProcessSpawnHandle(process_id, _task=task,
// agent_ref=teammate)；Go 任务生命周期由 goroutine + deps.RunMemberTask 承担）。
// teammate 以 SpawnableAgent 注入（*agent.TeamAgent 满足，由注入侧保证）。
func NewInProcessExternalHandle(processID string, teammate SpawnableAgent) *InProcessSpawnHandle {
	handleCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	_ = handleCtx
	return &InProcessSpawnHandle{
		processID: processID,
		cancelCtx: cancel,
		done:      done,
		agentRef:  teammate,
	}
}
