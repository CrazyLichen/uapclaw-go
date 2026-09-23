# 48h 逻辑审查报告

> 审查时间：2026-09-23
> 审查范围：48h 内提交记录（30 commits, ~95 files changed）
> 审查模块：Context Evolver P1/P2/P3 (9.82) / Worktree (9.66a) / Graph Memory (7.11+7.12) / Evolution Rail / Swarm / Workspace / Embeddings / Session
> 对比基准：Python 源码 `openjiuwen/` + `jiuwenswarm/`

---

## 摘要

| 模块 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| Context Evolver P1 (9.82) | 4 | 5 | 2 | 11 |
| Context Evolver P2 (9.82) | 3 | 5 | 2 | 10 |
| Context Evolver P3 ReMe Retrieve (9.82) | 3 | 4 | 2 | 9 |
| Context Evolver P3 ReMe Summary (9.82) | 7 | 8 | 3 | 18 |
| Graph Memory (7.11+7.12) | 8 | 14 | 4 | 26 |
| Worktree (9.66a) | 8 | 10 | 7 | 25 |
| Evolution Rail / DeepAdapter / Swarm | 10 | 8 | 6 | 24 |
| Workspace / Embeddings / Session | 4 | 4 | 3 | 11 |
| **合计** | **47** | **58** | **29** | **134** |

**最需优先修复的严重问题 TOP 15：**

1. **WT-S-01**：Enter/Exit 从未调用 fire* lifecycle hook，所有 hook（AutoSetup/DiffSummary）永远不会触发
2. **EV-S-06**：DeepAdapter.updateRuntimeConfig 缺少 updateRailsForMode 调用，模式切换时 rail 不更新
3. **EV-S-07**：DeepAdapter.buildStreamEventRail 返回 nil，中断/暂停/恢复功能全部缺失
4. **CE3R-S-01**：RecallMemoryOp 跳过 `NewReMeMemoryFromVectorNode` 反序列化，丢失类型校验
5. **CE3R-S-02**：RecallMemoryOp user_id 为空时不设 `"default"`，metadata_filter 缺 workspace_id
6. **CE3S-S-05**：MemoryDeduplicationOp embedding 为 nil 时 Go 加入 uniqueMemories，Python 跳过
7. **CE1-S-01**：MemoryPersistenceHelper 默认 persistType="json" 而非 "auto"，缺失探测逻辑
8. **WT-S-04**：AutoSetupRail.AfterWorktreeCreate 用 `context.Background()` 丢弃上游 ctx
9. **EV-S-04**：agent_configurator.go WorktreeManager 条件缺少 `spec.Worktree.Enabled`
10. **EV-S-09**：DeepAdapter.buildSkillCreateRail 返回 nil，技能创建功能缺失
11. **GM-S-01~S-03**：extraction_prompts.go / parse_response.go / graph_memory 目录完全缺失
12. **WT-S-05/S-06**：OnWorktreeSync/OnWorktreeFileWrite 签名与 Python 不匹配
13. **WS-S-01**：英文 workspace schema 缺少 coding_memory 目录
14. **CE2-S-01**：TaskMemory / PersonalMemory / Ours 系列 Schema 完全缺失
15. **EV-S-06b**：DeepAdapter.buildResponsePromptRail 返回 nil

---

## 一、Context Evolver P1 — 核心框架 (9.82)

### 严重 (S)

**CE1-S-01：MemoryPersistenceHelper 默认 persistType="json" 而非 Python "auto"，缺失 Milvus 探测逻辑**
- Python: `persistence.py:46` — `persist_type: str = "auto"`，`_resolve_backend()` 首次 Save/Load 探测 Milvus
- Go: `persistence_helper.go:81-88` — `persistType: "json"`, `resolvedType: "json"` 硬编码
- **影响**：Go 永远不会尝试连接 Milvus，即使 P7 注入 MilvusConnector 也无法触发自动探测
- **修复**：默认改为 `"auto"`，实现 `resolveBackend()` 探测逻辑，P7 Milvus 可用前先 stub 返回 "json"

**CE1-S-02：ServiceContext 非并发安全但无保护**
- Python: `service_context.py:11-25` — `__new__` 单例，全局只初始化一次
- Go: `service_context.go:79-81` — 非单例、`services` map 无 mutex
- **影响**：并发 `RegisterService` 会 data race
- **修复**：加 `sync.RWMutex` 或文档强制约定"初始化阶段完成后只读"

**CE1-S-03：PersistenceHelper.saveJSON 加载失败时不阻断，Python 会 raise**
- Python: `_save_json` — `existing = self._json_connector.load_from_file(path)` 异常传播中断 save
- Go: `persistence_helper.go:208-214` — 加载失败只记 error 日志继续写入
- **影响**：文件已存在但 JSON 损坏时，Go 用新数据覆盖（丢失旧数据），Python 会中断
- **修复**：加载失败时应返回 error

**CE1-S-04：Message/Role Schema 完全缺失**
- Python: `core/schema/message.py` — `Role(str, Enum)` + `Message(BaseModel)` 含 role/content/metadata
- Go: `core/schema/` 下只有 `vector_node.go`，无 Message/Role
- **影响**：Python `__init__.py` 导出 Message/Role，后续 P6 服务层依赖
- **修复**：新增 `message.go`，实现 Role 枚举和 Message 结构体

