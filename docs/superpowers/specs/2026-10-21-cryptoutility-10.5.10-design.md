# 10.5.10 CryptoUtility — 加解密扩展双路径（宿主内置 + 子进程插件）设计

> 状态：待评审
> 日期：2026-10-21
> 范围：IMPLEMENTATION_PLAN.md 10.5.10（CryptoUtility），含 7 处 ⤵️ 回填点闭环：
> ① plugin/proto 协议 crypto 扩展 ② loader 声明注册编排 crypto 分支 ③ registry any 收紧
> ④ sdk/crypto_utility.go stub 删除 ⑤ web/config_apply 本地接口统一 + app_gateway 装配
> ⑥ config/default_models decryptModelEntries 解密实装 ⑦ cmd 启动接线（密钥初始化 + WithDecrypt）。
> Python 参考：jiuwenswarm/extensions/sdk/crypto_utility.py · common/security/base_crypto.py ·
> extensions/registry.py（register_crypto_utility/get_crypto_provider）· common/config.py
> （resolve_env_vars L52-61 · _decrypt_model_entries L668-694）·
> gateway/channel_manager/web/app_web_handlers.py（L611-614/692-699/857-866）·
> gateway/channel_manager/tui/tui_connect.py（L631-637/741-745）

## 一、决策记录（与用户讨论确认）

| 决策点 | 选择 | 理由 |
|---|---|---|
| 实现位置 | **宿主内置 + 子进程插件双路径** | 宿主 `common/crypto.AesGcmProvider` 保底闭环全部回填点、config 读写零 RPC 开销；插件路径完整对齐 Python 生态位（crypto 由外部扩展供给），两者不冲突 |
| 密钥来源 | **密钥文件（data 目录下）** | 用户零配置：首次启动 `crypto/rand` 生成 32 字节写 `~/.uapclaw/crypto.key`（0600），之后读取；换机/迁移提示备份；删除文件即重新生成 |
| 接口签名 | **统一 common/crypto 带 error 签名** | 符合 Go 惯例，加解密失败可观察；web 侧调用失败时跳过该字段 + Warn 日志（对齐 Python `except: pass`）；删除 web 本地无 error 同名接口 |
| sdk stub 处置 | **删除整个 sdk/crypto_utility.go** | Go 无 Python importlib 同进程加载场景，`CryptoUtilityStub` 无任何消费方即死代码；插件作者 crypto 入口是 plugin 包 `CryptoImpl`；宿主内置不走 sdk 接口 |
| 插件覆盖语义 | **插件即全权接管** | 对齐 Python 唯一 crypto 来源语义：插件 `provides_crypto` 时 `SetCryptoProvider` 覆盖宿主全局；旧密文 Decrypt 失败逐字段跳过 + Warn（= Python `except: pass`） |

## 二、编号核实与 Python 事实基线

**编号澄清**：10.5.10 是 CryptoUtility（加密扩展），不是 TeamManager（TeamManager = 10.6.19，已于 2026-02-13 完成）。用户最初将两者关联，经核实后按 10.5.10 CryptoUtility 展开设计。

**已核实的事实基线**（探索结论，勿重复调查）：

1. **Python 本体极小（~45 行）**：
   - `base_crypto.py`（21 行）：`CryptoProvider` Protocol（`encrypt/decrypt(str)->str`）+ 模块级全局 `set/get_crypto_provider`——**该全局是死代码，全仓无人调用**
   - `crypto_utility.py`（17 行）：`CryptoUtility(BaseExtension)` 抽象类，唯一抽象方法 `get_crypto()`
   - `registry.py`：`register_crypto_utility(ext)` / `get_crypto_provider()`（= `ext.get_crypto()`）
2. **Python 无任何 CryptoProvider 内置实现**，也无 `register_crypto_utility` 的调用方——crypto 实例完全来自磁盘上的第三方扩展包（loader 动态加载）
3. **消费点全景**（Python 侧 4 处 + config 2 处）：
   - web `config.get`：api_key/token 字段 decrypt 后返回前端（app_web_handlers.py L611-614）
   - web `config.set`/`_encrypt_config_params`：encrypt 后写 .env（L692-699）
   - web `models.replace_all`/`_merge_models_for_replace_all`：merge 时 api_key encrypt 写回 config.yaml（L857-866）
   - tui `config.get`/`config.set`：同 web 语义（tui_connect.py L631-637/741-745）
   - `resolve_env_vars`：`${VAR}` 替换时变量名含 api_key/token 且有 crypto → decrypt（config.py L52-61）
   - `_decrypt_model_entries`：models.defaults 条目 api_key decrypt（config.py L668-694）
