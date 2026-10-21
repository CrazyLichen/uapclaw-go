# 10.5.10 CryptoUtility 实现计划（宿主内置 + 子进程插件双路径）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 10.5.10 CryptoUtility 双路径加解密：宿主内置 AesGcmProvider（密钥文件 `~/.uapclaw/crypto.key`）+ 子进程插件 `provides_crypto` 声明（gRPC Encrypt/Decrypt），闭环 7 处 ⤵️ 回填点。

**Architecture:** `common/crypto` 为权威接口层（CryptoProvider + 全局 + 密钥文件，保持叶子地位）；`plugin` 包扩展 proto 协议 7→9 RPC（双侧 SDK：CryptoImpl / GRPCPluginCryptoProvider）；`extensions` 包 registry 收紧 any→具体接口 + loader 编排"插件覆盖宿主全局（最后加载者胜）"；web/config 消费点统一改查全局；cmd 为唯一装配点（密钥初始化在 config.Load 之前）。设计文档：`docs/superpowers/specs/2026-10-21-cryptoutility-10.5.10-design.md`。

**Tech Stack:** Go 1.26 / hashicorp go-plugin / gRPC + protobuf（protoc 3.21.12 + protoc-gen-go v1.36.10 + protoc-gen-go-grpc 1.5.1，位于 `/root/go/bin`）/ AES-256-GCM。

---

## 全局纪律（每个任务执行前必读）

1. **编译互斥（项目规则 5）**：每次 `go build`/`go test` 前先 `pgrep -f 'go (build|test)'`，有残留先 kill；同一时间只允许一个编译进程。设置 `export GOPROXY=https://goproxy.cn,direct`。
2. **禁止 `go clean -cache`**（全量编译超 20 分钟）。
3. **protoc 工具链**：`protoc-gen-go`/`protoc-gen-go-grpc` 在 `/root/go/bin`，执行 proto 生成前 `export PATH=$PATH:/root/go/bin`。
4. **grpc 重复注册杀进程（实证坑）**：测试中每次用 `startTestProtoServer` 起新 grpc.Server，绝不复用同一 Server 重复 Register。
5. **覆盖率（项目规则 3）**：受影响包整体 ≥85%。命令 `go test -cover ./<pkg>/...`。
6. **注释全部中文**；源码声明按"结构体→枚举→常量→全局变量→导出函数→非导出函数"排列（项目规则 1）。
7. **日志（项目规则 3）**：`common/crypto` 与 `common/config` 因 `logger→config` 依赖环**禁止导入 common/logger**，改用 stdlib `log.Printf` 带 `[crypto]`/`[config]` 前缀（task 9 之后 config→crypto 边存在，crypto→logger 会成环）。
8. **工作目录**：所有命令在仓库根 `/home/opensource/uapclaw-gateway` 执行。

---

### Task 1: common/crypto 宿主密钥文件 LoadOrCreateKeyFile

**Files:**
- Create: `internal/common/crypto/keyfile.go`
- Test: `internal/common/crypto/keyfile_test.go`
- Modify: `internal/common/crypto/doc.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/common/crypto/keyfile_test.go`：

```go
package crypto

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateKeyFile_首次生成 不存在时生成 32 字节密钥并写文件（64 hex + 换行，0600）
func TestLoadOrCreateKeyFile_首次生成(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")

	key, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	if len(key) != KeyLength {
		t.Fatalf("密钥长度 = %d, 期望 %d", len(key), KeyLength)
	}

	// 文件存在且为 64 字符 hex + 换行
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取密钥文件失败: %v", err)
	}
	content := string(data)
	if len(content) != 65 || content[64] != '\n' {
		t.Fatalf("文件内容格式不符（期望 64 hex + \\n）: %q", content)
	}

	// 权限 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %o, 期望 600", info.Mode().Perm())
	}

	// 文件内容 hex 与返回 key 一致
	if content[:64] != hex.EncodeToString(key) {
		t.Error("文件 hex 内容与返回密钥不一致")
	}
}

// TestLoadOrCreateKeyFile_二次读取 已存在时读取并返回相同密钥
func TestLoadOrCreateKeyFile_二次读取(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")

	key1, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	key2, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("二次读取应返回相同密钥")
	}
}

// TestLoadOrCreateKeyFile_损坏hex 报错
func TestLoadOrCreateKeyFile_损坏hex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("zzzz\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("损坏 hex 应返回错误")
	}
}

// TestLoadOrCreateKeyFile_长度不符 报错（2 字节 hex）
func TestLoadOrCreateKeyFile_长度不符(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("aabb\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("密钥长度不符应返回错误")
	}
}

// TestLoadOrCreateKeyFile_空文件 报错
func TestLoadOrCreateKeyFile_空文件(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("空文件应返回错误")
	}
}

// TestLoadOrCreateKeyFile_写入失败 父目录不存在时生成报错
func TestLoadOrCreateKeyFile_写入失败(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noexist", "crypto.key")
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("父目录不存在应返回写入错误")
	}
}

// TestLoadOrCreateKeyFile_路径为目录 报错
func TestLoadOrCreateKeyFile_路径为目录(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateKeyFile(dir); err == nil {
		t.Error("目录路径应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/common/crypto/ -run TestLoadOrCreateKeyFile -v`
Expected: 编译失败 `undefined: LoadOrCreateKeyFile`

- [ ] **Step 3: 实现 keyfile.go**

创建 `internal/common/crypto/keyfile.go`：

```go
package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keyFilePerm 密钥文件权限（仅属主可读写）
	keyFilePerm = 0o600
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadOrCreateKeyFile 读取密钥文件，不存在时生成 32 字节密钥并写入。
//
// 文件格式：64 字符 hex 文本（32 字节 AES-256 密钥）+ 换行，权限 0600。
// 首次生成使用 crypto/rand；删除文件即重新生成（旧密文自然失效，
// 解密失败由消费方逐字段跳过，对齐 Python except: pass）。
//
// 接收路径参数保持本包叶子地位（不 import workspace/config；
// 本包亦禁止导入 common/logger——logger→config→crypto 会成环），
// 路径由调用方（cmd 层）组装，日志用 stdlib log 以 [crypto] 前缀区分。
func LoadOrCreateKeyFile(path string) ([]byte, error) {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("密钥路径 %s 是目录", path)
		}
		// 文件存在：读取并解析
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取密钥文件 %s 失败: %w", path, err)
		}
		key, err := parseKeyFileContent(string(data))
		if err != nil {
			return nil, fmt.Errorf("密钥文件 %s 损坏: %w", path, err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("访问密钥文件 %s 失败: %w", path, err)
	}

	// 首次生成：crypto/rand 32 字节 → hex 写文件
	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), keyFilePerm); err != nil {
		return nil, fmt.Errorf("写入密钥文件 %s 失败: %w", path, err)
	}
	log.Printf("[crypto] 首次生成密钥文件 %s（换机/迁移请备份；删除文件即重新生成，旧密文将失效）", path)
	return key, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseKeyFileContent 解析密钥文件内容为 32 字节密钥
func parseKeyFileContent(content string) ([]byte, error) {
	trimmed := strings.TrimSpace(content)
	key, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("密钥长度 %d 字节，期望 %d", len(key), KeyLength)
	}
	return key, nil
}
```

- [ ] **Step 4: 更新 crypto/doc.go 文件目录**

`internal/common/crypto/doc.go` 中文件目录段改为（新增 keyfile.go 行）：

```
//	crypto/
//	├── doc.go           # 包文档
//	├── aes_gcm.go       # AES-256-GCM 加解密实现
//	├── registry.go      # 加密算法注册表（并发安全）
//	├── crypto.go        # CryptoProvider 接口 + 全局管理 + AesGcmProvider
//	└── keyfile.go       # 宿主密钥文件加载/生成（10.5.10 双路径之宿主内置）
```

并在包注释"上层"要点后追加一行说明：`//   注意：本包禁止导入 common/logger（logger→config→crypto 环），日志用 stdlib log 以 [crypto] 前缀。`

- [ ] **Step 5: 运行测试验证通过**

Run: `go test ./internal/common/crypto/ -cover`
Expected: 全部 PASS，覆盖率 ≥85%

- [ ] **Step 6: 提交**

```bash
git add internal/common/crypto/keyfile.go internal/common/crypto/keyfile_test.go internal/common/crypto/doc.go
git commit -m "feat(crypto): 10.5.10 宿主密钥文件 LoadOrCreateKeyFile（首启生成/二次读取/0600）"
```

---

### Task 2: proto 协议扩展 provides_crypto + Encrypt/Decrypt RPC（7→9）

**Files:**
- Modify: `internal/swarm/extensions/plugin/proto/extension.proto`
- Regen: `internal/swarm/extensions/plugin/proto/extension.pb.go` + `extension_grpc.pb.go`

- [ ] **Step 1: 修改 extension.proto**

三处修改（其余内容不动）：

① service Extension 内、`rpc TransportClose` 之后追加：

```protobuf
  // Encrypt 加密明文（crypto 提供者能力，对齐 crypto.CryptoProvider.Encrypt）
  rpc Encrypt(EncryptRequest) returns (EncryptResponse);
  // Decrypt 解密密文（crypto 提供者能力，对齐 crypto.CryptoProvider.Decrypt）
  rpc Decrypt(DecryptRequest) returns (DecryptResponse);
```

