# 项目规则

## 规则 1：doc.go 包文档规范

每个 Go 包目录下**必须**包含一个 `doc.go` 文件，用于介绍当前包的实现内容和文件目录结构。

### doc.go 必须包含以下内容

1. **包功能概述**：用一到两段话说明本包的职责和在项目中的定位
2. **文件目录**：以树形结构列出本目录下所有 `.go` 文件（不含 `_test.go`）及其职责简述
3. **对应 Python 代码**：标注本包对应的 Python 源码路径（如果存在）
4. **核心类型/接口索引**：列出本包导出的核心类型和接口（如果包内容较多）

### doc.go 格式模板

```go
// Package xxx 提供本包功能的简要描述。
//
// 更详细的说明段落，描述本包在整体架构中的位置、
// 与其他包的依赖关系、核心设计决策等。
//
// 文件目录：
//
//	xxx/
//	├── doc.go           # 包文档
//	├── foo.go           # Foo 结构体和核心逻辑
//	├── bar.go           # Bar 辅助功能
//	└── baz.go           # Baz 配置定义
//
// 对应 Python 代码：openjiuwen/core/xxx/
package xxx
```

### 维护要求

- **新增文件时**：必须在同目录的 `doc.go` 文件目录中添加对应条目
- **删除文件时**：必须同步移除 `doc.go` 中的对应条目
- **重命名文件时**：必须同步更新 `doc.go` 中的文件名
- **修改包功能时**：如果包的核心职责发生变化，必须更新包功能概述
- **阅读源码前**：应先阅读目标包的 `doc.go` 了解包的整体结构和设计意图
- **文件目录排序**：按 `doc.go` → 核心类型文件 → 辅助功能文件 → 配置/常量文件的顺序排列

### 注意事项

- 文件目录中**不包含** `_test.go` 文件
- 树形结构使用 tab 缩进，与 Go doc 约定一致
- 如果包只有 `doc.go` 一个文件（尚未实现），文件目录段可省略，但在实现后必须补上
- 所有注释使用中文（与项目编码规范一致）

## 规则 2：实现计划进度同步

每完成一个实现计划中的任务步骤后，**必须**同步更新 `IMPLEMENTATION_PLAN.md` 中对应步骤的状态标记：

- 完成任务时：将 `☐` 改为 `✅`
- 开始任务时：将 `☐` 改为 `🔄`
- 分组标题行（如 `**5.x 会话系统**`）的状态为 `—`，不需要修改

**状态符号说明：**

| 符号 | 含义 |
|------|------|
| ✅ | 已完成 |
| ☐ | 未开始 |
| 🔄 | 进行中 |
| — | 分组标题（不跟踪） |

**要求：**
- 任务完成的标准是代码实现完毕且测试通过，不是代码写完就行
- 更新状态时不要遗漏，每个步骤对应一行
- 如果某个步骤部分完成（如 9.38-49 合并行），只有全部完成时才打 ✅
- 预留/延后实现的部分，在当前节用 ⤵️ 标记回填点，在目标节用 ⤴️ 标记需回填来源；后续完成回填时，修正为已完成

## 规则 3：日志同步规则

实现 Go 代码时，**必须**对照对应 Python 源码中的 logger 调用，在 Go 代码中等价位置补充日志。

### 3.1 基本要求

1. 实现 Go 代码前，先搜索对应 Python 文件中的 `logger.` 调用（`logger.info`、`logger.warning`、`logger.error`、`logger.debug`）
2. 在 Go 代码的等价位置使用组件级日志函数补充日志。包级定义 `const logComponent = logger.ComponentXxx`，调用 `logger.Info(logComponent).Str(...).Msg(...)`。不再使用 `logger.GetLogger`（已改为非导出 `getLogger`）
3. Python 级别映射：`logger.debug` → `logger.Debug(logComponent)`，`logger.info` → `logger.Info(logComponent)`，`logger.warning` → `logger.Warn(logComponent)`，`logger.error` → `logger.Error(logComponent)`
4. config 包因循环依赖（logger → config）不能导入 logger，改用标准库 `log.Printf` 以 `[config]` 前缀区分

### 3.2 组件选择规则

