package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// mustDefinition 构造并校验一份最小合法定义。
func mustDefinition(t *testing.T, name, instructions string) RuntimeAgentDefinition {
	t.Helper()
	d, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: name, Instructions: instructions, ToolsWildcard: true,
	})
	require.NoError(t, err)
	return d
}

// ──────────────────────────── 名称校验 ────────────────────────────

// TestRuntimeAgentDefinition_名称格式 去空白后须匹配 [A-Za-z0-9_-]{3,50}。
// Python: agent_definition.py:168-171
func TestRuntimeAgentDefinition_名称格式(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"合法三字符", "abc", true},
		{"合法含连字符下划线", "my-agent_1", true},
		{"合法五十字符上界", strings.Repeat("a", 50), true},
		{"太短二字符", "ab", false},
		{"含空格", "a b c", false},
		{"含中文", "智能体", false},
		{"含点号", "a.b.c", false},
		{"空串", "", false},
		{"仅空白", "   ", false},
		{"五十一字符超界", strings.Repeat("a", 51), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
				Name: c.value, Instructions: "do it", ToolsWildcard: true,
			})
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
			require.Equal(t, "name", AgentDefinitionErrorField(err))
		})
	}
}

// TestRuntimeAgentDefinition_名称去空白后存回 name 只去首尾空白，内部不含空白由正则拒绝。
// Python: agent_definition.py:168/171 name = _required_text("name", ...); setattr
func TestRuntimeAgentDefinition_名称去空白后存回(t *testing.T) {
	d := mustDefinition(t, "  abc  ", "do it")
	require.Equal(t, "abc", d.Name)
}

// ──────────────────────────── instructions 校验 ────────────────────────────

// TestRuntimeAgentDefinition_instructions保留原始空白 preserve=True 保留内部与首尾空白。
// Python: agent_definition.py:172-176 _required_text(..., preserve=True)
func TestRuntimeAgentDefinition_instructions保留原始空白(t *testing.T) {
	original := "  行一\n\n  行二  "
	d := mustDefinition(t, "abc", original)
	require.Equal(t, original, d.Instructions)
}

// TestRuntimeAgentDefinition_instructions不可为空白 全空白视为空。
// Python: agent_definition.py:92-93 if not value.strip(): raise
func TestRuntimeAgentDefinition_instructions不可为空白(t *testing.T) {
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "   \n\t ", ToolsWildcard: true,
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "instructions", AgentDefinitionErrorField(err))
}

// ──────────────────────────── 可选文本 ────────────────────────────

// TestRuntimeAgentDefinition_可选文本空白转nil description/model 全空白视为未提供。
// Python: agent_definition.py:97-100 _optional_text → _required_text（会报空）
func TestRuntimeAgentDefinition_可选文本空白报错(t *testing.T) {
	blank := "   "
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", Description: &blank, ToolsWildcard: true,
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "description", AgentDefinitionErrorField(err))
}

// TestRuntimeAgentDefinition_可选文本去空白 提供的值去首尾空白。
// Python: agent_definition.py:100 _required_text 返回 value.strip()
func TestRuntimeAgentDefinition_可选文本去空白(t *testing.T) {
	raw := "  desc  "
	d, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", Description: &raw, ToolsWildcard: true,
	})
	require.NoError(t, err)
	require.NotNil(t, d.Description)
	require.Equal(t, "desc", *d.Description)
}

// ──────────────────────────── skills 校验 ────────────────────────────

// TestRuntimeAgentDefinition_skills不得重复 重复元素报错。
// Python: agent_definition.py:109-110 if len(set(result)) != len(result)
func TestRuntimeAgentDefinition_skills不得重复(t *testing.T) {
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", ToolsWildcard: true,
		Skills: []string{"s1", "s1"},
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "skills", AgentDefinitionErrorField(err))
	require.Contains(t, err.Error(), "must not contain duplicates")
}

// TestRuntimeAgentDefinition_skills元素不得为空 空串元素报错。
// Python: agent_definition.py:106-108 逐项 _required_text
func TestRuntimeAgentDefinition_skills元素不得为空(t *testing.T) {
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", ToolsWildcard: true, Skills: []string{"s1", "  "},
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
}