② ExtensionDecl message 追加字段：

```protobuf
  // ProvidesCrypto 是否为 crypto 提供者（加解密能力）
  bool provides_crypto = 4;
```

③ 文件末尾（TransportCloseResponse 之后）追加：

```protobuf
// ─── Crypto 代理 ───

message EncryptRequest {
  // Plaintext 明文
  string plaintext = 1;
}

message EncryptResponse {
  // Ciphertext 密文
  string ciphertext = 1;
}

message DecryptRequest {
  // Ciphertext 密文
  string ciphertext = 1;
}

message DecryptResponse {
  // Plaintext 明文
  string plaintext = 1;
}
```

- [ ] **Step 2: 重新生成 pb 代码**

```bash
export PATH=$PATH:/root/go/bin && make proto-gen
```

Expected: 无报错，`extension.pb.go` 与 `extension_grpc.pb.go` 被更新。

- [ ] **Step 3: 验证生成结果与编译**

```bash
grep -n "ProvidesCrypto\|type EncryptRequest\|type DecryptRequest" internal/swarm/extensions/plugin/proto/extension.pb.go | head -6
go build ./internal/swarm/extensions/...
go test ./internal/swarm/extensions/plugin/ -count=1
```

Expected: grep 命中 ProvidesCrypto/EncryptRequest/DecryptRequest；编译通过；既有测试全绿（新 RPC 由 `UnimplementedExtensionServer` 嵌入自动满足，无需改 serve.go）。

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/extensions/plugin/proto/
git commit -m "feat(extensions): 10.5.10 proto 协议扩展 provides_crypto + Encrypt/Decrypt RPC（7→9）"
```

---

### Task 3: 插件侧 SDK——CryptoImpl 接口 + ServeOption 函数式选项 + 降级保护

**Files:**
- Modify: `internal/swarm/extensions/plugin/serve.go`
- Test: `internal/swarm/extensions/plugin/serve_test.go`

- [ ] **Step 1: 写失败测试**

`internal/swarm/extensions/plugin/serve_test.go` 末尾追加：

```go
// fakeCryptoImpl 用于测试的插件侧 crypto 实现
type fakeCryptoImpl struct{}

func (f *fakeCryptoImpl) Encrypt(ctx context.Context, plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (f *fakeCryptoImpl) Decrypt(ctx context.Context, ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

// TestCryptoImpl_接口满足 验证 fakeCryptoImpl 满足 CryptoImpl 接口
func TestCryptoImpl_接口满足(t *testing.T) {
	var _ CryptoImpl = (*fakeCryptoImpl)(nil)
}

// TestServeOptions_选项注入 测试 WithCryptoImpl/WithTransportImpl 写入 serveConfig
func TestServeOptions_选项注入(t *testing.T) {
	cfg := &serveConfig{}
	WithCryptoImpl(&fakeCryptoImpl{})(cfg)
	if cfg.cryptoImpl == nil {
		t.Error("WithCryptoImpl 未写入 cryptoImpl")
	}
	WithTransportImpl(nil)(cfg)
	if cfg.transportImpl != nil {
		t.Error("WithTransportImpl(nil) 应保持 nil")
	}
}

// TestExtensionServer_Register_Crypto降级 cryptoImpl 缺失时声明降级为 false
func TestExtensionServer_Register_Crypto降级(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{decls: []Declaration{{
		Metadata:      Metadata{ID: "ext-c"},
		ProvidesCrypto: true,
	}}}}
	resp, err := srv.Register(context.Background(), &extproto.RegisterRequest{})
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if resp.Decls[0].GetProvidesCrypto() {
		t.Error("cryptoImpl 缺失时 ProvidesCrypto 应降级为 false")
	}
}

// TestExtensionServer_Register_Crypto声明保留 cryptoImpl 就绪时声明保留
func TestExtensionServer_Register_Crypto声明保留(t *testing.T) {
	srv := &extensionServer{
		impl:      &fakeExtension{decls: []Declaration{{Metadata: Metadata{ID: "ext-c"}, ProvidesCrypto: true}}},
		cryptoImpl: &fakeCryptoImpl{},
	}
	resp, err := srv.Register(context.Background(), &extproto.RegisterRequest{})
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if !resp.Decls[0].GetProvidesCrypto() {
		t.Error("cryptoImpl 就绪时 ProvidesCrypto 应保留")
	}
}

// TestExtensionServer_EncryptDecrypt crypto RPC handler 直调往返
func TestExtensionServer_EncryptDecrypt(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{}, cryptoImpl: &fakeCryptoImpl{}}
	encResp, err := srv.Encrypt(context.Background(), &extproto.EncryptRequest{Plaintext: "sec"})
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if encResp.GetCiphertext() != "enc:sec" {
		t.Errorf("Ciphertext = %q, 期望 enc:sec", encResp.GetCiphertext())
	}
	decResp, err := srv.Decrypt(context.Background(), &extproto.DecryptRequest{Ciphertext: "enc:sec"})
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if decResp.GetPlaintext() != "sec" {
		t.Errorf("Plaintext = %q, 期望 sec", decResp.GetPlaintext())
	}
}

