## UapClaw v0.1.2

修复版，解决两个「首次安装、尚未配置模型」时会踩到的问题。功能与 v0.1.1 一致。

### 修复 1：未配置模型时不再真连外网

此前 `hasValidModelConfig` 只判断「模型名是否在缓存里」，`resources/config.yaml` 自带的占位配置（`api_base: https://example.com/compatible-mode/v1`）会被判定为有效，于是：

- 心跳每 60 秒真的向 `example.com` 发一次请求，日志里报的是底层网络错误（`url.Error ... connection forcibly closed`），而不是「模型未正确配置」；
- 未设 `HEARTBEAT_TIMEOUT` 时该请求可能长时间挂着（实测 150s 无响应）；
- 首次对话同样要等网络超时才拿到错误。

现在对齐 Python `_has_valid_model_config` / `_mcc_looks_usable`：**api_key 非空 && api_base 非空 && api_base 不在占位符黑名单**，判定顺序为「指定模型 → 默认模型 → 缓存中任意可用模型 → 无效」，未配置时立即返回 `模型未正确配置，请先配置模型信息`，不发任何出站请求。

### 修复 2：history.json 不再把 `< > &` 转义成 `\u003c`

Python 用 `json.dumps(..., ensure_ascii=False, indent=2)`，尖括号原样落盘；Go 侧 `json.MarshalIndent` 默认做 HTML 转义，导致 `</heartbeat_user_task>` 在文件里变成 `\u003c/heartbeat_user_task\u003e`，人工查看与 `grep` 排查都会踩坑。已改用仓库既有的 `utils.MarshalIndentNoEscape`，与 Python 逐字节语义一致。

### 升级方式

保留数据目录（Linux `~/.uapclaw/`，Windows `%USERPROFILE%\.uapclaw\`），覆盖程序目录即可。启动后 `uapclaw --version` 应显示 `0.1.2`。

### 仍未处理（已知问题）

- **心跳会话目录仍会每分钟新增一个** `~/.uapclaw/agent/sessions/heartbeat_*`（每个含 `metadata.json` + `history.json`），Go 与 Python 目前都是这个设计，两边都没有清理逻辑。v0.1.2 只保证不再是「空 `chat.final` + 真实外网请求」，目录增长本身待后续版本处理。临时缓解：`HEARTBEAT_INTERVAL=3600` 或 config.yaml `heartbeat.every: 3600`。
- `chat` / `serve` 子命令仍为占位实现，Web 界面是当前唯一完整入口。
