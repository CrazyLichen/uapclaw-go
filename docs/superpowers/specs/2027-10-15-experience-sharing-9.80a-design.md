# 9.80a ExperienceSharing 设计文档

## 概述

实现跨 Agent 经验共享（ExperienceSharing），让不同用户的技能演进经验可以上传到共享 Hub、搜索和下载安装。

本节位于领域 9（Agent 演化子系统）的后半段，依赖 9.78 Checkpointing 和 9.79 Experience，回填点为 9.24 P5（SkillEvolutionSharingMixin）。

### 流程位置

```
9.72 SkillExperienceOptimizer（LLM 生成经验草稿）
    ↓ 产出 EvolutionRecord
9.79 ExperienceManager（stage/approve/reject 在线经验）
    ↓ 审批通过的 EvolutionRecord
9.80 UpdateExecution（apply 经验变更到技能文件）
    ↓ 变更已本地生效
9.80a ExperienceSharing ← 本节
    ↓ 跨用户共享
9.24 P5 SkillEvolutionSharingMixin（Rail 层集成 ← ⤴️9.80a 回填点）
```

### 两条数据路径

- **上传路径**：本地已审批的 EvolutionRecord → QC 质量筛选（ShareStager）→ 关键词提取（KeywordExtractor）→ 打包为 SharedSkillBundle → 上传到 SharingBackend
- **下载路径**：用户遇到问题 → KeywordExtractor 从对话摘录提取检索关键词（QueryKeywords）→ ExperienceHubClient 搜索/下载 → EvolutionStore.InstallSkillPackage 安装到本地

### 关键约束

- 共享侧 QC 不影响本地持久化——被 ShareStager 丢弃的记录仍由 SkillEvolutionRail 的 auto-save 路径保存
- SharedSkillBundle 是后端存储的最小单元（一个 skill_id 下的一组经验）
- 技能包（skill.tar.gz）是不可变的——首次上传后后续只追加经验 bundle

## 对应 Python 代码

`openjiuwen/agent_evolving/sharing/`

## 文件结构与包组织

```
internal/evolving/sharing/
├── doc.go                    # 包文档
├── types.go                  # SharingMeta/SharedExperience/SharedSkillBundle/SkillPackageMeta/
│                            # SkillSearchResult/QueryKeywords/UploadResult/StagingResult
├── keyword_extractor.go      # KeywordExtractor + QUERY_KEYWORDS_LLM_POLICY
├── share_stager.go           # ShareStager + QC 逻辑 + messagesHasSuccessfulTool
├── experience_sharer.go      # ExperienceSharer + SkillSharingContextProvider 类型
├── hub_client.go             # ExperienceHubClient
└── backend/
    ├── doc.go                # 子包文档
    ├── interface.go          # SharingBackend 抽象接口（7 个方法）
    └── local_file.go         # LocalFileBackend + jaccard 私有函数

internal/evolving/sharing/                  ← 包名 sharing
internal/evolving/sharing/backend/          ← 包名 backend
```

### 依赖关系

```
hub_client → experience_sharer → backend ←
    ↓              ↓
share_stager → keyword_extractor
    ↓              ↓
types ←───────────┘
```

- `types` 不依赖包内其他文件，只依赖 `checkpointing.EvolutionRecord`/`EvolutionPatch`
- `backend` 只依赖 `types`
- `keyword_extractor` 依赖 `types` + `llm_resilience` + `checkpointing.EvolutionPatch`
- `experience_sharer` 依赖 `types` + `backend`
- `share_stager` 依赖 `types` + `keyword_extractor` + `experience_sharer` + `signal`（FAILURE_KEYWORDS）
- `hub_client` 依赖 `experience_sharer` + `types` + `checkpointing.EvolutionStore`

## 类型系统（types.go）

对照 Python `sharing/types.py`，7 个类型全部 1:1 复刻：

### SharingMeta

每条经验的共享侧元数据。

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| SkillName | string | "" | 技能名称 |
| SkillVersion | string | "" | 技能版本 |
| UploadTrigger | string | "user_approval" | 上传触发方式 |
| UploadAt | string | time.Now UTC ISO | 上传时间 |
| FeedbackExcerpt | *string | nil | 反馈摘录（可选） |
| SourceUserID | *string | nil | 来源用户 ID（可选） |
| Confidence | float64 | 0.7 | 置信度 |
| OriginBundleID | *string | nil | 来源 bundle ID（可选） |

方法：`ToDict() map[string]any`、`FromDictSharingMeta(data map[string]any) *SharingMeta`

### SharedExperience

EvolutionRecord 的共享包装。所有共享专属元数据（keywords/summary/SharingMeta）在此类型上，不侵入 EvolutionRecord 本身。