4. **Go 已有基础设施**（memory codec 时建立，"接口待定义"的延后理由已不成立）：
   - `common/crypto`：`CryptoProvider` 接口（带 error）+ `AesGcmProvider`（AES-256-GCM，密钥内持）+ `Set/GetCryptoProvider` 全局（RWMutex）+ `NewDecryptFunc`（对接 `config.WithDecrypt`）
   - `AesGcmCrypt` 密文格式 `hex(nonce)+hex(tag)+hex(ciphertext)`，与 Python `openjiuwen/core/common/security/crypt_utils.py` 完全兼容
5. **10.5.7/10.5.8 已完成**（2026-10-21，go-plugin 子进程 + gRPC，commit `ac8189bf..12a83c2d`）：现有协议 7 RPC（Register/SetExtensionDir/GetMetadata/Shutdown/HandleEvent + Transport 三件套）；声明注册编排模式已建立（`ExtensionDecl.provides_transport` → `registry.RegisterAgentServerClient`），crypto 沿此对称扩展
6. **依赖方向核实**（修正一个旧注释错误）：`default_models.go` 注释声称"crypto 不能进 common/config"**是错的**。实测：`common/crypto` 仅依赖 `common/exception`（叶子级）；`common/config` 仅依赖 utils；`common/logger→config` 已知环。**config → crypto 合法无环**，`decryptModelEntries` 可直接查全局。真正不能做的是 config → swarm/extensions（那条会环）
7. **Web 通道 Go 现状**：`config_apply.go` 本地另定义无 error 的 `CryptoProvider` 接口；`WebHandlersBindParams` 有 `CryptoProvider` 字段但 `app_gateway.go` 装配时未传 → 运行时恒 nil

## 三、架构总览

```
                     common/crypto（权威：CryptoProvider 接口 + 全局 + AesGcmProvider）
                    ↗                          ↗
        common/config                    swarm/extensions
   （WithDecrypt/decryptModelEntries      （registry.RegisterCryptoUtility）
     查 crypto 全局，解耦 extensions）          ↕
                     cmd 启动层（装配点）
       ① 密钥文件 → AesGcmProvider → SetCryptoProvider（config.Load 之前）
       ② Manager.LoadAll → 插件 provides_crypto → GRPCPluginCryptoProvider
          → SetCryptoProvider 覆盖全局 + registry.RegisterCryptoUtility
```

**职责分层**：
- `common/crypto`：接口 + 全局 + 内置实现 + 密钥文件读写（不 import workspace/config/extensions，保持叶子地位）
- `common/config`：消费全局 provider（`WithDecrypt`/`decryptModelEntries`），不感知 extensions
- `swarm/extensions`：registry 收纳 CryptoUtilityExtension；loader 编排插件声明
- `swarm/extensions/plugin`：proto 协议 + 双侧 SDK（CryptoImpl / GRPCPluginCryptoProvider）
- `cmd`：唯一装配点（密钥初始化 + 覆盖时序）

**启动时序**（对齐 Python app_gateway 顺序语义）：

```
dotenv → 宿主密钥初始化（覆盖首次 config 读取）
→ config.New(WithNormalize, WithDecrypt) + Load
→ ExtensionRegistry.CreateInstance → Manager.LoadAllExtensions
   → 插件 provides_crypto → 覆盖全局 → 后续 config 热重载用插件 crypto
```

对齐说明：Python 首次 `get_config()` 时扩展必然未加载、crypto 必为 None（跳过解密）；Go 宿主路径因不依赖扩展系统**首次即就绪**（偏差 #4）。插件路径保持与 Python 相同的"加载后才生效"语义。

## 四、宿主内置路径（密钥文件）

