# 13.15 Image Parser + Captioner 设计文档

- 日期：2026-10-23
- 计划条目：IMPLEMENTATION_PLAN.md 13.15（Image Parser + Captioner）
- Python 源：`openjiuwen/core/retrieval/indexing/processor/parser/image_parser.py`（57 行）· `captioner.py`（143 行）
- Go 目标包：`internal/agentcore/retrieval/indexing/processor/parser/`（13.10 已建，13.16 已交付两解析器 + registry）
- 前置事实：Parser 层换序二次修订——原链 13.16→13.12→13.13→13.14→13.15→13.11 按编号惯性未查 captioner 消费方向；实际 13.12（PDF）/13.13（Word）均硬依赖 `ImageCaptioner.caption_images`，本节整体提前，新链 **13.16 → 13.15 → 13.12 → 13.13 → 13.14 → 13.11**（2026-10-23 用户拍板方案A）

## 1. 背景与定位

Agent 会话流程位置（不在会话主循环内，是会话上游的知识注入侧——KB 入库管线）：

```
【入库管线】用户向知识库添加文件（KB 构建或会话中 add_file 工具触发）
  → KnowledgeBase.parse_files/parse_urls（KB 层，后续章节）
  → AutoParser 路由（13.11，按扩展名分发）
    └─ .png/.jpg/.jpeg/.webp/.gif/.jfif → ImageParser（★ 本节）
         ├─ CpImage 归档：复制原图到 ./images/（留存）
         ├─ LLM(VLM) 生成图片文字描述（caption）
         └─ Document(text=caption, metadata={"image_path": 归档路径})
  → Chunker（13.5-13.9 ✅）
  → ComputeChunkEmbeddings（13.4 ✅）
       └─ isImageChunk 检测 metadata["image_path"] ← ★ 本节是该键的生产者（管道已建、供数者缺位）
            ├─ MultimodalEmbedder.EmbedMultimodal（图片本体嵌入）
            └─ 或 caption 文本嵌入（use_caption_for_images=true 路径）
  → Indexer 写入向量库（13.2/13.3 ✅）
  → 【会话侧】检索工具查库 → chunk.text（caption）注入上下文
```

作用：让图片成为知识库可检索的一等公民，双通道保障（caption 文本嵌入 + 图片本体嵌入）。本节兑现 13.10 `ParseOptions.LLMClient` 的"13.15 Captioner 消费"预留，接通 13.4 多模态嵌入管道。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | 章节顺序 | **方案A：13.15 整体提前到 13.12 之前**（新链 13.16→13.15→13.12→13.13→13.14→13.11） | 否决 B（13.12 带出 captioner.py：带出的是 13.15 主体文件之一，13.15 编号意义被掏空）；否决 C（拆两半：IMPLEMENTATION_PLAN 状态标记别扭）。13.15 自身零前置依赖（llm.Model/BaseParser/registry 全就绪），13.12/13.13 直接消费现成 ImageCaptioner 无回填点 |
| 2 | LLM 测试策略 | **方案A：`llm.NewModel + WithClient(fakeClient)` 走真实 `Model.Invoke` 全链**（含回调框架/消息转换），fakeClient 断言收到的 messages 里 image_url data URI 正确 | 否决 B（Captioner 侧加非 Python 对应物的注入接口：偏离一比一对齐）；否决 C（真实调用 build tag 隔离：base64/消息构造失去单测覆盖，违覆盖率基线） |
| 3 | 双 cp_image 冗余 | **方案B：去重——Parse 单次 CpImage，ParseContent 复用归档路径** | 否决 A（字面保留双调用：warn 打两遍+文件复制两次的可观察冗余）。与 10.5.10 死代码保留先例的边界：零调用死代码保留；**重复执行**去重。语义等价论证：caption 读文件内容，副本≡原件 |
| 4 | 去重落点 | **B1：Parse 把归档路径传给 ParseContent（全程 caption 副本）** | 否决 B2（ParseContent 收原始路径内部不复制：caption 读原件非副本，语义漂移）；否决 B3（Parse 内联、ParseContent 保留原样：两方法语义割裂，测试要覆盖两条不一致路径） |
| 5 | CpImage 签名 | **包级导出函数 `CpImage(imageLoc string) (string, error)`，targetDir 定死 savedImageDir 常量** | Python `cp_image(image_loc, target_dir=SAVED_IMAGE_DIR)` static method；全 codebase 无非默认 targetDir 调用（grep 核实），加参数即死代码 |
| 6 | 禁用态语义 | **caption 禁用（nil client）≠ 解析失败：Parse 仍返回单 Document（text 空、image_path 有值）** | Python 字面行为：`_parse` 返回 ""（非 None）→ parse 照样包 Document。空文本 chunk 流到 13.4 靠 image_path 走纯多模态嵌入。**不可**照抄 BaseParser"空内容→空切片" |
| 7 | 提示词 | **IMAGE_CAPTION_PROMPT 英文逐字复制** | 既定规则：禁止翻译改写（记忆 feedback_prompt_verbatim_copy） |
| 8 | 注册 | **image_parser.go init() 调 RegisterParser 六扩展名** | 13.16 既定模式：Go 无装饰器，init() 对齐 Python 装饰器 import 时执行 |

