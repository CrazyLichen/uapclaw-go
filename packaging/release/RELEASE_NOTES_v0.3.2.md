## UapClaw v0.3.2

自 v0.3.1 以来的 114 个提交，无破坏性改动，可直接覆盖升级。本版主线是**把「看起来有、实际不生效」的几处链路修通**：记忆索引能从中文内容里检索到、规划模式能真正建 todo 并显示在右侧任务栏、技能演进 rail 在冷启动/热重载/slash 三条路径上都能正确装配、热重载后老会话跟随新模型。另附消息队列 Pulsar 客户端基础设施（默认未启用）。

### 一、记忆检索：从「有索引但查不到」到可用

- 引入 `gse` 中文分词，索引侧写入分词文本、`chunks` 保留原文，索引与查询共用同一分词入口
- 关键词检索改 **AND** 连接并截断前 5 个 token，命中不足时降级回 OR
- 索引初始化改用**调用方 ctx**——此前用后台 ctx 被沙箱拒绝，导致索引永远为空
- `vec0` 维度不匹配时按 `sqlite_master` 里的真实维度重建；重建判定改按**实际 provider 实例**，统一为 `needsReindex`
- 元数据记录实际 provider 与分词器标识；`memory.status` 上报真实 embedding provider，不再掩盖 mock 降级
- Web 端 `memory.compute` 由写死桩换成真实采集（含 `used_percent`），修复界面内存占用恒为 0
- `/tui` 路由注册 `memory.*` 五个本地 handler（status/list/edit/toggle/open），参数按 workspace/mode/project_dir 三级回退

### 二、规划模式与 todo 任务栏

- 规划模式**放行 todo 工具**（方案 A），修复 todo 创建路径整条断链
- Web 切换模式时同步置位 plan 状态，规划模式不再「选了但没生效」
- `todo.updated` 兼容 `[]map` 形态，修复右侧任务栏始终显示「暂无任务」
- factory 不再重复添加 `TaskPlanningRail`，消除性能模式下的 todo 提示残留

### 三、技能演进 rail 装配

- 冷启动不再构建演进/创建 rail，恢复字段注册语义
- 热重载**只重建 skillRail**并复用其它有状态 rail；动态构建路径补登记字段不变量；slash 懒加载后立即注册
- 演进 rail 构造参数对齐 Python（`auto_scan`/`auto_save`/`sharing`/`model_name`）
- `skill_create` 补 `enabled` 门控并切换到单 Agent `SkillCreateRail`（阈值检测 → follow_up 提议）

### 四、模型与上下文

- 热重载后**已存在的会话**跟随新模型与新阈值（此前只影响新会话）
- 默认模型按 `models.defaults` 顺序选择，不再依赖 map 随机序
- `invoke` 入口 identity 回写改用 `config.PromptTemplate` 渲染，修复提示词逐轮自我嵌套
- 上下文占用分子改用 `total_tokens`（输入+输出），与界面口径一致
- 统一压缩触发日志口径，修复 `duration_ms` 负溢出
- 出厂模板补 `tool_result_budget` 段，并锁定模板到 rail 的接线

### 五、工具与运行时

- **bash / powershell 未传 timeout 时回落 300 秒**，修复命令 1 秒即超时
- Windows 输出按系统编码解码，修复 GBK 乱码；同时修掉 `Invoke` 输出丢失竞态
- LLM 超时配置双形态生效，流中断不再被静默吞掉
- 工具结果展示统一走 `PyText`，修复失败工具的 `map[...]` 泄漏到展示文本
- `code_agent` 迭代上限按配置回退；code 模式接入 browser 子代理
- agents 写操作同步重载配置，`AgentManager` 重载错误立即返回
- `llm_usage` 累加改读嵌套 `usage_metadata`，并补跨会话隔离用例

### 六、消息队列（基础设施，默认未启用）

- `MessageQueuePulsar`：结构体与 Start/Stop、订阅生命周期、producer LRU 缓存与淘汰、Subscribe/Unsubscribe 错误码与复用语义
- Dmq 消息类型与序列化：`MessageToMap` / `SerializeMessage` / `DeserializeMessage`，`QueueMessage` 补 json tag 对齐 Python 字段名
- **注意**：尚未接入运行时 provider 选择，`message_queue.type` 维持原有行为，默认配置不受影响

### 七、工程与 CI

- Lint 作业固定 Go 工具链版本，避免 staticcheck 误报风暴；超时放宽到 10m 以覆盖冷缓存
- 集成覆盖率豁免登记若干纯字段/纯函数文件（`manager.go`、`vec_table.go`、tests 夹具等）
- 补齐记忆检索、上下文窗口统计、消息队列、todo 全链路等测试

### 升级提示

- 无配置项改名或删除；旧 `config.yaml` 可直接使用
- 新增出厂模板段 `react.context_engine_config.tool_result_budget_processor_config`（工具结果预算，仅 `session_memory` 路径生效），已有配置不会被自动改写
- 记忆库首次启动可能触发一次重建（维度/分词器变更），旧数据不会删除
- 若之前观察到「右侧任务栏一直为空」「内存占用恒为 0」「bash 1 秒超时」，本版即为对应修复
