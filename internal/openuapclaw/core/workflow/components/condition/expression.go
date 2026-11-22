package condition

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 常量 ────────────────────────────

// rules 表达式预翻译规则（Python RULES，expression.py:15-26 逐条复刻；
// true/false 带词边界——上游注释明确防误伤 is_false_alarm 类标识符）。
var rules = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`&&`), "and"},
	{regexp.MustCompile(`\|\|`), "or"},
	{regexp.MustCompile(`\btrue\b`), "True"},
	{regexp.MustCompile(`\bfalse\b`), "False"},
	{regexp.MustCompile(`length\(`), "len("},
	{regexp.MustCompile(`not_in`), "not in"},
	{regexp.MustCompile(`is_empty\(`), "_safe_is_empty("},
	{regexp.MustCompile(`is_not_empty\(`), "_safe_is_not_empty("},
}

var (
	// varFullPattern ${...} 变量模式（expression.py:33）
	varFullPattern = regexp.MustCompile(`\$\{[^}]*\}`)
	// innerVarPattern 变量名内层（expression.py:69）
	innerVarPattern = regexp.MustCompile(`\$\{([^{}]*)\}`)
	// stringPattern 字符串字面量（单/双引号含转义，expression.py:138）
	stringPattern = regexp.MustCompile(`("(?:[^"\\]|\\.)*")|('(?:[^'\\]|\\.)*')`)
)

// ──────────────────────────── AST 节点对照表 ────────────────────────────
// Python ast.BoolOp→boolOpNode / ast.Compare→compareNode / ast.BinOp→binOpNode /
// ast.UnaryOp→unaryOpNode / ast.Name→nameNode / ast.Constant→constantNode /
// ast.Subscript→subscriptNode / ast.Attribute→attrNode / ast.Call→callNode /
// ast.List→listNode / ast.Tuple→tupleNode / ast.Dict→dictNode。
// spec: docs/superpowers/specs/2026-11-21-branch-comp-8.19-design.md §4.2

// ──────────────────────────── 结构体 ────────────────────────────

// exprNode 求值器 AST 节点统一标记接口（标记接口：节点类型由 Go 类型区分，
// 不需要方法面——评估逻辑走类型 switch）。
type exprNode interface{}

// boolOpNode ≙ ast.BoolOp（And/Or）
type boolOpNode struct {
	isAnd  bool
	values []exprNode
}

// compareOp 链式比较的单个算子
type compareOp struct {
	kind string // == != < <= > >= is "is not" in "not in"
}

// compareNode ≙ ast.Compare（多链：ops 与 comparators 逐对 zip）
type compareNode struct {
	first  exprNode
	ops    []compareOp
	rights []exprNode
}

// binOpNode ≙ ast.BinOp
type binOpNode struct {
	kind        string // + - * / % ** // << >> | ^ &
	left, right exprNode
}

// unaryOpNode ≙ ast.UnaryOp
type unaryOpNode struct {
	kind    string // - + not ~
	operand exprNode
}

// nameNode ≙ ast.Name
type nameNode struct {
	id string
}

// constantNode ≙ ast.Constant
type constantNode struct {
	value any
}

// sliceNode 切片三段（可 nil，≙ ast.Slice lower/upper/step）
type sliceNode struct {
	lower, upper, step exprNode
}

// subscriptNode ≙ ast.Subscript（index=Index 形态 / slice=Slice 形态二选一）
type subscriptNode struct {
	value exprNode
	index exprNode
	slice *sliceNode
}

// attrNode ≙ ast.Attribute
type attrNode struct {
	value exprNode
	attr  string
}

// callNode ≙ ast.Call
type callNode struct {
	fn   exprNode
	args []exprNode
}

// listNode ≙ ast.List
type listNode struct {
	elts []exprNode
}

// tupleNode ≙ ast.Tuple
type tupleNode struct {
	elts []exprNode
}

// dictNode ≙ ast.Dict
type dictNode struct {
	keys   []exprNode
	values []exprNode
}

// ExpressionCondition 表达式条件（Python expression.py:29-64）。
// 覆写 Call：Python 覆写 __call__ 直接求值，**不走 atomic 链**（无 session 校验/
// CommitCmp）——逐字保留该行为差异（8.19 spec §4.1）。
type ExpressionCondition struct {
	BaseCondition
	// expression 原始表达式（运行期 string，每次评估时解析——spec §3 拍板①）
	expression string
	// matches 构造期抽出的 ${...} 变量名列表（供 trace_info 用）
	matches []string
}

