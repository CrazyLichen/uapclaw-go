package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveRequestQuery 验证本轮 query 的取值口径：params 优先、inputs 回落。
//
// Python 侧 query 只在 request.params 里（ensure_state 读 request.params["query"]）；
// Go 的进程内调用方允许只放 inputs，两者必须等价，否则进入 plan 的那一轮
// 会把用户原文改写成空串（用户消息整条丢失）。
func TestResolveRequestQuery(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		inputs map[string]any
		want   string
	}{
		{
			name:   "params 有值时优先",
			params: map[string]any{"query": "来自 params"},
			inputs: map[string]any{"query": "来自 inputs"},
			want:   "来自 params",
		},
		{
			name:   "params 为空时回落 inputs",
			params: map[string]any{},
			inputs: map[string]any{"query": "来自 inputs"},
			want:   "来自 inputs",
		},
		{
			name:   "params 非字符串时回落 inputs",
			params: map[string]any{"query": 42},
			inputs: map[string]any{"query": "来自 inputs"},
			want:   "来自 inputs",
		},
		{
			name:   "inputs 为 nil",
			params: map[string]any{},
			inputs: nil,
			want:   "",
		},
		{
			name:   "inputs 非字符串",
			params: map[string]any{},
			inputs: map[string]any{"query": 42},
			want:   "",
		},
		{
			name:   "两处都没有",
			params: map[string]any{},
			inputs: map[string]any{},
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveRequestQuery(tc.params, tc.inputs))
		})
	}
}
