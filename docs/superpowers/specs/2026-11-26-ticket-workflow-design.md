# 工单问题诊断闭环（TicketWorkflow）设计文档

- 日期：2026-11-26
- 状态：待评审
- 背景：外部工单系统通过 HTTP 触发本平台的诊断编排流，实现"历史知识库 → 业务知识库 → Agent+Skill"三级问题处理闭环，含渐进式引导、历史沉淀与 skill 自演进。

## 1. 需求与边界

### 1.1 需求

1. 用户提出产品问题（描述 + 可能的图片/日志附件），三级处理：
   - **第一级**：历史知识库（本平台沉淀的归因库）命中且有方案 → 直接返回结果 + 历史问题关联信息（ticket_id）；
   - **第二级**：历史库未命中或用户反馈未解决 → 业务知识库查询，命中处理同上，但用户确认解决后**只写历史库，业务库不动**；
   - **第三级**：两级均未成功 → 进入诊断 Agent（挂专门的问题定位 skill，用户侧预置），结合问题描述与附件日志分析给出结论。
2. 渐进式引导：新问题召回多个不同方向的历史结果时，不急于给答案，先生成判别式澄清选项引导用户补充，补充后再匹配。
3. 越用越准：历史知识库随闭环沉淀；skill 经 SkillEvolutionRail 自演进（auto_save，无需人工确认）。

### 1.2 边界与拍板记录

| 决策点 | 结论 |
|---|---|
| 实现位置 | 本仓（uapclaw-gateway）内，复用既有 KB/agent/skill/HITL 设施 |
| 多租户 | 不做，单用户 |
| 工单实体 | 外部工单系统持有，仅通过 HTTP 调用触发；两侧靠 `ticket_id` 关联 |
| 编排形态 | 方案 B：仓内 workflow（PregelGraph）编排；等待外部输入采用 **END + 持久化状态 + 重进路由**，不用图内 interrupt |
| 知识库引擎 | 历史库与业务库均为 GraphKnowledgeBase（两个独立 collection），分类树是三元组的查询侧视图，不是独立存储 |
| 初始 skill | 用户侧预置提供，本设计只负责装配与触发，不编写初始 skill 内容 |
| 业务知识库导入面板 | 独立成第二阶段子项目，本 spec 只定导入 API 契约（workflow 只依赖契约） |
| CI 结果 | 默认不追踪（规则 11），完成标准 = 本地编译 + vet + 单测 + 集成测试 + 推送 |

## 2. 对外接口（零新增 URL，向后兼容扩展）

复用既有会话创建 / send / metadata 轮询接口，扩展点全部为可选字段：

1. **会话创建**：session metadata 增加 `mode=workflow.ticket`（会话级固定），server 侧按 mode 分流。
2. **send**：请求体增加可选 `biz_event` 字段：
   - 不传 = 普通评论消息（旧行为不变）；
   - `confirm_solved`（用户确认解决）/ `not_solved`（未解决）/ `close`（工单关闭）。
   - 附件（图片/日志）跟随 message 传递，复用仓内附件安全检查通道。
3. **GET session metadata 轮询**：返回 metadata 新增扩展键：

```json
{
  "state": "guiding | wait_confirm | closed | abandoned | ...",
  "options": [{"label": "偶发还是必现?", "hint": "下游限流类多为高峰期偶发"}],
  "result": null,
  "kb_source": null,
  "ticket_id": "T-123"
}
```

- `result` 在 `wait_confirm` 阶段即交付全量（方案 + kb_source + ticket 关联）；`closed` 仅表示"已沉淀完成"的回执，result 不再变化。终态定义权在工单系统（他们确认解决并关单后下发 `biz_event=close`）。

## 3. 总体架构

```
外部工单系统
  │ ①POST 建会话(session_id, mode=workflow.ticket) ②POST send(问题+附件+ticket_id[, biz_event])
  │ ③GET metadata 轮询  ④send(biz_event=close)
  ▼
agentserver 入口（session metadata mode 分流）
  ├── mode 为空/普通 ──▶ 既有 DeepAgent 流式会话链路（零改动）
  └── mode=workflow.ticket ──▶ TicketWorkflow runner
        （读 store 状态 → 执行一次 → 写 store → END）
```

Workflow 注册表（`mode → workflow builder`）是通用机制：后续新增"聊天问答式"workflow 只需注册新类型 + 定义自己的 metadata 扩展键约定，接口与生命周期协议全部复用。

### 3.1 节点与状态

节点（图节点，6 个）与状态（store 持久化的阶段标记）是两个层面：

| 节点 | 职责 | 产出的状态 |
|---|---|---|
| RouterNode | 每次执行入口，读 store 状态路由；处理 biz_event | `searching` 等 |
| IntentNode | 附件预处理（图片→视觉模型抽现象；日志→文本抽关键字）+ 特征抽取，产出结构化特征 | `pending_intent`→路由 |
| GuidanceNode | 渐进式引导：开放式澄清 / 判别式选项生成 | `guiding` |
| KBSearchNode | GraphRetriever 检索（scope=history/business 参数化），方向判定 | `wait_confirm` |
| AgentNode | 诊断 DeepAgent（skill 分析 + 附件日志） | `wait_confirm` |
| SettleNode | 关闭后归档：写历史库 / 命中计数 / 演进信号 | `closed` / `abandoned` |