// TestExtensionServer_Encrypt_未实现 cryptoImpl nil 时返回 ErrNoCrypto
func TestExtensionServer_Encrypt_未实现(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{}}
	if _, err := srv.Encrypt(context.Background(), &extproto.EncryptRequest{}); err == nil {
		t.Error("cryptoImpl 缺失时 Encrypt 应返回错误")
	} else if !errors.Is(err, ErrNoCrypto) {
		t.Errorf("错误应为 ErrNoCrypto, 实际 %v", err)
	}
	if _, err := srv.Decrypt(context.Background(), &extproto.DecryptRequest{}); err == nil {
		t.Error("cryptoImpl 缺失时 Decrypt 应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/extensions/plugin/ -run "TestCryptoImpl|TestServeOptions|TestExtensionServer_Register_Crypto|TestExtensionServer_Encrypt" -v`
Expected: 编译失败 `undefined: CryptoImpl` / `undefined: serveConfig` 等

- [ ] **Step 3: 实现 serve.go 修改**

① 结构体区块（TransportImpl 定义之后）追加：

```go
// CryptoImpl crypto 提供者可选接口（Declaration.ProvidesCrypto=true 时必须实现）。
// 插件作者的 crypto 入口（10.5.10；对齐 Python CryptoUtility.get_crypto() 生态位）
type CryptoImpl interface {
	// Encrypt 加密明文（对齐 crypto.CryptoProvider.Encrypt）
	Encrypt(ctx context.Context, plaintext string) (string, error)
	// Decrypt 解密密文（对齐 crypto.CryptoProvider.Decrypt）
	Decrypt(ctx context.Context, ciphertext string) (string, error)
}

// serveConfig ServeExtension 选项聚合
type serveConfig struct {
	// transportImpl transport 实现（nil 表示无 transport 能力）
	transportImpl TransportImpl
	// cryptoImpl crypto 实现（nil 表示无 crypto 能力）
	cryptoImpl CryptoImpl
}

// ServeOption 插件服务启动选项（函数式选项，消除能力组合入口笛卡尔积）
type ServeOption func(*serveConfig)
```

② Declaration 结构体追加字段：

```go
	// ProvidesCrypto 是否为 crypto 提供者（true 时须实现 CryptoImpl）
	ProvidesCrypto bool
```

③ extensionServer 结构体追加字段（transportImpl 之后）：

```go
	// cryptoImpl crypto 实现（ProvidesCrypto 时非 nil）
	cryptoImpl CryptoImpl
```

④ 全局变量区块追加（ErrNoTransport 之后）：

```go
// ErrNoCrypto 插件未实现 CryptoImpl 却被调用 crypto RPC
var ErrNoCrypto = errors.New("扩展未提供 crypto 能力")
```

⑤ 导出函数区块：**替换** `ServeExtension` 与 `ServeExtensionWithTransport` 两个函数为：

```go
// ServeExtension 启动插件服务（插件 main.go 的唯一入口）。
//
// 用法：
//
//	func main() {
//	    e := &MyExtension{}
//	    plugin.ServeExtension(e,
//	        plugin.WithTransportImpl(e), // 可选：transport 能力
//	        plugin.WithCryptoImpl(e))   // 可选：crypto 能力
//	}
//
// 注意：插件内禁止向 stdout 写任何内容（go-plugin 握手协议独占 stdout），
// 日志请写文件或 stderr。
func ServeExtension(impl ExtensionImpl, opts ...ServeOption) {
	cfg := &serveConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	extensionGRPCServer := &extensionServer{
		impl:          impl,
		transportImpl: cfg.transportImpl,
		cryptoImpl:    cfg.cryptoImpl,
	}

	var pluginSet = plugin.PluginSet{
		"extension": &ExtensionGRPCPlugin{impl: extensionGRPCServer},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  ProtocolVersion,
			MagicCookieKey:   MagicCookieKey,
			MagicCookieValue: MagicCookieValue,
		},
		Plugins: pluginSet,
		GRPCServer: func(opts []grpc.ServerOption) *grpc.Server {
			return grpc.NewServer(opts...)
		},
	})
}

// WithTransportImpl 注入 transport 实现
func WithTransportImpl(t TransportImpl) ServeOption {
	return func(c *serveConfig) { c.transportImpl = t }
}

// WithCryptoImpl 注入 crypto 实现
func WithCryptoImpl(c CryptoImpl) ServeOption {
	return func(cfg *serveConfig) { cfg.cryptoImpl = c }
}

// ServeExtensionWithTransport 兼容入口：等价 ServeExtension(impl, WithTransportImpl(transport))。
// ProvidesTransport=true 的声明要求 impl 同时实现 TransportImpl。
func ServeExtensionWithTransport(impl ExtensionImpl, transport TransportImpl) {
	ServeExtension(impl, WithTransportImpl(transport))
}
```

⑥ 非导出函数区块：Register 处理器中 `pb := &extproto.ExtensionDecl{...}` 增加 `ProvidesCrypto` 字段与降级保护：

```go
		pb := &extproto.ExtensionDecl{
			Metadata:          metadataToProto(d.Metadata),
			ProvidesTransport: d.ProvidesTransport,
			ProvidesCrypto:    d.ProvidesCrypto,
		}
		if s.transportImpl == nil && d.ProvidesTransport {
			// 声明与实现不符：降级为非 transport（忽略该能力声明）
			pb.ProvidesTransport = false
		}
		if s.cryptoImpl == nil && d.ProvidesCrypto {
			// 声明与实现不符：降级为非 crypto（忽略该能力声明）
			pb.ProvidesCrypto = false
		}
```

⑦ TransportClose 处理器之后追加两个 RPC handler：

```go
// Encrypt 处理加密 RPC
func (s *extensionServer) Encrypt(ctx context.Context, req *extproto.EncryptRequest) (*extproto.EncryptResponse, error) {
	if s.cryptoImpl == nil {
		return nil, ErrNoCrypto
	}
	ciphertext, err := s.cryptoImpl.Encrypt(ctx, req.GetPlaintext())
	if err != nil {
		return nil, err
	}
	return &extproto.EncryptResponse{Ciphertext: ciphertext}, nil
}

// Decrypt 处理解密 RPC
func (s *extensionServer) Decrypt(ctx context.Context, req *extproto.DecryptRequest) (*extproto.DecryptResponse, error) {
	if s.cryptoImpl == nil {
		return nil, ErrNoCrypto
	}
	plaintext, err := s.cryptoImpl.Decrypt(ctx, req.GetCiphertext())
	if err != nil {
		return nil, err
	}
	return &extproto.DecryptResponse{Plaintext: plaintext}, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/swarm/extensions/plugin/ -cover -count=1`
Expected: 全部 PASS（含既有测试），覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/extensions/plugin/serve.go internal/swarm/extensions/plugin/serve_test.go
git commit -m "feat(extensions): 10.5.10 插件侧 CryptoImpl + ServeOption 函数式选项 + 降级保护"
```

---

### Task 4: 宿主侧 PluginClient.Encrypt/Decrypt RPC 封装 + 声明映射

**Files:**
- Modify: `internal/swarm/extensions/plugin/host.go`
- Test: `internal/swarm/extensions/plugin/host_test.go`

- [ ] **Step 1: 写失败测试**

`internal/swarm/extensions/plugin/host_test.go` 末尾追加：

```go
// TestPluginClient_CryptoRoundtrip 测试 Encrypt/Decrypt RPC 往返（进程内 gRPC）
func TestPluginClient_CryptoRoundtrip(t *testing.T) {
	addr, stop := startTestProtoServer(t, &extensionServer{impl: &fakeExtension{}, cryptoImpl: &fakeCryptoImpl{}})
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	enc, err := pc.Encrypt(context.Background(), "secret")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc:secret" {
		t.Errorf("Encrypt = %q, 期望 enc:secret", enc)
	}
	dec, err := pc.Decrypt(context.Background(), "enc:secret")
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "secret" {
		t.Errorf("Decrypt = %q, 期望 secret", dec)
	}
}

// TestPluginClient_Crypto_未实现 测试 cryptoImpl 缺失时 RPC 报错
func TestPluginClient_Crypto_未实现(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	if _, err := pc.Encrypt(context.Background(), "x"); err == nil {
		t.Error("cryptoImpl 缺失时 Encrypt 应报错")
	}
	if _, err := pc.Decrypt(context.Background(), "x"); err == nil {
		t.Error("cryptoImpl 缺失时 Decrypt 应报错")
	}
}

// TestPluginClient_Register_含Crypto声明 测试 ProvidesCrypto 声明往返保留
func TestPluginClient_Register_含Crypto声明(t *testing.T) {
	addr, stop := startTestProtoServer(t, &extensionServer{
		impl: &fakeExtension{decls: []Declaration{{
			Metadata:      Metadata{ID: "ext-crypto", Name: "crypto扩展", Version: "0.1.0"},
			ProvidesCrypto: true,
		}}},
		cryptoImpl: &fakeCryptoImpl{},
	})
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	decls, err := pc.Register(context.Background())
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if len(decls) != 1 || !decls[0].ProvidesCrypto {
		t.Errorf("ProvidesCrypto 声明应往返保留: %+v", decls)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/extensions/plugin/ -run "TestPluginClient_Crypto|TestPluginClient_Register_含Crypto" -v`
Expected: 编译失败 `pc.Encrypt undefined`

- [ ] **Step 3: 实现 host.go 修改**

① Register 方法中声明解析行改为（映射新字段）：

```go
		d := Declaration{ProvidesTransport: pb.GetProvidesTransport(), ProvidesCrypto: pb.GetProvidesCrypto()}
```

② HandleEvent 方法之后（RawProtoClient 之前）追加两个导出方法：

```go
// Encrypt 加密 RPC（crypto 提供者能力，10.5.10）
func (pc *PluginClient) Encrypt(ctx context.Context, plaintext string) (string, error) {
	resp, err := pc.protoClient.Encrypt(ctx, &extproto.EncryptRequest{Plaintext: plaintext})
	if err != nil {
		return "", err
	}
	return resp.GetCiphertext(), nil
}

// Decrypt 解密 RPC（crypto 提供者能力，10.5.10）
func (pc *PluginClient) Decrypt(ctx context.Context, ciphertext string) (string, error) {
	resp, err := pc.protoClient.Decrypt(ctx, &extproto.DecryptRequest{Ciphertext: ciphertext})
	if err != nil {
		return "", err
	}
	return resp.GetPlaintext(), nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/swarm/extensions/plugin/ -cover -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/extensions/plugin/host.go internal/swarm/extensions/plugin/host_test.go
git commit -m "feat(extensions): 10.5.10 宿主侧 PluginClient.Encrypt/Decrypt RPC 封装"
```

---

### Task 5: GRPCPluginCryptoProvider（宿主侧 crypto.CryptoProvider 插件代理）

**Files:**
- Create: `internal/swarm/extensions/plugin/crypto_provider.go`
- Test: `internal/swarm/extensions/plugin/crypto_provider_test.go`
- Modify: `internal/swarm/extensions/plugin/doc.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/swarm/extensions/plugin/crypto_provider_test.go`：

```go
package plugin

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// TestGRPCPluginCryptoProvider_接口满足 编译期断言满足 crypto.CryptoProvider
func TestGRPCPluginCryptoProvider_接口满足(t *testing.T) {
	var _ crypto.CryptoProvider = (*GRPCPluginCryptoProvider)(nil)
}

// TestGRPCPluginCryptoProvider_往返 进程内 gRPC 代理加解密
func TestGRPCPluginCryptoProvider_往返(t *testing.T) {
	addr, stop := startTestProtoServer(t, &extensionServer{impl: &fakeExtension{}, cryptoImpl: &fakeCryptoImpl{}})
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	p := NewGRPCPluginCryptoProvider(pc)

	enc, err := p.Encrypt("k1")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc:k1" {
		t.Errorf("Encrypt = %q, 期望 enc:k1", enc)
	}
	dec, err := p.Decrypt("enc:k1")
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "k1" {
		t.Errorf("Decrypt = %q, 期望 k1", dec)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/extensions/plugin/ -run TestGRPCPluginCryptoProvider -v`
Expected: 编译失败 `undefined: GRPCPluginCryptoProvider`

- [ ] **Step 3: 实现 crypto_provider.go**

创建 `internal/swarm/extensions/plugin/crypto_provider.go`：

```go
package plugin

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GRPCPluginCryptoProvider 扩展插件 crypto 的宿主侧代理，实现 crypto.CryptoProvider。
//
// 将 Encrypt/Decrypt 映射到插件 gRPC 的两个 RPC（10.5.10）。
// 对齐 Python get_crypto() 返回的 CryptoProvider 生态位；每次调用一次 gRPC 往返
// （设计偏差 #2：子进程架构固有代价，config 面板低频路径可接受）。
type GRPCPluginCryptoProvider struct {
	// pc 插件客户端
	pc *PluginClient
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：GRPCPluginCryptoProvider 满足 crypto.CryptoProvider 接口
var _ crypto.CryptoProvider = (*GRPCPluginCryptoProvider)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGRPCPluginCryptoProvider 创建插件 crypto 代理
func NewGRPCPluginCryptoProvider(pc *PluginClient) *GRPCPluginCryptoProvider {
	return &GRPCPluginCryptoProvider{pc: pc}
}

// Encrypt 加密明文（代理到插件 Encrypt RPC）
func (p *GRPCPluginCryptoProvider) Encrypt(plaintext string) (string, error) {
	return p.pc.Encrypt(context.Background(), plaintext)
}

// Decrypt 解密密文（代理到插件 Decrypt RPC）
func (p *GRPCPluginCryptoProvider) Decrypt(ciphertext string) (string, error) {
	return p.pc.Decrypt(context.Background(), ciphertext)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 更新 plugin/doc.go**

`internal/swarm/extensions/plugin/doc.go`：包概述追加一行 `- 宿主侧：GRPCPluginCryptoProvider（crypto.CryptoProvider 插件代理，10.5.10）`；文件目录中 proto 行改为 `# 协议定义（9 个 RPC）`，并在 transport_proxy.go 行后插入：

```
//	├── crypto_provider.go    # GRPCPluginCryptoProvider 实现 crypto.CryptoProvider
```

- [ ] **Step 5: 运行测试验证通过**

Run: `go test ./internal/swarm/extensions/plugin/ -cover -count=1`
Expected: 全部 PASS，覆盖率 ≥85%

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/extensions/plugin/crypto_provider.go internal/swarm/extensions/plugin/crypto_provider_test.go internal/swarm/extensions/plugin/doc.go
git commit -m "feat(extensions): 10.5.10 GRPCPluginCryptoProvider（crypto.CryptoProvider 插件代理）"
```

---

### Task 6: registry any→crypto.CryptoProvider 收紧 + sdk stub 删除 + doc 同步

**Files:**
- Modify: `internal/swarm/extensions/registry.go`
- Modify: `internal/swarm/extensions/registry_test.go`
- Modify: `internal/swarm/extensions/stub_test.go`（仅追加 fakeCryptoProvider）
- Delete: `internal/swarm/extensions/sdk/crypto_utility.go` + `internal/swarm/extensions/sdk/crypto_utility_test.go`
- Modify: `internal/swarm/extensions/sdk/doc.go` + `internal/swarm/extensions/doc.go`

- [ ] **Step 1: 写失败测试**

① `internal/swarm/extensions/registry_test.go`：将 `testCryptoUtilityExt` 定义（文件尾部区域）改为带 crypto 字段的版本：

```go
// testCryptoUtilityExt 测试用的 CryptoUtilityExtension 实现
type testCryptoUtilityExt struct {
	crypto crypto.CryptoProvider
}

func (e *testCryptoUtilityExt) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}
func (e *testCryptoUtilityExt) Shutdown(ctx context.Context) error { return nil }
func (e *testCryptoUtilityExt) Metadata() *ExtensionMetadata       { return nil }
func (e *testCryptoUtilityExt) SetExtensionDir(path string)        {}
func (e *testCryptoUtilityExt) GetCrypto() crypto.CryptoProvider   { return e.crypto }
```

registry_test.go 头部 import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② `TestExtensionRegistry_RegisterCryptoUtility` 中"GetCryptoProvider 调用 GetCrypto"段改为非 nil 断言：

```go
	// GetCryptoProvider 调用 GetCrypto（stub 返回 nil）
	provider := reg.GetCryptoProvider()
	if provider != nil {
		t.Error("GetCryptoProvider() should return nil for stub (GetCrypto returns nil)")
	}
```
（此段不变——testCryptoUtilityExt 零值 crypto 为 nil）

③ 末尾追加新测试：

```go
// TestExtensionRegistry_RegisterCryptoUtility_带provider 注册后 GetCryptoProvider 返回 provider
func TestExtensionRegistry_RegisterCryptoUtility_带provider(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	fake := &fakeCryptoProvider{}
	reg.RegisterCryptoUtility(&testCryptoUtilityExt{crypto: fake})

	if reg.GetCryptoProvider() != fake {
		t.Error("GetCryptoProvider 应返回注册的 provider")
	}
}
```

④ `internal/swarm/extensions/stub_test.go` 结构体区块追加共享 fake：

```go
// fakeCryptoProvider 测试用 crypto 提供者（extensions 包共享，loader/registry 测试均用）
type fakeCryptoProvider struct{}

func (f *fakeCryptoProvider) Encrypt(plaintext string) (string, error) { return "enc:" + plaintext, nil }
func (f *fakeCryptoProvider) Decrypt(ciphertext string) (string, error) { return "dec:" + ciphertext, nil }
```

stub_test.go 头部 import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/extensions/ -run TestExtensionRegistry_RegisterCryptoUtility -v`
Expected: 编译失败（`GetCrypto() any` 与 `crypto.CryptoProvider` 不匹配）

- [ ] **Step 3: 实现 registry.go 收紧**

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② `CryptoUtilityExtension` 接口替换为：

```go
// CryptoUtilityExtension CryptoUtility 扩展本地接口
// Python: CryptoUtility（在 sdk 包定义），此处为打破循环依赖的等价接口
// 10.5.10 ✅：GetCrypto 返回 crypto.CryptoProvider（common/crypto 权威接口，带 error 签名）
type CryptoUtilityExtension interface {
	Initialize(ctx context.Context, config *ExtensionConfig) error
	Shutdown(ctx context.Context) error
	Metadata() *ExtensionMetadata
	SetExtensionDir(path string)
	// GetCrypto 返回实际执行加解密的提供者，对齐 Python get_crypto()
	GetCrypto() crypto.CryptoProvider
}
```

③ `cryptoUtil` 字段注释改为 `// cryptoUtil 加解密扩展（10.5.10 ✅）`。

④ `RegisterCryptoUtility` 注释去掉 ⤵️ 行（保留 Python 对齐注释）。

⑤ `GetCryptoUtilityExtension` 注释去掉 ⤵️ 行。

⑥ `GetCryptoProvider` 替换为：

```go
// GetCryptoProvider 获取 CryptoProvider，对齐 Python get_crypto_provider()
// 10.5.10 ✅：返回 crypto.CryptoProvider；未注册扩展时返回 nil
func (r *ExtensionRegistry) GetCryptoProvider() crypto.CryptoProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cryptoUtil == nil {
		return nil
	}
	return r.cryptoUtil.GetCrypto()
}
```

- [ ] **Step 4: 删除 sdk stub（含测试）**

```bash
git rm internal/swarm/extensions/sdk/crypto_utility.go internal/swarm/extensions/sdk/crypto_utility_test.go
```

决策依据：`CryptoUtilityStub` 无任何消费方（死代码）；Go 无 Python importlib 同进程加载场景；插件作者 crypto 入口是 plugin 包 `CryptoImpl`；宿主内置不走 sdk 接口（设计决策 #4）。

- [ ] **Step 5: 同步 doc.go**

① `internal/swarm/extensions/sdk/doc.go` 整体替换为：

```go
// Package sdk 提供扩展 SDK 基类，对齐 Python jiuwenswarm/extensions/sdk/。
//
// 本包定义扩展开发的抽象接口：BaseExtension（基础扩展）、
// AgentServerClientExtension（AgentServer 客户端扩展）。
//
// 注意：宿主从不调用 Initialize（对齐 Python loader/manager 事实——仅
// register_extensions → set_extension_dir → shutdown 三步）。
//
// CryptoUtility 说明（10.5.10 ✅）：Go 无 Python importlib 同进程加载场景，
// sdk 不保留 CryptoUtility 接口（原 stub 已删除）；插件作者 crypto 入口为
// plugin 包 CryptoImpl，宿主侧注册接口为 extensions.CryptoUtilityExtension
// （GetCrypto 返回 common/crypto.CryptoProvider）。
//
// 文件目录：
//
//	sdk/
//	├── doc.go                 # 子包文档
//	├── base.go                # BaseExtension 接口 + BaseExtensionImpl 默认实现（10.5.4）
//	└── agent_server_client.go # AgentServerClientExtension 接口（10.5.5）
//
// 对应 Python 代码：jiuwenswarm/extensions/sdk/
package sdk
```

② `internal/swarm/extensions/doc.go`：
- 第 8 行"延后部分"句替换为：`// 延后部分：无。CallbackCompat 不需要（10.5.9 ⤴️）。CryptoUtility 已完成（10.5.10 ✅：宿主密钥文件 + 插件 provides_crypto 双路径）。`
- 概述段"（ExtensionLoader/ExtensionManager，方案 D：hashicorp go-plugin 子进程）"后追加一句：`crypto 扩展声明注册（provides_crypto → RegisterCryptoUtility + 全局覆盖）随 10.5.10 落地。`

- [ ] **Step 6: 运行测试验证通过**

Run: `go test ./internal/swarm/extensions/... -cover -count=1`
Expected: 全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/extensions/registry.go internal/swarm/extensions/registry_test.go internal/swarm/extensions/stub_test.go internal/swarm/extensions/sdk/doc.go internal/swarm/extensions/doc.go
git commit -m "refactor(extensions): 10.5.10 registry any→crypto.CryptoProvider 收紧 + sdk stub 删除"
```

---

### Task 7: loader crypto 分支编排（注册 + 全局覆盖，最后加载者胜）

**Files:**
- Modify: `internal/swarm/extensions/loader.go`
- Test: `internal/swarm/extensions/stub_test.go`

- [ ] **Step 1: 写失败测试**

`internal/swarm/extensions/stub_test.go`：

① `memPluginClient` 结构体追加字段与方法：

```go
type memPluginClient struct {
	setDirCalls   []string
	shutdownCalls []string
	handledEvents []string
	transport     transport.AgentTransport
	// cryptoProv 测试注入的 crypto 提供者（Crypto() 返回值）
	cryptoProv crypto.CryptoProvider
}

// Crypto 返回测试注入的 crypto 提供者（未注入时 nil）
func (m *memPluginClient) Crypto() crypto.CryptoProvider { return m.cryptoProv }
```

② 文件末尾追加四个测试：

```go
// TestExtensionLoader_RegisterDeclarations_Crypto 注册 crypto 声明 → registry + 全局覆盖
func TestExtensionLoader_RegisterDeclarations_Crypto(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()
	origGlobal := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(origGlobal)

	fake := &fakeCryptoProvider{}
	pc := &memPluginClient{cryptoProv: fake}
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extC": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata:      plugin.Metadata{ID: "extC", Name: "C", Version: "1.0"},
		ProvidesCrypto: true,
	}}
	loaded, err := loader.loadAndRegister(context.Background(), "/tmp/extC", pc, decls)
	if err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d, 期望 1", len(loaded))
	}
	if reg.GetCryptoUtilityExtension() == nil {
		t.Error("ProvidesCrypto 声明应注册 CryptoUtilityExtension")
	}
	if reg.GetCryptoProvider() != fake {
		t.Error("registry.GetCryptoProvider 应返回插件提供的 provider")
	}
	if crypto.GetCryptoProvider() != fake {
		t.Error("全局 provider 应被插件覆盖")
	}
}

// TestExtensionLoader_Crypto_覆盖宿主默认 宿主已设默认后被插件覆盖
func TestExtensionLoader_Crypto_覆盖宿主默认(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	origGlobal := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(origGlobal)
	hostDefault := &fakeCryptoProvider{}
	crypto.SetCryptoProvider(hostDefault)

	fake := &fakeCryptoProvider{}
	pc := &memPluginClient{cryptoProv: fake}
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extC": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata:      plugin.Metadata{ID: "extC", Name: "C", Version: "1.0"},
		ProvidesCrypto: true,
	}}
	if _, err := loader.loadAndRegister(context.Background(), "/tmp/extC", pc, decls); err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if crypto.GetCryptoProvider() != fake {
		t.Error("插件应覆盖宿主默认全局 provider（最后加载者胜）")
	}
}

// TestExtensionLoader_Crypto_无声明不动全局 无 ProvidesCrypto 声明时全局与 registry 不变
func TestExtensionLoader_Crypto_无声明不动全局(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	origGlobal := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(origGlobal)
	hostDefault := &fakeCryptoProvider{}
	crypto.SetCryptoProvider(hostDefault)

	pc := &memPluginClient{}
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extA": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata: plugin.Metadata{ID: "extA", Name: "A", Version: "1.0"},
	}}
	if _, err := loader.loadAndRegister(context.Background(), "/tmp/extA", pc, decls); err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if crypto.GetCryptoProvider() != hostDefault {
		t.Error("无 crypto 声明时全局 provider 不应变化")
	}
	if reg.GetCryptoUtilityExtension() != nil {
		t.Error("无 crypto 声明时不应注册 CryptoUtilityExtension")
	}
}

