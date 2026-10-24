package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wk8/go-ordered-map/v2"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JSONParser JSON 文档解析器。
//
// 嵌入 BaseParser 仅覆写 ParseContent（对齐 Python JSONParser 仅覆写 _parse）。
// 软失败语义：读文件/内部失败返回 ("", nil)（对齐 Python except → return None），
// BaseParser.Parse 收空串自然产空切片（13.10 决策 4 子类自治首次实战）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py (JSONParser)
type JSONParser struct {
	// BaseParser 默认适配器（Parse/LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 JSONParser 实现 Parser
var _ Parser = (*JSONParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONParser 构造 JSONParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）；NewBaseParser(self)
// 注入使模板方法动态派发到本类 ParseContent。
//
// Python: json_parser.py JSONParser.__init__
func NewJSONParser() (*JSONParser, error) {
	p := &JSONParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 解析 JSON 文件内容。
//
// 对齐 Python _parse 步骤顺序：
//  1. 读文件（utf-8 容错，对齐 aiofiles.open(encoding="utf-8", errors="ignore")）
//  2. json.Unmarshal 失败 → Error 日志 + 返回原始内容
//     （对齐 JSONDecodeError 分支 return raw_content）
//  3. 成功 → 两空格缩进序列化（对齐 json.dumps indent=2）+ HTML 字符不转义
//     （SetEscapeHTML(false) 对齐 ensure_ascii=False 的原样输出语义）
//  4. 整体失败 → ("", nil) 软失败 + Error 日志（对齐外层 except return None）
//
// Python: json_parser.py JSONParser._parse
func (p *JSONParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	_ = ctx // 对齐 Python 无 ctx 消费
	_ = llmClient
	raw, err := os.ReadFile(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse JSON {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse JSON")
		return "", nil
	}
	var data any
	if err := decodeJSONPreserving(raw, &data); err != nil {
		// 对齐 Python: logger.error(f"JSON format error: {file_path}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Msg("JSON format error")
		return string(raw), nil
	}
	// S-05/M-11: 自定义编码器对齐 json.dumps(indent=2, ensure_ascii=False)。
	// json.Encoder 无法使用——wk8 orderedmap.MarshalJSON 内部走标准
	// json.Marshal（默认 HTML 转义），外层 SetEscapeHTML(false) 穿不透。
	out, err := encodeJSONPreserving(data)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse JSON {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse JSON")
		return "", nil
	}
	return out, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeJSONPreserving 保真解码 JSON：插入序 + 大整数 + 浮点字面三保真。
//
// S-05/M-11: 对齐 Python json.loads → json.dumps(indent=2, ensure_ascii=False)
// 的往返语义。json.Unmarshal 默认 float64 数值化导致两类失真：
//  1. 大整数精度丢失（12345678901234567890 → 12345678901234567000）
//  2. 浮点字面漂移（1.0 → 1，Python 保持 "1.0"）
//  3. map 键按字母序输出而非插入序
//
// 解法：Decoder.UseNumber() + Token 流逐 key 构造 *orderedmap.OrderedMap
// （值保持 json.Number），wk8 MarshalJSON 按插入序输出，json.Number
// MarshalJSON 原样输出字面。
func decodeJSONPreserving(raw []byte, out *any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeJSONValue(dec)
	if err != nil {
		return err
	}
	// 校验流尾：应只剩空白（EOF）。有多余 JSON 值则报错（对齐 json.Unmarshal 行为）
	if _, err := dec.Token(); err != io.EOF {
		return errMultipleJSONValues
	}
	*out = v
	return nil
}

// decodeJSONValue 递归解码单个 JSON 值（Token 流）。
func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeJSONFromToken(dec, tok)
}

// encodeJSONPreserving 保真编码：插入序 + 数值字面 + HTML 不转义三保真。
//
// 对齐 Python json.dumps(indent=2, ensure_ascii=False) 的输出格式：
//   - 对象 key 按插入序输出（orderedmap）
//   - 数值保持 json.Number 原字面（大整数/1.0/1e2 均逐字保留）
//   - '<' '>' '&' 不转义（ensure_ascii=False）
//   - 空对象/空数组输出 "{}"/"[]"（无缩进换行）
//   - 缩进两空格、数组元素每项一行
//   - 控制字符仍转 \uXXXX（对齐 Python CPython 实现行为）
func encodeJSONPreserving(v any) (string, error) {
	var buf bytes.Buffer
	if err := encodeJSONValue(&buf, v, 0); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// encodeJSONValue 递归编码单个 JSON 值到 buf。indent 为当前缩进层级。
func encodeJSONValue(buf *bytes.Buffer, v any, indent int) error {
	switch tv := v.(type) {
	case *orderedmap.OrderedMap[string, any]:
		if tv.Len() == 0 {
			buf.WriteString("{}")
			return nil
		}
		// 对齐 Python 缩进格式：{\n<indent+1>"key": value,\n...<indent>}
		buf.WriteString("{\n")
		first := true
		for el := tv.Oldest(); el != nil; el = el.Next() {
			if !first {
				buf.WriteString(",\n")
			}
			first = false
			writeIndent(buf, indent+1)
			encodeJSONString(buf, el.Key)
			buf.WriteString(": ")
			if err := encodeJSONValue(buf, el.Value, indent+1); err != nil {
				return err
			}
		}
		buf.WriteString("\n")
		writeIndent(buf, indent)
		buf.WriteString("}")
	case []any:
		if len(tv) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, item := range tv {
			if i > 0 {
				buf.WriteString(",\n")
			}
			writeIndent(buf, indent+1)
			if err := encodeJSONValue(buf, item, indent+1); err != nil {
				return err
			}
		}
		buf.WriteString("\n")
		writeIndent(buf, indent)
		buf.WriteString("]")
	case json.Number:
		// 数值字面逐字保留（S-05 核心：大整数不经 float64 往返）
		buf.WriteString(tv.String())
	case string:
		encodeJSONString(buf, tv)
	case bool:
		if tv {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		// 防御：未知类型兜底走标准编码（不转义 HTML）
		b, err := json.Marshal(tv)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

// encodeJSONString 编码 JSON 字符串（对齐 Python ensure_ascii=False 转义集）。
//
// Python json.encoder.py_encode_basestring：仅转义 '"' '\\' 和
// < 0x20 控制字符（\b \f \n \r \t 之外的用 \uXXXX）；'/' '<' '>' '&'
// 及非 ASCII 均原样输出。
func encodeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// writeIndent 写入 n 层两空格缩进。
func writeIndent(buf *bytes.Buffer, n int) {
	for i := 0; i < n; i++ {
		buf.WriteString("  ")
	}
}

// decodeJSONFromToken 基于已消费的 token 继续解码。
func decodeJSONFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch tv := tok.(type) {
	case json.Delim:
		switch tv {
		case '{':
			om := orderedmap.New[string, any]()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				om.Set(key, val)
			}
			// 消费收尾 '}'
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return om, nil
		case '[':
			arr := make([]any, 0, 8)
			for dec.More() {
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			// 消费收尾 ']'
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
	case json.Number:
		return tv, nil
	}
	// string/bool/nil 直接返回
	return tok, nil
}

// errMultipleJSONValues 多余 JSON 值错误（对齐 json.Unmarshal 多值报错行为）。
var errMultipleJSONValues = errors.New("multiple JSON values in input")

// init 注册到 registry（对齐 Python @register_parser([".json", ".JSON"]) import 时执行）。
func init() {
	RegisterParser([]string{".json", ".JSON"}, func() Parser {
		p, _ := NewJSONParser()
		return p
	})
}