### 一般 (M)

**CE1-M-01：OpBase 缺少 _params 字段和 String() 方法**
- Python: `base_op.py:28-30,104-109` — `_params = kwargs`，`__repr__` 输出 `ClassName(key=val)`
- Go: `base_op.go:33-36` — 无 params，无 String()
- **修复**：添加 `String()` 辅助调试

**CE1-M-02：SequentialOp.Then 修改自身后返回 self，Python 返回新对象**
- Python: `SequentialOp.__rshift__` → `return SequentialOp(*self._ops, *other._ops)`
- Go: `s.ops = append(s.ops, nested.ops...)` 然后 `return s`
- **影响**：`x := a.Then(b); y := a.Then(c)` 会导致 x 和 y 共享底层数组
- **修复**：Then/With 应创建新对象并复制 ops slice

**CE1-M-03：ParallelOp.With 同样修改自身返回 self**
- 同 CE1-M-02

**CE1-M-04：MilvusConnector 接口不完整**
- Python 有 search/list_namespaces/count/flush/close/set_collection/delete_nodes/create_index/truncate/ids_expr
- Go 只定义了 SaveToDB/LoadFromDB/Exists/Delete
- **修复**：P7 补齐，P1 至少补充 Search 和 Close

**CE1-M-05：MemoryVectorStore.Search 返回 nil vs 空切片**
- Python `async_search` 空结果返回 `[]`
- Go: `if len(s.vectors) == 0 { return nil, nil }`
- **修复**：统一返回 `[]*schema.VectorNode{}`

### 提示 (T)

**CE1-T-01**：OpBase/SequentialOp/ParallelOp 缺少 Python BaseOp.__call__ 中的 debug/error 日志
**CE1-T-02**：MemoryVectorStore.Upsert 缺少 upsert 后 debug 日志

---

## 二、Context Evolver P2 — IO Schema 层 (9.82)

### 严重 (S)

**CE2-S-01：TaskMemory / PersonalMemory 完全缺失**
- Python: `schema/memory.py` 定义 `TaskMemory(BaseMemory)` 和 `PersonalMemory(BaseMemory)`，含完整 to_vector_node/from_vector_node/get_score
- Go: `schema/memory.go` 只实现了 ACEMemory/ReasoningBankMemory/ReMeMemory
- **修复**：实现 TaskMemory/PersonalMemory，并在 VectorNodeToMemory 中增加 `"task_memory"`/`"personal_memory"` 分发

**CE2-S-02：Ours 系列 Schema 全部缺失**
- Python: `io_schema.py` 定义 OursMemory/OursSummarizeRequest 等 6 个类型
- Go: 完全没有
- **修复**：`type OursMemory = ReMeMemory` 类型别名，Request/Response 同理

**CE2-S-03：ReMeMemory.FromVectorNode created_at 缺失时用 `time.Now()` 回填，Python 不做此回填**
- Python: `from_vector_node` 不回填 created_at
- Go: `memory.go:191-196` — `if createdAt.IsZero() { createdAt = time.Now().UTC() }`
- **影响**：反序列化旧数据时 created_at 被错误设为当前时间
- **修复**：移除 `time.Now()` 回填，保持零值

### 一般 (M)

**CE2-M-01**：ACESummarizeRequest.Feedback `omitempty` 无法区分 None 和 `[]`
**CE2-M-02**：ReMeMemoryMetadata 可选字段语义偏差（StepType/Confidence/Utility 零值无法区分"未设置"和"值为0"）
**CE2-M-03**：BaseMemory 默认 workspace_id 不对齐（Go 零值="" vs Python default="default"）
**CE2-M-04**：TrajectoryBatch 缺少 from_dict/to_dict
**CE2-M-05**：ReasoningBankMemory（memory.py 版本）Go 完全缺失（Python 有两套 RBMemory）

### 提示 (T)

**CE2-T-01**：ReMeRetrieveRequest 中 llm_rerank/llm_rewrite 在 Op 层而非请求层，对齐正确
**CE2-T-02**：safe_model_dump 不需要移植（Go 用 json.Marshal）

---

## 三、Context Evolver P3 — ReMe Retrieve (9.82)

### 严重 (S)

**CE3R-S-01：RecallMemoryOp 跳过 `NewReMeMemoryFromVectorNode` 反序列化**
- Python: `run.py:66-77` — `memory = ReMeMemory.from_vector_node(node)` → `ReMeRetrievedMemory(when_to_use=memory.when_to_use, content=memory.content)`
- Go: `run.go:133-141` — 直接从 `node.Metadata["when_to_use"]` / `node.Metadata["content"]` 取值
- **影响**：跳过 from_vector_node 的类型校验和字段映射，metadata 异常时取到空值不报错
- **修复**：
  ```go
  memory := ceschema.NewReMeMemoryFromVectorNode(node)
  retrieved = append(retrieved, ceschema.ReMeRetrievedMemory{
      WhenToUse: memory.WhenToUse,
      Content:   memory.Content,
  })
  ```

