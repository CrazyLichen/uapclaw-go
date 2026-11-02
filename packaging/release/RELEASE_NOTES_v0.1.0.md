## UapClaw v0.1.0

首个公开发布版本：**下载解压 → 运行一条命令 → 浏览器里配置模型并开始对话**。

### 下载

| 平台 | 文件 | 系统要求 |
| --- | --- | --- |
| Linux x86_64 | `uapclaw-0.1.0-linux-amd64.tar.gz` | glibc 2.17+（CentOS 7 / Ubuntu 16.04 及以上） |
| Windows x64 | `uapclaw-0.1.0-windows-amd64.zip` | Windows 10 / Server 2016 及以上 |

`sha256sums.txt` 提供两个产物的校验和。

### 快速开始

**Linux**

```bash
tar -xzf uapclaw-0.1.0-linux-amd64.tar.gz
cd uapclaw-0.1.0-linux-amd64
chmod +x uapclaw
./uapclaw app
```

**Windows**

解压后双击 `start.bat`（等价于 `uapclaw.exe app`），或在命令行执行 `uapclaw.exe app`。

启动后终端打印界面地址并自动打开浏览器（`--no-browser` 可关闭）：

```
  UapClaw 0.1.0 已启动
  界面地址: http://127.0.0.1:19000
  按 Ctrl+C 停止服务
```

首次使用：在界面「配置」页填写 **API Base / API Key / 模型名**（配置页自带连通性校验），保存后新建会话即可对话。配置存放于 `~/.uapclaw/`（Windows 为 `%USERPROFILE%\.uapclaw\`），升级版本时保留该目录即可。

### 本版包含

- **单文件交付**：Web 前端产物经 `go:embed` 打进二进制，无需另装 Node 或部署静态资源
- **完整链路**：进程内 Gateway + AgentServer，WebSocket 通道、文件 API、配置热重载、Cron 调度、心跳
- **持久化**：SQLite 会话与检查点，含 FTS5 全文检索
- **向量记忆**：随包提供 `sqlite-vec` 扩展（`linux-amd64/vec0.so`、`windows-amd64/vec0.dll`，v0.1.9），缺失时自动退化为内存余弦计算
- **多实例隔离**：`--name` / `--dotenv` 为每个实例分配独立数据目录与端口
- **Windows 支持**：进程组/进程树终止、文件锁等平台绑定代码按 build tag 拆分，并新增 Windows 版 vec0 扩展

### 已知限制

- 0.1.0 为开发者预览：CLI 的 `chat` / `serve` 子命令仍为占位实现，**Web 界面是当前唯一完整入口**
- 仅构建 linux-amd64 与 windows-amd64（macOS 需自行编译，仓库已带 darwin 的 vec0 扩展）
- Windows 产物为 Linux 上交叉编译，已通过 wine 启动冒烟（HTTP、WebSocket、静态资源、SQLite 建库均正常），尚未在真实 Windows 主机上做长时间运行验证，欢迎反馈