// NewExpressionCondition 构造表达式条件（Python __init__：存字段 + findall +
// 长度护栏）。
func NewExpressionCondition(expr string) (*ExpressionCondition, error) {
	if len(expr) > constants.MaxExpressionLength {
		return nil, exception.BuildError(exception.StatusExpressionEvalError,
			exception.WithParam("error_msg",
				fmt.Sprintf("expression length exceeds maximum allowed length of %d", constants.MaxExpressionLength)))
	}
	c := &ExpressionCondition{expression: expr, matches: varFullPattern.FindAllString(expr, -1)}
	c.BaseCondition = NewBaseCondition(c.invokeImpl, nil)
	return c, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 求值实现（Python invoke，expression.py:56-59）。
func (c *ExpressionCondition) Invoke(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, error) {
	if len(c.expression) == 0 {
		return true, nil
	}
	return c.evaluateExpression(c.expression, c.getInputs(sess))
}

// Call 覆写基座（Python __call__ 覆写，expression.py:61-64：直接求值，不走 atomic 链）。
func (c *ExpressionCondition) Call(_ context.Context, sess session.BaseSession) (bool, error) {
	if len(c.expression) == 0 {
		return true, nil
	}
	return c.evaluateExpression(c.expression, c.getInputs(sess))
}

// TraceInfo 返回 dict（Python trace_info，expression.py:42-46）。
func (c *ExpressionCondition) TraceInfo(sess session.BaseSession) any {
	return map[string]any{"bool_expression": c.expression, "inputs": c.getInputs(sess)}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl 基座回调槽（≙ Python invoke；原子链经 NewBaseCondition 接入，
// 虽然 ExpressionCondition.Call 已覆写不走此链，但 Invoke 直调面保持一致）。
func (c *ExpressionCondition) invokeImpl(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
	if len(c.expression) == 0 {
		return true, nil, nil
	}
	flag, err := c.evaluateExpression(c.expression, c.getInputs(sess))
	return flag, nil, err // Python invoke 返回裸 bool，无 tuple
}

// getInputs 变量取值（Python _get_inputs，expression.py:48-54：空表达式或
// session nil 返回 {}，否则 session.state().get_global(match[2:-1])）。
func (c *ExpressionCondition) getInputs(sess session.BaseSession) map[string]any {
	if len(c.expression) == 0 || sess == nil {
		return map[string]any{}
	}
	inputs := map[string]any{}
	for _, m := range c.matches {
		inputs[m] = sess.State().GetGlobal(state.StringKey(m[2 : len(m)-1]))
	}
	return inputs
}

// evaluateExpression 入口（Python _evaluate_expression，expression.py:66-128）。
func (c *ExpressionCondition) evaluateExpression(expression string, inputs map[string]any) (bool, error) {
	processed := convertCondition(expression, inputs)

	// 变量改名（expression.py:69-77：按出现序编号 var_i，
	// replacement 按 full match 长度降序应用防前缀串扰）
	matches := innerVarPattern.FindAllString(processed, -1)
	type rep struct{ full, safe string }
	reps := make([]rep, 0, len(matches))
	for i, m := range matches {
		reps = append(reps, rep{m, fmt.Sprintf("var_%d", i)})
	}
	sort.SliceStable(reps, func(i, j int) bool {
		return len(reps[i].full) > len(reps[j].full)
	})
	for _, r := range reps {
		processed = strings.ReplaceAll(processed, r.full, r.safe)
	}

	// runtime 环境（expression.py:79-92）
	runtime := map[string]any{
		"len":                wrapLen,
		"bool":               wrapBool,
		"sum":                wrapSum,
		"inputs":             inputs,
		"_safe_is_empty":     wrapIsEmpty,
		"_safe_is_not_empty": wrapIsNotEmpty,
		"is_empty":           wrapIsEmpty,
		"is_not_empty":       wrapIsNotEmpty,
	}
	// 变量值注入 runtime（expression.py:94-97：full match 存在于 inputs 才注入）
	for i, m := range matches {
		if v, ok := inputs[m]; ok {
			runtime[fmt.Sprintf("var_%d", i)] = v
		}
	}

	toks, err := tokenize(processed)
	if err != nil {
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithMsg(err.Error()))
	}
	p := &exprParser{toks: toks}
	tree, err := p.parseExpr()
	if err != nil {
		// 求值错误（如深度护栏）已是 BaseError：对齐 Python except BaseError: raise
		// 原样上抛（expression.py:120-122），不重包成语法错误
		if be, ok := err.(*exception.BaseError); ok {
			return false, be
		}
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithMsg(err.Error()))
	}
	if t := p.peek(); t != nil {
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithMsg(fmt.Sprintf("unexpected token %q", t.text)))
	}
	result, err := evaluateAST(tree, runtime)
	if err != nil {
		return false, err
	}
	return truthy(result), nil
}

// convertCondition 预翻译（Python convert_condition，expression.py:131-161
// 逐字复刻：字面量保护 → RULES → 还原）。
func convertCondition(condition string, _ map[string]any) string {
	var stringLiterals []string
	protected := stringPattern.ReplaceAllStringFunc(condition, func(s string) string {
		idx := len(stringLiterals)
		stringLiterals = append(stringLiterals, s)
		return fmt.Sprintf("__STRING_LITERAL_%d__", idx)
	})
	for _, r := range rules {
		protected = r.pattern.ReplaceAllString(protected, r.replacement)
	}
	for i, lit := range stringLiterals {
		protected = strings.ReplaceAll(protected,
			fmt.Sprintf("__STRING_LITERAL_%d__", i), lit)
	}
	return protected
}

// safeLen 安全长度（Python _safe_len，expression.py:164-173：None→0，非序列报错）。
func safeLen(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String, reflect.Map, reflect.Chan:
		return v.Len(), nil
	}
	return 0, exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg",
			fmt.Sprintf("len() operate on non-sequence value of type %s", typeName(value))))
}

