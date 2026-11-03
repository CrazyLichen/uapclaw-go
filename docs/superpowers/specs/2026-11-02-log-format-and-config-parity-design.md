# 日志格式改造与 config.yaml 对齐 Python 设计

**日期**：2026-11-02
**状态**：待实施
**涉及包**：`internal/common/logger`、`internal/common/config`、`internal/swarm/server/adapter`、`resources/config.yaml`

---

## 1. 背景

当前日志系统（`internal/common/logger`，zerolog v1.35.1）输出两类形态：

| 输出目标 | 现状 |
|---|---|
| 控制台（stderr） | `10:31:20AM INF some message component=common key=val` |
| 组件文件 / full.log | `{"level":"info","component":"common","time":1762000000000,"message":"...","key":"val"}` |

存在三个问题：

1. **缺少调用点定位**：zerolog 默认不采集 caller，4987 个调用点（`grep -rn "logger\.\(Info\|Warn\|Error\|Debug\|Fatal\)(" --include=*.go` 实测值）出问题只能靠消息内容猜位置。
2. **日期粒度不足**：文件里是 Unix 毫秒数（人眼不可读），控制台只显示 `HH:MM`，两者都不是常规可读形式。
3. **`logging.format` 是无效果配置**：`resources/config.yaml:18` 写了 `format: "json"`，但全仓无任何代码读取（`sections.go:41` 仅定义字段），属误导性死键。

此外 `resources/config.yaml` 只有 31 行、3 个顶层段（`server` / `logging` / `workspace`），而 Python 参照实现 `jiuwenswarm/resources/config.yaml` 有 917 行、23 个顶层段。`make release` 直接 `cp -r resources` 打进分发包（Makefile:75-76），因此用户拿到的默认配置是骨架，配置能力藏在代码里无从发现。

本设计同时解决上述两部分：日志输出形态改造 + 默认配置对齐。

---

## 2. 范围

| 做 | 不做 |
|---|---|
| 改控制台与全部日志文件的输出格式 | 不动 4987 个调用点的写法 |
| 新增 `[文件:行号]`（`.Caller()`） | 不引入日志采集/上报/JSON 双写 |
| 删除 `logging.format` 死键及其测试引用 | 不改日志级别语义、轮转策略、脱敏规则 |
| 重写 `resources/config.yaml` 到与 Go 实现匹配 | 不为未实现段写假配置 |
| 修 `resolveEnableTaskLoop` 漏接 `skill_create` 的缺陷 | 不改各配置段的代码默认值 |

---

## 3. 目标输出形态

控制台与所有日志文件使用同一格式（默认无色；设 `FORCE_COLOR=1` 且未设 `NO_COLOR` 时控制台带级别着色）：

```
[2026-11-02 10:31:20.123] [INFO] [common] [config/loader.go:88] config loaded path=/home/u/.uapclaw/config/config.yaml
[2026-11-02 10:31:20.456] [WARN] [gateway] [gateway/app_gateway.go:221] heartbeat timed out error="read tcp timeout" target=web
[2026-11-02 10:31:21.007] [ERROR] [channel] [web/web_connect.go:536] connection closed session_id=abc123
```

（消息正文保持英文——项目约定 `logger.Msg` 不中文化，仅代码注释用中文。）

分栏规则：

| 栏位 | 规则 |
|---|---|
| 时间 | 本地时区，`2006-01-02 15:04:05.000`（毫秒），方括号包裹 |
| 级别 | 大写全称，方括号包裹：`DEBUG` / `INFO` / `WARN` / `ERROR` / `FATAL` |
| component | `Component.String()` 结果，方括号包裹 |
| 文件:行号 | **末两段路径** + 行号，方括号包裹；路径段不足时降级为原值 |
| 描述 | 消息正文，单行（`\n` `\r` `\t` 转义为字面量） |
| 参数 | logfmt：`key=value` 空格分隔；字段按字母序，其中 `error` 被 `ConsoleWriter` 提前到首位（console.go:220-240）；含空格或特殊字符的值用双引号包裹 |