## 3. 依赖验证结论（讨论期间核实）

- `*llm.Model`：13.10 base.go 已 import（`ParseOptions.LLMClient` 预留兑现），零新增依赖方向
- 多模态消息链路全部现成：`NewMessagesParam(&UserMessage{...})` + `NewMultiModalContent(ContentPart{Type:"image_url", ImageURL:...})` + `MessageContent.MarshalJSON`（parts 数组序列化）+ `ConvertMessagesToDict`（dict 透传）——parser 包零转换代码
- `resp.Content.Text()`：取 AssistantMessage 文本的项目惯用方式（evaluator/llm_wrapper 等广泛先例）
- `.jfif` MIME：`mime.AddExtensionType(".jfif","image/jpeg")`——retrieval/common/document.go init 已注册（幂等无害），本节字面对齐 captioner.py 模块级 `mimetypes.add_type` 再注册一次
- `llm.NewModel` + `WithClient`：测试注入 mock 的现成通道（model_clients_test.go 先例）
- 依赖方向：parser → llm（foundation）维持 13.10 现状，parser 仍是叶子零循环
- 日志组件：`logComponent = logger.ComponentAgentCore`（13.16 registry.go 既定）
- Python 两文件 logger 对照：captioner 7 条（构造 warn×2 分支 / cp_image 复制失败 warn / mime fallback warn / 调用 info / LLM 失败 warn / caption_images 文件不存在 warn）+ image_parser 2 条 error（parse except / _parse except）——Go 全部同步（偏差 1/5 去重后 warn 单遍）

## 4. 类型与接口设计

### 4.1 captioner.go