// TestExtensionLoader_Crypto_代理为nil不覆盖 声明 crypto 但客户端不支持时注册扩展但不覆盖全局
func TestExtensionLoader_Crypto_代理为nil不覆盖(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	origGlobal := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(origGlobal)
	hostDefault := &fakeCryptoProvider{}
	crypto.SetCryptoProvider(hostDefault)

	pc := &memPluginClient{} // Crypto() 返回 nil
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extC": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata:      plugin.Metadata{ID: "extC", Name: "C", Version: "1.0"},
		ProvidesCrypto: true,
	}}
	if _, err := loader.loadAndRegister(context.Background(), "/tmp/extC", pc, decls); err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if reg.GetCryptoUtilityExtension() == nil {
		t.Error("扩展仍应注册进 registry")
	}
	if crypto.GetCryptoProvider() != hostDefault {
		t.Error("GetCrypto 为 nil 时不应覆盖全局")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/extensions/ -run "TestExtensionLoader.*Crypto" -v`
Expected: 编译失败 `memPluginClient does not implement PluginClientFace`（缺 Crypto 方法）

- [ ] **Step 3: 实现 loader.go**

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② `PluginClientFace` 接口追加方法（Transport 之后）：

```go
	// Crypto 返回 crypto 提供者代理（ProvidesCrypto 声明的扩展；不支持时返回 nil）
	Crypto() crypto.CryptoProvider
```

③ `ExtensionLoader` 结构体注释中"provides_transport 声明 → registry.RegisterAgentServerClient（GRPCPluginTransport 代理）"后追加一行：
`//   - provides_crypto 声明 → registry.RegisterCryptoUtility + 覆盖全局 provider（最后加载者胜）`

④ `loadAndRegister` 中 transport 分支之后追加 crypto 分支：

```go
		if decl.ProvidesTransport {
			t := pc.Transport()
			if t != nil {
				l.registry.RegisterAgentServerClient(&pluginTransportExtension{client: pc, metadata: decl.Metadata})
			}
		}
		if decl.ProvidesCrypto {
			ext := &pluginCryptoExtension{client: pc, metadata: decl.Metadata}
			l.registry.RegisterCryptoUtility(ext)
			// 插件覆盖宿主默认全局（最后加载者胜，对齐 Python loader 顺序覆盖语义）
			if p := ext.GetCrypto(); p != nil {
				crypto.SetCryptoProvider(p)
				logger.Info(logComponent).
					Str("event_type", "extension_crypto_registered").
					Str("ext_id", decl.Metadata.ID).
					Msg("[ExtensionLoader] crypto 插件已接管全局加密提供者")
			}
		}
```

⑤ `pluginClientAdapter` 追加方法（Transport 之后）：

```go
func (a *pluginClientAdapter) Crypto() crypto.CryptoProvider {
	return plugin.NewGRPCPluginCryptoProvider(a.pc)
}
```

⑥ 文件末尾（pluginTransportExtension 之后）追加适配器：

```go
// pluginCryptoExtension CryptoUtilityExtension 的插件代理实现（registry.RegisterCryptoUtility 入参）。
// 结构对齐 pluginTransportExtension 先例：生命周期代理到插件 RPC，GetCrypto 返回宿主侧代理。
type pluginCryptoExtension struct {
	// client 插件客户端
	client PluginClientFace
	// metadata 扩展元数据
	metadata plugin.Metadata
}

// Initialize 扩展初始化（对齐 Python 事实：host 从不调用，空实现）
func (e *pluginCryptoExtension) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}

// Shutdown 代理到插件 Shutdown RPC
func (e *pluginCryptoExtension) Shutdown(ctx context.Context) error {
	return e.client.Shutdown(ctx)
}

// Metadata 返回扩展元数据（Register 声明携带）
func (e *pluginCryptoExtension) Metadata() *ExtensionMetadata {
	return &ExtensionMetadata{
		ID:                    e.metadata.ID,
		Name:                  e.metadata.Name,
		Version:               e.metadata.Version,
		Description:           e.metadata.Description,
		Author:                e.metadata.Author,
		MinJiuwenSwarmVersion: e.metadata.MinHostVersion,
		Dependencies:          e.metadata.Dependencies,
	}
}

// SetExtensionDir 代理到插件 SetExtensionDir RPC
func (e *pluginCryptoExtension) SetExtensionDir(path string) {
	_ = e.client.SetExtensionDir(context.Background(), path)
}

// GetCrypto 返回包着 PluginClient 的 GRPCPluginCryptoProvider（对齐 Python get_crypto()）
func (e *pluginCryptoExtension) GetCrypto() crypto.CryptoProvider {
	return e.client.Crypto()
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/swarm/extensions/... -cover -count=1`
Expected: 全部 PASS，覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/extensions/loader.go internal/swarm/extensions/stub_test.go
git commit -m "feat(extensions): 10.5.10 loader crypto 分支编排（注册 + 全局覆盖，最后加载者胜）"
```

---

### Task 8: web 消费点接线（统一 error 签名 + 全局解析 + 装配补线）

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/config_apply.go`
- Modify: `internal/swarm/gateway/channel_manager/web/web_handlers.go`
- Modify: `internal/swarm/gateway/app_gateway.go`
- Test: `internal/swarm/gateway/channel_manager/web/config_apply_test.go` + `web_handlers_test.go`

本任务一次性删除 web 本地无 error 的 `CryptoProvider` 接口、统一为 `common/crypto` 带 error 签名（必须同 commit 原子完成，否则编译不过）。失败语义统一为"跳过该字段 + Warn"（对齐 Python `except: pass`，设计偏差表）。

- [ ] **Step 1: 写失败测试**

① `internal/swarm/gateway/channel_manager/web/config_apply_test.go`：`fakeCrypto` 签名改为带 error：

```go
// fakeCrypto 用于测试的模拟加密提供者（带 error 签名，对齐 crypto.CryptoProvider）
type fakeCrypto struct{}

func (f *fakeCrypto) Encrypt(plaintext string) (string, error)  { return "enc(" + plaintext + ")", nil }
func (f *fakeCrypto) Decrypt(ciphertext string) (string, error) { return "dec(" + ciphertext + ")", nil }
```

文件末尾追加两个失败语义测试：

```go
// fakeCryptoEncryptFail Encrypt 恒失败的 fake（测试加密失败保留明文语义）
type fakeCryptoEncryptFail struct{}

func (f *fakeCryptoEncryptFail) Encrypt(plaintext string) (string, error) {
	return "", errors.New("encrypt boom")
}
func (f *fakeCryptoEncryptFail) Decrypt(ciphertext string) (string, error) {
	return "dec(" + ciphertext + ")", nil
}

// TestApplyConfigPayload_加密失败保留明文 加密失败时字段保留明文 + 不报错
func TestApplyConfigPayload_加密失败保留明文(t *testing.T) {
	_ = os.Unsetenv("API_KEY")
	defer func() { _ = os.Unsetenv("API_KEY") }()

	envUpdates, _, err := ApplyConfigPayload(map[string]any{"api_key": "plain"}, &fakeCryptoEncryptFail{})
	require.NoError(t, err)
	if envUpdates["API_KEY"] != "plain" {
		t.Errorf("API_KEY = %q, 期望加密失败时保留明文", envUpdates["API_KEY"])
	}
}

// TestMergeModelsForReplaceAll_加密失败保留明文 新条目加密失败时 api_key 保留明文
func TestMergeModelsForReplaceAll_加密失败保留明文(t *testing.T) {
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "plain",
			},
			"model_config_obj": map[string]any{"temperature": 0.95},
			"is_default":       true,
		},
	}
	result := mergeModelsForReplaceAll(parsed, nil, nil, &fakeCryptoEncryptFail{})
	require.Len(t, result, 1)
	mcc := result[0]["model_client_config"].(map[string]any)
	if mcc["api_key"] != "plain" {
		t.Errorf("api_key = %v, 期望加密失败时保留明文", mcc["api_key"])
	}
}
```

（config_apply_test.go 头部 import 块改为——在 `"context"` 后插入 `"errors"`：）

```go
import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)
```

② `internal/swarm/gateway/channel_manager/web/web_handlers_test.go`：`fakeCryptoHandler` 签名改为带 error：

```go
// fakeCryptoHandler 用于测试的模拟加密提供者（带 error 签名，对齐 crypto.CryptoProvider）
type fakeCryptoHandler struct{}

func (f *fakeCryptoHandler) Encrypt(plaintext string) (string, error)  { return "enc(" + plaintext + ")", nil }
func (f *fakeCryptoHandler) Decrypt(ciphertext string) (string, error) { return "dec(" + ciphertext + ")", nil }
```

文件末尾追加三个测试：

```go
// fakeCryptoFailDecrypt Decrypt 恒失败的 fake（测试解密失败保留原值语义）
type fakeCryptoFailDecrypt struct{}

func (f *fakeCryptoFailDecrypt) Encrypt(plaintext string) (string, error) { return "enc(" + plaintext + ")", nil }
func (f *fakeCryptoFailDecrypt) Decrypt(ciphertext string) (string, error) {
	return "", errors.New("decrypt boom")
}

// TestHandleConfigGet_解密失败保留原值 解密失败时保留密文原值
func TestHandleConfigGet_解密失败保留原值(t *testing.T) {
	_ = os.Setenv("API_KEY", "rawcipher")
	defer func() { _ = os.Unsetenv("API_KEY") }()

	handler := handleConfigGet(&fakeCryptoFailDecrypt{})
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	if result["api_key"] != "rawcipher" {
		t.Errorf("api_key = %v, 期望解密失败时保留原值", result["api_key"])
	}
}

// TestHandleConfigGet_解密成功 注入 provider 时敏感字段解密返回
func TestHandleConfigGet_解密成功(t *testing.T) {
	_ = os.Setenv("API_KEY", "cipher")
	defer func() { _ = os.Unsetenv("API_KEY") }()

	handler := handleConfigGet(&fakeCryptoHandler{})
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	if result["api_key"] != "dec(cipher)" {
		t.Errorf("api_key = %v, 期望 dec(cipher)", result["api_key"])
	}
}

// TestHandleConfigGet_全局provider 注入为 nil 时回退查全局
func TestHandleConfigGet_全局provider(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)
	crypto.SetCryptoProvider(&fakeCryptoHandler{})

	_ = os.Setenv("API_KEY", "cipher")
	defer func() { _ = os.Unsetenv("API_KEY") }()

	handler := handleConfigGet(nil)
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	if result["api_key"] != "dec(cipher)" {
		t.Errorf("api_key = %v, 期望全局 provider 解密 dec(cipher)", result["api_key"])
	}
}
```

web_handlers_test.go 头部 import 块改为（追加 `"errors"` 与 crypto 包）：

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run "TestApplyConfigPayload_加密失败|TestHandleConfigGet_" -v`
Expected: 编译失败（fake 不满足旧无 error 接口）

- [ ] **Step 3: 实现 config_apply.go**

① 删除本地 `CryptoProvider` 接口定义（结构体区块第 21-29 行整段删除）。

② import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

③ `ApplyConfigPayload` 签名与加密段替换为：

```go
func ApplyConfigPayload(params map[string]any, cryptoProv crypto.CryptoProvider) (envUpdates map[string]string, yamlUpdated []string, err error) {
	// Python: _encrypt_config_params (L692-699):
	// 遍历 params，key 名包含 api_key 或 token 时加密。
	// 加密失败保留明文 + Warn（Go error 签名；对齐 Python 无 error 概念的宽容语义）
	if cryptoProv != nil {
		for key, val := range params {
			kl := strings.ToLower(key)
			if strings.Contains(kl, "api_key") || strings.Contains(kl, "token") {
				if strVal, ok := val.(string); ok && strVal != "" {
					if enc, encErr := cryptoProv.Encrypt(strVal); encErr == nil {
						params[key] = enc
					} else {
						logger.Warn(logComponentConfigApply).
							Str("key", key).
							Err(encErr).
							Msg("加密失败，保留明文写入")
					}
				}
			}
		}
	}
```

④ `buildModelsDefaultsFromFrontend` 签名改 `cryptoProv crypto.CryptoProvider`，函数体内 `mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults, crypto)` 改传 `cryptoProv`。

⑤ `mergeModelsForReplaceAll` 签名改 `cryptoProv crypto.CryptoProvider`；新增非导出辅助函数（放在 mergeModelsForReplaceAll 之前）：

```go
// encryptAPIKey 加密条目的 api_key（cryptoProv nil 或加密失败时保留明文 + Warn）
func encryptAPIKey(mcc map[string]any, cryptoProv crypto.CryptoProvider) {
	if cryptoProv == nil {
		return
	}
	apiKey, ok := mcc["api_key"].(string)
	if !ok || apiKey == "" {
		return
	}
	enc, err := cryptoProv.Encrypt(apiKey)
	if err != nil {
		logger.Warn(logComponentConfigApply).
			Err(err).
			Msg("api_key 加密失败，保留明文")
		return
	}
	mcc["api_key"] = enc
}
```

函数体内两处"新条目加密"（无 origin_index 分支 + 越界分支）替换为：

```go
			newEntry := utils.DeepCopyMap(entry)
			if mcc, ok := newEntry["model_client_config"].(map[string]any); ok {
				encryptAPIKey(mcc, cryptoProv)
			}
			result = append(result, newEntry)
			continue
```

（两处分支同样写法；删除原来的 `if crypto != nil { ... mcc["api_key"] = crypto.Encrypt(apiKey) ... }` 内联段）

⑥ 合并循环内 api_key 特殊分支替换为：

```go
					if !valuesMatch(parsedVal, resolvedVal) {
						// 前端值与解析值不同，覆写
						if k == "api_key" {
							// api_key 特殊处理：加密后写入（失败保留明文 + Warn）
							if apiKeyStr, ok := parsedVal.(string); ok && apiKeyStr != "" && cryptoProv != nil {
								if enc, encErr := cryptoProv.Encrypt(apiKeyStr); encErr == nil {
									mergedMCC[k] = enc
								} else {
									logger.Warn(logComponentConfigApply).Err(encErr).Msg("api_key 加密失败，保留明文")
									mergedMCC[k] = parsedVal
								}
							} else {
								mergedMCC[k] = parsedVal
							}
						} else {
							mergedMCC[k] = parsedVal
						}
					}
```

- [ ] **Step 4: 实现 web_handlers.go**

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② `WebHandlersBindParams.CryptoProvider` 字段类型改为：

```go
	// CryptoProvider 加密/解密提供者（注入通道，测试用；生产装配时传入全局快照）
	CryptoProvider crypto.CryptoProvider
```

③ 非导出函数区块追加解析函数（isSensitiveKey 附近）：

```go
// resolveCrypto 解析 crypto 提供者：注入优先（BindParams 测试通道），未注入时查全局。
// 对齐 Python 每次请求时取 ExtensionRegistry.get_instance().get_crypto_provider()——
// 插件加载后生效的语义由全局查找路径承接。
func resolveCrypto(injected crypto.CryptoProvider) crypto.CryptoProvider {
	if injected != nil {
		return injected
	}
	return crypto.GetCryptoProvider()
}
```

④ `RegisterWebHandlers` 内 `var cryptoProv CryptoProvider` 改为 `var cryptoProv crypto.CryptoProvider`（bind 提取逻辑不变）。

⑤ `handleConfigGet` 签名改 `cryptoProv crypto.CryptoProvider`，闭包开头加 `p := resolveCrypto(cryptoProv)`，环境变量读取解密段替换为：

```go
		// 从环境变量映射读取
		for key, envVar := range configEnvMap {
			val := os.Getenv(envVar)
			// Python: 对包含 api_key 或 token 的键解密（app_web_handlers.py L611-614）
			// 解密失败保留原值 + Warn（Go error 签名；Python 整体 except 降级 → Go 逐字段跳过）
			if p != nil && val != "" && isSensitiveKey(key) {
				if dec, decErr := p.Decrypt(val); decErr == nil {
					val = dec
				} else {
					logger.Warn(logComponent).
						Str("key", key).
						Err(decErr).
						Msg("config.get decrypt failed, keep raw value")
				}
			}
			result[key] = val
		}
```

⑥ `handleConfigSet` 签名改 `cryptoProv crypto.CryptoProvider`，闭包开头加 `p := resolveCrypto(cryptoProv)`，`ApplyConfigPayload(params, cryptoProv)` 改为 `ApplyConfigPayload(params, p)`。

⑦ `handleConfigSaveAll` 签名改 `cryptoProv crypto.CryptoProvider`，闭包开头加 `p := resolveCrypto(cryptoProv)`，函数体内两处 `cryptoProv` 传参（`buildModelsDefaultsFromFrontend(modelsVal, cryptoProv)` 与 `ApplyConfigPayload(configParams, cryptoProv)`）均改传 `p`。

⑧ `handleModelsList` 签名改 `cryptoProv crypto.CryptoProvider`，闭包开头加 `p := resolveCrypto(cryptoProv)`，api_key 解密段替换为：

```go
			if v, ok := mcc["api_key"].(string); ok {
				// Python: crypto.decrypt(v)（app_web_handlers.py L857-866 语义）
				// 解密失败保留原值 + Warn
				if p != nil && v != "" {
					if dec, decErr := p.Decrypt(v); decErr == nil {
						flat["api_key"] = dec
					} else {
						logger.Warn(logComponent).
							Int("origin_index", idx).
							Err(decErr).
							Msg("models.list decrypt failed, keep raw value")
						flat["api_key"] = v
					}
				} else {
					flat["api_key"] = v
				}
			}
```

⑨ `handleModelsReplaceAll` 签名改 `cryptoProv crypto.CryptoProvider`，闭包开头加 `p := resolveCrypto(cryptoProv)`，`buildModelsDefaultsFromFrontend(modelsVal, cryptoProv)` 改传 `p`。

- [ ] **Step 5: 实现 app_gateway.go 装配补线**

`internal/swarm/gateway/app_gateway.go`：

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② `RegisterWebHandlers(&web.WebHandlersBindParams{...})` 装配追加字段（修复恒 nil 接线缺失；此时扩展已加载完毕，全局已是最终 provider）：

```go
	web.RegisterWebHandlers(&web.WebHandlersBindParams{
		Channel:        wc,
		AgentClient:    agentClient,
		ChannelManager: channelMgr,
		OnConfigSaved:  onConfigSavedCb,
		CryptoProvider: crypto.GetCryptoProvider(),
	})
```

- [ ] **Step 6: 运行测试验证通过**

Run: `go test ./internal/swarm/gateway/... -cover -count=1`
Expected: 全部 PASS（含既有 fakeCrypto 用例断言不变——`enc(plaintext)` 值语义一致），覆盖率 ≥85%

Run: `go build ./...`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/config_apply.go internal/swarm/gateway/channel_manager/web/web_handlers.go internal/swarm/gateway/channel_manager/web/config_apply_test.go internal/swarm/gateway/channel_manager/web/web_handlers_test.go internal/swarm/gateway/app_gateway.go
git commit -m "feat(gateway): 10.5.10 web 消费点接线（统一 error 签名 + 全局解析 + 装配补线）"
```

---

### Task 9: config decryptModelEntries 实装（回填点 ⑥）

**Files:**
- Modify: `internal/common/config/default_models.go`
- Test: `internal/common/config/default_models_test.go`

- [ ] **Step 1: 写失败测试**

`internal/common/config/default_models_test.go` 头部 import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`，并定义测试密钥（全局变量区块）：

```go
// testCryptoKey 32 字节测试密钥（AES-256）
var testCryptoKey = []byte("0123456789abcdef0123456789abcdef")
```

文件末尾追加三个测试：

```go
// TestDecryptModelEntries_解密api_key 全局 provider 就绪时 api_key 解密返回
func TestDecryptModelEntries_解密api_key(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)

	provider, err := crypto.NewAesGcmProvider(testCryptoKey)
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	crypto.SetCryptoProvider(provider)

	ciphertext, err := provider.Encrypt("sk-secret")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": ciphertext, "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "sk-secret" {
		t.Errorf("api_key = %v, 期望解密为 sk-secret", got)
	}
	// 深拷贝：原 entries 不被修改
	orig := entries[0]["model_client_config"].(map[string]any)["api_key"]
	if orig != ciphertext {
		t.Errorf("原始条目被修改: %v", orig)
	}
}

