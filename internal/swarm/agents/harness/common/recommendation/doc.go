// Package recommendation 承载主动推荐能力（提示词建议 + 主动引擎）。
//
// 本包对应上游 jiuwenswarm/agents/harness/common/recommendation/ 的
// prompt_suggestion.py、switches.py、proactive_*.py、situation_report.py、
// calendar_source.py、profile_extractor.py、gradient_updater.py、
// feedback_collector.py，以及 jiuwenswarm/common/recommendation_switches.py。
// 完整的 topic 流水线（35 文件 / 43821 行）不在本包，由 10.3.31b~d 单独排期；
// 存储地基（TopicStarterStore 事件账本）已随 10.7.1 交付于 topic_starter_store.go；
// 10.7.2 已接通建议链路闭环：_fork 的结果落库（recordLedgerEvent）、ready 推送
// （窄接口 SuggestionReadyPusher，由 internal/swarm/server/handle_recommendation.go
// 装配注入）、页面回执（RecordPageOutcome）与回读（CurrentSuggestions）。
//
// 两个子系统：
//   - 提示词建议（10.3.30 已交付）：switches.go / prompt_suggestion.go /
//     suggestion_filter.go / fork_rail.go —— 每个成功轮结束后预测下一条用户输入。
//   - 主动引擎（10.3.31a **全量已交付**）：profile_extractor.go /
//     proactive_prompts.go / proactive_actions.go / situation_report.go /
//     calendar_source.go / gradient_updater.go / feedback_collector.go /
//     proactive_engine.go / proactive_adapter.go ——
//     定时 tick 聚合情境 → 单轮 LLM 决策 → 让主 agent 生成推荐话术。
//     两个 RPC（proactive.tick / proactive.feedback）在 internal/swarm/server/handle_proactive.go。
//
// 内置技能源目录不在本包，而在发布包资源
// resources/agent/workspace/skills/（10.3.31a-pre 原封拷贝上游 34 目录 /
// 415 文件），由 internal/swarm/server/runtime/skill 的 getBuiltinSkillsDir 解析。
//
// 文件目录：
//
//	recommendation/
//	├── doc.go                          # 包文档
//	├── switches.go                     # 推荐开关与同意 epoch
//	├── consent_state.go                # consent 同意状态只读探测（topic_controls.py 只读切片，写入 ⤵️ 10.3.31d）
//	├── prompt_suggestion.go            # MainCall / PromptSuggestionForks / _fork
//	├── suggestion_filter.go            # 建议过滤
//	├── fork_rail.go                    # PromptSuggestionForkRail
//	├── profile_extractor.go            # RecommendationState 持久化（recommendation.json）
//	├── proactive_prompts.go            # 决策/话术 prompt 模板（zh/en，逐字复刻上游）
//	├── proactive_actions.go            # 决策类型/限流/技能发现/LLM 分析/触发主 agent
//	├── situation_report.go             # 情境报告（多会话扫描/历史压缩/渲染）
//	├── calendar_source.go              # MCP 日历事件拉取与归一化
//	├── gradient_updater.go             # 策略梯度更新（显式/隐式反馈 → target/relation/tone/structure）
//	├── feedback_collector.go           # 反馈采集（buffer / 显隐式去重 / 关联最近推荐）
//	├── proactive_engine.go             # 引擎主体（tick 全流程 / 每日限流 / 生命周期）
//	├── proactive_adapter.go            # 主 agent 触发（chunk 打标 / 送达判定 / 引擎装配）
//	├── topic_starter_store.go          # topic 事件账本（InterestDrift + TopicStarterStore，2 表 4 索引）
//	├── topic_url_safety.go             # SSRF 公网校验（topic_url_safety.py，10.3.31c）
//	├── topic_model.go                  # Ark JSON 后台模型适配器 fail-closed（topic_model.py，10.3.31c）
//	├── history_state_store.go          # topic 历史状态快照接口 ⤴️ 10.3.31d 过渡（topic_history.py 4 函数切片）
//	├── topic_audit.go                  # 外部调用审计计数（topic_audit.py，10.3.31c）
//	├── topic_sources.go                # 内置语料包 + 现场检索准入（topic_sources.py，10.3.31c）
//	├── topic_search.go                 # native/Ark 双搜索适配器（topic_search.py，10.3.31c）
//	├── topic_news.go                   # Bing News RSS + 热搜榜（topic_news.py，10.3.31c）
//	├── topic_page_fetch.go             # 网页正文与发布时间抓取（topic_page_fetch.py，10.3.31c）
//	├── topic_terminal.go               # 终局组件契约 + RID API 蒸馏（topic_terminal.py，10.3.31c）
//	├── topic_rid.go                    # RID MATCH/NO_MATCH 配对裁判（topic_rid.py，10.3.31c）
//	├── topic_feedback.go               # 上下文反馈投影（topic_feedback.py，10.3.31c）
//	├── topic_outcomes.go               # 曝光/点击 outcome 记账（topic_outcomes.py，10.3.31c）
//	├── topic_pipeline_d1.go           # TopicPipeline 类骨架（topic_pipeline.py:4481-4622，10.3.31b）
//	├── topic_pipeline_d2.go           # scope_input_revision + refresh_short_intent（:4623-4906）
//	├── topic_pipeline_d5.go           # 摘要链 8 方法 + RefreshState（:5396-5942）
//	├── topic_pipeline_d6.go           # RID 提炼/记忆/复用 + 热榜新闻（:5943-6343）
//	├── topic_pipeline_d7.go           # 曝光问题/全局候选/retained 族/双校验（:6345-7097）
//	├── topic_pipeline_d8.go           # 项目候选/续聊校验/候选证据（:7098-7712）
//	├── topic_pipeline_d9.go           # 冻结文档/近期终稿/接地（:7714-8560）
//	├── topic_pipeline_d10.go          # 效用/多样性/评分选择/latest manifest（:8562-8870）
//	├── topic_pipeline_d11.go          # 池装配 AssembleResult（:8872-9167）
//	├── topic_pipeline_d12.go          # 三个生成入口（:9169-9704）
//	├── topic_pipeline_d13.go          # Generate 主入口 + RefreshNews（:4908-5393）
//	├── topic_product_validate1.go     # 组件/宿主小票校验族 v1（topic_product.py:3392-3790，⤴️31c 归位）
//	├── topic_product_validate2.go     # _valid_card 全链/池组成/打开判定（:3843-5603，⤴️31c 归位）
//	├── topic_product_validate3a.go    # news slot/组件与厂商血缘/模型绑定（:2777-3240，⤴️31c 归位）
//	├── topic_product_validate3b.go    # 列表搜索/manifest 主链/池读取链（:2427-6502，⤴️31c 归位）
//	└── topic_product.go               # 池组成契约/终局卡/小票校验/曝光选卡（topic_product.py，10.3.31c）
//
// 10.3.31c「topic 产物与呈现」切片：12 文件 / 13811 行（wc -l 实测，原登记 9 文件
// / 12889 口径过时）。依赖：⤴️ 10.3.31b（topic_selection / topic_retrieval /
// topic_pool / topic_starter 符号）；⤴️ 10.3.31d（topic_history 4 函数 →
// history_state_store.go 过渡接口，31d 落地后改真实类型并删 fake）。
//
// topic_starter_service.go（TopicStarterService + 画像证据收集，上游 topic_starter.py:515-1953）
// ⤵️ 10.3.31d；本节只交付存储层。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/
//
//	jiuwenswarm/common/recommendation_switches.py
package recommendation
