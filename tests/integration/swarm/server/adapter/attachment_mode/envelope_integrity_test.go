//go:build integration

package attachment_mode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EnvelopeIntegritySuite 断言 plan 首轮的用户消息信封完整（IT-S3-1）。
//
// 背景（S3 缺陷 #1/#2）：plan 提醒改写曾把 BuildUserPrompt 生成的 JSON 信封整段
// 覆盖成裸原文，导致 preferred_response_language/source/timestamp 等字段丢失。
type EnvelopeIntegritySuite struct {
	isuite.BaseIntegrationSuite
	// env 计划模式测试环境
	env *testhelpers.PlanEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEnvelopeIntegritySuite 运行 EnvelopeIntegritySuite。
func TestEnvelopeIntegritySuite(t *testing.T) { suite.Run(t, new(EnvelopeIntegritySuite)) }

// TestIT_S3_1_信封完整性 进 plan 首轮 user 消息同时含原文与信封字段。
//
// 跨子系统：runtime（query 改写）+ adapter（BuildInputs）+ context_engine。
func (s *EnvelopeIntegritySuite) TestIT_S3_1_信封完整性() {
	s.env.Send(itSession, planMode, "unique-marker 请规划", map[string]any{
		"plan_entry_source": "plan_toggle",
	})

	envelope := map[string]any{}
	for _, msg := range s.env.LastMessages() {
		if content, ok := parseUserEnvelope(msg.GetContent().Text()); ok {
			envelope = content
		}
	}
	s.Require().NotEmpty(envelope, "模型窗口里必须有 JSON 信封")

	content, _ := envelope["content"].(string)
	s.Contains(content, "unique-marker", "信封 content 必须保留用户原文")
	s.Contains(content, "<system-reminder>", "信封 content 必须含 plan 约束提醒")
	s.Equal("cn", envelope["preferred_response_language"], "信封语言字段不得丢失")
	s.NotEmpty(envelope["source"], "信封 source 字段不得丢失")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离环境。
func (s *EnvelopeIntegritySuite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "mock_s3_envelope")
}

// parseUserEnvelope 解析用户消息里的 JSON 信封（无信封返回 false）。
func parseUserEnvelope(text string) (map[string]any, bool) {
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, false
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text[start:]), &envelope); err != nil {
		return nil, false
	}
	return envelope, true
}
