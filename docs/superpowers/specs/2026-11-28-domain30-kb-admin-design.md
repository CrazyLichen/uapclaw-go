# 知识库管理页签（WeKnora 对标）整体方案设计

> 日期：2026-11-28 · 状态：设计定稿（用户逐节确认）· 性质：**新增能力域**（上游 Python 无对应物，对标 Tencent/WeKnora 组织方式）
> 对应计划章节：`IMPLEMENTATION_PLAN.md` 领域三十（30.1-30.9）

## 1. 背景与目标

为本项目新增「知识库」一级页签，提供知识库增删改查全管理面。组织方式对标腾讯 WeKnora（KB → 文档 → 分块三层模型）。后端检索/解析内核完全复用既有 `core/retrieval` 包（13.19-13.28 已收口），本方案只新增**管理编排层 + 通信面 + 前端**。

### 已拍板决策（澄清阶段确认）

| 决策点 | 结论 |
|---|---|
| 管理范围 | KB CRUD + 文档管理 + 分块管理 + 检索测试，四层全量 |
| KB 实现范围 | 全部三套：SimpleKB + MultiKB 聚合 + GraphKB |
| 解析任务 | goroutine 池 + SQLite 任务表 + WS 主动推送 + progress 端点兜底（不引 Redis） |
| 权限模型 | 单用户/全局共享，无 owner 概念 |
| 前端形态 | 顶级独立页签（非 settings 模块） |
| 解析格式 | 顺带补 CSV；PPT/EPUB/XMind ⤵️ 延后 |
| 原文定位 | 全量方案：pdf 页码+bbox，前端内嵌 pdf.js 渲染高亮 |
| 通信面 | **全 WS**：管理请求 E2A 转发、上传/下载走 WS 分帧通道，零独立 HTTP 路由；gateway 保持薄转发 |
| 上传护栏 | 单文件默认上限 50MB（KB config 可调）+ 通道流量控制 |

## 2. 总体架构

```
前端 features/knowledge 页签
   │  WS 请求（kb.* 方法族，走既有 WS 通道）
   ▼
Gateway（薄转发，零业务逻辑、零 HTTP 路由）
   │  E2A 请求（ChannelTransport 既有机制）
   ▼
AgentServer → kb_admin 域（新增，业务编排层）
   ├─ KBService：知识库 CRUD/置顶/文件夹树/AI 摘要标签
   ├─ DocService：导入/解析任务编排/重解析/取消/删除/预览
   ├─ ChunkService：分块列表/编辑(乐观锁)/启停/生成问题管理
   ├─ SearchService：KB 内检索测试（调 retrieval Retrieve）
   ├─ ParseWorker：goroutine 池 + 任务状态表（SQLite DAO）
   └─ 事件推送：解析进度 → 既有事件通道 → WS 推回前端
        │ 编排调用（不侵入，例外见 §7）
        ▼
   core/retrieval（KnowledgeBase 接口 / parser / chunker / retriever / embedding）
```

- kb_admin 是**编排层**：只做管理语义（状态机、任务、DTO 组装），检索/解析内核全部复用 retrieval。
- AI 摘要/生成问题：解析完成后 worker 二阶段调 LLM（复用既有模型配置），失败不阻塞主解析。

## 3. 数据模型（SQLite 新表）

### kb_info（知识库元数据）
| 字段 | 说明 |
|---|---|
| id, name, description | description 可由 AI 生成（WeKnora generated_profile 对应物） |
| type | `document` / `graph`（SimpleKB 与 GraphKB）；MultiKB 以聚合配置表达 |
| config_json | embedding 模型、chunker 类型/参数、检索默认参数（topK/阈值/rerank）、生成问题开关 |
| folder_path | 文件夹树路径 |
| pinned, created_at, updated_at | 置顶与时间戳 |

