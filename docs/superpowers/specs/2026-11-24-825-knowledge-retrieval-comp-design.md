# 8.25 批 KnowledgeRetrievalComp + Memory 两件 设计（spec）

- 日期：2026-11-24
- 章节：8.25（knowledge_retrieval）+ 8.25a（memory_retrieval）+ 8.25b（memory_write），方案 B 拆两轮实施
- Python 真相源：`openjiuwen/core/workflow/components/resource/{knowledge_retrieval_comp,memory_retrieval_comp,memory_write_comp}.py`（281/160/155 行）
- 拍板记录：
  - 方案 A 拍板：memory 两件新增 8.25a/8.25b 行，resource 子包一次成型
  - 方案 ① 拍板：按编号先 8.25 批，再 8.26 ReactComponent（含 8.24 ⤴️ ReAct 工具级中断桥）
  - 方案 B 拍板：拆两轮——第一轮 8.25 kb，第二轮 8.25a+b memory 两件
  - 集成硬要求（用户拍板）：真实创建 workflow → 真实挂载 KB 组件 → 多检索模式（vector/hybrid/graph + agentic）逐一验证 → 验证知识库存储/提取/检索全链；免费 LLM（glm-4.5-flash，8.24 真链冒烟同款）可用

---

## 一、流程位置与作用（背景，供 plan 撰写者参考）

KnowledgeRetrievalComp 是 workflow 图叶子执行节点（非子图容器），编排侧经 `workflow.add_workflow_comp(comp_id, comp, inputs_schema, ...)` → 组件 `AddComponent(graph, node_id)` → `graph.AddNode(node_id, ToExecutable())` 挂入 Pregel 图。数据面：

- 节点间不直接传值：边只发 TriggerMessage 控制信号；数据走共享 io_state（`vertex.set_outputs({node_id: data})` 写、下游 `_pre_invoke` 按 inputs_schema 前缀捞）；
- 字段匹配责任在编排声明（inputs_schema），组件运行时 pydantic 校验兜底（缺 query → 101102）；
- 8.25 三组件均为纯 INVOKE 组件，不涉流式路径（STREAM/TRANSFORM 走 StreamActor 通道，另套 stream_inputs_schema，与本批无关）。

## 二、第一轮：8.25 KnowledgeRetrievalComp

### 2.1 文件与结构映射（规则 9 一文件对一文件）

| Python | Go |
|---|---|
| `resource/knowledge_retrieval_comp.py` | `internal/openuapclaw/core/workflow/components/resource/knowledge_retrieval_comp.go` |
| `resource/__init__.py` | `resource/doc.go`（第一轮建包，含子包文档） |

类型归属不变：

- `ComponentKBConfig`：`KBConfig KnowledgeBaseConfig` / `VectorStoreConfig VectorStoreConfig` / `EmbedConfig *EmbeddingConfig` / `EmbedAdditionalConfig map[string]any`
- `KnowledgeRetrievalCompConfig`（dataclass kw_only → Go 结构体 + 构造校验）：`ComponentKBConfigs []ComponentKBConfig` / `VectorStoreConnectionConfig map[string]any` / `RetrievalConfig RetrievalConfig` / `ModelID *string` / `ModelClientConfig *ModelClientConfig` / `ModelConfig *ModelRequestConfig`
- `KnowledgeRetrievalInput{Query string}`（extra=allow → 对多余键宽容：只取 query 键，不拒绝）
- `KnowledgeRetrievalOutput{Results []string, Context string}`（`_format_output` 返回 model_dump 等价 map/struct，context = `strings.Join(texts, "\n\n")`）
- `KnowledgeRetrievalExecutable`：字段 `_config/_kbs/_llm/_initialized/_session/_lock`（asyncio.Lock 双检 → `sync.Mutex` + `initialized bool`；单机无跨协程泄漏问题）
  - `Invoke(inputs, session, context)` / `TraceMetadata()` / `setSession`（私有）/ `initializeIfNeeded` / `createKnowledgeBases` / `createLLMInstance` / `validateInputs`（静态→包级或同文件静态方法，归属对齐）/ `formatOutput`
- `KnowledgeRetrievalComponent` 薄外壳：`AddComponent(graph, nodeID, waitForAll)` + `ToExecutable()`，复用 8.18 WorkflowComponent 槽位模式基座（构造期 ToExecutableFn=注入 self，勿 override 基座两方法——8.20 教训）

### 2.2 执行链（invoke 步骤逐条对齐，规则 9.3）

