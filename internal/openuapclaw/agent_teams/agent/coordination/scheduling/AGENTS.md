# Scheduling — 调度模式决策引擎（F_62）

> 改编自上游 `agent_teams/agent/scheduling/AGENTS.md`。Go 侧落位 `agent/coordination/scheduling/`
> （Python 是与 coordination 平齐的 `agent/scheduling/`，Go 因 dispatcher 装配就近放置，D-25.2 已登记）。
> 结构见 `doc.go`。

leader 侧的调度分发 runtime，与 coordination 平齐：coordination 只唤醒、不决策；本包只决策、不直接
触碰其他成员的 round——所有成员交接都是 **leader 身份的邮箱消息**。仅在 `spec.dispatch_mode ==
"scheduled"`（静态配置）的 leader kernel 上构造，团队建成后激活。

## 文件地图

| Go 文件 | 职责 |
|---|---|
| `scheduler.go` | `TeamScheduler`：事件粗筛 + 五个幂等扫描（开工/交接补发/验票/退场/修改推送）+ `SchedulerHost` 窄协议 + 四窄接口（SchedulerHost/TaskManager/Backend/LeaderMessageSender） |
| `scheduler_review.go` | review 半区：`reconcileReviews` 派发去重/judge-pass+fail 结算/超限升级+rework_summary 一次/停滞升级/followUpUnvotedReviewer（上游 scheduler.py L527-1085 一比一） |
| `verdict.go` | 纯函数投票判定 `SettleReviewTally`（二元票池一票否决 + 检视者分数池平均≥0.85）——策略可整体替换，不碰票据存储 |
| `reviewer_spec.go` | `BuildReviewerAgentSpec`：成员 spec 收窄为临时 reviewer——框架级裁剪恒生效，宿主可再传 rail/内置工具白名单 |
| `reviewer_runner.go` | `BuildTempReviewerRunner`：临时 reviewer 生产运行体（reviewer-scoped TaskManager/verify_task_scheduled 工具/按类型模板/181001 三重试/用后即弃 Dispose） |
| `render.go` | 成员交接只组投递载荷 `meta_*`（content 空，文案在 prompts/、投递时渲染，F_63）；leader 摘要/升级走 deliver_input 直投（i18n `scheduler.leader_*`）；终态摘要带完成摘要 |
| `review_feedback_evolution.go` | `ReviewFeedbackEvolutionCoordinator`：逐 Task Feedback 归因、终态汇总演进与新 Skill 候选路由（F_73） |
| `adapter.go` | 四窄接口的实现粘合 |

## 核心不变量（十三条，逐条对齐上游）

1. **调度器不理解事件，只理解看板。** 任何触发都跑同一组五个幂等扫描；恢复=激活扫描本身。
2. **交接 = leader 身份邮箱消息，投递即启动**（auto_start_member 复用 CAS，幂等）。成员在线与否不是
   开工前置条件。
3. **发送存意图，投递才成文（F_63）。** 邮箱行 content 恒空，meta 带模板 key+refs；投递时刻按收件人
   语言渲染，`{{task.*}}` 取**当时**的任务行。**不要**把任务正文快照进消息。
4. **`SchedulerHost.DeliverInput` 只准注入 leader 自身**，对成员永不直投。
5. **判定在本包，票据在 DB。** settle 经 `task_manager.SettleReview` 的 IN_REVIEW 源态 CAS——单判定者
   + CAS 保证不双结算。
6. **轮数升级与停摆升级共用一条注入路径**，按 (task_id, review_round) 去重。
7. **leader 自发事件可见性靠 `SCHEDULER_SCAN` 回声**。
8. **异常语义与 coordination 对齐**：on_event 吞普通异常，绝不让 bus loop 挂掉。
9. **review feedback 是可选旁路**（F_73）：从 host harness 查找已挂载且开启的 TeamSkillEvolutionRail，
   后台调用其窄入口，不阻塞返工；scheduler 不理解 LLM/轨迹/Skill 归因。
10. **退场由调度器请求、成员自落**（F_118）：对 lifetime 非空且 READY 的成员在"名下无非终态任务+无
    未读消息"时调普通 shutdown_member；不直写 DORMANT、不拆 handle。
11. **交接以邮箱为真相，改派不漏人**（B_06）：`reconcileHandoffs` 对每个活跃任务核对当前 owner 邮箱
    是否有开工交接行，没有就补发；幂等来自邮箱+进程内 `_handoff_known` 记账。
12. **修改以版本差为真相**（F_129）：`reconcileRevisions` 读 `unseen_revisions()` 窄投影；投递渲染即
    推进 observed_revision；调度器不消费 TASK_UPDATED 本身。
13. **已退役负责人的任务不开工、不补发**（B_10）：每次扫描前取 `list_retired_members`；按策略退场的
    负责人名下搁浅任务向 leader 升级一次。

## 生命周期（kernel 接线）

- kernel.setup 仅在 scheduled leader 上构造（休眠态）。
- 激活点两个：`NotifyTeamBuilt()`（build_team 成功回调）与 `kernel.start()`（team 行已存在）。
- wake 路径：kernel 组合 "coordination dispatch → scheduler.on_event"。
- pause/stop 即 deactivate；内存记账不持久化——重启后最坏重发一次送审或升级，board 真相在 DB。

## 配置消费

`default_max_review_rounds`（默认 3）、`review_stall_timeout`（默认 1800s）只在本包消费；催办间隔
600s 是包内常量。

设计上下文：spec `docs/superpowers/specs/2026-12-02-25.2-scheduled-dispatch-design.md`；
25.2a/25.2b 交付注记见 IMPLEMENTATION_PLAN 领域二十五。
