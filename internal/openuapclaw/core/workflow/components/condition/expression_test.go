package condition

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 预处理面 ────────────────────────────

// TestNewExpressionCondition_超长拒绝 对齐 expression.py:36-40
func TestNewExpressionCondition_超长拒绝(t *testing.T) {
	expr := strings.Repeat("a", 5001)
	_, err := NewExpressionCondition(expr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expression length exceeds maximum")
}

// TestConvertCondition_八条规则 对齐 RULES（expression.py:15-26）+ 字面量保护
func TestConvertCondition_八条规则(t *testing.T) {
	cases := []struct{ in, want string }{
		{"${a} && ${b}", "${a} and ${b}"},
		{"${a} || ${b}", "${a} or ${b}"},
		{"true && false", "True and False"},
		{"is_false_alarm == true", "is_false_alarm == True"}, // 词边界反例：标识符不被误改
		{"length(${list}) > 0", "len(${list}) > 0"},
		{"${a} not_in ${list}", "${a} not in ${list}"},
		{"is_empty(${list})", "_safe_is_empty(${list})"},
		{"is_not_empty(${list})", "_safe_is_not_empty(${list})"},
		{"'a && b'", "'a && b'"}, // 字符串字面量保护：引号内不被替换
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, convertCondition(tc.in, nil), "in=%q", tc.in)
	}
}

// TestExpressionCondition_TraceInfo_变量提取 对齐 trace_info/_get_inputs
func TestExpressionCondition_TraceInfo_变量提取(t *testing.T) {
	c, err := NewExpressionCondition("${amount} > 100")
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, map[string]any{"amount": 150})
	info := c.TraceInfo(sess).(map[string]any)
	assert.Equal(t, "${amount} > 100", info["bool_expression"])
	assert.Equal(t, map[string]any{"${amount}": 150}, info["inputs"])
}

// TestExpressionCondition_TraceInfo_空表达式 对齐 _get_inputs 空表达式返 {}
func TestExpressionCondition_TraceInfo_空表达式(t *testing.T) {
	c, err := NewExpressionCondition("")
	require.NoError(t, err)
	info := c.TraceInfo(mustWorkflowSession(t)).(map[string]any)
	assert.Empty(t, info["inputs"])
}

// TestExpressionCondition_Call_空表达式恒真 对齐 __call__ 空表达式分支
func TestExpressionCondition_Call_空表达式恒真(t *testing.T) {
	c, err := NewExpressionCondition("")
	require.NoError(t, err)
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
}

// TestExpressionCondition_Call_sessionNil_不panic 对齐 _get_inputs session None 分支
func TestExpressionCondition_Call_sessionNil_不panic(t *testing.T) {
	c, err := NewExpressionCondition("true")
	require.NoError(t, err)
	got, err := c.Call(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, got)
}

// ──────────────────────────── 求值面全语料 ────────────────────────────

// evalOK 求值成功断言助手
func evalOK(t *testing.T, expr string, globals map[string]any, want bool) {
	t.Helper()
	c, err := NewExpressionCondition(expr)
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, globals)
	got, err := c.Call(context.Background(), sess)
	require.NoError(t, err, "expr=%q", expr)
	assert.Equal(t, want, got, "expr=%q", expr)
}

// evalErr 求值报错断言助手
func evalErr(t *testing.T, expr string, globals map[string]any, msgPart string) {
	t.Helper()
	c, err := NewExpressionCondition(expr)
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, globals)
	_, err = c.Call(context.Background(), sess)
	require.Error(t, err, "expr=%q", expr)
	assert.Contains(t, err.Error(), msgPart)
}

// TestEval_比较与逻辑 对齐 Compare/BoolOp/BinOp/UnaryOp 各分支
func TestEval_比较与逻辑(t *testing.T) {
	g := map[string]any{"a": 150, "b": "hello", "list": []any{150, 2, 3}}
	evalOK(t, "${a} > 100", g, true)
	evalOK(t, "${a} > 100 && ${b} == 'hello'", g, true)
	evalOK(t, "${a} > 100 || ${a} < 50", g, true)
	evalOK(t, "not ${a} < 100", g, true)
	evalOK(t, "${a} in ${list}", g, true)
	evalOK(t, "${a} not_in ${list}", map[string]any{"a": 9, "list": []any{150, 2, 3}}, true)
	evalOK(t, "100 < ${a} < 200", g, true)  // 链式比较
	evalOK(t, "1 < 2 and 3 > 2", nil, true) // 原生 and/or
	evalOK(t, "1 < 2 or 1 > 2", nil, true)
	evalOK(t, "true", nil, true)
	evalOK(t, "false || true", nil, true)
	evalOK(t, "1 + 1 == 2", nil, true)
	evalOK(t, "2 ** 10 == 1024", nil, true)
	evalOK(t, "7 // 2 == 3", nil, true)
	evalOK(t, "7 % 3 == 1", nil, true)
	evalOK(t, "10 / 4 == 2.5", nil, true) // 真除法恒 float
	evalOK(t, "1 << 3 == 8", nil, true)
	evalOK(t, "-5 + 2 == -3", nil, true)
	evalOK(t, "~5 == -6", nil, true)
	evalOK(t, "'x' * 2 == 'xx'", nil, true)
	evalOK(t, "'ab' + 'cd' == 'abcd'", nil, true)
}