| 字段 | 类型 | 说明 |
|---|---|---|
| Record | checkpointing.EvolutionRecord | 底层演进记录 |
| Keywords | []string | 关键词列表 |
| Summary | string | 摘要 |
| SharingMeta | *SharingMeta | 共享元数据（可选） |

方法：`ToDict()`、`FromDictSharedExperience()`

### SharedSkillBundle

后端存储最小单元：一个 skill_id 下的经验集合。

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| BundleID | string | `sb_{uuid10hex}` | Bundle 标识 |
| SkillID | string | "" | 技能 ID |
| SkillName | string | "" | 技能名称 |
| SkillVersion | string | "" | 技能版本 |
| KeywordsAggregate | []string | [] | 聚合关键词 |
| SummaryAggregate | string | "" | 聚合摘要 |
| Experiences | []SharedExperience | [] | 经验列表 |
| CreatedAt | string | time.Now UTC ISO | 创建时间 |

工厂方法 `MakeSharedSkillBundle(skillName, experiences, skillVersion, summaryAggregate)`：聚合所有 experience 的 keywords 并去重，拼接 summary。

方法：`ToDict()`、`FromDictSharedSkillBundle()`（含 `skill_content_hash` 兼容回退）

### SkillPackageMeta

Hub 上技能包的元数据。

| 字段 | 类型 | 说明 |
|---|---|---|
| SkillID | string | 技能 ID |
| SkillName | string | 技能名称 |
| Description | string | 描述 |
| UploadedAt | string | 上传时间 |

### SkillSearchResult

搜索结果行。

| 字段 | 类型 | 说明 |
|---|---|---|
| SkillID | string | 技能 ID |
| SkillName | string | 技能名称 |
| Description | string | 描述 |
| ExperienceCount | int | 经验数量 |
| Keywords | []string | 关键词 |
| Score | float64 | 相关性分数 |

### QueryKeywords

下载路径检索关键词集。

| 字段 | 类型 | 说明 |
|---|---|---|
| Keywords | []string | 检索关键词 |
| Intent | string | 查询意图描述 |
| RawExcerpt | string | 原始摘录 |

### UploadResult

上传结果。

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| OK | bool | - | 是否成功 |
| BundleID | string | "" | Bundle ID |
| Reason | string | "" | 失败原因 |
| Retryable | bool | false | 是否可重试 |

### StagingResult

ShareStager 筛选结果。

| 字段 | 类型 | 说明 |
|---|---|---|
| StagedForShare | []SharedExperience | 通过 QC 的经验 |
| DroppedForShare | []DroppedRecord | 被 QC 丢弃的记录+原因 |

辅助类型 `DroppedRecord`：`{ Record EvolutionRecord; Reason string }`

方法：`EmptyStagingResult()`、`HasShareable() bool`

## Backend 接口（backend/interface.go）

### SharingBackend 接口（7 个方法）

| 方法 | 签名 | 说明 |
|---|---|---|
| UploadBundle | `(ctx context.Context, bundle SharedSkillBundle) UploadResult` | 上传经验 bundle |
| DownloadBundles | `(ctx context.Context, skillID string, query QueryKeywords, topK int) []SharedSkillBundle` | 按 skill_id + 关键词检索下载 |
| HasSkillPackage | `(ctx context.Context, skillID string) bool` | Hub 是否已有该技能包 |
| UploadSkillPackage | `(ctx context.Context, skillID string, packageBytes []byte, meta SkillPackageMeta) error` | 上传初始技能包（不可变） |
| DownloadSkillPackage | `(ctx context.Context, skillID string) ([]byte, error)` | 下载技能包字节 |
| GetSkillPackageMeta | `(ctx context.Context, skillID string) (*SkillPackageMeta, error)` | 获取技能包元数据 |
| SearchSkills | `(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult` | 全局关键词搜索技能 |

## LocalFileBackend（backend/local_file.go）

### Hub 目录布局

```
~/.openjiuwen/experience_hub/
├── packages/<skill_id>/skill.tar.gz    # 不可变技能包
├── packages/<skill_id>/meta.json       # 技能元数据
├── bundles/<skill_id>/sb_xxx.json      # 经验 bundle
├── index/<skill_id>.jsonl              # 按 skill 的关键词索引
├── index/global.jsonl                  # 全局搜索索引
└── .outbox/<skill_id>/sb_xxx.json      # 上传失败暂存
```

### 并发控制

- `sync.RWMutex`：写操作（UploadBundle/UploadSkillPackage）加写锁，读操作（Download/Search）不加锁

### 去重机制

