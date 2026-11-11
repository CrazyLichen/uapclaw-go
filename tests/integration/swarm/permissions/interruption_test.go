//go:build integration

// 权限中断三场景集成测试（9.90e Task 10）。
//
// 覆盖三个跨子系统链路：
//  1. 审计-落盘-重放：权限决策 → EmitPermissionAudit → PersistentAuditWriter
//     落盘 JSONL → 重新读回校验（脱敏、字段完整性、追加语义）
//  2. 中断分流：RootPermissionQueue 建卡 → Reconcile 校准 → 投影层按
//     live/absent/invalid 三态分流 → 渲染卡片或撤销快照
//  3. 隔离恢复：损坏运行时 → QuarantineSession 双写 → 进程内/跨重启两路
//     拦截 → before_invoke 拒绝新工作
//
// 真实跨越的 internal 二级子系统（规则 7 要求 ≥3）：
//   - agentcore/harness（interrupt / rails：投影渲染、选项词表）
//   - agentcore/session（session state 读写、流事件写入）
//   - swarm/agents（permissions：队列状态机、审计落盘、隔离集合）
package permissions_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	streamrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	permrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptionSuite 权限中断三场景集成套件。
type InterruptionSuite struct {
	isuite.AgentSuite
}

// recordingSession 记录状态写入与流事件的可观测会话。
//
// 不用 ProxySession：这里需要断言 state 与流事件的具体内容，
// 而 ProxySession 需要底层 stub 才能工作。
type recordingSession struct {
	mu      sync.Mutex
	state   map[string]any
	written []stream.OutputSchema
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInterruptionSuite 运行套件。
func TestInterruptionSuite(t *testing.T) {
	suite.Run(t, new(InterruptionSuite))
}

// Test审计落盘重放 场景 1：决策 → 审计 → 落盘 → 读回。
//
// 跨子系统：swarm/agents（ToolDecisionFacts 构造 + EmitPermissionAudit +
// PersistentAuditWriter 落盘）+ agentcore/harness（脱敏边界）。
func (s *InterruptionSuite) Test审计落盘重放() {
	dataRoot := s.T().TempDir()

	facts := permrails.ToolDecisionFacts{
		Capability: permrails.ToolCapability{
			ToolName:        "bash",
			Category:        "shell",
			RiskTier:        "high",
			OperationFamily: "dynamic_execution",
		},
		UntrustedArgs:        map[string]any{"command": "rm -rf /tmp/x", "api_key": "sk-secret-123456"},
		ArgumentsValidObject: true,
		AccessesKnown:        true,
		Command:              "rm -rf /tmp/x",
		WorkspaceRoot:        "/workspace",
	}

	writer := permrails.NewPersistentAuditWriter(dataRoot)
	s.Require().NotNil(writer)

	result := permrails.EmitPermissionAudit(facts, permrails.AuditOptions{
		Decision:    "deny",
		Reason:      "forbidden_path",
		Degraded:    false,
		GrantID:     "grant-1",
		GrantReason: "manual",
	}, writer)
	s.Require().NotNil(result, "有 writer 时应返回落盘结果")
	s.Require().True(result.Persisted, "审计应成功落盘，原因: %s", result.Reason)

	// ── 重放：读回落盘的 JSONL ──
	records := readAuditJSONL(s.T(), result.Path)
	s.Require().NotEmpty(records, "审计文件应至少有一条记录")

	record := records[0]
	s.Require().Equal("deny", record["decision"], "决策应落盘")
	s.Require().Equal("forbidden_path", record["reason"], "原因码应落盘")

	// 脱敏边界：原始密钥绝不能出现在审计文件里
	raw, err := os.ReadFile(result.Path)
	s.Require().NoError(err)
	s.Require().NotContains(string(raw), "sk-secret-123456",
		"审计文件不得包含原始密钥（observe-only 脱敏边界）")

	// 追加语义：再写一条，文件应有两行且顺序保持
	facts.Capability.ToolName = "read_file"
	facts.Capability.Category = "path"
	second := permrails.EmitPermissionAudit(facts, permrails.AuditOptions{
		Decision: "allow",
		Reason:   "workspace_read",
	}, writer)
	s.Require().True(second.Persisted)

	records = readAuditJSONL(s.T(), result.Path)
	s.Require().Len(records, 2, "审计是追加式，不应覆盖前一条")
	s.Require().Equal("deny", records[0]["decision"], "第一条应保持 deny")
	s.Require().Equal("allow", records[1]["decision"], "第二条应为 allow")
}

// Test审计无writer不落盘 验证 nil writer 时只记日志不落盘。
func (s *InterruptionSuite) Test审计无writer不落盘() {
	facts := permrails.ToolDecisionFacts{
		Capability:    permrails.ToolCapability{ToolName: "bash", Category: "shell"},
		AccessesKnown: true,
	}
	result := permrails.EmitPermissionAudit(facts, permrails.AuditOptions{
		Decision: "allow",
		Reason:   "test",
	}, nil)
	s.Require().Nil(result, "nil writer 时不应返回落盘结果")
}

// Test中断分流live 场景 2a：队列命中 pending 卡 → 渲染权限卡片。
//
// 跨子系统：swarm/agents（RootPermissionQueue 建卡 + Reconcile）+
// agentcore/harness（BuildVerifiedPermissionAskUserQuestion 渲染）+
// agentcore/session（流事件写入）。
func (s *InterruptionSuite) Test中断分流live() {
	session := newRecordingSession()
	queue := permrails.NewRootPermissionQueue(nil)
	rail := streamrails.NewJiuClawStreamEventRail()
	rail.SetRootPermissionQueue(queue)

	// 建立会话映射，使 ResolveSID 能得到 "sess-1"
	cbc := s.newCBC(session, "sess-1", nil)
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc))

	// 队列建卡 → 状态推进到 pending
	card, err := queue.Begin("sess-1", "req-1", "exec-1", "tc-1", "bash")
	s.Require().NoError(err)

	// 构造与卡片匹配的中断交互
	wire := map[string]any{
		"version":              card.Key.Version,
		"invocation_id":        card.Key.InvocationID,
		"root_session_id":      card.Key.RootSessionID,
		"request_id":           card.Key.RequestID,
		"executor_kind":        card.Key.ExecutorKind,
		"execution_session_id": card.Key.ExecutionSessionID,
		"tool_call_id":         card.Key.ToolCallID,
	}
	interaction := map[string]any{
		"id": "tc-1",
		"value": map[string]any{
			"message":             "需要授权执行 bash",
			"tool_name":           "bash",
			"tool_invocation_key": wire,
			"metadata":            map[string]any{"risk_level": "HIGH"},
		},
	}
	cbc = s.newCBC(session, "sess-1", map[string]any{
		"result_type":   "interrupt",
		"state":         []any{interaction},
		"interrupt_ids": []any{"tc-1"},
	})
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc))

	// live 路径：沿真实队列校验 request_id 后渲染
	payload := interrupt.BuildVerifiedPermissionAskUserQuestion(
		interaction, card.Key.ToolCallID, card.Key.InvocationID)
	s.Require().NotNil(payload, "队列命中时应渲染出卡片载荷")
	s.Require().Equal("chat.ask_user_question", payload["event_type"])
	s.Require().Equal("tc-1", payload["request_id"])

	questions, ok := payload["questions"].([]any)
	s.Require().True(ok)
	s.Require().Len(questions, 1)

	question, ok := questions[0].(map[string]any)
	s.Require().True(ok)
	s.Require().Equal("权限审批: bash", question["header"], "标题应含工具名")
	s.Require().Equal(card.Key.InvocationID, question["card_id"], "卡片必须带队列身份")

	// 审查器元数据必须投影出来且经过白名单
	metadata, ok := question["reviewer_metadata"].(map[string]any)
	s.Require().True(ok, "权限类卡片应带 reviewer_metadata")
	s.Require().Equal("HIGH", metadata["risk_level"])
}

