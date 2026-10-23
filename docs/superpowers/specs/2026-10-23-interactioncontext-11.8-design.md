# 11.8 InteractionContext（PendingInteraction）设计

- **日期**：2026-10-23
- **对应计划**：IMPLEMENTATION_PLAN.md 领域十一 11.8
- **Python 源**：`jiuwenswarm/gateway/routing/interaction_context.py`（189 行）
- **方案**：方案A —— 11.8 单独实现（数据层先行，消费方 11.12/11.16/11.21 ⤵️ 回填）

## 1. 背景与定位

PendingInteraction 是数字分身「追问-等待-恢复」闭环的持久化核心：

```
群聊提问 → Agent 回复带 [群聊追问@张三] / [私聊追问] 前缀
  → Outbound Pipeline 创建 PendingInteraction 落盘        (11.12 消费)
被追问者回复
  → 群聊路径：Inbound Pipeline find_group_pending 命中     (11.12 消费)
  → 私聊路径：通道层 find_pending 判 principal 触发 resume  (11.16/11.21 消费)
  → build_resume_content 拼 [任务恢复] 提示词 → Agent 继续任务
```

本期交付纯文件存储层：**零 LLM、零 adapter、零 channel 依赖**。交付后处于零调用状态（先例：10.5.10 CryptoUtility 的 `set_crypto_provider` 零调用死代码保留、Python `find_all_group_pending` 全仓零消费方仍实现）。

## 2. 包与文件

| 文件 | 职责 |
|------|------|
| `internal/swarm/gateway/routing/interaction_context.go` | PendingInteraction 结构体 + 8 方法 + 敏感键过滤 |
| `internal/swarm/gateway/routing/interaction_context_test.go` | 单元测试（覆盖率目标 ≥85%） |
| `internal/swarm/gateway/routing/doc.go` | 追加文件条目 + 包概述补一句 |

与 Python `gateway/routing/interaction_context.py` 同包对齐（routing 包已有 11.7 SessionMap 先例）。

依赖：`internal/common/utils/path`（AgentInteractionsDir，已实现）+ `internal/common/logger` + 标准库。**无循环依赖**（path/logger 均为叶子依赖）。

## 3. 类型与常量

```go
// PendingInteraction 待处理追问交互（统一群聊追问与 DM 追问两种模式）
type PendingInteraction struct {
    // 15 字段对齐 Python dataclass，json tag 逐字段相同
    InteractionID    string         `json:"interaction_id"`
    Mode             string         `json:"mode"`
    OriginChannelID  string         `json:"origin_channel_id"`
    OriginSessionID  string         `json:"origin_session_id"`
    OriginContent    string         `json:"origin_content"`
    OriginSenderName string         `json:"origin_sender_name"`
    OriginSenderID   string         `json:"origin_sender_id"`
    Question         string         `json:"question"`
    TargetUserID     string         `json:"target_user_id"`
    TargetUserName   string         `json:"target_user_name"`
    OriginMetadata   map[string]any `json:"origin_metadata"`
    PrincipalID      string         `json:"principal_id"`
    Status           string         `json:"status"`
    CreatedAt        float64        `json:"created_at"`
    UpdatedAt        float64        `json:"updated_at"`
}
```

常量（对齐 Python 字面值，**不发明 iota 枚举**——Python 是自由字符串，JSON 落盘也是字符串）：

```go
const (
    ModeDM          = "dm"
    ModeGroup       = "group"
    StatusPending   = "pending"
    StatusCompleted = "completed"
    DefaultTTL      = 86400.0
)
// sensitiveKeys 敏感键集合（小写比较）：api_key / token / authorization
```

文件名前缀语义（不做成常量，只在方法内拼接）：
- `gpq_{session_id}_{user_id}.json` —— 群聊追问（确定性 ID，可直达 load）
- `iact_{channel_id}_{hex}.json` —— DM 追问（随机后缀）

存储路径：`path.AgentInteractionsDir()` = `{workspace}/agent/workspace/interactions/`（Python `get_interactions_dir()` 对齐物，已实现）。

## 4. 方法集（8 个）

Python `@classmethod`/实例方法 → Go 包级函数/指针接收者方法：

| Python | Go | 语义要点 |
|--------|-----|---------|
| `save()` | `(*PendingInteraction) Save()` | 刷 updated_at→过滤敏感键→MarshalIndent(2空格, 非ASCII不转义)→WriteFile；失败仅 Warn |
| `remove()` | `(*PendingInteraction) Remove()` | Stat 存在才删；失败仅 Warn |
| `load(id)` | `LoadPendingInteraction(id) *PendingInteraction` | 文件不存在→nil；解析失败→Warn+nil |
| `find_pending(channel, principal)` | `FindPendingInteraction(channelID, principalID) *PendingInteraction` | 见 §5 |
| `find_all_group_pending()` | `FindAllGroupPending() []*PendingInteraction` | gpq_* + status=pending + 未过期 |
| `find_group_pending(session, user)` | `FindGroupPending(sessionID, userID) *PendingInteraction` | 直达 load `gpq_{session}_{user}`，**不过滤 status/TTL** |
| `cleanup_expired(ttl)` | `CleanupExpired(ttl float64) int` | 全 *.json 宽松解析按 created_at 删过期，返回删除数 |
| `build_resume_content(answer)` | `(*PendingInteraction) BuildResumeContent(answer string) string` | dm/group 两段模板**逐字复制** Python 中文原文 |

