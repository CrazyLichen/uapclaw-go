# 10.5.7 + 10.5.8 扩展系统 — ExtensionLoader/ExtensionManager go-plugin 完整对齐设计（方案 D-②-B）

> 状态：待评审
> 日期：2026-10-21
> 范围：IMPLEMENTATION_PLAN.md 10.5.7（ExtensionLoader）+ 10.5.8（ExtensionManager），
> 附带回填（B 决策）：Gateway before_chat_request 触发点（forward_loop.go 步骤9，~30 行，
> 归属 11.3 既有逻辑而非 11.13）+ 事件写回闭环（uapclaw.go 4 处 / handle_envelope.go 1 处的
> FromMap 缺口修复）+ cmd 接线（runAppCmd ⤵️ 10.5.8 注释块放开 + transport 选择回填）。
> 10.5.10 CryptoUtility 保持 ⤵️ 不动；11.13 gateway/hooks 用户 hook 系统不动。
> Python 参考：jiuwenswarm/extensions/loader.py · manager.py · sdk/ · hooks_context.py

## 一、决策记录（与用户讨论确认）

| 决策点 | 选择 | 理由 |
|---|---|---|
| 加载机制 | **D. hashicorp go-plugin 子进程 + gRPC** | 跨平台、真动态、进程隔离；grpc/protobuf 已是直接依赖，仅新增 go-plugin + yamux + oklog/run |
| 协议范围 | **② 生命周期 + 事件桥 + Transport 代理** | 钩子消费点已就绪可端到端验证；Transport 代理落地"扩展替换 AgentServerClient"旗舰场景；不碰 config 解密（10.5.10 留 ⤵️） |
| 章节顺序 | **B. 10.5.7+10.5.8 + 顺带回填触发点** | Gateway before_chat_request 在 Python 是 MessageHandler(11.3) 既有逻辑，Go 误挂 TODO(#11.13)；回填后事件桥两侧可验证 |

**已核实的事实基线**（探索结论）：

1. **Python loader 目前空转**：全仓无任何扩展实现 `register_extensions`，默认
   `extension_dirs: jiuwenswarm/extensions` 下也无符合特征的子目录。loader 是为未来第三方
   扩展准备的纯框架，Go 侧无兼容包袱。
2. **Python host 从不调 `BaseExtension.initialize()`**：loader/manager 只走
   register_extensions → set_extension_dir → shutdown 三步。Go 协议**不做 Initialize RPC**。
3. **YuanrongFrontendAgentClient 是死代码**：定义于 extensions/yuanrong_frontend_client.py，
   无任何 import/实例化点；config.yaml `gateway.agent_client.type: yuanrong` 无读取代码。
   Go 不移植该客户端本身，只打通"扩展自声明 transport 提供者"机制。
4. **`AgentClient` 只依赖 `transport.AgentTransport`**（Send/Recv/Close 纯 byte 管道）——
   gRPC 代理实现该接口后直接注入 `routing.NewAgentClient(...)`，AgentClient/receiverLoop
   零改动（规则 6 Transport 架构的设计意图）。
5. **config↔extensions 循环依赖**：Python manager.py import get_config 读 extension_dirs，
   而 common/config.py 反向用 ExtensionRegistry.get_crypto_provider()（函数级 import 混过去）。
   Go 解法：Manager 不 import config，由 cmd 启动层读配置经 Functional Options 注入。
6. **memCtx 写回缺口（现有 bug）**：uapclaw.go 4 处 `Trigger(ctx, event, memCtx.ToMap())`
   传新 map 后从结构体字段读结果——进程内扩展改 map 写不回结构体，跨进程返回更无处落地。
   Python 传 dataclass 原地改、宿主读同一对象，语义闭环。不修则事件桥无意义。
7. 工具链已就绪：protoc 3.21.12 + protoc-gen-go v1.36.10 + protoc-gen-go-grpc 1.5.1；
   go-plugin v1.8.0 已入 go.mod 且全量编译通过。

## 二、Python 侧语义基线（对齐映射表）

| Python（loader.py / manager.py） | Go 对应 | 偏差说明 |
|---|---|---|
| `MANIFEST_FILENAME = "extension.yaml"` / `ENTRY_FILENAME = "extension.py"` | `extension.yaml` / `ext-plugin`（可执行二进制） | 入口形态差异（方案 D 本质） |
| `add_search_path(path)`（存在才加） | `AddSearchPath(path)` 同语义 | — |
| `discover_extension_roots()`（一级子目录，manifest **或** entry 存在即根） | `DiscoverExtensionRoots()` 同语义 | — |
| `load_extension(root)`：manifest 解析（缺省 `{}`）→ 装依赖 → importlib → `register_extensions(registry)` → `set_extension_dir(root)` → 返回注册结果 | `LoadExtension(ctx, root)`：manifest 解析 → 启动插件子进程 → Register RPC → 逐声明注册 → SetExtensionDir RPC | 依赖安装删除（Go 静态编译）；registry 无法跨进程传，改为"声明返回+宿主代注册" |
| `_install_dependencies`（uv/pip，120s 超时，失败仅日志） | **删除**；dependencies 仅元数据 | go-plugin 子进程自带依赖 |
| `manager._setup_search_paths`（get_config 读 `extensions.extension_dirs`，';'分割，相对→resolve 绝对） | cmd 层读 config → `NewExtensionManager(registry, WithSearchPaths(...))` | 解 config↔extensions 环 |
| `load_all_extensions`：逐个 load；单个失败 error 日志继续；loaded 记入 `_loaded_extensions` | `LoadAllExtensions(ctx)` 同语义 | — |
| `shutdown_all_extensions`：逐个 shutdown（hasattr 检查），失败仅 warn，最后 clear | `ShutdownAllExtensions(ctx)`：Shutdown RPC + client.Kill() | — |
| `list_extensions()` → [{id,name,version}] | `ListExtensions() []map[string]string` 同构（保留现有 stub 签名） | — |
| `register_extensions(registry)` 返回单实例或列表 | Register RPC 返回 `repeated ExtensionDecl` | 列表语义对齐 |
| 返回对象 `hasattr(ext, "set_extension_dir")` 才设目录 | 所有声明统一 SetExtensionDir RPC（Go 接口必有该方法） | 接口必然具备 |
| `hasattr(module, "register_extensions")` 缺失 → registered=None | 插件必须实现 Register（协议必备）→ Go 返回空声明列表等价 | — |

## 三、gRPC 插件协议（extensions/plugin/proto/extension.proto）

```protobuf
syntax = "proto3";
package extension.v1;

service Extension {
  // 注册声明：返回扩展元数据 + 事件订阅 + 能力声明（对齐 register_extensions）
  rpc Register(RegisterRequest) returns (RegisterResponse);
  // 设置扩展目录（对齐 set_extension_dir，宿主在 Register 后逐声明调用）
  rpc SetExtensionDir(SetDirRequest) returns (SetDirResponse);
  // 元数据查询（对齐 BaseExtension.metadata property）
  rpc GetMetadata(GetMetadataRequest) returns (GetMetadataResponse);
  // 关闭（对齐 shutdown）
  rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);
  // 事件桥：宿主 Trigger → 插件原地修改 context → 返回完整 map → 宿主写回
  rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
  // Transport 代理三件套（对齐 AgentTransport 接口）
  rpc TransportSend(TransportSendRequest) returns (TransportSendResponse);
  rpc TransportRecv(TransportRecvRequest) returns (stream TransportRecvResponse);
  rpc TransportClose(TransportCloseRequest) returns (TransportCloseResponse);
}
```

要点：

- **无 Initialize RPC**——事实基线 2（host 从不调 initialize，扩展在 Register 内自初始化）。
- `RegisterResponse`：`repeated ExtensionDecl decls`；每声明 =
  `ExtensionMetadataProto`（id/name/version/description/author/min_version/dependencies/config_schema）
  + `repeated EventSubscription`（event 字符串 + priority，priority 默认 100）
  + `bool provides_transport`（AgentServerClient 能力声明）。
- `HandleEventResponse.context` 为 `google.protobuf.Struct`（map[string]any 往返），
  插件返回**修改后的完整 context**，宿主侧 bridge 负责写回原 data map。
- 事件名直接用 Go 常量字符串（`"gateway:before_chat_request"` 等，与 Python
  HookEventBase.get_event 格式一致），不另设枚举。
- proto 生成代码提交入库（`extension.pb.go` + `extension_grpc.pb.go`），仓库不依赖
  protoc 可用性；Makefile 提供 `proto-gen` 目标用于再生成。

## 四、宿主侧包结构（依赖方向单向，无环）

```
internal/swarm/extensions/
├── doc.go                    # 更新：加 plugin/ 与 FromMap 说明
├── types.go                  # 不变
├── hook_event.go             # 不变
├── hooks_context.go          # +4 个 FromMap(m) 方法（ToMap 逆操作，防御式类型断言）
├── registry.go               # 不变（消费 GetInstanceErr 的现有代码不动）
├── loader.go                 # 重写：go-plugin Client 封装 + 发现/加载编排
├── manager.go                # 重写：WithSearchPaths + LoadAll/ShutdownAll/List
├── callback_compat.go        # 不变
└── plugin/                   # 新子包（extensions → plugin 单向依赖）
    ├── doc.go
    ├── proto/
    │   ├── extension.proto
    │   ├── extension.pb.go   # 生成
    │   └── extension_grpc.pb.go  # 生成
    ├── host.go               # 握手常量(ProtocolVersion=1+MagicCookie) + PluginClient 封装
    │                         #   Register/SetExtensionDir/GetMetadata/Shutdown/HandleEvent
    ├── transport_proxy.go    # GRPCPluginTransport 实现 transport.AgentTransport
    └── serve.go              # 插件作者 SDK：plugin.ServeExtension(impl)
                              #   ExtensionImpl 接口 = Metadata/Register/Shutdown/HandleEvent/Transport*
```

依赖方向：`extensions` → `extensions/plugin` → `transport`。
`extensions/plugin` **不 import extensions**（proto 自带类型；loader 做类型转换，
ExtensionMetadata ↔ ExtensionMetadataProto 在 loader.go 内互转）。
`extensions` 保持不依赖 config（环由 cmd 层注入解开）。

## 五、宿主侧注册流程（LoadExtension 内部）

```
LoadExtension(ctx, root):
  1. manifest 解析（findManifest：root/extension.yaml，缺省空 map——对齐 _load_manifest_dict）
  2. pluginClient = pluginHost.Load(root)          # 启动 ext-plugin 子进程 + gRPC 握手
  3. resp = pluginClient.Register(ctx)             # 声明列表
  4. for each decl in resp.Decls:
       a. SetExtensionDir(ctx, root)               # 对齐 Python 逐 ext set_extension_dir
       b. for each sub in decl.EventSubs:
            registry.Register(sub.Event, bridgeFn(pluginClient, sub.Event), sub.Priority)
            # bridgeFn = HandleEvent RPC → 写回 data map（见第六节）
       c. if decl.ProvidesTransport:
            ext := &pluginTransportExtension{client: pluginClient}
            registry.RegisterAgentServerClient(ext)
            # pluginTransportExtension 实现 extensions.AgentServerClientExtension：
            #   GetClient() → &GRPCPluginTransport{client}
            #   Metadata()/SetExtensionDir/Initialize/Shutdown 代理到 RPC
  5. 返回加载结果（供 Manager 记入 loaded 列表 + 日志 "[ExtensionManager] 加载 %s"）
```

失败语义（对齐 Python）：第 2/3 步失败返回 error，由 Manager 记 error 日志后**继续下一个
root**；单个 root 失败不影响整体。

## 六、事件桥写回闭环（修复现有缺口）

### 6.1 bridgeFn 形态

```go
func (l *ExtensionLoader) makeEventBridge(pc *plugin.PluginClient, event string) callback.CustomCallbackFunc {
    return func(ctx context.Context, data map[string]any) any {
        if data == nil { return nil }
        result, err := pc.HandleEvent(ctx, event, data)
        if err != nil { /* Error 日志（对齐 Python 异常路径规则 3.4），返回原 data */ return nil }
        if result != nil { mergeMaps(data, result) }  // 原地写回：宿主调用方从 data 读结果
        return nil
    }
}
```

关键点：**写回传入的 data map 本身**（原地），这样现有消费点
`memCtx.ToMap()` 产生的 map 与调用方持有的引用是同一个——配合 6.2 的 FromMap 即闭环。

### 6.2 消费点修复（5 处现有 + 1 处新增）

| 位置 | 修复内容 |
|---|---|
| `uapclaw.go` L206/354（MEMORY_BEFORE_CHAT ×2） | Trigger 后加 `memCtx.FromMap(data)`；同时修两处既有偏差：①join 过滤空块（对齐 Python `join(b for b in blocks if b)`）②`inputs["memory_block"]` 无条件写入（Go 当前是空串跳过，Python 无条件赋值） |
| `uapclaw.go` L267/500（MEMORY_AFTER_CHAT ×2） | Trigger 后加 `afterCtx.FromMap(data)`（当前无读回需求，仅为对称闭环 + future-proof） |
| `handle_envelope.go` L80（BEFORE_CHAT_REQUEST） | Trigger 后 `hookCtx.FromMap(data)` + 把 `hookCtx.Params` 重新 `json.Marshal` 回 `request.Params`（Python 同 dict 引用语义的 Go 等价物） |
| `forward_loop.go` 步骤9（**新增**） | `triggerBeforeChatRequestHook(ctx, agentMsg)`：CHAT_SEND/CHAT_RESUME/CHAT_ANSWER 门控（对齐 `_should_trigger_before_chat_request_hook`）→ 构建 `GatewayChatHookContext`（request_id/channel_id/session_id/req_method/params）→ Trigger → FromMap → 回写 `agentMsg.Params`。修正注释 `TODO(#11.13)` → 归属 11.3（对齐 Python message_handler.py L1871-1888 + L2501-2502） |

`FromMap` 实现要点（4 个结构体同构）：

- 防御式类型断言：字段不存在或类型不符时跳过（不 panic、不报错）
- 指针字段（ChannelID/SessionID/ReqMethod/AssistantMessage）：map 值为 string 时取址，
  非 string/缺失时保留原值
- `MemoryBlocks`：`[]any` → `[]string` 逐元素断言过滤
- 仅写回**输出语义字段**（memory_blocks/params/metadata/home_dir/skill_dir/
  assistant_message），输入字段（session_id 等）不回写——扩展改输入字段对宿主无意义

## 七、GRPCPluginTransport（Transport 代理）

```go
type GRPCPluginTransport struct {
    client *plugin.PluginClient
    recvCh chan []byte        // TransportRecv 流 → channel（对齐 Recv() 契约）
    // mu/closed/recvOnce 惰性启动接收 goroutine
}
```

- `Send(ctx, data)` → `TransportSend` RPC；ctx 取消传播
- `Recv()` → 首次调用惰性启动 goroutine 消费 `TransportRecv` server-streaming，逐帧写入
  recvCh；流断开/出错时 close(recvCh)（对齐 ChannelTransport 的 Recv 语义）
- `Close()` → `TransportClose` RPC + 防重入；幂等
- 编译期断言 `var _ transport.AgentTransport = (*GRPCPluginTransport)(nil)`
- **不改 AgentClient/receiverLoop 一行代码**（规则 6：Transport 纯 byte 管道，
  E2A 语义留在 AgentClient）

## 八、cmd 接线（runAppCmd 第 6 步回填）

```go
// 现有注释块 ⤵️ 10.5.8 放开为真实实现：
fw := callback.GetCallbackFramework()
extRegistry, err := extensions.CreateInstance(fw, map[string]any{}, nil)
...
extDirs := readExtensionDirs(cfg)   // cfg.Get("extensions") → extension_dirs 字符串 → ';'分割 → 相对→绝对
extManager := extensions.NewExtensionManager(extRegistry, extensions.WithSearchPaths(extDirs))
if err := extManager.LoadAllExtensions(ctx); err != nil { /* Error 日志不中断启动 */ }
logger.Info(...).Int("count", len(extManager.ListExtensions())).Msg("extensions loaded")

// L229 transport 选择回填（对齐 app_gateway.py L828-833）：
var agentTransport transport.AgentTransport
if t := extRegistry.GetAgentServerClient(); t != nil {
    agentTransport = t
} else {
    agentTransport = transport.NewChannelTransport()
}
agentClient := routing.NewAgentClient(agentTransport)
// AgentServer 单进程模式：仅当走 ChannelTransport 时构造进程内 server；
// 扩展 transport 存在时 AgentServer 不在本进程（远端场景），跳过本地 server 启动
```

注：`readExtensionDirs` 是 cmd 层的小函数（~15 行），对齐 Python
`_extension_dir_paths_from_config`（仅支持字符串值、';' 分割、strip 空段）+
`_setup_search_paths` 的相对路径 resolve 语义（Go：非绝对路径按 cwd 转绝对）。

启动顺序（对齐 app_agentserver.py L130-141 / app_gateway.py L814-822）：
CreateInstance → NewExtensionManager → LoadAllExtensions → **之后**才创建
AgentServer/GatewayServer。

## 九、插件作者体验（serve.go SDK）

插件仓库结构（对齐 Python 扩展目录形态）：

```
my-extension/
├── extension.yaml      # manifest（id/name/version/...，同 Python 格式）
├── config.yaml         # 可选插件私有配置（读取由插件自己负责）
└── main.go             # 插件入口：plugin.ServeExtension(&MyExtension{})
                        # go build -o ext-plugin ./  → 产出二进制放入该目录
```

`ExtensionImpl`（插件实现接口，host/serve 共享定义于 plugin 包）：

```go
type ExtensionImpl interface {
    // Register 注册声明（对齐 register_extensions(registry)：返回单声明或列表）
    Register(ctx context.Context) ([]Declaration, error)
    // Shutdown 关闭（对齐 shutdown）
    Shutdown(ctx context.Context) error
    // HandleEvent 事件处理：原地修改 ctx 并返回（对齐扩展钩子函数）
    HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error)
}

type Declaration struct {
    Metadata        Metadata            // plugin 包自定义的元数据结构（id/name/version/...，
                                        //   与 extensions.ExtensionMetadata 同构；loader 负责互转）
    EventSubs       []EventSubscription // {event, priority}
    ProvidesTransport bool              // 声明为 transport 提供者则需实现 TransportImpl
}

// 可选接口（ProvidesTransport=true 时 ServeExtension 断言要求实现）：
type TransportImpl interface {
    Send(ctx context.Context, data []byte) error
    Recv() (<-chan []byte, error)
    Close() error
}
```

`plugin.ServeExtension(impl)`：go-plugin 标准入口（plugin.Serve + 握手常量 +
gRPC server 注册 + 信号处理）。**不做进程内 GetMetadata RPC 的宿主侧依赖**——宿主
Register 已拿到全部声明。

## 十、测试策略（规则 3：默认跑 + 无逃避 tag）

1. **单元测试（默认 `go test` 跑）**：
   - loader/manager：注入 fake 插件工厂（接口抽象 `pluginHost`，in-memory handle 模拟
     Register/SetExtensionDir/HandleEvent 返回值）覆盖：发现语义（yaml 或 ext-plugin）、
     加载编排、单插件失败继续、shutdown 逐个 + 失败仅日志、list 结构
   - `FromMap` ×4 结构体：完整字段往返 / 防御分支（类型不符、缺失、nil）
   - forward_loop 步骤9：门控三方法、Trigger 后 params 回写（registry.Register 注入
     闭包改 params）
   - uapclaw 写回：bridgeFn 写回后 memory_block 注入 inputs（空块过滤）
   - GRPCPluginTransport：本地 grpc 侦听器（bufconn 或 localhost:0）mock 三 RPC，
     Send/Recv 回环 + Close 幂等 + Recv 惰性
2. **端到端测试（同一 test 包，TestMain 中 `go build -o testdata/ext-plugin`）**：
   - 全链路：临时目录放 fake 插件（extension.yaml + 测试插件 main.go 编译产物）→
     Manager.LoadAll → registry 填充 → Trigger 往返改写 memory_blocks → Transport
     Send/Recv 回环（echo transport 插件）→ ShutdownAll
   - 不加 build tag：本地 `go build` 非外部服务（规则 3.3 httptest 同理）
   - 编译失败时 t.Skip（保护无 go 工具链的环境）
3. 覆盖率 ≥85%（规则 3.6），生成代码（.pb.go）不人为补测（声明覆盖豁免理由于
   coverage 说明）

## 十一、计划表与标记回填清单（内容不丢失原则）

| 位置 | 动作 |
|---|---|
| 10.5.7 行 | ☐ ⤵️ → ✅，产出描述更新（go-plugin 方案 + 协议 + 发现/加载语义对齐） |
| 10.5.8 行 | ☐ ⤵️ → ✅（WithSearchPaths 注入解环 + LoadAll/ShutdownAll/List） |
| 10.5.3 行 | 补注：FromMap 写回闭环已实现（4 上下文） |
| 10.5.10 行 | 保持 ☐ ⤵️（本期不动；config DecryptFunc 回填点继续等 CryptoProvider） |
| 11.3 行 | 补注：before_chat_request 触发点已回填（forward_loop 步骤9） |
| `forward_loop.go` L126 | `TODO(#11.13)` 修正为 11.3 既有逻辑已实现注释 |
| `runAppCmd` L216-226 | ⤵️ 10.5.8 注释块替换为真实实现 |
| `uapclaw.go`/`handle_envelope.go` | 新增 FromMap 调用（无 ⤵️ 残留） |
| extensions/doc.go、plugin/doc.go | 目录树与包说明更新（新增 plugin/ 子包、loader/manager 实装） |
| sdk/doc.go | 不变（BaseExtension initialize 语义注释补一句"host 不调用"） |

## 十二、风险与边界

1. **go-plugin 平台行为**：Windows 下子进程 stdout/stderr 处理有已知怪癖（go-plugin
   协议要求 stdout 专用于握手）。serve.go 强制提示插件不要写 stdout（文档注释 +
   README 段落）；端到端测试在 Linux CI 验证。
2. **Struct 往返性能**：HandleEvent 每次全量 map 往返。事件频率低（每条消息至多 3 次
   钩子），且 Python 版同样全量 to_dict，可接受；不做增量优化。
3. **生命周期**：宿主退出路径 ShutdownAllExtensions 调 Shutdown RPC（2s 超时）后
   client.Kill()；插件崩溃时 go-plugin 自动带崩宿主进程组（FailNow 语义）——本期接受
   该默认（对齐 Python 进程内扩展异常即整体崩的行为差异已在方案 D 决策中确认）。
4. **GetAgentServerClientExtension 本地接口**：现有 registry.go 定义了本地
   AgentServerClientExtension 接口（Initialize/Shutdown/Metadata/SetExtensionDir/GetClient）；
   pluginTransportExtension 需实现全接口面（Initialize 空实现——对齐事实基线 2 host 不调）。
5. **单进程 ChannelTransport 分支**：扩展 transport 生效时本地 AgentServer 不启动，
   该场景为"远端 AgentServer"部署形态；单进程默认路径行为不变（回归测试保证）。
