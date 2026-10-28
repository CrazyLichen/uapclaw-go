# 13.24 QueryRewriter 设计（查询改写器 + 历史压缩）

> 拍板（2026-10-28）：方案 A（go:embed + 包内私有实现，与 Python 目录同构）；维持编号序 13.24 → 13.25（两节互不依赖已核实）；方法签名带 ctx（`Rewrite(ctx, query)`，13.17 先例）；构造方案 A1（`NewQueryRewriter(cfg, ctx, opts...)` + `WithModelClientOption` 测试注入通道）；ssl_cert/verify_ssl/custom_headers 走 BaseModelInfo.Extra 防御性读取。

## 1. 定位与流程位置

领域 13 读取侧第五节。QueryRewriter 是检索管线最上游的**查询预处理**组件：把多轮对话中的省略/指代 query 改写成独立可检索的 standalone_query，并在历史超长时用 LLM 压缩历史控制 token 膨胀。

```
Agent 会话（每轮）
├─ ① QueryRewriter.Rewrite(用户原始 query)      ← 13.24 本节，检索前置
│     ├─ 历史条数 ≥ compressRange(默认 20)？
│     │     ├─ 是 → Compress()：LLM 压缩成 {theme, summary} JSON
│     │     │        → SetMessages([SystemMessage(name="compressed_history")])
│     │     │        → 失败降级：原文 Msg2Text 写回（name="original_history"）
│     │     └─ 否 → 跳过压缩
│     ├─ GetMessages(size=compressRange) 取最近 N 条防无限膨胀
│     └─ intention_completion 提示词填 {history}{query} → LLM
│           → {before, intention, standalone_query, references,
│              missing, typo, gibberish, from_history}
├─ ② 检索侧用 standalone_query（13.20/13.21/13.23）
└─ ③ 调用方 add_messages(user) → 回复后 add_messages(assistant)
     （Python docstring 明确约定：QR 不写当前轮消息，调用方负责）
```

**与 13.25 关系澄清**（已核查源码）：AgenticRetriever 的 `_rewrite` 是其私有提示词 `_REWRITE_PROMPT` 的多跳追问生成器，与 QueryRewriter 互不调用。13.24 域内零消费方；理论消费方为 8.25 KnowledgeRetrievalComp（未实施），Python 侧当前仅 showcase 示例脚本消费。

## 2. Python 源码基线

- 源码：`openjiuwen/core/retrieval/query_rewriter/query_rewriter.py`（525 行）
- 提示词：`prompts/` 4 个 md 共 276 行（compression_zh 54 / compression_en 55 / intention_completion_zh 83 / intention_completion_en 84）
- 测试：`tests/unit_tests/core/retrieval/query_rewriter/test_query_rewriter.py`（654 行，11 测试类约 30 用例）
- 依赖：`ModelContext`（context_engine）、`Model`/`BaseMessage`/`SystemMessage`（foundation.llm）、`JsonOutputParser`、`json_repair`、`ModelConfig`（mode_info）

### 2.1 模块级函数行为清单