// TestRuntimeAgentDefinition_skills去空白 元素去首尾空白后存。
func TestRuntimeAgentDefinition_skills去空白(t *testing.T) {
	d, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", ToolsWildcard: true, Skills: []string{" s1 "},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"s1"}, d.Skills)
}

// ──────────────────────────── tools 校验 ────────────────────────────

// TestRuntimeAgentDefinition_单星号等价通配 单项 ["*"] 归一为通配。
// Python: agent_definition.py:119-120 if tools == ("*",): return "*"
func TestRuntimeAgentDefinition_单星号等价通配(t *testing.T) {
	d, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", Tools: []string{"*"},
	})
	require.NoError(t, err)
	require.True(t, d.ToolsWildcard)
}

// TestRuntimeAgentDefinition_星号不可与其他混用 混用报错。
// Python: agent_definition.py:121-122
func TestRuntimeAgentDefinition_星号不可与其他混用(t *testing.T) {
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", Tools: []string{"*", "read_file"},
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "tools", AgentDefinitionErrorField(err))
	require.Contains(t, err.Error(), "cannot be combined")
}

// TestRuntimeAgentDefinition_工具列表去重 重复工具名报错（复用 _string_tuple）。
// Python: agent_definition.py:118 → _string_tuple 的去重检查
func TestRuntimeAgentDefinition_工具列表去重(t *testing.T) {
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", Tools: []string{"a", "a"},
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "tools", AgentDefinitionErrorField(err))
}

// ──────────────────────────── max_iterations 校验 ────────────────────────────

// TestRuntimeAgentDefinition_最大迭代次数须为正整数 零与负数报错。
// Python: agent_definition.py:185-194
func TestRuntimeAgentDefinition_最大迭代次数须为正整数(t *testing.T) {
	positive := 3
	_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
		Name: "abc", Instructions: "x", ToolsWildcard: true, MaxIterations: &positive,
	})
	require.NoError(t, err)

	for _, invalid := range []int{0, -1} {
		value := invalid
		_, err := NewRuntimeAgentDefinition(RuntimeAgentDefinition{
			Name: "abc", Instructions: "x", ToolsWildcard: true, MaxIterations: &value,
		})
		require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition, "value=%d", invalid)
		require.Equal(t, "max_iterations", AgentDefinitionErrorField(err))
	}
}

// ──────────────────────────── fromMapping 严格性 ────────────────────────────

// TestRuntimeAgentDefinitionFromMapping_未知键报错 未知键 field=None。
// Python: agent_definition.py:136-141（field=None）
func TestRuntimeAgentDefinitionFromMapping_未知键报错(t *testing.T) {
	_, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x", "bogus_key": 1,
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "", AgentDefinitionErrorField(err), "上游未知键的 field=None")
	require.Contains(t, err.Error(), "unknown fields: bogus_key")
}

// TestRuntimeAgentDefinitionFromMapping_多未知键排序 排序后拼接。
// Python: agent_definition.py:136-141 sorted(keys - _DEFINITION_FIELDS)
func TestRuntimeAgentDefinitionFromMapping_多未知键排序(t *testing.T) {
	_, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x", "zzz": 1, "aaa": 2,
	})
	require.Contains(t, err.Error(), "unknown fields: aaa, zzz")
}

// TestRuntimeAgentDefinitionFromMapping_缺必填键报错 缺 name 或 instructions。
// Python: agent_definition.py:142-147
func TestRuntimeAgentDefinitionFromMapping_缺必填键报错(t *testing.T) {
	_, err := RuntimeAgentDefinitionFromMapping(map[string]any{"name": "abc"})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Contains(t, err.Error(), "missing required fields: instructions")

	_, err = RuntimeAgentDefinitionFromMapping(map[string]any{"instructions": "x"})
	require.Contains(t, err.Error(), "missing required fields: name")
}

// TestRuntimeAgentDefinitionFromMapping_非对象报错 字符串/切片/ nil 均拒绝。
// Python: agent_definition.py:131-132
func TestRuntimeAgentDefinitionFromMapping_非对象报错(t *testing.T) {
	for _, value := range []any{"not a map", 42, []any{"a"}, nil} {
		_, err := RuntimeAgentDefinitionFromMapping(value)
		require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
		require.Contains(t, err.Error(), "must be an object")
	}
}