- UploadBundle 时用 `jaccard` 相似度检查（阈值 0.85），高重叠则拒绝
- `jaccard` 为 `local_file.go` 内的私有函数

### jaccard 相似度

1. 完全交集：`len(intersection) / len(union)`
2. 无交集但有子串包含：`0.5 / max(len(union), 1)`
3. 完全无关：`0.0`
4. 空集双方：`0.0`

### 不可变技能包

`UploadSkillPackage` 检测已存在则跳过（no-op）。

### outbox 暂存

写入失败时 spool 到 `.outbox/<skill_id>/`，返回 `UploadResult{OK:false, Retryable:true}`。

### 全局索引维护

- `_upsertGlobalIndex`：上传 bundle 时合并同 skill_id 的关键词和 experience_count
- `_ensureGlobalIndexEntry`：上传 skill package 时确保全局索引有条目
- `_readGlobalIndex` / `_writeGlobalIndex`：读写 global.jsonl

## KeywordExtractor

### 上传路径：ParseFromOptimizerOutput（静态方法）

- 输入：`EvolutionPatch`（Go 结构体）或 `map[string]any`（原始 JSON）
- 从 `patch.Keywords` 和 `patch.Summary` 提取，纯解析，不需要 LLM

### 下载路径：ExtractQueryKeywords（需 LLM）

- 输入：对话摘录（feedbackExcerpt）+ 可选技能提示（skillHint）
- 调用 `llm_resilience.InvokeTextWithRetry` 提取 QueryKeywords
- LLM 失败时返回空关键词（keywords=[], intent=excerpt[:40]），不阻塞调用方
- 提示词一比一复刻 Python 的中/英文版本

### 结构体

```go
type KeywordExtractor struct {
    llm      llm.Model       // 可选，nil 时跳过 LLM 调用
    model    string          // 模型名称
    language string          // "cn" 或 "en"，默认 "cn"
    policy   LLMInvokePolicy // 默认 QUERY_KEYWORDS_LLM_POLICY
}
```

### 常量

`QUERY_KEYWORDS_LLM_POLICY`：`AttemptTimeoutSecs=1500, TotalBudgetSecs=4000, MaxAttempts=5`

### 私有辅助

`extractQueryJSON(raw string) map[string]any` — 尝试直接 JSON 解析，失败则正则提取 `{...}` 块。

## ShareStager

**职责**：QC 筛选 → 包装 → 入队。不写 evolutions.json，不上传。

### QC 质量门控（qc 方法）

两条规则，任一不通过即丢弃：

1. **不可行门控**：`record.Source == "execution_failure"` 且对话中没有成功工具调用 → 丢弃
2. **分数门控**：`record.Score < qcScoreThreshold`（默认 0.6）→ 丢弃

被丢弃的记录仍然走本地 auto-save 路径，QC 只影响是否进入共享池。

### 辅助函数 messagesHasSuccessfulTool

- 检查消息列表中是否有非失败的工具结果
- 依赖 signal 包的 FAILURE_KEYWORDS 正则

### ScreenAndStage 主方法

```
输入：skillName, records []EvolutionRecord, messages
对每条 record：
  1. KeywordExtractor.ParseFromOptimizerOutput(record.Change) → keywords, summary
  2. qc(record, messages) → 通过/丢弃
  3. 通过 → wrap(record, keywords, summary, skillName) → SharedExperience
  4. sharer.StageForUpload(skillName, wrapped)
  5. 汇总到 StagingResult
输出：StagingResult{StagedForShare, DroppedForShare}
```

### wrap 方法

创建 SharingMeta（skill_name, skill_version, confidence=record.Score, source_user_id）→ 封装为 SharedExperience。

Go 中 EvolutionRecord 是值类型赋值即拷贝，EvolutionPatch 内的 Keywords 切片需显式 copy。

## ExperienceSharer

**职责**：skill 粒度的上传/下载门面，维护内存待上传队列。

### 核心字段

```go
type ExperienceSharer struct {
    backend                    backend.SharingBackend
    localCacheDir              *string
    maxUploadRetries           int          // 默认 3
    backoffBaseSecs            float64      // 默认 0.5
    pendingUploads             map[string][]SharedExperience  // skill_name → 队列
    pendingKeys                map[string]map[dedupKey]bool   // skill_name → 去重集合
    mu                         sync.RWMutex
    skillSharingContextProvider SkillSharingContextProvider    // 可选，后置绑定
}

type dedupKey struct {
    skillName string
    recordID  string
}
```

### SkillSharingContextProvider 函数类型

```go
type SkillSharingContextProvider func(ctx context.Context, skillName string) (
    skillID string, packageBytes []byte, skillName string, description string, err error)
```

### 关键方法

