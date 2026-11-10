package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// ownerRequest 构造带 channel 与 session 的请求。
func ownerRequest(channelID, sessionID string) *schema.AgentRequest {
	req := &schema.AgentRequest{ChannelID: channelID}
	if sessionID != "" {
		sid := sessionID
		req.SessionID = &sid
	}
	return req
}

// ownerExecution 构造指定 instructions 的执行体（改 instructions 即改指纹）。
func ownerExecution(t *testing.T, instructions string) *RuntimeAgentExecution {
	t.Helper()
	def, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: instructions, ToolsWildcard: true,
	})
	require.NoError(t, err)
	execution, err := NewRuntimeAgentExecution(def, RuntimeAgentModeWorkNormal)
	require.NoError(t, err)
	return execution
}

// ──────────────────────────── 键归一化 ────────────────────────────

// TestAgentExecutionOwnerKeyOf_归一化 channel 空/空白→default，大小写归一；session 空→无键。
// Python: service.py:1584-1591 _agent_execution_owner_key
func TestAgentExecutionOwnerKeyOf_归一化(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})

	cases := []struct {
		name     string
		channel  string
		session  string
		wantOK   bool
		wantChan string
		wantSess string
	}{
		{"channel空回default", "", "s1", true, "default", "s1"},
		{"channel空白回default", "   ", "s1", true, "default", "s1"},
		{"channel大小写归一", "  WEB  ", "s1", true, "web", "s1"},
		{"session去空白", "web", "  s1  ", true, "web", "s1"},
		{"session空无键", "web", "", false, "", ""},
		{"session仅空白无键", "web", "   ", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, ok := runtime.agentExecutionOwnerKeyOf(ownerRequest(c.channel, c.session))
			require.Equal(t, c.wantOK, ok)
			if c.wantOK {
				require.Equal(t, c.wantChan, key.ChannelID)
				require.Equal(t, c.wantSess, key.SessionID)
			}
		})
	}
}

// TestAgentExecutionOwnerKeyOf_nil请求 nil 请求视为无键。
// Python: :1586 request.session_id 访问前 request 必非 None
func TestAgentExecutionOwnerKeyOf_nil请求(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	_, ok := runtime.agentExecutionOwnerKeyOf(nil)
	require.False(t, ok)
}

// ──────────────────────────── 认领语义 ────────────────────────────

// TestClaimAgentExecutionOwner_首次写入 首次认领写入条目。
// Python: service.py:1618
func TestClaimAgentExecutionOwner_首次写入(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	req := ownerRequest("web", "s1")
	execution := ownerExecution(t, "v1")

	require.NoError(t, runtime.claimAgentExecutionOwner(req, execution))
	require.Same(t, execution, runtime.agentExecutionOwner(req))
}

// TestClaimAgentExecutionOwner_同定义幂等 相同指纹重复认领不报错且不覆盖。
// Python: service.py:1616-1617 if current is not None: return
func TestClaimAgentExecutionOwner_同定义幂等(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	req := ownerRequest("web", "s1")
	first := ownerExecution(t, "v1")
	require.NoError(t, runtime.claimAgentExecutionOwner(req, first))

	second := ownerExecution(t, "v1")
	require.NoError(t, runtime.claimAgentExecutionOwner(req, second))
	require.Same(t, first, runtime.agentExecutionOwner(req), "幂等不覆盖原条目")
}

// TestClaimAgentExecutionOwner_换定义冲突 指纹不同 → SESSION_CONFLICT（field=agent）。
// Python: service.py:1609-1615
func TestClaimAgentExecutionOwner_换定义冲突(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	req := ownerRequest("web", "s1")
	require.NoError(t, runtime.claimAgentExecutionOwner(req, ownerExecution(t, "v1")))

	err := runtime.claimAgentExecutionOwner(req, ownerExecution(t, "v2"))
	require.ErrorIs(t, err, ErrAgentExecutionSessionConflict)
	require.Equal(t, "agent", AgentDefinitionErrorField(err))
	require.Contains(t, err.Error(), "already bound to another Agent definition")
}

// TestClaimAgentExecutionOwner_无会话不建条目 空 session 直接返回不建键。
// Python: service.py:1598-1600 if key is None: return
func TestClaimAgentExecutionOwner_无会话不建条目(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", ""), ownerExecution(t, "v1")))
	require.Empty(t, runtime.agentExecutionOwners)
}

// TestClaimAgentExecutionOwner_nil执行体不panic nil 执行体安全返回。
func TestClaimAgentExecutionOwner_nil执行体不panic(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(ownerRequest("web", "s1"), nil))
	require.Empty(t, runtime.agentExecutionOwners)
}

