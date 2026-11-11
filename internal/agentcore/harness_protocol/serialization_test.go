package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsString 验证必填字符串三种失败形态。
// Python: serialization.py:486-493 _string / _required_string
func TestAsString(t *testing.T) {
	t.Run("正常", func(t *testing.T) {
		got, err := asString(map[string]any{"k": "v"}, "k")
		require.NoError(t, err)
		assert.Equal(t, "v", got)
	})

	t.Run("键缺失", func(t *testing.T) {
		_, err := asString(map[string]any{}, "k")
		require.Error(t, err)
		// Python 是 KeyError 被外层包成 invalid harness event wire object
		assert.Contains(t, err.Error(), "invalid harness event wire object")
	})

	t.Run("空串", func(t *testing.T) {
		_, err := asString(map[string]any{"k": ""}, "k")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "k must be a non-empty string")
	})

	t.Run("非字符串", func(t *testing.T) {
		_, err := asString(map[string]any{"k": 1}, "k")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "k must be a non-empty string")
	})
}

// TestAsOptionalString 验证 nil 与空串的区别。
// Python: serialization.py:496-499 _optional_string
func TestAsOptionalString(t *testing.T) {
	got, err := asOptionalString(nil, "k")
	require.NoError(t, err)
	assert.Nil(t, got, "nil 表示未提供")

	// 空串是非法值，不是「未提供」
	_, err = asOptionalString("", "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "k must be a non-empty string")
}

// TestAsInteger 验证四步：拒 bool、整数族、整值 float64、其余报错。
// Python: serialization.py:502-506 _integer（+ spec D4 放宽整值 float64）
func TestAsInteger(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		want    int
		wantErr bool
	}{
		{"int", 3, 3, false},
		{"int64", int64(7), 7, false},
		{"整值 float64", 3.0, 3, false},
		{"负零 float64", math.Copysign(0, -1), 0, false},
		{"bool true 拒绝", true, 0, true},
		{"bool false 拒绝", false, 0, true},
		{"小数 3.5 拒绝", 3.5, 0, true},
		{"NaN 拒绝", math.NaN(), 0, true},
		{"+Inf 拒绝", math.Inf(1), 0, true},
		{"溢出 1e300 拒绝", 1e300, 0, true},
		{"字符串拒绝", "3", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := asInteger(c.value, "sequence")
			if c.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "sequence must be an integer")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestAsNumber 验证拒 bool、接受整数族与任意 float64。
// Python: serialization.py:509-513 _number
func TestAsNumber(t *testing.T) {
	got, err := asNumber(3, "timestamp")
	require.NoError(t, err)
	assert.Equal(t, 3.0, got)

	got, err = asNumber(2.5, "timestamp")
	require.NoError(t, err)
	assert.Equal(t, 2.5, got)

	// NaN 在 asNumber 不拒（交由 FreezeJsonValue 拒绝）
	got, err = asNumber(math.NaN(), "timestamp")
	require.NoError(t, err)
	assert.True(t, math.IsNaN(got))

	_, err = asNumber(true, "timestamp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp must be a number")

	_, err = asNumber("1", "timestamp")
	require.Error(t, err)
}

// TestAsMapping 验证对象与非 string key 拒绝。
// Python: serialization.py:470-473 _mapping
func TestAsMapping(t *testing.T) {
	got, err := asMapping(map[string]any{"a": 1}, "obj")
	require.NoError(t, err)
	assert.Equal(t, 1, got["a"])

	_, err = asMapping([]any{1}, "obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obj must be a JSON object")

	// 非 string key（Go 的 map[any]any）
	_, err = asMapping(map[any]any{1: "x"}, "obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obj must be a JSON object")
}

// TestAsList 验证数组与非数组拒绝。
// Python: serialization.py:480-483 _list
func TestAsList(t *testing.T) {
	got, err := asList([]any{1, 2}, "arr")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	_, err = asList(map[string]any{}, "arr")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arr must be a JSON array")
}

// TestParseXxx_合法值 验证 13 个枚举解析各自接受全部合法值。
// 成员数已机器核对（spec §5.2.2）：合计 45 成员。
// Python: serialization.py 的 14 处 _enum 调用
func TestParseXxx_合法值(t *testing.T) {
	cases := map[string]struct {
		parse func(any, string) (string, error)
		valid []string
	}{
		"MessageRole":         {func(v any, f string) (string, error) { r, e := parseMessageRole(v, f); return string(r), e }, []string{"user", "assistant", "system", "tool"}},
		"ModelRequestStatus":  {func(v any, f string) (string, error) { r, e := parseModelRequestStatus(v, f); return string(r), e }, []string{"completed", "failed", "cancelled"}},
		"TurnStatus":          {func(v any, f string) (string, error) { r, e := parseTurnStatus(v, f); return string(r), e }, []string{"completed", "interrupted", "failed"}},
		"TurnTerminationKind": {func(v any, f string) (string, error) { r, e := parseTurnTerminationKind(v, f); return string(r), e }, []string{"user_abort", "timeout", "policy", "provider", "harness_stop"}},
		"OutputKind":          {func(v any, f string) (string, error) { r, e := parseOutputKind(v, f); return string(r), e }, []string{"text", "structured"}},
		"OutputOperation":     {func(v any, f string) (string, error) { r, e := parseOutputOperation(v, f); return string(r), e }, []string{"delta", "snapshot", "final"}},
		"OutputChannel":       {func(v any, f string) (string, error) { r, e := parseOutputChannel(v, f); return string(r), e }, []string{"answer", "reasoning", "system"}},
		"ItemEventKind":       {func(v any, f string) (string, error) { r, e := parseItemEventKind(v, f); return string(r), e }, []string{"started", "updated", "completed"}},
		"UsageUpdateMode":     {func(v any, f string) (string, error) { r, e := parseUsageUpdateMode(v, f); return string(r), e }, []string{"delta", "cumulative"}},
		"HarnessState":        {func(v any, f string) (string, error) { r, e := parseHarnessState(v, f); return string(r), e }, []string{"idle", "running", "pausing", "paused", "terminated"}},
		"TurnEventKind":       {func(v any, f string) (string, error) { r, e := parseTurnEventKind(v, f); return string(r), e }, []string{"started", "paused", "resumed", "finished", "aborted", "failed"}},
		"HookEventPhase":      {func(v any, f string) (string, error) { r, e := parseHookEventPhase(v, f); return string(r), e }, []string{"started", "finished"}},
		"DiagnosticLevel":     {func(v any, f string) (string, error) { r, e := parseDiagnosticLevel(v, f); return string(r), e }, []string{"debug", "info", "warning", "error"}},
	}
	total := 0
	for name, c := range cases {
		total += len(c.valid)
		for _, v := range c.valid {
			got, err := c.parse(v, name)
			require.NoError(t, err, "%s 应接受 %q", name, v)
			assert.Equal(t, v, got)
		}
	}
	// spec §5.2.2 机器核对值：45
	assert.Equal(t, 45, total, "13 个枚举成员合计")
}

// TestParseXxx_非法值 验证非法值与非字符串的两种错误文案。
// Python: serialization.py:517-522
func TestParseXxx_非法值(t *testing.T) {
	t.Run("非字符串", func(t *testing.T) {
		_, err := parseOutputKind(1, "output.kind")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "output.kind must be a string enum value")
	})

	t.Run("未知值带单引号 repr", func(t *testing.T) {
		_, err := parseOutputKind("bogus", "output.kind")
		require.Error(t, err)
		// 上游用 {value!r}，字符串表现为 'bogus'
		assert.Contains(t, err.Error(), "unknown output.kind: 'bogus'")
	})

	t.Run("空串是非法值", func(t *testing.T) {
		_, err := parseOutputKind("", "output.kind")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown output.kind: ''")
	})
}