| 项 | Python 行为 | Go 归宿 |
|---|---|---|
| `_fill_template(template, **kwargs)` | 逐 key `out.replace("{"+key+"}", value)`；只替换显式占位符，JSON 示例花括号不误伤 | `fillTemplate(template string, kv map[string]string) string`（strings.ReplaceAll 循环） |
| `_extract_json(model_output)` | `find("{")`/`rfind("}")`；`-1 或 end<=start` → Debug "No JSON object found in model output" + `""`；否则 Debug "JSON object from output extracted" + 切片 | `extractJSON(output string) string`（IndexByte/LastIndexByte；两条 Debug 日志对齐） |
| `_parse_llm_json(json_str)` | 空白 → None；`json.loads` 成 dict → 返回；失败 → `repair_json(json_str)` 再 loads（Debug "JSON parsed after repair_json"）；非 dict 根 → None；不抛异常 | `parseLLMJSON(jsonStr string) (map[string]any, bool)`（jsonrepair.RepairWithOptions，13.17 同款依赖） |
| `_force_string(raw)` | dict → `json.dumps(ensure_ascii=False)`，失败 Debug + `str(raw)`；其他 `str(raw)` | `forceString(raw any) string` |
| `_force_list(raw)` | list 直返；其他 `[raw]` | `forceList(raw any) []any` |
| `_force_json(key, raw)` | dict 直返；str → loads 成 dict 返/成功非 dict `{key: parsed}`/失败 `{key: raw}`；list/其他 → `{key: raw}` | `forceJSON(key string, raw any) map[string]any` |
| `_schema_repair(output, output_schema)` | 非 dict → 155604 "output must be a dict"；逐 schema 字段：None → 按期望类型填 `""`/`[]`/`{}`（其他类型 155604）；**"typo" 特判**（list 化 → 逐元素非 dict 则 `_force_json(field, item)` → 每项只留 original/corrected/reason 三键，None→`""`、非 str→`_force_string`）；类型不匹配 → force 三兄弟；三类型外 155604；完成 Debug "Schema check and repair done" | `schemaRepair(output map[string]any, schema map[string]schemaFieldType) (map[string]any, error)` |

### 2.2 QueryRewriter 类行为清单

| 项 | Python 行为 | Go 归宿 |
|---|---|---|
| `__init__(cfg, ctx, compress_range=20, prompt_lang="zh")` | `compress_range = max(compress_range, 1)`；`prompt_lang or "zh"`；从 mi 读 ssl_cert(None)/verify_ssl(True)/timeout(60)/custom_headers(None)；**verify_ssl 且 ssl_cert None → verify_ssl=False**（注释：BaseModelClient 要求 cert）；建 Model（temperature=0.0, top_p=float(getattr(mi,"top_p",0.1))）；`_json_output_parser`；两 schema dict | `NewQueryRewriter` + 选项；ssl_cert/verify_ssl/custom_headers 走 `mi.Extra` 读取（§4.2） |
| `load_template(prompt_base)` | 缓存 key `(base, lang)`；`Path(__file__).parent/prompts/{base}_{lang}.md`；不存在 → 155603 "prompt file not found: {path}"；OSError → 155603 "prompt file read failed" + cause；缓存后返回 | `LoadTemplate`（embed.FS；读失败分支编译期消解——偏差 §8-2） |
| `msg_2_text(messages=None)` | None → `context.get_messages(with_history=True)`；逐条 `f"{m.role}: {content}"`；list/dict content → `json.dumps(ensure_ascii=False)`；None → `""`（`str(content) if content is not None else ""`）；`"\n".join(...).strip()` | `Msg2Text(messages []BaseMessage)`（nil → GetMessages(0, true) 全量） |
| `compress(raw)` | msg_2_text → compression 模板填 history → invoke(SystemMessage, temperature=self.llm.model_config.temperature) 失败 155605；content strip → extract_json → parse_llm_json，None → `json_output_parser.parse`；非 dict 155604 "parse returned non-dict"（含 content[:500] repr）；JSONDecodeError 155604；schema_repair；Info "Compress completed: %s"（完整 dict） | `Compress(ctx, raw []BaseMessage)` |
| `rewrite(query)` | 非法输入（非 str/空白）→ 155606 "query must be a non-empty string"；load intention_completion；全量历史；`len >= compress_range` → compress → 成功：`json.dumps(summary, ensure_ascii=False)` 写 `SystemMessage(name="compressed_history")`；**BaseError 降级**：Warning "falling back to original history" + 原文 msg_2_text 写 `SystemMessage(name="original_history")`；取 `get_messages(size=compress_range)` → msg_2_text → 模板填 history/query → invoke（失败 155605）→ 解析链同 compress（"LLM rewrite output"文案）→ schema_repair → 返回 | `Rewrite(ctx, query string)` |

### 2.3 Python 测试基线（11 类约 30 用例 1:1 映射）