**CE3R-S-02：RecallMemoryOp user_id 为空时不设 `"default"`，metadata_filter 缺 workspace_id**
- Python: `run.py:44,61` — `user_id = context.get("user_id", "default")`，filter 含 `"workspace_id": user_id`
- Go: `run.go:103-120` — `if userID != "" { metadataFilter["workspace_id"] = userID }`，空时跳过
- **影响**：userID 为空时返回所有用户记忆，造成数据泄露
- **修复**：`if userID == "" { userID = "default" }`，始终设 workspace_id

**CE3R-S-03：RerankMemoryOp 解析失败时不截断，Python 仍截断到 topKRerank**
- Python: `run.py:141-145` — 解析失败用原始列表，但仍截断 `[:self.topk_rerank]`
- Go: `run.go:196-199` — 解析失败直接 return nil，不截断
- **修复**：解析失败时仍将原始列表截断到 topKRerank 后写入 RuntimeContext

### 一般 (M)

**CE3R-M-01**：RecallMemoryOp 缺少 Python 的 debug 日志 "Generating query embedding..."
**CE3R-M-02**：RerankMemoryOp 缺少 "No memories retrieved, skipping rerank" info 日志
**CE3R-M-03**：RewriteMemoryOp 缺少 "No memories retrieved, skipping rewrite" info 日志
**CE3R-M-04**：ParseJSONField 返回 `string`（空字符串=失败）vs Python 返回 `Optional[str]`（None=失败），无法区分"字段值为空字符串"和"解析失败"

### 提示 (T)

**CE3R-T-01**：retrieve/reme/run.go:87 "导出函数" 分隔注释重复
**CE3R-T-02**：ReMeRetrievePrompts 命名更具体（Go 区分 retrieve/summary），设计决策合理

---

## 四、Context Evolver P3 — ReMe Summary (9.82)

### 严重 (S)

**CE3S-S-01：TrajectoryPreprocessOp threshold=0 被改为 1，Python 区分"缺失"和"值为0"**
- Python: `update.py:64` — `threshold = context.get("threshold", 1)`
- Go: `update.go:180-183` — `if threshold == 0 { threshold = 1 }`
- **影响**：显式传入 threshold=0 时 Go 错误替换为 1
- **修复**：用 `GetDefault("threshold", 1)` 或 pointer 区分缺失和零值

**CE3S-S-02：ComparativeExtractionOp LLM 失败返回 error 终止管线**
- Python: `update.py:324-326` — 无 try/except，异常传播终止
- Go: `update.go:432-436` — `return err` 终止
- **影响**：行为等价（两者都终止），但与 SuccessExtractionOp 的 `continue` 行为不一致
- **修复**：统一错误处理策略——要么都终止，要么都 continue

**CE3S-S-03：ComparativeAllExtractionOp 同 CE3S-S-02**
- Go: `update.go:503-507` — `return err`

**CE3S-S-04：MemoryDeduplicationOp 用 GetAll 替代 Python async_search(dummy_embedding)**
- Python: `update.py:657-660` — `async_search(embedding=[0.0]*2560, top_k=1000, ...)`
- Go: `update.go:616` — `vectorStore.GetAll(map[string]any{...})`
- **影响**：Python 受 top_k=1000 截断，Go 不受截断。Go 更正确但语义不同
- **修复**：保留 GetAll（更正确），添加注释说明差异

**CE3S-S-05：MemoryDeduplicationOp embedding 为 nil 时 Go 加入 uniqueMemories，Python 跳过**
- Python: `update.py:614-617` — `if current_embedding is None: continue`
- Go: `update.go:639-642` — `uniqueMemories = append(uniqueMemories, memory); continue`
- **影响**：Go 保留无法去重的记忆，Python 丢弃它们。行为相反
- **修复**：对齐 Python，embedding 为 nil 时不加 uniqueMemories

**CE3S-S-06：SuccessExtractionOp/FailureExtractionOp LLM 失败用 continue，Python 无 try/except 直接抛异常**
- Python: `update.py:138-139` — `response = await self.llm.async_generate(prompt=user_prompt)` 无 try/except
- Go: `update.go:251-255` — `if err != nil { logger.Warn(...); continue }`
- **影响**：Go 静默跳过失败轨迹继续处理，Python 遇 LLM 错误终止整个 Op
- **修复**：对齐 Python，LLM 失败时返回 error（终止 Op）；或文档化 Go 的有意改进

**CE3S-S-07：MemoryDeduplicationOp 用 Embed（单条），Python 用 async_embed_batch（批量）**
- Python: `update.py:694` — `embeddings = await self.embedding_model.async_embed_batch([text_for_embedding])`
- Go: `update.go:633` — `emb, err = embeddingModel.Embed(ctx, embedContent)`
- **影响**：功能等价但性能不同，逐条调用远慢于 batch
- **修复**：改为 EmbedBatch 批量生成

### 一般 (M)