- **位置**：`workspace.WorkspaceDir()/crypto.key`（`~/.uapclaw/crypto.key`，受 `UAPCLAW_DATA_DIR` 重定向；测试用 `t.Setenv("UAPCLAW_DATA_DIR", t.TempDir()) + path.ResetCache()` 隔离）
- **格式**：64 字符 hex 文本（32 字节 AES-256 密钥）+ 换行；文件权限 `0600`
- **首次启动**：文件不存在 → `crypto/rand` 生成 32 字节 → 写文件 → Info 日志（含换机/备份提示）
- **API**：`common/crypto` 新增 `LoadOrCreateKeyFile(path string) ([]byte, error)`——接收路径参数保持 crypto 包叶子地位，cmd 层组装路径
- **降级语义**：读取/生成失败（目录只读、hex 损坏）→ Warn 日志 → 全局 provider 保持 nil → 等价 Python 无扩展行为，api_key 明文路径照常工作
- **换密钥语义**：删除文件即重新生成；旧密文 Decrypt 失败逐字段跳过（对齐 Python `except: pass`）

## 五、插件路径（proto 协议扩展，7 RPC → 9 RPC）

沿 10.5.7 Transport 模式对称扩展：

**proto/extension.proto**：
```protobuf
message ExtensionDecl {
  ...
  bool provides_crypto = 4;   // 新增：crypto 能力声明
}
rpc Encrypt(EncryptRequest) returns (EncryptResponse);   // {plaintext} → {ciphertext}
rpc Decrypt(DecryptRequest) returns (DecryptResponse);   // {ciphertext} → {plaintext}
```

**serve.go（插件侧 SDK）**：
- `CryptoImpl` 可选接口：`Encrypt(ctx, string) (string, error)` / `Decrypt(ctx, string) (string, error)`（对齐 TransportImpl 模式）
- serve 入口改为 **Functional Options**（消除"transport+crypto 双能力需要组合入口"的笛卡尔积，且与代码库 WithSearchPaths/WithDecrypt 惯例一致）：
  ```go
  type ServeOption func(*serveConfig)
  func WithTransportImpl(t TransportImpl) ServeOption   // 既有能力选项化
  func WithCryptoImpl(c CryptoImpl) ServeOption         // 新增
  func ServeExtension(impl ExtensionImpl, opts ...ServeOption)   // 变参向后兼容：现有零参调用不变
  ```
  既有 `ServeExtensionWithTransport(impl, t)` 保留为 `ServeExtension(impl, WithTransportImpl(t))` 薄包装（echo 测试插件不改）
- `extensionServer` 增加 `cryptoImpl` 字段 + 两个 RPC handler；`cryptoImpl == nil && ProvidesCrypto` 时降级 false（复用 transport 同款保护）

**host.go（宿主侧）**：`PluginClient.Encrypt/Decrypt` 两个 RPC 封装

**loader.go（编排）**：`loadAndRegister` 增加 crypto 分支——`decl.ProvidesCrypto` 时构造 `pluginCryptoExtension`（loader 包内适配器，实现 registry 的 `CryptoUtilityExtension` 接口，`GetCrypto()` 返回包着 PluginClient 的 `GRPCPluginCryptoProvider`）→ `registry.RegisterCryptoUtility(...)` + `crypto.SetCryptoProvider(...)`（覆盖宿主默认）。结构对齐既有 `pluginTransportExtension` 适配器先例（loader.go L306）

**覆盖时序**：LoadAllExtensions 逐插件顺序执行，后加载的 crypto 插件覆盖先加载的（Python 同为 loader 顺序覆盖，无优先级仲裁——最后者胜）。宿主密钥最先设置，插件覆盖之。多个 crypto 插件时最终全局为最后加载者，registry 内 `cryptoUtil` 字段同样被最后者覆盖。

## 六、registry / sdk 接口收紧（回填点 ③④）

```go
// registry.go — any 全部收紧为具体接口
type CryptoUtilityExtension interface {
    Initialize(ctx, *ExtensionConfig) error
    Shutdown(ctx) error
    Metadata() *ExtensionMetadata
    SetExtensionDir(path string)
    GetCrypto() crypto.CryptoProvider   // was: any
}
func (r *ExtensionRegistry) GetCryptoProvider() crypto.CryptoProvider   // was: any
```