- TestQueryRewriterModelConfigPropagation 1 例：custom_headers 透传到 ModelClientConfig
- TestFillTemplate 2 例：占位符替换 / JSON 花括号不误伤
- TestExtractJson 4 例：单对象提取 / 无括号空 / 只有开括号 / 首{到尾}
- TestParseLlmJson 5 例：合法返回 / 空白 None / 纯文本 None / 尾逗号修复 / 非字典根 None（[1,2,3] 与 "null"）
- TestForceHelpers 3 例：string/list/json 三兄弟
- TestSchemaRepair 5 例：compress schema / None 填默认 / rewrite 全字段 / typo 子结构 / 非 dict 抛 155604
- TestQueryRewriterLoadTemplate 4 例：加载现有 / 二次缓存 / lang 不存在 155603 / 读失败 155603（→ Go 归档不可达）
- TestQueryRewriterMsg2Text 2 例：显式消息 / None 读 context
- TestQueryRewriterCompress 3 例：合法 mock / 非法 JSON 155604 / invoke 失败 155605
- TestQueryRewriterRewrite 5 例：合法 mock / 前后缀包裹 JSON / 非法输出 155604 / 空串 155606 / 纯空白 155606
- TestQueryRewriterRewriteCompressFallback 1 例：compress 抛错 → 原文降级 + rewrite 仍成功
- TestRewriteWithTrailingCommaJsonRepair 2 例：尾逗号修复函数级 / mock 集成
- TestFullConversationWithCompressAndRewrite 1 例：6 轮对话 compress_range=5，压缩后 context 剩 1 条 system

## 3. Go 包结构

新包 `internal/agentcore/retrieval/query_rewriter/`（与 Python 目录同构）：

```
query_rewriter/
├── doc.go                 # 包文档（含文件目录 + Python 对应）
├── embed.go               # //go:embed prompts/*.md + promptsFS
├── query_rewriter.go      # QueryRewriter 结构体 + NewQueryRewriter + LoadTemplate/Msg2Text/Compress/Rewrite
├── repair.go              # fillTemplate/extractJSON/parseLLMJSON/forceString/forceList/forceJSON/schemaRepair + schemaFieldType
├── query_rewriter_test.go # 类方法测试（mock 注入）
├── repair_test.go         # 模块级函数测试
└── prompts/
    ├── compression_zh.md          # 逐字复制（54 行）
    ├── compression_en.md          # 逐字复制（55 行）
    ├── intention_completion_zh.md # 逐字复制（83 行）
    └── intention_completion_en.md # 逐字复制（84 行）
```

依赖方向：`query_rewriter → ceiface(context_engine/interface) + llm + llmschema + model_clients + output_parsers + exception + logger + jsonrepair`。retrieval 树新叶子，零循环依赖。

## 4. Go 设计

### 4.1 结构体与构造

```go
// QueryRewriter 查询重写器
type QueryRewriter struct {
    modelConfig        *llmschema.ModelConfig
    compressRange      int
    sessionCtx         ceiface.ModelContext      // Python self.context（Go ctx 参数名冲突，字段名避让）
    promptLang         string
    llm                *llm.Model
    jsonOutputParser   *output_parsers.JsonOutputParser
    compressOutputSchema map[string]schemaFieldType
    rewriteOutputSchema  map[string]schemaFieldType
    templateCache      map[promptCacheKey]string
    cacheMu            sync.Mutex              // Go 补充：templateCache 并发保护（Python asyncio 单线程隐式安全）
}

type promptCacheKey struct {
    base string
    lang string
}

// QueryRewriterOption 构造选项
type QueryRewriterOption func(*QueryRewriter)

// WithCompressRange 设置压缩阈值（构造内 max(n,1) 钳制，对齐 Python L245）
func WithCompressRange(n int) QueryRewriterOption

// WithPromptLang 设置提示词语言后缀（空串构造后回落 "zh"）
func WithPromptLang(lang string) QueryRewriterOption

// WithModelClientOption 注入外部 Model（测试通道；传入则跳过内部构造）
func WithModelClientOption(m *llm.Model) QueryRewriterOption
```

`NewQueryRewriter(cfg *llmschema.ModelConfig, ctx ceiface.ModelContext, opts ...QueryRewriterOption) (*QueryRewriter, error)`：