// safeIsEmpty 安全判空（Python _safe_is_empty，expression.py:176-196）。
func safeIsEmpty(value any) (bool, error) {
	if value == nil {
		return true, nil
	}
	switch value.(type) {
	case int, int64, float64, bool:
		return false, exception.BuildError(exception.StatusExpressionEvalError,
			exception.WithParam("error_msg",
				fmt.Sprintf("cannot check emptiness of %s type", typeName(value))))
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String, reflect.Map:
		if v.Len() > constants.MaxCollectionSize {
			return false, exception.BuildError(exception.StatusExpressionEvalError,
				exception.WithParam("error_msg",
					fmt.Sprintf("collection size exceeds maximum allowed size of %d", constants.MaxCollectionSize)))
		}
		return v.Len() == 0, nil
	}
	// 无 __len__ → False（Python: if not hasattr(value, "__len__"): return False）
	return false, nil
}

// safeIsNotEmpty 安全判非空（Python _safe_is_not_empty，expression.py:199-201）。
func safeIsNotEmpty(value any) (bool, error) {
	// Python: return not _safe_is_empty(value)
	empty, err := safeIsEmpty(value)
	if err != nil {
		return false, err
	}
	return !empty, nil
}

// typeName 值类型名（Python type(value).__name__ 等价面）。
func typeName(value any) string {
	t := reflect.TypeOf(value)
	if t == nil {
		return "NoneType"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Name() == "" {
		switch t.Kind() {
		case reflect.Slice:
			return "list"
		case reflect.Map:
			return "dict"
		case reflect.String:
			return "str"
		case reflect.Bool:
			return "bool"
		case reflect.Int:
			return "int"
		case reflect.Float64:
			return "float"
		}
		return t.Kind().String()
	}
	return t.Name()
}

// ──────────────────────────── 词法 ────────────────────────────

// tokenKind 词元类型
type tokenKind int

const (
	tokNum tokenKind = iota
	tokStr
	tokName
	tokOp // 运算符/分隔符
)

// token 词元
type token struct {
	kind tokenKind
	text string
	// num 数值字面量（int 解析失败回落 float，对齐 Python 字面量双轨）
	num any
}

// opTexts 运算符表（长算子在前保证最长匹配）
var opTexts = []string{
	"**", "//", "==", "!=", "<=", ">=", "<<", ">>",
	"+", "-", "*", "/", "%", "<", ">", "(", ")", "[", "]", "{", "}",
	",", ":", ".", "=", "~", "!", "&", "|", "^",
}

// tokenize 词法分析（字面量/标识符/数值/运算符）。
func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	isIdentStart := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	isIdentPart := func(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'' || c == '"':
			end := i + 1
			for end < len(s) {
				if s[end] == '\\' {
					end += 2
					continue
				}
				if s[end] == c {
					break
				}
				end++
			}
			if end >= len(s) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			lit, err := strconv.Unquote(strconv.Quote(s[i+1 : end]))
			if err != nil {
				lit = s[i+1 : end]
			}
			toks = append(toks, token{kind: tokStr, text: lit})
			i = end + 1
		case isDigit(c):
			j := i
			for j < len(s) && (isDigit(s[j]) || s[j] == '.' || s[j] == 'e' || s[j] == 'E' ||
				((s[j] == '+' || s[j] == '-') && j > i && (s[j-1] == 'e' || s[j-1] == 'E'))) {
				j++
			}
			text := s[i:j]
			if iv, err := strconv.ParseInt(text, 10, 64); err == nil {
				toks = append(toks, token{kind: tokNum, num: int(iv)})
			} else {
				fv, _ := strconv.ParseFloat(text, 64)
				toks = append(toks, token{kind: tokNum, num: fv})
			}
			i = j
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			toks = append(toks, token{kind: tokName, text: s[i:j]})
			i = j
		default:
			matched := false
			for _, op := range opTexts {
				if strings.HasPrefix(s[i:], op) {
					toks = append(toks, token{kind: tokOp, text: op})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("invalid character %q", c)
			}
		}
	}
	return toks, nil
}

// ──────────────────────────── parser ────────────────────────────

// exprParser 递归下降 parser。depth 承载 Python _check_ast_depth（parser 期即拦，
// 超过 MAX_AST_DEPTH 报 EXPRESSION_EVAL_ERROR，语义等价）。
type exprParser struct {
	toks  []token
	pos   int
	depth int
}

func (p *exprParser) peek() *token {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *exprParser) eat(text string) bool {
	if t := p.peek(); t != nil && t.text == text {
		p.pos++
		return true
	}
	return false
}

func (p *exprParser) expect(text string) error {
	if !p.eat(text) {
		return fmt.Errorf("expected %q", text)
	}
	return nil
}

// parseExpr 优先级链：or < and < not < 比较 < +- < */%// < **/一元 < 后缀
// （对齐 Python ast 结构层）。
func (p *exprParser) parseExpr() (exprNode, error) {
	return p.parseOr()
}

func (p *exprParser) parseOr() (exprNode, error) {
	if err := p.checkDepth(); err != nil {
		return nil, err
	}
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	if p.eat("or") {
		values := []exprNode{left}
		for {
			v, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			if !p.eat("or") {
				break
			}
		}
		return &boolOpNode{isAnd: false, values: values}, nil
	}
	return left, nil
}

