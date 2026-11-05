# Web WS RPC 下游缺口清单

> 由 `go run ./cmd/rpc-matrix` 生成，勿手工编辑。
> 本清单只列**网关层补齐也不能真正工作**的方法：它们在上游是薄代理，但本仓缺 req method 常量、AgentServer 处理器或底层子系统。

## 一、有 req method 常量、AgentServer 无处理器（2 个）

| 方法 | 下游说明 | 上游注册 |
|---|---|---|
| `harness.export` | 代理 HARNESS_PACKAGES_EXPORT（有常量，AgentServer 无分支） | channel_manager/web/app_web_handlers.py:6567 |
| `harness.import` | 代理 HARNESS_PACKAGES_IMPORT（有常量，AgentServer 无分支） | channel_manager/web/app_web_handlers.py:6555 |

## 二、既无常量也无处理器（29 个）

| 方法 | 下游说明 | 上游注册 |
|---|---|---|
| `document.formats` | 代理 DOCUMENT_FORMATS（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5814 |
| `document.persist` | 代理 DOCUMENT_PERSIST（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5813 |
| `file.upload_chunk` | 代理 FILE_UPLOAD_CHUNK（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5802 |
| `media.discard` | 代理 MEDIA_DISCARD（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5812 |
| `media.persist` | 代理 MEDIA_PERSIST（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5811 |
| `path.select_directory` | 代理 PATH_SELECT_DIRECTORY（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5732 |
| `path.select_files` | 代理 PATH_SELECT_FILES（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5733 |
| `project.create` | 代理目标 req method 未识别 | channel_manager/web/app_web_handlers.py:5705 |
| `project.get_cron_sessions` | 代理 PROJECT_GET_CRON_SESSIONS（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5704 |
| `project.get_sessions` | 代理 PROJECT_GET_SESSIONS（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5703 |
| `project.info` | 代理 PROJECT_INFO（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5702 |
| `project.list` | 代理 PROJECT_LIST（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5701 |
| `project.pin` | 代理 PROJECT_PIN（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5707 |
| `project.pinned_sessions` | 代理 PROJECT_PINNED_SESSIONS（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5716 |
| `project.rename` | 代理 PROJECT_RENAME（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5706 |
| `project.restore` | 代理目标 req method 未识别 | channel_manager/web/app_web_handlers.py:5709 |
| `prompt_suggestion.event` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5828 |
| `prompt_suggestion.get` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5829 |
| `session.get_metadata` | 代理 SESSION_GET_METADATA（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5690 |
| `session.message.continue_queued` | 代理 SESSION_MESSAGE_CONTINUE_QUEUED（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5691 |
| `session.pin` | 代理 SESSION_PIN（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5694 |
| `topic_starters.control` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5822 |
| `topic_starters.event` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5827 |
| `topic_starters.foreground` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5826 |
| `topic_starters.get` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5825 |
| `topic_starters.profile` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5820 |
| `topic_starters.restore_direction` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5821 |
| `topic_starters.select_model` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5824 |
| `topic_starters.status` | 代理 RECOMMENDATION（本仓无该 req method） | channel_manager/web/app_web_handlers.py:5823 |

## 三、依赖未迁移子系统（17 个）

| 方法 | 下游说明 | 上游注册 |
|---|---|---|
| `heartbeat.job.cancel` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6205 |
| `heartbeat.job.create` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6199 |
| `heartbeat.job.delete` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6201 |
| `heartbeat.job.get` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6198 |
| `heartbeat.job.list` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6196 |
| `heartbeat.job.meta` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6197 |
| `heartbeat.job.preview` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6203 |
| `heartbeat.job.run_now` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6204 |
| `heartbeat.job.toggle` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6202 |
| `heartbeat.job.update` | 缺 heartbeat job store | channel_manager/web/app_web_handlers.py:6200 |
| `openai_account.auth.logout` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5685 |
| `openai_account.auth.pending_login` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5683 |
| `openai_account.auth.poll_login` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5684 |
| `openai_account.auth.start_login` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5682 |
| `openai_account.auth.status` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5681 |
| `openai_account.models.list` | 23.6 OpenAI 账号 OAuth | channel_manager/web/app_web_handlers.py:5686 |
| `task.asr.transcribe` | ASR 子系统 | channel_manager/web/app_web_handlers.py:5680 |