栏位之间保留**一个空格**（`][` 不紧贴）——这是复用 `zerolog.ConsoleWriter` 的必然结果，省掉约 80 行「自解析 JSON + 自行渲染」的格式化代码。

---

## 4. 日志格式实现

### 4.1 数据流

```
业务代码 logger.Info(comp).Str(...).Msg(...)
  └─ zerolog 编码 JSON（含 caller）
       └─ sanitizerWriter       ← 脱敏（在 JSON 上，规则与顺序完全不变）
            └─ textWriter       ← 新增：ConsoleWriter 定制，JSON → 目标文本行
                 └─ mutexWriter → lumberjack（组件文件 / full.log）
```

关键点：脱敏仍在 JSON 阶段完成。插入点位于 sanitizer **内侧**，因此现有脱敏规则、顺序、测试全部不受影响，也不引入漏脱敏风险。

### 4.2 `format.go` 函数契约（新增文件）

| 函数 | 职责 | 边界处理 |
|---|---|---|
| `newTextWriter(out io.Writer, noColor bool) zerolog.ConsoleWriter` | 组装定制项：`PartsOrder=[time,level,component,caller,message]`、`FieldsExclude=["component"]` | 两个调用方分别传 `os.Stderr,false`（控制台）与 `fileWriter,true`（文件） |
| `formatTimestamp(v any) string` | Unix 毫秒 → `[2026-11-02 10:31:20.123]` | 兼容 `json.Number` / `float64` / `int64`；字符串形态按 RFC3339 解析；**无法解析或缺失时返回空串**，由 `ConsoleWriter` 跳过该栏（不输出半截括号） |
| `formatLevel(v any) string` | `info` → `[INFO]`（显式 `FORCE_COLOR` 且无 `NO_COLOR` 时着色） | 未知级别原样大写；默认无色对齐 Python，避免文件/管道日志混入 ANSI（Setup 时读取一次） |
| `formatPartValue(v any, _ string) string` | `component` → `[common]` | 非字符串值走 `fmt.Sprint` |
| `formatCaller(v any) string` | `/abs/path/pkg/file.go:88` → `[pkg/file.go:88]` | 路径段 <2 时返回原文；空值返回 `""` |
| `formatMessage(v any) string` | 消息正文 | `\n`→`\\n`、`\r`→`\\r`、`\t`→`\\t`，保证一行一条 |

### 4.3 `logger.go` 改动（三处）

1. `createComponentLogger` 的 `With()` 链增加 `.Caller()`：`zerolog.New(...).Level(...).With().Timestamp().Caller().Str("component", comp.String()).Logger()`
2. 文件 writer 链插入 formatter：`NewSanitizerWriter(newTextWriter(NewMutexWriter(rot), true), l.sanitizer)`
3. 控制台 writer 换成 `newTextWriter(os.Stderr, false)`，同样被 sanitizer 包裹

`Permissions → agent_server.log` 的额外 writer 同样套用第 2 条。

### 4.4 已实测的关键事实

以下四点均在实施前通过临时探针测试验证，已从仓库删除：

1. **`.Caller()` 穿过包级包装函数后行号准确**，指向业务调用点（`logger.Info(comp)` 返回 event 后立即返回，`Msg()` 仍在业务帧执行，故 zerolog 默认 `CallerSkipFrameCount=3` 无需调整）。
2. **`FieldsExclude` 必需**：`component` 放入 `PartsOrder` 后仍会被 `writeFields` 再次输出（`component=common` 重复），必须显式排除。
3. **消息转义必需**：`ConsoleWriter` 的 `FormatMessage` 默认输出原始消息，`\n` 会真的换行，破坏「一行一条」；字段值不受影响（`needsQuote` 已把含控制字符的值加引号转义）。
4. **时间字段是 Unix 毫秒数**：`Setup` 里设了 `zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs`（logger.go:182），格式化函数必须做数值→本地时间转换。

### 4.5 行为变化清单