// Test中断分流invalid 场景 2b：request_id 与队列卡片不符 → 不渲染。
//
// 这是权限路径的核心保护：中断快照可能已过期，用户不能批准一个已失效的请求。
func (s *InterruptionSuite) Test中断分流invalid() {
	queue := permrails.NewRootPermissionQueue(nil)
	card, err := queue.Begin("sess-1", "req-1", "exec-1", "tc-1", "bash")
	s.Require().NoError(err)

	interaction := map[string]any{
		"id":    "tc-OTHER", // 与卡片 tool_call_id 不符
		"value": map[string]any{"message": "过期快照"},
	}
	payload := interrupt.BuildVerifiedPermissionAskUserQuestion(
		interaction, card.Key.ToolCallID, card.Key.InvocationID)
	s.Require().Nil(payload, "request_id 与卡片不符时不得渲染卡片")
}

// Test中断分流absent 场景 2c：交互无 tool_invocation_key → 队列定位为 absent。
//
// 跨子系统：swarm/agents（ToolInvocationLocatorFromInteraction 三态）+
// agentcore/harness（interrupt 判据）。
func (s *InterruptionSuite) Test中断分流absent() {
	queue := permrails.NewRootPermissionQueue(nil)

	// 无 key → absent
	state, card := permrails.ToolInvocationLocatorFromInteraction(
		map[string]any{"id": "tc-1"}, map[string]any{"message": "普通中断"}, queue)
	s.Require().Equal(permrails.ToolLocatorStateAbsent, state)
	s.Require().Nil(card)

	// 有 key 但卡片不存在 → invalid
	badState, badCard := permrails.ToolInvocationLocatorFromInteraction(
		map[string]any{
			"metadata": map[string]any{"tool_invocation_key": map[string]any{
				"version": 1, "invocation_id": "missing", "root_session_id": "sess-1",
				"request_id": "req-1", "executor_kind": "agent",
				"execution_session_id": "exec-1", "tool_call_id": "tc-1",
			}},
		}, map[string]any{}, queue)
	s.Require().Equal(permrails.ToolLocatorStateInvalid, badState,
		"卡片不在队列里应判 invalid")
	s.Require().Nil(badCard)
}