**CE3S-M-01**：TrajectoryPreprocessOp score/trajectories 长度不一致时 Go 归入 failure，Python zip 截断不处理
**CE3S-M-02**：ComparativeExtractionOp/ComparativeAllExtractionOp 都写 `comparative_memories`，后者覆盖前者
**CE3S-M-03**：MemoryValidationOp 缺少 "Validating N memories" / "Validated N out of M" 汇总日志
**CE3S-M-04**：MemoryDeduplicationOp 缺少 "Starting deduplication" / "Deduplicated X to Y" 等多条日志
**CE3S-M-05**：UpdateVectorStoreOp 缺少 "Storing N memories" / "Successfully stored N" 等多条日志
**CE3S-M-06**：PersistMemoryOp "reme" 硬编码，Python 用 `_ALGO_NAME` 常量
**CE3S-M-07**：PersistMemoryOp helper 字段未导出，Python 有 `helper` 属性
**CE3S-M-08**：MemoryValidationOp.validateMemory 有 feedback fallback，Python 不做

### 提示 (T)

**CE3S-T-01**：日志 "Preprocessed trajectories" 使用英文（根据日志规范保留英文）
**CE3S-T-02**：ReMeMemory created_at 默认值回填逻辑与 Python 等价（确认正确）
**CE3S-T-03**：ParseJSONExperienceResponse fallback 路径不做校验（与 Python 行为等价）

---

## 五、Graph Memory (7.11+7.12)

### 严重 (S)

**GM-S-01：extraction_prompts.go 完全缺失（9 个核心提示词组装函数）**
- Python `extraction_prompts.py` 包含：extract_entity_declaration/extract_entity_attributes/extract_relation_declaration/extract_timezone/merge_existing_entities/filter_relations_for_merge/dedupe_entity_list/dedupe_relation_list/format_new_entities
- Go: 无文件
- **修复**：创建 `extraction_prompts.go`，逐一实现

**GM-S-02：parse_response.go 完全缺失（JSON 容错解析器）**
- Python: `parse_json(resp, output_schema)` 含代码块提取、截断修复、模糊 key 匹配
- Go: 无文件
- **修复**：创建 `parse_response.go`，实现 ParseJSON/rawDecodeJSON/TryGetKey/EnsureList

**GM-S-03：graph_memory/ 整个目录缺失（6 个文件）**
- 缺失：base.go（GraphMemory 主类 ~1139行）/ states.go / parse_llm_response.go / postprocess.go / utils.go / validate_input.go
- **修复**：创建目录并实现

**GM-S-04：FormatExistingRelations 时间处理逻辑完全缺失**
- Python: `update.py` 含 `load_stored_time_from_db` + valid_since/valid_until + offset 处理
- Go: `base.go:169-182` — `includeTime` 参数完全未使用，时间信息全部丢失
- **修复**：在 FormatExistingRelations 中添加时间处理分支

**GM-S-05：FormatNewEntities 缺失 entity_types 分支**
- Python: 有 entity_types 时先列类型定义再列实体（带类型名），无时简单列出
- Go: 只有简单列表，无 entity_types 参数
- **修复**：修改签名添加 `entityTypes []EntityDef` 和 `startIdx int` 参数

**GM-S-06：format_helpers.go（GetFormattingKwargs）缺失**
- Python: `get_formatting_kwargs()` 组装 source_description/extra_message/context 三个模板变量
- Go: doc.go 列出但文件不存在
- **修复**：创建 `prompts/format_helpers.go`

**GM-S-07：MultilingualBaseModel 的 strict 模式 BFS 遍历未实现**
- Python: `multilingual_model_json_schema(language, strict=True)` BFS 设 `additionalProperties: False`
- Go: `ResponseFormat` 永远 `"strict": false`
- **修复**：在 ResponseFormat 调用链中添加 strict 模式 BFS

**GM-S-08：EntityDef 缺少 Attributes 字段（EntityDefAttr）**
- Python: `EntityDef.attributes: MultilingualBaseModel = Field(default_factory=EntityDefAttr)`
- Go: `EntityDef` 只有 Name/Description
- **修复**：补充 Attributes 字段和 EntityDefAttr 结构体

### 一般 (M)

**GM-M-01**：custom_types.go 为空文件（Python 定义 JSONLike 类型）
**GM-M-02**：EntityDef/RelationDef Python 用类引用（type[EntityDef]），Go 用实例引用（*EntityDef）
**GM-M-03**：ReadableSchema 格式多了 `class Output:` 包装，Python 无此包装
**GM-M-04**：extract_relation_declaration 中 tz_info/reference_time 格式化逻辑缺失
**GM-M-05**：dedupe_relation_list 中 removeprefix 处理缺失
**GM-M-06**：GraphMemPrompting schema 默认值工厂函数缺失
**GM-M-07**：GraphMemUpdate Merge 逻辑缺失
**GM-M-08**：LookupTables GetEntity/GetRelation/GetEpisode 去重逻辑缺失
**GM-M-09**：UpdateEntity 函数缺失（_parse_summary/_parse_attributes）
**GM-M-10**：AssembleInvokeParams 函数缺失
**GM-M-11**：parse_json 容错解析链路缺失（代码块+raw_decode+截断修复+模糊key）
**GM-M-12**：extract_entity_attributes 中 human 实体 summary_target 倍增逻辑缺失
**GM-M-13**：EnsureValidLanguage 缺少 Python 的非字符串类型处理（Go 强类型不需要）
**GM-M-14**：FormatExistingEntities 只替换 {i}/{name}/{content}，Python 的 str.format 支持任意字段

