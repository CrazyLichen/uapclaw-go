package evolving

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGetContentStringFromTemplate(t *testing.T) {
	t.Run("字符串模板", func(t *testing.T) {
		tpl := prompt.NewPromptTemplate("test", "hello world")
		result := GetContentStringFromTemplate(tpl)
		if result != "hello world" {
			t.Errorf("got %q, expected %q", result, "hello world")
		}
	})

	t.Run("多消息模板", func(t *testing.T) {
		msgs := []llmschema.BaseMessage{
			llmschema.NewSystemMessage("system content"),
			llmschema.NewUserMessage("user content"),
		}
		tpl := prompt.NewPromptTemplate("test", msgs)
		result := GetContentStringFromTemplate(tpl)
		expected := "system content\nuser content"
		if result != expected {
			t.Errorf("got %q, expected %q", result, expected)
		}
	})

	t.Run("空模板", func(t *testing.T) {
		tpl := prompt.NewPromptTemplate("test", "")
		result := GetContentStringFromTemplate(tpl)
		if result != "" {
			t.Errorf("got %q, expected empty string", result)
		}
	})

	t.Run("带占位符模板", func(t *testing.T) {
		tpl := prompt.NewPromptTemplate("test", "hello {{name}}")
		result := GetContentStringFromTemplate(tpl)
		if result != "hello {{name}}" {
			t.Errorf("got %q, expected %q", result, "hello {{name}}")
		}
	})
}