状态转移：

```
init ──Intent模糊──▶ guiding（开放式澄清，不查库）
init ──Intent就绪──▶ searching
guiding ──用户补充──▶ searching（scope 不变，重新检索）
searching ──历史库高置信单向──▶ wait_confirm（历史方案+ticket关联）
searching ──历史库多向──▶ guiding（判别式引导）
searching ──历史库无命中──▶ searching（scope=business 重查）
searching ──业务库命中──▶ wait_confirm（业务库方案）
wait_confirm ──biz_event=confirm_solved──▶ 等待 close
wait_confirm ──biz_event=not_solved / 普通评论──▶ scope 升级重查或 AgentNode
biz_event=close ──▶ SettleNode ──▶ closed
轮数超限/scope 已到顶 ──▶ AgentNode
```

每次执行遇 `guiding` / `wait_confirm` 必然 END；执行体永远短促无挂起。

### 3.2 跨轮上下文（store 持久化）

- `intent_features`：累积的结构化特征（结构化键值，**不是向量**；向量只出现在检索查询文本上），每轮 Intent 合并；
- `guidance_rounds`：已引导次数（上限 2~3，可配置），开放式与判别式合并计数；
- `scope`：当前库级，history → business → agent 只进不退；
- `hit_doc_ids`：历史命中案例 doc_id（关闭时直接 `hit_count+1`，零判重）；
- trail（可选，YAGNI 可缓）：各节点检索/结论摘要，供沉淀文档正文与最终展示拼装。

## 4. 知识库设计

### 4.1 存储结构（一个 GraphKB collection = 两个向量 collection）

**表 1 `kb_{id}_chunks`（案例文档）**——一个历史问题一条记录：

| 字段 | 类型 | 内容 |
|---|---|---|
| doc_id | string | 历史问题唯一 id |
| text | string | 完整叙述：表象描述+排查过程+根因+方案（向量召回主体） |
| vector | float32[] | text 的 embedding |
| metadata.ticket_id | string | 工单系统 id（回关联） |
| metadata.component | string | 归属组件（检索过滤） |
| metadata.hit_count | int | 被召回/确认次数（业务加权与展示） |
| metadata.settled_at | int64 | 沉淀时间 |

**表 2 `kb_{id}_triples`（三元组）**——案例产出多条记录：

| 字段 | 类型 | 内容 |
|---|---|---|
| triple_id | string | 三元组唯一 id |
| text(caption) | string | "主语 -谓语-> 宾语"叙述句（embedding，实体语义匹配用） |
| vector | float32[] | caption embedding（辅助：长尾合并找近邻类别；主链路以 metadata 过滤为主） |
| metadata.subject / predicate / object | string | 实体与谓词 |
| metadata.subject_type / object_type | string | 枚举：表象 / 根因类别 / 组件 / 方案 |
| metadata.case_doc_id | string | 指回表 1 案例 |
| metadata.hit_count | int | 该三元组命中次数 |

**谓词收口（3 个，校验器限定非法组合写不进）**：

| predicate | 合法组合 | 用途 |
|---|---|---|
| 归类 | 表象 → 根因类别 | 案例归因 |
| 方案 | 根因类别 → 方案 | 该类根因怎么修 |
| 影响 | 根因类别 → 组件 | 根因↔组件关系 |

组件归属不进三元组（表 1 `metadata.component` 承担）。实体不单独建表；"分类树"是三元组按表象相似度聚合的查询侧视图。

### 4.2 检索流程（复用 GraphRetriever，不自建管线）

混合检索 + 三元组递归即 `GraphRetriever` 的原生流程（chunk 检索 + beam search 图扩展消费 `kb_{id}_triples` + RRF k=60 融合；HybridRetriever 为向量+稀疏 alpha 权重路由，融合在 store 层）。

实际例子（表象"收不到消息"，历史两个根因方向）：

1. IntentNode：抽特征 `{表象:"收不到消息", 组件:"推送服务"}`；
2. 混合检索：原始完整语句查表 1（向量+BM25），reranker 阈值剔除低分，召回案例 A（下游限流）、B（证书过期）；
3. 三元组两跳：`case_doc_id ∈ {A,B}` → 第一跳得根因分布 {下游限流, 证书过期}；第二跳 `predicate=影响` 验证两方向均吻合组件 → 双向并存 → 触发引导；
4. LLM 一次调用：候选聚类 + 从各根因区分性特征生成判别问题文案；
5. 用户补充（如"偶发，仅高峰期"）→ 特征并入 → 重新检索 → 单向 → 返回方案 + ticket 关联 + 确认；
6. 关闭 → SettleNode（命中案例 `hit_count+1`）。