| 方法 | 说明 |
|---|---|
| `SetSkillSharingContextProvider(provider)` | 后置绑定 context provider |
| `ResolveSkillID(ctx, skillName) (string, error)` | 通过 provider 获取 skill_id |
| `HasPending(skillName) bool` | 队列中是否有待上传经验 |
| `StageForUpload(skillName, exp)` | 入队，按 (skill_name, record_id) 去重 |
| `DiscardPendingUploads(skillName) int` | 丢弃队列（负反馈场景） |
| `FlushPendingUploads(ctx, skillName) UploadResult` | 打包 bundle → 同步技能包 → 上传（含重试+指数退避） |
| `DownloadRelevant(ctx, skillID, query, topK) []SharedSkillBundle` | 下载并镜像 |
| `SearchSkills(ctx, query, topK) []SkillSearchResult` | 全局搜索 |
| `DownloadSkillPackage(ctx, skillID) []byte` | 下载技能包 |
| `GetSkillPackageMeta(ctx, skillID) *SkillPackageMeta` | 获取元数据 |
| `ListCachedBundles(skillID) []SharedSkillBundle` | 列出本地镜像的 bundle |

### FlushPendingUploads 流程

```
1. 加写锁取走 pendingUploads[skillName]
2. SharedSkillBundle.Make() 打包
3. syncSkillPackage: 通过 provider 获取 skill_id + package_bytes
   → 若 Hub 无此技能包，先上传 skill.tar.gz（不可变，只传一次）
4. 若 skill_id 为空，返回 UploadResult{OK:false, Reason:"skill_id unavailable"}
5. 重试循环（max_retries, 指数退避 backoff_base * 2^(attempt-1)）
   → backend.UploadBundle(bundle)
   → ok → mirrorBundle 到 local_cache_dir/uploaded/ + 返回
   → !retryable → 立即返回失败
6. 全部重试失败 → 返回最后一次结果
```

### mirrorBundle 方法

- 将 bundle 序列化为 JSON 写入 `local_cache_dir/{uploaded|downloaded}/{skill_id}/{bundle_id}.json`
- kind 只允许 "uploaded" 或 "downloaded"，否则返回 error
- 写入失败只打 warning 不影响主流程

## ExperienceHubClient

**职责**：搜索 + 安装的高层封装，供 Rail 层直接调用。

### 结构

```go
type ExperienceHubClient struct {
    sharer *ExperienceSharer
    store  *checkpointing.EvolutionStore
}
```

### 方法

| 方法 | 签名 | 说明 |
|---|---|---|
| `SearchSkills` | `(ctx, query QueryKeywords, topK int) []SkillSearchResult` | 委托 sharer.SearchSkills |
| `InstallSkill` | `(ctx, skillID string, skillName string) (string, error)` | 下载技能包 → store.InstallSkillPackage 安装到本地 |

### InstallSkill 流程

```
1. sharer.DownloadSkillPackage(skillID) → packageBytes
2. 若空，返回 warning 日志 + error
3. sharer.GetSkillPackageMeta(skillID) → meta
4. 确定 targetName: skillName > meta.SkillName > ""
5. store.InstallSkillPackage(ctx, packageBytes, targetName) → 安装路径
6. 成功 → info 日志 + 返回安装路径
```

与 Python 的差异：Python 返回 `Optional[Path]`，Go 返回 `(string, error)`——安装路径以字符串返回，失败走 error。

## 实现顺序

9.80a 先行，9.24 P3+P5 后续接入。

sharing 内部实现顺序（自底向上）：

1. `types.go` — 数据类型，无外部依赖
2. `backend/interface.go` — 抽象接口
3. `backend/local_file.go` — 本地文件实现
4. `keyword_extractor.go` — 关键词提取
5. `experience_sharer.go` — 上传/下载门面
6. `share_stager.go` — QC + 入队
7. `hub_client.go` — 高层搜索/安装

每步实现后编写测试，确保覆盖率 ≥ 85%。

## 测试策略

对照 Python 测试 `tests/unit_tests/agent_evolving/sharing/test_sharing_module.py`：

1. **LocalFileBackend**：upload/download/去重/不可变技能包/different skill IDs 不冲突
2. **ExperienceSharer**：初始技能包上传一次/重复上传被拒绝/discard pending
3. **ShareStager**：execution_failure 无成功后续工具调用被丢弃/正常记录通过
4. **HubClient**：搜索+安装端到端
5. **KeywordExtractor**：ParseFromOptimizerOutput 从 patch 解析
6. **ensure_skill_id_in_content**：已有测试在 checkpointing 包

所有测试使用 `t.TempDir()` 创建临时目录，不依赖外部环境。
