package resources_manager

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestAgentMgr_添加获取正常 测试 AddAgent → GetAgent 正常流程
func TestAgentMgr_添加获取正常(t *testing.T) {
	mgr := NewAgentMgr()

	mockAgent := &stubBaseAgent{}
	provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return mockAgent, nil
	}

	err := mgr.AddAgent("agent-1", provider)
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	agent, err := mgr.GetAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("GetAgent 失败: %v", err)
	}
	if agent == nil {
		t.Error("GetAgent 返回 nil，期望非 nil")
	}
}

// TestAgentMgr_重复注册报错 测试同 ID 二次注册报错
func TestAgentMgr_重复注册报错(t *testing.T) {
	mgr := NewAgentMgr()

	provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return &stubBaseAgent{}, nil
	}

	err := mgr.AddAgent("agent-1", provider)
	if err != nil {
		t.Fatalf("首次 AddAgent 失败: %v", err)
	}

	err = mgr.AddAgent("agent-1", provider)
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestAgentMgr_移除后获取报错 测试 RemoveAgent 后 GetAgent 报错
func TestAgentMgr_移除后获取报错(t *testing.T) {
	mgr := NewAgentMgr()

	provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return &stubBaseAgent{}, nil
	}

	err := mgr.AddAgent("agent-1", provider)
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	removed, err := mgr.RemoveAgent("agent-1")
	if err != nil {
		t.Fatalf("RemoveAgent 失败: %v", err)
	}
	if removed == nil {
		t.Error("RemoveAgent 应返回被注销的 provider")
	}

	_, err = mgr.GetAgent(context.Background(), "agent-1")
	if err == nil {
		t.Error("移除后 GetAgent 应返回错误")
	}
}

// TestAgentMgr_获取不存在返回错误 测试不存在的 agentID 返回错误
func TestAgentMgr_获取不存在返回错误(t *testing.T) {
	mgr := NewAgentMgr()

	_, err := mgr.GetAgent(context.Background(), "not-exist")
	if err == nil {
		t.Error("获取不存在的 Agent 应返回错误")
	}
}

// TestAgentMgr_空ID报错 测试空 agentID 添加报错
func TestAgentMgr_空ID报错(t *testing.T) {
	mgr := NewAgentMgr()

	provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return &stubBaseAgent{}, nil
	}

	err := mgr.AddAgent("", provider)
	if err == nil {
		t.Error("空 agentID 应返回错误")
	}
}

// TestAgentMgr_Provider返回错误 测试 provider 执行时返回错误
func TestAgentMgr_Provider返回错误(t *testing.T) {
	mgr := NewAgentMgr()

	provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return nil, errors.New("internal error")
	}

	err := mgr.AddAgent("err-agent", provider)
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	_, err = mgr.GetAgent(context.Background(), "err-agent")
	if err == nil {
		t.Error("provider 返回错误时 GetAgent 应传播错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
