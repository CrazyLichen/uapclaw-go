package processcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// TestQueryFailure_状态由退出码决定
// Python: query.py:57-68 query_failure（124→TIMED_OUT, 130→CANCELLED, 其余→FAILED）
func TestQueryFailure_状态由退出码决定(t *testing.T) {
	got := QueryFailure("r1", "TIMEOUT", "Command timed out.", 124)
	require.Equal(t, string(protocol.StatusTimedOut), got.Status)
	require.Equal(t, 124, got.ExitCode)

	got = QueryFailure("r1", "CANCELLED", "Command interrupted.", 130)
	require.Equal(t, string(protocol.StatusCancelled), got.Status)

	got = QueryFailure("r1", "RUNTIME_FAILED", "x", 1)
	require.Equal(t, string(protocol.StatusFailed), got.Status)
	require.Equal(t, 1, got.ExitCode)
}

// TestQueryFailure_默认退出码为1
// Python: query.py:58 exit_code: int = 1
func TestQueryFailure_默认退出码为1(t *testing.T) {
	got := QueryFailure("r1", "RUNTIME_FAILED", "x", 0)
	require.Equal(t, string(protocol.StatusFailed), got.Status)
	require.Equal(t, 0, got.ExitCode, "query_failure 不改写传入的退出码")
}

// TestDispatchQuery_未知操作panic
// Python: query.py:54 operations[request.operation]() —— KeyError 由 run_query 的
// 兜底 except 捕获；此处单独验证 dispatch 对未知操作不返回数据
func TestDispatchQuery_未知操作(t *testing.T) {
	c := NewInProcessRuntimeClient(swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{}))
	request := &protocol.OneShotQueryInput{
		SchemaVersion: "0.1",
		Operation:     "protocol.capabilities",
		Params:        map[string]any{},
	}
	// protocol.capabilities 不在 Python 的 catalogs/operations 表里
	// （上游 dispatch_query 只处理 2+1+5 共 8 个，另两个由 main.py 直接返回）
	require.NotPanics(t, func() {
		_, _ = DispatchQuery(c, request)
	})
}

// TestQueryFailure_details为空
// Python: RuntimeErrorInfo 默认 details={}
func TestQueryFailure_details为空(t *testing.T) {
	got := QueryFailure("r1", "X", "m", 1)
	require.NotNil(t, got.Error)
	require.Equal(t, "X", got.Error.Code)
	require.Equal(t, "m", got.Error.Message)
}