// TestEval_调用与容器 对齐 Call/Subscript/Slice/Attribute/List/Dict 分支
func TestEval_调用与容器(t *testing.T) {
	g := map[string]any{"list": []any{1, 2, 3}, "dict": map[string]any{"k": "v"}, "s": "abc"}
	evalOK(t, "len(${list}) == 3", g, true)
	evalOK(t, "is_empty(${list}) == False", g, true)
	evalOK(t, "is_not_empty(${s})", g, true)
	evalOK(t, "${dict}['k'] == 'v'", g, true)
	evalOK(t, "${dict}.k == 'v'", g, true)  // dict 属性回落（getattr→dict access）
	evalOK(t, "${list}[1] == 2", g, true)   // 下标
	evalOK(t, "${list}[-1] == 3", g, true)  // 负索引
	evalOK(t, "${s}[0:2] == 'ab'", g, true) // 切片
	evalOK(t, "${s}[:2] == 'ab'", g, true)  // 开放切片
	evalOK(t, "${list}[0:2] == [1, 2]", g, true)
	evalOK(t, "len([1, 2]) == 2", g, true)           // List 节点
	evalOK(t, "len({'a': 1, 'b': 2}) == 2", g, true) // Dict 节点
	evalOK(t, "sum([1, 2, 3]) == 6", g, true)
	evalOK(t, "bool('') == False", g, true)
}

// TestEval_Name四级解析 对齐 _evaluate_name 顺序
func TestEval_Name四级解析(t *testing.T) {
	evalOK(t, "${var_0} == 5", map[string]any{"var_0": 5}, true) // runtime（变量改名后注入）
	evalOK(t, "True and not False", nil, true)
	evalErr(t, "undefined_var == 1", nil, "is not defined")
}

// TestEval_护栏 对齐三重护栏与封禁分支
func TestEval_护栏(t *testing.T) {
	big := make([]any, 100001)
	evalErr(t, "is_empty(${big})", map[string]any{"big": big}, "collection size exceeds maximum")
	evalErr(t, "${o}.__class__ == 1", map[string]any{"o": map[string]any{}}, "disallowed operation")
	evalErr(t, "${o}.__dunder__ == 1", map[string]any{"o": map[string]any{}}, "disallowed operation")
	evalErr(t, "2 ** 101 > 0", nil, "exponent too large")
	evalErr(t, "${a} +", map[string]any{"a": 1}, "unexpected end") // 语法错误→SYNTAX_ERROR
	evalErr(t, "len(123)", nil, "len() operate on non-sequence")
	evalErr(t, "is_empty(123)", nil, "cannot check emptiness")
	evalErr(t, "${list}[999]", map[string]any{"list": []any{1}}, "index out of range")
	evalErr(t, "${list}[0:100001]", map[string]any{"list": []any{1, 2}}, "slice operation would create")
	evalErr(t, "${s} + 1", map[string]any{"s": "a"}, "unsupported operand type")
}

// TestEval_深度护栏 对齐 MAX_AST_DEPTH
func TestEval_深度护栏(t *testing.T) {
	deep := ""
	for i := 0; i < 60; i++ {
		deep += "("
	}
	deep += "1"
	for i := 0; i < 60; i++ {
		deep += ")"
	}
	evalErr(t, deep, nil, "nesting depth exceeds maximum")
}

// TestEval_In遇不可含类型返False 对齐 expression.py:412-416 TypeError→False
// （链式比较中 `in` 返 False 使全链为 False，非报错）
func TestEval_In遇不可含类型返False(t *testing.T) {
	evalOK(t, "not (${a} in 123)", map[string]any{"a": 1}, true)
	evalOK(t, "not (${a} not in ${list})", map[string]any{"a": 1, "list": []any{1}}, true)
}

// ──────────────────────────── 补充覆盖（错误分支/边角语料） ────────────────────────────

// TestEval_身份比较 对齐 is / is not 分支（Go 值相等近似，spec §7 记录）
func TestEval_身份比较(t *testing.T) {
	evalOK(t, "${a} is 1", map[string]any{"a": 1}, true)
	evalOK(t, "${a} is not 2", map[string]any{"a": 1}, true)
}

