# SendFileToolkit 全量对齐 + D-23.8-5 回填 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** send_file.go 全量对齐上游 861 行版（信封/去重/静默/物化/target_channels/skill package/授权链），完整移植 verified_download_assets 资产账本，并回收 D-23.8-5 视频投递缝。

**Architecture:** 三段交付——①新包 `internal/swarm/common/verified_download_assets/`（密封拷贝+sidecar digest 账本）；②`send_file.go` 按 Python `send_file → snapshotRuntimeEnvelope → sendFileWithEnvelope` 两层结构重写；③`video_gen_tools.go` 投递缝改为对齐 Python `video_gen_tools.py:172-176` 的 ctx 现建 toolkit。enterprise 分支恒 False（偏差 D-x1）。

**Tech Stack:** Go 1.22 / archive+zip（skill 包探测）/ crypto/sha256（digest）/ sync.Mutex + goroutine sweeper（对齐 `_sweep_loop`）。

**Spec:** `docs/superpowers/specs/2026-11-16-sendfiletoolkit-full-alignment-design.md`

**Python 源锚点：** `jiuwenswarm-develop/jiuwenswarm/common/verified_download_assets.py`（409 行）、`jiuwenswarm-develop/jiuwenswarm/agents/harness/common/tools/send_file_to_user.py`（861 行）、`jiuwenswarm-develop/jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:14350-14384`。

**测试卫生（强制）：** 所有落盘测试用 `t.TempDir()`，禁止真实 `/tmp`（共享大目录曾致测试挂死）。

**每个任务完成标准：** 该任务测试绿 + `go build ./...` + `go vet ./...` 通过 + 提交。

---

### Task 1: verified_download_assets 包——核心类型与 stage/commit/revoke

**Files:**
- Create: `internal/swarm/common/verified_download_assets/verified_download_assets.go`
- Create: `internal/swarm/common/verified_download_assets/doc.go`
- Test: `internal/swarm/common/verified_download_assets/verified_download_assets_test.go`

对齐 Python `common/verified_download_assets.py`。逐符号行号锚点：`VerifiedDownloadAsset` L40、`VerifiedDownloadAssetOwner.__init__` L53、`stage` L74、`commit` L119、`revoke` L132、`is_active` L139、`prune` L177、`close` L223、`_ensure_root` L231、`_ensure_sweeper` L237、`_sweep_loop` L250、`_copy_verified_source` L258、`_write_sidecar_atomic` L297、`_read_sidecar` L327、`_payload_matches_asset` L336、`_sidecar_path` L348、`_safe_unlink` L354、`_normalize_asset_id` L363、`_normalize_digest` L374、`_regular_file_without_symlink` L386、`get_verified_download_asset_owner` L406。

- [ ] **Step 1: 写 doc.go（规则 1）**

```go
// Package verified_download_assets 提供授权 send_file 投递的密封资产账本。
//
// 授权模式（require_execution_authorization=True）下，send_file 在 push 前
// 把授权那一刻的文件字节密封拷贝到资产目录并写 sidecar（asset_id/digest/大小/过期），
// download_token 只指向密封副本；push 成功 commit（保留至 TTL），失败 revoke。
// 下载路由 serve 前校验 digest/过期/symlink，保证用户下载到的字节即授权字节。
//
// 文件目录：
//
//	verified_download_assets/
//	├── doc.go                     # 包文档
//	└── verified_download_assets.go # Asset/Owner 类型与账本逻辑
//
// 对应 Python 代码：jiuwenswarm/common/verified_download_assets.py
package verified_download_assets
```

- [ ] **Step 2: 写失败测试（核心路径 + 拒绝路径）**

测试函数清单（全用 `t.TempDir()`）：
- `TestOwnerStage_密封拷贝与sidecar`：stage 一个常规文件 → 返回 asset（`sealedPath` 存在、`sidecar` JSON 含 asset_id/digest/size/expires）；篡改 sealedPath 内容后 `Owner.is_active` 返回 false。
- `TestOwnerStage_拒绝symlink`：源为 symlink → stage 返回 error（对齐 `_regular_file_without_symlink`）。
- `TestOwnerCommitRevoke`：commit 后 asset 目录保留；revoke 后 sealedPath+sidecar 删除、重复 revoke 幂等。
- `TestOwnerPrune_过期清理`：构造 expiresAt 已过期的 asset → `prune(now)` 删除。
- `TestSweeper_后台清理`：close 时停 goroutine 不泄漏（`go vet`/race 下跑）。