// TestAgentExecutionOwner_不同渠道互不影响 同 session 不同 channel 是两条独立记录。
// Python: key = (channel_id, session_id) 二元组
func TestAgentExecutionOwner_不同渠道互不影响(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	web := ownerRequest("web", "s1")
	cli := ownerRequest("cli", "s1")

	require.NoError(t, runtime.claimAgentExecutionOwner(web, ownerExecution(t, "v1")))
	require.NoError(t, runtime.claimAgentExecutionOwner(cli, ownerExecution(t, "v2")))

	require.Equal(t, "v1", runtime.agentExecutionOwner(web).Definition.Instructions)
	require.Equal(t, "v2", runtime.agentExecutionOwner(cli).Definition.Instructions)
}

// TestAgentExecutionOwner_查不到返回nil 未绑定会话返回 nil。
// Python: service.py:1625 .get(key)
func TestAgentExecutionOwner_查不到返回nil(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.Nil(t, runtime.agentExecutionOwner(ownerRequest("web", "never")))
}

// ──────────────────────────── 解绑语义 ────────────────────────────

// TestForgetAgentExecutionOwner_按会话删除 删除后查不到。
// Python: service.py:1627-1640
func TestForgetAgentExecutionOwner_按会话删除(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	req := ownerRequest("web", "s1")
	require.NoError(t, runtime.claimAgentExecutionOwner(req, ownerExecution(t, "v1")))

	runtime.forgetAgentExecutionOwner("web", "s1")
	require.Nil(t, runtime.agentExecutionOwner(req))
}

// TestForgetAgentExecutionOwner_独立构造键 该方法不复用 request 版键逻辑，直接传参数。
// Python: service.py:1635-1638 key = (归一化 channel, 去空白 session)
func TestForgetAgentExecutionOwner_独立构造键(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), ownerExecution(t, "v1")))

	// 大小写与空白都要归一后才命中
	runtime.forgetAgentExecutionOwner("  WEB  ", "  s1  ")
	require.Nil(t, runtime.agentExecutionOwner(ownerRequest("web", "s1")))
}

// TestForgetAgentExecutionOwner_空channel回default 空白 channel 归一为 default。
// Python: service.py:1636-1637 str(channel_id or "default").strip().lower() or "default"
func TestForgetAgentExecutionOwner_空channel回default(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("", "s1"), ownerExecution(t, "v1")))

	runtime.forgetAgentExecutionOwner("", "s1")
	require.Nil(t, runtime.agentExecutionOwner(ownerRequest("default", "s1")))
}

// TestForgetAgentExecutionOwner_空会话直接返回 session 为空时不操作。
// Python: service.py:1638-1639 if not key[1]: return
func TestForgetAgentExecutionOwner_空会话直接返回(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), ownerExecution(t, "v1")))

	runtime.forgetAgentExecutionOwner("web", "   ")
	require.NotNil(t, runtime.agentExecutionOwner(ownerRequest("web", "s1")), "不应误删")
}

// TestForgetAgentExecutionOwner_不存在键无副作用 删不存在的键安全。
func TestForgetAgentExecutionOwner_不存在键无副作用(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	runtime.forgetAgentExecutionOwner("web", "never-existed")
	require.Empty(t, runtime.agentExecutionOwners)
}

// ──────────────────────────── Close 清表 ────────────────────────────

// TestClose_清空owner表 关闭后整表清空。
// Python: service.py:3545-3546
func TestClose_清空owner表(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), ownerExecution(t, "v1")))
	require.Len(t, runtime.agentExecutionOwners, 1)

	require.NoError(t, runtime.Close())
	require.Empty(t, runtime.agentExecutionOwners)
}

// TestClose_幂等 重复关闭不报错。
func TestClose_幂等(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.Close())
	require.NoError(t, runtime.Close())
}

// TestClose_清表后可重新认领 关闭后 map 仍可写入（不被置 nil）。
func TestClose_清表后可重新认领(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.Close())
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s2"), ownerExecution(t, "v1")))
	require.NotNil(t, runtime.agentExecutionOwner(ownerRequest("web", "s2")))
}

// ──────────────────────────── 并发安全 ────────────────────────────

// TestOwner表_并发读写不panic 多协程并发认领/查询/解绑不触发 map 竞态。
// spec D-4：Python 单线程无锁，Go 必须加锁。
func TestOwner表_并发读写不panic(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	execution := ownerExecution(t, "v1")

	done := make(chan struct{})
	for index := 0; index < 8; index++ {
		go func(offset int) {
			defer func() { done <- struct{}{} }()
			session := "s" + string(rune('0'+offset))
			for round := 0; round < 200; round++ {
				req := ownerRequest("web", session)
				_ = runtime.claimAgentExecutionOwner(req, execution)
				_ = runtime.agentExecutionOwner(req)
				runtime.hasAgentExecutionOwner("web", session)
				if round%2 == 0 {
					runtime.forgetAgentExecutionOwner("web", session)
				}
			}
		}(index)
	}
	for index := 0; index < 8; index++ {
		<-done
	}
}