// TestDecryptModelEntries_无provider 全局 nil 时 api_key 保持原值
func TestDecryptModelEntries_无provider(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)
	crypto.SetCryptoProvider(nil)

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": "plain", "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "plain" {
		t.Errorf("api_key = %v, 期望无 provider 时保持 plain", got)
	}
}

// TestDecryptModelEntries_解密失败保留原值 密文无效时保留原值（对齐 Python except: pass）
func TestDecryptModelEntries_解密失败保留原值(t *testing.T) {
	original := crypto.GetCryptoProvider()
	defer crypto.SetCryptoProvider(original)

	provider, err := crypto.NewAesGcmProvider(testCryptoKey)
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	crypto.SetCryptoProvider(provider)

	entries := []map[string]any{{
		"model_client_config": map[string]any{"api_key": "not-a-ciphertext", "model_name": "m"},
	}}
	result := decryptModelEntries(entries)

	got := result[0]["model_client_config"].(map[string]any)["api_key"]
	if got != "not-a-ciphertext" {
		t.Errorf("api_key = %v, 期望解密失败时保留原值", got)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/common/config/ -run TestDecryptModelEntries -v`
Expected: `TestDecryptModelEntries_解密api_key` FAIL（当前为空操作，api_key 保持密文）；`解密失败保留原值` FAIL（密文未变则通过——空操作下恰好通过，属预期；以第一个用例失败为准）

- [ ] **Step 3: 实现 decryptModelEntries**

`internal/common/config/default_models.go`：

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② 函数 doc 注释与实现整体替换为（**同时修正错误注释**）：

```go
// decryptModelEntries 解密模型条目并推断 is_default，返回深拷贝。
// 对齐 Python: _decrypt_model_entries (L668-694)
//
// api_key 解密查全局 crypto provider（10.5.10 ✅ 实装）。
// 依赖方向说明（修正旧错误注释）：config→crypto 合法无环（crypto 仅依赖
// exception 为叶子；真正禁止的是 config→swarm/extensions，那条会环）。
// 解密失败保留原值（对齐 Python try/except pass）。
// custom_headers 经 ParseCustomHeaders 手动处理
// （NormalizeConfig 全项目尚未接线，无法依赖 Load 后处理）。
func decryptModelEntries(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(entries))
	provider := crypto.GetCryptoProvider()
	for _, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		if mcc, ok := copied["model_client_config"].(map[string]any); ok {
			// Python: if mcc.get("api_key") and crypto: mcc["api_key"] = crypto.decrypt(...)
			if key, ok := mcc["api_key"].(string); ok && key != "" && provider != nil {
				if dec, decErr := provider.Decrypt(key); decErr == nil {
					mcc["api_key"] = dec
				}
			}
			if rawHeaders, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(rawHeaders)
			}
		}
		result = append(result, copied)
	}
	return InferIsDefault(result)
}
```

（provider 在循环外取一次，对齐 Python `crypto = ...get_crypto_provider()` 的取值时机。）

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/common/config/ -cover -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/common/config/default_models.go internal/common/config/default_models_test.go
git commit -m "feat(config): 10.5.10 decryptModelEntries 实装（api_key 解密，修正错误注释）"
```

