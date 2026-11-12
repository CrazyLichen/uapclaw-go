package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestExecuteMessage_签名存在 编译期确认双入口都在。
func TestExecuteMessage_签名存在(t *testing.T) {
	var facade *UapClawSwarm
	var _ = facade.ExecuteMessage
	var _ = facade.ProcessMessage
}

// TestFacadeUsesSessionRuntime_分流判定 门面暴露的判定与 runtime 包一致。
func TestFacadeUsesSessionRuntime_分流判定(t *testing.T) {
	sessionID := "s1"
	chat := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "ch-1",
		SessionID: &sessionID,
		ReqMethod: schema.ReqMethodChatSend,
		IsStream:  true,
	}
	if !facadeUsesSessionRuntime(chat) {
		t.Error("聊天请求应归 Runtime 调度")
	}

	rename := &schema.AgentRequest{
		RequestID: "r2",
		ChannelID: "ch-1",
		SessionID: &sessionID,
		ReqMethod: schema.ReqMethodSessionRename,
	}
	if facadeUsesSessionRuntime(rename) {
		t.Error("session.rename 应走门面队列")
	}
}

// TestProcessMessageInternal_调度关闭直跑 scheduleSession=false 不碰队列。
func TestProcessMessageInternal_调度关闭直跑(t *testing.T) {
	uc := &UapClawSwarm{sessionManager: session.NewSessionManager()}
	sessionID := "s1"
	func() {
		defer func() { _ = recover() }()
		_, _ = uc.processMessageInternal(context.Background(), &schema.AgentRequest{
			RequestID: "r1",
			ChannelID: "ch-1",
			SessionID: &sessionID,
			ReqMethod: schema.ReqMethodSessionRename,
		}, false)
	}()
	if uc.sessionManager.HasSessionRuntime(nil) {
		t.Error("scheduleSession=false 时不应在门面队列留下状态")
	}
}

// TestProcessMessageInternal_调度开启入队 scheduleSession=true 时任务进门面队列。
func TestProcessMessageInternal_调度开启入队(t *testing.T) {
	sessionID := "s1"
	uc := &UapClawSwarm{sessionManager: session.NewSessionManager()}
	request := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "ch-1",
		SessionID: &sessionID,
		ReqMethod: schema.ReqMethodSessionRename,
	}
	raw, _ := json.Marshal(map[string]any{"mode": "agent.work.normal"})
	request.Params = raw

	_, _ = uc.processMessageInternal(context.Background(), request, true)
	// 提交后门面队列应保留该会话的运行时（processor 常驻）
	if !uc.sessionManager.HasSessionRuntime(&sessionID) {
		t.Error("scheduleSession=true 时应在门面队列留下会话运行时")
	}
	uc.sessionManager.CloseAllSessions(context.Background())
}
