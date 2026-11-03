## UapClaw v0.3.0

自 v0.2.0 以来的 98 个提交，两个功能主题：**模型 token 上限可配置（并驱动上下文窗口与压缩水位）**、**Web 端渲染思考输出**；其余为取消/打断语义收口、上下文检查点恢复修复与一批 Python 对齐修正。

> ⚠️ **升级注意（端口环境变量改名）**
>
> 监听地址/端口的读取方式对齐 Python 的三段式（命令行 > 环境变量 > 默认），变量名统一加 `UAPCLAW_` 前缀：
>
> - Web 通道：`UAPCLAW_WEB_HOST` / `UAPCLAW_WEB_PORT`（默认 `127.0.0.1:19000`），`--host` / `--port` 写入这两个
> - GatewayServer：`UAPCLAW_GATEWAY_HOST` / `UAPCLAW_GATEWAY_PORT`（默认 `127.0.0.1:19001`）
> - AgentServer：`UAPCLAW_AGENT_SERVER_HOST` / `UAPCLAW_AGENT_SERVER_PORT`（默认 `18092`）
>
> 旧变量名（`WEB_PORT` / `AGENT_SERVER_PORT` 等）**不再读取**，也不再回退到 `config.yaml` 的 `gateway` 段。自定义过端口的部署请改用新变量名，或直接用 `--port`。

### 一、模型 token 上限可配置

`config.yaml` 的 `models.defaults` 新增 `max_input_tokens` / `max_output_tokens`（0 表示不限制），并贯通到三个位置：

- **请求侧**：`max_output_tokens` 落成 `ModelRequestConfig.MaxTokens`，并加入模型配置白名单
- **上下文侧**：`max_input_tokens` 灌入 `ContextEngineConfig` 作为窗口上限（未配置时恢复到 200/10 的默认值）
- **压缩侧**：新增压缩水位比例 `compression_trigger_ratio: 0.85` / `compression_target_ratio: 0.55`，实际阈值取 `min(绝对阈值, ratio × 输入上限)` —— 只下调、不放大

Web 端「模型配置」页可输入并回显这两个值，`models.list` / `models.replace_all` 同步解析，非法值写入被拦截。

### 二、Web 端渲染思考输出

模型返回的 `reasoning_content` 现在会在 Web 界面渲染为思考块，行为对齐 Python TUI；规划模式等无思考输出的场景不受影响。

### 三、取消与打断语义收口

- 用户取消不再被上报为错误帧，且清理逻辑只执行一次
- 流式任务收尾时只释放自身登记，不再误取消下一轮（supplement 打断后的新一轮可正常继续）
- 流已结束但已被取消时返回 error，避免半截回答被当成最终答复
- 各通道的取消错误帧统一收口：不写流、不落 history

### 四、上下文检查点恢复修复

- **进程/服务重启后上下文可恢复**：`SessionModelContext.LoadState` 原实现对 `messages` 等字段用具体类型断言，检查点 JSON 反序列化后一律是 `[]any`，断言落空且静默跳过，导致重启后上下文被清空。现按 `role` 还原为具体消息类型，`tool_calls` / `tool_call_id` 成对保留
- `FileKVStore` 的 pipeline `get` 与 `Get` 语义不一致（前者返回 base64 包装的 JSON），已统一为解包；底层条目损坏时返回错误而非静默 `nil`
- 新增「服务重启后从 SQLite 检查点恢复上下文」的集成用例，装配与线上 `EnsurePersistentCheckpointer` 同构

### 五、工具执行与结果渲染

- 工具执行 panic 转为 error 并仍产出 `tool_result`（此前前端工具卡会永久停在「执行中」）
- `RegisterRail` 补 `DeepAgentRail` 依赖注入；热重载配置补齐 `workspace` / `sys_operation` 等字段
- `tool_result` 的 `result` 按 Python `str()` 语义渲染，记忆工具结果的键名与空结果形状对齐 Python

### 六、context_evolver 对齐 Python

`AddMemory` 的 `section` 缺省补 `general`、手工记忆复用 `ReMeMemory/ACEMemory.ToVectorNode`、`LoadMemories` 内部吞异常不阻断构造、构造签名与 `AutoConfigure` 对齐、`matts_k` 默认值统一由 `RunTrials` 读取、`Execute` 显式建会话并从上下文提取完整轨迹。

### 七、其它

- `graph`：`relationDedupe` 并发调用 LLM 并按索引保序；`ReadableSchema` 改为反射生成，修正字段顺序与类型名
- `utils`：`PyStr` / `PyFloatStr` 收口重复的 Python 格式化助手；`Canceled` 只认 `context.Canceled`
- `llm/openai`：`WrapError` 保留 cause，错误链可判定
