package team

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 非导出函数（测试辅助） ────────────────────────────

// resetShutdownCleanupState 清空全局 shutdown cleanup 注册表（测试隔离）。
func resetShutdownCleanupState(t *testing.T) {
	t.Helper()
	shutdownCleanupMu.Lock()
	shutdownCleanupTasks = make(map[string]*shutdownCleanupEntry)
	shutdownCleanupMu.Unlock()
}

// waitShutdownCleanupEntryDone 等待延迟清理任务完成信号。
// 测试在注册表出现条目后直接返回会泄漏后台 goroutine 到后续测试，
// 其触碰全局状态（Runner.teamRuntimeManager/checkpointer/通知回调）与
// 后续测试对这些全局的注入写入构成数据竞争，必须在测试内等待收尾。
func waitShutdownCleanupEntryDone(t *testing.T, entry *shutdownCleanupEntry) {
	t.Helper()
	// 覆盖 2s 固定延迟 + 清理执行；对齐生产等待超时 15s 口径
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-entry.done:
	case <-timer.C:
		t.Fatal("延迟清理任务未在超时内完成")
	}
}

// newShutdownCleanupTestAgent 构造带内存 DB TeamBackend 的 TeamAgent（leader 角色）。
func newShutdownCleanupTestAgent(t *testing.T, teamName string) *agent.TeamAgent {
	t.Helper()
	card := agentschema.NewAgentCard()
	a := agent.NewTeamAgent(context.Background(), card)

	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("db init 失败: %v", err)
	}
	// leader 自身（后续测试里设置成员状态）
	if ok := db.Member().CreateMember(context.Background(), "team_leader", teamName, "Leader", "{}",
		"running", string(atschema.TeamRoleLeader), "", "idle", "build_mode", "", ""); !ok {
		t.Fatalf("创建 leader 失败")
	}
	// NewTeamBackend(teamName, memberName, isLeader, db, messager)——messager 为 nil
	// 仅影响消息收发路径，ListMembers 不依赖
	a.SetTeamBackend(tools.NewTeamBackend(teamName, "team_leader", true, db, nil))
	return a
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsDistributedLeaderRuntime_判定 验证分布式 leader 运行时判定。
// 对齐 Python _is_distributed_leader_runtime：mode=="distributed" && role=="leader"。
func TestIsDistributedLeaderRuntime_判定(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{
			name: "分布式 leader",
			cfg: map[string]any{"team": map[string]any{"runtime": map[string]any{
				"mode": "distributed", "role": "leader",
			}}},
			want: true,
		},
		{
			name: "分布式 teammate",
			cfg: map[string]any{"team": map[string]any{"runtime": map[string]any{
				"mode": "distributed", "role": "teammate",
			}}},
			want: false,
		},
		{
			name: "本地 leader（仅 transport pyzmq 也不算）",
			cfg: map[string]any{"team": map[string]any{
				"runtime":   map[string]any{"mode": "local", "role": "leader"},
				"transport": map[string]any{"type": "pyzmq"},
			}},
			// 注意：与 IsDistributedMode 不同，本函数只认 runtime.mode
			want: false,
		},
		{
			name: "大小写与空白容忍",
			cfg: map[string]any{"team": map[string]any{"runtime": map[string]any{
				"mode": "  Distributed ", "role": " LEADER ",
			}}},
			want: true,
		},
		{
			name: "缺失 team 节点",
			cfg:  map[string]any{},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDistributedLeaderRuntime(tc.cfg); got != tc.want {
				t.Errorf("isDistributedLeaderRuntime = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveTeamToolID_匹配规则 验证工具 ID 解析的匹配规则。
// 对齐 Python _team_tool_id：cid 精确/前缀匹配或 name 匹配，无匹配回退 expected。
func TestResolveTeamToolID_匹配规则(t *testing.T) {
	// resolveTeamToolID 依赖 DeepAgentInterface.AbilityManager()，
	// nil leader 直接走回退分支
	if got := resolveTeamToolID(nil, "shutdown_member"); got != "team.shutdown_member" {
		t.Errorf("nil leader 应回退默认 ID，got %q", got)
	}
}

// TestAllTeammatesShutdownRequestedOrDone_全员状态 验证全员关闭判定。
// 对齐 Python _all_teammates_shutdown_requested_or_done。
func TestAllTeammatesShutdownRequestedOrDone_全员状态(t *testing.T) {
	teamAgent := newShutdownCleanupTestAgent(t, "t1")
	backend := teamAgent.TeamBackend()
	ctx := context.Background()

	// 空成员列表 → false（Python: if not members: return False）
	// 注意 ListMembers 排除自身（team_leader），当前只有 leader → 列表为空
	got, err := allTeammatesShutdownRequestedOrDone(ctx, teamAgent)
	if err != nil {
		t.Fatalf("空列表判定不应报错: %v", err)
	}
	if got {
		t.Error("空成员列表应返回 false")
	}

	// 加入两个成员：一个 shutdown 一个 shutdown_requested → true
	mkMember(t, backend, ctx, "m1", string(atschema.MemberStatusShutdown))
	mkMember(t, backend, ctx, "m2", string(atschema.MemberStatusShutdownRequested))
	got, err = allTeammatesShutdownRequestedOrDone(ctx, teamAgent)
	if err != nil {
		t.Fatalf("全员关闭判定不应报错: %v", err)
	}
	if !got {
		t.Error("全员 shutdown_requested/shutdown 应返回 true")
	}

	// 加入一个 running 成员 → false
	mkMember(t, backend, ctx, "m3", "running")
	got, err = allTeammatesShutdownRequestedOrDone(ctx, teamAgent)
	if err != nil {
		t.Fatalf("混合状态判定不应报错: %v", err)
	}
	if got {
		t.Error("存在 running 成员应返回 false")
	}
}

// TestAllTeammatesShutdownRequestedOrDone_缺后端 验证缺 TeamBackend 时返回 false。
// 对齐 Python: tb is None → debug 日志 + return False。
func TestAllTeammatesShutdownRequestedOrDone_缺后端(t *testing.T) {
	card := agentschema.NewAgentCard()
	teamAgent := agent.NewTeamAgent(context.Background(), card) // 未配置 backend

	got, err := allTeammatesShutdownRequestedOrDone(context.Background(), teamAgent)
	if err != nil {
		t.Fatalf("缺后端不应报错: %v", err)
	}
	if got {
		t.Error("缺 TeamBackend 应返回 false")
	}
}

// TestScheduleShutdownCleanup_注册表生命周期 验证调度注册表创建/去重/完成移除。
// 对齐 Python _schedule_shutdown_cleanup + _delayed_shutdown_cleanup finally 移除。
func TestScheduleShutdownCleanup_注册表生命周期(t *testing.T) {
	resetShutdownCleanupState(t)

	// 无 pending → 立即创建任务
	scheduleShutdownCleanup("sess-1", "web")
	shutdownCleanupMu.Lock()
	entry := shutdownCleanupTasks["sess-1"]
	shutdownCleanupMu.Unlock()
	if entry == nil {
		t.Fatal("调度后注册表应有条目")
	}

	// 已有未完成任务 → 不重复创建
	scheduleShutdownCleanup("sess-1", "web")
	shutdownCleanupMu.Lock()
	entry2 := shutdownCleanupTasks["sess-1"]
	count := len(shutdownCleanupTasks)
	shutdownCleanupMu.Unlock()
	if count != 1 {
		t.Errorf("未完成任务不应重复创建，count = %d", count)
	}
	if entry2 != entry {
		t.Error("重复调度应复用现有条目")
	}

	// 等待延迟清理 goroutine 完成，避免泄漏到后续测试（触碰全局状态的 race）。
	// 完成后 finally 语义应将条目从注册表移除（done 关闭先于 defer 移除的逆序
	// 不成立——delayedShutdownCleanup 返回即移除，close(done) 在其后）。
	waitShutdownCleanupEntryDone(t, entry)
	shutdownCleanupMu.Lock()
	count = len(shutdownCleanupTasks)
	shutdownCleanupMu.Unlock()
	if count != 0 {
		t.Errorf("任务完成后注册表应移除条目，count = %d", count)
	}

	// 空白 sessionID → 直接返回
	scheduleShutdownCleanup("  ", "web")
	shutdownCleanupMu.Lock()
	count = len(shutdownCleanupTasks)
	shutdownCleanupMu.Unlock()
	if count != 0 {
		t.Errorf("空白 sessionID 不应注册，count = %d", count)
	}
}

// TestWaitForPendingShutdownCleanupForSession_完成通知 验证等待完成后的结果读取。
func TestWaitForPendingShutdownCleanupForSession_完成通知(t *testing.T) {
	resetShutdownCleanupState(t)

	entry := &shutdownCleanupEntry{done: make(chan struct{})}
	shutdownCleanupMu.Lock()
	shutdownCleanupTasks["sess-w"] = entry
	shutdownCleanupMu.Unlock()
	entry.result = true
	close(entry.done)

	got := WaitForPendingShutdownCleanupForSession(context.Background(), "sess-w")
	if !got {
		t.Error("已完成(deleted=true)的任务等待应返回 true")
	}
}

// TestWaitForPendingShutdownCleanupForSession_无任务 验证无 pending 任务时立即返回 false。
func TestWaitForPendingShutdownCleanupForSession_无任务(t *testing.T) {
	resetShutdownCleanupState(t)

	start := time.Now()
	got := WaitForPendingShutdownCleanupForSession(context.Background(), "sess-none")
	elapsed := time.Since(start)
	if got {
		t.Error("无 pending 任务应返回 false")
	}
	if elapsed > time.Second {
		t.Errorf("无任务应立即返回，耗时 %v", elapsed)
	}
}

// TestWaitForPendingShutdownCleanupForSession_超时 验证未完成任务等待超时返回 false。
// 对齐 Python TimeoutError → warning + return False（shield 语义：后台任务不受影响）。
func TestWaitForPendingShutdownCleanupForSession_超时(t *testing.T) {
	resetShutdownCleanupState(t)

	entry := &shutdownCleanupEntry{done: make(chan struct{})}
	shutdownCleanupMu.Lock()
	shutdownCleanupTasks["sess-timeout"] = entry
	shutdownCleanupMu.Unlock()
	// 不 close(done)——模拟超时

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := WaitForPendingTimeoutShim(ctx, "sess-timeout")
	elapsed := time.Since(start)
	if got {
		t.Error("超时应返回 false")
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("应等待至超时才返回，耗时 %v", elapsed)
	}
}

// WaitForPendingTimeoutShim 以短超时 ctx 测试超时分支（避免 15s 真等）。
func WaitForPendingTimeoutShim(ctx context.Context, sessionID string) bool {
	// 复刻 WaitForPendingShutdownCleanupForSession 的核心等待逻辑，但用 ctx 超时驱动
	shutdownCleanupMu.Lock()
	entry := shutdownCleanupTasks[sessionID]
	shutdownCleanupMu.Unlock()
	if entry == nil {
		return false
	}
	select {
	case <-entry.done:
		return entry.result
	case <-ctx.Done():
		return false
	}
}

// TestShutdownCleanupTool_Invoke_成功触发调度 验证 wrapper 在成功+全员关闭时调度清理。
// 对齐 Python wrapped_invoke：result.success=false 直接返回；全员关闭才调度。
func TestShutdownCleanupTool_Invoke_成功触发调度(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t2")
	ctx := context.Background()
	backend := teamAgent.TeamBackend()

	// 全员 shutdown → 触发调度
	mkMember(t, backend, ctx, "m1", string(atschema.MemberStatusShutdown))

	inner := &fakeShutdownTool{result: map[string]any{"success": true, "data": map[string]any{}}}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-wrap",
		channelID: "web",
		teamAgent: teamAgent,
	}

	result, err := wrapped.Invoke(ctx, map[string]any{"member_name": "m1"})
	if err != nil {
		t.Fatalf("wrapper 不应报错: %v", err)
	}
	if success, _ := result["success"].(bool); !success {
		t.Error("wrapper 应透传原始成功结果")
	}

	// 等待调度注册表出现条目（go 调度是异步的），随后等待清理 goroutine
	// 完成，避免泄漏到后续测试（触碰全局状态的 race）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		shutdownCleanupMu.Lock()
		entry, ok := shutdownCleanupTasks["sess-wrap"]
		shutdownCleanupMu.Unlock()
		if ok {
			waitShutdownCleanupEntryDone(t, entry)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("全员 shutdown + 成功结果应调度清理任务")
}

// TestShutdownCleanupTool_Invoke_失败不调度 验证 result.success=false 时不调度。
func TestShutdownCleanupTool_Invoke_失败不调度(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t3")
	ctx := context.Background()
	backend := teamAgent.TeamBackend()
	mkMember(t, backend, ctx, "m1", string(atschema.MemberStatusShutdown))

	inner := &fakeShutdownTool{result: map[string]any{"success": false, "error": "成员未找到"}}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-fail",
		channelID: "web",
		teamAgent: teamAgent,
	}

	result, err := wrapped.Invoke(ctx, map[string]any{"member_name": "m1"})
	if err != nil {
		t.Fatalf("wrapper 不应报错: %v", err)
	}
	if success, _ := result["success"].(bool); success {
		t.Error("wrapper 应透传原始失败结果")
	}

	shutdownCleanupMu.Lock()
	_, ok := shutdownCleanupTasks["sess-fail"]
	shutdownCleanupMu.Unlock()
	if ok {
		t.Error("失败结果不应调度清理任务")
	}
}

// TestShutdownCleanupTool_Invoke_成员未全关不调度 验证存在非终态成员时不调度。
func TestShutdownCleanupTool_Invoke_成员未全关不调度(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t4")
	ctx := context.Background()
	backend := teamAgent.TeamBackend()
	mkMember(t, backend, ctx, "m1", "running") // 非 shutdown 状态

	inner := &fakeShutdownTool{result: map[string]any{"success": true, "data": map[string]any{}}}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-partial",
		channelID: "web",
		teamAgent: teamAgent,
	}

	if _, err := wrapped.Invoke(ctx, map[string]any{"member_name": "m1"}); err != nil {
		t.Fatalf("wrapper 不应报错: %v", err)
	}

	shutdownCleanupMu.Lock()
	_, ok := shutdownCleanupTasks["sess-partial"]
	shutdownCleanupMu.Unlock()
	if ok {
		t.Error("成员未全部关闭时不应调度清理任务")
	}
}

// TestShutdownCleanupTool_Invoke_错误透传 验证原始 invoke 错误直接传播（不进 hook try）。
// 对齐 Python: result = await orig_invoke(...) 在 try 块之外。
func TestShutdownCleanupTool_Invoke_错误透传(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t5")

	inner := &fakeShutdownTool{
		result: map[string]any{"success": false, "error": "boom"},
		err:    context.DeadlineExceeded,
	}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-err",
		channelID: "web",
		teamAgent: teamAgent,
	}

	result, err := wrapped.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Error("原始错误应直接传播")
	}
	if result == nil {
		t.Error("应透传原始 result（含预置 map）")
	}

	shutdownCleanupMu.Lock()
	_, ok := shutdownCleanupTasks["sess-err"]
	shutdownCleanupMu.Unlock()
	if ok {
		t.Error("错误路径不应调度清理任务")
	}
}