1. `compressRange = max(compressRange, 1)`（Python `max(compress_range, 1)`）
2. `promptLang == "" → "zh"`（Python `prompt_lang or "zh"`；先应用选项再回落，`WithPromptLang("nonexistent")` 保留原值不回落——Python 测试改字段后 load 失败的同款语义）
3. schema 两份初始化：`{"theme": schemaFieldList, "summary": schemaFieldString}` 与 8 字段 rewrite schema（before/intention/standalone_query/from_history=string，references=string→dict，missing/typo/gibberish=list）
4. 若已注入 Model（WithModelClientOption）则跳过构造；否则：
   - `sslCert := mi.Extra["ssl_cert"].(string)`（缺失 `""`，即 Python None）
   - `verifySSL := mi.Extra["verify_ssl"].(bool)`（缺失 true，对齐 Python `getattr(mi, "verify_ssl", True)`）
   - `customHeaders := mi.Extra["custom_headers"].(map[string]string)`（缺失 nil）
   - `verifySSL && sslCert == "" → verifySSL = false`（Python 注释规则：BaseModelClient 要求 cert）
   - `llmschema.NewModelClientConfig(cfg.ModelProvider, mi.APIKey, mi.APIBase, WithTimeout(float64(mi.Timeout)), WithVerifySSL(verifySSL), WithSSLCert(sslCert)（非空时）, WithCustomHeaders(customHeaders))`
   - `llmschema.NewModelRequestConfig(WithModelName(mi.ModelName), WithTemperature(0.0), WithTopP(mi.TopP))`
   - `llm.NewModel(clientCfg, requestCfg)`，err 透传返回
5. `jsonOutputParser: output_parsers.NewJsonOutputParser()`

### 4.2 ssl_cert/verify_ssl 走 Extra 的对齐理由

Python `BaseModelInfo` 无 ssl_cert/verify_ssl 声明字段（mode_info.py L11-27 已核），`getattr(mi, "ssl_cert", None)` 读的是 pydantic `extra="allow"` 塞进来的动态字段。Go `BaseModelInfo.Extra` 正是 extra="allow" 的等价物（MarshalJSON/UnmarshalJSON 已实现合并语义），因此 `Extra["ssl_cert"]` 读取是**同构对齐而非近似**。top_p/timeout 是双方标准字段，直接读。

### 4.3 LoadTemplate

```go
func (q *QueryRewriter) LoadTemplate(promptBase string) (string, error)
```

1. 缓存命中（`promptCacheKey{promptBase, q.promptLang}`，锁内查）→ 直返
2. `fs.ReadFile(promptsFS, "prompts/"+promptBase+"_"+q.promptLang+".md")`
3. 不存在 → 155603 `WithParam("error_msg", "prompt file not found: prompts/"+name)`
4. 成功 → 锁内置缓存 → 返回

embed 模式读失败（OSError）分支编译期消解，仅"不存在"分支可达——偏差 §8-2。

### 4.4 Msg2Text

```go
func (q *QueryRewriter) Msg2Text(messages []llmschema.BaseMessage) (string, error)
```

1. `messages == nil` → `q.sessionCtx.GetMessages(0, true)`（Go size≤0 = 全量，对齐 Python `get_messages(with_history=True)` 无 size；err 透传）
2. 逐条：`role := m.GetRole().String()`；content 按 MessageContent 三态：
   - `IsText()` → `Text()`（空串天然，Python None→"" 对齐；`str(None)→"None"` 死分支归档 §8-1）
   - 多模态（Parts）→ `json.Marshal(parts)`（对齐 `json.dumps(list, ensure_ascii=False)`；Go json 默认不转义非 ASCII，HTML 字符不设 SetEscapeHTML）
3. `strings.TrimSpace(strings.Join(lines, "\n"))`

### 4.5 Compress

```go
func (q *QueryRewriter) Compress(ctx context.Context, raw []llmschema.BaseMessage) (map[string]any, error)
```