### 提示 (T)

**GM-T-01**：doc.go 引用了不存在的 extraction_prompts.go 和 parse_response.go
**GM-T-02**：prompts/doc.go 引用了不存在的 format_helpers.go
**GM-T-03**：ParsePRContent 中有无用死代码（L53-78 首次遍历循环）
**GM-T-04**：VectorNode Embedding `omitempty` 导致 JSON 输出差异（nil vs [] vs null）

---

## 六、Worktree (9.66a)

### 严重 (S)

**WT-S-01：Enter/Exit 从未调用 fire* lifecycle hook**
- Python: `manager.py` 中 `_fire_rail("before_worktree_create", ...)` / `_fire_rail("after_worktree_create", ...)` 等
- Go: `manager.go` 定义了 fireBeforeCreate/fireAfterCreate 等 7 个方法（L459-L563），但 Enter/Exit/CreateOwnerWorktree 中**从未调用**
- **影响**：AutoSetupRail、DiffSummaryRail 等所有 hook 永远不会触发
- **修复**：在 Enter 的 backend.Create 前后调用 fireBeforeCreate/fireAfterCreate，在 Exit 变更检查前后调用 fireBeforeExit/fireAfterExit

**WT-S-02：Exit "remove" 分支用 `SetCurrentSession(ctx, nil)` 清除 session，与 Enter 设置路径不一致**
- Python: 两处都用 `set_current_session(None)` 单一路径
- Go: Enter (L145) 用 `m.sessionState.SetCurrentSession(session)`，Exit keep (L209) 用 `m.sessionState.SetCurrentSession(nil)`，Exit remove (L225) 用 `SetCurrentSession(ctx, nil)`
- **影响**：Exit remove 路径可能因 ctx 无 WorktreeSessionState 而静默失败，session 残留
- **修复**：L225 改为 `m.sessionState.SetCurrentSession(nil)`

**WT-S-03：ExitWorktreeTool.Invoke 获取 session 路径与 EnterWorktreeTool 不一致**
- Go: `tools.go:107-111` EnterTool 优先用 `t.manager.SessionState().GetCurrentSession()`
- Go: `tools.go:160` ExitTool 只用 `GetCurrentSession(ctx)`
- **修复**：ExitTool 应与 EnterTool 保持一致

**WT-S-04：AutoSetupRail.AfterWorktreeCreate 用 `context.Background()` 丢弃上游 ctx**
- Python: 直接在当前 async 上下文中运行，继承 cancellation scope
- Go: `rails.go:350` — `context.WithTimeout(context.Background(), 120*time.Second)`
- **影响**：上游 context 取消后 setup 命令仍继续运行
- **修复**：改为 `context.WithTimeout(ctx, 120*time.Second)`

**WT-S-05：OnWorktreeSync 签名缺 direction/files 参数**
- Python: `on_worktree_sync(ctx, session, direction, files) -> list[str]`
- Go: `backend.go:39` — `OnWorktreeSync(ctx, session) error`
- **修复**：签名改为 `OnWorktreeSync(ctx, session, direction string, files []string) ([]string, error)`

**WT-S-06：OnWorktreeFileWrite 返回 error 非 bool**
- Python: `on_worktree_file_write(ctx, session, file_path) -> bool`（True=允许，False=阻止）
- Go: `backend.go:30` — `OnWorktreeFileWrite(ctx, session, filePath) error`
- **修复**：返回 `(bool, error)`，bool 表示是否允许

**WT-S-07：BeforeWorktreeCreate/BeforeWorktreeExit 空字符串表示不干预的约定未文档化**
- Python: 返回 `str | None`，None=不干预
- Go: 返回 `(string, error)`，空字符串=不干预
- **修复**：接口注释明确约定；或改为 `*string`（nil=不干预）

**WT-S-08：fireBeforeCommit 不链式传递修改后的 message**
- Go: `manager.go:529-542` — 每次循环传原始 message
- **修复**：如果 `r != ""`，则 `message = r`，让下一个 rail 看到修改后值

### 一般 (M)

**WT-M-01**：findCanonicalGitRoot 返回值双条件判断（err!=nil || repoRoot==""），容易遗漏
**WT-M-02**：WorktreeRail.BeforeInvoke 未同步更新 manager.sessionState
**WT-M-03**：WorktreeRail.AfterInvoke 未通过 manager.sessionState 获取 session
**WT-M-04**：session 操作路径不一致（ctx vs manager.sessionState）
**WT-M-05**：NewWorktreeConfig 默认 Enabled=false，Python WorktreeConfig(enabled=True)
**WT-M-06**：cleanup.go errgroup 使用不正确（goroutine 不返回 error，Wait 被忽略）
**WT-M-07**：copyFile 不保留 atime（Python shutil.copy2 保留）
**WT-M-08**：DiffSummaryRail.BeforeWorktreeExit 返回 ("", nil) 而非 nil（可能覆盖 action/slug）
**WT-M-09**：resolveOwner 从 inputs 读取而非 kwargs（语义不一致）
**WT-M-10**：EnterWorktreeTool.Invoke 返回值不含 success 字段

### 提示 (T)