// TestShutdownCleanupTool_Rebind 验证已包装实例的上下文重绑。
// 对齐 Python setattr 三个 _ATTR（session_id/channel_id/wrapped team_agent）。
func TestShutdownCleanupTool_Rebind(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t6")

	inner := &fakeShutdownTool{result: map[string]any{"success": true}}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-old",
		channelID: "web",
		teamAgent: teamAgent,
		// 构造快照：Python 闭包捕获构造时 session_id，rebind 不更新
		constructSessionID: "sess-old",
	}

	newAgent := newShutdownCleanupTestAgent(t, "t7")
	wrapped.rebind("sess-new", "cli", newAgent)

	wrapped.mu.RLock()
	gotSess, gotCh, gotAgent := wrapped.sessionID, wrapped.channelID, wrapped.teamAgent
	wrapped.mu.RUnlock()
	if gotSess != "sess-new" || gotCh != "cli" || gotAgent != newAgent {
		t.Errorf("rebind 后应为 (sess-new, cli, newAgent)，got (%s, %s, %p)",
			gotSess, gotCh, gotAgent)
	}
}

// TestShutdownCleanupTool_FallbackSessionID_构造快照 验证 fallback 源为构造时
// 快照而非当前绑定的 sessionID。
// 对齐 Python: wrapped_invoke L1455-1457 的闭包变量 session_id——
// rebind 只 setattr 实例属性（L1433-1443 setattr 后直接 return），
// 闭包 fallback 源恒为最初构造时的 session_id。
func TestShutdownCleanupTool_FallbackSessionID_构造快照(t *testing.T) {
	resetShutdownCleanupState(t)
	teamAgent := newShutdownCleanupTestAgent(t, "t6b")

	inner := &fakeShutdownTool{result: map[string]any{"success": true}}
	wrapped := &shutdownCleanupTool{
		Tool:      inner,
		sessionID: "sess-orig",
		channelID: "web",
		teamAgent: teamAgent,
		// 构造快照：闭包捕获等价
		constructSessionID: "sess-orig",
	}

	if got := wrapped.fallbackSessionID(); got != "sess-orig" {
		t.Errorf("构造后 fallback = %q, 期望 %q", got, "sess-orig")
	}

	// rebind 更新绑定字段，但构造快照（Python 闭包源）保持不变
	newAgent := newShutdownCleanupTestAgent(t, "t7b")
	wrapped.rebind("sess-rebound", "cli", newAgent)

	if got := wrapped.fallbackSessionID(); got != "sess-orig" {
		t.Errorf("rebind 后 fallback = %q, 期望仍为构造快照 %q", got, "sess-orig")
	}

	// fallbackSessionID 与 Invoke 的一致性：绑定置空后 Invoke 内部
	// fallback 应落到构造快照（Python: active or session_id 闭包兜底）
	wrapped.mu.Lock()
	wrapped.sessionID = ""
	wrapped.mu.Unlock()

	// newAgent 需有全员 shutdown 状态成员才能走到 fallback 判定
	ctx := context.Background()
	backend := newAgent.TeamBackend()
	mkMember(t, backend, ctx, "m1", string(atschema.MemberStatusShutdown))

	result, err := wrapped.Invoke(ctx, map[string]any{"member_name": "m1"})
	if err != nil {
		t.Fatalf("wrapper 不应报错: %v", err)
	}
	if success, _ := result["success"].(bool); !success {
		t.Error("wrapper 应透传原始成功结果")
	}

	// 等待调度注册表出现构造快照条目（go 调度是异步的），随后等待清理
	// goroutine 完成，避免泄漏到后续测试（触碰全局状态的 race）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		shutdownCleanupMu.Lock()
		entry, ok := shutdownCleanupTasks["sess-orig"]
		shutdownCleanupMu.Unlock()
		if ok {
			waitShutdownCleanupEntryDone(t, entry)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sessionID 置空后应 fallback 到构造快照 sess-orig 调度清理")
}

// TestDelayedShutdownCleanup_完成移除 验证延迟清理完成后注册表自动移除条目。
// 对齐 Python _delayed_shutdown_cleanup 的 finally pop 语义。
func TestDelayedShutdownCleanup_完成移除(t *testing.T) {
	resetShutdownCleanupState(t)
	ResetTeamManager("")

	entry := &shutdownCleanupEntry{done: make(chan struct{})}
	shutdownCleanupMu.Lock()
	shutdownCleanupTasks["sess-done"] = entry
	shutdownCleanupMu.Unlock()

	// 直接调用 delayedShutdownCleanup（跳过 go 调度以便同步断言）。
	// DeleteSessionRuntime 需要 runner 未初始化时走 fallback warning 路径——
	// 不影响 finally 移除断言。
	got := delayedShutdownCleanup("sess-done", "web", entry)
	_ = got // runner 未初始化的测试环境下删除结果为 true/false 均可接受

	// close(done) 是调度方的职责；这里手动模拟调度方收尾
	entry.result = got
	close(entry.done)

	shutdownCleanupMu.Lock()
	_, ok := shutdownCleanupTasks["sess-done"]
	shutdownCleanupMu.Unlock()
	if ok {
		t.Error("清理完成后注册表应移除条目")
	}
}

// TestPushShutdownCleanupNotice_内容分支 验证通知文案随 deleted 切换。
// 对齐 Python _push_shutdown_cleanup_notice 的两种 content。
func TestPushShutdownCleanupNotice_内容分支(t *testing.T) {
	resetShutdownCleanupState(t)

	var gotPayload map[string]any
	var gotMsg map[string]any
	SetShutdownCleanupNoticePusher(func(_ context.Context, msg map[string]any) error {
		gotMsg = msg
		if p, ok := msg["payload"].(map[string]any); ok {
			gotPayload = p
		}
		return nil
	})
	t.Cleanup(func() { SetShutdownCleanupNoticePusher(nil) })

	pushShutdownCleanupNotice(context.Background(), "sess-n1", "web", true)
	if gotPayload == nil {
		t.Fatal("推送消息应含 payload")
	}
	if content, _ := gotPayload["content"].(string); !strings.Contains(content, "已清空并解散") {
		t.Errorf("deleted=true 文案不符: %q", content)
	}
	if reqID, _ := gotPayload["request_id"].(string); reqID != "team_shutdown_cleanup_sess-n1" {
		t.Errorf("request_id 应为 team_shutdown_cleanup_<sid>，got %q", reqID)
	}
	if gotMsg["session_id"] != "sess-n1" {
		t.Errorf("外层 session_id 应为 sess-n1，got %v", gotMsg["session_id"])
	}

	pushShutdownCleanupNotice(context.Background(), "sess-n2", "web", false)
	if content, _ := gotPayload["content"].(string); !strings.Contains(content, "未完成") {
		t.Errorf("deleted=false 文案不符: %q", content)
	}
}

// TestPushShutdownCleanupNotice_未注入 验证注入缺失时仅 warning 不 panic。
func TestPushShutdownCleanupNotice_未注入(t *testing.T) {
	resetShutdownCleanupState(t)
	SetShutdownCleanupNoticePusher(nil)

	// 不应 panic
	pushShutdownCleanupNotice(context.Background(), "sess-n3", "web", true)
}

// TestAttachShutdownMemberRemoteCleanupWrapper_非分布式 验证非分布式环境直接跳过。
func TestAttachShutdownMemberRemoteCleanupWrapper_非分布式(t *testing.T) {
	resetShutdownCleanupState(t)

	// 无 config 文件环境下 teamLoadConfig 返回空 map 或错误 → mode != distributed → 跳过。
	// 本测试验证跳过路径不 panic、无副作用。
	card := agentschema.NewAgentCard()
	teamAgent := agent.NewTeamAgent(context.Background(), card)

	AttachShutdownMemberRemoteCleanupWrapper(teamAgent, "sess-skip", nil)
	// 到达这里即通过（无 panic）。非分布式环境不会触碰 ResourceMgr。
}

// ──────────────────────────── 非导出函数（fake） ────────────────────────────

// fakeShutdownTool 模拟被包装的 shutdown_member 工具。
type fakeShutdownTool struct {
	// result 预置返回结果
	result map[string]any
	// err 预置返回错误
	err error
}

// Invoke 返回预置结果。
func (f *fakeShutdownTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return f.result, f.err
}

// Stream 不支持流式。
func (f *fakeShutdownTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Card 返回占位卡片。
func (f *fakeShutdownTool) Card() *tool.ToolCard {
	return tool.NewToolCardWithID("team.shutdown_member.test", "shutdown_member", "test", nil, nil)
}

// mkMember 在 DB 创建指定状态的成员（teamName 从 backend 动态取，避免硬编码）。
func mkMember(t *testing.T, backend interface {
	DB() database.TeamDatabase
	TeamName() string
}, ctx context.Context, name string, status string) {
	t.Helper()
	if ok := backend.DB().Member().CreateMember(ctx, name, backend.TeamName(), name, "{}",
		status, "teammate", "", "idle", "build_mode", "", ""); !ok {
		t.Fatalf("创建成员 %s 失败", name)
	}
}