```go
// 常量区块
const (
    // imageCaptionPrompt 图片描述生成提示词（Python 逐字复制，禁止翻译）。
    imageCaptionPrompt = "You are an assistant specialized in document and image analysis. " +
        "Your task is to provide a detailed, qualitative description of the provided image " +
        "so that it can be embedded and used for semantic retrieval. " +
        "Describe all visible content including text, figures, charts, tables, diagrams, and layout. " +
        "Focus on what the image conveys and means, not just what it literally depicts. " +
        "Do not include any preamble — output only the description."
    // savedImageDir 归档图片目录（相对路径，Python SAVED_IMAGE_DIR）。
    savedImageDir = "images"
)

// 全局变量区块
var (
    // supportedLLMClient 支持图片 captioning 的模型名前缀白名单。
    supportedLLMClient = []string{"gpt-4o", "gpt-5", "qwen3-vl", "qwen-vl"}
)

// ImageCaptioner 图片描述生成器。
type ImageCaptioner struct {
    // llmClient 可选 LLM 客户端（nil = 禁用态，仅归档不 caption）。
    llmClient *llm.Model
}

// NewImageCaptioner 构造（构造即调 isLLMSupported，None/不匹配仅 warn 不报错）。
func NewImageCaptioner(llmClient *llm.Model) *ImageCaptioner

// init 注册 .jfif MIME（幂等，字面对齐 Python 模块级 mimetypes.add_type）。
func init()

// CpImage 复制图片到 savedImageDir 并返回目标路径。
// 分支：不存在→err；MkdirAll 失败→err；源=目标跳过；复制失败→Warn+返回原路径+nil（软）。
// Python: captioner.py ImageCaptioner.cp_image（static method）
func CpImage(imageLoc string) (string, error)

// isLLMSupported 检查 LLM 客户端对图片 captioning 的支持度（仅 warn）。
func (c *ImageCaptioner) isLLMSupported()

// llmCall 生成单图 caption：mime 检测→base64→多模态消息→Invoke→Content.Text()。
// 软失败全分支返回 ""。
// Python: captioner.py _llm_call_async
func (c *ImageCaptioner) llmCall(ctx context.Context, imageLoc string) string

// CaptionImages 逐图串行生成 caption 列表；文件不存在→Warn+""。
// Python: captioner.py caption_images
func (c *ImageCaptioner) CaptionImages(ctx context.Context, imageLocs []string) []string
```

对齐细节：
- `cp_image` 的 `shutil.copy2` → `os.ReadFile + os.WriteFile`（copy2 保留文件元数据，caption 场景只读内容，元数据无意义——偏差归档 §8）
- `filepath.Join(savedImageDir, filepath.Base(imageLoc))` 对齐 `os.path.join`（平台分隔符语义一致）
- 软失败分支的 Python f-string `{self.llm_client=}` 自省语法 → Go `Str("llm_client", "nil")` 近似（偏差归档 §8）
- Python warn `f"LLM-based caption for {image_loc=} with invocation failed for {image_loc}: {e}"` 变量冗余 → Go 字段不重复（偏差归档 §8）

### 4.2 image_parser.go

```go
// ImageParser 图片解析器（嵌入 BaseParser，覆写整体 Parse 产出 image_path 元数据；
// LazyParse/Process/Supports 继承默认）。
type ImageParser struct{ *BaseParser }

// NewImageParser 构造（零参数，对齐 __init__(**kwargs): pass）。
func NewImageParser() (*ImageParser, error)

// init 注册六扩展名（对齐 @register_parser 装饰器 import 时执行）。
func init()

// Parse 覆写整体解析（非 ParseContent——产出 metadata 需要）：
//  1. CpImage 单次归档（决策 3/4 去重+B1）
//  2. self.ParseContent(ctx, savedPath, opts.LLMClient)（归档路径直通，B1）
//  3. 无条件返回单 Document{ID: opts.DocID, Text: content, Metadata: {"image_path": savedPath}}
//     ——空 caption 也返回（决策 6 禁用态语义）
//  4. CpImage err → Error 日志 + 空切片（软失败，对齐 except → []）
// Python: image_parser.py ImageParser.parse
func (p *ImageParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error)

// ParseContent 对齐 _parse（B1 改形：doc = 归档路径）：
//  1. NewImageCaptioner(llmClient)（单次构造，warn 单遍）
//  2. CaptionImages(ctx, []string{doc})
//  3. 非空 caption 过滤后 "\n".join；空 → ""（对齐 None falsy）
// Python: image_parser.py ImageParser._parse
func (p *ImageParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error)
```

