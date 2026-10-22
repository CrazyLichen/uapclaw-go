package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"

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
	if err := json.Unmarshal(raw, &data); err != nil {
		// 对齐 Python: logger.error(f"JSON format error: {file_path}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Msg("JSON format error")
		return string(raw), nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		// 对齐 Python: logger.error(f"Failed to parse JSON {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse JSON")
		return "", nil
	}
	// json.Encoder.Encode 尾部追加 \n，Python json.dumps 无尾换行——TrimSuffix 消除差异
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".json", ".JSON"]) import 时执行）。
func init() {
	RegisterParser([]string{".json", ".JSON"}, func() Parser {
		p, _ := NewJSONParser()
		return p
	})
}
