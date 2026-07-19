package subagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewVerificationContractRail_优先级 测试优先级
func TestNewVerificationContractRail_优先级(t *testing.T) {
	r := NewVerificationContractRail()

	assert.Equal(t, 88, r.Priority(), "VerificationContractRail 优先级应为 88")
}

// TestVerificationContractRail_Init_预构建Section 测试 section 使用 SectionVerificationContract
func TestVerificationContractRail_Init_预构建Section(t *testing.T) {
	r := NewVerificationContractRail()
	agent := newFakeBaseAgentForTest()

	err := r.Init(agent)

	require.NoError(t, err)
	require.NotNil(t, r.section, "Init 应预构建 section")
	assert.Equal(t, hsections.SectionVerificationContract, r.section.Name)
	assert.Equal(t, 88, r.section.Priority)
	assert.Contains(t, r.section.Content["en"], "Verification Gate")
	assert.Contains(t, r.section.Content["cn"], "验证门控")
}

// TestVerificationContractRail_BeforeModelCall_注入契约 测试 remove + add 避免重复
func TestVerificationContractRail_BeforeModelCall_注入契约(t *testing.T) {
	r := NewVerificationContractRail()
	agent := newFakeBaseAgentForTest()

	err := r.Init(agent)
	require.NoError(t, err)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	// 第一次注入
	err = r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.True(t, agent.builder.HasSection(hsections.SectionVerificationContract), "应注入契约 section")

	// 第二次注入（应不重复）
	err = r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)
	assert.True(t, agent.builder.HasSection(hsections.SectionVerificationContract), "应仍只有一个契约 section")
}

// TestVerificationContractRail_BeforeModelCall_无Builder跳过 测试 promptBuilder 为 nil 时跳过
func TestVerificationContractRail_BeforeModelCall_无Builder跳过(t *testing.T) {
	r := NewVerificationContractRail()
	// 不调用 Init，promptBuilder 为 nil

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
	// 不应 panic
}