1. `setSession(session)`
2. `initializeIfNeeded`：双检锁内——agentic 时先 `createLLMInstance`，再 `createKnowledgeBases`；任一失败 wrap 101100（"Failed to initialise knowledge retrieval component"）
3. 校验输入：`validateInputs`（校验失败 101102，msg 用 ExceptionUtils.format_validation_error 等价物）；query `strings.TrimSpace` 为空 → 101102 "Query must be a non-empty string"
4. 起始日志：WORKFLOW_COMPONENT_START，component_id=session.get_executable_id() 等价、metadata=query_length/top_k/sensitive_mode（规则 3.3 逐字段）
5. `retrieve_multi_kb_with_source(kbs, query, retrievalConfig)`（13.x `multi_kb.go` 直接消费）；失败：ERROR 日志（WORKFLOW_COMPONENT_ERROR，注意此处 Python 用的是入参 session 而非 self._session 的 component_id——逐字保留这个差异）+ wrap 101100
6. `formatOutput`：texts=[r.text for r in results]，context="\n\n".join
7. 结束日志：WORKFLOW_COMPONENT_END，metadata=num_results/sensitive_mode
8. 返回 output

### 2.3 初始化子链

- `createLLMInstance`（仅 retrieval_config.agentic 时调用）：
  - `model_id` 为 nil：`model_client_config` 或 `model_config` 任一缺失 → 101103 "LLM model config is required for agentic retrieval"；否则直构 `Model(clientConfig, requestConfig)`（foundation/llm 已有）
  - `model_id` 非 nil：`ResourceMgr.GetModel(ctx, []string{modelID})` 取 [0]（单元素收窄，注释登记偏差：Python `get_model(id=...)` 返回单实例、Go 签名收 []string）。ResourceMgr 获取途径对齐 8.22 先例（构造/注入缝按 8.x 既有模式，spec 不新造接口）
- `createKnowledgeBases`（逐 ComponentKBConfig）：
  1. `create_vector_store(vector_store_config, **vector_store_connection_config)` → Go `vector_store.CreateVectorStore`（13.x store.go 已有，connection config 平铺 kwargs → map 透传，键名对齐 Python）
  2. embed 校验：`embed_config == nil` 且 `kb_config.index_type ∈ {vector, hybrid}` → 101101 "Embedding config is required for vector or hybrid index type"；否则 `OpenAIEmbedding(config, **embed_additional_config)` 构造，失败 wrap 101101
  3. `use_graph` 二选一：`GraphKnowledgeBase` / `SimpleKnowledgeBase`（均传 config/vector_store/embed_model/llm_client 四参；llm_client 仅 agentic 时非 nil）
- `TraceMetadata`：`{top_k, data_source_ids:[每个 kb_config.kb_id]}`；agentic 且 model_id 非 nil 时附 `request_model`。挂既有 TraceMetadata 机制（component.go）

### 2.4 错误码与日志

- 101100/101101/101102/101103 已在 `codes_workflow.go` 预置（零消费状态），本节全部转已消费
- 日志按规则 3：`const logComponent = logger.ComponentAgentCore`（workflow 组件归属按 8.18-8.24 同款先例，plan 落地时以 8.22/8.24 实际选用的组件常量为准）；Python 的 LogEventType（WORKFLOW_COMPONENT_START/END/ERROR）等价映射

### 2.5 缺口核查清单（实施前第一件事）

- `UserConfig.IsSensitive()`：Python invoke 三处日志用 `UserConfig.is_sensitive()`。实施前 grep Go `common/security` UserConfig；缺则按规则 13.2 提出讨论（三档：本批补 / ⤵️ / 不修），禁止默默桩掉
- `RetrievalConfig` / `KnowledgeBaseConfig` / `VectorStoreConfig` / `EmbeddingConfig` 字段完备性对照（13.x 已落，预期齐）
- `Model` 构造（foundation/llm）与 agentic 检索入参（GraphKB/SimpleKB 的 llm_client 形参类型）签名核对

### 2.6 测试（第一轮）

单元（真实链纪律，禁窄接口替身）：

- 惰性初始化：并发 Invoke 双检（-race）；初始化失败 wrap 101100 且 `_initialized` 保持 false 可重试
- embed 校验三路径：vector/hybrid 无 embed_config → 101101；构造失败 → 101101 wrap；sparse/无索引类型不校验
- 101103 两分支（model_id nil 且配置缺 / model_id 非 nil 走 ResourceMgr）
- 空 query / validateInputs 失败 → 101102
- use_graph 分叉构造（断言实例类型）；formatOutput 拼接（空 results → results=[] & context=""）
- TraceMetadata：非 agentic 无 request_model；agentic+model_id 有
- 日志字段逐项断言（query_length/top_k/sensitive_mode/num_results）

**集成测试（用户硬要求，`//go:build integration && llm`，env 缺 key 时 Skip）**：

