package handoff

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHandoffOrchestrator_默认值 测试 NewHandoffOrchestrator 使用默认配置
func TestNewHandoffOrchestrator_默认值(t *testing.T) {
	agents := []string{"a", "b", "c"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	if coord.CurrentAgentID() != "a" {
		t.Errorf("期望 CurrentAgentID = a，实际 = %s", coord.CurrentAgentID())
	}
	if coord.HandoffCount() != 0 {
		t.Errorf("期望 HandoffCount = 0，实际 = %d", coord.HandoffCount())
	}
	if coord.maxHandoffs != defaultMaxHandoffs {
		t.Errorf("期望 maxHandoffs = %d，实际 = %d", defaultMaxHandoffs, coord.maxHandoffs)
	}
}

// TestNewHandoffOrchestrator_自定义配置 测试 NewHandoffOrchestrator 使用自定义配置
func TestNewHandoffOrchestrator_自定义配置(t *testing.T) {
	agents := []string{"a", "b", "c"}
	routes := []HandoffRoute{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	termCond := func(_ *HandoffOrchestrator) bool { return false }
	config := &HandoffConfig{
		MaxHandoffs:          5,
		Routes:               routes,
		TerminationCondition: termCond,
	}

	coord := NewHandoffOrchestrator("a", agents, config)

	if coord.maxHandoffs != 5 {
		t.Errorf("期望 maxHandoffs = 5，实际 = %d", coord.maxHandoffs)
	}
	if coord.terminationCondition == nil {
		t.Errorf("期望 terminationCondition 不为 nil")
	}
	if coord.CurrentAgentID() != "a" {
		t.Errorf("期望 CurrentAgentID = a，实际 = %s", coord.CurrentAgentID())
	}
}

// TestBuildRouteGraph_全互联 测试空路由时全互联
func TestBuildRouteGraph_全互联(t *testing.T) {
	agents := []string{"a", "b", "c"}
	graph := BuildRouteGraph(agents, nil)

	// 每个 Agent 可交接给其他 2 个
	for _, src := range agents {
		expected := 2
		actual := len(graph[src])
		if actual != expected {
			t.Errorf("期望 Agent %s 可交接给 %d 个 Agent，实际 = %d", src, expected, actual)
		}
	}

	// 验证不允许自己交接给自己
	for _, src := range agents {
		if _, ok := graph[src][src]; ok {
			t.Errorf("Agent %s 不应允许交接给自己", src)
		}
	}

	// 验证具体路由
	if _, ok := graph["a"]["b"]; !ok {
		t.Errorf("期望 a→b 路由存在")
	}
	if _, ok := graph["a"]["c"]; !ok {
		t.Errorf("期望 a→c 路由存在")
	}
	if _, ok := graph["b"]["a"]; !ok {
		t.Errorf("期望 b→a 路由存在")
	}
}

// TestBuildRouteGraph_显式路由 测试显式路由规则
func TestBuildRouteGraph_显式路由(t *testing.T) {
	agents := []string{"a", "b", "c"}
	routes := []HandoffRoute{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}
	graph := BuildRouteGraph(agents, routes)

	// a → b 允许
	if _, ok := graph["a"]["b"]; !ok {
		t.Errorf("期望 a→b 路由存在")
	}
	// a → c 不允许
	if _, ok := graph["a"]["c"]; ok {
		t.Errorf("期望 a→c 路由不存在")
	}
	// b → c 允许
	if _, ok := graph["b"]["c"]; !ok {
		t.Errorf("期望 b→c 路由存在")
	}
	// b → a 不允许
	if _, ok := graph["b"]["a"]; ok {
		t.Errorf("期望 b→a 路由不存在")
	}
	// c 无出边
	if len(graph["c"]) != 0 {
		t.Errorf("期望 c 无出边，实际 = %d", len(graph["c"]))
	}
}

// TestBuildRouteGraph_空Agent列表 测试空 Agent 列表
func TestBuildRouteGraph_空Agent列表(t *testing.T) {
	graph := BuildRouteGraph(nil, nil)
	if len(graph) != 0 {
		t.Errorf("期望空图，实际 = %d", len(graph))
	}
}

// TestBuildRouteGraph_路由中包含未注册Agent 测试路由中包含未在 agents 列表中的 Agent
func TestBuildRouteGraph_路由中包含未注册Agent(t *testing.T) {
	agents := []string{"a", "b"}
	routes := []HandoffRoute{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"}, // c 不在 agents 中
	}
	graph := BuildRouteGraph(agents, routes)

	// a → b 允许
	if _, ok := graph["a"]["b"]; !ok {
		t.Errorf("期望 a→b 路由存在")
	}
	// b → c 应被添加（c 不在 agents 中，但路由显式声明）
	if _, ok := graph["b"]["c"]; !ok {
		t.Errorf("期望 b→c 路由存在（显式路由）")
	}
}

// TestRequestHandoff_正常批准 测试正常交接请求
func TestRequestHandoff_正常批准(t *testing.T) {
	agents := []string{"a", "b", "c"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	approved := coord.RequestHandoff("b", "test reason")
	if !approved {
		t.Errorf("期望交接请求被批准")
	}
	if coord.HandoffCount() != 1 {
		t.Errorf("期望 HandoffCount = 1，实际 = %d", coord.HandoffCount())
	}
	if coord.CurrentAgentID() != "b" {
		t.Errorf("期望 CurrentAgentID = b，实际 = %s", coord.CurrentAgentID())
	}
}

// TestRequestHandoff_连续交接 测试连续多次交接
func TestRequestHandoff_连续交接(t *testing.T) {
	agents := []string{"a", "b", "c"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	// a → b
	if !coord.RequestHandoff("b", "") {
		t.Errorf("期望 a→b 交接被批准")
	}
	// b → c
	if !coord.RequestHandoff("c", "") {
		t.Errorf("期望 b→c 交接被批准")
	}
	if coord.HandoffCount() != 2 {
		t.Errorf("期望 HandoffCount = 2，实际 = %d", coord.HandoffCount())
	}
	if coord.CurrentAgentID() != "c" {
		t.Errorf("期望 CurrentAgentID = c，实际 = %s", coord.CurrentAgentID())
	}
}

// TestRequestHandoff_超过MaxHandoffs 测试超过最大交接次数
func TestRequestHandoff_超过MaxHandoffs(t *testing.T) {
	agents := []string{"a", "b"}
	config := &HandoffConfig{MaxHandoffs: 2}
	coord := NewHandoffOrchestrator("a", agents, config)

	// 第 1 次
	if !coord.RequestHandoff("b", "") {
		t.Errorf("期望第 1 次交接被批准")
	}
	// 第 2 次
	if !coord.RequestHandoff("a", "") {
		t.Errorf("期望第 2 次交接被批准")
	}
	// 第 3 次：超过 maxHandoffs=2
	if coord.RequestHandoff("b", "") {
		t.Errorf("期望第 3 次交接被拒绝")
	}
	if coord.HandoffCount() != 2 {
		t.Errorf("期望 HandoffCount = 2，实际 = %d", coord.HandoffCount())
	}
}

// TestRequestHandoff_路由不允许 测试路由不允许的交接
func TestRequestHandoff_路由不允许(t *testing.T) {
	agents := []string{"a", "b", "c"}
	routes := []HandoffRoute{
		{Source: "a", Target: "b"},
	}
	config := &HandoffConfig{Routes: routes}
	coord := NewHandoffOrchestrator("a", agents, config)

	// a → b 允许
	if !coord.RequestHandoff("b", "") {
		t.Errorf("期望 a→b 交接被批准")
	}
	// a → c 不允许
	if coord.RequestHandoff("c", "") {
		t.Errorf("期望 a→c 交接被拒绝")
	}
}

// TestRequestHandoff_终止条件满足 测试终止条件返回 true 时拒绝交接
func TestRequestHandoff_终止条件满足(t *testing.T) {
	agents := []string{"a", "b"}
	termCond := func(_ *HandoffOrchestrator) bool { return true }
	config := &HandoffConfig{
		TerminationCondition: termCond,
	}
	coord := NewHandoffOrchestrator("a", agents, config)

	if coord.RequestHandoff("b", "") {
		t.Errorf("期望终止条件满足时交接被拒绝")
	}
}

// TestRequestHandoff_终止条件不满足 测试终止条件返回 false 时允许交接
func TestRequestHandoff_终止条件不满足(t *testing.T) {
	agents := []string{"a", "b"}
	termCond := func(_ *HandoffOrchestrator) bool { return false }
	config := &HandoffConfig{
		TerminationCondition: termCond,
	}
	coord := NewHandoffOrchestrator("a", agents, config)

	if !coord.RequestHandoff("b", "") {
		t.Errorf("期望终止条件不满足时交接被批准")
	}
}

// TestRequestHandoff_源Agent不在路由图中 测试当前 Agent 不在路由图中
func TestRequestHandoff_源Agent不在路由图中(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("unknown", agents, nil)

	if coord.RequestHandoff("a", "") {
		t.Errorf("期望源 Agent 不在路由图中时交接被拒绝")
	}
}

// TestComplete_发送结果 测试 Complete 发送结果到 doneCh
func TestComplete_发送结果(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	result := map[string]any{"status": "done"}
	coord.Complete(result)

	select {
	case hr := <-coord.DoneCh():
		if hr.err != nil {
			t.Errorf("期望 err 为 nil，实际 = %v", hr.err)
		}
		if hr.result["status"] != "done" {
			t.Errorf("期望 result[status] = done，实际 = %v", hr.result["status"])
		}
	default:
		t.Errorf("期望 doneCh 中有结果")
	}
}

// TestComplete_多次调用只发送一次 测试 doneOnce 保证只发送一次
func TestComplete_多次调用只发送一次(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	result1 := map[string]any{"status": "first"}
	result2 := map[string]any{"status": "second"}

	coord.Complete(result1)
	coord.Complete(result2) // 第二次调用应被忽略

	// 只应收到第一次的结果
	hr := <-coord.DoneCh()
	if hr.err != nil {
		t.Errorf("期望 err 为 nil，实际 = %v", hr.err)
	}
	if hr.result["status"] != "first" {
		t.Errorf("期望收到第一次的结果，实际 = %v", hr.result["status"])
	}
}

// TestError_发送错误 测试 Error 发送错误到 doneCh
func TestError_发送错误(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	coord.Error(context.Canceled)

	select {
	case hr := <-coord.DoneCh():
		if hr.err == nil {
			t.Errorf("期望 err 不为 nil，实际为 nil")
		}
		if hr.result != nil {
			t.Errorf("期望 result 为 nil，实际 = %v", hr.result)
		}
	default:
		t.Errorf("期望 doneCh 中有错误信息")
	}
}

// TestDoneCh_返回只读通道 测试 DoneCh 返回只读通道
func TestDoneCh_返回只读通道(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	ch := coord.DoneCh()
	// 验证只能从通道读取（编译时检查：<-chan 类型）
	_ = ch
}

// TestClose_关闭通道 测试 Close 关闭 doneCh
func TestClose_关闭通道(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	coord.Close()

	// 关闭后从通道读取应立即返回零值
	val, ok := <-coord.DoneCh()
	if ok {
		t.Errorf("期望通道已关闭，但读取成功，val = %v", val)
	}
}

// TestClose_多次调用不panic 测试多次调用 Close 不会 panic
func TestClose_多次调用不panic(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	coord.Close()
	coord.Close() // 第二次调用不应 panic
}

// TestSaveToSession_和RestoreFromSession 测试状态持久化和恢复
func TestSaveToSession_和RestoreFromSession(t *testing.T) {
	// 创建真实的 AgentTeamSession
	sess := session.NewAgentTeamSession()

	agents := []string{"a", "b", "c"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	// 执行两次交接
	coord.RequestHandoff("b", "step 1")
	coord.RequestHandoff("c", "step 2")

	// 保存状态
	coord.SaveToSession(sess)

	// 验证状态已写入
	snapshot, err := sess.GetState(state.StringKey(CoordinatorStateKey))
	if err != nil {
		t.Fatalf("获取状态失败: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("期望状态快照不为 nil")
	}

	snapMap, ok := snapshot.(map[string]any)
	if !ok {
		t.Fatalf("期望状态快照类型为 map[string]any")
	}

	if snapMap["current_agent_id"] != "c" {
		t.Errorf("期望 current_agent_id = c，实际 = %v", snapMap["current_agent_id"])
	}

	// 恢复状态
	restored := RestoreFromSession(sess, "a", agents, nil)

	if restored.CurrentAgentID() != "c" {
		t.Errorf("期望恢复后 CurrentAgentID = c，实际 = %s", restored.CurrentAgentID())
	}
	if restored.HandoffCount() != 2 {
		t.Errorf("期望恢复后 HandoffCount = 2，实际 = %d", restored.HandoffCount())
	}
}

// TestRestoreFromSession_无快照 测试无状态快照时使用默认值
func TestRestoreFromSession_无快照(t *testing.T) {
	sess := session.NewAgentTeamSession()
	agents := []string{"a", "b", "c"}

	restored := RestoreFromSession(sess, "a", agents, nil)

	if restored.CurrentAgentID() != "a" {
		t.Errorf("期望恢复后 CurrentAgentID = a（默认值），实际 = %s", restored.CurrentAgentID())
	}
	if restored.HandoffCount() != 0 {
		t.Errorf("期望恢复后 HandoffCount = 0（默认值），实际 = %d", restored.HandoffCount())
	}
}

// TestHandoffCount_和CurrentAgentID_Getter 测试 getter 方法
func TestHandoffCount_和CurrentAgentID_Getter(t *testing.T) {
	agents := []string{"a", "b"}
	coord := NewHandoffOrchestrator("a", agents, nil)

	if coord.CurrentAgentID() != "a" {
		t.Errorf("期望 CurrentAgentID = a，实际 = %s", coord.CurrentAgentID())
	}
	if coord.HandoffCount() != 0 {
		t.Errorf("期望 HandoffCount = 0，实际 = %d", coord.HandoffCount())
	}

	coord.RequestHandoff("b", "")

	if coord.CurrentAgentID() != "b" {
		t.Errorf("期望 CurrentAgentID = b，实际 = %s", coord.CurrentAgentID())
	}
	if coord.HandoffCount() != 1 {
		t.Errorf("期望 HandoffCount = 1，实际 = %d", coord.HandoffCount())
	}
}

// TestRestoreFromSession_快照类型异常 测试快照类型不是 map[string]any 时使用默认值
func TestRestoreFromSession_快照类型异常(t *testing.T) {
	sess := session.NewAgentTeamSession()
	// 写入非法类型的快照
	sess.UpdateState(map[string]any{
		CoordinatorStateKey: "not_a_map",
	})
	agents := []string{"a", "b", "c"}

	restored := RestoreFromSession(sess, "a", agents, nil)

	// 应使用默认值
	if restored.CurrentAgentID() != "a" {
		t.Errorf("期望恢复后 CurrentAgentID = a（默认值），实际 = %s", restored.CurrentAgentID())
	}
	if restored.HandoffCount() != 0 {
		t.Errorf("期望恢复后 HandoffCount = 0（默认值），实际 = %d", restored.HandoffCount())
	}
}

// TestRestoreFromSession_handoffCount为int 测试 handoff_count 为 int 类型（非 float64）
func TestRestoreFromSession_handoffCount为int(t *testing.T) {
	sess := session.NewAgentTeamSession()
	sess.UpdateState(map[string]any{
		CoordinatorStateKey: map[string]any{
			"current_agent_id": "b",
			"handoff_count":    3,
		},
	})
	agents := []string{"a", "b", "c"}

	restored := RestoreFromSession(sess, "a", agents, nil)

	if restored.CurrentAgentID() != "b" {
		t.Errorf("期望恢复后 CurrentAgentID = b，实际 = %s", restored.CurrentAgentID())
	}
	if restored.HandoffCount() != 3 {
		t.Errorf("期望恢复后 HandoffCount = 3，实际 = %d", restored.HandoffCount())
	}
}

// TestRestoreFromSession_handoffCount为float64 测试 handoff_count 为 float64 类型（JSON 反序列化结果）
func TestRestoreFromSession_handoffCount为float64(t *testing.T) {
	sess := session.NewAgentTeamSession()
	sess.UpdateState(map[string]any{
		CoordinatorStateKey: map[string]any{
			"current_agent_id": "c",
			"handoff_count":    float64(5),
		},
	})
	agents := []string{"a", "b", "c"}

	restored := RestoreFromSession(sess, "a", agents, nil)

	if restored.CurrentAgentID() != "c" {
		t.Errorf("期望恢复后 CurrentAgentID = c，实际 = %s", restored.CurrentAgentID())
	}
	if restored.HandoffCount() != 5 {
		t.Errorf("期望恢复后 HandoffCount = 5，实际 = %d", restored.HandoffCount())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