// TestRuntimeAgentDefinitionFromMapping_完整字段 全字段解析成功。
// Python: agent_definition.py:197-209
func TestRuntimeAgentDefinitionFromMapping_完整字段(t *testing.T) {
	d, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "do it", "description": "d",
		"model": "m1", "tools": "*", "skills": []any{"s1"}, "max_iterations": float64(3),
	})
	require.NoError(t, err)
	require.Equal(t, "abc", d.Name)
	require.True(t, d.ToolsWildcard)
	require.NotNil(t, d.Description)
	require.Equal(t, "d", *d.Description)
	require.NotNil(t, d.Model)
	require.Equal(t, "m1", *d.Model)
	require.Equal(t, []string{"s1"}, d.Skills)
	require.NotNil(t, d.MaxIterations)
	require.Equal(t, 3, *d.MaxIterations)
}

// TestRuntimeAgentDefinitionFromMapping_缺省tools与skills 缺省为通配与空列表。
// Python: agent_definition.py:206-207 data.get("tools","*") / data.get("skills",())
func TestRuntimeAgentDefinitionFromMapping_缺省tools与skills(t *testing.T) {
	d, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x",
	})
	require.NoError(t, err)
	require.True(t, d.ToolsWildcard)
	require.Empty(t, d.Skills)
}

// TestRuntimeAgentDefinitionFromMapping_单字符串工具被拒 非 "*" 的单字符串不合法。
// Python: agent_definition.py:124-126（先 _required_text 再报 must be '*' or sequence）
func TestRuntimeAgentDefinitionFromMapping_单字符串工具被拒(t *testing.T) {
	_, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x", "tools": "read_file",
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "tools", AgentDefinitionErrorField(err))
	require.Contains(t, err.Error(), "must be '*' or a sequence of strings")
}

// TestRuntimeAgentDefinitionFromMapping_非整数maxIterations被拒 1.5 与 true 均拒绝。
// Python: agent_definition.py:186-189 isinstance(bool) 排除
func TestRuntimeAgentDefinitionFromMapping_非整数maxIterations被拒(t *testing.T) {
	_, err := RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x", "max_iterations": float64(1.5),
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
	require.Equal(t, "max_iterations", AgentDefinitionErrorField(err))

	_, err = RuntimeAgentDefinitionFromMapping(map[string]any{
		"name": "abc", "instructions": "x", "max_iterations": true,
	})
	require.ErrorIs(t, err, ErrAgentDefinitionInvalidDefinition)
}

// ──────────────────────────── 错误码 ────────────────────────────

// TestAgentDefinitionError_七码可判定 7 个错误码各自可判定且 code 可读。
// Python: agent_definition.py:37-46 7 个错误码
func TestAgentDefinitionError_七码可判定(t *testing.T) {
	cases := []struct {
		sentinel error
		code     string
		field    string
	}{
		{ErrAgentDefinitionInvalidDefinition, "AGENT_DEFINITION_INVALID", "name"},
		{ErrAgentDefinitionInvalidRequest, "AGENT_DEFINITION_REQUEST_INVALID", "request"},
		{ErrAgentDefinitionInvalidMode, "AGENT_DEFINITION_MODE_INVALID", "mode"},
		{ErrAgentDefinitionUnsupportedMode, "AGENT_DEFINITION_MODE_UNSUPPORTED", "mode"},
		{ErrAgentDefinitionModelNotFound, "AGENT_DEFINITION_MODEL_NOT_FOUND", "model"},
		{ErrAgentDefinitionToolAllowlistUnsupported,
			"AGENT_DEFINITION_TOOL_ALLOWLIST_UNSUPPORTED", "tools"},
		{ErrAgentExecutionSessionConflict, "AGENT_DEFINITION_SESSION_CONFLICT", "agent"},
	}
	for _, c := range cases {
		err := &agentDefinitionError{sentinel: c.sentinel, field: &c.field, message: "boom"}
		require.ErrorIs(t, err, c.sentinel)
		require.Equal(t, c.code, AgentDefinitionErrorCode(err))
		require.Equal(t, c.field, AgentDefinitionErrorField(err))
	}
}