- 真实创建 Workflow（8.14 BaseWorkflow 门面）：Start 节点（喂 query）→ `AddWorkflowComp("kb1", kbComp, inputs_schema={"query": "start.question"})` → End 节点，AddEdge 串联，RunWorkflow 端到端
- 多检索模式逐一验证（存储→提取→检索全链）：
  1. **vector**：Chroma/InMemory store + 真 embedding（glm 免费端点 embed 或 env 配置的 embedding），写入文档后检索命中断言
  2. **hybrid**：向量+稀疏混合检索模式跑通
  3. **graph**（use_graph=true）：GraphKnowledgeBase 链，写三元组→图检索
  4. **agentic**（retrieval_config.agentic=true + model_id=glm-4.5-flash）：多跳 agentic 检索走真 LLM（13.25 AgenticRetriever 已具备能力，本节验证的是组件装配层把 Model 正确注入 KB）
- 断言：输出 results 非空、context 拼接含命中文本、inputs_schema 字段映射正确（start.question → kb1.query → end 引用 kb1.context）
- 存储侧验证：写入后经 KB 原生 API（13.26-13.28）读回，确认组件消费的 KB 与存储层一致

## 三、第二轮：8.25a MemoryRetrievalComp / 8.25b MemoryWriteComp

### 3.1 结构映射

- `MemoryRetrievalCompConfig`：`Memory *ltm.LongTermMemory`（配置直接持实例，无 lazy/无 ResourceMgr；Go 用具体类型指针，规则 10 例外出口：这是"组件依赖别人"的方向，允许持具体类型）+ `ScopeID/UserID string`（默认 `"__default__"` 原样对齐）+ `Threshold float64`（默认 0.3）
- `MemoryRetrievalInput{Query string, TopK int=5}`；Output `{FragmentMemoryResults []MemResult, SummaryResults []MemResult}`（MemResult 用 13.x/memory 域既有结构体，model_dump 序列化形态对齐）
- `MemoryWriteCompConfig`：`Memory *ltm.LongTermMemory` + `ScopeID/UserID/SessionID`（默认 `"__default__"`）+ `AgentConfig memory.AgentMemoryConfig` + `GenMem bool=true` + `GenMemWithHistoryMsgNum int=2`
- `MemoryWriteInput{Messages []BaseMessage, Timestamp *time.Time}`；Output `{Success bool=true}`
- 两组件 Executable/Component 外壳与 8.25 同款槽位模式；无 `trace_metadata`（Python 没有就不加）

### 3.2 执行链

- retrieval.invoke：校验（空 query → `COMPONENT_MEMORY_RETRIEVAL_INPUT_PARAM_ERROR`）→ 起始日志（query_length/top_k/threshold/user_id/scope_id/sensitive_mode）→ 双路 `SearchUserMem` + `SearchUserHistorySummary`（同参）→ 失败 wrap `COMPONENT_MEMORY_RETRIEVAL_INVOKE_CALL_FAILED` → 结束日志（num_results/num_summary_results）
- write.invoke：校验（messages 空 → `COMPONENT_MEMORY_WRITE_INPUT_PARAM_ERROR` "Messages list cannot be empty"）→ 起始日志（message_count/scope_id/user_id/gen_mem/sensitive_mode）→ `AddMessages(messages, agent_config, user_id, scope_id, session_id, timestamp, gen_mem, gen_mem_with_history_msg_num)` → 失败 wrap `COMPONENT_MEMORY_WRITE_INVOKE_CALL_FAILED` → 结束日志（message_count）→ 返回 `{success: true}`
- 两个 write/retrieval 错误码若 codes 未预置，登记新码（对齐 Python StatusCode 数值），经用户确认后录入

### 3.3 测试（第二轮）

- 真实链：真 `ltm.LongTermMemory` 单例 + InMemory store（7.27 integration tag 用例先例）；gen_mem=True 路径 LLM 边界用 harness 既有 fake LLM（fake 只在协议边界）
- 覆盖：默认值三元组落 `"__default__"`；threshold 传递；TopK 默认 5；write 空 messages 报错；gen_mem_with_history_msg_num 透传；双路检索结果映射；-race

## 四、计划表与回填登记（不丢失项）

1. 8.25 行更新为 ✅（完成后）；新增 8.25a/8.25b 行标注方案 B 拆轮
2. 跨域联动注记「8.25 KnowledgeRetrievalComp ⤴️ 依赖 13.26-13.28」闭环改 ✅（消费兑现）
3. 7.36 行 ⤵️ 缺口清单指向领域十三的登记随 8.25 核销
4. 101100-101103 转已消费；memory 两件错误码同法
5. 8.13 drawable 缝继续压后不动；8.26 ReAct 中断桥 ⤴️ 留待 8.26
6. `resource/` 子包 doc.go 登记（规则 1）；`components/doc.go` 文件树同步加 resource/ 条目