| # | 变化 | 影响 |
|---|---|---|
| 1 | 日志文件从 JSON 变行文本 | `jq` 类消费失效；已核实无任何生产代码读取日志文件（`full.log` 在全仓仅出现在 `sections.go:56` 的注释里，其余引用都在 logger 包内部的写入侧） |
| 2 | 消息内换行变字面 `\n` | 长错误堆栈不再跨行，需靠 `sed 's/\\n/\n/g'` 展开 |
| 3 | `Err(err)` 落成 `error=xxx` | zerolog 的 `ErrorFieldName` 是 `error` 而非 `err` |
| 4 | 空消息行不再有 `""` 占位 | 极少数 `.Msg("")` 调用会只剩前缀与参数 |
| 5 | 单行变长（前缀约 +40 字节） | 轮转体积略增，忽略 |
| 6 | caller 采集开销 | 仅在事件启用时发生（级别禁用时 zerolog 返回 nil event，`Msg` 直接返回），禁用路径零成本 |

### 4.6 死键删除

删除 `LoggingConfig.Format` 字段及全部引用：

- `internal/common/config/sections.go:40-41`
- `internal/common/config/testdata/config.yaml:12`、`testdata/template.yaml:12`
- `internal/common/config/sections_test.go:87-88`、`:128`、`:151-152`
- `internal/common/logger/logger_test.go:551`（yaml 字面量）
- `resources/config.yaml:18`（随第 5 节整体重写，新模板不再包含该键）

依据：Python 模板 `logging` 段只有 `level / console_level / gateway / channel / agent_server / full`，**本来就没有 `format` 键**（`jiuwenswarm/resources/config.yaml:8-14`）。删除即是向 Python 对齐。Go 多出的 `common / permissions / agent_core` 是 Go 独有的分流文件，保留。

---

## 5. `resources/config.yaml` 补全

### 5.1 段处置

| 类别 | 段 | 处置 |
|---|---|---|
| 已实现（18） | logging, memory, models, acp_agents, agents, react, embed, gateway, heartbeat, channels, permissions, browser, extensions, hooks, team, mcp, modes, preferred_language | 按 Python 结构写入，**键集合以 Go 实读为准** |
| Go 未实现（5） | telemetry, task_memory, tools, email_settings, updater | 不写键，文件末尾用注释登记段名并标注「当前版本未实现」 |
| 死段（2） | `server:`, `workspace:` | 删除 |

死段删除依据（已核实生产零调用）：`GetServerConfig()` 与 `GetWorkspaceConfig()` 仅有函数定义（sections.go:75、:153），无生产调用方；`server.agentserver` 与顶层 `workspace` 亦无原始 map 读取。`gateway.host` / `gateway.port` 是真实读取点（app_gateway.go:779、:797），必须放在**顶层 `gateway:`** 段（Python 模板的 gateway 段没有这两个键，直接照搬会退回 `127.0.0.1:19000`）。

### 5.2 出厂值裁决（10 项）

| # | 键 | 出厂值 | 依据 |
|---|---|---|---|
| 1 | `heartbeat.every` | `3600` | 对齐 Python 出厂（Python 代码默认也是 `"60"`，差异仅来自模板覆盖） |
| 2 | `heartbeat.active_hours` | `08:00`–`22:00` | 对齐 Python 出厂 |
| 3 | `react.max_iterations` | `100` | 对齐 Python 出厂（代码默认两侧同为 15） |
| 4 | `react.enable_task_loop` | `true` | 保持 Go 现状（用户裁决） |
| 5 | `react.subagents.general_agent.enabled` | `true` | 对齐 Python 出厂 |
| 6 | `modes.agent.fast.memory` | `enabled: true` / `is_proactive: false` | 对齐 Python 出厂 |
| 7 | `modes.agent.plan.memory` | `enabled: true` / `is_proactive: true` | 对齐 Python 出厂 |
| 8 | `channels.web.send_file_allowed` | `true` | 对齐 Python 出厂 |
| 9 | `team.team_name` | `uapclaw_team` | 自家品牌（Python 为旧品牌 `jiuwen_team`） |
| 10 | `memory.external.mem0.user_id` / `agent_id` | `uapclawswarm-user` / `uapclawswarm` | 自家品牌（Python 为 `jiuwenswarm-user` / `jiuwenswarm`） |