1. `rawText, err := q.Msg2Text(raw)`（err 透传——Python 无此分支，Go GetMessages 带 error 的附加防线；raw 非空时直接格式化不经 context）
2. `template, err := q.LoadTemplate("compression")`（err 透传 155603）
3. `compressionPrompt := fillTemplate(template, map[string]string{"history": rawText})`
4. `resp, err := q.llm.Invoke(ctx, model_clients.NewMessagesParam(llmschema.NewSystemMessage(compressionPrompt)), model_clients.WithInvokeTemperature(0.0))`
   - err → 155605（`WithParam("error_msg", err.Error())` + `WithCause(err)`）
   - temperature 传 0.0：Python `self.llm.model_config.temperature` 构造时已固定 0.0，字面等价
5. `contentStr := strings.TrimSpace(resp.Content.Text())`（resp nil 防御：空串）
6. `contentJSON := extractJSON(contentStr)`
7. `parsed, ok := parseLLMJSON(contentJSON)`；!ok → `q.jsonOutputParser.Parse(contentJSON)`（返回 any；非 map[string]any 视为非 dict）
8. 非 dict → 155604 `"LLM compress output is not valid JSON (parse returned non-dict); content: {contentJSON[:500]}"`
9. `schemaRepair(parsed, q.compressOutputSchema)`（Go 无 error 返回，spec §8-9）
10. Info 日志 `"Compress completed"` + `.Str("theme", ...)` `.Str("summary", ...)`（对齐 Python `logger.info("Compress completed: %s", dict)`）
11. 返回 repaired

json.Unmarshal 的 JSONDecodeError 路径：Go 侧 parseLLMJSON 内部吞掉（Python `_parse_llm_json` 不抛、JsonOutputParser 的 except 被 Python 外层 `except Exception` 兜住转 155604），Go 统一在步骤 6-8 消解为同一 155604 文案——结构对齐，文案差异归档 §8-3。

### 4.6 Rewrite

```go
func (q *QueryRewriter) Rewrite(ctx context.Context, query string) (map[string]any, error)
```

1. `strings.TrimSpace(query) == ""` → 155606 `"query must be a non-empty string"`
2. `promptTemplate, err := q.LoadTemplate("intention_completion")`（err 透传 155603）
3. `historyFull := q.sessionCtx.GetMessages(0, true)`（err 透传）
4. `len(historyFull) >= q.compressRange` 时：
   - `compressed, err := q.Compress(ctx, historyFull)`
   - 成功：`historyText, _ := json.Marshal(compressed)` → `q.sessionCtx.SetMessages([]llmschema.BaseMessage{llmschema.NewSystemMessage(string(historyText), llmschema.WithMessageName("compressed_history"))}, true)`
     - json.Marshal 对 map[string]any：非 ASCII 不转义 = `json.dumps(ensure_ascii=False)` 对齐
   - err（errors.As BaseError）：Warn 日志 `"Query rewriter compress failed, falling back to original history"` + Err 字段 → `historyText, _ := q.Msg2Text(historyFull)` → SetMessages(name="original_history")
5. `historyForRewrite := q.sessionCtx.GetMessages(q.compressRange, true)`
6. `historyText, err := q.Msg2Text(historyForRewrite)`（err 透传）
7. `completionPrompt := fillTemplate(promptTemplate, map[string]string{"history": historyText, "query": query})`
8. Invoke 同 Compress 步骤 4-5（文案 "LLM rewrite output..."）
9. 解析链 + `schemaRepair(parsed, q.rewriteOutputSchema)`（Go 无 error 返回，spec §8-9）
10. 返回 repaired

### 4.7 GetMessages 语义核实

Go `SessionModelContext.GetMessages(N>0, true)` = `messageBuffer.GetBack(N, true)` 取尾部 N 条（session_model_context.go L208-213），与 Python `get_messages(size=N)` 的 `[-N:]` 一致；`GetMessages(0, true)` = 全量。已核实无需适配。

## 5. 错误码与日志对齐

### 5.1 错误码（已注册零消费，本节转已消费）

