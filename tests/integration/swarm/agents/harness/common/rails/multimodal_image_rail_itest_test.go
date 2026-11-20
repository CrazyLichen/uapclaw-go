//go:build integration

package rails

// 23.8 图片注入链全生命周期集成测试。
//
// 覆盖（用户硬要求的讨论点）：
//   - 带图轮：rail 安装 mutator → 变形器把图片注入最后一条 user 消息
//     （text {query,file} JSON 块在前 + image_url 块在后）
//   - 无图轮：注入 0 块、mutator 不安装
//   - 开关关闭：历史 image 块被剥光（IMAGE_CONTENT_OMITTED / 合并文本）
//   - 跨轮防串图：ctx 袋子每轮一换，第 N 轮读不到第 N-1 轮图片
//
// 链路：adapter 侧 ctx 袋子（prompt.WithMultimodalImageFiles）→ MultimodalImageRail
// （harness rail 体系）→ 窗口变形器（llm schema 消息面），跨 ≥3 个 internal 二级子系统。
// Python: tests/unit_tests/agents/harness/test_multimodal_image_rail.py 的链路级等价物。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
	rails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MultimodalImageRailSuite 图片注入链集成套件。
type MultimodalImageRailSuite struct {
	isuite.BaseIntegrationSuite
	tmpDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMultimodalImageRailSuite(t *testing.T) {
	suite.Run(t, new(MultimodalImageRailSuite))
}

// SetupTest 初始化临时目录。
func (s *MultimodalImageRailSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
}

// ──────────────────────────── 测试方法 ────────────────────────────

// writeImage 写一张测试图片并返回归一化记录。
func (s *MultimodalImageRailSuite) writeImage(name string) prompt.MultimodalImageFile {
	p := filepath.Join(s.tmpDir, name)
	s.Require().NoError(os.WriteFile(p, []byte("imgdata"), 0o644))
	return prompt.MultimodalImageFile{Type: "image", Filename: name, Path: p, MimeType: "image/png"}
}

// TestMultimodalImageRailSuite_带图轮全链 adapter ctx 袋子 → rail → mutator 注入。
func (s *MultimodalImageRailSuite) TestMultimodalImageRailSuite_带图轮全链() {
	img := s.writeImage("shot.png")
	// 1) adapter 入口：params 提取 + ctx 袋子
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "path": img.Path, "mime_type": img.MimeType, "filename": img.Filename},
	}}
	ctx := prompt.WithMultimodalImageFiles(context.Background(), prompt.ExtractMultimodalImageFiles(params))

	// 2) rail 开启路径
	rail := rails.NewMultimodalImageRail(boolPtr(true))
	rail.BeforeModelCall(ctx, &agentinterfaces.AgentCallbackContext{})
	mutator := rail.ImageMutator()
	s.Require().NotNil(mutator, "带图轮应安装 mutator")

	// 3) 窗口组装点执行变形器：最后一条 user 消息被注入
	messages := []llmschema.BaseMessage{
		llmschema.NewDefaultMessage(llmschema.RoleTypeSystem, "sys"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "看图"),
	}
	updated := mutator(messages)
	content := updated[1].GetContent()
	parts := content.Parts()
	s.Require().Len(parts, 2)

	// 4) 形状断言：{query,file} JSON 文本块 + image_url data URI
	var payload struct {
		Query string `json:"query"`
		File  []struct {
			Filename string `json:"filename"`
			Path     string `json:"path"`
			MimeType string `json:"mime_type"`
		} `json:"file"`
	}
	s.Require().NoError(json.Unmarshal([]byte(parts[0].Text), &payload))
	s.Equal("看图", payload.Query)
	s.Require().Len(payload.File, 1)
	s.Equal("shot.png", payload.File[0].Filename)
	s.Require().NotNil(parts[1].ImageURL)
	s.True(strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,"))
}

// TestMultimodalImageRailSuite_无图轮与跨轮防串 每轮袋子一换。
func (s *MultimodalImageRailSuite) TestMultimodalImageRailSuite_无图轮与跨轮防串() {
	rail := rails.NewMultimodalImageRail(boolPtr(true))

	// 第 3 轮带图
	img := s.writeImage("r3.png")
	ctx3 := prompt.WithMultimodalImageFiles(context.Background(), []prompt.MultimodalImageFile{img})
	rail.BeforeModelCall(ctx3, &agentinterfaces.AgentCallbackContext{})
	s.Require().NotNil(rail.ImageMutator())

	// 第 4 轮新 ctx 无图：残留 mutator 被清（Python 每轮窗口全新，旧 mutator 不带入本轮）
	ctx4 := context.Background()
	rail.BeforeModelCall(ctx4, &agentinterfaces.AgentCallbackContext{})
	s.Nil(rail.ImageMutator(), "无图轮应清掉上一轮 mutator")

	// 第 5 轮全新会话 ctx：读不到第 3 轮图片（防串）
	s.Nil(prompt.MultimodalImageFilesFromContext(context.Background()))
}

// TestMultimodalImageRailSuite_开关关闭剥离路径 关闭原生输入时历史图片块被剥光。
func (s *MultimodalImageRailSuite) TestMultimodalImageRailSuite_开关关闭剥离路径() {
	msg := llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "")
	msg.SetContent(llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "text", Text: "前文"},
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,x"}},
	))
	mc := &imageItestStubModelContext{messages: []llmschema.BaseMessage{msg}}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetModelContext(mc)

	rail := rails.NewMultimodalImageRail(boolPtr(false))
	s.Require().NoError(rail.BeforeModelCall(context.Background(), cbc))

	msgs, err := mc.GetMessages(0, true)
	s.Require().NoError(err)
	content := msgs[0].GetContent()
	s.True(content.IsText(), "图片块应被剥离")
	s.Equal("前文", content.Text())

	// 全剥光 → IMAGE_CONTENT_OMITTED
	onlyImg := llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "")
	onlyImg.SetContent(llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "x"}},
	))
	mc2 := &imageItestStubModelContext{messages: []llmschema.BaseMessage{onlyImg}}
	cbc2 := &agentinterfaces.AgentCallbackContext{}
	cbc2.SetModelContext(mc2)
	s.Require().NoError(rail.BeforeModelCall(context.Background(), cbc2))
	msgs2, _ := mc2.GetMessages(0, true)
	s.Equal(prompt.ImageContentOmitted, msgs2[0].GetContent().Text())
}

// ──────────────────────────── 测试辅助 ────────────────────────────

func boolPtr(b bool) *bool { return &b }

// imageItestStubModelContext 只覆盖 GetMessages/SetMessages 的 ModelContext 桩。
type imageItestStubModelContext struct {
	ceiface.ModelContext
	messages []llmschema.BaseMessage
}

func (m *imageItestStubModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return m.messages, nil
}

func (m *imageItestStubModelContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	m.messages = messages
}