```go
// 核心断言示例（TestOwnerStage_密封拷贝与sidecar 主体）
dir := t.TempDir()
owner := verifieddownloadassets.NewVerifiedDownloadAssetOwner(dir)
src := filepath.Join(t.TempDir(), "report.pdf")
os.WriteFile(src, []byte("payload-bytes"), 0o644)
asset, err := owner.Stage(context.Background(), src, "report.pdf", time.Now().Add(time.Minute))
require.NoError(t, err)
data, err := os.ReadFile(asset.SealedPath)
require.NoError(t, err)
require.Equal(t, "payload-bytes", string(data))
// sidecar digest 与文件一致
sidecar, err := verifieddownloadassets.ReadSidecar(asset.SealedPath + ".sidecar.json")
require.NoError(t, err)
sum := sha256.Sum256(data)
require.Equal(t, hex.EncodeToString(sum[:]), sidecar.Digest)
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/swarm/common/verified_download_assets/ -v`
Expected: FAIL（`undefined: verifieddownloadassets`）

- [ ] **Step 4: 实现核心类型（结构体/常量/导出/非导出分区，规范 2）**

骨架（字段名/签名按 Python 属性一一对应；方法内步骤按 Python 行号顺序，注释标行号）：

```go
// VerifiedDownloadAsset 单个密封资产（Python L40 dataclass）。
type VerifiedDownloadAsset struct {
	AssetID    string    // asset_id（Python L41）
	SourcePath string    // source_path（L42）
	SealedPath string    // sealed_path（L43）
	ExpiresAt  float64   // expires_at（L44）
}

// VerifiedDownloadAssetOwner 资产账本宿主（Python L50）。
type VerifiedDownloadAssetOwner struct {
	root        string        // asset_root（L54）
	ttlSeconds  float64       // 保留供 prune 兜底（对齐 _VERIFIED_ASSET_TTL_SECONDS 600 语义）
	mu          sync.Mutex
	closed      bool
	sweeperOnce sync.Once     // Python _ensure_sweeper L237：首个 stage 起后台 goroutine
	stopCh      chan struct{}
	sweepDone   chan struct{} // close 时等待退出（Python _sweep_loop 循环体结束即 goroutine 退出）
}

// NewVerifiedDownloadAssetOwner 构造（Python L53 __init__：root 落盘目录）。
// Stage（Python L74）：步骤 ①校验 _regular_file_without_symlink(L386)→
// ②asset_id=_normalize_asset_id(L363)→③sealed 目录 root/<asset_id[:2]>/ 建目录→
// ④_copy_verified_source(L258)（os.Rename 优先、跨设备回退 copy+fsync）→
// ⑤_write_sidecar_atomic(L297)（tmp 文件+rename，payload 含 asset_id/digest/size/expires_at）。
// Commit（L119）：幂等校验 is_active 后无操作（保留至 TTL）。
// Revoke（L132）：删 sealedPath+sidecar，_safe_unlink(L354) 忽略不存在。
// is_active（L139）：sidecar 存在 + _payload_matches_asset(L336)（digest/size/expired 三查）。
// Prune（L177）：扫 root 两层，sidecar 过期或 payload 不匹配 → 删。
// get_verified_download_asset_owner（L406）：进程级单例，root 取 _default_asset_root（L31，
// 环境变量资产目录，缺省 data 目录下 verified_assets/）。
```

细节裁决（实现前固定）：
- digest 用 sha256 hex（对齐 Python hashlib 用法，读 `_normalize_digest` L374 与 sidecar 写入段确认字段名后逐字对齐键名）。
- `_copy_verified_source` 的 fsync/跨设备回退逐行对齐 L258-296；Go 用 `os.Rename` 失败后 `io.Copy` + `f.Sync()`。
- Python 方法是 Owner 方法/模块函数归属不变：`_normalize_*`/`_regular_file_without_symlink`/`_write_all` 为包级非导出函数。

- [ ] **Step 5: 测试通过**

Run: `go test ./internal/swarm/common/verified_download_assets/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/common/verified_download_assets/
git commit -m "feat(verifiedassets): 资产账本 stage/commit/revoke/prune/sweeper——对齐 verified_download_assets.py 409 行"
```