内部辅助（非导出）：
- `interactionsDir() string` —— 每次动态调 `path.AgentInteractionsDir()` + `os.MkdirAll(0o755)`（对齐 Python `_get_interactions_dir()` 每次调用含 mkdir）
- `pi.path() string` —— `{dir}/{interaction_id}.json`
- `filterSensitive(map[string]any) map[string]any` —— 小写比较剔除三敏感键
- `parseInteractionFile(fp string) *PendingInteraction` —— 读+Unmarshal，失败 Debug 日志 + nil（对齐 find_pending 内层 try/except debug 分支）
- `(pi *PendingInteraction) UnmarshalJSON` —— 见 §6 决策1

## 5. FindPending 过滤链（顺序不可调换）

对齐 Python L105-130（执行顺序纪律：审查要点 §2）：

```
for prefix in ("iact_{channelID}_", "gpq_"):          # 顺序遍历两前缀
    for fp in filepath.Glob(dir/prefix*.json):
        pi = parseInteractionFile(fp)                  # 失败 Debug+跳过
        if pi == nil: continue
        if pi.Status != "pending": continue            # ① 状态过滤
        if now-pi.CreatedAt > DefaultTTL:              # ② TTL 过滤
            pi.Remove(); continue
        if pi.PrincipalID != "" && pi.PrincipalID != principalID: continue  # ③ 归属过滤
        if pi.TargetUserID != "" && pi.TargetUserID != principalID: continue # ④ 目标过滤
        if oldest == nil || pi.CreatedAt < oldest.CreatedAt:                # ⑤ 取最旧
            oldest = pi
CleanupExpired(DefaultTTL)                             # 尾部无条件清理
return oldest
```

## 6. 关键设计决策（已拍板）

1. **动态目录（与 SessionMap 差异点）**：每个方法内部调 `interactionsDir()` 动态解析 + mkdir，不构造时固定 storePath。**Why:** Python `_get_interactions_dir()` 每次调用都 mkdir（测试中途创建目录立即生效）；SessionMap 构造时固定是因其 Python 原文就在 `__init__` 里定路径。**How to apply:** 11.8 无 New 构造函数，全部包级函数 + 值由调用方构造后调 Save。

2. **时间字段与 dataclass 默认值 = 自定义 UnmarshalJSON 对齐**（用户拍板 2026-10-23）。
   Python `cls(**{k: data[k] for k in data if k in cls.__dataclass_fields__})` 的重建语义按字段分三类：
   - **必填字段缺失 → TypeError → except 捕获 → 整个文件跳过**：interaction_id/mode/origin_channel_id/origin_session_id/origin_content/origin_sender_name/origin_sender_id/question/target_user_id 九个。Go 对齐：UnmarshalJSON 用指针 alias 结构体检测字段存在性，任一必填缺失 → 返回 error → parseInteractionFile/Load 返回 nil（文件跳过）。
   - **可选字段缺失 → dataclass 默认值**：target_user_name→""、principal_id→""、origin_metadata→`map[string]any{}`、status→**"pending"**（⚠️ 陷阱：缺 status 的文件会通过 find_pending 的状态过滤而非被跳过——必须对齐，不能用 Go 零值 ""）、created_at/updated_at→**当前时间**（`default_factory=time.time` 构造时求值；避免旧文件被 TTL 立即误删）。
   - **未知键 → 忽略**（`k in dataclass_fields` 过滤；Go json 天然忽略）。
   Go 侧不发明 `NewPendingInteraction` 工厂（Python 无此物，调用方逐字段构造 dataclass 时 default_factory 自动生效）。对齐方式：`Save` 内补零值——`if pi.CreatedAt == 0 { pi.CreatedAt = now }`，UpdatedAt 无条件刷新为 now（对齐 Python `save()` 首行 `self.updated_at = time.time()`）。

3. **日志 Msg 译英文**（用户拍板 2026-10-23，已存记忆 feedback-logs-english）：Python 中文日志原文（"save 失败"等）→ Go 英文 Msg（"PendingInteraction save failed"）。**边界：** 代码注释仍中文（项目规范）；BuildResumeContent 提示词模板逐字保留中文（提示词一比一纪律）。结构化字段对齐 Python %s/%d 变量。

4. **origin_metadata nil → 空 map**：`filterSensitive(nil)` 返回 `map[string]any{}`，序列化 `{}`（Python 空 dict 序列化 `{}` 非 `null`）。Unmarshal 时 JSON `null` → nil map（Python None）。

