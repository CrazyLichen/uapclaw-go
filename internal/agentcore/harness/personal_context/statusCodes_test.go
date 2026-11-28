package personal_context

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

func TestStatusCodes_七个专属状态码(t *testing.T) {
	cases := []struct {
		status exception.StatusCode
		name   string
		code   int
	}{
		{StatusCodeContextProactiveConfigInvalid, "CONTEXT_PROACTIVE_CONFIG_INVALID", 154000},
		{StatusCodeContextProactiveStateInvalid, "CONTEXT_PROACTIVE_STATE_INVALID", 154001},
		{StatusCodeContextProactiveFileExecutionError, "CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", 154002},
		{StatusCodeContextProactiveFetchExecutionError, "CONTEXT_PROACTIVE_FETCH_EXECUTION_ERROR", 154003},
		{StatusCodeContextProactivePipelineExecutionError, "CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR", 154004},
		{StatusCodeContextProactivePublishExecutionError, "CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR", 154005},
		{StatusCodeContextProactiveRuntimeTimeout, "CONTEXT_PROACTIVE_RUNTIME_TIMEOUT", 154006},
	}
	for _, c := range cases {
		require.Equal(t, c.name, c.status.Name())
		require.EqualValues(t, c.code, c.status.Code())
		require.Contains(t, c.status.Message(), "{error_msg}")
	}
}

func TestBuildError_专属码本地解析(t *testing.T) {
	err := BuildError(StatusCodeContextProactiveStateInvalid, exception.WithParam("error_msg", "sandbox missing"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "context proactive_state is invalid")
}

func TestBuildError_框架码回落(t *testing.T) {
	// 框架码走 exception.BuildError 统一构建（D-24.3a-6：Go 侧已统一分类解析）。
	err := BuildError(exception.StatusError)
	require.Error(t, err)
}