补充（无需裁决，对齐 Python 且为默认关闭/默认值）：`memory.mode: local`、`memory.engine: builtin`、`modes.team.memory.enabled: false`、`permissions.enabled: false`。

**LTM 目录无差异**：Python `_resolve_ltm_dir()` 用 `get_user_workspace_dir()/memory/ltm`（external_memory_config.py:80-84），支持 `JIUWENSWARM_DATA_DIR` 隔离；Go `ResolveLtmDir()` 用 `path.WorkspaceDir()/memory/ltm`（external_memory_config.go:157），支持 `UAPCLAW_DATA_DIR`。两者语义一致，仅品牌目录名不同（`.jiuwenswarm` → `.uapclaw`）。

### 5.3 注释规范

每个键三行信息：环境变量覆盖名、代码默认值、来源位置。

```yaml
heartbeat:
  # 心跳间隔（秒）。环境变量：无。代码默认：60（app_gateway.go:221）
  every: 3600
```

对交互影响大的键追加提示，例如：

```yaml
react:
  # ReAct 最大迭代次数。环境变量：无。代码默认：15（deep_adapter.go:575）
  # 注意：enable_task_loop=true 时内层上限被置为 MaxInt，本项对主 deep agent 不生效
  max_iterations: 100
```

### 5.4 禁止照抄清单

| 项 | 原因 |
|---|---|
| `${MODEL_NAME}` / `${MEM0_USER_ID}` 等占位符 | 必须保留占位形态，不可替换为 Python 模板里的示例值（如 `deepseek-chat`） |
| 品牌词 `jiuwenswarm` | 改 `uapclaw`；仅 9/10 两项按裁决使用 `uapclaw*` |
| `telemetry.enabled: false` | Go observability 默认 `true`，照抄会反转语义 |
| `task_memory` 段 | Go 未实现，写入即为无效配置 |
| `models` 段具体模型名 | Go 无内置 `model_name`：`buildEnvFallbackModelEntry` 用 `API_BASE` / `API_KEY` / `MODEL_NAME` / `MODEL_PROVIDER` 环境变量兜底（default_models.go:174-193），照抄 Python 的示例模型名会覆盖环境变量链路 |

### 5.5 防漂移测试

新增 `internal/common/config/config_yaml_parity_test.go`：

1. **存在性**：表驱动列出约 60 条「Go 会读的键路径」，逐条断言模板中存在且 `cfg.Get(path) != nil`。有人删模板键即翻红。
2. **值一致**：对可枚举默认值的键（如 `heartbeat.every`、`react.max_iterations`）断言「模板值 == 代码默认值常量」，不写死字面量。有人改代码默认值忘记改模板即翻红。
3. **可解析**：断言模板可被 `Load` 无错解析。

---

## 6. D-1 缺陷修复：`resolveEnableTaskLoop` 漏接 `skill_create`

**Python 行为**（interface_deep.py:2215-2248）：`_resolve_enable_task_loop` 读 `config_base.evolution.skill_create`，为 true 时**强制返回 true** 并打 warning，因为 `SkillCreateRail` 依赖 `AFTER_TASK_ITERATION` 事件与 `enqueue_follow_up()`，在单轮模式下无法工作。

**Go 现状**（deep_adapter.go:2657-2663）：函数签名收了 `configBase` 参数但**完全未使用**，只读 `config["enable_task_loop"]`，缺 `skill_create` 覆盖。Go 侧 `GetSkillCreateEnabled` 已存在（runtime_inheritance.go:138-144），属漏接。

**后果**：用户显式 `enable_task_loop: false` 且开启 `skill_create` 时，Python 强制打开并告警，Go 真关掉 → `SkillCreateRail` 静默失效。

**修法**：在返回用户配置前判 `GetSkillCreateEnabled(configBase)`，为 true 时返回 true 并打 `Warn`（含 `configured_value`、`reason` 字段）。

**测试**：`skill_create=true + enable_task_loop=false → true`；`skill_create=true + 缺键 → true`；`skill_create=false + enable_task_loop=false → false`。

---

## 7. 文件清单