### Task 2: web_file_download 契约修正 + 资产下载信息

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/web_file_download.go`
- Test: `internal/swarm/agents/harness/common/tools/web_file_download_test.go`

- [ ] **Step 1: 写失败测试**

- `TestBuildFileDownloadInfo_带UserID`：URL/token 里携带 user_id 维度（对齐 Python `build_file_download_info(..., user_id=envelope.user_id)`；先读 Python `web_file_download.py` 确认 user_id 落点——token payload 字段或 URL query，按 Python 逐字段对齐）。
- `TestBuildVerifiedAssetDownloadInfo`：给定 staged asset → 返回 path=sealed_path、name、size、mime_type、download_url、download_token（对齐 Python `build_verified_asset_download_info`；Python 锚点 `tools/web_file_download.py`，先 grep 定位行号再对齐）。
- **payload 删 `expires_at`**：新增断言 download info map 不含 `expires_at` 键（对齐新版 861 行 payload 结构，见 send_file_to_user.py L750-760）。

- [ ] **Step 2: 运行确认失败** — Expected: FAIL

- [ ] **Step 3: 实现**——`BuildFileDownloadInfo(filePath, baseName, sessionID string, userID string)`（签名改造，调用点 Task 4 同步）；新增 `BuildVerifiedAssetDownloadInfo(asset, baseName, sessionID, userID)`；删 `expires_at` 输出；网关 `/file-api/download` 下载处理段补资产校验（对齐 `file_download_token.py:229`：token 指向资产时 `is_active` 校验不过 → 拒绝）。注意守卫收窄：非资产 token 路径行为不变。

- [ ] **Step 4: 测试通过 + 全包回归**

Run: `go test ./internal/swarm/agents/harness/common/tools/ -run 'TestBuildFile|TestBuildVerified' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/web_file_download*.go
git commit -m "feat(webfiledl): user_id 维度+资产下载信息+payload 去.expires_at——对齐新版 web_file_download"
```

### Task 3: 会话级去重 + 静默守卫（send_file.go 第一批增补）

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/send_file.go`
- Test: `internal/swarm/agents/harness/common/tools/send_file_test.go`

- [ ] **Step 1: 写失败测试**

- `TestSendFile_会话去重`：同一 session 两次发同一路径 → 第二次返回「文件已在本次会话发送过，跳过重复投递：…」（对齐 L531-553）；不同 session 不去重；路径大小写/反斜杠归一（`_normalize_sent_file_path` L121：abspath + `\`→`/` + lower）。
- `TestClearSentFilesForSession`：clear 后可重发（对齐 L152-155）。
- `TestSendFile_静默守卫`：metadata 带 `skills_rebuild_silent` / `skills_create_from_knowledge_silent` → 返回静默提示串、不 push、不写 history（对齐 L379-390，提示串逐字复制）。

- [ ] **Step 2: 运行确认失败** — Expected: FAIL

- [ ] **Step 3: 实现**

```go
// 全局变量区新增（对齐 Python L109 模块级 dict；Go 加锁，Python 单线程无需锁）
var (
	sentFilePathsMu     sync.Mutex
	sentFilePathsBySession = map[string]map[string]struct{}{} // key: _sent_files_key → session id（tenant 恒空，D-x1）
)

// sentFilesKey 会话去重键（Python _sent_files_key L112；enterprise 分支不移植，D-x1）。
func sentFilesKey(sessionID string) string {
	if sid := strings.TrimSpace(sessionID); sid != "" {
		return sid
	}
	return "default"
}

// normalizeSentFilePath 归一（Python L121：abspath + 反斜杠转正斜杠 + lower）。
// partitionSentFiles/markFilesSent/clearSentFilesForSession 按 L125/L145/L152 一比一。
```

静默守卫插入 `SendFile` 步骤 1 之前（对齐 Python `send_file` 首段 L379）。

- [ ] **Step 4: 测试通过** - [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/send_file*.go
git commit -m "feat(sendfile): 会话级去重+静默守卫——对齐 send_file_to_user.py L109-155/L379-390"
```

