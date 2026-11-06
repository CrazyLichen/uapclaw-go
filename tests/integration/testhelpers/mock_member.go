package testhelpers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MockMember 一个已装配（Configure + MockLLM）的真 TeamAgent。
//
// 对齐 Python tests/system_tests 里构造真 TeamAgent + MockLLMModel 的 fixture：
// 通过 AgentConfigurator.Configure 走完整装配链（SetupInfra + SetupAgent →
// BuildTeamHarness → buildDeepAgentFromSpec → llm.NewModel 取 MockLLM）。
type MockMember struct {
	// Agent 已装配的 TeamAgent
	Agent *agent.TeamAgent
	// ProviderName 注册到 ClientRegistry 的 MockLLM provider 名
	ProviderName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildMockMember 构造一个已装配 MockLLM 的真 TeamAgent。
//
// providerName 指向调用方已注册到 ClientRegistry 的 MockLLM provider
// （通常由 tests/integration/suite 的 RunnerSuite.SetupSuite 完成）。
// 本助手据此构造 TeamModelConfig 注入 DeepAgentSpec.Model，走完整 Configure
// 装配链（对齐 Python 真 TeamAgent + MockLLMModel 系统测试 fixture）。
func BuildMockMember(t testing.TB, providerName string, role atschema.TeamRole, memberName string) *MockMember {
	t.Helper()

	if providerName == "" {
		providerName = fmt.Sprintf("mock_member_%d", time.Now().UnixNano())
	}
	if role == "" {
		role = atschema.TeamRoleLeader
	}
	if memberName == "" {
		memberName = "member"
	}

	// 构造模型配置（ClientProvider 指向已注册的 MockLLM provider）
	modelCfg := models.TeamModelConfig{
		ModelClientConfig: llmschema.ModelClientConfig{
			ClientProvider: providerName,
			ClientID:       providerName + "_id",
			APIKey:         "mock-api-key",
		},
		ModelRequestConfig: &llmschema.ModelRequestConfig{ModelName: "mock-model"},
	}

	deepSpec := atschema.DeepAgentSpec{
		Model: &modelCfg,
		Card: agentschema.NewAgentCard(
			agentschema.WithAgentID(fmt.Sprintf("itest_%s_%d", memberName, time.Now().UnixNano())),
			agentschema.WithAgentName(memberName),
		),
		MaxIterations: 4,
	}

	spec := atschema.TeamAgentSpec{
		Agents: map[string]atschema.DeepAgentSpec{
			string(role): deepSpec,
		},
		TeamName:  "itest-team",
		Lifecycle: atschema.TeamLifecycleTemporary,
	}

	rtCtx := atschema.TeamRuntimeContext{
		Role:       role,
		MemberName: memberName,
	}

	ta := agent.NewTeamAgent(context.Background(), deepSpec.Card)
	err := ta.Configure(context.Background(), spec, rtCtx)
	require.NoError(t, err, "TeamAgent Configure 失败")

	return &MockMember{Agent: ta, ProviderName: providerName}
}
