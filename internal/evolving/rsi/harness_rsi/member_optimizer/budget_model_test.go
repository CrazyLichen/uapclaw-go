// budget_model_test.go 对齐 Python budget_model.py 的可测路径。
// Python: member_optimizer/budget_model.py（24 行，BudgetedRsiModel 包装语义）。

package memberoptimizer

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestBudgetOptions_剔除非正限额并保留显式限额 对齐 _budget_options→rsi_output_options。
func TestBudgetOptions_剔除非正限额并保留显式限额(t *testing.T) {
	explicit := 512
	var bad int
	opts := []model_clients.InvokeOption{
		model_clients.WithInvokeMaxTokens(explicit), // Python：已配置的 max_tokens 不被抹除
		model_clients.WithInvokeMaxTokens(bad),      // Python：None/退化键线上省略
	}
	filtered := BudgetOptions(opts...)
	if len(filtered) != len(opts)-1 {
		t.Fatalf("应剔除非正 MaxTokens 选项: %d", len(filtered))
	}
	params := model_clients.NewInvokeParams(filtered...)
	if params.MaxTokens == nil || *params.MaxTokens != explicit {
		t.Fatalf("显式限额应保留: %v", params.MaxTokens)
	}
	// 空 opts 原样
	if got := BudgetOptions(); len(got) != 0 {
		t.Fatal("空选项应返回空")
	}
}

// TestBudgetStreamOptions_同语义 流式版本共享同一预算语义。
func TestBudgetStreamOptions_同语义(t *testing.T) {
	explicit := 256
	opts := []model_clients.StreamOption{model_clients.WithStreamMaxTokens(explicit)}
	params := model_clients.NewStreamParams(BudgetStreamOptions(opts...)...)
	if params.MaxTokens == nil || *params.MaxTokens != explicit {
		t.Fatalf("显式限额应保留: %v", params.MaxTokens)
	}
}

// TestNewBudgetedRsiModel_空配置构造 构造期预算化在 Go 指针语义下直接成立。
func TestNewBudgetedRsiModel_空配置构造(t *testing.T) {
	cfg, err0 := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.example.com")
	if err0 != nil {
		t.Fatalf("client config 构造失败: %v", err0)
	}
	m, err := NewBudgetedRsiModel(cfg, llmschema.NewModelRequestConfig())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if m == nil || m.Model == nil {
		t.Fatal("应返回包装模型")
	}
}