### kb_doc（文档元数据）
| 字段 | 说明 |
|---|---|
| id, kb_id, file_name, folder_path | 归属与组织 |
| parse_status | `pending → processing → completed / failed / cancelled`；processing 内含 stages 细分（见 §4） |
| stages_json, last_activity_at, stall_state | 阶段明细 + 活动追踪（对齐 WeKnora） |
| error_msg, retry_count | 失败原因与重试计数 |
| summary, tags_json | AI 摘要与标签 |
| enhance_state | LLM 二阶段状态（成功/失败独立标记，不阻塞 completed） |
| meta_json, content_hash | 格式/大小/页数；同 KB 内重复上传冲突判据 |

### kb_chunk_meta（分块管理元数据，chunk 本体仍在向量库）
| 字段 | 说明 |
|---|---|
| id, doc_id, kb_id | 归属 |
| chunk_type | text / image_ocr / image_caption（对齐 WeKnora 类型集） |
| enabled | 启停开关（检索侧过滤） |
| revision, content_hash | 乐观锁；变更历史存 `kb_chunk_revision` 表（diff/回滚） |
| locators_json | 原文定位：pdf 页码+bbox、docx 段落号、字符区间 |
| questions_json | 生成问题列表及 embedding 存储状态 |

### kb_task（解析任务状态机）
- id、doc_id、kb_id、status、progress（0-100 + stage 描述）、created/started/finished_at
- worker 每完成一个 span 更新 `last_activity_at`；长时间无活动标「疑似停滞」，提供 cancel 入口
- 进程重启：孤儿 processing 任务标 cancelled（reason=server_restart），可一键重解析

### 向量库侧扩展
生成问题在同 collection 以独立向量条目存储，metadata `kind=generated_question, chunk_id=xxx`，检索时聚合回 chunk；不新建 collection。**生成问题召回为可选参数**：KB config 级开关（默认）+ 检索请求级参数 `use_generated_questions`（临时覆盖，便于对比召回效果）。

## 4. 解析状态机（对齐 WeKnora）

- 状态集：`pending → processing → completed / failed / cancelled`
- processing 内 stages：`parsing（格式解析）→ chunking（分块）→ embedding（向量化）→ extracting（LLM 二阶段：摘要/标签/生成问题）`，各阶段独立开始/结束时间与失败信息
- cancel 语义：processing 中可取消 → cancelled 终态；failed 与 cancelled 均可 reparse
- 文档列表按状态/阶段过滤

## 5. 通信面：WS 方法族

管理操作与进度推送同走 WS，gateway 薄转发经 E2A 到 agentserver。命名对齐 WeKnora REST 语义。

**kb.kb.***：create / list / get / update / delete / pin / move / setConfig / statistics / generateProfile（异步）

**kb.doc.***：upload（发起，文件本体走数据帧）/ importUrl / importManual / list / get / update / delete / batchDelete / reparse / batchReparse / cancelParse / regenerateSummary / preview / download

**kb.chunk.***：list / update / revisions / revert / delete / generateQuestions / listQuestions

**kb.chunker.***：preview（无状态试切，传文本+chunker 配置实时返回切分效果）

**kb.search.***：test（query + topK + 阈值 + rerank 开关 + use_generated_questions；返回命中 chunk + 相似度 + 召回来源徽标：向量/稀疏/生成问题）

**kb.task.***：progress（重连兜底全量对齐）/ subscribe（正常路径 WS 推送，无轮询）

### 文件传输（WS 分帧）
- 上传：前端切帧发送，agentserver 流式落盘（不整块驻内存），收完校验 hash 后触发解析任务；控制面（upload 发起/结果）与数据面（文件帧）分离
- 下载/预览：反向流式，前端聚合为 `ArrayBuffer → blob: URL`，pdf.js 吃 blob URL 渲染并按 bbox 高亮
- 护栏：单文件默认 50MB（KB config 可调）；帧通道流量控制/背压，防止大文件占满 WS 影响正常会话

## 6. 前端页面设计（features/knowledge，顶级页签）