### 4.3 沉淀与判重（按处理路径分流）

| 处理路径 | 关闭时落库动作 | 判重 |
|---|---|---|
| 历史库命中 | 命中案例 `hit_count+1`，增量并入 text | 无需（doc_id 已记录） |
| 业务库命中 | 写历史库新文档；业务库不动 | 轻判重（一次向量相似度查询） |
| AgentNode 处理 | 写历史库全量新文档（含排查链路、附件摘要） | 轻判重（同上） |

轻判重：新文档表象向量与历史库比对，超阈值且根因同向 → 并入既有案例 +1；否则 `AddDocuments`（含三元组抽取）。

### 4.4 长尾合并（防类别爆炸）

- 沉淀时 LLM 拿新根因类别先在既有类别集合里语义匹配（复用类别 caption embedding 近邻），命中则复用既有实体节点；
- 新建节点规则：目标类别 `hit_count < 3` 且存在 embedding 相似度 > 阈值（如 0.85）的近邻类别 → 指向近邻；`hit_count ≥ 3` 允许自立；
- 效果：碎片类别在成长早期被吸附进主干，类别集合收敛；规则在每次沉淀时生效，无需独立合并任务。

## 5. 引导机制

- **判定规则化**：候选根因类别数 ≥2 触发判别式引导；Intent 抽不出关键特征（表象/组件全模糊）触发开放式澄清（不查库）；轮数上限、超限路由均为规则，不进 LLM。
- **生成 LLM 化**：仅当规则判定要引导时一次 LLM 调用（候选聚类 + 判别问题文案）。选项文案来自历史根因分布 + 区分性特征 + 命中次数（如"历史上有 N 次类似问题归在'推送延迟'"）。
- **自迭代**：引导质量随"同表象多根因"案例增长自动提升，机制零改动；冷启动期历史少直接给答案或跳级，不会空转。

## 6. IntentNode 与附件处理

1. 附件预处理：图片附件 → 视觉模型（多模态 LLM）抽取现象特征（错误码、界面异常、堆栈）；日志附件 → 文本抽取（错误关键字、时间戳、组件线索）。均并入 intent 特征。
2. 检索查询文本 = 用户描述 + 附件抽取结果合成（图片先转文字再入查询，多模态与文本链路统一）。
3. 视觉模型走配置；无多模态模型时降级为只用文本并 Warn 日志。
4. IntentNode 的 LLM 抽取不为第一次主检索服务（主检索用原始完整语句），而为：组件过滤、引导轮特征并入、沉淀结构化。第一版可砍为"仅触发引导后才抽取"（冷路径省一次调用）。

## 7. AgentNode（诊断 Agent）装配

- 构造诊断 DeepAgent，走与普通会话相同的 DeepAdapter/会话管线，差异仅在装配参数：
  - **skills**：用户预置诊断 skill 置于专用目录，`skill_mode=auto_list` 注入（SkillUseRail 现成）；
  - **rails**：SkillEvolutionRail（`enabled=true` + `auto_save=true`）+ 既有基础 rails；
  - **提示词**：诊断场景模板（逐字管理）+ 上下文注入（intent 特征序列化文本、trail、附件引用）；
  - **工具**：skill 触发、日志/附件读取（复用仓内附件通道）。
- SettleNode 关闭时注入演进信号（`handleEvolutionFromSignals`），skill 经验自动落盘（SkillCreateRail 护栏覆盖新 skill 创建场景）。
- 所有节点的 LLM 调用走既有模型配置体系（user config / model routing），节点 prompt 模板独立文件；可按节点配不同档位模型（抽取用便宜模型、agent 用主力模型）。

## 8. 业务知识库导入面板（第二阶段，本 spec 仅定契约）

- 参考 WeKnora 模式：文档上传（PDF/Word/MD/网页）→ 解析分块（复用 13.x chunker 系列）→ 三元组抽取 → 写入业务 GraphKB collection；collection 管理、导入进度/失败重试、文档删除。
- workflow 只依赖导入契约（GraphKB `AddDocuments` 写入面 + collection 管理接口），面板独立 spec 实施。

## 9. 测试策略

1. **单测**：真实 KB 会话管线验证（真实链纪律——禁窄接口/替身承载能力面；函数参数收具体类型）；GraphRetriever/判重规则/长尾合并/状态转移表逐一覆盖，目标 ≥85%。
2. **集成测试**：覆盖"建会话→引导→确认→关闭落库→演进信号"全链，真实跨 KB/agent/store ≥3 子系统（满足集成审计两层门禁）；`biz_event` 三态与轮数超限分支必测。
3. **mock 只在边界**：HTTP 端点、LLM 端点；不得 mock 被测子系统本身。

## 10. 已知偏差与遗留

- 无对齐上游场景（本功能为平台自有业务编排流），不存在 Python 对照问题。
- 遗留：业务库导入面板第二阶段；trail 字段第一版可缓（YAGNI）；定期类别合并任务不做（沉淀时规则已覆盖）。
