# 13.16 JSON/TxtMd Parser 设计文档

- 日期：2026-10-22
- 计划条目：IMPLEMENTATION_PLAN.md 13.16（JSON/TxtMd Parser）
- Python 源：`openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py`（39 行）· `txt_md_parser.py`（47 行）· `auto_file_parser.py`（registry 部分，带出依赖）
- Go 目标包：`internal/agentcore/retrieval/indexing/processor/parser/`（13.10 已建）
- 前置事实：13.10 Parser 抽象接口已交付（Parser 五方法 + ParseOptions + BaseParser 模板适配器）；错误码 155103/155104 留给 13.11 AutoFileParser 消费（本节零消费）；Parser 层已换序——本节是具体解析器第一站，13.11 AutoParser 最后组装（记忆 `parser-reorder-13.11.md`）

## 1. 背景与定位

Parser 层按方案A换序：**13.16 → 13.12 → 13.13 → 13.14 → 13.15 → 13.11（组装）**。本节交付两个最简具体解析器（JSON/TxtMd），并带出 registry 骨架——Python `_PARSER_REGISTRY` + `register_parser` 装饰器定义在 `auto_file_parser.py`（13.11 的文件），按"带出依赖不改编号"先例（13.6 带出 text_splitter CharSplitter 部分、13.7 带出全部 splitter 包）归入本节。AutoFileParser 类本体（parse/supports/ensureParsersLoaded/registerNewParser/getSupportedFormats）留 13.11。

Agent 会话流程位置（不变，入库管线第一站的具体化）：

```
【入库管线】文件/链接 → AutoParser（13.11 组装层，未实现）
  → AutoFileParser → _PARSER_REGISTRY 按扩展名分发 ← ★ 13.16 提供两个注册项 + registry 本体
    ├─ .json/.JSON → JSONParser（★ 本节）
    └─ .txt/.TXT/.md/.MD/.markdown/.MARKDOWN → TxtMdParser（★ 本节）
  → KnowledgeBase.parse_files/parse_urls（后续 KB 层章节）
  → Chunker（13.5-13.9 ✅）→ Embed（13.4 ✅）→ Indexer（13.2/13.3 ✅）
```

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | 章节顺序 | **方案A换序：13.16 起步、13.11 最后组装** | 否决 B（守编号先做 13.11 骨架：默认注册表大部分为空的中间态、回填点多）；13.10 spec 决策 1 否决C的"依赖面过宽"障碍由此消除 |
| 2 | TxtMd 编码检测 | **方案A：saintfish/chardet 统计检测 + x/text 解码** | 否决 B（x/net DetermineEncoding：WHATWG HTML 嗅探，纯文本 GBK 兜底 windows-1252 → 中文乱码不可接受）；否决 C（移植 web_tools 多候选评分链：~100 行复制且"暴力全试"非"检测"语义）。chardet 与 charset-normalizer 同为统计检测，v0.0.0-20230101081208 已验证 goproxy.cn 可拉取，纯 Go 零传递依赖 BSD-3 |
| 3 | registry 归属 | **13.16 带出骨架：parser/registry.go（map + mutex + RegisterParser）；AutoFileParser 类本体留 13.11** | Python registry 与 AutoFileParser 同文件，按"带出依赖"先例拆分归属；否决统一注册文件 register.go（与 Python 装饰器分散注册语义有偏差） |
| 4 | 注册时机 | **各解析器文件 init() 调 RegisterParser** | 对齐 Python 装饰器在 import 时执行；13.11 ensureParsersLoaded 对应物届时改为直接 import（Go import 即触发 init） |
| 5 | 重复注册语义 | **RegisterParser first-wins：已存在扩展名静默跳过 + info 日志（字面）**；13.11 RegisterNewParser 届时按 last-wins 对齐 | Python 两处注册入口语义本就不同（register_parser 跳过 vs register_new_parser 覆盖），各自忠实对齐；与 chunker registry"重名报错"的差异是 Python 真实语义差异，非发明 |
| 6 | 工厂签名 | **`func() Parser`（对齐 Python `Callable[[], Parser]`）** | 值是工厂非实例，每次 parse 新实例化（无状态并发安全） |
| 7 | 软失败语义 | **`("", nil)` + Error 日志（子类自治，13.10 决策 4 首次实战）** | Python `_parse` try/except → None；BaseParser.Parse 收空串自然产空切片 |

## 3. 依赖验证结论（讨论期间核实）