**WT-T-01**：randomInt 模偏差（`int(b[0]) % n` 当 n 不整除 256 时）
**WT-T-02**：TeamLifecycle 字段始终为空
**WT-T-03**：LifecyclePolicy 存储解析值（EPHEMERAL）而非原始 AUTO
**WT-T-04**：WorktreeEventHandler 返回 error 但调用处忽略
**WT-T-05**：WorktreeConfig.Enabled 默认值 Go=false vs Python=True
**WT-T-06**：readWorktreeHeadSHA 错误处理差异（Go 记不必要 error 日志）
**WT-T-07**：generateRandomSlug 模偏差（建议用 crypto/rand.Int）

---

## 七、Evolution Rail / DeepAdapter / Swarm / AgentConfigurator / Session

### 严重 (S)

**EV-S-01：agent_configurator.go SetupInfra 缺少 messager 创建（步骤 5）**
- Python: `agent_configurator.py:219-224` — `self.messager = create_messager(messager_config)`
- Go: `agent_configurator.go:198-199` — `// TODO(#9.65)` 未实现
- **影响**：`c.Messager()` 为 nil，后续 SetupTeamBackend 传入 nil

**EV-S-02：agent_configurator.go SetupInfra 缺少模型分配器构建（步骤 7）**
- Python: `agent_configurator.py:229-234` — `self.model_allocator = build_model_allocator(spec, ctx.team_spec)`
- Go: `agent_configurator.go:209-210` — `// TODO(#9.64)` 未实现

**EV-S-03：agent_configurator.go SetupInfra 缺少 SetupTeamBackend 调用（步骤 8）**
- Python: `agent_configurator.py:236-242` — `self.setup_team_backend(spec, ctx, self.messager, ...)`
- Go: `agent_configurator.go:213` — `// TODO(#9.58)` 未实现

**EV-S-04：agent_configurator.go WorktreeManager 条件缺少 `spec.Worktree.Enabled`**
- Python: `agent_configurator.py:244` — `if ctx.role != TeamRole.LEADER and spec.worktree and spec.worktree.enabled`
- Go: `agent_configurator.go:217` — `if ctx.Role != atschema.TeamRoleLeader` 缺少 worktree.enabled 检查
- **影响**：非 leader 角色即使 worktree 未启用也会创建 WorktreeManager
- **修复**：`if ctx.Role != atschema.TeamRoleLeader && spec.Worktree != nil && spec.Worktree.Enabled`

**EV-S-05：WorktreeManager 事件处理器可能导致双重发布**
- Go: `agent_configurator.go:441-465` 事件处理器中调 `tb.PublishEvent`
- Python: 只在 WorktreeManager 内部发布事件
- **修复**：确认 Go 的 worktree.NewWorktreeManager 内部是否也发布事件

**EV-S-06：DeepAdapter.updateRuntimeConfig 缺少 updateRailsForMode 调用**
- Python: `_update_rails_for_mode` 在 `_update_runtime_config` 内部被调用（L2908-2910）
- Go: `updateRuntimeConfig` 方法存在但未调用 `updateRailsForMode`
- **影响**：模式切换时 rails 不更新，plan 模式下 TaskPlanningRail/SkillEvolutionRail 不会按需注册
- **修复**：在 `updateRuntimeConfig` 末尾添加 `d.updateRailsForMode(rc.Mode)` 调用

**EV-S-07：DeepAdapter.buildStreamEventRail 返回 nil（空实现）**
- Python: `_build_stream_event_rail()` (L2051-2080) 构建了 `JiuClawStreamEventRail`
- Go: `buildStreamEventRail()` L406-409: `return nil`
- **影响**：ProcessMessageStreamImpl 中 reset_abort 跳过；ProcessInterrupt 中 pause/resume/abort 全部跳过
- **修复**：实现 JiuClawStreamEventRail Go 等价，至少支持 pause/resume/abort/reset_abort

**EV-S-08：DeepAdapter.buildSkillCreateRail 返回 nil**
- Python: `_build_skill_create_rail()` (L2011-2050) 构建了 SkillCreateRail
- Go: L398-401: `return nil`
- **影响**：技能创建功能完全缺失
- **修复**：实现 SkillCreateRail Go 等价

**EV-S-09：DeepAdapter.buildResponsePromptRail 返回 nil**
- Python: `_build_response_prompt_rail()` (L2171-2180) 构建了 ResponsePromptRail
- Go: L631-634: `return nil`
- **影响**：响应提示词 rail 缺失影响输出格式控制
- **修复**：实现 ResponsePromptRail Go 等价

**EV-S-10：WorktreeRail.BeforeInvoke CWD 恢复可能静默失败**
- Python: `before_invoke` L187-188: `set_cwd(stored.worktree_path)` — ContextVar 始终有值
- Go: L302-306: `cwdState.SetCwd` — `cwdState` 可能为 nil（`cwd.CwdStateFromCtx(ctx)` 返回 nil 时）
- **影响**：CWD 恢复被静默跳过，工具在错误目录执行
- **修复**：cwdState 为 nil 时记录警告，或回退到 `cwd.SetCwd(ctx, ws.WorktreePath)`

### 一般 (M)