**页面 1 知识库列表**：搜索框 + 新建按钮；文件夹树侧栏过滤；卡片（名称/AI 描述/类型/统计/置顶星标），置顶+更新时间排序。创建向导三步：①名称+文件夹 ②embedding + chunker 配置（内嵌试切预览面板实时看切分）③检索默认参数 + 生成问题开关。

**页面 2 知识库详情**：左侧文档列表（文件夹分组、状态/阶段过滤 tab、批量选择操作栏、拖拽上传 → 确认对话框 → 分帧上传 → 列表 pending 条目）；条目内联解析时间线（stages+耗时）、取消/重解析按钮、停滞标记；右侧检索测试面板（参数区 + 结果卡片带召回来源徽标，点击跳分块）。

**页面 3 文档详情**：上半元信息（摘要/标签/状态）+ 下载/预览/重解析；下半分块列表（分页+类型过滤），chunk 卡片展开：inline 编辑（乐观锁，冲突提示刷新）、启停、删除、版本历史 diff+回滚、生成问题管理、原文定位跳转（pdf.js 页码+bbox 高亮；docx 段落/字符区间显示定位描述）。

交互通用：解析进度 WS 订阅实时刷新；断线重连调 kb.task.progress 对齐；错误复用既有通知组件。

## 7. 错误处理

- **错误码族**（复用 E2A envelope 分流）：`KB_NOT_FOUND` / `KB_CONFIG_INVALID` / `DOC_DUPLICATE` / `CHUNK_REVISION_CONFLICT`（附最新 revision）/ `TASK_STALLED_CANCELLED` / `FILE_TOO_LARGE` / `UPLOAD_CHANNEL_BUSY`
- **失败分级**：格式解析失败（标 `parse`）/ embedding 索引失败（标 `index`，可重解析）/ LLM 二阶段失败 → 不置 failed，主文档 completed + `enhance_failed`，可单独补做
- **一致性**：chunk 删除/编辑先写向量库再更新 DAO，向量库失败回滚并返回错误（操作顺序固定）
- **WS 中断**：上传中断清理临时分片；下载中断前端重拉

## 8. 对 retrieval 的唯一侵入点（例外登记）

source_locators 全量方案要求 parser/chunker 侧携带坐标信息：
- pdf 解析需提取每块页码+bbox（引入坐标提取链）
- docx 段落号、纯文本字符区间随 chunk 传递
- chunk 元数据落 DAO 时同步写入 `locators_json`

此为 retrieval 包本批唯一改动，实施时单独提交、单独登记偏差/影响面。

## 9. 格式支持

现有 9 类（pdf/word/excel/html/web_page/wechat_article/image/json/txt/md）+ 本批补 **CSV**。PPT/EPUB/XMind ⤵️ 延后（登记到计划表）。

## 10. 测试策略

- **单测 ≥85%**：各 Service 用真实 SQLite（内存库）+ 真实 retrieval 管线（遵守真实链路测试纪律，禁窄接口/替身）；状态机转移表驱动（全状态×事件矩阵）；乐观锁冲突、hash 去重、分帧上传粘包/断包场景
- **集成测试**（过双层审计门禁）：WS 通道 → gateway 薄转发 → agentserver kb_admin → retrieval 解析入库 → 检索返回全链（txt/md/csv 小真实文件）；上传-解析-进度推送-检索测试-分块编辑-回滚端到端
- **前端**：试切预览/解析时间线/检索测试面板组件级测试；pdf.js 高亮标 `e2e` 隔离

## 11. 已知延后项（⤵️）

| 项 | 原因 |
|---|---|
| FAQ 库 / Wiki 库类型 | 本批聚焦文档型 + GraphKB；FAQ/Wiki 语义差异大 |
| KB 发布为 MCP 端点 | 独立能力面，另行立项 |
| 共享权限/多用户 | 单用户模型已拍板 |
| PPT/EPUB/XMind 解析 | 需新解析依赖 |
| bbox 高亮以外的富预览（docx 在线渲染等） | 轻量定位描述已覆盖 |