对齐细节：
- Python `_parse` 的第二次 cp_image（返回值丢弃的冗余调用）按决策 3 去重移除；行为差异（warn 单遍、复制一次）归档 §8
- Python `if captions else None`：caption_images 总返回等长列表，单图场景"空列表"不可达；可达的空是 caption 空串被过滤后 `"\n".join([])=""` → Go `strings.Join` 同语义
- registry 工厂吞错返回 nil 与 13.16 parserTypeName nil 防御先例一致

## 5. 文件布局

```
parser/                          # 13.10/13.16 已建包
├── doc.go                       # 更新：文件目录追加 2 条目（captioner/image_parser）
├── base.go                      # 13.10 交付（零改动；LLMClient 预留本节兑现）
├── registry.go                  # 13.16 交付（零改动；六扩展名注册消费）
├── json_parser.go               # 13.16 交付（零改动）
├── txt_md_parser.go             # 13.16 交付（零改动）
├── captioner.go                 # ★ 新增：ImageCaptioner + CpImage + 提示词常量
├── captioner_test.go            # ★ 新增：~10 用例
├── image_parser.go              # ★ 新增：ImageParser + init 注册
└── image_parser_test.go         # ★ 新增：~6 用例
```

- 外部依赖：零新增（llm/schema/base64/mime 全现成）
- 声明顺序/中文注释/doc.go 更新按规则 1、2
- IMPLEMENTATION_PLAN.md：13.15 ☐→✅ + 产出列摘要（备注换序：13.15 提前至 13.12 前实施）；13.12/13.13 行备注 ImageCaptioner 已就绪

## 6. 测试设计

| 文件 | 用例 |
|------|------|
| captioner_test.go | 构造×3（nil/匹配名/不匹配名不报错）/ CpImage×4（正常复制内容相等/不存在 err/源=目标跳过/复制失败 fallback 原路径）/ CaptionImages×2（文件不存在→[""]/单图成功）/ llmCall×4（nil client→""/mime fallback/Invoke err→""/成功取 Content.Text()） |
| image_parser_test.go | Parse 正常（单 Document：text=caption、metadata.image_path=归档路径、ID=opts.DocID）/ **禁用态：nil LLMClient 仍单 Document（text 空、image_path 有值）★核心用例** / cp_image 失败→空切片+nil error / ParseContent 归档路径直通 CaptionImages（B1：断言 fakeClient 收到的文件内容=副本）/ 注册：六扩展名在 registry（snapshotRegistry）/ LazyParse/Process 转发继承 |

- mock 方案（决策 2）：`llm.NewModel(cfg, cfg, WithClient(fakeClient))`，fakeClient 实现 `model_clients.BaseModelClient`（model_clients_test.go 先例），Invoke 内断言 messages dict：role=user、content 数组两项、text part 含提示词前缀、image_url part 的 data URI 解码后等于 fixture PNG 字节
- fixture：1×1 PNG 真实字节（t.TempDir() 下写文件）
- 零 build tag（规范 3.3：LLM mock 走接口注入，文件系统走 t.TempDir()）
- 覆盖率目标：两新文件 ≥90%（13.16 的 90.2% 先例水平），包整体只升不降
- 验证命令：`go test -race -cover ./internal/agentcore/retrieval/indexing/processor/parser/...`

## 7. 实施后状态同步清单（规则 2）

- IMPLEMENTATION_PLAN.md 13.15 ☐→✅ + 产出摘要（含决策 3 去重、B1 落点标注）
- 13.12/13.13 行注：ImageCaptioner 已就绪（⤴️ 13.15）
- parser/doc.go 文件目录 2 条目追加
- go.mod：零改动（无新增依赖）
- 记忆 `parser-reorder-13.11.md` 补实施进度

## 8. 偏差归档（对齐 Python 的已知差异）