### Task 4: 信封快照 + target_channels + 团队物化 + skill package（send_file.go 第二批）

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/send_file.go`
- Modify: `internal/swarm/agents/harness/common/tools/send_file_skill_package.go`（`looks_like_skill_package` 若体量大于 60 行可独立文件；否则并入 send_file.go——Python 为同文件模块级函数，**默认并入 send_file.go 保持一文件对一文件**）
- Test: `internal/swarm/agents/harness/common/tools/send_file_test.go`

- [ ] **Step 1: 写失败测试**

- `TestSendFile_targetChannels`：第二参数传 `["web","human-player-1"]` → push msg `metadata.send_file_targets` 为该列表（对齐 L794-795）；传非法类型 → normalize 后空（`NormalizeSendFileTargetChannels` permissions 包已有）。
- `TestSendFile_信封快照`：并发场景——`SendFile` 执行中另一 goroutine `UpdateRuntimeContext` 换 session → 投递仍用快照时的三元组（用可阻塞的 push 注入函数验证）。
- `TestSendFile_团队物化`：文件位于 `.agent_teams/<team>/team-workspace/...` 且构造传 project_dir → 交付前复制到 project 下相对路径，结果串含「最终交付文件已位于当前项目目录：」（L637-640）；目标已存在同内容 → 直接用目标；存在不同内容 → 返回错误串「发送文件失败：无法将团队交付文件写入当前项目目录」（L509-511）；目标逃逸 project root → 同错误（L322-326）；project_dir 为空 → 原路径直投。
- `TestSendFile_团队物化_projectDir回退`：构造不传 project_dir，session metadata 存 `project_dir` → `_resolve_project_dir` 回退生效（对齐 L240-264；Go 侧经 `sendFileHistoryFunc` 同族注入缝或既有 session metadata 访问器，**实现前先 grep Go 侧 session metadata 读取函数**，无则登记偏差走注入缝）。
- `TestLooksLikeSkillPackage`：.skill/.skill.zip 扩展名 → true；普通 zip 含根级或一层 SKILL.md → true；无 SKILL.md zip → false；损坏 zip → false 不 panic（对齐 L48-88）。
- `TestSendFile_结果串`：`成功发送 N 个文件` + copied_to_project/skipped/missing 三段顺序对齐 L636-648。

- [ ] **Step 2: 运行确认失败** — Expected: FAIL

- [ ] **Step 3: 实现（重写 send_file.go 主体）**

```go
// sendFileRuntimeEnvelope 运行时信封（Python _SendFileRuntimeEnvelope L91-103，九字段）。
type sendFileRuntimeEnvelope struct {
	routingRequestID              string
	sessionID                     string
	channelID                     string
	userID                        string
	metadata                      map[string]any // 深拷贝（D-x2：拷贝语义替代 MappingProxyType）
	projectDir                    string
	teamWorkspaceRoot             string
	requireExecutionAuthorization bool
	assetOwner                    *verifieddownloadassets.VerifiedDownloadAssetOwner // enterprise 分支不移植，D-x1
}

// snapshotRuntimeEnvelope 冻结可变宿主状态（Python L421-450；projectDir 解析含 session metadata 回退）。
// sendFileWithEnvelope 对齐 Python _send_file_with_envelope L452-657 步骤顺序：
// ①normalize target_channels → ②requested paths 归一（permissions.NormalizeSendFilePaths）→
// ③授权分支/普通分支分流 → ④团队物化 → ⑤全缺失早退 → ⑥会话去重分流 →
// ⑦（授权）stage 资产 → ⑧buildFilesPayload → ⑨buildPushMessage →
// ⑩push（commit 点：成功后 commit + markFilesSent；失败 revoke 未暴露资产）→
// ⑪history（失败 warn 不失败）→ ⑫结果串。
// SendFile 外壳对齐 Python send_file L358-419：静默守卫→快照→执行→except 兜底错误串→finally 清授权。
// buildFilesPayload 对齐 L683-774：enterprise 分支不移植（D-x1）；授权分支走 BuildVerifiedAssetDownloadInfo；
// 普通分支走 BuildFileDownloadInfo(带 userID)；downloadErr 整块回退基础模式（含 is_skill_package）。
// buildPushMessage 对齐 L777-798：metadata 合并 send_file_targets。
```

结构体字段增补：`SendFileToolkit` 增 `userID/projectDir/teamWorkspaceRoot/requireExecutionAuthorization/assetOwner`（对齐 `__init__` L161-197）；`NewSendFileToolkit`/`UpdateRuntimeContext` 增对应参数（UpdateRuntimeContext 的 `requireExecutionAuthorization` 为三态：nil 不改，对齐 L228-231——Go 用 `*bool`）。`GetTools` 增 `target_channels` 数组参数（schema L846-855 逐字复制 description），invokeFn 透传两参数。删 `sendValidFiles` 持锁迷你拷贝（被信封替代，规则 8）。`copySendFileMetadata` 升级为深拷贝（对齐 `copy.deepcopy`）。

- [ ] **Step 4: 测试通过** - [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/send_file*.go
git commit -m "feat(sendfile): 信封快照+target_channels+团队物化+skill包探测——对齐 send_file_to_user.py L48-798"
```

