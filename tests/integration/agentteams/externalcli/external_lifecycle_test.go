//go:build integration

package externalcli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalCliLifecycleSuite 外部 CLI 成员 adapter 路全生命周期集成套件。
//
// 跨子系统：spawn（externalcli.BuildCliRuntime）→ externalcli runtime +
// tracker（ReinvokeCliRuntime + TeamContextTracker）→ schema（TeamAgentSpec
// 校验器）→ session（AgentTeamSession 成员子会话）→ tools/database（InMemory
// 团队库名册探针）。子进程用 t.TempDir() 下假 CLI 脚本承担，绝不打真实 CLI。
type ExternalCliLifecycleSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExternalCliLifecycleSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestExternalCliLifecycleSuite(t *testing.T) {
	suite.Run(t, new(ExternalCliLifecycleSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestAdapterLifecycle_一轮消息与tracker折叠 adapter 路全生命周期：
// spec 校验过门 → BuildCliRuntime（openclaw one-shot + command_override 假脚本）
// → 发一轮消息读回输出 → tracker 折叠（首条含 pending 文本，commit 后第二条
// 不再携带）→ aclose 进程回收（Stop 终态 + 后续 Send 报错 + 输出通道关闭）。
//
// 对齐 spec §4 用例①；Python: build_cli_runtime (spawn.py:200-537) +
// CliRuntimeBase.send (runtime.py:297) + _build_team_context_tracker
// (external_cli_spawn.py:194-236)。
func (s *ExternalCliLifecycleSuite) TestAdapterLifecycle_一轮消息与tracker折叠() {
	ctx := s.Ctx
	// 假 CLI 探针：one-shot 进程把启动 argv 原样回显到 stdout 后退出
	//（prompt 走 --message argv，故外发消息文本可从输出断言）
	script := writeFakeCliScript(s.T(), "fake_openclaw.sh", `printf '%s\n' "$*"`)

	// schema：openclaw 条目过校验器门（子系统 3）
	spec := &schema.TeamAgentSpec{
		TeamName: "pay",
		ExternalCliAgents: []schema.ExternalCliAgentSpec{{
			CLIAgent: "openclaw",
			Command:  []string{script},
		}},
	}
	s.Require().NoError(spec.Validate(), "openclaw 条目必须过校验器")

	// 团队库 + 探针后端；预埋两条团队状态变化：团队行 + 同伴 alice
	db := database.NewInMemoryTeamDatabase()
	s.Require().NoError(db.Initialize(ctx))
	s.T().Cleanup(func() { _ = db.Close() })
	s.Require().True(db.Team().CreateTeam(ctx, "pay", "支付团队", "leader", "", "", "autonomous", false))
	memberDB := db.Member()
	s.Require().True(memberDB.CreateMember(ctx, "beta", "pay", "Beta", "{}", "ready", "teammate", "", "", "build_mode", "", ""))
	s.Require().True(memberDB.CreateMember(ctx, "alice", "pay", "Alice", "{}", "ready", "teammate", "", "", "build_mode", "", ""))
	backend := tools.NewTeamBackend("pay", "beta", false, db,
		messager.NewInProcessMessager(schema.MessagerTransportConfig{}))

	// spawn：BuildTeamContextTracker + BuildCliRuntime（子系统 1/2）
	rtc := &schema.TeamRuntimeContext{
		Role:        schema.TeamRoleExternalCLI,
		MemberName:  "beta",
		DisplayName: "Beta",
		Prompt:      "beta-私有约定",
		TeamSpec:    &schema.TeamSpec{TeamName: "pay", Language: "cn"},
		CLIAgent:    "openclaw",
	}
	state := &sessionctx.SessionState{}
	state.SetSessionID("sess-extcli-lifecycle")
	ctx = sessionctx.WithSessionState(ctx, state)
	tracker := externalcli.BuildTeamContextTracker(backend, &schema.TeamAgentSpec{}, rtc, "beta", "pay")
	s.Require().NotNil(tracker)

	rt, err := externalcli.BuildCliRuntime(ctx, rtc, externalcli.Options{
		CommandOverride:    []string{script},
		InjectMCP:          boolPtr(false),
		MemberAgentID:      "pay_beta",
		TeamContextTracker: tracker,
	})
	s.Require().NoError(err, "BuildCliRuntime 应成功")
	reinvoke, ok := rt.(*externalcli.ReinvokeCliRuntime)
	s.Require().True(ok, "openclaw 是 one-shot，应返回 ReinvokeCliRuntime，got %T", rt)

	// 成员子会话：真实团队会话派生（子系统 4），tracker 投递基线落在它上面
	teamSession := session.CreateAgentTeamSession("sess-extcli-lifecycle", nil, "pay")
	reinvoke.SetMemberSessionFactory(func() (sessioninterfaces.SessionFacade, error) {
		return teamSession.CreateAgentSession(nil, "pay_beta", false, "beta"), nil
	})
	s.Require().NoError(reinvoke.Start(nil))

	// 轮次事件订阅：收集 kind 供等待 finished
	roundKinds := make(chan string, 16)
	reinvoke.Subscribe(nil, func(kind string, _ int, _ string, _ *harness_protocol.TurnResult) {
		roundKinds <- kind
	})
	outputs := reinvoke.Outputs()

	// 第一轮：预埋的两条状态变化折叠为 pending 文本（<team-context> 块）
	s.Require().NoError(waitRoundKind(roundKinds, func() error {
		_, err := reinvoke.Send("第一轮任务", false)
		return err
	}, "finished"), "第一轮应正常收尾")
	first := drainOutputs(outputs, 2*time.Second)
	s.True(strings.Contains(first, "<team-context>"),
		"首条外发消息必须折叠 pending 团队状态文本: %q", first)
	s.True(strings.Contains(first, "第一轮任务"),
		"假 CLI 必须回显外发消息本体（--message argv）: %q", first)
	s.False(strings.Contains(first, "roster-change"),
		"首条应为全量名册快照而非增量: %q", first)

	// 第三条状态变化：新同伴 bob。第一轮 commit 已推进基线，第二轮只应携带
	// 名册增量（roster-change），不再重复 pending 的 <team-context> 文本
	s.Require().True(memberDB.CreateMember(ctx, "bob", "pay", "Bob", "{}", "ready", "teammate", "", "", "build_mode", "", ""))
	s.Require().NoError(waitRoundKind(roundKinds, func() error {
		_, err := reinvoke.Send("第二轮任务", false)
		return err
	}, "finished"), "第二轮应正常收尾")
	second := drainOutputs(outputs, 2*time.Second)
	s.True(strings.Contains(second, "第二轮任务"),
		"第二条应携带消息本体: %q", second)
	s.True(strings.Contains(second, `kind="roster-change"`) && strings.Contains(second, "bob"),
		"第二条应携带名册增量并点名 bob: %q", second)
	s.False(strings.Contains(second, "<team-context>"),
		"commit 后第二条不得再携带 pending 团队状态文本: %q", second)

	// aclose：Stop 终态 + 进程回收断言（one-shot 子进程逐轮已被 Wait 收编，
	// Stop 后相位 TERMINATED、输出通道关闭、Send 报执行错误）
	reinvoke.Stop()
	s.Equal("terminated", string(reinvoke.State()), "Stop 后相位必须 TERMINATED")
	_, err = reinvoke.Send("stop 之后", false)
	s.Require().Error(err, "Stop 后 Send 必须报错")
	s.True(strings.Contains(err.Error(), "already stopped"), "报文对齐 runtime.py:376: %v", err)
	for chunk := range outputs {
		_ = chunk
	}
	s.True(true, "输出通道已在 Stop 后关闭（range 终止）")
}

// ──────────────────────────── 非导出函数（本目录共享测试助手） ────────────────────────────

// boolPtr 布尔指针助手。
func boolPtr(v bool) *bool { return &v }

// Chunk 输出块的包内别名（stream.OutputSchema）。
type Chunk = stream.OutputSchema

// chunkText 从输出块提取叙述文本（cliRuntimeBase.makeChunk 的 payload 形状）。
func chunkText(chunk stream.OutputSchema) string {
	payload, _ := chunk.Payload.(map[string]any)
	if payload == nil {
		return ""
	}
	text, _ := payload["content"].(string)
	return text
}

// writeFakeCliScript 在 t.TempDir() 下写可执行假 CLI 脚本（测试禁真实 /tmp
// 与真实 CLI，进程边界即集成边界）。
func writeFakeCliScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake cli script: %v", err)
	}
	return path
}

// waitRoundKind 发起动作后等待指定轮次事件 kind（超时 30s）。
// Subscribe 回调在轮次 goroutine 触发；action 与等待串行化避免漏过 started。
func waitRoundKind(kinds chan string, action func() error, want string) error {
	if err := action(); err != nil {
		return err
	}
	deadline := time.After(30 * time.Second)
	for {
		select {
		case kind := <-kinds:
			if kind == want {
				return nil
			}
		case <-deadline:
			return errTimeout{want: want}
		}
	}
}

// errTimeout 等待轮次事件超时。
type errTimeout struct{ want string }

func (e errTimeout) Error() string { return "等待轮次事件超时: " + e.want }

// drainOutputs 在时限内排空当前可读输出块并拼接文本。
// 输出泵把队列缓冲逐块投递到通道；轮次 finished 后短暂等待即可拿全。
func drainOutputs(ch <-chan Chunk, wait time.Duration) string {
	var sb strings.Builder
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.WriteString(chunkText(chunk))
		case <-time.After(100 * time.Millisecond):
		}
	}
	return sb.String()
}