| 码 | 常量 | 触发点 |
|---|---|---|
| 155603 | StatusRetrievalQueryRewriterPromptNotFound | LoadTemplate 文件不存在 |
| 155604 | StatusRetrievalQueryRewriterOutputInvalid | 解析非 dict / schemaRepair 不可修复 |
| 155605 | StatusRetrievalQueryRewriterLlmInvokeFailed | Invoke 错误（Compress/Rewrite 各一处） |
| 155606 | StatusRetrievalQueryRewriterInputInvalid | Rewrite 空/空白 query |

### 5.2 日志（英文 Msg，记忆规则；字段对齐 Python f-string 变量）

| 位置 | 级别 | 消息 | 字段 |
|---|---|---|---|
| extractJSON 无括号 | Debug | "No JSON object found in model output" | — |
| extractJSON 成功 | Debug | "JSON object from output extracted" | — |
| parseLLMJSON 修复后 | Debug | "JSON parsed after repair_json" | — |
| forceString dumps 失败 | Debug | "json.dumps failed when forcing value to string, using str(raw)" | — |
| schemaRepair 完成 | Debug | "Schema check and repair done" | — |
| Compress 完成 | Info | "Compress completed" | theme(string 化)/summary |
| Rewrite 降级 | Warn | "Query rewriter compress failed, falling back to original history" | Err |

## 6. 测试设计

### 6.1 mock 基建

- `stubQRClient` 实现 `model_clients.BaseModelClient` 全 8 方法（Invoke 真逻辑 + Stream/GenerateImage/GenerateSpeech/GenerateVideo/TranscribeAudio/Release/SupportsKVCacheRelease 薄桩，13.17 stubTripleClient 先例）：按 messages 首条 SystemMessage 内容分派——含 `"当前用户输入"`（zh 模板）或 `"Current user input"`（en）→ rewrite 响应，否则 compress 响应（Python mock_invoke 的 `"当前用户输入" in last_content` 判据同款）
- `fakeModelContext`：包内测试桩实现 ceiface.ModelContext 全接口（GetMessages/SetMessages/Len/AddMessages 真逻辑 + 其余薄桩），参考 react_agent_test.go fakeModelContext 先例
- Model 注入：`llm.NewModel(clientCfg, reqCfg, llm.WithClient(&stub))`（13.17 dispatchTripleClient 同款）

### 6.2 用例映射（11 类 1:1）

| Python 类 | Go 测试函数 | 备注 |
|---|---|---|
| ModelConfigPropagation | TestNewQueryRewriter_配置透传 | custom_headers 经 Extra → ModelClientConfig |
| FillTemplate | TestFillTemplate×2 | |
| ExtractJson | TestExtractJSON×4 | |
| ParseLlmJson | TestParseLLMJSON×5 | 尾逗号走 jsonrepair |
| ForceHelpers | TestForceString/ForceList/ForceJSON | |
| SchemaRepair | TestSchemaRepair×5 | 非 dict → 155604 |
| LoadTemplate | TestLoadTemplate×3 | 读失败分支归档不可达 |
| Msg2Text | TestMsg2Text×2 | |
| Compress | TestCompress×3 | 合法/非法 JSON/invoke 失败 |
| Rewrite | TestRewrite×5 | |
| CompressFallback | TestRewrite_压缩失败降级 | compress 失败 → original_history + rewrite 成功 |
| TrailingComma | TestRewrite_尾逗号修复×2 | |
| FullConversation | TestRewrite_完整对话流 | 6 轮 × compressRange=5 |

### 6.3 Go 增强（Python 无）

- 并发 LoadTemplate 竞态（t.Parallel + -race）
- `parseLLMJSON` "null" 字面量 → 非 dict → false
- `forceJSON` 四分支全覆盖
- schemaRepair typo 非 dict 元素 forceJSON 包裹路径
- Extra 读取边界：verify_ssl=true 无 cert → false；verify_ssl=false 有 cert 透传
- NewQueryRewriter 非法 provider → error 透传（NewModelClientConfig Validate）
- WithModelClientOption 注入路径不触发内部构造

### 6.4 覆盖率目标

≥ 90%（模块级函数 + 四方法全路径 + 降级分支；embed 不可达路径除外）。

## 7. 与后续章节的衔接

