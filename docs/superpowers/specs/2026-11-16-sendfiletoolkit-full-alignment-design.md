# SendFileToolkit 全量对齐 + D-23.8-5 回填 设计

日期：2026-11-16
状态：已批准（用户裁决：范围方案 B / 资产账方案案 B / enterprise 方案 A / 回填形态方案 A）

## 1. 背景与事实核实

- Go 现有 `internal/swarm/agents/harness/common/tools/send_file.go`（355 行）对齐的是**旧版** `send_file_to_user.py`（255 行，10.6.24b 批次）；上游文件已演进到 **861 行**，存在大面积能力缺口。
- 23.8 偏差 **D-23.8-5 是过期登记**：spec `2026-11-28-domain23.8` 写「Go 侧 SendFileToolkit 未移植」，实际 10.6.24b 早两个月落地。真正缺口只是视频投递接线（`videoGenSendFile` 注入缝为 nil）。
- Go 已完整移植 auto-permission（`rails/permissions/auto_permission/` + adapter `IsAutoPermissionEnabled` 链），故授权路径是活代码，资产账本必须完整移植。
- 前端已迁移新版协议（`frontend/src/utils/skillPackageFile.ts` 消费 `is_skill_package`；`fileDownloadDedup.ts`），Go 后端 payload 缺字段是真实契约缺口。

### SendFileToolkit 在 Agent 会话中的流程位置

1. **注册期**：`interface_deep.py:14362` / Go `deep_adapter_config.go` + `team_manager_spec.go` 按 `send_file_allowed` 三态开关创建 toolkit，注册 `send_file_to_user` 工具；`_enable_auto_permission` 决定 `require_execution_authorization`。
2. **投递期**：LLM 调工具 → 参数解析 → 授权消费（授权模式）/ 文件存在性分流 → 团队交付物物化 → 会话级去重 → payload 构造 → push（commit 点）→ history → 结果串。
3. **视频挂点**：`video_gen_tools.py:172-176` 盯单 goroutine 完成后按 ctx 三元组**现建临时 toolkit** 调 `send_file`；失败降级文本通知。

## 2. 范围（三项，均已裁决）

### 2.1 D-23.8-5 回收（回填形态：方案 A，对齐 Python 现建）

- `video_gen_tools.go`：`videoGenSendFile` 注入缝删除，改为投递时按 ctx（request_id/session_id/channel_id）调 `NewSendFileToolkit(...)` 现场构造再 `SendFile`（对齐 Python 无注入缝的结构）。
- 删除：`SetVideoGenSendFileFunc` 桩、`deep_adapter.go:414`「SetVideoGenSendFileFunc 不注入」注释、D-23.8-5 偏差登记（spec + IMPLEMENTATION_PLAN.md 23.8 行勘误）。
- 集成测试：`multimodal_video_itest` 断言从 fallback 文本通知改为真实 chat.file 投递。

### 2.2 send_file.go 全量对齐 861 行版

结构一比一：`send_file` → `snapshotRuntimeEnvelope` → `sendFileWithEnvelope` 两层；`buildFilesPayload` / `buildPushMessage` 助手归属与 Python 静态方法一致。九项能力：

| # | 能力 | Python 锚点 |
|---|---|---|
| 1 | `target_channels` 参数：工具 schema 第二参数 + `NormalizeSendFileTargetChannels`（permissions 包已有）+ push metadata `send_file_targets` 合并 | L349/591/794 |
| 2 | 会话级去重：`_SENT_FILE_PATHS_BY_SESSION` + `_partition_sent_files`/`_mark_files_sent`/`clear_sent_files_for_session`；key 对齐 `_sent_files_key`（tenant 恒空 → session id 单值） | L109-155 |
| 3 | 静默守卫：metadata `skills_rebuild_silent`/`skills_create_from_knowledge_silent` 拦截，返回提示串不投递 | L379-390 |
| 4 | 团队交付物物化：`_resolve_project_dir`（session metadata 回退解析）/ `_infer_team_workspace_root`（`.agent_teams/*/team-workspace` 识别）/ `_materialize_team_deliverable*`（相对路径复制 + project root escape 守卫 + 存在冲突拒绝） | L240-346 |
| 5 | `looks_like_skill_package` + payload `is_skill_package`（.skill/.skill.zip 扩展名；zip 中央目录根级或一层 SKILL.md） | L48-88 |
| 6 | `sendFileRuntimeEnvelope` 快照：九字段（routing_request_id/session_id/channel_id/user_id/metadata/project_dir/team_workspace_root/require_execution_authorization/asset_owner），第一次可挂起点前冻结，后段全读信封 | L91-103/421-450 |
| 7 | 授权路径：构造/update 增 `require_execution_authorization` 等参数；`ConsumeSendFileExecutionGrant`（permissions 包已有 API）+ finally `ClearSendFileExecutionGrant`；授权模式下 valid_files 来自授权项、collision 检测（`send_file_materialized_path_collision`） | L392-419/468-523 |
| 8 | 授权资产链：stage（密封拷贝+sidecar）→ push → commit（保留至 TTL）/ revoke（push 失败且未 exposure）+ TTL 600s（`_VERIFIED_ASSET_TTL_SECONDS`） | L563-657 |
| 9 | 语义修正：payload 删 `expires_at`；`BuildFileDownloadInfo` 增 `user_id` 参数；history 失败 warn 不失败（Python L628-635）；push 返回 False → RuntimeError 语义（Go 返回 error → 错误串）；`get_tools` description 增 target_channels 文案；结果串增 `copied_to_project`/`skipped_files` 段 | L566-649/800-861 |