## 五、已知偏差（实施后固化，2026-11-24 实施轮）

- D-25-1：`Runner.resource_mgr.get_model(id)` 单实例 vs Go `ResourceMgr.GetModel(ctx, []string)` 收窄取 [0]（8.18 getModel 先例）
- D-25-2：Python 组件向 KB 传 vector_store/embed_model/llm_client、agentic 包装由 KB 内部组装；Go 侧组件按 index_type 显式组装基础检索器（Sparse/Vector/Hybrid），agentic 包装仍留 KB 层 Retrieve 时按 RetrievalConfig.Agentic 自动 wrapAgenticRetriever（simple_kb.go:142/graph_kb.go:192，13.27 语义）——构造归属显式化，检索行为等价
- D-25-3：Python OpenAIEmbedding 构造可抛异常（wrap 101101）；Go NewOpenAIEmbedding 无 error——101101 收窄为「embed_config 缺失且 index_type∈{vector,hybrid}」配置校验分支，运行期 embed 失败经 RetrieveMultiKBWithSource 传播 wrap 101100
- D-25-4：pydantic ValidationError → Go 类型断言直校（pythonsem "string" 是 str() 宽松语义不适用字段校验），消息对齐 pydantic "Field required / Input should be a valid string" 形态
- D-25-5（2026-11-24 二轮修复，方案A对齐重构）：~~直构 Model 缺 TranscribeAudio 不满足接口→adapter~~ **根因已修**——TranscribeAudio 是 Go 接口面超集（Python Model 无此方法，转写归 harness/multimodal 工具层自建 OpenAI client，audio.py:98 _build_openai_client）。已从 BaseModelClient 接口删除该方法；*llm.Model 方法面回归 Python 形态（编译期断言通过）；resource/integration 两处 adapter 删除（规则 8）；anthropic/intellirouter 死桩删除；audio 工具转写调用点改 audioTranscribeFn 函数接缝（Python monkeypatch 同款手法）；modelClientAdapter/testModelClientAdapter/intellirouter 不支持分支测试随之删除
- D-25-6：Python `**vector_store_connection_config` kwargs 透传未知键；Go storeOptionsFromConnection 仅映射已支持键、未知键忽略（chroma/milvus 已覆盖 7 键；类型不符忽略）
- 实施注记（2026-11-24 第二轮用户拍板升级）：chroma 本地模式不支持 sparse/hybrid（155508 库层既有约束，上游 knowledge_base.py:63 原文）——**经独立调研确认 chroma embedded 无倒排索引/无 BM25（where_document 仅子串过滤），限制为产品能力事实**；用户拍板方案 A：新增 **StoreTypeSqlite（Go 侧私有 provider，D-25-7）** 支撑 bm25/hybrid/graph 真链验证与轻量部署
- D-25-7：SQLite 向量存储 Go 私有 provider（上游 Python 工厂仅 milvus/chroma/pgvector 三种）。能力面：VectorSearch=sqlite-vec vec0 KNN（f32 blob，复用 lite.ResolveVec0Path/LoadVec0Extension）、SparseSearch=FTS5 `bm25()` 真 BM25 打分（取负归一为越大越相关）、HybridSearch=两路 + `utils.RRFFusion` k=60（chroma HybridSearch 同款语义）；表结构 `{collection}_chunks/{_fts}/{_vec}/{_vec_map}` 四表；无向量写入仍进 chunks/FTS（bm25-only 场景合法，向量检索自然不命中）；vec0 按连接加载 → 单专用连接（SetMaxOpenConns(1)）+ mu 串行化；connection config 键 `sqlite_path`（上游无对应键名，Go 侧新设）；FTS5 需 `-tags sqlite_fts5`（本仓标准标签）。**回收条件：上游引入同能力 provider（或 chroma v1.x server 真身可用且上游解锁 hybrid）时迁移对齐；sqlite provider 不进入 Python 对照面**
- D-25-8：memory 两件单测覆盖率 75.6%/76.1%（<85%）——invoke 成功路径需完整 LTM 存储装配（scope config + embedding + InMemory/真实 store），裸 NewLongTermMemory 无轻量成功构造（ltm 测试基线同款：TestAddMessages_正常流程 也仅错误路径）。⤵️ 成功路径全链验证登记至集成测试回填（需要 LTM 测试装配 harness，属 7.27 集成先例范畴）；错误传播路径已真链覆盖（101201/101151 wrap 断言通过）。另发现：裸 LTM + 默认 scope 调 AddMessages 存在 panic 风险（add_messages.go:40 addMessagesImpl 深层空指针，ltm 域既有问题）——非本批范围，登记不动。