| 文件 | 动作 |
|---|---|
| `internal/common/logger/format.go` | 新增（约 130 行） |
| `internal/common/logger/format_test.go` | 新增（表驱动） |
| `internal/common/logger/logger.go` | 改 3 处（Caller / 文件链 / 控制台链） |
| `internal/common/logger/logger_test.go` | 增端到端、脱敏、单行、NO_COLOR、caller 断言；删 `format: "json"` 字面量 |
| `internal/common/logger/doc.go` | 文件目录补 `format.go` 条目（项目规则硬要求） |
| `internal/common/config/sections.go` | 删 `Format` 字段 |
| `internal/common/config/sections_test.go` | 删 `Format` 断言 3 处 |
| `internal/common/config/testdata/config.yaml`、`template.yaml` | 删 `format: "json"` |
| `internal/common/config/config_yaml_parity_test.go` | 新增 |
| `internal/swarm/server/adapter/deep_adapter.go` | 修 `resolveEnableTaskLoop` |
| `internal/swarm/server/adapter/deep_adapter_config_test.go` | 补 3 条单测 |
| `resources/config.yaml` | 重写（31 行 → 约 450 行） |

---

## 8. 测试计划

| 层次 | 用例 |
|---|---|
| 单元（format） | 时间：`json.Number` / `float64` / RFC3339 字符串 / 非法值；级别：5 个枚举 + 未知；caller：正常绝对路径 / 单段路径 / 空值；消息：`\n` `\r` `\t` 转义；component：字符串与非字符串 |
| 单元（logger） | 端到端正则断言整行形态 `^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}\] \[INFO\] \[common\] \[logger/logger_test\.go:\d+\] .+$`；caller 行号用 `runtime.Caller(0)` 定位区间断言（不写死行号） |
| 单元（脱敏） | `.Str("password","secret123")` → 文件中不含明文且含 `******` |
| 单元（单行） | `.Msg("a\nb")` → 文件中只有一行且含字面 `a\nb` |
| 单元（颜色） | 默认无色；`FORCE_COLOR=1` → 有 `\x1b[`；`FORCE_COLOR=1` + `NO_COLOR=1` → 无 `\x1b[` |
| 单元（config） | parity 三条（存在性 / 值一致 / 可解析） |
| 单元（D-1） | 3 条 skill_create 与 enable_task_loop 组合 |
| 回归 | `go build ./...`、`gofmt -l`、`staticcheck`、`go test ./...` |

覆盖率：改动文件按项目门禁「改动行覆盖 ≥85%」执行。

---

## 9. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 其他包存在断言旧 JSON 日志格式的测试 | **已实测排查**：全仓 `grep '"level":' --include=*_test.go` 的命中都是权限/参数字典，无一条断言日志行；`grep 'component=' --include=*_test.go` 零命中。风险已基本排除 |
| 补 `channels` 段触发归一化「整段替换」（normalize.go:65-79 在 web 缺键时把整个 `channels.web` 替换为 `{send_file_allowed: true}`） | 模板中 `channels.web` 各键写全，并在 parity 测试中断言 `send_file_allowed` 存在 |
| 补全配置后启动行为变化 | 落地后本地冒烟：`make build` + 启动 CLI，核对日志形态与生效配置（内存/记忆 rail 是否按裁决挂载） |
| `max_iterations: 100` 在 `enable_task_loop=true` 下不生效造成误解 | 模板注释显式说明（见 5.3） |
| 分发包体积增长 | 31 → 450 行，可忽略 |

---

## 10. 验收标准

1. 控制台与全部日志文件输出符合第 3 节形态，`[文件:行号]` 指向真实业务调用点。
2. 密码、token、手机号等敏感数据在文本形态下仍被脱敏。
3. `logging.format` 从代码与全部测试中消失，`go test ./internal/common/config ./internal/common/logger` 通过。
4. `resources/config.yaml` 中每个键都能在代码里找到读取点（由 parity 测试兜底）；5 个未实现段有明确标注。
5. `resolveEnableTaskLoop` 具备 `skill_create` 覆盖并有测试锁定。
6. `go build ./...`、`gofmt -l`、`staticcheck`、`go test ./...` 全部通过。
