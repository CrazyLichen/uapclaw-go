package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// queryDoc 构造一份最小 query 文档。
func queryDoc(operation string, params map[string]any) map[string]any {
	doc := map[string]any{
		"schema_version": "0.1",
		"type":           "query",
		"operation":      operation,
	}
	if params != nil {
		doc["params"] = params
	}
	return doc
}

// TestQueryFields_十操作 验证操作集合。
// Python: query.py:28-39 QUERY_FIELDS
func TestQueryFields_十操作(t *testing.T) {
	ops := []string{
		"protocol.capabilities", "protocol.schema",
		"session.get", "session.list",
		"model.list", "model.resolve",
		"mode.list", "mode.resolve",
		"permission.get", "mcp.validate",
	}
	require.Len(t, ops, 10)
	for _, op := range ops {
		require.Contains(t, queryFieldSpecs, op, "op=%s 应被识别", op)
	}
	require.Len(t, QueryOperations(), 10)
}

// TestOneShotQueryInput_未知操作报错
// Python: query.py:53-54 if self.operation not in QUERY_FIELDS: raise ValueError
func TestOneShotQueryInput_未知操作报错(t *testing.T) {
	_, err := OneShotQueryInputFromDict(queryDoc("bogus.op", nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "query operation is not supported")
}

// TestOneShotQueryInput_sessionGet缺sessionId报错
// Python: query.py:31 "session.get": ({"session_id"}, {"session_id"})
func TestOneShotQueryInput_sessionGet缺sessionId报错(t *testing.T) {
	_, err := OneShotQueryInputFromDict(queryDoc("session.get", nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required fields")
}

// TestOneShotQueryInput_limit越界报错
// Python: query.py:69-72 if not 1 <= limit <= 200: raise ValueError
func TestOneShotQueryInput_limit越界报错(t *testing.T) {
	for _, n := range []int{0, 201} {
		_, err := OneShotQueryInputFromDict(queryDoc("session.list", map[string]any{"limit": n}))
		require.Error(t, err, "limit=%d", n)
	}
}

// TestOneShotQueryInput_search超长报错
// Python: query.py:65-68 if len(search) > 200: raise ValueError
func TestOneShotQueryInput_search超长报错(t *testing.T) {
	long := make([]rune, 201)
	for i := range long {
		long[i] = 'a'
	}
	_, err := OneShotQueryInputFromDict(queryDoc("session.list", map[string]any{"search": string(long)}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 200 characters")
}

// TestOneShotQueryInput_未知参数报错
// Python: query.py:56-61 params 严格对象
func TestOneShotQueryInput_未知参数报错(t *testing.T) {
	_, err := OneShotQueryInputFromDict(queryDoc("session.list", map[string]any{"bogus": 1}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown fields")
}

// TestOneShotQueryResult_成功须有operation与data
// Python: query.py:142-148
func TestOneShotQueryResult_成功须有operation与data(t *testing.T) {
	_, err := NewOneShotQueryResult(OneShotQueryResultParams{
		RequestID: "r1", Status: string(StatusCompleted), ExitCode: 0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an operation and data")
}

// TestOneShotQueryResult_失败须有error与非0码
// Python: query.py:149-150
func TestOneShotQueryResult_失败须有error与非0码(t *testing.T) {
	_, err := NewOneShotQueryResult(OneShotQueryResultParams{
		RequestID: "r1", Status: string(StatusFailed), ExitCode: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonzero exit_code and error")
}

// TestOneShotQueryResult_ToDict固定sequence0与会话nil
// Python: query.py:154-166
func TestOneShotQueryResult_ToDict固定sequence0与会话nil(t *testing.T) {
	op := "session.list"
	res, err := NewOneShotQueryResult(OneShotQueryResultParams{
		RequestID: "r1", Operation: &op, Status: string(StatusCompleted),
		ExitCode: 0, Data: map[string]any{"sessions": []any{}},
	})
	require.NoError(t, err)
	dict := res.ToDict()
	require.Equal(t, 0, dict["sequence"])
	require.Nil(t, dict["session_id"])
	require.Equal(t, "query_result", dict["type"])
}