// TestAgentDefinitionError_非本类型返回空 普通 error 两个取值函数均返回空串。
func TestAgentDefinitionError_非本类型返回空(t *testing.T) {
	other := errors.New("other")
	require.Equal(t, "", AgentDefinitionErrorField(other))
	require.Equal(t, "", AgentDefinitionErrorCode(other))
}

// TestAgentDefinitionError_无字段时Error无前缀 field=nil 时 Error() 只有消息。
// Python: agent_definition.py:49-64 field 可为 None
func TestAgentDefinitionError_无字段时Error无前缀(t *testing.T) {
	err := invalidNoField("agent definition must be an object")
	require.Equal(t, "agent definition must be an object", err.Error())
}

// ──────────────────────────── 执行体 ────────────────────────────

// TestPrepareAgentExecution_非法模式被拒 非法 mode → INVALID_MODE。
// Python: agent_definition.py:241-253
func TestPrepareAgentExecution_非法模式被拒(t *testing.T) {
	for _, mode := range []string{"bogus.mode", "", "  ", "team"} {
		_, err := PrepareAgentExecution(mustDefinition(t, "abc", "x"), mode)
		require.ErrorIs(t, err, ErrAgentDefinitionInvalidMode, "mode=%q", mode)
		require.Equal(t, "mode", AgentDefinitionErrorField(err))
	}
}

// TestPrepareAgentExecution_四模式均可 四个 canonical 模式构造成功。
// Python: agent_definition.py:67-73 RuntimeAgentMode 四个成员
func TestPrepareAgentExecution_四模式均可(t *testing.T) {
	for _, mode := range []string{
		RuntimeAgentModeWorkNormal, RuntimeAgentModeWorkPlan,
		RuntimeAgentModeCodeNormal, RuntimeAgentModeCodePlan,
	} {
		exec, err := PrepareAgentExecution(mustDefinition(t, "abc", "x"), mode)
		require.NoError(t, err, mode)
		require.Equal(t, mode, exec.Mode)
		require.Equal(t, "abc", exec.Definition.Name)
	}
}

// TestPrepareAgentExecution_接受映射 传入 map 时先走 from_mapping。
// Python: agent_definition.py:296-300
func TestPrepareAgentExecution_接受映射(t *testing.T) {
	exec, err := PrepareAgentExecution(map[string]any{
		"name": "abc", "instructions": "x",
	}, RuntimeAgentModeWorkNormal)
	require.NoError(t, err)
	require.Equal(t, "abc", exec.Definition.Name)
}

