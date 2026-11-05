# Web WS RPC 契约矩阵

> 由 `go run ./cmd/rpc-matrix` 生成，勿手工编辑。

- 方法总数：196（✅ 一致 79 / ⚠️ 漂移 0 / ❌ 缺 109 / ❓ 前端独有 8）
- 缺口下游分布：local 18 / agent:ok 7 / agent:const-only 2 / agent:missing 65 / dep 17

覆盖状态：`一致` 上游有本仓也有；`漂移` 本仓有但上游没有（自造或上游已删）；`缺` 上游有本仓无；`前端独有` 前端在调但两侧都没注册（前端独占能力或上游未实现）。

下游状态含义：`local` 网关本地可实现；`agent:ok` 代理且本仓已有处理器；`agent:const-only` 有常量无处理器；`agent:missing` 常量与处理器皆无；`dep` 依赖未迁移子系统。

| 方法 | 覆盖 | 下游 | 下游说明 | 前端引用 | 本仓注册 | 上游注册 |
|---|---|---|---|---|---|---|
| `a4p.authorization.complete` | ❌ 缺 | agent:missing | 代理 A4P_AUTHORIZATION_COMPLETE（本仓无该 req method） | — | — | app_web_handlers.py:6377 |
| `a4p.authorization.pending` | ❌ 缺 | agent:missing | 代理 A4P_AUTHORIZATION_PENDING（本仓无该 req method） | — | — | app_web_handlers.py:6379 |
| `a4p.authorization.reject` | ❌ 缺 | agent:missing | 代理 A4P_AUTHORIZATION_REJECT（本仓无该 req method） | — | — | app_web_handlers.py:6378 |
| `a4p.authorization.reprepare` | ❌ 缺 | agent:missing | 代理 A4P_AUTHORIZATION_REPREPARE（本仓无该 req method） | — | — | app_web_handlers.py:6376 |
| `a4p.config.get` | ❌ 缺 | agent:missing | 代理 A4P_CONFIG_GET（本仓无该 req method） | — | — | app_web_handlers.py:6374 |
| `a4p.config.update` | ❌ 缺 | agent:missing | 代理 A4P_CONFIG_UPDATE（本仓无该 req method） | — | — | app_web_handlers.py:6375 |
| `channel.dingtalk.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3406 | app_web_handlers.py:5850 |
| `channel.dingtalk.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3407 | app_web_handlers.py:5851 |
| `channel.discord.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3410 | app_web_handlers.py:5854 |
| `channel.discord.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3411 | app_web_handlers.py:5855 |
| `channel.feishu.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3400 | app_web_handlers.py:5844 |
| `channel.feishu.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3401 | app_web_handlers.py:5845 |
| `channel.get` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:271 | app_web_handlers.py:5679 |
| `channel.slack.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3412 | app_web_handlers.py:5856 |
| `channel.slack.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3413 | app_web_handlers.py:5857 |
| `channel.telegram.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3404 | app_web_handlers.py:5848 |
| `channel.telegram.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3405 | app_web_handlers.py:5849 |
| `channel.wechat.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3416 | app_web_handlers.py:5860 |
| `channel.wechat.get_login_ui` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:368 | app_web_handlers.py:5862 |
| `channel.wechat.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3417 | app_web_handlers.py:5861 |
| `channel.wechat.unbind` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:369 | app_web_handlers.py:5863 |
| `channel.wecom.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3414 | app_web_handlers.py:5858 |
| `channel.wecom.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3415 | app_web_handlers.py:5859 |
| `channel.whatsapp.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3408 | app_web_handlers.py:5852 |
| `channel.whatsapp.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3409 | app_web_handlers.py:5853 |
| `channel.xiaoyi.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3402 | app_web_handlers.py:5846 |
| `channel.xiaoyi.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:3403 | app_web_handlers.py:5847 |
| `chat.interrupt` | ✅ 一致 | local | 网关本地实现 | src/features/designer/components/DesignerChatPanel.tsx:267 | gateway/channel_manager/tui/tui_connect.go:263<br>gateway/channel_manager/web/app_web_handlers.go:291 | app_web_handlers.py:5816 |
| `chat.resume` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/tui/tui_connect.go:262<br>gateway/channel_manager/web/app_web_handlers.go:290 | app_web_handlers.py:5815 |
| `chat.send` | ✅ 一致 | local | 网关本地实现 | src/features/designer/components/DesignerChatPanel.tsx:282<br>src/features/designer/designerEntry.ts:114<br>src/hooks/useWebSocket.ts:2303 | gateway/channel_manager/tui/tui_connect.go:261<br>gateway/channel_manager/web/app_web_handlers.go:289 | app_web_handlers.py:5803 |
| `chat.swarmflow_reply` | ✅ 一致 | local | 网关本地实现 | src/hooks/useWebSocket.ts:2780 | gateway/channel_manager/web/app_web_handlers.go:340 | app_web_handlers.py:5818 |
| `chat.user_answer` | ✅ 一致 | local | 网关本地实现 | src/hooks/useWebSocket.ts:2926 | gateway/channel_manager/tui/tui_connect.go:264<br>gateway/channel_manager/web/app_web_handlers.go:292 | app_web_handlers.py:5817 |
| `commands.list` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:338 | app_web_handlers.py:5775 |
| `config.get` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | — | gateway/channel_manager/web/app_web_handlers.go:268 | app_web_handlers.py:5638 |
| `config.moa_status` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:339 | app_web_handlers.py:5637 |
| `config.save_all` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | src/features/settings/modules/models/FreeModelSettingsDialog.tsx:73<br>src/features/settings/services/useSettingsConfig.ts:73 | gateway/channel_manager/web/app_web_handlers.go:279 | app_web_handlers.py:5640 |
| `config.set` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | src/App.tsx:4218<br>src/App.tsx:4233 | gateway/channel_manager/web/app_web_handlers.go:269 | app_web_handlers.py:5639 |
| `config.validate_model` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | src/features/settings/modules/models/ModelDialog.tsx:279<br>src/features/settings/modules/models/ModelsSettings.tsx:479 | gateway/channel_manager/web/app_web_handlers.go:280 | app_web_handlers.py:5641 |
| `connection.status` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:337 | app_web_handlers.py:5676 |
| `cron.job.create` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:378<br>gateway/channel_manager/web/app_web_handlers.go:389 | app_web_handlers.py:5867 |
| `cron.job.delete` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:380<br>gateway/channel_manager/web/app_web_handlers.go:391 | app_web_handlers.py:5869 |
| `cron.job.get` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:377 | app_web_handlers.py:5866 |
| `cron.job.list` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:376<br>gateway/channel_manager/web/app_web_handlers.go:388 | app_web_handlers.py:5864 |
| `cron.job.meta` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:384<br>gateway/channel_manager/web/app_web_handlers.go:392 | app_web_handlers.py:5865 |
| `cron.job.preview` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:382 | app_web_handlers.py:5871 |
| `cron.job.run_now` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:383 | app_web_handlers.py:5872 |
| `cron.job.toggle` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:381 | app_web_handlers.py:5870 |
| `cron.job.update` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:379<br>gateway/channel_manager/web/app_web_handlers.go:390 | app_web_handlers.py:5868 |
| `document.formats` | ❌ 缺 | agent:missing | 代理 DOCUMENT_FORMATS（本仓无该 req method） | — | — | app_web_handlers.py:5814 |
| `document.persist` | ❌ 缺 | agent:missing | 代理 DOCUMENT_PERSIST（本仓无该 req method） | — | — | app_web_handlers.py:5813 |
| `external_cli.codex_install_status` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5499 |
| `external_cli.custom_agent.remove` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5502 |
| `external_cli.detect` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5496 |
| `external_cli.install` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5501 |
| `external_cli.install_status` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5500 |
| `external_cli.prune_path_histories` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5498 |
| `external_cli.validate_paths` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5497 |
| `file.upload_chunk` | ❌ 缺 | agent:missing | 代理 FILE_UPLOAD_CHUNK（本仓无该 req method） | — | — | app_web_handlers.py:5802 |
| `harness.activate` | ❌ 缺 | agent:ok | 代理 HARNESS_PACKAGES_ACTIVATE | — | — | app_web_handlers.py:6474 |
| `harness.deactivate` | ❌ 缺 | agent:ok | 代理 HARNESS_PACKAGES_DEACTIVATE | — | — | app_web_handlers.py:6475 |
| `harness.delete` | ❌ 缺 | agent:ok | 代理 HARNESS_PACKAGES_DELETE | — | — | app_web_handlers.py:6476 |
| `harness.export` | ❌ 缺 | agent:const-only | 代理 HARNESS_PACKAGES_EXPORT（有常量，AgentServer 无分支） | — | — | app_web_handlers.py:6567 |
| `harness.import` | ❌ 缺 | agent:const-only | 代理 HARNESS_PACKAGES_IMPORT（有常量，AgentServer 无分支） | — | — | app_web_handlers.py:6555 |
| `harness.packages` | ❌ 缺 | agent:ok | 代理 HARNESS_PACKAGES_GET | — | — | app_web_handlers.py:6472 |
| `harness.packages.scan` | ❌ 缺 | agent:ok | 代理 HARNESS_PACKAGES_SCAN | — | — | app_web_handlers.py:6473 |
| `health_check.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:309<br>gateway/channel_manager/web/app_web_handlers.go:318 | app_web_handlers.py:5839 |
| `health_check.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:310<br>gateway/channel_manager/web/app_web_handlers.go:319 | app_web_handlers.py:5840 |
| `heartbeat.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:311<br>gateway/channel_manager/web/app_web_handlers.go:320 | app_web_handlers.py:5842 |
| `heartbeat.job.cancel` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6205 |
| `heartbeat.job.create` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6199 |
| `heartbeat.job.delete` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6201 |
| `heartbeat.job.get` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6198 |
| `heartbeat.job.list` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6196 |
| `heartbeat.job.meta` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6197 |
| `heartbeat.job.preview` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6203 |
| `heartbeat.job.run_now` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6204 |
| `heartbeat.job.toggle` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6202 |
| `heartbeat.job.update` | ❌ 缺 | dep | 缺 heartbeat job store | — | — | app_web_handlers.py:6200 |
| `heartbeat.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:312<br>gateway/channel_manager/web/app_web_handlers.go:321 | app_web_handlers.py:5843 |
| `history.get` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/tui/tui_connect.go:265<br>gateway/channel_manager/web/app_web_handlers.go:276 | app_web_handlers.py:5819 |
| `hooks.list` | ✅ 一致 | agent:ok | 代理 HOOKS_LIST | — | gateway/channel_manager/web/app_web_handlers.go:343 | app_web_handlers.py:5772 |
| `locale.get_conf` | ✅ 一致 | agent:missing | 代理 LOCALE_GET_CONF（本仓无该 req method） | — | gateway/channel_manager/web/app_web_handlers.go:303 | app_web_handlers.py:5830 |
| `locale.set_conf` | ✅ 一致 | agent:missing | 代理 LOCALE_SET_CONF（本仓无该 req method） | src/features/settings/services/SettingsSourceProvider.tsx:249 | gateway/channel_manager/web/app_web_handlers.go:304 | app_web_handlers.py:5831 |
| `media.discard` | ❌ 缺 | agent:missing | 代理 MEDIA_DISCARD（本仓无该 req method） | src/services/obsUpload.ts:69 | — | app_web_handlers.py:5812 |
| `media.persist` | ❌ 缺 | agent:missing | 代理 MEDIA_PERSIST（本仓无该 req method） | — | — | app_web_handlers.py:5811 |
| `memory.compute` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:300 | app_web_handlers.py:5771 |
| `memory.forbidden.get` | ✅ 一致 | agent:missing | 代理 MEMORY_FORBIDDEN_GET（本仓无该 req method） | — | gateway/channel_manager/web/app_web_handlers.go:362 | app_web_handlers.py:6428 |
| `memory.forbidden.set` | ✅ 一致 | agent:missing | 代理 MEMORY_FORBIDDEN_SET（本仓无该 req method） | — | gateway/channel_manager/web/app_web_handlers.go:363 | app_web_handlers.py:6429 |
| `model_groups.delete` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5657 |
| `model_groups.list` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5653 |
| `model_groups.upsert` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5655 |
| `models.delete` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | src/features/settings/modules/models/ModelsSettings.tsx:415 | — | app_web_handlers.py:5649 |
| `models.get` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | src/features/settings/modules/models/ModelsSettings.tsx:300 | — | app_web_handlers.py:5643 |
| `models.list` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | src/features/settings/modules/models/FreeModelSettingsDialog.tsx:74<br>src/features/settings/modules/models/ModelsSettings.tsx:217<br>src/features/settings/modules/models/ModelsSettings.tsx:331 | gateway/channel_manager/web/app_web_handlers.go:270 | app_web_handlers.py:5642 |
| `models.references` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5651 |
| `models.replace_all` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | — | gateway/channel_manager/web/app_web_handlers.go:283 | app_web_handlers.py:5645 |
| `models.upsert` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5647 |
| `models.validate` | ✅ 一致 | local | config 代理族（单用户走本地配置读写） | — | gateway/channel_manager/web/app_web_handlers.go:284 | app_web_handlers.py:5646 |
| `openai_account.auth.logout` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5685 |
| `openai_account.auth.pending_login` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5683 |
| `openai_account.auth.poll_login` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5684 |
| `openai_account.auth.start_login` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5682 |
| `openai_account.auth.status` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5681 |
| `openai_account.models.list` | ❌ 缺 | dep | 23.6 OpenAI 账号 OAuth | — | — | app_web_handlers.py:5686 |
| `path.get` | ✅ 一致 | agent:const-only | 代理 PATH_GET（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:295 | app_web_handlers.py:5730 |
| `path.select_directory` | ❌ 缺 | agent:missing | 代理 PATH_SELECT_DIRECTORY（本仓无该 req method） | — | — | app_web_handlers.py:5732 |
| `path.select_file` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5734 |
| `path.select_files` | ❌ 缺 | agent:missing | 代理 PATH_SELECT_FILES（本仓无该 req method） | — | — | app_web_handlers.py:5733 |
| `path.set` | ✅ 一致 | agent:const-only | 代理 PATH_GET（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:296 | app_web_handlers.py:5731 |
| `permissions.approval_overrides.delete` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_APPROVAL_OVERRIDES_DELETE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:359 | app_web_handlers.py:6276 |
| `permissions.approval_overrides.get` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_APPROVAL_OVERRIDES_GET（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:358 | app_web_handlers.py:6275 |
| `permissions.owner_scopes.get` | ✅ 一致 | agent:missing | 代理 PERMISSIONS_OWNER_SCOPES_GET（本仓无该 req method） | — | gateway/channel_manager/web/app_web_handlers.go:348 | app_web_handlers.py:6234 |
| `permissions.owner_scopes.set` | ✅ 一致 | agent:missing | 代理 PERMISSIONS_OWNER_SCOPES_SET（本仓无该 req method） | — | gateway/channel_manager/web/app_web_handlers.go:349 | app_web_handlers.py:6235 |
| `permissions.rules.create` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_RULES_CREATE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:355 | app_web_handlers.py:6272 |
| `permissions.rules.delete` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_RULES_DELETE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:357 | app_web_handlers.py:6274 |
| `permissions.rules.get` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_RULES_GET（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:354 | app_web_handlers.py:6271 |
| `permissions.rules.update` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_RULES_UPDATE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:356 | app_web_handlers.py:6273 |
| `permissions.tools.delete` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_TOOLS_DELETE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:353 | app_web_handlers.py:6270 |
| `permissions.tools.get` | ✅ 一致 | agent:ok | 代理 PERMISSIONS_TOOLS_GET | — | gateway/channel_manager/web/app_web_handlers.go:350 | app_web_handlers.py:6266 |
| `permissions.tools.set` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_TOOLS_SET（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:351 | app_web_handlers.py:6267 |
| `permissions.tools.update` | ✅ 一致 | agent:const-only | 代理 PERMISSIONS_TOOLS_UPDATE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:352 | app_web_handlers.py:6268 |
| `permissions.tools.update_user` | ❌ 缺 | agent:missing | 代理 PERMISSIONS_TOOLS_UPDATE_USER（本仓无该 req method） | — | — | app_web_handlers.py:6269 |
| `proactive.feedback` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/components/ChatPanel/ProactiveRecommendationCard.tsx:93 | — | — |
| `project.create` | ❌ 缺 | agent:missing | 代理目标 req method 未识别 | — | — | app_web_handlers.py:5705 |
| `project.get_cron_sessions` | ❌ 缺 | agent:missing | 代理 PROJECT_GET_CRON_SESSIONS（本仓无该 req method） | — | — | app_web_handlers.py:5704 |
| `project.get_sessions` | ❌ 缺 | agent:missing | 代理 PROJECT_GET_SESSIONS（本仓无该 req method） | — | — | app_web_handlers.py:5703 |
| `project.git.commit` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_COMMIT（本仓无该 req method） | — | — | app_web_handlers.py:5724 |
| `project.git.create_branch` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_CREATE_BRANCH（本仓无该 req method） | — | — | app_web_handlers.py:5723 |
| `project.git.diff_status` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_DIFF_STATUS（本仓无该 req method） | — | — | app_web_handlers.py:5726 |
| `project.git.diff_unwatch` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/features/code-mode/useCodeGitDiffWatch.ts:149<br>src/features/code-mode/useCodeGitDiffWatch.ts:248<br>src/features/code-mode/useCodeGitDiffWatch.ts:301 | — | — |
| `project.git.init` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_INIT（本仓无该 req method） | — | — | app_web_handlers.py:5721 |
| `project.git.probe` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_PROBE（本仓无该 req method） | — | — | app_web_handlers.py:5720 |
| `project.git.push` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_PUSH（本仓无该 req method） | — | — | app_web_handlers.py:5725 |
| `project.git.status` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_STATUS（本仓无该 req method） | — | — | app_web_handlers.py:5719 |
| `project.git.switch_branch` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_SWITCH_BRANCH（本仓无该 req method） | — | — | app_web_handlers.py:5722 |
| `project.git.turn_diff` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_TURN_DIFF（本仓无该 req method） | — | — | app_web_handlers.py:5728 |
| `project.git.turn_diff_list` | ❌ 缺 | agent:missing | 代理 PROJECT_GIT_TURN_DIFF_LIST（本仓无该 req method） | — | — | app_web_handlers.py:5727 |
| `project.info` | ❌ 缺 | agent:missing | 代理 PROJECT_INFO（本仓无该 req method） | — | — | app_web_handlers.py:5702 |
| `project.list` | ❌ 缺 | agent:missing | 代理 PROJECT_LIST（本仓无该 req method） | — | — | app_web_handlers.py:5701 |
| `project.pin` | ❌ 缺 | agent:missing | 代理 PROJECT_PIN（本仓无该 req method） | — | — | app_web_handlers.py:5707 |
| `project.pinned_sessions` | ❌ 缺 | agent:missing | 代理 PROJECT_PINNED_SESSIONS（本仓无该 req method） | — | — | app_web_handlers.py:5716 |
| `project.remove` | ❌ 缺 | agent:missing | 代理目标 req method 未识别 | — | — | app_web_handlers.py:5708 |
| `project.rename` | ❌ 缺 | agent:missing | 代理 PROJECT_RENAME（本仓无该 req method） | — | — | app_web_handlers.py:5706 |
| `project.restore` | ❌ 缺 | agent:missing | 代理目标 req method 未识别 | — | — | app_web_handlers.py:5709 |
| `prompt_suggestion.event` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5828 |
| `prompt_suggestion.get` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5829 |
| `rsi.artifact.download` | ❌ 缺 | agent:missing | 代理 RSI_ARTIFACT_DOWNLOAD（本仓无该 req method） | — | — | app_web_handlers.py:6492 |
| `rsi.artifact.files.get` | ❌ 缺 | agent:missing | 代理 RSI_ARTIFACT_FILES_GET（本仓无该 req method） | — | — | app_web_handlers.py:6494 |
| `rsi.artifact.files.list` | ❌ 缺 | agent:missing | 代理 RSI_ARTIFACT_FILES_LIST（本仓无该 req method） | — | — | app_web_handlers.py:6493 |
| `rsi.dataset.validate` | ❌ 缺 | agent:missing | 代理 RSI_DATASET_VALIDATE（本仓无该 req method） | — | — | app_web_handlers.py:6481 |
| `rsi.harness.install` | ❌ 缺 | agent:missing | 代理 RSI_HARNESS_INSTALL（本仓无该 req method） | — | — | app_web_handlers.py:6496 |
| `rsi.harness.rollback` | ❌ 缺 | agent:missing | 代理 RSI_HARNESS_ROLLBACK（本仓无该 req method） | — | — | app_web_handlers.py:6498 |
| `rsi.harness.versions.list` | ❌ 缺 | agent:missing | 代理 RSI_HARNESS_VERSIONS_LIST（本仓无该 req method） | — | — | app_web_handlers.py:6497 |
| `rsi.report.get` | ❌ 缺 | agent:missing | 代理 RSI_REPORT_GET（本仓无该 req method） | — | — | app_web_handlers.py:6490 |
| `rsi.task.create` | ❌ 缺 | agent:missing | 代理 RSI_TASK_CREATE（本仓无该 req method） | — | — | app_web_handlers.py:6482 |
| `rsi.task.delete` | ❌ 缺 | agent:missing | 代理 RSI_TASK_DELETE（本仓无该 req method） | — | — | app_web_handlers.py:6485 |
| `rsi.task.get` | ❌ 缺 | agent:missing | 代理 RSI_TASK_GET（本仓无该 req method） | — | — | app_web_handlers.py:6484 |
| `rsi.task.list` | ❌ 缺 | agent:missing | 代理 RSI_TASK_LIST（本仓无该 req method） | — | — | app_web_handlers.py:6483 |
| `rsi.training.pause` | ❌ 缺 | agent:missing | 代理 RSI_TRAINING_PAUSE（本仓无该 req method） | — | — | app_web_handlers.py:6487 |
| `rsi.training.resume` | ❌ 缺 | agent:missing | 代理 RSI_TRAINING_RESUME（本仓无该 req method） | — | — | app_web_handlers.py:6488 |
| `rsi.training.start` | ❌ 缺 | agent:missing | 代理 RSI_TRAINING_START（本仓无该 req method） | — | — | app_web_handlers.py:6486 |
| `rsi.training.terminate` | ❌ 缺 | agent:missing | 代理 RSI_TRAINING_TERMINATE（本仓无该 req method） | — | — | app_web_handlers.py:6489 |
| `rsi.tree.get` | ❌ 缺 | agent:missing | 代理 RSI_TREE_GET（本仓无该 req method） | — | — | app_web_handlers.py:6495 |
| `rsi.usage.get` | ❌ 缺 | agent:missing | 代理 RSI_USAGE_GET（本仓无该 req method） | — | — | app_web_handlers.py:6491 |
| `sample.organization.get` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | tests/fixtures/settings-extension/ExtensionOrganizationSettings.tsx:41 | — | — |
| `sample.organization.update` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | tests/fixtures/settings-extension/ExtensionOrganizationSettings.tsx:65 | — | — |
| `session.create` | ✅ 一致 | agent:const-only | 代理 SESSION_CREATE（有常量，AgentServer 无分支） | — | gateway/channel_manager/web/app_web_handlers.go:273 | app_web_handlers.py:5689 |
| `session.get_metadata` | ❌ 缺 | agent:missing | 代理 SESSION_GET_METADATA（本仓无该 req method） | — | — | app_web_handlers.py:5690 |
| `session.list` | ✅ 一致 | agent:ok | 代理 SESSION_LIST | — | gateway/channel_manager/web/app_web_handlers.go:272 | app_web_handlers.py:5688 |
| `session.message.continue_queued` | ❌ 缺 | agent:missing | 代理 SESSION_MESSAGE_CONTINUE_QUEUED（本仓无该 req method） | src/App.tsx:2096 | — | app_web_handlers.py:5691 |
| `session.pin` | ❌ 缺 | agent:missing | 代理 SESSION_PIN（本仓无该 req method） | — | — | app_web_handlers.py:5694 |
| `session.plan_status` | ❌ 缺 | agent:ok | 代理 SESSION_PLAN_STATUS | — | — | app_web_handlers.py:5692 |
| `session.rename` | ❌ 缺 | agent:ok | 代理 SESSION_RENAME | — | — | app_web_handlers.py:5693 |
| `session.selection.set` | ❌ 缺 | local | config 代理族（单用户走本地配置读写） | — | — | app_web_handlers.py:5659 |
| `session.switch` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/App.tsx:3875 | — | — |
| `skills.graph.build` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/components/SkillPanel/symphonyGraphAction.ts:71 | — | — |
| `skills.graph.cancel` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/components/SkillPanel/symphonyGraphAction.ts:78 | — | — |
| `task.asr.transcribe` | ❌ 缺 | dep | ASR 子系统 | — | — | app_web_handlers.py:5680 |
| `topic_starters.control` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5822 |
| `topic_starters.event` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5827 |
| `topic_starters.foreground` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5826 |
| `topic_starters.get` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5825 |
| `topic_starters.profile` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5820 |
| `topic_starters.restore_direction` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5821 |
| `topic_starters.select_model` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | src/features/settings/modules/general/TopicModelSettings.tsx:81<br>src/features/settings/modules/general/TopicModelSettings.tsx:108 | — | app_web_handlers.py:5824 |
| `topic_starters.status` | ❌ 缺 | agent:missing | 代理 RECOMMENDATION（本仓无该 req method） | — | — | app_web_handlers.py:5823 |
| `updater.check` | ✅ 一致 | local | 网关本地实现 | src/App.tsx:2170 | gateway/channel_manager/web/app_web_handlers.go:326 | app_web_handlers.py:5833 |
| `updater.download` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:327 | app_web_handlers.py:5834 |
| `updater.get_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:330 | app_web_handlers.py:5836 |
| `updater.get_status` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:328 | app_web_handlers.py:5832 |
| `updater.reset_source` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:332 | app_web_handlers.py:5837 |
| `updater.set_conf` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:331 | app_web_handlers.py:5838 |
| `updater.upgrade` | ✅ 一致 | local | 网关本地实现 | — | gateway/channel_manager/web/app_web_handlers.go:333 | app_web_handlers.py:5835 |
| `vendors.fetch_models` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5678 |
| `vendors.list` | ❌ 缺 | local | 网关本地实现 | — | — | app_web_handlers.py:5677 |
| `video.search.answer` | ❓ 前端独有 | local | 非上游方法（本仓自造/漂移） | src/hooks/useWebSocket.ts:2824 | — | — |