| 组件常量 | 适用范围 |
|---------|---------|
| `ComponentCommon` | 基础设施层（config/workspace/dotenv/version/schema/exception/crypto/utils） |
| `ComponentGateway` | Gateway 日志 |
| `ComponentChannel` | swarm/channel/* |
| `ComponentAgentServer` | swarm/server/* |
| `ComponentPermissions` | 安全/权限 |
| `ComponentAgentCore` | agentcore/*（LLM 模型客户端、schema 等 agentcore 下所有包） |

### 3.3 日志字段对齐规则（逐条对照，不可省略）

5. Python 日志中的 f-string 变量必须在 Go 日志中以结构化字段等价体现。例如 Python `f"model: {model}, size: {size}"` → Go `.Str("model_name", model).Str("size", size)`
6. Python 在同一逻辑点对不同分支记录不同日志时（如 i2v/t2v 分开记录），Go 也应分别记录，各自包含该分支的特定字段
7. Python 日志中对输出结果的描述性字段（如 `url_present`、`data_present`、`image_count`、`video_url[:100]`）也必须同步，这些是排查问题的关键信息

### 3.4 异常路径日志规则（防御性日志，不可省略）

8. Python 中 `except Exception as e: logger.error(..., exc_info=True)` 对应的 Go 代码，必须在每个可返回 error 的分支添加 Error 日志，包括但不限于：
   - 参数验证失败（validate 函数返回 error 时）
   - API 调用失败（HTTP 层已记录，但方法层也应在解析失败时记录）
   - 响应解析失败（extract 函数返回 error 时）
9. 异常路径日志必须包含 `event_type=LLM_CALL_ERROR`、`method`、`model_provider` 等上下文字段，并使用 `.Err(err)` 记录原始错误

## 规则 4：cmd 包文件组织

cmd 包的文件组织方式：

1. **main.go** 加 `//go:build !test` 只放 `Execute()` + `main()`
2. 所有可测试的命令构建逻辑放 **cmd.go**
3. 不要创建多余的 `main_main.go` 文件
4. Makefile 的 test/test-cover 目标需加 `-tags test`

**原因：** `Execute()` 含 `os.Exit(1)` 无法在测试中调用，`main()` 不参与单元测试覆盖率。直接在 main.go 上加 build tag 隔离，避免创建多余文件。

## 规则 5：Go 编译规则

编译 Go 项目时必须遵循以下规则：

1. **使用 GOPROXY 代理**：编译前设置 `export GOPROXY=https://goproxy.cn,direct`，加速依赖下载
2. **单进程互斥**：同一时间只允许一个 `go build`/`go test` 进程运行。如果检测到有多个 go 编译进程，先 `pkill -f 'go build'` 或 `pkill -f 'go test'` 杀掉旧进程，再启动新的编译
3. **避免清理缓存**：`go clean -cache` 后全量编译非常耗时（可能超过 20 分钟），除非必要否则**不要**执行
4. **长时间等待**：确认只有一个 go 编译进程在运行后，可以长时间等待直到编译完成，不要中途中断
5. **编译前检查**：每次编译前先执行 `pgrep -f 'go (build|test)'` 检查残留进程，如有则 kill 后再编译

**原因：** 清理缓存后全量编译极慢，多进程并发编译会争抢资源导致更慢甚至失败

## 规则 6：集成测试完成标准（远端 CI 全绿）

编写或修改集成测试（`tests/integration/**` 下任何文件）时，**本地测试通过不算完成**。完成标准：

1. **提交远端**：将改动推送到远端仓库（push 到分支或提交 PR）
2. **监控 CI**：通过 `gh run list --limit 5` / `gh run watch <run-id>` 或 `gh run view <run-id>` 跟踪触发的 CI 运行
3. **全部通过**：Build / Lint / Unit Test / Integration Test 四个 job 全部 SUCCESS 才算完成
4. **失败必须修复**：任何 job 失败时，分析失败原因、修复、再次推送，直到全绿；不允许在 CI 红灯状态下宣告集成测试工作完成

**原因：** 集成测试的正确性依赖远端环境（race 检测、覆盖率门禁、合规性检查），本地跑通无法覆盖这些门禁。历史上出现过本地通过但远端偶发失败（如 TestOuterLoop_多个FollowUpFIFO消费）被漏掉的案例。

## 规则 7：集成测试真实性判定（防单元测试冒充）

**集成测试必须真实跨 internal 二级子系统。** 由 `cmd/integration-audit` 工具在 CI Integration Test job 中以两层门禁强制执行，违反即失败：

### 第一层：静态审计（"验证集成测试合规性"步骤）

基于 AST CallExpr 调用分析，击杀符号引用凑数与空壳测试：

1. **调用跨度**：目录内真实调用（`pkg.Fn(...)` 形式，函数值引用/类型/常量不计）的 internal 二级子系统数 ≥ 3。跨度不足的目录标记为灰区，交由第二层裁决（不直接判罚）
2. **有效测试密度**：每个目录的有效测试方法数 ≥ 1（方法体非空、非纯 `s.T().Skip`、断言数 ≥ 2）
3. **硬性规则**：每个文件必须有 `//go:build integration` 标签、必须调用 `suite.Run(`；目录聚合 distinct internal import 路径数 ≥ 2
4. **复合 build tag 正确处理**：`//go:build integration && llm` 文件用 `go/build/constraint` 求值，不在 CI 套件内的文件跳过分析而非误报

### 第二层：覆盖交叉验证（"覆盖交叉验证"步骤）

对第一层标记的灰区目录，逐目录独立运行 `go test -coverprofile`，按 internal 二级子系统聚合真实覆盖行数，要求 **≥ 3 个子系统各 ≥ 30 行**。

- 共享 suite 基类的 `SetupTest` 会给每个目录带来约 15~24 行"基线污染"，30 行门槛高于基线最高值，用于区分污染与测试体驱动的真实覆盖
- 实测案例：静跨度=1 的 heartbeat 覆盖 7 个子系统 ≥30 行 → 判真集成；冒充目录 swarm/server 各子系统 ≤24 行（纯基线污染）→ 判不通过
- **这层无法静态伪造**：调用可以只写不跑，覆盖行数只能由真实执行产生

### 编写要求

1. **写集成测试前先自检**：确认测试链路真实跨越至少 3 个子系统（例如 Runner 编排 + ResourceMgr + session 生命周期，或 LLM 客户端 + harness + 工具系统），或跨越 ≥2 个子系统且覆盖行数显著
2. **mock 只允许在边界**：只 mock 网络协议端（MCP client、HTTP 端点、进程边界），不得 mock 被测子系统本身
3. **单子系统测试属于单元测试**：应放在对应 `internal` 包的 `_test.go` 中，不得放进 `tests/integration/`
4. **豁免机制**：确属集成性质但跨子系统数不足的（如 `external/spawn` 真跨进程、`external/chromadb` 需真实服务），在 `.integration-scope-exempt` 中登记目录并注明理由；豁免必须给出具体集成边界（进程/网络/文件系统）
5. **本地自检命令**：`go run ./cmd/integration-audit -verify`（完整两层门禁模拟）

**原因：** 曾发现两类冒充手法：①纯函数包测试（如 headers_helper）通过 import 2 个包（其中一个仅作类型引用）绕过 import 计数检查；②符号引用凑数（如 `s.NotNil(runner.GetResourceMgr)` 引用函数值不调用，swarm/server 全目录仅包引用验证无真实测试）。import 计数可伪造、符号引用可伪造，只有 AST 调用分析 + 运行时覆盖交叉验证双层结构无法绕过。

---

## 规则 8：重构必须同一改动内删除残留（禁止兼容壳）

重构、迁移、层次对齐（把逻辑从 A 层搬到 B 层）完成时，**必须在同一次改动里删除被替代的旧实现**，不允许"先留着、以后再删"。

### 8.1 必须删除的对象

1. 旧函数/方法/类型/字段/常量/全局变量，含只被旧实现使用的私有助手
2. 旧文件与包（同步删它的 `doc.go` 条目与 `_test.go`）
3. 已失效的注释、`doc.go` 文件树条目、`IMPLEMENTATION_PLAN.md` 的残留条目
4. 只为切换新实现而存在的开关/标志位/兼容分支（如 `useNewXxx bool`、`legacyXxxPath`）
5. 仅服务于旧实现的测试桩、mock、测试数据文件

### 8.2 禁止的做法

- 保留只做转调的"兼容壳"并挂 `TODO: 删除`
- 用 `_ = oldFn` / `var _ = oldFn` / `_ = oldField` 消警
- 注释掉代码代替删除（git 历史即备份）
- 以"以防外部引用"为由保留已无调用方的导出符号（本仓非公开库；确需保留必须在 spec 的「已知偏差」写明理由与回收条件）

### 8.3 验收方式

1. 实现计划必须含一节「删除清单」，逐条列出：文件、符号、注释/doc.go 条目、测试文件
2. **grep 锚点为空**：旧符号名在 `internal/`、`tests/`、`cmd/` 中 0 命中（spec/计划/台账中的历史记载除外）
3. 改名场景：旧文件名与新文件名不得同时存在（含 `_test.go`）
4. `go build ./...`、`go vet ./...`、golangci-lint（`unused`/`staticcheck`）通过；不得靠豁免文件掩盖
5. 集成测试与审计：删掉的入口若被集成用例引用，须同步改用新入口，不允许留 `Skip`

**原因：** 残留会变成"看起来还在用"的假信号——历史上多次出现旧实现与新实现并存，导致行为走旧分支（如 plan 状态机同时留在 adapter 与 runtime、旧 mode 解析三份并存）。残留也让改动行覆盖率门禁失真：老代码仍在被测试覆盖，新代码却没人调用。

---

## 规则 9：对齐 Python 必须结构忠实（禁止私自拆分/搬移步骤）

对齐上游 Python 时，Go 侧必须在**目录层级、文件归属、类型与方法归属、方法内步骤**四个层面与原实现一一对应。行为等价不等于对齐；"用 Go 惯用法重写一遍"视为未对齐。

### 9.1 目录与文件层级

1. Python 一个模块文件 → Go 一个文件；Python 一个包目录 → Go 一个包目录
2. 禁止把一个 Python 文件拆成多个 Go 文件（如 `xxx.go` + `xxx_helpers.go` + `xxx_rails.go`）
3. 禁止把 Python 的多个文件合并成一个 Go 文件
4. 文件名按 Go 命名习惯转换（下划线 → 小驼峰/全小写），但**一个文件对一文件**的映射必须可在 `doc.go` 中一眼核对

### 9.2 类型与方法归属

1. Python 的类方法/实例方法/静态方法，在 Go 中分别对应 `func (T) M` / `func (T) M` / 包级函数，**归属（挂在哪个类型上）不得改动**
2. Python 的模块级函数保持包级函数，禁止为了"类型自洽"塞进某个 struct
3. 方法名按 Python 原名转换（`snake_case` → `CamelCase`），导出与否只由 Go 可见性规则决定，**不得改名以"更符合 Go 习惯"**
4. Python 的 `Enum` 成员集合与取值必须完整映射；不得因为"Go 侧用不到"而裁剪成员

### 9.3 方法内的步骤

1. 方法体按 Python 原始步骤顺序排列，步骤边界（注释编号、空行分组）保持可对应
2. 条件分支结构、短路顺序、提前返回位置不得调整
3. Python 内联在父方法里的逻辑，Go 也内联；Python 抽了助手函数的，Go 也用助手函数

### 9.4 禁止的做法

- 为"可读性/可测试性"把 Python 的一步拆成 Go 的独立助手函数
- 把 Python 助手函数的步骤内联回调用方
- 把两步合并成一层循环/管道（如把"计算 + 写入"合成一个 range）
- 用 `defer`/`goroutine`/中间抽象层替换 Python 的显式顺序步骤来"简化"代码
- 以"Go 没有继承/单线程语义"为由自行设计结构，而不先登记偏差

### 9.5 对齐后删除历史实现

对齐完成后，被替代的旧实现按**规则 8**在同一改动内删除，不留兼容壳、不留"旧路径开关"。改名/迁移场景必须做到旧符号 0 命中。

### 9.6 例外的唯一出口

确实无法一比一（如 Go 无继承、Python 单线程共享状态、语言层缺能力）时：

1. 在 spec 的「已知偏差」写明：Python 符号 + 文件行号、Go 等价实现位置、偏差性质、理由、回收条件
2. 经用户确认后才可实施
3. 不得在代码注释里"自行宣布"偏差了事（注释只记录，不替代确认）

**原因：** 结构漂移是行为漂移的前兆。历史上多次出现"行为看起来一样"但步骤归属已变的对齐：plan 状态机同时留在 adapter 与 runtime、mode 解析三份并存、`_mode_change_cancel_and_notice` 与 `/switch` 分支被合进同一函数后语义各自漂移。结构一旦不可逐行对照，后续上游每次变更都无法判断"该改哪一处"，对齐成本从"看 diff"退化为"重读两份源码"。