**EV-M-01**：agent_configurator.go CreateWorkspaceManager team_name 缺少 `spec.TeamName` 兜底
**EV-M-02**：agent_server.go Stop() 中 4 个关键方法为空 stub（已知有意延后）
**EV-M-03**：session_manager.go waitTaskDone 用 10ms 轮询代替 await（CPU 开销）
**EV-M-04**：session_manager.go EnsureSessionProcessor 重建时不关闭旧 signal channel（内存泄漏）
**EV-M-05**：EvolutionRail.BeforeInvoke 中 sessionID 解析 fallback 不完整
**EV-M-06**：SkillEvolutionRail.initSharing 中 recover() 无法捕获 error（只能捕获 panic）
**EV-M-07**：TeamSkillEvolutionRail.OnAfterToolCall 中 view_task 完成检测过于简单
**EV-M-08**：DeepAdapter.handleSlashCommand 中 /evolve_rollback 不处理 TeamSkillEvolutionRail

### 提示 (T)

**EV-T-01**：agent_configurator.go BuildMemoryManager 中 Harness 可能为 nil 时 panic
**EV-T-02**：agent_server.go SendPush recover 无法获取 stack trace
**EV-T-03**：DeepAdapter.buildSkillEvolutionRail 中 language 硬编码 "cn"（应使用 resolveRuntimeLanguage）
**EV-T-04**：DeepAdapter.buildExternalMemoryRail 返回 nil（已知 ⤵️ 标记）
**EV-T-05**：DeepAdapter.ProcessMessageStreamImpl 步骤 15 streamEventRail.reset_abort 缺失
**EV-T-06**：DeepAdapter 中多处 `⤵️` 标记的未实现功能（A2X/cron/SkillCreate/ExternalMemory/ResponsePrompt/streamEventRail/load_user_rails）

---

## 八、Workspace / Embeddings / ProjectMemoryRail / RuntimePromptRail

### 严重 (S)

**WS-S-01：英文 workspace schema 缺少 coding_memory 目录**
- Python `DEFAULT_WORKSPACE_SCHEMA_EN` 包含 `coding_memory` 节点
- Go `defaultWorkspaceSchemaEN` 从 `memory` 直接跳到 `todo`
- **修复**：在 memory 和 todo 之间加入 coding_memory 节点

**WS-S-02：runtime_prompt_rail.go samePath 缺少 filepath.Abs**
- Python: `os.path.normcase(os.path.abspath(left)) == os.path.normcase(os.path.abspath(right))`
- Go: `filepath.Clean(strings.ToLower(left)) == filepath.Clean(strings.ToLower(right))`
- **影响**：传入相对路径时错误判断为"相同"
- **修复**：先做 `filepath.Abs` 再比较

**WS-S-03：project_memory_rail.go AfterToolCall 类型断言可能遗漏非 ToolCallInputs 的 tool_name**
- Python: `getattr(ctx.inputs, "tool_name", "")` 更宽容
- Go: `cbc.Inputs().(*agentinterfaces.ToolCallInputs)` 类型断言失败时 toolName=""
- **修复**：确认所有 after_tool_call 回调的 Inputs 类型是否统一

**WS-S-04：embeddings.go CreateEmbeddingProvider 缺少 error log 路径**
- Python: `embedding_config is None` 时 error log 后返回
- Go: embeddingConfig 为 nil 时直接走 fallback mock，无 error log
- **修复**：加 `logger.Error` 后再走 fallback

### 一般 (M)

**WS-M-01**：runtime_prompt_rail.go SetRuntimePaths 空字符串 vs Python None 语义差异
**WS-M-02**：runtime_prompt_rail.go osType 大写化（"Linux" vs Python "linux"）
**WS-M-03**：project_memory_rail.go SetAdditionalDirectories EvalSymlinks 失败降级
**WS-M-04**：embeddings.go MockEmbeddingProvider 随机种子计算方式与 Python 不同（平台限制）

### 提示 (T)

**WS-T-01**：project_memory_rail.go Uninit 方法签名与 Python 不同（Go 惯例）
**WS-T-02**：list_team_links/list_worktree_links 返回类型差异（已知简化）
**WS-T-03**：embeddings.go 无 OpenAICompatibleEmbeddingProvider（Go 用 apiEmbedding 适配）

---

## 九、关键流程对比验证

### ReMe Retrieve 流程

```
Python: RecallMemoryOp >> RerankMemoryOp >> RewriteMemoryOp
  RecallMemoryOp:
    ✅ embed(query) → search(vector_store, top_k)
    ❌ 直接取 metadata 跳过 from_vector_node（CE3R-S-01）
    ❌ user_id 为空时缺少 workspace_id 过滤（CE3R-S-02）
  RerankMemoryOp:
    ❌ 解析失败时不截断（CE3R-S-03）
    ✅ 重排序逻辑正确
  RewriteMemoryOp:
    ✅ llm_rewrite=false 降级正确
    ✅ LLM 改写 + fallback 正确
```

### ReMe Summary 流程