### 2.3 新模块 verified_download_assets（资产账方案案 B：完整移植）

`jiuwenswarm/common/verified_download_assets.py`（409 行）→ `internal/swarm/common/verified_download_assets/`：

- `VerifiedDownloadAsset` / `VerifiedDownloadAssetOwner`：stage/commit/revoke/is_active/prune/close。
- sweeper goroutine 对齐 `_sweep_loop`（后台定期清过期资产）；sidecar 原子写（`_write_sidecar_atomic`）/ digest 校验（`_payload_matches_asset`）/ symlink 守卫（`_regular_file_without_symlink`）/ asset_id 与 digest 归一。
- 下载路由侧校验挂接：`web_file_download.go` 增 `build_verified_asset_download_info` 对齐 `file_download_token.py:229` 的 serve 前校验（digest 一致 + 未过期 + 非 symlink 常规文件）。
- tools 包的 `verified_download_assets.py`（15 行）仅为 re-export，Go 侧由包导入路径天然覆盖，不另建文件。

## 3. 已知偏差（登记项）

- **D-x1 enterprise 恒 False**：Python `is_enterprise()` 三处分支不移植——envelope 快照期 asset_owner 选择分支、`_build_files_payload` 的 `obs_file_delivery` 对象存储投递分支、授权资产跳过分支；`_sent_files_key` tenant 维度恒空。理由：Go 无 enterprise 域；移植即不可达代码。回收条件：Go 引入 enterprise 域。
- **D-x2 MappingProxyType → 拷贝语义**：信封 metadata 用深拷贝 + 不可变约定（Go 无 readonly map），只读性靠约定而非类型强制。
- **D-x3 push/history 注入缝沿用**：保留 `SetSendFilePushFunc`/`SetSendFileHistoryFunc` 启动接线（Python 为 lazy import 单例，Go swarmtools→server 编译期成环，既有偏差）；push 语义改为对齐 `send_runtime_push` 返回值检查。

## 4. 测试与验证

- **单测**：`send_file_test.go` 扩齐九项能力分支（target_channels/去重/静默/物化/skill package/envelope 快照/授权消费/资产 stage-commit-revoke/payload 字段）；verified_download_assets 独立包测试（stage/commit/revoke/prune/sweeper/sidecar 篡改拒绝/symlink 拒绝，全部 `t.TempDir()` 落盘——**禁止真实 /tmp**）。
- **集成**：`multimodal_video_itest` 补「视频完成 → 真实 chat.file 投递」断言；权限授权路径 send_file 全链一例（rail 发 grant → 工具消费 → 资产密封 → 下载校验）。
- **门禁**：`go build ./...` + `go vet ./...` + 全量 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` + `go run ./cmd/integration-audit -verify`。
- **台账同步**：IMPLEMENTATION_PLAN.md 23.8 行 D-23.8-5 勘误关闭；10.6.24b 行追加对齐批记录；本批产出与偏差写入 spec。

## 5. 回填/删除清单

| 动作 | 对象 |
|---|---|
| 删 | `videoGenSendFile` nil 注入缝 + fallback 分支旧注释（video_gen_tools.go:817/829 附近） |
| 删 | `SetVideoGenSendFileFunc`（若存在导出桩） |
| 删 | `deep_adapter.go:414`「SetVideoGenSendFileFunc 不注入」注释 |
| 删 | D-23.8-5 偏差登记（spec 2026-11-28-domain23.8 §9 + IMPLEMENTATION_PLAN.md 23.8 行） |
| 删 | 旧 payload `expires_at` 字段 + 旧 downloadInfoBuilder 注入点的 expires 语义（按新 payload 结构） |
| 删 | `sendValidFiles` 持锁迷你拷贝（被 envelope 快照替代） |
| 改 | `BuildFileDownloadInfo` 增 `user_id`（web_file_download.go） |
| 增 | `internal/swarm/common/verified_download_assets/` 新包 + doc.go |