---

### Task 10: cmd 启动接线（宿主密钥初始化 + WithDecrypt）

**Files:**
- Modify: `cmd/uapclaw/cmd.go`
- Test: 无（启动装配路径，遵循 cmd.go build tag 惯例不单测；由 Task 11 e2e 与手动启动验证）

- [ ] **Step 1: 修改 runAppCmd**

① import 追加 `"github.com/uapclaw/uapclaw-go/internal/common/crypto"`。

② 函数头注释的启动步骤列表（第 175-182 行）更新为：

```go
//  1. ParseEarly        → 早期 dotenv（--dotenv/--name，在 PreRunE 中）
//  2. workspace.Prepare → workspace 自动初始化
//  3. logger.Setup      → 日志初始化（尽早，后续步骤的日志可写文件）
//  4. dotenv.Load       → 加载主 .env（~/.uapclaw/config/.env）
//  4.5 宿主密钥初始化    → crypto.key 加载/生成 + SetCryptoProvider（10.5.10）
//  5. config.New+Load   → 完整配置加载（WithDecrypt 解密 api_key/token）
//  6. ResetFlags        → 重置免费搜索运行时标志
```

③ dotenv.Load 段之后、config.New 之前插入（对齐设计启动时序：密钥就绪须早于首次 config 读取）：