- `saintfish/chardet v0.0.0-20230101081208-5e3ef4b5456d`：goproxy.cn 拉取成功（试探后已还原 go.mod，实施时正式引入）；go.sum 已有 hash 记录
- `golang.org/x/text`：已在依赖树（harness/web_tools 先例，charmap/simplifiedchinese/traditionalchinese/japanese 全可用）
- 依赖方向：parser 包零新增内部依赖（llm/reranker/context 维持 13.10 现状）；harness 与 retrieval 无互相依赖（`go list -deps` 双向核实为零）
- go.mod 头部分领域依赖注释清单需补"领域十三"条目（13.7 引入 sentencizer 同款惯例）
- 日志组件：`logComponent = logger.ComponentAgentCore`（chunker/indexer 层既定先例）
- Python 两文件 logger 对照：json_parser 2 条 error（格式错误分支/外层 except）、txt_md_parser 1 条 error（外层 except）、auto_file_parser 注册 1 条 info（带出部分）——Go 全部同步

## 4. 类型与接口设计

### 4.1 registry.go

```go
// 全局变量区块
var (
    // parserRegistry 扩展名 → 解析器工厂。并发访问由 parserRegistryMu 保护
    parserRegistry = map[string]func() Parser{}
    // parserRegistryMu registry 互斥锁（chunker registry 同款先例）
    parserRegistryMu sync.Mutex
)

// RegisterParser 按扩展名列表注册解析器工厂。
// 扩展名小写归一（".JSON"→".json"）；已存在的扩展名静默跳过（first-wins，
// 字面对齐 Python if normalized_ext not in _PARSER_REGISTRY）并记 info 日志。
// Python: auto_file_parser.py register_parser 装饰器
func RegisterParser(fileExtensions []string, factory func() Parser)

// getParserFactory 按扩展名取工厂。13.11 AutoFileParser 消费（届时按需评估导出）。
func getParserFactory(fileExt string) (func() Parser, bool)
```

- info 日志字段：Python `f"Registered parser {parser_class.__name__} for {normalized_ext}"` → Go `.Str("parser_name", "JSONParser").Str("ext", ext)`（Python 类名以 Go 类型名等价）
- 测试不 copy/restore 全局 map（Python 测试做法在 Go 并发下不安全），用独立扩展名（`.test`/`.custom`）注册验证，内置注册项不依赖不污染；`go test -race` 验证并发安全

### 4.2 json_parser.go

```go
// JSONParser JSON 文档解析器（嵌入 BaseParser 仅覆写 ParseContent）。
type JSONParser struct{ *BaseParser }

// NewJSONParser 构造（NewBaseParser(self) 注入，nil 防御对齐 13.10 决策 7）。
func NewJSONParser() (*JSONParser, error)

// ParseContent 对齐 _parse：
//  1. os.ReadFile 读文件（utf-8 容错：非法字节替换，对齐 errors="ignore"）
//  2. json.Unmarshal 到 any；失败 → Error 日志 + 返回原始内容（JSONDecodeError 分支）
//  3. 成功 → json.Encoder（MarshalIndent 两空格 + SetEscapeHTML(false)）
//  4. 整体失败 → ("", nil) 软失败
func (p *JSONParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error)
```

对齐细节：
- `json.dumps(indent=2)` ↔ `MarshalIndent(v, "", "  ")` 输出逐字节一致
- `ensure_ascii=False` ↔ Go 默认不转义非 ASCII ✓；Go 默认 HTML 转义 `<>&` 需 `SetEscapeHTML(false)` 消除（对齐 Python 输出）
- 日志 2 条：`.Str("file_path", ...)` 格式错误分支（Python 无 `{e}` 变量）与 `.Str("file_path", ...).Err(err)` 外层分支

### 4.3 txt_md_parser.go

```go
// TxtMdParser 文本/Markdown 解析器（嵌入 BaseParser 仅覆写 ParseContent）。
type TxtMdParser struct{ *BaseParser }

func NewTxtMdParser() (*TxtMdParser, error)

// ParseContent 对齐 _parse：
//  1. os.ReadFile 读原始字节
//  2. chardet.DetectBest 统计检测；失败/无结果 → utf-8（对齐 detected None 分支）
//  3. IANA 编码名映射 x/text encoding 解码（GB18030/Big5/Shift_JIS/windows-1252…）；
//     未知编码名 → utf-8 容错解码（对齐 open(encoding=...) 回落语义）
//  4. strings.TrimSpace；空串 → ("", nil)
//  5. 整体失败 → ("", nil) + Error 日志
func (p *TxtMdParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error)
```