### Task 5: 授权路径全链（grant 消费 + 资产接线）

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/send_file.go`
- Test: `internal/swarm/agents/harness/common/tools/send_file_test.go`

- [ ] **Step 1: 写失败测试**

- `TestSendFile_授权路径`：`PublishSendFileExecutionGrant` 注入 grant（permissions 包已有）→ 构造 toolkit 传 `requireExecutionAuthorization=true` → valid_files 来自 `ConsumeSendFileExecutionGrant` 的授权项 resolved_path（对齐 L469-478）；不存在的授权路径 → missing 为空（授权即有效，L478）；资产 staged 且 push 成功后 commit（文件保留）；push 失败 → revoke（sealed 文件被删）+ 返回错误串。
- `TestSendFile_授权物化碰撞`：授权模式下物化后两路径归一相同 → 返回 `send_file_materialized_path_collision` 错误串（L517-523）。
- `TestSendFile_授权缺失资产宿主`：授权开启但 assetOwner 为 nil 且 `get_verified_download_asset_owner` 不可用 → 返回 `send_file_asset_owner_missing` 错误串（L574-575）。
- `TestSendFile_授权finally清grant`：执行后 `CurrentSendFileExecutionGrant(ctx)` 为 nil（L413-419）。

- [ ] **Step 2: 运行确认失败** - [ ] **Step 3: 实现**

按 Python L392-419（owns_execution_grant/envelope/finally）+ L469-478（consume→resolved_path）+ L563-657（owned_assets/exposure_started/commit/revoke 时机）接线。grant 消费与清理走 permissions 包既有 `PublishSendFileExecutionGrant`/`CurrentSendFileExecutionGrant`/`ConsumeSendFileExecutionGrant`/`ClearSendFileExecutionGrant`（ctx 传播，Go 用 ctx 携带替代 Python contextvars——既有 D 偏差沿用）。

- [ ] **Step 4: 测试通过** - [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/send_file*.go
git commit -m "feat(sendfile): 授权路径全链——grant 消费/资产密封/commit-revoke 时序/collision 守卫，对齐 L392-657"
```

### Task 6: 注册点参数传播（adapter + team_manager）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go`（`NewSendFileToolkit` 两处调用 L626/L644）
- Modify: `internal/swarm/server/adapter/deep_adapter.go:329-331`（注释更新）
- Modify: `internal/swarm/agents/harness/team/team_manager_spec.go:396`
- Test: 相关既有测试适配（grep `NewSendFileToolkit(` 全部调用方）

- [ ] **Step 1: 盘点调用方** — `grep -rn "NewSendFileToolkit(\|UpdateRuntimeContext(" internal/ tests/`，列全清单（含测试构造 `newSendFileToolkitForTest`）。

- [ ] **Step 2: 写失败测试** — deep_adapter 侧：`_enable_auto_permission` 对应判定（`IsAutoPermissionEnabled`，Python `require_send_authorization = self._enable_auto_permission`，interface_deep.py:14355）为 true 时构造的 toolkit `requireExecutionAuthorization` 为 true；`user_id` 取 `_CRON_TOOL_USER_ID.get()` 对应的 Go ctx 携带值（先 grep Go 侧 cron ctx 携带实现，对齐字段来源）。

- [ ] **Step 3: 实现** — 两处构造传新参数（userID/projectDir=`d._project_dir` 对应 Go 字段/requireExecutionAuthorization）；team_manager_spec.go 步骤 f 同步；`deep_adapter.go:414` 注释改写为「SetVideoGenSendFileFunc 已删：videoGenSendFile 经 ctx 现建 toolkit（Task 7）」。

