//go:build integration

package adapter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAdapterEvolutionSuite 覆盖适配器层的技能自演进入口：
//
//	react.evolution.enabled=true → /evolve 懒加载 SkillEvolutionRail
//	→ 生成待审批经验（approval_chunks）
//	→ HandleUserAnswer 按 skill_evolve_ 前缀路由审批
//	→ 落盘 SKILL.md 演进索引与 evolutions.json
//
// 现有测试只在 rail 层验证同样的能力，从未从适配器入口走通，
// 因此「配置开关 → 懒加载 → 审批回包 → 落盘」这条链路是零覆盖。
type DeepAdapterEvolutionSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
	// skillName 测试技能名
	skillName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepAdapterEvolutionSuite 运行适配器层演化集成测试。
func TestDeepAdapterEvolutionSuite(t *testing.T) {
	suite.Run(t, new(DeepAdapterEvolutionSuite))
}

// TestEvolveSlash_审批后落盘 /evolve 生成待审批经验，审批回包后写入磁盘。
func (s *DeepAdapterEvolutionSuite) TestEvolveSlash_审批后落盘() {
	ctx := s.Ctx
	d := s.newAdapterWithEvolution(true)

	// 演化 rail 的 LLM 调用：返回一条经验记录
	s.MockLLM.SetResponses(s.recordsResponse())

	req := s.newChatRequest("/evolve " + s.skillName + " 把权限失败的教训记下来")
	resp, err := d.ProcessMessageImpl(ctx, req, nil)
	s.Require().NoError(err, "ProcessMessageImpl 不应返回错误")
	s.Require().NotNil(resp)

	requestID := s.approvalRequestID(resp)
	s.Require().NotEmpty(requestID, "应返回待审批 request_id，payload=%v", resp.Payload)
	s.True(strings.HasPrefix(requestID, "skill_evolve_"), "request_id 前缀应为 skill_evolve_，实际 %s", requestID)

	// 审批前：磁盘无演进产物
	s.NotContains(s.readSkillMD(), "<!-- evolution-index-start -->", "审批前不应写回 SKILL.md")

	// 审批回包（接收）
	answerReq := schema.NewAgentRequest(
		"req-answer", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{
			"request_id": requestID,
			"answers":    []any{map[string]any{"selected_options": []any{"接收"}}},
		}),
		schema.WithAgentSessionID("itest-evolve"),
	)
	answerResp, err := d.HandleUserAnswer(ctx, answerReq)
	s.Require().NoError(err)
	s.Require().NotNil(answerResp)
	s.True(answerResp.Payload["resolved"] == true, "审批应被路由并处理，payload=%v", answerResp.Payload)

	// 审批后：落盘
	skillMD := s.readSkillMD()
	s.Contains(skillMD, "<!-- evolution-index-start -->", "审批后 SKILL.md 应写入演进索引")
	s.Contains(skillMD, "Evolution Experiences", "索引应包含演进经验标题")
	s.Contains(skillMD, "# Data Processor Skill", "原有正文应保留")
	s.FileExists(filepath.Join(s.env.DataDir, "agent", "workspace", "skills", s.skillName, "evolutions.json"))
}

// TestEvolveSlash_未启用时拒绝 /evolve 在 evolution.enabled=false 时不可用，
// 且不应写盘。
func (s *DeepAdapterEvolutionSuite) TestEvolveSlash_未启用时拒绝() {
	ctx := s.Ctx
	d := s.newAdapterWithEvolution(false)

	req := s.newChatRequest("/evolve " + s.skillName + " 记录经验")
	resp, err := d.ProcessMessageImpl(ctx, req, nil)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// 未启用演进：返回错误提示而非审批
	_, hasApproval := resp.Payload["approval_chunks"]
	s.False(hasApproval, "未启用时不应返回审批块，payload=%v", resp.Payload)
	s.NotContains(s.readSkillMD(), "<!-- evolution-index-start -->", "未启用时不应写盘")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每测试隔离环境 + 预置技能。
func (s *DeepAdapterEvolutionSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_deep_evolve_itest")
	s.skillName = "data-processor"

	skillDir := filepath.Join(s.env.DataDir, "agent", "workspace", "skills", s.skillName)
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	skillMD := "---\nname: data-processor\ndescription: 处理 CSV 与 JSON 数据文件\n---\n" +
		"# Data Processor Skill\n\n读取并处理数据文件。\n"
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o600))
}

// newAdapterWithEvolution 写入含 evolution 段的配置并装配 DeepAdapter。
func (s *DeepAdapterEvolutionSuite) newAdapterWithEvolution(enabled bool) *adapter.DeepAdapter {
	reactExtra := fmt.Sprintf("  evolution:\n    enabled: %t\n    auto_scan: true\n    auto_save: false\n", enabled)
	s.env.WriteConfigYAML(s.T(), reactExtra)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// newChatRequest 构造一次聊天请求。
func (s *DeepAdapterEvolutionSuite) newChatRequest(query string) *schema.AgentRequest {
	return schema.NewAgentRequest(
		"req-chat", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": query, "mode": "agent.work.plan"}),
		schema.WithAgentSessionID("itest-evolve"),
	)
}

// recordsResponse 构造演化 LLM 的固定经验记录响应。
func (s *DeepAdapterEvolutionSuite) recordsResponse() mockllm.MockResponse {
	payload, err := json.Marshal([]map[string]any{
		{
			"action":  "append",
			"target":  "body",
			"section": "Troubleshooting",
			"content": "### 系统文件权限\n- 读取系统文件前先确认权限，必要时使用 sudo 提权",
		},
	})
	s.Require().NoError(err)
	return mockllm.CreateTextResponse(string(payload))
}

// approvalRequestID 从响应 payload 的 approval_chunks 中提取 request_id。
func (s *DeepAdapterEvolutionSuite) approvalRequestID(resp *schema.AgentResponse) string {
	chunks, ok := resp.Payload["approval_chunks"].([]map[string]any)
	if !ok {
		return ""
	}
	for _, chunk := range chunks {
		if v, ok := chunk["request_id"]; ok {
			return fmt.Sprintf("%v", v)
		}
		if inner, ok := chunk["payload"].(map[string]any); ok {
			if v, ok := inner["request_id"]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

// readSkillMD 读取测试技能的 SKILL.md。
func (s *DeepAdapterEvolutionSuite) readSkillMD() string {
	s.T().Helper()
	data, err := os.ReadFile(filepath.Join(s.env.DataDir, "agent", "workspace", "skills", s.skillName, "SKILL.md"))
	s.Require().NoError(err)
	return string(data)
}

// jsonParams 将 map 序列化为请求参数。
func (s *DeepAdapterEvolutionSuite) jsonParams(params map[string]any) json.RawMessage {
	data, err := json.Marshal(params)
	s.Require().NoError(err)
	return data
}