`sdk/crypto_utility.go`：**删除整个文件**（决策见上）。`sdk/doc.go` 与 `extensions/doc.go` 文件目录同步更新。

## 七、消费点接线（回填点 ⑤⑥⑦）

| 消费点 | 改动 |
|--------|------|
| `web/config_apply.go` | 删本地 `CryptoProvider` 接口 → import `common/crypto`（带 error）；`ApplyConfigPayload`/`buildModelsDefaultsFromFrontend`/`mergeModelsForReplaceAll` 调用处 error → 跳过该字段 + Warn 日志 |
| `web/web_handlers.go` | `handleConfigGet/Set/SaveAll/ModelsList/ModelsReplaceAll` 内部 `cryptoProv` 改从 `crypto.GetCryptoProvider()` 全局取；BindParams 字段保留注入通道供测试 |
| `app_gateway.go` | `WebHandlersBindParams` 装配处补 `CryptoProvider: crypto.GetCryptoProvider()`（修复恒 nil 接线缺失） |
| `config/default_models.go` | `decryptModelEntries` 实装（对齐 Python L685-689 双条件 + try/except）：<br>`if key, ok := mcc["api_key"].(string); ok && key != "" { if p := crypto.GetCryptoProvider(); p != nil { if dec, err := p.Decrypt(key); err == nil { mcc["api_key"] = dec } } }`；同时修正那条错误的"不能进 common/config"注释 |
| `cmd/uapclaw/cmd.go` | ① dotenv 后插入宿主密钥初始化段（`workspace.WorkspaceDir()` + `LoadOrCreateKeyFile` + `NewAesGcmProvider` + `SetCryptoProvider`，失败 Warn 继续）② `config.New("", WithNormalize(...), WithDecrypt(crypto.NewDecryptFunc()))` |

## 八、对齐偏差表（审查对照用）

| # | 偏差 | 理由 |
|---|------|------|
| 1 | Go 增加宿主内置 AesGcmProvider + 密钥文件（Python 无任何内置实现） | 用户决策：双路径保底，闭环 7 处回填点 |
| 2 | 插件 crypto 每次 Encrypt/Decrypt 一次 gRPC 往返（Python 同进程零开销） | go-plugin 子进程架构固有代价；config 面板低频路径可接受 |
| 3 | Python `base_crypto.set/get_crypto_provider` 全局死代码 → Go 激活为统一事实源 | 双路径优先级（插件覆盖宿主）需要单一裁决点 |
| 4 | Python 首次 config 读取时 crypto 必未就绪 → Go 宿主路径首次即就绪 | 宿主密钥不依赖扩展系统；能力增强非语义偏离 |
| 5 | sdk 包 CryptoUtility 接口删除（Python 有、Go 无同进程加载场景） | 无实现者的死代码；插件侧由 plugin.CryptoImpl 承接 |
| 6 | 密钥文件路径/格式/生成语义 Python 无参考，Go 自行设计 | 用户决策：data 目录 + 0600 + hex + 自动生成 |
| 7 | 宿主密钥加密的旧密文在插件接管后解不开（跳过 + Warn） | 用户决策：插件即全权接管，对齐 Python 唯一来源语义 |

## 九、测试策略

- **common/crypto**：`LoadOrCreateKeyFile`（首次生成/二次读取/损坏 hex 报错/目录只读报错/权限 0600）+ 既有测试回归
- **plugin 包**：fake 插件实现 CryptoImpl → 进程内 gRPC 直连（复用 `NewPluginClientFromConn` + 既有 fake 模式）测 Encrypt/Decrypt RPC + `cryptoImpl==nil && ProvidesCrypto` 降级分支
- **extensions/loader**：fake PluginClientFace 声明 ProvidesCrypto → 断言 `RegisterCryptoUtility` 被调 + 全局 provider 被覆盖（含"宿主已设默认后被插件覆盖"用例）
- **web**：既有 `fakeCrypto` 改带 error 签名；新增"Decrypt 失败跳过字段"用例
- **config/default_models**：全局 provider + 预加密 api_key 的 defaults → 断言解密后返回；provider 为 nil 时不解密
- **e2e**：`plugin/testdata/echo` 扩展加 crypto 能力（echo 加解密实现），沿用真实 go-plugin 子进程回环模式验证端到端
- **cmd 层**：不测（启动装配路径，遵循 cmd.go build tag 惯例）

