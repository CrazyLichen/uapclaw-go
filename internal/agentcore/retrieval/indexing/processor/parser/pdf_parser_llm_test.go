//go:build llm

package parser

import (
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestPDFParser_ParseContent_真实VLM 真实 VLM caption 集成用例。
// 运行方式: go test -tags=llm -run 'TestPDFParser_ParseContent_真实VLM' ./internal/agentcore/retrieval/indexing/processor/parser/ -v
// 前置: OPENAI_API_KEY（或 DashScope 等多模态模型 key）环境变量
func TestPDFParser_ParseContent_真实VLM(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("需要 OPENAI_API_KEY 环境变量")
	}

	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfPath := writeTempPDF(t, "real.pdf", []string{"real caption test"}, map[int]bool{1: true})

	// 真实多模态模型（gpt-4o 系）——模型配置按项目实际 key 提供方调整
	clientCfg, err := llmschema.NewModelClientConfig("openai", apiKey, "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	modelCfg := llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4o-mini"))
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}

	p := newPDFParserOrFatal(t)
	content, err := p.ParseContent(t.Context(), pdfPath, model)
	if err != nil {
		t.Fatalf("真实调用不应报错: %v", err)
	}
	t.Logf("真实 caption 结果:\n%s", content)
	if content == "" {
		t.Error("真实 VLM 应产出非空内容（文本+caption）")
	}
}