5. **cleanup 宽松解析（字符串时间戳）**：`CleanupExpired` 不走 UnmarshalJSON（它读的是"裸 dict"而非重建 dataclass），用 `map[string]any` 手动取 `created_at`：
   - 数字 → 直接用
   - 字符串 `"123.5"` → `strconv.ParseFloat` 成功则用（Python `float("123.5")` 同效）
   - 字符串 `"abc"` → ParseFloat 失败 → **跳过该文件不删**（Python `float("abc")` 抛 ValueError → except 捕获 → continue，语义一致）
   - 缺失 → Python `data.get("created_at", 0)` 得 0 → `now-0 > ttl` 恒真 → **删除**。Go 对齐：缺失当 0 处理走删除分支（与 Python 完全一致，这不是跳过）。
   日期提取失败与 JSON 解析失败均仅 Debug 日志。

6. **不加锁**：结构体无共享可变状态（与 SessionMap 的本质差异：SessionMap 有内存 map + checkpoint 持久化需一致性；11.8 是无状态文件操作）。并发双删幂等无害。对齐 Python asyncio 单线程无锁语义。

## 7. 测试设计

注入模式（memoryrpc 先例）：
```go
func setupInteractionsEnv(t *testing.T) string {
    t.Helper()
    tmp := t.TempDir()
    t.Setenv("UAPCLAW_DATA_DIR", tmp)
    pathutil.ResetCache()
    t.Cleanup(pathutil.ResetCache)
    return tmp
}
```

用例清单（目标覆盖率 ≥85%，纯文件层无 build tag）：

| 用例 | 覆盖点 |
|------|--------|
| TestSave_落盘与敏感键过滤 | 写文件内容断言 + api_key/Token/AUTHORIZATION 大小写三态剔除 |
| TestSave_失败仅Warn | 目录路径被同名文件占据 → MkdirAll 失败不 panic |
| TestRemove_存在与不存在 | 存在删除 + 不存在静默 |
| TestLoad_完整往返 | Save→Load 字段逐一断言（15 字段） |
| TestLoad_未知键忽略与缺失时间补now | 手写 JSON 含未知字段、缺 created_at → 断言 now 附近 |
| TestLoad_文件不存在与损坏 | nil 返回 + 坏 JSON → nil |
| TestLoad_必填字段缺失跳过 | 缺 question 的 JSON → nil（TypeError→except 对齐） |
| TestLoad_缺status默认pending | 手写 JSON 无 status 字段 → Load 后 Status=="pending" |
| TestFindPending_五层过滤顺序 | ①非pending跳过 ②过期触发Remove+跳过 ③principal不匹配跳过 ④target不匹配跳过 ⑤两前缀间取最旧 |
| TestFindPending_尾随cleanup | 预置一个**坏 JSON** gpq 文件（两前缀扫描都会 parse 失败跳过）→ FindPending 尾部 CleanupExpired 仍扫 *.json；另预置一个缺 created_at 的 iact 文件 → 尾部清理当 0 删除 |
| TestFindAllGroupPending | pending+未过期收集、completed/过期排除 |
| TestFindGroupPending_直达加载 | ID 拼接正确 + 不做 status/TTL 过滤（completed 也返回） |
| TestCleanupExpired | 过期删/未过期留/坏文件跳过不删/返回计数 |
| TestCleanupExpired_字符串时间戳 | `"created_at": "1"` 字符串 → ParseFloat 成功 → 过期删除 |
| TestCleanupExpired_字符串无法解析 | `"created_at": "abc"` → 跳过不删（对齐 Python ValueError→except） |
| TestCleanupExpired_字段缺失当0删除 | 无 created_at 键 → 当 0 → 恒过期 → 删除（对齐 Python `data.get("created_at", 0)`） |
| TestBuildResumeContent_DM模板 | 逐字节断言中文模板拼接（含 origin_content/sender_name/question/answer） |
| TestBuildResumeContent_群聊模板 | 含 target_user_name 回退 "用户" 分支 + 显式名分支 |

边界注意（Python 对齐审查要点 §4）：本文件无 splitlines 场景；glob 排序不稳定 → FindPending 最旧断言用时间戳差值构造而非依赖枚举顺序。

## 8. 回填清单（不可丢失）

| 位置 | 内容 | 时机 |
|------|------|------|
| `routing/doc.go` | 文件目录追加 `interaction_context.go # PendingInteraction 追问上下文（11.8）` + 概述一句话 | 本期 |
| `IMPLEMENTATION_PLAN.md` 11.8 行 | ☐→✅，产出列注明「⤵️ 消费方 11.12（im_pipeline）/ 11.16 飞书 / 11.21 企微 DM resume」 | 本期 |
| `forward_loop.go` L113 `TODO(#11.12)` | **不动**（步骤6 Inbound Pipeline 属 11.12） | 11.12 |
| `message_handler.go` L105-107 `TODO(#11.12)` | **不动**（OutboundPipeline interface 预留属 11.12） | 11.12 |
| 10.6.18 memory_rpc ⤵️11.15、11.3 cron stub→11.10 | **不动**（与 11.8 无交集） | 各自章节 |

## 9. 验收标准

1. `go build ./...` 通过，零新 import 环
2. `go test ./internal/swarm/gateway/routing/...` 全绿，interaction_context.go 覆盖率 ≥85%
3. `go vet` 干净
4. 8 方法语义逐条对照 Python 源码可追溯（注释带 `Python:` 行号）
5. 回填清单 §8 前两项完成