// TestEval_一元正号与按位 对齐 UnaryOp UAdd/Invert
func TestEval_一元正号与按位(t *testing.T) {
	evalOK(t, "+5 == 5", nil, true)
	evalOK(t, "5 & 3 == 1", nil, true)
	evalOK(t, "5 | 2 == 7", nil, true)
	evalOK(t, "5 ^ 3 == 6", nil, true)
	evalOK(t, "1 >> 1 == 0", nil, true)
}

// TestEval_负索引字符串 下标负回绕（string 形态）
func TestEval_负索引字符串(t *testing.T) {
	evalOK(t, "${s}[-1] == 'c'", map[string]any{"s": "abc"}, true)
	evalErr(t, "${s}[99]", map[string]any{"s": "abc"}, "string index out of range")
	evalErr(t, "${s}['x']", map[string]any{"s": "abc"}, "string indices must be integers")
}

// TestEval_字典错误分支 KeyError 与不可下标
func TestEval_字典错误分支(t *testing.T) {
	evalErr(t, "${d}['nope']", map[string]any{"d": map[string]any{}}, "KeyError")
	evalErr(t, "123[0]", nil, "not subscriptable")
	evalErr(t, "${d}.nope", map[string]any{"d": map[string]any{}}, "has no attribute")
}

// TestEval_调用错误分支 非 callable 与未知函数
func TestEval_调用错误分支(t *testing.T) {
	evalErr(t, "123(1)", nil, "is not defined or not callable")
	evalErr(t, "nope(1)", nil, "name 'nope' is not defined") // 对齐 Python：Name 先于 callable 校验
	evalErr(t, "len()", nil, "takes exactly one argument")
	evalErr(t, "bool()", nil, "takes exactly one argument")
	evalErr(t, "sum()", nil, "requires at least one")
	evalErr(t, "sum('abc')", nil, "operands must be numeric")
	evalErr(t, "is_empty()", nil, "takes exactly one argument")
	evalErr(t, "is_not_empty()", nil, "takes exactly one argument")
}

// TestEval_词法错误分支 非法字符与未闭合字符串（→SYNTAX_ERROR）
func TestEval_词法错误分支(t *testing.T) {
	evalErr(t, "1 @ 2", nil, "invalid character")
	evalErr(t, "'abc", nil, "unterminated string")
	evalErr(t, "1 2", nil, "unexpected token")
	evalErr(t, "(1", nil, "expected")
	evalErr(t, "f(,)", nil, "unexpected token")
	evalErr(t, "${s}.", map[string]any{"s": "x"}, "expected attribute name")
	evalErr(t, "[1,", nil, "unexpected end")
	evalErr(t, "{'a'", nil, "expected")
	evalErr(t, "${list}[::0]", map[string]any{"list": []any{1}}, "slice step cannot be zero")
}

// TestEval_字符串与数值比较 不可比较类型报 TypeError 语义
func TestEval_字符串与数值比较(t *testing.T) {
	evalOK(t, "'b' > 'a'", nil, true)
	evalErr(t, "'a' < 1", nil, "not supported between")
	evalOK(t, "True == 1", nil, true) // Python bool 归一数值
}

// TestEval_无len属性值判空 对齐 safeIsEmpty：无 __len__ → False（bool 值走护栏报错）
func TestEval_无len属性值判空(t *testing.T) {
	evalErr(t, "is_empty(True)", nil, "cannot check emptiness of bool type")
}

// TestEval_变量值未注入时经inputs解析 对齐 _evaluate_name inputs 二级
func TestEval_变量值未注入时经inputs解析(t *testing.T) {
	// 变量在 runtime（var_i 注入）与 inputs（原键）均可命中
	evalOK(t, "${v} + 1 == 2", map[string]any{"v": 1}, true)
}

// TestExpressionCondition_Invoke_直调 对齐 invoke 面（与 Call 同语义）
func TestExpressionCondition_Invoke_直调(t *testing.T) {
	c, err := NewExpressionCondition("${a} > 1")
	require.NoError(t, err)
	got, err := c.Invoke(context.Background(), map[string]any{}, mustWorkflowSessionWithGlobals(t, map[string]any{"a": 2}))
	require.NoError(t, err)
	assert.True(t, got)
}