| # | 偏差 | 理由 |
|---|------|------|
| 1 | 双 cp_image 去重：Parse 单次复制，ParseContent 复用归档路径（B1） | 用户拍板（决策 3/4）；语义等价：caption 读文件内容，副本≡原件。可观察差异：`_is_llm_supported` warn 单遍（Python 双遍）、文件复制一次（Python 两次） |
| 2 | `shutil.copy2` → `os.ReadFile+os.WriteFile`（不保留文件元数据） | caption 场景只读内容；归档副本的元数据对下游（13.4 多模态嵌入读内容）无意义 |
| 3 | Python `{self.llm_client=}` f-string 自省语法 → Go `Str("llm_client", "nil")` | Go 无等价自省；字段语义保留 |
| 4 | Python LLM 失败 warn 的 `{image_loc=}` 重复变量 → Go 字段单次 | Python 日志本身冗余，Go 侧字段不重复 |
| 5 | `_parse` 内二次构造 ImageCaptioner 移除（并入 ParseContent 单次构造） | 决策 3 去重的组成部分；warn 单遍为可观察差异 |
| 6 | Python parse 的 try 包裹整个流程（含 Document 构造）→ Go 仅 CpImage/ParseContent err 检查 | Document 构造无 panic 路径，err 传播链等价 |

## 9. 审查要点预案

- 声明顺序（规则 2）：captioner.go 按常量→var→init；image_parser.go 按结构体→var（编译期断言）→导出→init
- 日志（规则 3）：Python 6 条日志逐条对照（构造 warn×2 分支/mime fallback warn/调用 info/LLM 失败 warn/cp_image 复制失败 warn + image_parser 2 error），异常路径含 `.Err(err)`
- doc.go（规则 1）：2 条目 + 包概述微调（"图片解析 + caption 生成"入概述）
- 覆盖率（规范 3）：≥85% 硬线，本节预计 >90%
- any 审查：零新增破环 any（Metadata map[string]any 是 13.10 数据契约既定）
- 编译互斥（规则 5）：实施前 `pgrep -f 'go (build|test)'` 检查残留

## 10. 实施偏差记录（2026-10-23 审查回填）

实施与计划（docs/superpowers/plans/2026-10-23-image-parser-13.15.md）的 5 处偏差。前 4 项为实施期发现并修正的计划笔误/改进，第 5 项为实施后审查发现的不彻底修复，本节回填时已一并修复：

| # | 计划原文 | 实际实施 | 性质 | 处置 |
|---|---------|---------|------|------|
| 1 | `cfg, _ := llmschema.NewModelRequestConfig(...)`（双值接收） | `cfg := ...`（单值） | 计划笔误：函数实际单返回值，计划代码编译不过 | 无代码动作（实现即唯一正确写法） |
| 2 | 测试统一 `t.Context()` | 实施先用了 `context.Background()` | 计划自注的兼容选项之一 | 2026-10-23 审查后按用户拍板改回 `t.Context()`（10 处） |
| 3 | `TestCpImage_正常复制` 无 Chdir（计划明示容忍 images/ 落在测试工作目录） | 实施加了 Chdir(t.TempDir()) 防污染；但 `复制失败回落原路径` 漏防（该用例 Stat 通过后同样执行 MkdirAll("images")，且未切目录——实施照抄计划原文） | 实施改进但不彻底 | 2026-10-23 审查补齐该用例 Chdir+Cleanup 并清理源码目录残留 images/（空目录，测试运行产物） |
| 4 | image_parser_test.go 导入块含未使用的 `llm`（编译报错）且缺 B1 闭包需要的 `llmschema` | 去 `llm`、补 `llmschema` | 计划导入块笔误 | 无代码动作（实现即正确解） |
| 5 | `captionClient.invokeFn` 用内联函数类型字面量，`captionInvokeFn` 命名类型定义置于 `newCaptionModel` 之后 | 命名类型前置声明后再用于字段 | 声明顺序调整（两写法皆可编译） | 无代码动作（等价改进，可读性更佳） |

回填后回归：`go test -race -cover ./internal/agentcore/retrieval/indexing/processor/parser/` → ok，coverage 90.9%（基线不降），race 零报告，测试运行后源码目录零残留。