- **13.25 AgenticRetriever**：互不依赖，按编号序实施。其 `_rewrite`/`_read`/`_link_triples` 消费 13.22 TripleMemory + 13.23 GraphRetriever（已 ✅）
- **13.27 SimpleKnowledgeBase**：不消费 QueryRewriter（retrieve 直连 Retriever）
- **8.25 KnowledgeRetrievalComp**（远期）：QueryRewriter 的真实消费方，届时按 Python docstring 约定接线（rewrite → retrieve → add_messages 顺序）

## 8. 偏差归档（有意偏差，逐条对齐审查项）

| # | 偏差 | 理由 |
|---|---|---|
| 1 | `msg_2_text` Python `str(content) if content is not None else ""`——content None 渲染 ""，非 "None"；Go MessageContent 空串天然 | Python BaseMessage.content pydantic 校验不可能 None，该分支死代码；Go MessageContent.Text() 空串即 Python "" 语义 |
| 2 | load_template 的 OSError → 155603 read failed 分支在 go:embed 下不可达 | embed 编译期固化内容，运行时无 I/O 失败；Python 测试用 patch read_text 模拟该路径，Go 侧归档为不可达，对应测试不迁移 |
| 3 | Python 异常分层（JSONDecodeError / BaseError / Exception 三层 except 转不同 155604 文案）→ Go 统一消解为 parse 失败 + 非 dict 两条 155604 路径 | Go parseLLMJSON 不抛错误（bool 返回）等价 Python None；JsonOutputParser 错误在 Go 侧表现为 (nil, err)，统一走非 dict 155604；文案字段（content[:500]）保留 |
| 4 | Python `_template_cache` 无锁（asyncio 单线程）→ Go 加 sync.Mutex | Go 并发安全显式化，message_buffer.go 先例；行为等价 |
| 5 | Python `prompt_lang` 可后期改字段（测试 `qr.prompt_lang = "nonexistent_lang"`）→ Go 构造期固定，测试改用 `WithPromptLang("nonexistent_lang")` 构造新实例 | Go 小写字段包外不可写；构造选项是等价通道，测试语义不变 |
| 6 | Python 测试 `qr.llm = mock`（运行时换字段）→ Go `WithModelClientOption` 构造注入 / stub 走 `llm.NewModel(WithClient)` | Go 无运行时字段替换；13.17 dispatchTripleClient 同款注入先例 |
| 7 | Compress fallback 测试 Python 猴子补丁 `qr.compress = failing` → Go 用坏 JSON stub 让 Compress 内部走 155604，或 stub 返回 error 走 invoke 失败路径 | 等价触发降级分支；两条路径都覆盖（Python 只测前者） |
| 8 | Python `llm.invoke(model=..., messages=..., temperature=...)` 显式传 model 名（构造已设，字面重复）；Go Invoke 不传 WithInvokeModel（ModelRequestConfig.ModelName 已含） | Go resolveModelName 从 m.ModelConfig 取默认；显式重传是 Python 冗余执行，Go 侧去重（记忆规则：重复调用/冗余参数去重先例 13.15 双 cp_image） |
| 9 | Python `_schema_repair` 非 dict 入参与未知类型两分支抛 155604；Go `schemaRepair` 去掉 error 返回值，只返回 `map[string]any` | Go 入参类型 `map[string]any` + `schemaFieldType` 三态枚举使两条 155604 路径编译期消解，error 无发生路径；Go 惯例不为不可能路径保留 error 签名（实施后审查与用户共同拍板，2026-10-28） |

## 9. 实施任务切分（writing-plans 输入）

1. prompts/ 4 md 逐字复制 + embed.go + doc.go
2. repair.go 模块级函数 + schemaFieldType + 测试（repair_test.go）
3. query_rewriter.go 结构体 + 构造 + 选项 + LoadTemplate/Msg2Text + 测试
4. Compress + 测试
5. Rewrite（含降级链）+ 测试
6. full conversation 集成用例 + Go 增强 + 覆盖率核验（≥90%）
7. 实施后审查（对齐 Python 逐行核 + 偏差表核对 + IMPLEMENTATION_PLAN.md 回填）
