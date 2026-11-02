# UapClaw 0.1.0

UapClaw 是一个 Agent 平台：主程序内嵌 Web 界面，解压后运行一条命令即可在浏览器里配置模型并开始对话。

## 一、运行

**Linux amd64**

```bash
chmod +x uapclaw          # 首次运行需要
./uapclaw app
```

**Windows amd64**

```bat
uapclaw.exe app
```

也可以直接双击 `start.bat`（等价于 `uapclaw.exe app`，窗口保持打开便于查看日志）。

启动成功后终端会打印：

```
  UapClaw 0.1.0 已启动
  界面地址: http://127.0.0.1:19000
  按 Ctrl+C 停止服务
```

浏览器会自动打开该地址；若未自动打开，手动访问 <http://127.0.0.1:19000> 即可。按 `Ctrl+C` 停止服务。

## 二、首次配置

1. 浏览器打开界面后，进入「配置」页；
2. 在模型配置中填写 **API Base**、**API Key**、**模型名**、**模型提供方**（配置页提供「校验」按钮，可直接发一次最小请求验证连通性）；
3. 保存后新建会话，即可开始对话。

配置保存在用户目录下的 `.uapclaw/`，无需重新解压。

## 三、常用参数

| 参数 | 说明 |
| --- | --- |
| `--port 19000` | 指定监听端口（默认 19000） |
| `--host 127.0.0.1` | 指定监听地址（默认 127.0.0.1；局域网访问用 `0.0.0.0`） |
| `--no-browser` | 启动后不自动打开浏览器（也可设环境变量 `UAPCLAW_NO_BROWSER=1`） |
| `--dotenv <path>` | 指定 .env 文件（多实例隔离） |
| `--name <name>` | 指定命名实例（多实例隔离，独立数据目录与端口） |
| `--dist <path>` | 使用外部前端产物目录（默认用内嵌界面） |

查看版本：`./uapclaw --version`（根命令，`app` 子命令不带该参数）。

示例：

```bash
./uapclaw app --port 19001 --host 0.0.0.0 --no-browser
```

## 四、数据与备份

| 平台 | 数据目录 |
| --- | --- |
| Linux | `~/.uapclaw/` |
| Windows | `%USERPROFILE%\.uapclaw\` |

该目录包含配置（`config/config.yaml`、`config/.env`）、会话数据库、Agent 工作区与加密密钥 `crypto.key`。

- 升级版本：保留 `~/.uapclaw/` 直接替换程序目录即可；
- 迁移机器：连同 `crypto.key` 一起复制，否则已加密的 API Key 无法解密；
- 彻底重置：删除 `~/.uapclaw/` 后重新启动。

## 五、目录说明

```
uapclaw            # 主程序（内嵌 Web 界面；Windows 为 uapclaw.exe）
start.bat          # Windows 快捷启动脚本
resources/         # 默认配置与 Agent 工作区模板（首次启动复制到数据目录）
third_party/
└── sqlite-vec/    # 向量检索扩展 vec0（记忆检索用；缺失时退化为内存余弦计算）
```

## 六、常见问题

| 现象 | 处理 |
| --- | --- |
| Linux 提示 `Permission denied` | `chmod +x uapclaw` |
| 提示端口被占用 | 换端口：`./uapclaw app --port 19001`，或设 `UAPCLAW_GATEWAY_PORT` |
| 浏览器没有自动打开 | 手动访问 <http://127.0.0.1:19000>；无桌面环境时加 `--no-browser` |
| 界面返回 404 / 白屏 | 确认启动命令为 `uapclaw app`（`app` 子命令才带 Web 界面） |
| 模型调用失败 | 到「配置」页检查 API Base / Key / 模型名，并用「校验」按钮验证 |

## 七、系统要求

- Linux：x86_64，glibc 2.17+（CentOS 7 / Ubuntu 16.04 及以上）
- Windows：Windows 10 / Server 2016 及以上（x86_64）
- 浏览器：Chrome / Edge / Firefox 最新版
