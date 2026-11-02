## UapClaw v0.1.1

修复版：解决**首次打开界面就弹「获取历史记录失败：参数无效或会话不存在」**的问题。功能与 v0.1.0 一致。

### 修复了什么

首次启动时，Web 端尚未产生任何会话，浏览器会先采纳 `connection.ack` 下发的新 `session_id` 并请求历史，服务端正确地回了一个「历史不存在」的 `chat.error`。

前端本来专门把这句话当**正常态静默忽略**（`useWebSocket.ts`：`includes('invalid page_idx or session history not found')` 时直接 return，因为新会话没有 `history.json` 属正常）。但移植时这句**协议约定串**被一起本地化成了中文，白名单失配，于是它被当成真错误渲染成了红色系统消息。

- `chat.error` 文案恢复为与 Python 逐字一致的英文协议串（`invalid page_idx or session history not found`）
- 新增回归用例**断言字面英文**（而不是引用常量），防止以后再被翻译破坏契约
- 该用例之外的业务逻辑零改动

### 下载与运行

| 平台 | 文件 | 系统要求 |
| --- | --- | --- |
| Linux x86_64 | `uapclaw-0.1.1-linux-amd64.tar.gz` | glibc 2.17+（CentOS 7 / Ubuntu 16.04 及以上） |
| Windows x64 | `uapclaw-0.1.1-windows-amd64.zip` | Windows 10 / Server 2016 及以上 |

```bash
# Linux
tar -xzf uapclaw-0.1.1-linux-amd64.tar.gz && cd uapclaw-0.1.1-linux-amd64
chmod +x uapclaw && ./uapclaw app
```

Windows 解压后双击 `start.bat`（或 `uapclaw.exe app`）。启动后自动打开 `http://127.0.0.1:19000`。

升级方式：保留原有数据目录（Linux `~/.uapclaw/`，Windows `%USERPROFILE%\.uapclaw\`），直接替换程序目录即可，配置与历史都在。

### 验证

- 单测：`TestHandleHistoryGetStream_错误文案是英文协议串` 等历史相关用例全绿
- 端到端（Linux 包）：真实 WS 连接 → `history.get` 不存在的会话 → 收到 `chat.error`，`error` 字段为英文原文；`connection.ack`、`history.message` 等其余帧不变
- 已知限制同 v0.1.0：`chat` / `serve` 子命令仍为占位实现，Web 界面是当前唯一完整入口