```go
	// 4.5 宿主加密密钥初始化（10.5.10 双路径之宿主内置，须在 config.Load 之前）
	//     密钥文件不存在时自动生成（~/.uapclaw/crypto.key，0600，受 UAPCLAW_DATA_DIR 重定向）；
	//     失败仅 Warn 不阻断（等价 Python 无扩展时的明文路径，设计偏差表 #4）。
	cryptoKeyPath := filepath.Join(workspace.WorkspaceDir(), "crypto.key")
	if key, keyErr := crypto.LoadOrCreateKeyFile(cryptoKeyPath); keyErr != nil {
		logger.Warn(logComponentGateway).
			Str("key_path", cryptoKeyPath).
			Err(keyErr).
			Msg("宿主加密密钥初始化失败，api_key 加解密路径降级为明文")
	} else if provider, provErr := crypto.NewAesGcmProvider(key); provErr != nil {
		logger.Warn(logComponentGateway).
			Str("key_path", cryptoKeyPath).
			Err(provErr).
			Msg("AesGcmProvider 构造失败，api_key 加解密路径降级为明文")
	} else {
		crypto.SetCryptoProvider(provider)
	}
```

④ config.New 行改为（追加 WithDecrypt）：

```go
	// 5. 完整配置加载（等价 Python: get_config()）
	//    WithDecrypt：api_key/token 环境变量解密钩子（10.5.10，全局 provider 就绪后生效）
	cfg, err := config.New("",
		config.WithNormalize(config.NormalizeConfig),
		config.WithDecrypt(crypto.NewDecryptFunc()))
```

