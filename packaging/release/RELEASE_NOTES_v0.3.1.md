## UapClaw v0.3.1

自 v0.3.0 以来的 7 个提交，无破坏性改动，可直接覆盖升级。主要内容：**技能开发流程的五个阶段由占位数据改为模型真实执行**（内部实现补完），以及 **2026-11-03 48 小时审查的 19 项问题与静态分析告警处置**。

### 一、技能开发流程：五阶段接入阶段 Agent（内部实现补完）

此前 `PLAN` / `GENERATE` / `EVALUATE` / `IMPROVE` / `DESC_OPTIMIZE` 五个阶段在上游只返回占位数据，技能生成链路实际跑不通。本版按各阶段注释的语义补齐，使「生成 → 校验 → 评测 → 审阅 → 多轮改进 → 描述优化」真正跑在模型上：

- **PLAN**：拼装需求、参考资料与既有 SKILL.md → 模型产出 plan JSON（容错解析）+ 差量分析
- **GENERATE**：单 Agent 复用，逐文件生成 → 剥离代码围栏 → SKILL.md 缺 frontmatter 时按 plan 补齐，避免 VALIDATE 反复回退打满迭代上限
- **EVALUATE**：Grader Agent 按用例评分（JSON 解析失败退回确定性兜底），Analyst Agent 产出观察；无真实运行记录时给出明确提示
- **IMPROVE**：填充 ImproveSystemPrompt → 改写 SKILL.md，保证 frontmatter 存续
- **DESC_OPTIMIZE**：触发查询生成 / 触发判定 / 描述改进三处接模型，同分时保留更靠后的迭代
- `CreateStageAgent` 补齐两处关键装配（否则模型收不到任何输入），`RegisterTools` 按工具名白名单注册

### 二、稳定性与正确性修复

- **出厂模板不再 panic**：`setFieldValue` 遇 nil 值跳过，`messages_threshold: null` 不会触发空指针
- **并发 reload 竞态**：`DeepAdapter` 增加 `stateMu` / `sessionMu` 与快照助手，模型/配置/活跃会话状态改为「构建完再发布」
- **模型预设合并**：收全 5 类默认配置（yaml 覆盖必命中类型），合并后按 `session_memory` 过滤激活集（默认 4 / 开启 3）；合并失败降级为告警并跳过
- **流式任务收尾**：先按身份释放槽位，再无条件 cancel 本次子 ctx，避免误伤其它轮次
- **成员单轮超时**：改读 `DeepAgentConfig.CompletionTimeout`（缺省 600s）
- **上下文统计**：统一使用 `InputTokens`（为 0 时回退 `TotalTokens`）
- **会话记忆配置**：新增三态解析（两键 / True / dict / 关闭），`update_mode` 默认对齐 `agent_edit`
- **提示词语言**：改读顶级 `preferred_language`（原先读 `react` 段，导致恒回落默认语言，与 Python `get_config()` 不一致）

### 三、界面与日志

- 前端推理块 / 文件条目的 id 改用「局部自增序号」，避免同一毫秒内时间戳撞车导致条目被去重丢弃
- 日志默认输出无色；`FORCE_COLOR` 显式开启颜色，`NO_COLOR` 可否决

### 四、其它

- 技能目录解析删除死分支；技能创建开关改读 `environment.evolution.skill_create`（默认 `false`）
- 模型配置兜底优先读 `configBase` 快照，未初始化时才读盘
- 静态分析：清理 SA5011 / SA1012 告警；`Canceled` 恢复 nil 守卫并写明语义边界
- 集成覆盖率豁免登记 recap 模型调用路径（走真实 HTTP，由 real-llm workflow 覆盖）