## 十、交付物清单

| 文件 | 动作 | 内容 |
|------|------|------|
| `internal/common/crypto/keyfile.go` | 新增 | `LoadOrCreateKeyFile` |
| `internal/common/crypto/crypto.go` | 微调 | 无（接口/全局已就绪） |
| `internal/swarm/extensions/plugin/proto/extension.proto` | 修改 | `provides_crypto = 4` + Encrypt/Decrypt 两 RPC + 四个 message |
| `plugin/proto/*.pb.go` | 重新生成 | protoc-gen-go v1.36.10 / protoc-gen-go-grpc 1.5.1（已装，/root/go/bin 需 PATH） |
| `internal/swarm/extensions/plugin/serve.go` | 修改 | CryptoImpl 接口 + ServeOption/WithCryptoImpl（ServeExtension 变参化，既有调用零改动）+ 两个 RPC handler + 降级保护 |
| `internal/swarm/extensions/plugin/host.go` | 修改 | PluginClient.Encrypt/Decrypt 封装 |
| `internal/swarm/extensions/plugin/crypto_provider.go` | 新增 | GRPCPluginCryptoProvider（宿主侧 crypto.CryptoProvider 实现） |
| `internal/swarm/extensions/loader.go` | 修改 | loadAndRegister crypto 分支 |
| `internal/swarm/extensions/registry.go` | 修改 | any → crypto.CryptoProvider 收紧（3 处） |
| `internal/swarm/extensions/sdk/crypto_utility.go` | 删除 | stub 死代码（决策记录） |
| `internal/swarm/extensions/sdk/doc.go` + `extensions/doc.go` | 更新 | 文件目录/延后说明同步 |
| `internal/swarm/gateway/channel_manager/web/config_apply.go` | 修改 | 删本地接口 + error 处理 + Warn |
| `internal/swarm/gateway/channel_manager/web/web_handlers.go` | 修改 | handler 内改查全局 + BindParams 注入通道保留 |
| `internal/swarm/gateway/app_gateway.go` | 修改 | 装配补 CryptoProvider |
| `internal/common/config/default_models.go` | 修改 | decryptModelEntries 实装 + 修正错误注释 |
| `cmd/uapclaw/cmd.go` | 修改 | 宿主密钥初始化段 + WithDecrypt |
| `IMPLEMENTATION_PLAN.md` | 更新 | 10.5.10 ✅ + 相关行回填记录 |

## 十一、范围边界（不做什么）

- **TUI 消费点（11.15）不动**：Python tui_connect.py 的 config.get/set crypto 消费属 11.15 TUI 通道实现范围，本设计只建好全局事实源；届时 TUI 直接查 `crypto.GetCryptoProvider()` 即可
- **宿主密钥轮换/多密钥不支持**：单密钥文件单密钥；轮换 = 删除文件重新生成 + 旧密文自然失效（跳过语义）
- **密钥文件不进 .gitignore 管理**：位于用户 data 目录（`~/.uapclaw/`），仓库无需处理
- **`base_crypto.py` 模块级全局死代码不移植为独立死代码**：Go 的全局激活为事实源（偏差 #3），不保留"两个全局"形态

## 十二、风险与边界

- **grpc 重复注册杀进程**（记忆中的实证坑）：测试中避免对同一 grpc.Server 重复 RegisterExtensionServer，用 GetServiceInfo 检查
- **proto 生成**：生成代码提交入库，仓库不依赖 protoc（既有惯例）；本设计需重新生成一次
- **覆盖率**：plugin 包新增 RPC handler 与宿主/插件两侧适配均可用进程内 fake 覆盖；`ServeExtension`（含 options）与既有 serve 入口同属 e2e 豁免（调 plugin.Serve 阻塞）
- **既有 fakeCrypto 适配**：web 测试的 fake（无 error 签名）需同步改签名，属机械改动