- [ ] **Step 2: 编译与全量回归**

```bash
go build ./...
go vet ./cmd/uapclaw/
```

Expected: 编译与 vet 通过。（NewDecryptFunc 闭包每次调用时读全局——插件后续覆盖对热重载生效，对齐设计"加载后生效"语义。）

- [ ] **Step 3: 提交**

```bash
git add cmd/uapclaw/cmd.go
git commit -m "feat(cmd): 10.5.10 宿主密钥初始化 + WithDecrypt 接线"
```

---

### Task 11: e2e——echo 插件 crypto 能力（真实子进程回环）

**Files:**
- Modify: `internal/swarm/extensions/plugin/testdata/echo/main.go`
- Test: `internal/swarm/extensions/plugin/e2e_test.go`

- [ ] **Step 1: 修改 echo 插件**

`internal/swarm/extensions/plugin/testdata/echo/main.go`：

① import 追加 `"strings"`。

② Register 声明追加 `ProvidesCrypto: true`：

```go
	return []plugin.Declaration{{
		Metadata: plugin.Metadata{
			ID: "echo-ext", Name: "回环扩展", Version: "0.1.0",
			Description: "端到端测试用", Author: "test",
		},
		EventSubs: []plugin.EventSubscription{
			{Event: "agent_server:memory_before_chat", Priority: 100},
			{Event: "gateway:before_chat_request", Priority: 200},
		},
		ProvidesTransport: true,
		ProvidesCrypto:    true,
	}}, nil
```

③ 结构体追加两个 crypto 方法（Close 之后）：

```go
// Encrypt 加密（回环测试用简单前后缀）
func (e *echoExtension) Encrypt(ctx context.Context, plaintext string) (string, error) {
	return "enc(" + plaintext + ")", nil
}

// Decrypt 解密（剥掉前后缀）
func (e *echoExtension) Decrypt(ctx context.Context, ciphertext string) (string, error) {
	return strings.TrimSuffix(strings.TrimPrefix(ciphertext, "enc("), ")"), nil
}
```

④ main 改为函数式选项（同时具备 transport + crypto）：

```go
func main() {
	e := newEchoExtension()
	plugin.ServeExtension(e,
		plugin.WithTransportImpl(e),
		plugin.WithCryptoImpl(e))
}
```

- [ ] **Step 2: 扩展 e2e 测试**

`internal/swarm/extensions/plugin/e2e_test.go` 的 `TestE2E_FullLifecycle` 中，`ProvidesTransport` 断言后追加：

```go
	if !decls[0].ProvidesCrypto {
		t.Error("应声明 ProvidesCrypto")
	}
```

`// Transport 回环` 段之前（Shutdown RPC 断言之前均可，放在 HandleEvent 段后）追加 crypto 往返段：

```go
	// Crypto RPC 往返（真实子进程 Encrypt/Decrypt）
	enc, err := pc.Encrypt(context.Background(), "topsecret")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc(topsecret)" {
		t.Errorf("Encrypt = %q, 期望 enc(topsecret)", enc)
	}
	dec, err := pc.Decrypt(context.Background(), enc)
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "topsecret" {
		t.Errorf("Decrypt = %q, 期望 topsecret", dec)
	}
```

- [ ] **Step 3: 运行 e2e 验证通过**

Run: `go test ./internal/swarm/extensions/plugin/ -run TestE2E_FullLifecycle -v -count=1`
Expected: PASS（含 crypto 声明与往返断言；TestMain 自动预编译 echo 插件）

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/extensions/plugin/testdata/echo/main.go internal/swarm/extensions/plugin/e2e_test.go
git commit -m "test(extensions): 10.5.10 e2e echo 插件 crypto 能力（真实子进程 Encrypt/Decrypt 回环）"
```

---

### Task 12: 收尾——IMPLEMENTATION_PLAN.md 回填 + 全量验证

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 回填计划表**

`IMPLEMENTATION_PLAN.md` 第 705 行（10.5.10 行）替换为：

```markdown
| 10.5.10 | ✅ | CryptoUtility | 双路径：宿主内置 AesGcmProvider（密钥文件 ~/.uapclaw/crypto.key 自动生成，0600）+ 插件 provides_crypto 声明（proto 7→9 RPC：Encrypt/Decrypt；loader 注册 + 覆盖全局，最后加载者胜）；registry any→crypto.CryptoProvider 收紧；web/config 消费点接线（error 签名统一 + 失败跳过字段）；sdk stub 删除 | `jiuwenswarm/extensions/sdk/crypto_utility.py` · `jiuwenswarm/common/security/base_crypto.py` |
```

- [ ] **Step 2: 全量构建 + 测试 + 覆盖率**

```bash
go build ./...
go test ./internal/common/crypto/ ./internal/common/config/ ./internal/swarm/extensions/... ./internal/swarm/gateway/... -cover -count=1
```

Expected: 全部 PASS；受影响包覆盖率均 ≥85%（crypto / config / extensions / extensions/plugin / extensions/sdk / gateway 系列）。

- [ ] **Step 3: 清理 ⤵️ 标记复查**

```bash
grep -rn "10.5.10" --include="*.go" internal/ | grep "⤵️"
```

Expected: 无输出（全部回填点已闭环）。如有残留，逐个清除后再继续。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 10.5.10 完成——计划表回填 ✅（双路径加解密 7 处回填点闭环）"
```

---

## 自查记录（Self-Review）

1. **Spec 覆盖**：设计文档第 10 节 17 项交付物 → Task 1(keyfile+doc) / Task 2(proto+regen) / Task 3(serve) / Task 4(host) / Task 5(crypto_provider+doc) / Task 6(registry+sdk删除+两 doc) / Task 7(loader) / Task 8(web 三件+app_gateway) / Task 9(default_models) / Task 10(cmd) / Task 12(IMPLEMENTATION_PLAN)；e2e echo → Task 11。crypto.go"微调无"无需任务。✅
2. **占位符扫描**：所有步骤含完整代码/命令，无 TBD/TODO/"适当处理"。✅
3. **类型一致性**：`CryptoImpl.Encrypt(ctx, string)(string, error)`（T3）↔ serve handler 调用一致；`PluginClient.Encrypt(ctx, string)(string, error)`（T4）↔ `GRPCPluginCryptoProvider`（T5）调用一致；`PluginClientFace.Crypto() crypto.CryptoProvider`（T7）↔ `pluginCryptoExtension.GetCrypto()`（T7）↔ `CryptoUtilityExtension.GetCrypto()`（T6）一致；web 侧 `cryptoProv crypto.CryptoProvider` + `resolveCrypto`（T8）贯穿五个 handler；fake 三处签名统一带 error。✅