- [ ] **Step 4: 测试通过** - [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/ internal/swarm/agents/harness/team/
git commit -m "feat(adapter): send_file 注册点传 user_id/project_dir/require_execution_authorization——对齐 interface_deep.py:14350-14384"
```

### Task 7: D-23.8-5 回收——视频投递缝改现建 toolkit

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/video_gen_tools.go`（L151-152 缝定义、L817-829 投递段）
- Test: `internal/swarm/agents/harness/common/tools/video_gen_tools_test.go`

- [ ] **Step 1: 写失败测试** — 盯单完成路径断言收到真实 `chat.file` push（files payload 含视频 path/name/is_skill_package）而非 fallback 文本通知；toolkit 构造三元组来自 ctx（request_id/session_id/channel_id，对齐 Python `context.request_id` L175）。

- [ ] **Step 2: 运行确认失败** - [ ] **Step 3: 实现**

```go
// videoGenSendFile 完成视频投递（对齐 Python video_gen_tools.py L168-190 _deliver 分支）：
// 投递时按 ctx 三元组现建临时 toolkit（Python L172-176 同构，无注入缝、无生命周期管理），
// 调 SendFile(ctx, localPath)；SendFile 恒不返回 error（异常兜底错误串），
// 与 Python「send_file never raises」注释一致——失败走既有文本通知分支。
func videoGenSendFile(ctx context.Context, localPath string) error {
	toolkit := NewSendFileToolkit(ctxRequestID(ctx), ctxSessionID(ctx), ctxChannelID(ctx), ctxMetadata(ctx))
	_, _ = toolkit.SendFile(ctx, localPath)
	return nil
}
```

删除：`SetVideoGenSendFileFunc` 导出桩、`deep_adapter_config.go` 对应注入调用、D-23.8-5 相关注释（video_gen_tools.go L25/L126/L152/L829）；fallback 分支保留（Python L183 失败同路文本通知）。ctx 取值函数沿用 23.8 既有 ctx 携带（grep `ctxRequestID` 等，无则用 adapter 侧既有三元组 ctx 键）。

- [ ] **Step 4: 测试通过** - [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/video_gen*.go internal/swarm/server/adapter/
git commit -m "fix(videogen): D-23.8-5 回收——投递缝改 ctx 现建 SendFileToolkit，删除 SetVideoGenSendFileFunc 桩"
```

### Task 8: 集成测试 + 台账同步

**Files:**
- Modify: `tests/integration/`（multimodal video itest + 权限授权 send_file 全链新例；**每个 _test.go 自带 suite.Run**）
- Modify: `IMPLEMENTATION_PLAN.md`（23.8 行 D-23.8-5 勘误关闭、10.6.24b 行追加记录）
- Modify: `docs/superpowers/specs/2026-11-28-domain23.8-multimodal-content-design.md`（D-23.8-5 勘误注记）

- [ ] **Step 1: multimodal_video_itest 断言升级** — 「视频完成 → 真实 chat.file 投递」（含 is_skill_package=false 字段）替换 fallback 文本通知断言。

- [ ] **Step 2: 新增授权 send_file 集成例** — rail 发 grant → LLM 工具消费 → 资产密封 → 下载 token 校验（自检：调用跨度 ≥3 子系统，过 `go run ./cmd/integration-audit -verify`）。

- [ ] **Step 3: 台账同步** — IMPLEMENTATION_PLAN.md：23.8 行「D-23.8-5 ⤴️」改为已回收注记；10.6.24b 行追加「2026-11-16 全量对齐批」。23.8 spec §9 的 D-23.8-5 加勘误注记（未移植判断错误，实际 10.6.24b 已落地，本批回收）。

- [ ] **Step 4: 全量门禁**

```bash
go build ./... && go vet ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
go run ./cmd/integration-audit -verify
```

Expected: 全绿。

- [ ] **Step 5: Commit + 推送**

```bash
git add -A
git commit -m "test(itest): 视频投递真链+授权 send_file 全链；docs: D-23.8-5 勘误关闭+台账同步"
git push
```

---

## 删除清单（规则 8 验收锚点）

| 锚点 | 验收 |
|---|---|
| `SetVideoGenSendFileFunc` | grep 全仓 0 命中 |
| `sendValidFiles` | 0 命中（被信封结构替代） |
| `webFileDownloadDefaultExpiresSeconds` 的 expires_at 输出 | payload map 无该键 |
| `D-23.8-5` 登记 | spec/PLAN 中改为勘误注记，不再作为待办 |
| `videoGenSendFile` nil 缝注释 | video_gen_tools.go 三处注释改写 |

## 偏差登记（spec §3，实施时写入代码注释与 spec）

- D-x1 enterprise 恒 False（三处分支 + tenant 去重维度）
- D-x2 信封 metadata 深拷贝替代 MappingProxyType
- D-x3 push/history 启动接线缝沿用（既有偏差），push 语义改返回值检查