```
Python: TrajectoryPreprocessOp >> Success | Failure | Comparative
        >> MemoryValidationOp >> MemoryDeduplicationOp >> UpdateVectorStoreOp >> PersistMemoryOp

  TrajectoryPreprocessOp: ❌ threshold=0 误替换（CE3S-S-01）
  SuccessExtractionOp: ❌ LLM 失败 continue vs Python 抛异常（CE3S-S-06）
  FailureExtractionOp: ❌ 同上
  ComparativeExtractionOp: ❌ LLM 失败 return err（CE3S-S-02）
  ComparativeAllExtractionOp: ❌ 同上（CE3S-S-03）
  MemoryDeduplicationOp: ❌ GetAll vs Search（CE3S-S-04）/ embedding nil 加入 vs 跳过（CE3S-S-05）/ Embed vs EmbedBatch（CE3S-S-07）
  UpdateVectorStoreOp: ✅ embedding 内容确认一致
  PersistMemoryOp: ✅ 对齐正确
```

### Graph Memory Extraction 流程

```
Python 完整流程:
  LLM Invoke → parse_json → _parse_summary/_parse_attributes → UpdateEntity → AssembleInvokeParams

Go 当前状态:
  ✅ extraction_models.go — 输出模型定义正确
  ✅ entity_type_definition.go — EntityDef/RelationDef 基本对齐
  ✅ base.go — ReadableSchema/ResponseFormat/FormatSchemaInfo
  ✅ registry — 多语言注册
  ✅ prompts/manager.go — TemplateManager
  ❌ extraction_prompts.go — 9 个组装函数缺失
  ❌ parse_response.go — JSON 容错解析缺失
  ❌ format_helpers.go — GetFormattingKwargs 缺失
  ❌ graph_memory/ — 6 个文件全部缺失
```

---

## 十、综合待回填占位检查

| 占位标记 | 位置 | 状态 | 说明 |
|---------|------|------|------|
| `7.22-7.23 Migrator + run_migrations` | 7.21 | ☐ 未实现 | 正确，MigrationPlan 只部分提前 |
| `9.24 P6 ContextEvolutionRail` | 9.24 | ☐ 未实现 | 正确，标记为 `⤴️9.82 P7` |
| `9.38-49 Playwright MCP 端到端` | 9.26 | 6处占位 | 正确，BrowserAgent 6 处占位 |
| `9.68-69 team.plan 特化` | 9.28 | ⤵️ | 正确，PlanAgent 未含 TeamPlanModeRail |
| `9.82 P4-P7` | 9.82 | ☐ 未实现 | 正确，仅 P1-P3 已完成 |
| `agent_configurator.go SetupInfra` 步骤 5/7/8 | 9.57 | ☐ TODO | 正确，标注为 TODO |
| `graph_memory/ 6 个文件` | 7.11+7.12 | ☐ 未实现 | 正确，当前只完成了 extraction 子包 |
| `extraction_prompts.go` | 7.11+7.12 | ☐ 未实现 | 正确，doc.go 已列出 |
| `parse_response.go` | 7.11+7.12 | ☐ 未实现 | 正确，doc.go 已列出 |

---

## 十一、修复优先级建议

### P0 — 必须立即修复（功能正确性 / 数据安全）

1. **CE3R-S-01**：RecallMemoryOp 改用 `NewReMeMemoryFromVectorNode` 反序列化
2. **CE3R-S-02**：RecallMemoryOp user_id 为空时使用 `"default"` 并始终设 workspace_id
3. **CE3S-S-05**：MemoryDeduplicationOp embedding 为 nil 时不加 uniqueMemories
4. **CE3S-S-01**：TrajectoryPreprocessOp threshold=0 误替换
5. **WT-S-01**：Enter/Exit 中调用 fire* lifecycle hook
6. **WT-S-02**：Exit remove 分支统一用 `m.sessionState.SetCurrentSession(nil)`
7. **WT-S-04**：AutoSetupRail 用传入 ctx 而非 `context.Background()`
8. **EV-S-04**：WorktreeManager 条件补 `spec.Worktree.Enabled`
9. **WS-S-01**：英文 schema 补 coding_memory 目录
10. **WS-S-02**：samePath 补 filepath.Abs

### P1 — 本轮迭代修复

11. **CE2-S-03**：ReMeMemory.FromVectorNode 移除 `time.Now()` 回填
12. **CE1-S-01**：MemoryPersistenceHelper 默认改为 "auto"
13. **CE3S-S-06**：统一 ExtractionOp 错误处理策略
14. **CE3S-S-07**：MemoryDeduplicationOp 改用 EmbedBatch
15. **CE3R-S-03**：RerankMemoryOp 解析失败时仍截断
16. **WT-S-05/S-06**：OnWorktreeSync/OnWorktreeFileWrite 签名对齐 Python
17. **EV-S-05**：确认 WorktreeManager 事件是否双重发布
18. **CE1-S-03**：saveJSON 加载失败返回 error

### P2 — 后续迭代修复

19. **GM-S-01~S-06**：Graph Memory 缺失文件（extraction_prompts/parse_response/format_helpers/graph_memory 目录）
20. **CE2-S-01/S-02**：TaskMemory/PersonalMemory/Ours 系列
21. **CE1-S-02**：ServiceContext 并发安全
22. **CE1-M-02/M-03**：SequentialOp/ParallelOp 创建新对象而非修改自身
23. **GM-S-07/S-08**：strict 模式 BFS / EntityDef Attributes
