package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// Python knowledge_retrieval_comp.py :249-258 validate_inputs 一比一

// assertKBCode 断言错误为 BaseError 且状态码匹配（llm_comp_test.go assertLLMCode 同款惯例）
func assertKBCode(t *testing.T, err error, status exception.StatusCode) {
	t.Helper()
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok, "期望 BaseError，实际 %T", err)
	assert.Equal(t, status, be.Status())
}

func TestValidateInputs_query缺失时返回101102(t *testing.T) {
	_, err := validateKnowledgeRetrievalInputs(map[string]any{})
	require.Error(t, err)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInputParamError)
}

func TestValidateInputs_query非字符串时返回101102(t *testing.T) {
	_, err := validateKnowledgeRetrievalInputs(map[string]any{"query": 123})
	require.Error(t, err)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInputParamError)
}

func TestValidateInputs_extra键宽容只取query(t *testing.T) {
	// Python KnowledgeRetrievalInput model_config = ConfigDict(extra="allow")
	in, err := validateKnowledgeRetrievalInputs(map[string]any{"query": "q", "other": 1})
	require.NoError(t, err)
	assert.Equal(t, "q", in.Query)
}

func TestValidateInputs_query空串合法由invoke层拒绝(t *testing.T) {
	// pydantic 只校验类型不校验空白；空串拒绝在 invoke :88-92
	in, err := validateKnowledgeRetrievalInputs(map[string]any{"query": ""})
	require.NoError(t, err)
	assert.Equal(t, "", in.Query)
}

// Python knowledge_retrieval_comp.py :260-269 _format_output 一比一

func TestFormatOutput_results拼接context(t *testing.T) {
	out := formatKnowledgeRetrievalOutput([]retrievalcommon.MultiKBRetrievalResult{
		{Text: "a"}, {Text: "b"},
	})
	assert.Equal(t, []string{"a", "b"}, out["results"])
	assert.Equal(t, "a\n\nb", out["context"])
}

func TestFormatOutput_空results(t *testing.T) {
	// pydantic default_factory=list 语义：非 nil 空切片
	out := formatKnowledgeRetrievalOutput(nil)
	assert.Equal(t, []string{}, out["results"])
	assert.Equal(t, "", out["context"])
}