func (p *exprParser) parseAnd() (exprNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	if p.eat("and") {
		values := []exprNode{left}
		for {
			v, err := p.parseNot()
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			if !p.eat("and") {
				break
			}
		}
		return &boolOpNode{isAnd: true, values: values}, nil
	}
	return left, nil
}

func (p *exprParser) parseNot() (exprNode, error) {
	if p.eat("not") {
		v, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &unaryOpNode{kind: "not", operand: v}, nil
	}
	return p.parseCompare()
}

func (p *exprParser) parseCompare() (exprNode, error) {
	left, err := p.parseShift()
	if err != nil {
		return nil, err
	}
	var ops []compareOp
	var rights []exprNode
	for {
		kind := p.matchCompareOp()
		if kind == "" {
			break
		}
		right, err := p.parseShift()
		if err != nil {
			return nil, err
		}
		ops = append(ops, compareOp{kind: kind})
		rights = append(rights, right)
	}
	if len(ops) == 0 {
		return left, nil
	}
	return &compareNode{first: left, ops: ops, rights: rights}, nil
}

// parseShift 移位层（Python 优先级：| < ^ < & < << >> < +-）。
func (p *exprParser) parseShift() (exprNode, error) {
	left, err := p.parseBitOr()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "<<" && t.text != ">>") {
			break
		}
		p.pos++
		right, err := p.parseBitOr()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: t.text, left: left, right: right}
	}
	return left, nil
}

// parseBitOr 按位或层。
func (p *exprParser) parseBitOr() (exprNode, error) {
	left, err := p.parseBitXor()
	if err != nil {
		return nil, err
	}
	for p.eat("|") {
		right, err := p.parseBitXor()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: "|", left: left, right: right}
	}
	return left, nil
}

// parseBitXor 按位异或层。
func (p *exprParser) parseBitXor() (exprNode, error) {
	left, err := p.parseBitAnd()
	if err != nil {
		return nil, err
	}
	for p.eat("^") {
		right, err := p.parseBitAnd()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: "^", left: left, right: right}
	}
	return left, nil
}

// parseBitAnd 按位与层。
func (p *exprParser) parseBitAnd() (exprNode, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.eat("&") {
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: "&", left: left, right: right}
	}
	return left, nil
}

// matchCompareOp 匹配比较算子（双词算子 not in / is not 需跨两个标识符词元）。
func (p *exprParser) matchCompareOp() string {
	t := p.peek()
	if t == nil {
		return ""
	}
	at := func(off int) *token {
		if p.pos+off < len(p.toks) {
			return &p.toks[p.pos+off]
		}
		return nil
	}
	switch {
	case t.kind == tokOp:
		for _, k := range []string{"==", "!=", "<=", ">=", "<", ">"} {
			if t.text == k {
				p.pos++
				return k
			}
		}
	case t.text == "not" && at(1) != nil && at(1).text == "in":
		p.pos += 2
		return "not in"
	case t.text == "is" && at(1) != nil && at(1).text == "not":
		p.pos += 2
		return "is not"
	case t.text == "in":
		p.pos++
		return "in"
	case t.text == "is":
		p.pos++
		return "is"
	}
	return ""
}