// TestRuntimeAgentExecution_fingerprint取定义 执行体指纹等于定义指纹。
// Python: agent_definition.py:273-277
func TestRuntimeAgentExecution_fingerprint取定义(t *testing.T) {
	d := mustDefinition(t, "abc", "x")
	exec, err := PrepareAgentExecution(d, RuntimeAgentModeWorkNormal)
	require.NoError(t, err)

	want, err := RuntimeAgentDefinitionFingerprint(d)
	require.NoError(t, err)
	got, err := RuntimeAgentExecutionFingerprint(exec)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestRuntimeAgentExecutionToDict_三字段 含 agent/agent_fingerprint/mode。
// Python: agent_definition.py:279-286
func TestRuntimeAgentExecutionToDict_三字段(t *testing.T) {
	d := mustDefinition(t, "abc", "x")
	exec, err := PrepareAgentExecution(d, RuntimeAgentModeCodePlan)
	require.NoError(t, err)

	got, err := RuntimeAgentExecutionToDict(exec)
	require.NoError(t, err)
	require.Contains(t, got, "agent")
	require.Contains(t, got, "agent_fingerprint")
	require.Equal(t, RuntimeAgentModeCodePlan, got["mode"])

	wantFingerprint, err := RuntimeAgentDefinitionFingerprint(d)
	require.NoError(t, err)
	require.Equal(t, wantFingerprint, got["agent_fingerprint"])
}

// ──────────────────────────── fingerprint golden ────────────────────────────

// TestRuntimeAgentDefinitionFingerprint_PythonGolden 与 Python 实算值逐字节一致。
// Python: agent_definition.py:224-238 fingerprint
//
// golden 由运行上游 Python 得到：
//
//	RuntimeAgentDefinition.from_mapping({...}).fingerprint
//
// 关键坑：Go 的 json.Encoder 追加 '\n'，必须 TrimSuffix，否则 sha256 不同。
func TestRuntimeAgentDefinitionFingerprint_PythonGolden(t *testing.T) {
	cases := []struct {
		name       string
		definition map[string]any
		want       string
	}{
		{
			name:       "最小定义",
			definition: map[string]any{"name": "abc", "instructions": "do it"},
			want:       "78d83b176ee5da429df5a818e1fc913053aabd25eba5aaa7fb7dbbe42fb487c5",
		},
		{
			name: "带描述与模型",
			definition: map[string]any{
				"name": "abc", "instructions": "do it", "description": "d", "model": "m1",
			},
			want: "00ece482b00e4e0037835139b62048757f0372647253f7dc26059238b4bae812",
		},
		{
			name:       "中文指令",
			definition: map[string]any{"name": "abc", "instructions": "中文指令，含标点。"},
			want:       "3b38cb53062100bd012124723ace653ff9383baa8b058596109e25b39feb8f8f",
		},
		{
			name: "显式工具与技能",
			definition: map[string]any{
				"name": "abc", "instructions": "x",
				"tools": []any{"read_file", "write_file"}, "skills": []any{"s1"},
			},
			want: "292a9e9875e12362efba65c386e63682767c8fc4350e9afb5adb060981b32248",
		},
		{
			name: "带最大迭代次数",
			definition: map[string]any{
				"name": "abc", "instructions": "x", "max_iterations": float64(3),
			},
			want: "b79f664ef5048fbde113560218f60b8181ab93c13d7b09db05b3bf835a0359c2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := RuntimeAgentDefinitionFromMapping(c.definition)
			require.NoError(t, err)
			got, err := RuntimeAgentDefinitionFingerprint(d)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

// TestRuntimeAgentDefinitionFingerprint_同定义稳定 两次调用同值且长度 64。
func TestRuntimeAgentDefinitionFingerprint_同定义稳定(t *testing.T) {
	d := mustDefinition(t, "abc", "x")
	a, err := RuntimeAgentDefinitionFingerprint(d)
	require.NoError(t, err)
	b, err := RuntimeAgentDefinitionFingerprint(d)
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Len(t, a, 64)
}

// TestRuntimeAgentDefinitionFingerprint_字段差异产生不同指纹 任一字段变则指纹变。
// Python: to_dict 的全部字段参与 fingerprint
func TestRuntimeAgentDefinitionFingerprint_字段差异产生不同指纹(t *testing.T) {
	base := mustDefinition(t, "abc", "x")
	baseFingerprint, err := RuntimeAgentDefinitionFingerprint(base)
	require.NoError(t, err)

	one := 5
	description := "d"
	skills := []string{"s"}

	mutations := []RuntimeAgentDefinition{
		{Name: "abd", Instructions: "x", ToolsWildcard: true},
		{Name: "abc", Instructions: "y", ToolsWildcard: true},
		{Name: "abc", Instructions: "x", Description: &description, ToolsWildcard: true},
		{Name: "abc", Instructions: "x", Tools: []string{"read_file"}},
		{Name: "abc", Instructions: "x", Skills: skills, ToolsWildcard: true},
		{Name: "abc", Instructions: "x", MaxIterations: &one, ToolsWildcard: true},
	}
	for index, mutation := range mutations {
		got, err := RuntimeAgentDefinitionFingerprint(mutation)
		require.NoError(t, err)
		require.NotEqual(t, baseFingerprint, got, "第 %d 个字段差异应改变指纹", index)
	}
}

// TestRuntimeAgentDefinitionToDict_规范形状 与上游 to_dict 键集合一致。
// Python: agent_definition.py:211-222
func TestRuntimeAgentDefinitionToDict_规范形状(t *testing.T) {
	d := mustDefinition(t, "abc", "x")
	got := RuntimeAgentDefinitionToDict(d)
	require.Len(t, got, 7)
	for _, key := range []string{
		"name", "instructions", "description", "model",
		"tools", "skills", "max_iterations",
	} {
		require.Contains(t, got, key)
	}
	require.Equal(t, "*", got["tools"], "通配时序列化为字符串 '*'")
	require.Equal(t, []string{}, got["skills"], "空技能序列化为空列表")
	require.Nil(t, got["max_iterations"])
}