对齐细节：
- Python `detect()` 三态返回（None/dict/CharsetMatch）多分支防御收敛为 chardet 单态 + 失败回落 utf-8（语义等价）
- `errors="ignore"`：Python 丢弃非法序列 vs Go x/text 解码器替换为 U+FFFD——差异归档不消除（对文本检索无实质影响）
- 空文件：Python `content=""` falsy → None → `[]`；Go 空串 → `("", nil)` → BaseParser 空切片 ✓
- BOM：实施时以真实行为验证并在测试固化（设计按"对齐 Python 最终输出"处理）
- 日志 1 条：外层 `.Str("file_path", ...).Err(err)`

## 5. 文件布局

```
parser/                          # 13.10 已建包
├── doc.go                       # 更新：文件目录追加 3 条目（registry/json_parser/txt_md_parser）
├── base.go                      # 13.10 交付（零改动）
├── base_test.go                 # 13.10 交付（零改动）
├── registry.go                  # ★ 新增：parserRegistry + RegisterParser（带出依赖）
├── registry_test.go             # ★ 新增：~6 用例
├── json_parser.go               # ★ 新增：JSONParser
├── json_parser_test.go          # ★ 新增：~9 用例
├── txt_md_parser.go             # ★ 新增：TxtMdParser
└── txt_md_parser_test.go        # ★ 新增：~8 用例
```

- 外部依赖：新增 chardet（唯一）；x/text 已在依赖树
- 声明顺序/中文注释/doc.go 更新按规则 1、2
- IMPLEMENTATION_PLAN.md：13.16 ☐→✅ + 产出列摘要（含 registry 带出标注）；13.11 行加 ⤴️ 指向 registry 已带出

## 6. 测试设计

| 文件 | 用例（对齐 Python 测试 + Go 惯例增强） |
|------|------|
| registry_test.go | 注册成功 / 多扩展名小写归一 / 重复扩展名静默跳过（first-wins 断言原工厂保留）/ 空扩展名列表防御 / 工厂返回实例类型断言 / 并发注册 race 安全 |
| json_parser_test.go | 构造 / 成功解析含中文 / 空对象 `{}` 输出字面 `{}` / 数组 / 非法 JSON 返回原文 / 文件不存在软失败空切片 / HTML 转义关闭断言（`<>&` 原样）/ WithDocID 透传 Document.ID / 经 registry 工厂实例化解析 |
| txt_md_parser_test.go | 构造 / 空文件空切片 / 不存在软失败 / strip 前后空白 / UTF-8 中文 / **GBK 编码文件正确解码**（chardet 核心价值场景）/ Big5 或 Shift_JIS / 纯 ASCII |

- 零 build tag：全部 `t.TempDir()` 临时文件（规范 3.3）
- 覆盖率目标：三个新文件 ≥90%（chunker 层 93.7%/93.8% 先例水平），包整体只升不降（基线 88.6%）
- 验证命令：`go test -race -cover ./internal/agentcore/retrieval/indexing/processor/parser/...`

## 7. 实施后状态同步清单（规则 2）

- IMPLEMENTATION_PLAN.md 13.16 ☐→✅ + 产出摘要（registry 骨架带出自 auto_file_parser.py，AutoFileParser 本体留 13.11）
- 13.11 行注 ⤴️：registry 已由 13.16 带出
- parser/doc.go 文件目录 3 条目追加
- go.mod：chardet 依赖 + 领域十三注释条目
- 记忆 `parser-reorder-13.11.md` 补实施进度（届时更新）

## 8. 审查要点预案

- 声明顺序（规则 2）：registry.go 的 var 区块含 mutex；解析器文件按 结构体→常量→var（编译期断言）→导出→非导出
- 日志（规则 3）：Python 4 条日志（json 2 error / txt 1 error / 注册 1 info）逐条对照，勿漏注册 info
- doc.go（规则 1）：三条目 + 包概述微调（"registry 骨架"入概述）
- 覆盖率（规范 3）：≥85% 硬线，本节预计 >90%
- any 审查：零新增 any（工厂签名具体类型；无 Extra 消费——file_name 消费在 13.11 AutoFileParser）
- 编译互斥（规则 5）：实施前 `pgrep -f 'go (build|test)'` 检查残留