// Test隔离恢复内存 场景 3a：隔离后进程内立即拒绝新工作。
//
// 跨子系统：swarm/agents（QuarantineSession / RaiseIfQuarantined）+
// agentcore/session（state 双写）+ agentcore/harness（before_invoke 钩子）。
func (s *InterruptionSuite) Test隔离恢复内存() {
	session := newRecordingSession()
	rail := streamrails.NewJiuClawStreamEventRail()

	// 隔离前：正常放行
	cbc := s.newCBC(session, "conv-bad", nil)
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc),
		"隔离前应放行")

	// 隔离：写内存集合 + 写会话状态 + 终止
	rail.QuarantineSession("conv-bad", session)

	s.Require().Equal(true, session.get(interrupt.PermissionRuntimeQuarantinedKey),
		"隔离标记必须双写到会话状态（跨进程重启后仍生效）")

	// 隔离后：before_invoke 拒绝
	cbc = s.newCBC(session, "conv-bad", nil)
	err := rail.BeforeInvoke(context.Background(), cbc)
	s.Require().Error(err, "隔离后必须拒绝新工作")
	s.Require().Contains(err.Error(), "quarantined")
}

// Test隔离恢复重启 场景 3b：进程重启（内存集合清空）后仍从 state 恢复隔离。
//
// 这是双写的另一半价值：新进程的 rail 实例内存集合为空，
// 但会话状态里的标记还在，RaiseIfQuarantined 应据此继续拒绝。
func (s *InterruptionSuite) Test隔离恢复重启() {
	session := newRecordingSession()

	// 模拟上一个进程留下的隔离标记
	session.UpdateState(map[string]any{
		interrupt.PermissionRuntimeQuarantinedKey: true,
	})

	// 全新 rail 实例，内存集合为空
	freshRail := streamrails.NewJiuClawStreamEventRail()

	cbc := s.newCBC(session, "conv-old", nil)
	err := freshRail.BeforeInvoke(context.Background(), cbc)
	s.Require().Error(err, "重启后应从会话状态恢复隔离并拒绝")
	s.Require().Contains(err.Error(), "quarantined")

	// 命中后应回填内存集合，后续同会话检查不再依赖 state 读
	s.Require().Error(freshRail.RaiseIfQuarantined("conv-old", nil),
		"回填后即使不传 session 也应拒绝")
}

