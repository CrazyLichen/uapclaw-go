# 2026-08-22 审查问题修复设计

> 基于 docs/review/2026-08-22-logic-review.md 审查结果，逐项验证后确认的真实问题及修复方案。
> 审查文档中 53 个问题，10 个已不存在（代码已修复），28 个确认真实存在，其余为 T7.1 低影响项。
> 28 个中 22 个现在实现，6 个标注 ⤵️ 回填。

---

## 一、7.6 FragmentMemoryManager（5个）

### S7.1：AddMemories / MemUpdateChecker.Check 缺少 llm 参数

**修改签名：**
```go
// BaseMemoryManager 接口
AddMemories(ctx context.Context, userID, scopeID string,
    memories map[string][]*mem_model.BaseMemoryUnit, llm ...*llm.Model) ([]*mem_model.BaseMemoryUnit, error)

// MemUpdateChecker.Check
type CheckOption func(*checkConfig)
func WithModel(m *llm.Model) CheckOption { ... }
func WithRetries(n int) CheckOption { ... }
func (c *MemUpdateChecker) Check(newMemories, oldMemories map[string]string, opts ...CheckOption) ([]*MemoryActionItem, error)
```

### S7.2：BaseMemoryManager 接口使用具体类型

改用 `BaseMemoryUnit` 基类型。FragmentMemoryManager 实现内部做类型断言转具体类型（与 Python 的 isinstance 检查对应）。

### M7.1：Search 缺少排序+截断

在 FragmentMemoryManager.Search 返回前添加 `sort.Slice` 按 Score 降序 + topK 截断。

### M7.2：ListFragmentMemories 缺少校验+排序

1. 添加 `isFragmentMemoryType(memType)` 校验，非法时记录 Error 日志并返回空列表
2. 对结果按 (mem, timestamp) 降序排序

### M7.3：缺失 processConflictInfo 方法

仅在 AddMemories 已有 ⤵️ 注释处补充说明 processConflictInfo 需在 7.8 回填时实现。

---

## 二、9.65a-4 TeamBackend（7个）

### S9.2：ShutdownMember 缺少 force 参数

```go
type ShutdownOption func(*shutdownConfig)
func WithForce(force bool) ShutdownOption { ... }
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string, opts ...ShutdownOption) MemberOpResult
```

### S9.4：ShutdownMember 不应调用 CancelAllTasks

直接移除 `tb.taskManager.CancelAllTasks` 调用。

### S9.7：ApprovePlan 参数缺失

```go
type ApprovePlanOption func(*approvePlanConfig)
func WithApproved(approved bool) ApprovePlanOption { ... }
func WithFeedback(feedback string) ApprovePlanOption { ... }
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, opts ...ApprovePlanOption) MemberOpResult
```

### S9.9：ForceCleanTeam 不应触发 onTeamCleaned

直接移除 ForceCleanTeam 中的 `tb.onTeamCleaned(ctx)` 调用。

### S9.10：ForceCleanTeam 无法传 force=True

1. 移除 `m.Status != MemberStatusShutdown` 前置检查，依赖 ShutdownMember 幂等逻辑
2. S9.2 修复后调用 `tb.ShutdownMember(ctx, m.MemberName, WithForce(true))`

### S9.12：SpawnMember 硬编码参数

```go
type SpawnMemberOption func(*spawnMemberConfig)
func WithStatus(s MemberStatus) SpawnMemberOption { ... }
func WithExecutionStatus(s ExecutionStatus) SpawnMemberOption { ... }
func WithMode(m MemberMode) SpawnMemberOption { ... }
func WithAllocation(a *Allocation) SpawnMemberOption { ... }
```

### M9.3：Leader 绕过 SpawnMember

S9.12 修复后，BuildTeam 中 Leader 注册从 `db.Member().CreateMember(...)` 改为 `tb.SpawnMember(ctx, ..., WithStatus(Busy), WithExecutionStatus(Running))`。

---

## 三、10.6.3 StructuredAskUserRail（4个，合并修复）

### S10.1+S10.2+M10.1+M10.2：schema 重构 + 目录迁移

**目录迁移：**
- 从 `internal/swarm/server/rails/structured_ask_user_*.go`
- 迁移到 `internal/swarm/agents/harness/common/rails/`
- 对齐 Python：`jiuwenswarm/agents/harness/common/rails/ask_user_rail.py`
- 保持 Go 拆分文件风格（tool + rail 分开）
- 更新包名、import 路径、doc.go

**Schema 重构（在 NewStructuredAskUserTool 中自建）：**
```go
// 顶层 properties
"query": { "type": "string", "description": "向用户展示的问题（必填）。" }  // required
"questions": { "type": "array", ... }  // optional

// questions item
"required": ["question"]  // 仅 question 必填

// options item
"required": ["label"]  // 仅 label 必填
```