// TestAlwaysTrue_Invoke_直调 对齐非导出 Invoke 面
func TestAlwaysTrue_Invoke_直调(t *testing.T) {
	c := NewAlwaysTrue()
	got, err := c.Invoke(context.Background(), map[string]any{}, mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
}

// TestEval_float比较 对齐 floatCompare 分支
func TestEval_float比较(t *testing.T) {
	evalOK(t, "2.5 > 2", nil, true)
	evalOK(t, "0.5 <= 0.5", nil, true)
	evalOK(t, "is_not_empty('ab')", nil, true)
}

// TestExpressionCondition_原子链回调 经 AtomicInvoke 触达基座回调（invokeImpl 面）
func TestExpressionCondition_原子链回调(t *testing.T) {
	c, err := NewExpressionCondition("${a} > 1")
	require.NoError(t, err)
	res, err := c.AtomicInvoke(context.Background(),
		map[string]any{"session": mustWorkflowSessionWithGlobals(t, map[string]any{"a": 2})})
	require.NoError(t, err)
	assert.Equal(t, true, res)
}

// TestEval_比较方向全覆盖 对齐 intCompare/floatCompare/stringCompare 各分支
func TestEval_比较方向全覆盖(t *testing.T) {
	evalOK(t, "2 <= 2 && 2 >= 2 && 2 > 1", nil, true)
	evalOK(t, "2.0 <= 2.5 && 2.5 > 2.0", nil, true)
	evalOK(t, "'b' >= 'a' && 'b' > 'a'", nil, true)
	evalOK(t, "${l} == [1, 2]", map[string]any{"l": []any{1, 2}}, true) // 容器相等 fallback 分支
}

// TestEval_truthy分支 对齐 None/空容器/未知类型
func TestEval_truthy分支(t *testing.T) {
	evalOK(t, "not None", nil, true)
	evalOK(t, "not [] == True", nil, true)
	evalOK(t, "not {} == True", nil, true)
	evalOK(t, "${v}", map[string]any{"v": []any{1}}, true) // 非空列表 truthy
}

// TestEval_切片负步长 对齐 sliceIndices 负方向分支
func TestEval_切片负步长(t *testing.T) {
	evalOK(t, "${s}[::-1] == 'cba'", map[string]any{"s": "abc"}, true)
	evalOK(t, "${l}[::-1] == [3, 2, 1]", map[string]any{"l": []any{1, 2, 3}}, true)
	evalErr(t, "${s}[::0]", map[string]any{"s": "abc"}, "slice step cannot be zero")
}

// TestEval_Name数字解析 对齐 _evaluate_name int/float 双轨
func TestEval_Name数字解析(t *testing.T) {
	evalOK(t, "1e2 == 100", nil, true) // ParseFloat 兜底
}

// TestEval_一元符号float 对齐 evalUnary +/- float 分支
func TestEval_一元符号float(t *testing.T) {
	evalOK(t, "-1.5 + 1.5 == 0", nil, true)
	evalOK(t, "+0.5 == 0.5", nil, true)
	evalErr(t, "~1.5 == 0", nil, "bad operand type for unary ~")
}

// TestEval_边界补语料 收口覆盖率：NewBaseCondition schema 分支与 bool() 包装
func TestEval_边界补语料(t *testing.T) {
	// bool 双轨：int 入参保持 int；float 入参转 float
	evalOK(t, "bool([]) == False", nil, true)
	evalOK(t, "bool('x') == True", nil, true)
	// true/false 大小写词边界（规则 \btrue\b 不匹配 True）
	evalOK(t, "True and true", nil, true)
}

// TestEval_容器in与dict回落 对齐 containsOp map 分支与 typeName 非常规类型
func TestEval_容器in与dict回落(t *testing.T) {
	evalOK(t, "'k' in ${d}", map[string]any{"d": map[string]any{"k": 1}}, true)
	evalOK(t, "${d}['k'] == 1", map[string]any{"d": map[string]any{"k": 1}}, true)
	evalOK(t, "len(${m}) == 1", map[string]any{"m": map[string]any{"k": 1}}, true) // map len
	evalOK(t, "not (1 in 'abc')", nil, true)                                       // str 右值遇非 str 左值 → False（链失败非报错）
}

// TestEval_浮点与字符串方向剩余分支 收口 floatCompare/stringCompare 剩余方向
func TestEval_浮点与字符串方向剩余分支(t *testing.T) {
	evalOK(t, "1.5 >= 1.5 && 1.5 < 2.0", nil, true)
	evalOK(t, "'b' <= 'c' && 'b' != 'a'", nil, true)
}

// TestEval_truthy未知类型 对齐 truthy 默认真分支（无 len 的自定义值近似）
func TestEval_truthy未知类型(t *testing.T) {
	evalOK(t, "not (not ${v})", map[string]any{"v": struct{}{}}, true)
}

// TestEval_未知类型二元运算报错 对齐 evalBinary 非数值 unsupported operand
func TestEval_未知类型二元运算报错(t *testing.T) {
	evalErr(t, "${v} + 1", map[string]any{"v": struct{}{}}, "unsupported operand type")
}

// TestEval_Name非法字面量报错 收口 evalName 解析失败分支
func TestEval_Name非法字面量报错(t *testing.T) {
	evalErr(t, "abc == 1", nil, "name 'abc' is not defined")
}