// Test隔离不影响其他会话 验证隔离是每会话独立。
func (s *InterruptionSuite) Test隔离不影响其他会话() {
	badSession := newRecordingSession()
	goodSession := newRecordingSession()
	rail := streamrails.NewJiuClawStreamEventRail()

	rail.QuarantineSession("conv-bad", badSession)

	s.Require().Error(rail.BeforeInvoke(context.Background(),
		s.newCBC(badSession, "conv-bad", nil)), "被隔离会话应拒绝")
	s.Require().NoError(rail.BeforeInvoke(context.Background(),
		s.newCBC(goodSession, "conv-good", nil)), "其他会话不应受影响")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCBC 构造带会话与结果的回调上下文。
func (s *InterruptionSuite) newCBC(
	session *recordingSession,
	conversationID string,
	result map[string]any,
) *agentinterfaces.AgentCallbackContext {
	inputs := &agentinterfaces.InvokeInputs{ConversationID: conversationID, Result: result}
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, session)
}

// readAuditJSONL 读回审计文件的所有记录。
func readAuditJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开审计文件失败: %v", err)
	}
	defer file.Close()

	var records []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("解析审计行失败 %q: %v", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("读审计文件失败: %v", err)
	}
	return records
}

// ──────────────────────────── 测试替身 ────────────────────────────

// newRecordingSession 构造可观测会话。
func newRecordingSession() *recordingSession {
	return &recordingSession{state: make(map[string]any)}
}

// GetSessionID 实现 SessionFacade
func (r *recordingSession) GetSessionID() string { return "recording-session" }

// GetCacheIdentity 实现 SessionFacade
func (r *recordingSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 实现 SessionFacade
func (r *recordingSession) GetParentSessionID() string { return "" }

// UpdateState 实现 SessionFacade
func (r *recordingSession) UpdateState(data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range data {
		r.state[key] = value
	}
}

// GetState 实现 SessionFacade
func (r *recordingSession) GetState(key state.StateKey) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key.IsAll() {
		return r.state, nil
	}
	return r.state[key.String()], nil
}

// DumpState 实现 SessionFacade
func (r *recordingSession) DumpState() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[string]any, len(r.state))
	for key, value := range r.state {
		copied[key] = value
	}
	return copied
}

// WriteStream 实现 SessionFacade
func (r *recordingSession) WriteStream(_ context.Context, data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch typed := data.(type) {
	case stream.OutputSchema:
		r.written = append(r.written, typed)
	case *stream.OutputSchema:
		r.written = append(r.written, *typed)
	}
	return nil
}

// WriteCustomStream 实现 SessionFacade
func (r *recordingSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 实现 SessionFacade
func (r *recordingSession) GetEnv(_ string, _ ...any) any { return nil }

// Interact 实现 SessionFacade
func (r *recordingSession) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 实现 SessionFacade
func (r *recordingSession) ClearSession(_ context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = make(map[string]any)
	r.written = nil
}

// get 读一个状态值。
func (r *recordingSession) get(key string) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state[key]
}