---

## 四、10.3.19-20 技能管理（13个）

### S10.3.1：SkillNet 搜索/安装/评估均为 stub → ⤵️ 回填

三个方法添加 ⤵️ 回填标记 + TODO 注释。Install 的 job 改为设置 status=failed + detail="尚未实现"。

### S10.3.2：mirror 目录机制缺失

实现 `getMirrorSkillsDirs()` 始终返回空切片（Go 二进制等价 Python package 安装模式）。在 ClawHub/TeamSkillsHub/SkillNet/Uninstall 四处调用此方法。添加 ⤵️ 标记说明如需源码开发模式支持需补全。

### S10.3.3：uninstall 内置技能保护缺失

实现：遍历 builtin 目录 → 解析 SKILL.md 匹配技能名 → 检测到内置技能则拒绝删除。

### S10.3.4：import_local 远程 URL 下载缺失

实现：`isHTTPDownloadTarget(url)` → `assertImportLocalDownloadURLAllowed(url)` 白名单 → HTTP 下载 → SHA256 校验 → ZIP/tar.gz 解压 → 复用本地导入逻辑。

### S10.3.5：TeamSkillsHub Publish 缺少 plugin.yaml

实现：解析 SKILL.md → 生成 plugin.yaml（name/version/display_name/description/runtime/metadata）→ 构建规范化 ZIP → SHA256 校验 → 上传。

### S10.3.6：git 操作均为 stub

实现 `gitClone/gitPull/gitGetCommit`：使用 `os/exec` 调用 git 命令，返回 commit hash。

### M10.3.1：marketplace_add 默认 enabled 不一致

改为 `enabled: false`。

### M10.3.2：marketplace 缓存清理缺失

- `HandleSkillsMarketplaceRemove`：添加 `os.RemoveAll` 删除本地缓存目录
- `HandleSkillsMarketplaceToggle`：启用时调用 gitPull/gitClone，禁用时 os.RemoveAll 删除缓存

### M10.3.3：Validate 校验简化

添加 skill_type 判断 + teamskills 类型的 roles 完整校验（非空列表、每个 role 有 id、至少 2 个、id 不重复）。

### M10.3.4：YAML 解析简化

引入 `gopkg.in/yaml.v3` 替代逐行解析，支持完整 YAML 语法。补全默认字段和 tags/allowed_tools 类型转换。

### M10.3.5：matchHost 缺少后缀匹配

在 matchHost 中添加以 `.` 开头的规则的后缀匹配逻辑：`strings.HasSuffix(host, rule)`。

### M10.3.6：代理环境变量缺失

实现 `skillnetNetworkContext`：调用前设置 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY 环境变量，defer 恢复原值。从 FREE_SEARCH_PROXY_URL 配置读取代理 URL。

### M10.3.7：安装记录缺 version/commit

AddInstalledPlugin 记录中添加 `version`（从 SKILL.md meta 获取）和 `commit`（空字符串默认值）。

### T10.3.1：无 Windows 重试

实现 `safeRmtree`：最多 3 次重试 + `runtime.GOOS == "windows"` 时修改文件权限 + 指数退避延迟。

### T10.3.2：refreshAgentDataIndexes 空操作

实现 `generateAgentDataForWorkspace` + `refreshAgentDataIndexes`，遍历工作区目录生成 agent-data.json。

---

## 五、10.6.1-2 Prompt Builder（3个）

### M10.6.1：品牌名不统一

统一为 "UapClawSwarm"：
- Code Intro：`"JiuwenSwarm"` → `"UapClawSwarm"`
- Identity 节：`"UapClaw"` → `"UapClawSwarm"`
- 目录名：`".uapclaw"` → `".uapclawswarm"`

### T10.6.1：readWorkspaceFile 缺少 Debug 日志

在 `os.IsNotExist(err)` 分支添加 `logger.Debug(logComponent).Str("file_path", filePath).Msg("文件不存在")`。

### T10.6.2：readWorkspaceFile 缺少 TrimSpace

在返回前添加 `content = strings.TrimSpace(content)`。

---

## 依赖关系

- M10.3.2（缓存清理）依赖 S10.3.6（git 操作）
- M9.3（Leader 走 SpawnMember）依赖 S9.12（SpawnMemberOption）
- S9.10（ForceCleanTeam 传 force）依赖 S9.2（ShutdownOption）
- S10.3.4（远程下载）与 M10.3.5（matchHost 后缀匹配）相关（白名单校验）
- T10.3.2（refreshAgentDataIndexes）依赖 S10.3.2（getMirrorSkillsDirs）