func (p *exprParser) parseAdd() (exprNode, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "+" && t.text != "-") {
			break
		}
		p.pos++
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: t.text, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseMul() (exprNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "*" && t.text != "/" && t.text != "%" && t.text != "//") {
			break
		}
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binOpNode{kind: t.text, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseUnary() (exprNode, error) {
	t := p.peek()
	if t != nil && (t.text == "-" || t.text == "+" || t.text == "~") {
		p.pos++
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryOpNode{kind: t.text, operand: v}, nil
	}
	return p.parsePower()
}

func (p *exprParser) parsePower() (exprNode, error) {
	base, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	if p.eat("**") {
		exp, err := p.parseUnary() // Python ** 右结合
		if err != nil {
			return nil, err
		}
		return &binOpNode{kind: "**", left: base, right: exp}, nil
	}
	return base, nil
}

func (p *exprParser) parsePostfix() (exprNode, error) {
	v, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.eat("."):
			t := p.peek()
			if t == nil || t.kind != tokName {
				return nil, fmt.Errorf("expected attribute name")
			}
			p.pos++
			v = &attrNode{value: v, attr: t.text}
		case p.eat("["):
			// Slice 判定：[a:b] / [a:b:c]；否则 Index
			var lower, upper, step exprNode
			if !p.eat(":") {
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if p.eat(":") {
					lower = e
				} else {
					if err := p.expect("]"); err != nil {
						return nil, err
					}
					v = &subscriptNode{value: v, index: e}
					continue
				}
			}
			if t := p.peek(); t != nil && t.text != "]" && t.text != ":" {
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				upper = e
			}
			if p.eat(":") {
				if t := p.peek(); t != nil && t.text != "]" {
					e, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					step = e
				}
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			v = &subscriptNode{value: v, slice: &sliceNode{lower: lower, upper: upper, step: step}}
		case p.eat("("):
			var args []exprNode
			if t := p.peek(); t != nil && t.text != ")" {
				for {
					e, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, e)
					if !p.eat(",") {
						break
					}
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			v = &callNode{fn: v, args: args}
		default:
			return v, nil
		}
	}
}

func (p *exprParser) parseAtom() (exprNode, error) {
	if err := p.checkDepth(); err != nil {
		return nil, err
	}
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch {
	case t.kind == tokNum:
		p.pos++
		return &constantNode{value: t.num}, nil
	case t.kind == tokStr:
		p.pos++
		return &constantNode{value: t.text}, nil
	case t.kind == tokName && t.text == "True":
		p.pos++
		return &constantNode{value: true}, nil
	case t.kind == tokName && t.text == "False":
		p.pos++
		return &constantNode{value: false}, nil
	case t.kind == tokName && t.text == "None":
		p.pos++
		return &constantNode{value: nil}, nil
	case t.kind == tokName:
		p.pos++
		return &nameNode{id: t.text}, nil
	case t.text == "(":
		p.pos++
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	case t.text == "[":
		p.pos++
		var elts []exprNode
		if tt := p.peek(); tt != nil && tt.text != "]" {
			for {
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				elts = append(elts, e)
				if !p.eat(",") {
					break
				}
			}
		}
		if len(elts) > constants.MaxCollectionSize {
			return nil, p.evalErr("list contains too many elements, maximum allowed is " +
				strconv.Itoa(constants.MaxCollectionSize))
		}
		if err := p.expect("]"); err != nil {
			return nil, err
		}
		return &listNode{elts: elts}, nil
	case t.text == "{":
		p.pos++
		var keys, values []exprNode
		if tt := p.peek(); tt != nil && tt.text != "}" {
			for {
				k, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if err := p.expect(":"); err != nil {
					return nil, err
				}
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				keys = append(keys, k)
				values = append(values, v)
				if !p.eat(",") {
					break
				}
			}
		}
		if len(keys) > constants.MaxCollectionSize {
			return nil, p.evalErr("dictionary has too many key-value pairs, maximum allowed is " +
				strconv.Itoa(constants.MaxCollectionSize))
		}
		if err := p.expect("}"); err != nil {
			return nil, err
		}
		return &dictNode{keys: keys, values: values}, nil
	}
	return nil, fmt.Errorf("unexpected token %q", t.text)
}

func (p *exprParser) checkDepth() error {
	p.depth++
	if p.depth > constants.MaxASTDepth {
		return p.evalErr(fmt.Sprintf("expression nesting depth exceeds maximum allowed depth of %d",
			constants.MaxASTDepth))
	}
	return nil
}

func (p *exprParser) evalErr(msg string) error {
	return exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg", msg))
}

// ──────────────────────────── evaluator ────────────────────────────

// evaluateAST 求值器（Python _evaluate_ast，expression.py:261-303；节点分支
// 顺序对齐源码，BaseError 原样上抛）。
func evaluateAST(node exprNode, rt map[string]any) (any, error) {
	switch n := node.(type) {
	case *boolOpNode:
		// Python _evaluate_bool_op 先全量求值再 all/any（不做短路优化，逐字复刻）
		var values []bool
		for _, v := range n.values {
			r, err := evaluateAST(v, rt)
			if err != nil {
				return nil, err
			}
			values = append(values, truthy(r))
		}
		if n.isAnd {
			for _, b := range values {
				if !b {
					return false, nil
				}
			}
			return true, nil
		}
		for _, b := range values {
			if b {
				return true, nil
			}
		}
		return false, nil
	case *compareNode:
		// Python _evaluate_compare：逐对比较，False 即返
		left, err := evaluateAST(n.first, rt)
		if err != nil {
			return nil, err
		}
		for i, op := range n.ops {
			right, err := evaluateAST(n.rights[i], rt)
			if err != nil {
				return nil, err
			}
			ok, err := compareValues(left, right, op.kind)
			if err != nil {
				return nil, err
			}
			if !ok {
				return false, nil
			}
			left = right
		}
		return true, nil
	case *nameNode:
		return evalName(n, rt)
	case *constantNode:
		return n.value, nil
	case *subscriptNode:
		return evalSubscript(n, rt)
	case *attrNode:
		return evalAttribute(n, rt)
	case *callNode:
		fn, err := evaluateAST(n.fn, rt)
		if err != nil {
			return nil, err
		}
		args := make([]any, 0, len(n.args))
		for _, a := range n.args {
			r, err := evaluateAST(a, rt)
			if err != nil {
				return nil, err
			}
			args = append(args, r)
		}
		if fn == nil {
			return nil, evalErrAny("function <nil> is not defined or not callable")
		}
		callable, ok := fn.(func([]any) (any, error))
		if !ok {
			return nil, evalErrAny(fmt.Sprintf("function %v is not defined or not callable", fn))
		}
		return callable(args)
	case *listNode:
		out := make([]any, 0, len(n.elts))
		for _, e := range n.elts {
			r, err := evaluateAST(e, rt)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	case *tupleNode:
		out := make([]any, 0, len(n.elts))
		for _, e := range n.elts {
			r, err := evaluateAST(e, rt)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	case *dictNode:
		out := map[string]any{}
		for i := range n.keys {
			k, err := evaluateAST(n.keys[i], rt)
			if err != nil {
				return nil, err
			}
			v, err := evaluateAST(n.values[i], rt)
			if err != nil {
				return nil, err
			}
			out[fmt.Sprintf("%v", k)] = v
		}
		return out, nil
	case *unaryOpNode:
		v, err := evaluateAST(n.operand, rt)
		if err != nil {
			return nil, err
		}
		return evalUnary(n.kind, v)
	case *binOpNode:
		left, err := evaluateAST(n.left, rt)
		if err != nil {
			return nil, err
		}
		right, err := evaluateAST(n.right, rt)
		if err != nil {
			return nil, err
		}
		return evalBinary(n.kind, left, right)
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported AST node type: %T", node))
}

// evalName 四级解析（Python _evaluate_name，expression.py:428-457：runtime →
// inputs → int → float → True/False/None → 报错）。
func evalName(n *nameNode, rt map[string]any) (any, error) {
	if v, ok := rt[n.id]; ok {
		return v, nil
	}
	if inputs, ok := rt["inputs"].(map[string]any); ok {
		if v, ok := inputs[n.id]; ok {
			return v, nil
		}
	}
	if iv, err := strconv.Atoi(n.id); err == nil {
		return iv, nil
	}
	if fv, err := strconv.ParseFloat(n.id, 64); err == nil {
		return fv, nil
	}
	switch n.id {
	case "True":
		return true, nil
	case "False":
		return false, nil
	case "None":
		return nil, nil
	}
	return nil, evalErrAny(fmt.Sprintf("name '%s' is not defined", n.id))
}

// compareValues 比较（Python _compare_values，expression.py:395-425：
// in/not in 遇 TypeError 返 False）。
func compareValues(left, right any, op string) (bool, error) {
	switch op {
	case "==":
		return pyEqual(left, right), nil
	case "!=":
		return !pyEqual(left, right), nil
	case "<", "<=", ">", ">=":
		if li, lok := toInt(left); lok {
			if ri, rok := toInt(right); rok {
				return intCompare(li, ri, op), nil
			}
		}
		if lf, lok := toFloat(left); lok {
			if rf, rok := toFloat(right); rok {
				return floatCompare(lf, rf, op), nil
			}
		}
		// 非数值（字符串等）按字典序比较；不可比较按 Python TypeError 处理
		if ls, lok := left.(string); lok {
			if rs, rok := right.(string); rok {
				return stringCompare(ls, rs, op), nil
			}
		}
		return false, evalErrAny(fmt.Sprintf("'<' not supported between instances of '%s' and '%s'",
			typeName(left), typeName(right)))
	case "is":
		return left == right, nil // Go 无对象身份，值相等近似（spec §7 记录）
	case "is not":
		return left != right, nil
	case "in":
		return containsOp(left, right) // TypeError→False（expression.py:412-416）
	case "not in":
		ok, _ := containsOp(left, right)
		return !ok, nil
	}
	return false, evalErrAny(fmt.Sprintf("unsupported comparison operator: %s", op))
}

// containsOp Python `in`（str/切片/map；TypeError 返 False）。
func containsOp(left, right any) (bool, error) {
	switch r := right.(type) {
	case string:
		l, ok := left.(string)
		if !ok {
			return false, nil
		}
		return strings.Contains(r, l), nil
	case []any:
		for _, e := range r {
			if pyEqual(e, left) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		_, ok := r[fmt.Sprintf("%v", left)]
		return ok, nil
	}
	return false, nil // TypeError → False
}

// evalBinary 二元运算（Python _evaluate_bin_op，expression.py:324-377；
// Mult/Pow 含护栏）。
func evalBinary(kind string, left, right any) (any, error) {
	// 字符串拼接/重复优先（Python str.__add__/__mul__）
	if kind == "+" {
		if ls, ok := left.(string); ok {
			if rs, ok2 := right.(string); ok2 {
				return ls + rs, nil
			}
		}
	}
	if kind == "*" {
		if ls, ok := left.(string); ok {
			if ri, ok2 := toInt(right); ok2 {
				return strings.Repeat(ls, ri), nil
			}
		}
		if rs, ok := right.(string); ok {
			if li, ok2 := toInt(left); ok2 {
				return strings.Repeat(rs, li), nil
			}
		}
		// 列表乘法护栏（expression.py:334-347）
		if llist, ok := left.([]any); ok {
			if ri, ok2 := toInt(right); ok2 && ri > 0 && len(llist)*ri > constants.MaxCollectionSize {
				return nil, evalErrAny("operation would create collection exceeding maximum size of " +
					strconv.Itoa(constants.MaxCollectionSize))
			}
		}
		if rlist, ok := right.([]any); ok {
			if li, ok2 := toInt(left); ok2 && li > 0 && len(rlist)*li > constants.MaxCollectionSize {
				return nil, evalErrAny("operation would create collection exceeding maximum size of " +
					strconv.Itoa(constants.MaxCollectionSize))
			}
		}
	}
	// Pow 指数护栏（expression.py:353-361）
	if kind == "**" {
		if _, lok := toFloat(left); lok {
			if rf, rok := toFloat(right); rok && rf > 100 {
				return nil, evalErrAny("exponent too large in power operation")
			}
		}
	}
	// 数值双轨：全 int 走 int，否则 float
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if !lok || !rok {
		return nil, evalErrAny(fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'",
			kind, typeName(left), typeName(right)))
	}
	li, lInt := toInt(left)
	ri, rInt := toInt(right)
	bothInt := lInt && rInt
	switch kind {
	case "+":
		if bothInt {
			return li + ri, nil
		}
		return lf + rf, nil
	case "-":
		if bothInt {
			return li - ri, nil
		}
		return lf - rf, nil
	case "*":
		if bothInt {
			return li * ri, nil
		}
		return lf * rf, nil
	case "//":
		if bothInt {
			return li / ri, nil // Python int // int 截断除
		}
		return math.Floor(lf / rf), nil
	case "%":
		if bothInt {
			return li % ri, nil
		}
		return math.Mod(lf, rf), nil
	case "/":
		return lf / rf, nil // Python 真除法恒 float
	case "**":
		return math.Pow(lf, rf), nil
	case "<<":
		return li << uint(ri), nil
	case ">>":
		return li >> uint(ri), nil
	case "|":
		return li | ri, nil
	case "^":
		return li ^ ri, nil
	case "&":
		return li & ri, nil
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported binary operator: %s", kind))
}

// evalUnary 一元运算（Python _evaluate_unary_op，expression.py:307-320）。
func evalUnary(kind string, v any) (any, error) {
	switch kind {
	case "not":
		return !truthy(v), nil
	case "-":
		if i, ok := toInt(v); ok {
			return -i, nil
		}
		if f, ok := toFloat(v); ok {
			return -f, nil
		}
		return nil, evalErrAny("bad operand type for unary -")
	case "+":
		if i, ok := toInt(v); ok {
			return i, nil
		}
		if f, ok := toFloat(v); ok {
			return f, nil
		}
		return nil, evalErrAny("bad operand type for unary +")
	case "~":
		i, ok := toInt(v)
		if !ok {
			return nil, evalErrAny("bad operand type for unary ~")
		}
		return ^i, nil
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported unary operator: %s", kind))
}

// evalSubscript 下标/切片（Python _evaluate_subscript，expression.py:460-489；
// 尺寸/切片跨度护栏）。
func evalSubscript(n *subscriptNode, rt map[string]any) (any, error) {
	value, err := evaluateAST(n.value, rt)
	if err != nil {
		return nil, err
	}
	if l, lerr := safeLen(value); lerr == nil && l > constants.MaxCollectionSize {
		return nil, evalErrAny("collection size exceeds maximum allowed size of " +
			strconv.Itoa(constants.MaxCollectionSize))
	}
	if n.slice != nil {
		var lower, upper, step any
		if n.slice.lower != nil {
			if lower, err = evaluateAST(n.slice.lower, rt); err != nil {
				return nil, err
			}
		}
		if n.slice.upper != nil {
			if upper, err = evaluateAST(n.slice.upper, rt); err != nil {
				return nil, err
			}
		}
		if n.slice.step != nil {
			if step, err = evaluateAST(n.slice.step, rt); err != nil {
				return nil, err
			}
		}
		if li, ok := toInt(lower); ok {
			if ui, ok2 := toInt(upper); ok2 && ui-li > constants.MaxCollectionSize {
				return nil, evalErrAny("slice operation would create collection exceeding maximum size of " +
					strconv.Itoa(constants.MaxCollectionSize))
			}
		}
		return pythonSlice(value, lower, upper, step)
	}
	index, err := evaluateAST(n.index, rt)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case []any:
		i, ok := toInt(index)
		if !ok {
			return nil, evalErrAny("list indices must be integers")
		}
		if i < 0 {
			i += len(v)
		}
		if i < 0 || i >= len(v) {
			return nil, evalErrAny("list index out of range")
		}
		return v[i], nil
	case string:
		i, ok := toInt(index)
		if !ok {
			return nil, evalErrAny("string indices must be integers")
		}
		if i < 0 {
			i += len(v)
		}
		if i < 0 || i >= len(v) {
			return nil, evalErrAny("string index out of range")
		}
		return string(v[i]), nil
	case map[string]any:
		out, ok := v[fmt.Sprintf("%v", index)]
		if !ok {
			return nil, evalErrAny(fmt.Sprintf("KeyError: '%v'", index))
		}
		return out, nil
	}
	return nil, evalErrAny(fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

// evalAttribute 属性访问（Python _evaluate_attribute，expression.py:492-523：
// dunder 封禁 + 敏感属性表 + getattr→dict 回落）。
func evalAttribute(n *attrNode, rt map[string]any) (any, error) {
	value, err := evaluateAST(n.value, rt)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(n.attr, "__") && strings.HasSuffix(n.attr, "__") {
		return nil, evalErrAny(fmt.Sprintf(
			"disallowed operation: access to special attribute '%s' is prohibited", n.attr))
	}
	for _, s := range []string{"__class__", "__bases__", "__subclasses__", "__module__", "__dict__"} {
		if n.attr == s {
			return nil, evalErrAny(fmt.Sprintf(
				"disallowed operation: Access to sensitive attribute '%s' is prohibited", n.attr))
		}
	}
	if m, ok := value.(map[string]any); ok {
		if v, ok := m[n.attr]; ok {
			return v, nil
		}
	}
	return nil, evalErrAny(fmt.Sprintf("'dict' object has no attribute '%s'", n.attr))
}

// ──────────────────────────── 辅助 ────────────────────────────

// truthy Python truthiness（None/0/空串/空容器为假；bool 直取）。
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// toInt 数值收 int（float 仅整值时成立）
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	}
	return 0, false
}

// toFloat 数值收 float
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// pyEqual 值相等（跨数值双轨；bool 归一数值对齐 Python True == 1 恒真）。
func pyEqual(a, b any) bool {
	if ab, ok := a.(bool); ok {
		a = boolToNum(ab)
	}
	if bb, ok := b.(bool); ok {
		b = boolToNum(bb)
	}
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
		return false
	}
	if as, ok := a.(string); ok {
		bs, ok2 := b.(string)
		return ok2 && as == bs
	}
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func boolToNum(b bool) any {
	if b {
		return 1
	}
	return 0
}

func intCompare(l, r int, op string) bool {
	switch op {
	case "<":
		return l < r
	case "<=":
		return l <= r
	case ">":
		return l > r
	default:
		return l >= r
	}
}

func floatCompare(l, r float64, op string) bool {
	switch op {
	case "<":
		return l < r
	case "<=":
		return l <= r
	case ">":
		return l > r
	default:
		return l >= r
	}
}

func stringCompare(l, r string, op string) bool {
	switch op {
	case "<":
		return l < r
	case "<=":
		return l <= r
	case ">":
		return l > r
	default:
		return l >= r
	}
}

// wrapLen/wrapBool/wrapSum/wrapIsEmpty/wrapIsNotEmpty 把 Go 多返回值适配成
// callNode 可调用的 func([]any) (any, error) 形态。
func wrapLen(args []any) (any, error) {
	if len(args) != 1 {
		return nil, evalErrAny("len() takes exactly one argument")
	}
	n, err := safeLen(args[0])
	if err != nil {
		return nil, err
	}
	return n, nil
}

func wrapBool(args []any) (any, error) {
	if len(args) != 1 {
		return nil, evalErrAny("bool() takes exactly one argument")
	}
	return truthy(args[0]), nil
}

func wrapSum(args []any) (any, error) {
	// Python sum(iterable)：单列表参数时对元素求和；多参数为多实参形态
	if len(args) == 1 {
		if list, ok := args[0].([]any); ok {
			args = list
		}
	}
	if len(args) == 0 {
		return nil, evalErrAny("sum() requires at least one argument")
	}
	total := 0.0
	allInt := true
	for _, a := range args {
		f, ok := toFloat(a)
		if !ok {
			return nil, evalErrAny("sum() operands must be numeric")
		}
		if _, isInt := toInt(a); !isInt {
			allInt = false
		}
		total += f
	}
	if allInt {
		s := 0
		for _, a := range args {
			iv, _ := toInt(a)
			s += iv
		}
		return s, nil
	}
	return total, nil
}

func wrapIsEmpty(args []any) (any, error) {
	if len(args) != 1 {
		return nil, evalErrAny("is_empty() takes exactly one argument")
	}
	b, err := safeIsEmpty(args[0])
	if err != nil {
		return nil, err
	}
	return b, nil
}

func wrapIsNotEmpty(args []any) (any, error) {
	if len(args) != 1 {
		return nil, evalErrAny("is_not_empty() takes exactly one argument")
	}
	return safeIsNotEmpty(args[0])
}

func evalErrAny(msg string) error {
	return exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg", msg))
}

// pythonSlice 切片（str 与 []any 两形态，负索引/步长对齐 Python 切片规则）。
func pythonSlice(value, lower, upper, step any) (any, error) {
	switch v := value.(type) {
	case string:
		out, err := sliceIndices(len(v), lower, upper, step)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		for _, i := range out {
			b.WriteByte(v[i])
		}
		return b.String(), nil
	case []any:
		out, err := sliceIndices(len(v), lower, upper, step)
		if err != nil {
			return nil, err
		}
		res := make([]any, 0, len(out))
		for _, i := range out {
			res = append(res, v[i])
		}
		return res, nil
	}
	return nil, evalErrAny(fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

// sliceIndices 切片索引归一（Python slice 规则：负索引回绕、越界钳制、步长方向）。
func sliceIndices(n int, lower, upper, step any) ([]int, error) {
	st := 1
	if step != nil {
		si, ok := toInt(step)
		if !ok || si == 0 {
			return nil, evalErrAny("slice step cannot be zero")
		}
		st = si
	}
	norm := func(v any, d int) int {
		if v == nil {
			return d
		}
		i, _ := toInt(v)
		if i < 0 {
			i += n
			if i < 0 {
				if st < 0 {
					i = -1
				} else {
					i = 0
				}
			}
		}
		if i > n {
			i = n
		}
		return i
	}
	var out []int
	if st > 0 {
		lo, hi := norm(lower, 0), norm(upper, n)
		for i := lo; i < hi; i += st {
			out = append(out, i)
		}
	} else {
		lo, hi := norm(lower, n-1), norm(upper, -1)
		for i := lo; i > hi; i += st {
			out = append(out, i)
		}
	}
	return out, nil
}
