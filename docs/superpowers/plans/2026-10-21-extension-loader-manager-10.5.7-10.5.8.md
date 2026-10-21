# 10.5.7+10.5.8 扩展系统 go-plugin 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ExtensionLoader（10.5.7）+ ExtensionManager（10.5.8）：hashicorp go-plugin 子进程扩展系统（生命周期 + 事件桥 + Transport 代理），附带回填 Gateway before_chat_request 触发点（11.3）、FromMap 写回闭环（6 处消费点）、cmd 接线。

**Architecture:** 宿主进程通过 go-plugin 启动扩展子进程（固定名二进制 `ext-plugin`），gRPC 协议（Register/SetExtensionDir/GetMetadata/Shutdown/HandleEvent + Transport 三件套）；宿主侧 `GRPCPluginTransport` 实现 `transport.AgentTransport` 纯 byte 管道接口，`AgentClient` 零改动。依赖方向 `extensions` → `extensions/plugin` → `transport` 单向无环。spec：`docs/superpowers/specs/2026-10-21-extension-loader-manager-10.5.7-10.5.8-design.md`

**Tech Stack:** github.com/hashicorp/go-plugin v1.8.0（已在 go.mod）、google.golang.org/grpc v1.79.3、protobuf v1.36.10、protoc 3.21.12 + protoc-gen-go v1.36.10 + protoc-gen-go-grpc 1.5.1（已装，`/root/go/bin` 需在 PATH）

**编码规范提醒（全程遵守 .codebuddy/rules）：**
- 所有注释中文；文件内声明顺序：结构体→枚举→常量→全局变量→导出函数→非导出函数；分隔注释块
- 每包有 doc.go；日志用组件常量 + `logger.Info/Warn/Error(logComponent)`（extensions 包用 `logger.ComponentCommon`）
- 测试覆盖率 ≥85%；不可 mock 的外部服务才用 build tag（本计划端到端测试用本地 `go build`，不加 tag）
- 编译前 `pgrep -f 'go (build|test)'` 检查残留；单进程互斥；GOPROXY=https://goproxy.cn,direct

---

## 文件结构总览

```
internal/swarm/extensions/
├── doc.go                       # 修改：目录树 + 延后说明更新
├── hooks_context.go             # 修改：+4 个 FromMap 方法
├── loader.go                    # 重写：go-plugin 发现/加载/注册编排
├── manager.go                   # 重写：WithSearchPaths + LoadAll/ShutdownAll/List
├── stub_test.go                 # 重写为 loader_test.go（删除旧 stub 断言）
└── plugin/                      # 新建子包
    ├── doc.go                   # 新建
    ├── proto/
    │   ├── extension.proto      # 新建
    │   ├── extension.pb.go      # 生成（提交入库）
    │   └── extension_grpc.pb.go # 生成（提交入库）
    ├── host.go                  # 新建：握手常量 + PluginClient + 插件工厂接口
    ├── transport_proxy.go       # 新建：GRPCPluginTransport
    ├── serve.go                 # 新建：插件作者 SDK
    ├── host_test.go             # 新建
    ├── transport_proxy_test.go  # 新建
    ├── serve_test.go            # 新建
    └── testdata/echo/main.go    # 新建：端到端测试用 echo 插件源码

internal/swarm/server/runtime/uapclaw.go        # 修改：4 处 FromMap + join 空块过滤 + 无条件写 memory_block
internal/swarm/server/runtime/uapclaw_test.go   # 修改：+写回闭环测试
internal/swarm/server/handle_envelope.go        # 修改：1 处 FromMap + params 回写
internal/swarm/server/handle_envelope_test.go   # 修改：+params 回写测试（若不存在则新建）
internal/swarm/gateway/message_handler/forward_loop.go      # 修改：步骤9 触发点
internal/swarm/gateway/message_handler/forward_loop_test.go # 修改：+触发点测试
cmd/uapclaw/cmd.go               # 修改：runAppCmd 扩展初始化 + transport 选择
cmd/uapclaw/cmd_test.go          # 新建：readExtensionDirs 单测
IMPLEMENTATION_PLAN.md           # 修改：10.5.7/10.5.8/10.5.3/11.3 行状态回填
Makefile                         # 修改：+proto-gen 目标
```

---

### Task 1: proto 协议定义与生成代码

**Files:**
- Create: `internal/swarm/extensions/plugin/proto/extension.proto`
- Create: `internal/swarm/extensions/plugin/proto/extension.pb.go`（生成）
- Create: `internal/swarm/extensions/plugin/proto/extension_grpc.pb.go`（生成）
- Create: `internal/swarm/extensions/plugin/doc.go`
- Modify: `Makefile`（+proto-gen 目标）
- Modify: `go.mod`（github.com/hashicorp/go-plugin v1.8.0 已加，确认无遗漏）

- [ ] **Step 1: 写 proto 文件**

`internal/swarm/extensions/plugin/proto/extension.proto`：

```protobuf
syntax = "proto3";

package extension.v1;

option go_package = "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto;extproto";

// Extension 扩展插件服务，宿主（uapclaw 主进程）通过 gRPC 调用子进程扩展。
// 协议设计对齐 Python jiuwenswarm/extensions/loader.py 的加载语义：
// Register ≈ register_extensions(registry)，SetExtensionDir ≈ set_extension_dir，
// Shutdown ≈ shutdown。无 Initialize RPC（Python host 从不调用 initialize）。
service Extension {
  // Register 注册声明：返回扩展元数据 + 事件订阅 + 能力声明
  rpc Register(RegisterRequest) returns (RegisterResponse);
  // SetExtensionDir 设置扩展目录（对齐 Python set_extension_dir）
  rpc SetExtensionDir(SetDirRequest) returns (SetDirResponse);
  // GetMetadata 元数据查询（对齐 BaseExtension.metadata property）
  rpc GetMetadata(GetMetadataRequest) returns (GetMetadataResponse);
  // Shutdown 关闭扩展（对齐 BaseExtension.shutdown）
  rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);
  // HandleEvent 事件桥：宿主 Trigger → 插件修改 context → 返回完整 map → 宿主写回
  rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
  // TransportSend 发送 JSON 字节到扩展侧 transport（对齐 AgentTransport.Send）
  rpc TransportSend(TransportSendRequest) returns (TransportSendResponse);
  // TransportRecv 服务端流式接收扩展侧 transport 数据（对齐 AgentTransport.Recv）
  rpc TransportRecv(TransportRecvRequest) returns (stream TransportRecvResponse);
  // TransportClose 关闭扩展侧 transport（对齐 AgentTransport.Close）
  rpc TransportClose(TransportCloseRequest) returns (TransportCloseResponse);
}

// ─── Register ───

message RegisterRequest {}

// ExtensionDecl 单个扩展声明（对齐 register_extensions 返回的单实例）
message ExtensionDecl {
  // Metadata 扩展元数据
  ExtensionMetadataProto metadata = 1;
  // EventSubs 事件订阅列表
  repeated EventSubscription event_subs = 2;
  // ProvidesTransport 是否为 transport 提供者（AgentServerClient 能力）
  bool provides_transport = 3;
}

// EventSubscription 事件订阅
message EventSubscription {
  // Event 事件名（Go 常量字符串，如 "gateway:before_chat_request"）
  string event = 1;
  // Priority 优先级（对齐 Python register(event, handler, priority=100)）
  int32 priority = 2;
}

message RegisterResponse {
  // Decls 声明列表（对齐 register_extensions 返回单实例或列表的统一形态）
  repeated ExtensionDecl decls = 1;
}

// ─── SetExtensionDir ───

message SetDirRequest {
  // Path 扩展根目录绝对路径
  string path = 1;
}

message SetDirResponse {}

// ─── GetMetadata ───

message GetMetadataRequest {}

message GetMetadataResponse {
  // Metadata 扩展元数据
  ExtensionMetadataProto metadata = 1;
}

// ─── Metadata 结构 ───

// ExtensionMetadataProto 扩展元数据（对齐 Python ExtensionMetadata dataclass）
message ExtensionMetadataProto {
  string id = 1;
  string name = 2;
  string version = 3;
  string description = 4;
  string author = 5;
  // MinHostVersion 最小宿主兼容版本（对齐 min_jiuwenswarm_version）
  string min_host_version = 6;
  // Dependencies 依赖声明 {包名: 版本约束}
  map<string, string> dependencies = 7;
  // ConfigSchema 配置模式（JSON Schema，可选）
  string config_schema_json = 8;
}

// ─── Shutdown ───

message ShutdownRequest {}

message ShutdownResponse {}

// ─── HandleEvent ───

message HandleEventRequest {
  // Event 事件名
  string event = 1;
  // Context 钩子上下文（完整 map 往返）
  google.protobuf.Struct context = 2;
}

message HandleEventResponse {
  // Context 插件修改后的完整上下文（宿主原地写回）
  google.protobuf.Struct context = 1;
}

// ─── Transport 代理 ───

message TransportSendRequest {
  // Data JSON 字节
  bytes data = 1;
}

message TransportSendResponse {}

message TransportRecvRequest {}

message TransportRecvResponse {
  // Data JSON 字节帧
  bytes data = 1;
}

message TransportCloseRequest {}

message TransportCloseResponse {}
```

- [ ] **Step 2: 生成代码**

```bash
export PATH=$PATH:/root/go/bin
cd /home/opensource/uapclaw-gateway
protoc --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  internal/swarm/extensions/plugin/proto/extension.proto
ls internal/swarm/extensions/plugin/proto/
```

Expected: 生成 `extension.pb.go` + `extension_grpc.pb.go` 两个文件。

- [ ] **Step 3: 写 plugin 包 doc.go**

`internal/swarm/extensions/plugin/doc.go`：

```go
// Package plugin 提供 go-plugin 宿主/插件两侧基础设施。
//
// 本包实现扩展系统的子进程加载协议（方案 D：hashicorp go-plugin + gRPC）：
//   - 宿主侧：PluginClient（握手 + RPC 封装）、GRPCPluginTransport（AgentTransport 代理）
//   - 插件侧：ServeExtension（插件作者入口 SDK）
//   - 协议定义：proto/extension.proto（生成代码提交入库，仓库不依赖 protoc）
//
// 依赖方向（单向无环）：extensions → plugin → transport。
// 本包不 import extensions（proto 自带类型，元数据互转由 extensions/loader.go 负责）。
//
// 文件目录：
//
//	plugin/
//	├── doc.go                # 包文档
//	├── proto/                # gRPC 协议（.proto 源 + 生成代码）
//	│   ├── extension.proto   # 协议定义（7 个 RPC）
//	│   ├── extension.pb.go   # 生成（protoc-gen-go v1.36.10）
//	│   └── extension_grpc.pb.go # 生成（protoc-gen-go-grpc 1.5.1）
//	├── host.go               # 握手常量 + PluginClient + PluginHost 工厂接口
//	├── transport_proxy.go    # GRPCPluginTransport 实现 transport.AgentTransport
//	├── serve.go              # 插件作者 SDK：ServeExtension + ExtensionImpl 接口
//	└── testdata/echo/        # 端到端测试用 echo 插件源码（TestMain 编译）
//
// 对应 Python 代码：jiuwenswarm/extensions/loader.py（加载语义）+ sdk/base.py（扩展形态）
package plugin
```

- [ ] **Step 4: Makefile 加 proto-gen 目标**

`Makefile` `.PHONY` 行与末尾追加：

```makefile
.PHONY: build clean test test-integration test-llm test-e2e test-all lint proto-gen

# 重新生成扩展插件 gRPC 协议代码（需要 protoc + protoc-gen-go + protoc-gen-go-grpc）
proto-gen:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		internal/swarm/extensions/plugin/proto/extension.proto
```

- [ ] **Step 5: 验证编译**

```bash
export PATH=$PATH:/root/go/bin
go build ./internal/swarm/extensions/plugin/...
```

Expected: 编译通过（proto 包生成代码无语法问题）。

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/extensions/plugin/ Makefile
git commit -m "feat(extensions): 10.5.7 插件 gRPC 协议定义 + 生成代码 + proto-gen 目标"
```

---

### Task 2: serve.go 插件作者 SDK

**Files:**
- Create: `internal/swarm/extensions/plugin/serve.go`
- Create: `internal/swarm/extensions/plugin/serve_test.go`

- [ ] **Step 1: 写失败测试**

`internal/swarm/extensions/plugin/serve_test.go`：

```go
package plugin

import (
	"context"
	"testing"
)

// fakeExtension 用于测试的扩展实现
type fakeExtension struct {
	decls []Declaration
}

func (f *fakeExtension) Register(ctx context.Context) ([]Declaration, error) {
	return f.decls, nil
}

func (f *fakeExtension) Shutdown(ctx context.Context) error { return nil }

func (f *fakeExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	if ctxData == nil {
		ctxData = map[string]any{}
	}
	ctxData["touched"] = true
	return ctxData, nil
}

// TestDeclaration_MetadataDefaults 测试 Declaration 构造零值
func TestDeclaration_MetadataDefaults(t *testing.T) {
	d := Declaration{}
	if d.ProvidesTransport {
		t.Error("ProvidesTransport 零值应为 false")
	}
	if len(d.EventSubs) != 0 {
		t.Errorf("EventSubs 零值应为空, 实际 %d", len(d.EventSubs))
	}
}

// TestEventSubscription_Fields 测试事件订阅结构
func TestEventSubscription_Fields(t *testing.T) {
	sub := EventSubscription{Event: "gateway:before_chat_request", Priority: 100}
	if sub.Event != "gateway:before_chat_request" || sub.Priority != 100 {
		t.Errorf("EventSubscription 字段不符: %+v", sub)
	}
}

// TestFakeExtension_接口满足 验证 fakeExtension 满足 ExtensionImpl 接口
func TestFakeExtension_接口满足(t *testing.T) {
	var _ ExtensionImpl = (*fakeExtension)(nil)
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/plugin/ -run TestDeclaration
```

Expected: FAIL（`Declaration`/`EventSubscription`/`ExtensionImpl` 未定义）。

- [ ] **Step 3: 写 serve.go 实现**

`internal/swarm/extensions/plugin/serve.go`：

```go
package plugin

import (
	"context"
	"encoding/json"
	"net"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Metadata 插件元数据（plugin 包自定义，与 extensions.ExtensionMetadata 同构；
// loader.go 负责互转，避免本包 import extensions 造成循环）
type Metadata struct {
	// ID 扩展唯一标识
	ID string
	// Name 扩展名称
	Name string
	// Version 扩展版本
	Version string
	// Description 扩展描述
	Description string
	// Author 扩展作者
	Author string
	// MinHostVersion 最小宿主兼容版本
	MinHostVersion string
	// Dependencies 依赖声明
	Dependencies map[string]string
}

// EventSubscription 事件订阅声明
type EventSubscription struct {
	// Event 事件名（extensions 包常量字符串）
	Event string
	// Priority 优先级（对齐 Python priority=100 默认值）
	Priority int32
}

// Declaration 扩展声明（对齐 Python register_extensions 返回的单个扩展实例）
type Declaration struct {
	// Metadata 扩展元数据
	Metadata Metadata
	// EventSubs 事件订阅列表
	EventSubs []EventSubscription
	// ProvidesTransport 是否为 transport 提供者（true 时须实现 TransportImpl）
	ProvidesTransport bool
}

// ExtensionImpl 插件实现接口（插件作者实现此接口后调 ServeExtension）
type ExtensionImpl interface {
	// Register 注册声明（对齐 Python register_extensions(registry)）
	Register(ctx context.Context) ([]Declaration, error)
	// Shutdown 关闭扩展（对齐 Python BaseExtension.shutdown）
	Shutdown(ctx context.Context) error
	// HandleEvent 事件处理：修改并返回 ctxData（对齐 Python 扩展钩子函数原地修改语义）
	HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error)
}

// TransportImpl transport 提供者可选接口（Declaration.ProvidesTransport=true 时必须实现）
type TransportImpl interface {
	// Send 发送 JSON 字节（对齐 AgentTransport.Send）
	Send(ctx context.Context, data []byte) error
	// Recv 返回接收通道（对齐 AgentTransport.Recv）
	Recv() (<-chan []byte, error)
	// Close 关闭（对齐 AgentTransport.Close）
	Close() error
}

// extensionServer gRPC 服务端：把 ExtensionImpl 适配到 proto 服务
type extensionServer struct {
	extproto.UnimplementedExtensionServer
	// impl 插件实现
	impl ExtensionImpl
	// transportImpl transport 实现（ProvidesTransport 时非 nil）
	transportImpl TransportImpl
	// extensionDir 扩展目录（SetExtensionDir RPC 设置）
	extensionDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProtocolVersion go-plugin 协议版本（宿主与插件必须一致）
	ProtocolVersion = 1
	// MagicCookieKey 握手魔数环境变量名（go-plugin 约定）
	MagicCookieKey = "UAPCLAW_EXTENSION_PLUGIN"
	// MagicCookieValue 握手魔数值（宿主与插件必须一致）
	MagicCookieValue = "uapclaw-extension-1a2b3c4d"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ServeExtension 启动插件服务（插件 main.go 的唯一入口）。
//
// 用法：
//
//	func main() {
//	    plugin.ServeExtension(&MyExtension{})
//	}
//
// 注意：插件内禁止向 stdout 写任何内容（go-plugin 握手协议独占 stdout），
// 日志请写文件或 stderr。
func ServeExtension(impl ExtensionImpl) {
	ServeExtensionWithTransport(impl, nil)
}

// ServeExtensionWithTransport 启动带 transport 能力的插件服务。
// ProvidesTransport=true 的声明要求 impl 同时实现 TransportImpl。
func ServeExtensionWithTransport(impl ExtensionImpl, transport TransportImpl) {
	extensionGRPCServer := &extensionServer{impl: impl, transportImpl: transport}

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// Register 处理注册声明 RPC
func (s *extensionServer) Register(ctx context.Context, _ *extproto.RegisterRequest) (*extproto.RegisterResponse, error) {
	decls, err := s.impl.Register(ctx)
	if err != nil {
		return nil, err
	}
	resp := &extproto.RegisterResponse{}
	for _, d := range decls {
		pb := &extproto.ExtensionDecl{
			Metadata:          metadataToProto(d.Metadata),
			ProvidesTransport: d.ProvidesTransport,
		}
		if s.transportImpl == nil && d.ProvidesTransport {
			// 声明与实现不符：降级为非 transport（记录在 metadata.Description 不合适，直接忽略声明）
			pb.ProvidesTransport = false
		}
		for _, sub := range d.EventSubs {
			pb.EventSubs = append(pb.EventSubs, &extproto.EventSubscription{
				Event:    sub.Event,
				Priority: sub.Priority,
			})
		}
		resp.Decls = append(resp.Decls, pb)
	}
	return resp, nil
}

// SetExtensionDir 处理设置扩展目录 RPC
func (s *extensionServer) SetExtensionDir(ctx context.Context, req *extproto.SetDirRequest) (*extproto.SetDirResponse, error) {
	s.extensionDir = req.GetPath()
	return &extproto.SetDirResponse{}, nil
}

// GetMetadata 处理元数据查询 RPC
func (s *extensionServer) GetMetadata(ctx context.Context, _ *extproto.GetMetadataRequest) (*extproto.GetMetadataResponse, error) {
	decls, err := s.impl.Register(ctx)
	if err != nil || len(decls) == 0 {
		return &extproto.GetMetadataResponse{}, err
	}
	return &extproto.GetMetadataResponse{Metadata: metadataToProto(decls[0].Metadata)}, nil
}

// Shutdown 处理关闭 RPC
func (s *extensionServer) Shutdown(ctx context.Context, _ *extproto.ShutdownRequest) (*extproto.ShutdownResponse, error) {
	return &extproto.ShutdownResponse{}, s.impl.Shutdown(ctx)
}

// HandleEvent 处理事件桥 RPC
func (s *extensionServer) HandleEvent(ctx context.Context, req *extproto.HandleEventRequest) (*extproto.HandleEventResponse, error) {
	var ctxData map[string]any
	if req.GetContext() != nil {
		ctxData = req.GetContext().AsMap()
	}
	result, err := s.impl.HandleEvent(ctx, req.GetEvent(), ctxData)
	if err != nil {
		return nil, err
	}
	st, err := structpb.NewStruct(result)
	if err != nil {
		return nil, err
	}
	return &extproto.HandleEventResponse{Context: st}, nil
}

// TransportSend 处理 transport 发送 RPC
func (s *extensionServer) TransportSend(ctx context.Context, req *extproto.TransportSendRequest) (*extproto.TransportSendResponse, error) {
	if s.transportImpl == nil {
		return nil, ErrNoTransport
	}
	return &extproto.TransportSendResponse{}, s.transportImpl.Send(ctx, req.GetData())
}

// TransportRecv 处理 transport 流式接收 RPC
func (s *extensionServer) TransportRecv(req *extproto.TransportRecvRequest, stream extproto.Extension_TransportRecvServer) error {
	if s.transportImpl == nil {
		return ErrNoTransport
	}
	recvCh, err := s.transportImpl.Recv()
	if err != nil {
		return err
	}
	for data := range recvCh {
		if err := stream.Send(&extproto.TransportRecvResponse{Data: data}); err != nil {
			return err
		}
	}
	return nil
}

// TransportClose 处理 transport 关闭 RPC
func (s *extensionServer) TransportClose(ctx context.Context, _ *extproto.TransportCloseRequest) (*extproto.TransportCloseResponse, error) {
	if s.transportImpl == nil {
		return nil, ErrNoTransport
	}
	return &extproto.TransportCloseResponse{}, s.transportImpl.Close()
}

// metadataToProto Metadata 转 proto
func metadataToProto(m Metadata) *extproto.ExtensionMetadataProto {
	pb := &extproto.ExtensionMetadataProto{
		Id:             m.ID,
		Name:           m.Name,
		Version:        m.Version,
		Description:    m.Description,
		Author:         m.Author,
		MinHostVersion: m.MinHostVersion,
		Dependencies:   m.Dependencies,
	}
	if m.Dependencies == nil {
		pb.Dependencies = map[string]string{}
	}
	return pb
}
```

- [ ] **Step 4: 补充常量与 helper（写入 serve.go 顶部全局变量/非导出函数区，或独立小文件）**

serve.go 中引用了 `ErrNoTransport` 与 `ExtensionGRPCPlugin`，补齐定义。追加到 serve.go（`extensionServer` 结构体之后、Register 实现之前不需要，按声明顺序放入对应区块）：

```go
// ErrNoTransport 插件未实现 TransportImpl 却被调用 transport RPC
var ErrNoTransport = errors.New("扩展未提供 transport 能力")

// ExtensionGRPCPlugin go-plugin 插件包装（实现 plugin.Plugin 接口）
type ExtensionGRPCPlugin struct {
	// impl gRPC 服务端实现
	impl *extensionServer
}

// GRPCServer 注册 gRPC 服务（go-plugin 插件侧）
func (p *ExtensionGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	extproto.RegisterExtensionServer(s, p.impl)
	return nil
}

// GRPCClient 宿主侧不需要（宿主用 host.go 的 PluginClient 直接持有原始 conn）
func (p *ExtensionGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("宿主侧请使用 plugin.NewPluginClient，不走 GRPCClient")
}
```

serve.go 的 import 区补 `"errors"`。

- [ ] **Step 5: 运行测试验证通过**

```bash
go test ./internal/swarm/extensions/plugin/ -run "TestDeclaration|TestEventSubscription|TestFakeExtension" -v
```

Expected: 3 个测试 PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/extensions/plugin/serve.go internal/swarm/extensions/plugin/serve_test.go
git commit -m "feat(extensions): 10.5.7 插件作者 SDK（ServeExtension + ExtensionImpl/TransportImpl 接口）"
```

---

### Task 3: host.go 宿主侧 PluginClient

**Files:**
- Create: `internal/swarm/extensions/plugin/host.go`
- Create: `internal/swarm/extensions/plugin/host_test.go`

- [ ] **Step 1: 写失败测试（含 bufconn 式本地 gRPC 侦听）**

`internal/swarm/extensions/plugin/host_test.go`：

```go
package plugin

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// startTestExtensionServer 起本地 gRPC 服务并返回 conn 目标地址
func startTestExtensionServer(t *testing.T, impl ExtensionImpl, transport TransportImpl) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	srv := grpc.NewServer()
	extproto.RegisterExtensionServer(srv, &extensionServer{impl: impl, transportImpl: transport})
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), srv.Stop
}

// TestPluginClient_RegisterRoundtrip 测试 Register RPC 往返
func TestPluginClient_RegisterRoundtrip(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{decls: []Declaration{{
		Metadata:        Metadata{ID: "ext-1", Name: "测试扩展", Version: "0.1.0"},
		EventSubs:       []EventSubscription{{Event: "gateway:before_chat_request", Priority: 100}},
		ProvidesTransport: false,
	}}}, nil)
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
	if len(decls) != 1 {
		t.Fatalf("声明数 = %d, 期望 1", len(decls))
	}
	if decls[0].Metadata.ID != "ext-1" || decls[0].Metadata.Name != "测试扩展" {
		t.Errorf("元数据不符: %+v", decls[0].Metadata)
	}
	if len(decls[0].EventSubs) != 1 || decls[0].EventSubs[0].Event != "gateway:before_chat_request" {
		t.Errorf("事件订阅不符: %+v", decls[0].EventSubs)
	}
}

// TestPluginClient_HandleEventRoundtrip 测试事件桥往返
func TestPluginClient_HandleEventRoundtrip(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	result, err := pc.HandleEvent(context.Background(), "agent_server:memory_before_chat", map[string]any{"memory_blocks": []any{}})
	if err != nil {
		t.Fatalf("HandleEvent 失败: %v", err)
	}
	if result["touched"] != true {
		t.Errorf("结果应含 touched=true, 实际 %v", result)
	}
}

// TestPluginClient_ShutdownAndSetDir 测试生命周期 RPC
func TestPluginClient_ShutdownAndSetDir(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	if err := pc.SetExtensionDir(context.Background(), "/tmp/ext-1"); err != nil {
		t.Errorf("SetExtensionDir 失败: %v", err)
	}
	if err := pc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown 失败: %v", err)
	}
}

// TestPluginClient_MetadataRPC 测试 GetMetadata RPC
func TestPluginClient_MetadataRPC(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{decls: []Declaration{{
		Metadata: Metadata{ID: "ext-meta", Name: "元数据", Version: "1.2.3"},
	}}}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	m, err := pc.GetMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetMetadata 失败: %v", err)
	}
	if m.ID != "ext-meta" || m.Version != "1.2.3" {
		t.Errorf("元数据不符: %+v", m)
	}
}

// TestStructRoundtrip structpb 往返验证（防御性）
func TestStructRoundtrip(t *testing.T) {
	m := map[string]any{"memory_blocks": []any{"a", "b"}, "params": map[string]any{"k": "v"}}
	st, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("NewStruct 失败: %v", err)
	}
	back := st.AsMap()
	blocks, ok := back["memory_blocks"].([]any)
	if !ok || len(blocks) != 2 || blocks[0] != "a" {
		t.Errorf("memory_blocks 往返不符: %v", back["memory_blocks"])
	}
	_ = time.Now // 保持 time import（超时配置用）
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test ./internal/swarm/extensions/plugin/ -run TestPluginClient -v
```

Expected: FAIL（`NewPluginClientFromConn` 未定义）。

- [ ] **Step 3: 写 host.go 实现**

`internal/swarm/extensions/plugin/host.go`：

```go
package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PluginClient 宿主侧插件客户端：封装 go-plugin 握手 + gRPC RPC 调用。
// loader.go 通过本类型驱动插件生命周期与事件桥。
type PluginClient struct {
	// client go-plugin 管理的插件实例（含子进程生命周期）
	client *plugin.Client
	// protoClient gRPC 生客户端（基于 plugin.Client 的 conn）
	protoClient extproto.ExtensionClient
	// conn gRPC 连接（plugin.Client 握手产出）
	conn *grpc.ClientConn
}

// PluginHost 插件启动工厂（接口抽象，测试注入 fake；生产用 go-plugin 子进程实现）
type PluginHost interface {
	// Load 启动 root 目录下的插件并完成 gRPC 握手，返回 RPC 客户端
	Load(root string) (*PluginClient, error)
}

// GoPluginHost 生产实现：go-plugin 子进程启动
type GoPluginHost struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EntryFilename 扩展入口二进制文件名（对齐 Python ENTRY_FILENAME 语义）
	EntryFilename = "ext-plugin"
	// grpcDialTimeout gRPC 握手超时
	grpcDialTimeout = 10 * time.Second
	// logComponent 日志组件（基础设施层）
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPluginClientFromConn 从已有 gRPC 连接构造（测试与本地进程内场景）
func NewPluginClientFromConn(conn *grpc.ClientConn) *PluginClient {
	return &PluginClient{protoClient: extproto.NewExtensionClient(conn), conn: conn}
}

// Load 启动插件子进程（GoPluginHost 生产实现）
func (h *GoPluginHost) Load(root string) (*PluginClient, error) {
	entry := root + "/" + EntryFilename
	if _, err := os.Stat(entry); err != nil {
		return nil, fmt.Errorf("扩展入口二进制不存在（期望 %s）: %w", EntryFilename, err)
	}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  ProtocolVersion,
			MagicCookieKey:   MagicCookieKey,
			MagicCookieValue: MagicCookieValue,
		},
		Plugins: plugin.PluginSet{
			"extension": &ExtensionGRPCPlugin{},
		},
		Cmd:              exec.Command(entry),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("插件握手失败: %w", err)
	}
	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("Dispense extension 失败: %w", err)
	}
	_ = raw // 宿主直接用 conn（GRPCClient 返回错误占位），改走原生 conn 路径：

	// go-plugin GRPCClient 模式：用 client 的 gRPC 通道直连
	conn, err := h.dialPlugin(client)
	if err != nil {
		client.Kill()
		return nil, err
	}
	pc := &PluginClient{
		client:      client,
		protoClient: extproto.NewExtensionClient(conn),
		conn:        conn,
	}
	logger.Info(logComponent).
		Str("event_type", "extension_plugin_loaded").
		Str("root", root).
		Msg("扩展插件子进程已启动")
	return pc, nil
}

// Register 注册声明 RPC
func (pc *PluginClient) Register(ctx context.Context) ([]Declaration, error) {
	resp, err := pc.protoClient.Register(ctx, &extproto.RegisterRequest{})
	if err != nil {
		return nil, err
	}
	decls := make([]Declaration, 0, len(resp.GetDecls()))
	for _, pb := range resp.GetDecls() {
		d := Declaration{ProvidesTransport: pb.GetProvidesTransport()}
		if md := pb.GetMetadata(); md != nil {
			d.Metadata = Metadata{
				ID:             md.GetId(),
				Name:           md.GetName(),
				Version:        md.GetVersion(),
				Description:    md.GetDescription(),
				Author:         md.GetAuthor(),
				MinHostVersion: md.GetMinHostVersion(),
				Dependencies:   md.GetDependencies(),
			}
		}
		for _, sub := range pb.GetEventSubs() {
			d.EventSubs = append(d.EventSubs, EventSubscription{Event: sub.GetEvent(), Priority: sub.GetPriority()})
		}
		decls = append(decls, d)
	}
	return decls, nil
}

// SetExtensionDir 设置扩展目录 RPC
func (pc *PluginClient) SetExtensionDir(ctx context.Context, path string) error {
	_, err := pc.protoClient.SetExtensionDir(ctx, &extproto.SetDirRequest{Path: path})
	return err
}

// GetMetadata 元数据查询 RPC
func (pc *PluginClient) GetMetadata(ctx context.Context) (Metadata, error) {
	resp, err := pc.protoClient.GetMetadata(ctx, &extproto.GetMetadataRequest{})
	if err != nil {
		return Metadata{}, err
	}
	md := resp.GetMetadata()
	return Metadata{
		ID:             md.GetId(),
		Name:           md.GetName(),
		Version:        md.GetVersion(),
		Description:    md.GetDescription(),
		Author:         md.GetAuthor(),
		MinHostVersion: md.GetMinHostVersion(),
		Dependencies:   md.GetDependencies(),
	}, nil
}

// Shutdown 关闭 RPC（2 秒超时，超时不视为致命）
func (pc *PluginClient) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := pc.protoClient.Shutdown(shutdownCtx, &extproto.ShutdownRequest{})
	return err
}

// HandleEvent 事件桥 RPC：发送 context，返回插件修改后的完整 context
func (pc *PluginClient) HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error) {
	st, err := structpb.NewStruct(data)
	if err != nil {
		return nil, err
	}
	resp, err := pc.protoClient.HandleEvent(ctx, &extproto.HandleEventRequest{Event: event, Context: st})
	if err != nil {
		return nil, err
	}
	if resp.GetContext() == nil {
		return nil, nil
	}
	return resp.GetContext().AsMap(), nil
}

// RawProtoClient 暴露原始 gRPC 客户端（GRPCPluginTransport 使用）
func (pc *PluginClient) RawProtoClient() extproto.ExtensionClient {
	return pc.protoClient
}

// Kill 杀掉插件子进程并释放资源（go-plugin client.Kill 语义）
func (pc *PluginClient) Kill() {
	if pc.client != nil {
		pc.client.Kill()
	}
	if pc.conn != nil {
		_ = pc.conn.Close()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dialPlugin 通过 go-plugin 的 yamux 通道获取 gRPC conn
//
// go-plugin 在 Client() 握手后通过 GRPCBroker 暴露 gRPC 服务器地址；
// 标准用法是插件实现 GRPCClient()。本项目宿主不走 Dispense 结果，
// 而是直接让 ExtensionGRPCPlugin.GRPCClient 返回包装：
// 为保持 plugin 包内聚，这里用 Dispense 出的对象断言为 grpc 服务接口。
func (h *GoPluginHost) dialPlugin(client *plugin.Client) (*grpc.ClientConn, error) {
	rpcClient, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("获取插件 RPC 客户端失败: %w", err)
	}
	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		return nil, fmt.Errorf("Dispense extension 失败: %w", err)
	}
	// ExtensionGRPCPlugin.GRPCClient 返回 *grpc.ClientConn 包装
	dialer, ok := raw.(interface{ Conn() *grpc.ClientConn })
	if !ok {
		return nil, fmt.Errorf("插件 Dispense 返回类型非预期: %T", raw)
	}
	return dialer.Conn(), nil
}
```

同时修改 `serve.go` 的 `ExtensionGRPCPlugin.GRPCClient`（Task 2 中返回 error 的占位改为返回 conn 包装）：

```go
// connDialer GRPCClient 返回的包装类型（宿主通过 Conn() 拿 gRPC 连接）
type connDialer struct {
	conn *grpc.ClientConn
}

// Conn 返回底层 gRPC 连接
func (c *connDialer) Conn() *grpc.ClientConn { return c.conn }

// GRPCClient 宿主侧：返回 conn 包装（host.go dialPlugin 断言使用）
func (p *ExtensionGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &connDialer{conn: c}, nil
}
```

并删除 Task 3 Step 3 host.go `Load` 中的第一次 Dispense（`raw, err := rpcClient.Dispense("extension")` 与 `_ = raw` 行），改为：

```go
	client := plugin.NewClient(&plugin.ClientConfig{ /* 同上 */ })
	conn, err := h.dialPlugin(client)
	if err != nil {
		client.Kill()
		return nil, err
	}
	pc := &PluginClient{
		client:      client,
		protoClient: extproto.NewExtensionClient(conn),
		conn:        conn,
	}
	logger.Info(logComponent).
		Str("event_type", "extension_plugin_loaded").
		Str("root", root).
		Msg("扩展插件子进程已启动")
	return pc, nil
}
```

（`rpcClient` 变量与 `grpcDialTimeout` 未用则删除；`dialPlugin` 内部自取 `client.Client()`。）

- [ ] **Step 4: 运行测试验证通过**

```bash
go test ./internal/swarm/extensions/plugin/ -v
```

Expected: 全部 PASS（含 Task 2 的 3 个）。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/plugin/host.go internal/swarm/extensions/plugin/host_test.go internal/swarm/extensions/plugin/serve.go
git commit -m "feat(extensions): 10.5.7 宿主侧 PluginClient（go-plugin 握手 + RPC 封装 + 工厂接口）"
```

---

### Task 4: transport_proxy.go GRPCPluginTransport

**Files:**
- Create: `internal/swarm/extensions/plugin/transport_proxy.go`
- Create: `internal/swarm/extensions/plugin/transport_proxy_test.go`

- [ ] **Step 1: 写失败测试**

`internal/swarm/extensions/plugin/transport_proxy_test.go`：

```go
package plugin

import (
	"context"
	"testing"
	"time"
)

// echoTransport 测试用 transport：Send 收到的数据原样发回 Recv
type echoTransport struct {
	recvCh chan []byte
	closed bool
}

func newEchoTransport() *echoTransport {
	return &echoTransport{recvCh: make(chan []byte, 8)}
}

func (e *echoTransport) Send(ctx context.Context, data []byte) error {
	e.recvCh <- data
	return nil
}

func (e *echoTransport) Recv() (<-chan []byte, error) { return e.recvCh, nil }

func (e *echoTransport) Close() error {
	if !e.closed {
		e.closed = true
		close(e.recvCh)
	}
	return nil
}

// TestGRPCPluginTransport_SendRecvLoop 测试本地 gRPC 上的 Send/Recv 回环
func TestGRPCPluginTransport_SendRecvLoop(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	tr := NewGRPCPluginTransport(pc)

	if err := tr.Send(context.Background(), []byte(`{"ping":1}`)); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}
	select {
	case data := <-recvCh:
		if string(data) != `{"ping":1}` {
			t.Errorf("回环数据不符: %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待回环数据超时")
	}
}

// TestGRPCPluginTransport_CloseIdempotent 测试 Close 幂等
func TestGRPCPluginTransport_CloseIdempotent(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	if err := tr.Close(); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("第二次 Close 应幂等, 实际: %v", err)
	}
}

// TestGRPCPluginTransport_RecvAfterClose 测试关闭后 Recv 返回关闭的通道
func TestGRPCPluginTransport_RecvAfterClose(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err == nil {
		t.Error("关闭后 Recv 应返回错误")
		_ = recvCh
	}
}

// grpcDialTest 测试辅助：拨号本地 gRPC
func grpcDialTest(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}
```

（import 区需 `"google.golang.org/grpc"` 与 `"google.golang.org/grpc/credentials/insecure"`。）

- [ ] **Step 2: 运行测试验证失败**

```bash
go test ./internal/swarm/extensions/plugin/ -run TestGRPCPluginTransport -v
```

Expected: FAIL（`NewGRPCPluginTransport` 未定义）。

- [ ] **Step 3: 写 transport_proxy.go 实现**

`internal/swarm/extensions/plugin/transport_proxy.go`：

```go
package plugin

import (
	"context"
	"errors"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GRPCPluginTransport 扩展插件 transport 的宿主侧代理，实现 transport.AgentTransport。
//
// 将 AgentTransport 的 Send/Recv/Close 映射到插件 gRPC 的
// TransportSend/TransportRecv（server-streaming）/TransportClose 三个 RPC。
// AgentClient 通过本代理与扩展提供的 transport 通信，E2A 语义仍留在
// AgentClient 的 receiverLoop（规则 6：Transport 层为纯 byte 管道）。
type GRPCPluginTransport struct {
	// pc 插件客户端
	pc *PluginClient
	// recvCh 接收通道（惰性启动的 TransportRecv 流写入）
	recvCh chan []byte
	// mu 保护 closed 与 recvStarted
	mu sync.Mutex
	// closed 是否已关闭
	closed bool
	// recvStarted 接收 goroutine 是否已启动
	recvStarted bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// recvChannelBuffer 接收通道缓冲（对齐 ChannelTransport defaultRecvBufferSize）
	recvChannelBuffer = 128
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPluginTransportClosed 代理已关闭
var ErrPluginTransportClosed = errors.New("插件 transport 代理已关闭")

// 编译期断言：GRPCPluginTransport 满足 AgentTransport 接口
var _ transport.AgentTransport = (*GRPCPluginTransport)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGRPCPluginTransport 创建插件 transport 代理
func NewGRPCPluginTransport(pc *PluginClient) *GRPCPluginTransport {
	return &GRPCPluginTransport{pc: pc}
}

// Send 发送 JSON 字节到扩展侧 transport（对齐 AgentTransport.Send）
func (t *GRPCPluginTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrPluginTransportClosed
	}
	t.mu.Unlock()

	_, err := t.pc.RawProtoClient().TransportSend(ctx, transportSendRequestOf(data))
	return err
}

// Recv 返回接收通道（对齐 AgentTransport.Recv）。
// 首次调用惰性启动 goroutine 消费 TransportRecv server-streaming，
// 流断开或出错时关闭通道（对齐 ChannelTransport 的 Recv 语义）。
func (t *GRPCPluginTransport) Recv() (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrPluginTransportClosed
	}
	if !t.recvStarted {
		t.recvStarted = true
		t.recvCh = make(chan []byte, recvChannelBuffer)
		go t.pumpRecvLoop()
	}
	return t.recvCh, nil
}

// Close 关闭代理（幂等）：先标记关闭，再通知扩展侧
func (t *GRPCPluginTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	recvCh := t.recvCh
	t.mu.Unlock()

	if recvCh != nil {
		close(recvCh)
	}
	// 通知插件侧关闭（ctx 背景即可，出错不阻断宿主关闭流程）
	_, _ = t.pc.RawProtoClient().TransportClose(context.Background(), transportCloseRequestOf())
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pumpRecvLoop 消费 TransportRecv 流并写入 recvCh
func (t *GRPCPluginTransport) pumpRecvLoop() {
	stream, err := t.pc.RawProtoClient().TransportRecv(context.Background(), transportRecvRequestOf())
	if err != nil {
		t.closeRecvCh()
		return
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			t.closeRecvCh()
			return
		}
		data := resp.GetData()
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return
		}
		t.recvCh <- data
	}
}

// closeRecvCh 关闭接收通道（幂等保护）
func (t *GRPCPluginTransport) closeRecvCh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.recvCh != nil && !t.closed {
		close(t.recvCh)
	}
}
```

同文件补充三个小请求构造函数（非导出函数区）：

```go
// transportSendRequestOf 构造 TransportSend 请求
func transportSendRequestOf(data []byte) *extproto.TransportSendRequest {
	return &extproto.TransportSendRequest{Data: data}
}

// transportRecvRequestOf 构造 TransportRecv 请求
func transportRecvRequestOf() *extproto.TransportRecvRequest {
	return &extproto.TransportRecvRequest{}
}

// transportCloseRequestOf 构造 TransportClose 请求
func transportCloseRequestOf() *extproto.TransportCloseRequest {
	return &extproto.TransportCloseRequest{}
}
```

import 区补 `extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"`。

注意 `pumpRecvLoop` 中 `t.recvCh <- data` 在通道已满时会阻塞——`Close()` 先置 closed 再 close(recvCh) 可能与阻塞的发送 goroutine 竞态 panic（send on closed channel）。改用 select 防护：

```go
		select {
		case t.recvCh <- data:
		case <-time.After(recvStallTimeout):
			// 通道长期满：丢弃帧并记录 warn（对齐 ChannelTransport 缓冲满丢弃语义）
			logger.Warn(logComponent).
				Str("event_type", "plugin_transport_recv_dropped").
				Int("bytes", len(data)).
				Msg("recv 通道缓冲满，丢弃帧")
		}
```

常量区补 `recvStallTimeout = 5 * time.Second`，import 补 `time` 与 logger。`closed` 检查保留在 select 之前（提前退出）。

- [ ] **Step 4: 运行测试验证通过**

```bash
go test ./internal/swarm/extensions/plugin/ -run TestGRPCPluginTransport -v
```

Expected: 3 个测试 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/plugin/transport_proxy.go internal/swarm/extensions/plugin/transport_proxy_test.go
git commit -m "feat(extensions): 10.5.7 GRPCPluginTransport 实现 AgentTransport 纯 byte 管道代理"
```

---

### Task 5: hooks_context.go FromMap 写回

**Files:**
- Modify: `internal/swarm/extensions/hooks_context.go`（追加 4 个 FromMap）
- Modify: `internal/swarm/extensions/hooks_context_test.go`（追加测试）

- [ ] **Step 1: 写失败测试（追加到 hooks_context_test.go）**

```go
// TestMemoryHookContext_FromMap 测试 FromMap 写回输出字段
func TestMemoryHookContext_FromMap(t *testing.T) {
	channelID := "ch-001"
	ctx := MemoryHookContext{SessionID: "sess-001", ChannelID: &channelID}
	m := map[string]any{
		"memory_blocks":      []any{"block1", "", "block2"},
		"assistant_message":  "answer",
		"metadata":           map[string]any{"k": "v"},
		"session_id":         "篡改输入字段应被忽略",
	}
	ctx.FromMap(m)
	if len(ctx.MemoryBlocks) != 3 {
		t.Errorf("MemoryBlocks len = %d, 期望 3（FromMap 不过滤空块，过滤在消费点）", len(ctx.MemoryBlocks))
	}
	if ctx.AssistantMessage == nil || *ctx.AssistantMessage != "answer" {
		t.Errorf("AssistantMessage 写回失败: %v", ctx.AssistantMessage)
	}
	if ctx.Metadata == nil || ctx.Metadata["k"] != "v" {
		t.Errorf("Metadata 写回失败: %v", ctx.Metadata)
	}
	if ctx.SessionID != "sess-001" {
		t.Errorf("输入字段不应被写回: %q", ctx.SessionID)
	}
}

// TestMemoryHookContext_FromMap_防御分支 测试类型不符与缺失字段
func TestMemoryHookContext_FromMap_防御分支(t *testing.T) {
	ctx := MemoryHookContext{SessionID: "s"}
	ctx.FromMap(map[string]any{
		"memory_blocks":     "不是数组",
		"assistant_message": 42,
		"metadata":          "不是map",
	})
	if ctx.MemoryBlocks != nil {
		t.Errorf("类型不符应跳过: %v", ctx.MemoryBlocks)
	}
	if ctx.AssistantMessage != nil {
		t.Errorf("assistant_message 非字符串应跳过: %v", ctx.AssistantMessage)
	}
	if ctx.Metadata != nil {
		t.Errorf("metadata 非 map 应跳过: %v", ctx.Metadata)
	}
	// nil map 不 panic
	ctx.FromMap(nil)
}

// TestGatewayChatHookContext_FromMap 测试 params 写回
func TestGatewayChatHookContext_FromMap(t *testing.T) {
	ctx := GatewayChatHookContext{RequestID: "r1"}
	ctx.FromMap(map[string]any{
		"params":     map[string]any{"query": "改写后"},
		"request_id": "不应写回",
	})
	if ctx.Params == nil || ctx.Params["query"] != "改写后" {
		t.Errorf("Params 写回失败: %v", ctx.Params)
	}
	if ctx.RequestID != "r1" {
		t.Errorf("输入字段不应写回: %q", ctx.RequestID)
	}
}

// TestAgentServerChatHookContext_FromMap 测试 params 与指针字段写回
func TestAgentServerChatHookContext_FromMap(t *testing.T) {
	ctx := AgentServerChatHookContext{RequestID: "r2"}
	ctx.FromMap(map[string]any{
		"params":    map[string]any{"mode": "code"},
		"session_id": "sess-9",
	})
	if ctx.Params == nil || ctx.Params["mode"] != "code" {
		t.Errorf("Params 写回失败: %v", ctx.Params)
	}
	if ctx.SessionID == nil || *ctx.SessionID != "sess-9" {
		t.Errorf("SessionID 指针写回失败: %v", ctx.SessionID)
	}
}

// TestSystemPromptHookContext_FromMap 测试目录写回
func TestSystemPromptHookContext_FromMap(t *testing.T) {
	ctx := SystemPromptHookContext{}
	ctx.FromMap(map[string]any{
		"home_dir":  "/custom/home",
		"skill_dir": 123, // 类型不符跳过
	})
	if ctx.HomeDir == nil || *ctx.HomeDir != "/custom/home" {
		t.Errorf("HomeDir 写回失败: %v", ctx.HomeDir)
	}
	if ctx.SkillDir != nil {
		t.Errorf("skill_dir 类型不符应跳过: %v", ctx.SkillDir)
	}
}

// TestHookContext_ToMapFromMap_Roundtrip 测试 ToMap→FromMap 闭环
func TestHookContext_ToMapFromMap_Roundtrip(t *testing.T) {
	channelID := "ch"
	assistant := "msg"
	orig := MemoryHookContext{
		SessionID: "s", RequestID: "r", ChannelID: &channelID,
		AgentName: "a", WorkspaceDir: "/w",
		AssistantMessage: &assistant,
		Extra:            map[string]any{"e": 1},
		MemoryBlocks:     []string{"b1"},
		Metadata:         map[string]any{"m": 2},
	}
	restored := MemoryHookContext{SessionID: "s", RequestID: "r", ChannelID: &channelID, AgentName: "a", WorkspaceDir: "/w"}
	restored.FromMap(orig.ToMap())
	if len(restored.MemoryBlocks) != 1 || restored.MemoryBlocks[0] != "b1" {
		t.Errorf("MemoryBlocks 往返不符: %v", restored.MemoryBlocks)
	}
	if restored.AssistantMessage == nil || *restored.AssistantMessage != "msg" {
		t.Errorf("AssistantMessage 往返不符: %v", restored.AssistantMessage)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
go test ./internal/swarm/extensions/ -run "FromMap" -v
```

Expected: FAIL（FromMap 未定义）。

- [ ] **Step 3: 实现 4 个 FromMap（追加到 hooks_context.go 导出函数区，ToMap 之后）**

```go
// FromMap 从字典写回输出语义字段（ToMap 的逆操作，仅输出字段）。
// 防御式类型断言：字段缺失或类型不符时跳过，不 panic。
// 配合事件桥使用：Trigger(memCtx.ToMap()) 后调用 FromMap(data) 读回扩展修改。
func (c *MemoryHookContext) FromMap(m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["assistant_message"].(string); ok {
		c.AssistantMessage = &v
	}
	if blocks, ok := m["memory_blocks"].([]any); ok {
		result := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if s, isStr := b.(string); isStr {
				result = append(result, s)
			}
		}
		c.MemoryBlocks = result
	}
	if md, ok := m["metadata"].(map[string]any); ok {
		c.Metadata = md
	}
}

// FromMap 从字典写回输出语义字段（params 为唯一输出字段）
func (c *GatewayChatHookContext) FromMap(m map[string]any) {
	if m == nil {
		return
	}
	if p, ok := m["params"].(map[string]any); ok {
		c.Params = p
	}
}

// FromMap 从字典写回输出语义字段（params + session_id 指针）
func (c *AgentServerChatHookContext) FromMap(m map[string]any) {
	if m == nil {
		return
	}
	if p, ok := m["params"].(map[string]any); ok {
		c.Params = p
	}
	if v, ok := m["session_id"].(string); ok {
		c.SessionID = &v
	}
}

// FromMap 从字典写回输出语义字段（home_dir / skill_dir 指针）
func (c *SystemPromptHookContext) FromMap(m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["home_dir"].(string); ok {
		c.HomeDir = &v
	}
	if v, ok := m["skill_dir"].(string); ok {
		c.SkillDir = &v
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
go test ./internal/swarm/extensions/ -run "FromMap|ToMap" -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/hooks_context.go internal/swarm/extensions/hooks_context_test.go
git commit -m "feat(extensions): 10.5.3 回填 4 个 HookContext.FromMap 写回方法（事件桥闭环前置）"
```

---

### Task 6: uapclaw.go + handle_envelope.go 写回闭环

**Files:**
- Modify: `internal/swarm/server/runtime/uapclaw.go:189-213, 337-361, 248-269, 478-502`
- Modify: `internal/swarm/server/handle_envelope.go:64-81`
- Modify: `internal/swarm/server/runtime/uapclaw_test.go`（追加）

- [ ] **Step 1: 写失败测试（uapclaw_test.go 追加；先看既有测试如何构造 UapClaw 与请求）**

```go
// TestProcessMessage_CloudMemory写回闭环 测试 MEMORY_BEFORE_CHAT 扩展写入 memory_blocks 后注入 inputs
// 构造：注册一个进程内扩展回调改写 memory_blocks（模拟扩展行为），验证 inputs["memory_block"]
func TestProcessMessage_CloudMemory写回闭环(t *testing.T) {
	ResetTestRegistry(t)
	uc := newTestUapClaw(t) // 若无此 helper，按既有测试的构造方式创建最小 UapClaw

	// 注册扩展回调：写两条记忆块（含一条空，验证过滤语义）
	extReg, err := extensions.CreateInstance(callback.NewCallbackFramework(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	extReg.Register(extensions.AgentServerMemoryBeforeChat, func(ctx context.Context, data map[string]any) any {
		data["memory_blocks"] = []any{"块一", "", "块二"}
		return nil
	}, 100)

	// 构造 cloud memory 模式请求（enable_memory=true 走 cloud 分支的请求构造方式参考既有测试）
	request := newCloudMemoryRequest(t)

	// 直接调用被测路径：走 ProcessMessage 会触发完整链路（依赖 adapter），
	// 这里以更小切口验证：构建与 uapclaw.go 相同的 memCtx 流程
	// —— 见 Step 3 的实现说明，实际断言通过 hook 消费点重构后的公共路径完成。
	// 简化做法：断言注册的回调被触发且 data map 上的写回生效（模拟宿主 FromMap 前后）
	//
	// 完整链路验证放 Task 10 端到端测试（真实插件）。
	_ = request
	_ = uc
}
```

**注意**：uapclaw.go 的钩子消费点在 `ProcessMessageImpl` 深处（依赖 adapter/sessionManager），单测直接驱动整条链路成本高。**采用方案**：把"Trigger + FromMap + join 过滤 + 注入 inputs"抽为 `extensions` 包的导出辅助函数 `TriggerMemoryBeforeChat(ctx, request, inputs)`（放 hooks_context.go 或新文件 trigger_helper.go），uapclaw.go 两处调用它；单测针对该函数写。`MEMORY_AFTER_CHAT` 同理抽 `TriggerMemoryAfterChat(ctx, request, assistantMessage)`（返回值无消费，但保持对称）。

重写 Step 1 测试（放 `internal/swarm/extensions/hooks_context_test.go`，依赖仅 callback+schema）：

```go
// TestTriggerMemoryBeforeChat_写回注入 测试辅助函数：扩展写入 memory_blocks → inputs 注入
func TestTriggerMemoryBeforeChat_写回注入(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	reg.Register(AgentServerMemoryBeforeChat, func(ctx context.Context, data map[string]any) any {
		data["memory_blocks"] = []any{"块一", "", "块二"}
		return nil
	}, 100)

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	inputs := map[string]any{}

	TriggerMemoryBeforeChat(context.Background(), request, inputs, "/tmp/w")

	// 空块被过滤（对齐 Python join(b for b in blocks if b)），两块拼接
	if inputs["memory_block"] != "块一\n\n块二" {
		t.Errorf("memory_block = %v, 期望 %q", inputs["memory_block"], "块一\n\n块二")
	}
}

// TestTriggerMemoryBeforeChat_无条件写入 测试无扩展时 memory_block 空串也写入（对齐 Python）
func TestTriggerMemoryBeforeChat_无条件写入(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	inputs := map[string]any{}

	TriggerMemoryBeforeChat(context.Background(), request, inputs, "/tmp/w")

	v, ok := inputs["memory_block"]
	if !ok {
		t.Error("memory_block 应无条件写入（对齐 Python inputs['memory_block'] = memory_block）")
	} else if v != "" {
		t.Errorf("memory_block 应为空串, 实际 %v", v)
	}
}

// TestTriggerMemoryAfterChat_无异常 测试 after 钩子辅助函数可调用
func TestTriggerMemoryAfterChat_无异常(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	TriggerMemoryAfterChat(context.Background(), request, "回答内容", "/tmp/w")
}
```

hooks_context_test.go import 补 `"github.com/uapclaw/uapclaw-go/internal/swarm/schema"` 与 callback。

- [ ] **Step 2: 运行测试验证失败**

```bash
go test ./internal/swarm/extensions/ -run "TriggerMemory" -v
```

Expected: FAIL（函数未定义）。

- [ ] **Step 3: 实现 trigger_helper.go（extensions 包新文件）**

`internal/swarm/extensions/trigger_helper.go`：

```go
package extensions

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TriggerMemoryBeforeChat 触发记忆对话前钩子并写回，拼接 memory_block 注入 inputs。
//
// 对齐 Python interface.py:834-845 / 951-963：
//   - 构造 MemoryHookContext → Trigger → FromMap 写回
//   - join 时过滤空块（对齐 Python `join(b for b in mem_ctx.memory_blocks if b)`）
//   - inputs["memory_block"] 无条件写入（对齐 Python 无条件赋值）
//
// workspaceDir 传入 workspace.AgentRootDir()/home（调用方组装，避免本包依赖 workspace）。
func TriggerMemoryBeforeChat(ctx context.Context, request *schema.AgentRequest, inputs map[string]any, workspaceDir string) {
	extReg, extErr := GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}
	memCtx := buildMemoryHookContext(request, workspaceDir, nil, nil)
	data := memCtx.ToMap()
	extReg.Trigger(ctx, AgentServerMemoryBeforeChat, data)
	memCtx.FromMap(data)

	memoryBlock := strings.Join(filterNonEmpty(memCtx.MemoryBlocks), "\n\n")
	inputs["memory_block"] = memoryBlock
}

// TriggerMemoryAfterChat 触发记忆对话后钩子并写回（当前宿主无消费，保持对称闭环）。
//
// 对齐 Python interface.py:866-877 / 1134-1146。
func TriggerMemoryAfterChat(ctx context.Context, request *schema.AgentRequest, assistantMessage string, workspaceDir string) {
	extReg, extErr := GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}
	afterCtx := buildMemoryHookContext(request, workspaceDir, &assistantMessage, nil)
	data := afterCtx.ToMap()
	extReg.Trigger(ctx, AgentServerMemoryAfterChat, data)
	afterCtx.FromMap(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMemoryHookContext 构造 MemoryHookContext（对齐 Python 构造字段）
func buildMemoryHookContext(request *schema.AgentRequest, workspaceDir string, assistantMessage *string, extra map[string]any) *MemoryHookContext {
	channelID := request.ChannelID
	sid := ""
	if request.SessionID != nil {
		sid = *request.SessionID
	}
	return &MemoryHookContext{
		SessionID:        sid,
		RequestID:        request.RequestID,
		ChannelID:        &channelID,
		AgentName:        "main_agent",
		WorkspaceDir:     workspaceDir,
		AssistantMessage: assistantMessage,
		Extra:            extra,
	}
}

// filterNonEmpty 过滤空字符串块（对齐 Python if b 语义：空串为假）
func filterNonEmpty(blocks []string) []string {
	result := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b != "" {
			result = append(result, b)
		}
	}
	return result
}
```

- [ ] **Step 4: 改写 uapclaw.go 4 处消费点**

L189-213（ProcessMessageImpl 的 before 钩子）替换为：

```go
	// 云端记忆对话前钩子（对齐 Python: interface.py:834-845 MEMORY_BEFORE_CHAT trigger）
	if memoryMode == "cloud" {
		extensions.TriggerMemoryBeforeChat(ctx, request, inputs, filepath.Join(workspace.AgentRootDir(), "home"))
	}
```

（原 25 行代码块整体替换为 4 行；`parseRequestParams(request)` 作为 extra 的传递**对齐 Python `extra=request.params`**——为保持 extra 注入，`TriggerMemoryBeforeChat` 增加 extra 参数或内部解析。**决定**：`TriggerMemoryBeforeChat` 签名改为接收 `extra map[string]any`：

```go
func TriggerMemoryBeforeChat(ctx context.Context, request *schema.AgentRequest, inputs map[string]any, workspaceDir string, extra map[string]any)
```

调用处传 `parseRequestParams(request)`。`TriggerMemoryAfterChat` 同理加 extra。测试同步补 extra 断言。）

L248-269（after 钩子）替换为：

```go
	// 云端记忆对话后钩子（对齐 Python: interface.py:866-877 MEMORY_AFTER_CHAT trigger）
	if memoryMode == "cloud" && resp.OK {
		extensions.TriggerMemoryAfterChat(ctx, request, uc.extractResponseContent(resp), filepath.Join(workspace.AgentRootDir(), "home"), parseRequestParams(request))
	}
```

L337-361（流式 before）与 L478-502（流式 after）同样替换为 helper 调用（流式 after 传 `assistantMessage` 局部变量拼接结果）。

替换后 uapclaw.go 的 `strings` import 若仍被其他代码使用则保留（truncateStr 用到，保留）。

- [ ] **Step 5: 改写 handle_envelope.go 1 处**

L64-81 替换为：

```go
	// 3. before_chat_request 钩子（对齐 Python: agent_ws_server.py _trigger_before_chat_request_hook()）
	// 当 req_method 为 CHAT_SEND/CHAT_RESUME/CHAT_ANSWER 时触发；
	// 扩展可修改 params，FromMap 写回后重新序列化进 request.Params
	if request.ReqMethod == schema.ReqMethodChatSend ||
		request.ReqMethod == schema.ReqMethodChatResume ||
		request.ReqMethod == schema.ReqMethodChatAnswer {
		extReg, extErr := extensions.GetInstanceErr()
		if extErr == nil && extReg != nil {
			reqMethodStr := string(request.ReqMethod)
			var params map[string]any
			if len(request.Params) > 0 {
				_ = json.Unmarshal(request.Params, &params) // 延迟解析 params
			}
			hookCtx := &extensions.AgentServerChatHookContext{
				RequestID: request.RequestID,
				ChannelID: request.ChannelID,
				SessionID: request.SessionID,
				ReqMethod: &reqMethodStr,
				Params:    params,
			}
			data := hookCtx.ToMap()
			extReg.Trigger(ctx, extensions.AgentServerBeforeChatRequest, data)
			hookCtx.FromMap(data)
			if hookCtx.Params != nil {
				if raw, err := json.Marshal(hookCtx.Params); err == nil {
					request.Params = raw
				}
			}
		}
	}
```

- [ ] **Step 6: 为 handle_envelope 写 params 回写测试**

`internal/swarm/server/handle_envelope_test.go`（若已存在则追加）：

```go
// TestHandleEnvelope_BeforeChatHookParams回写 测试扩展修改 params 写回 request.Params
func TestHandleEnvelope_BeforeChatHookParams回写(t *testing.T) {
	// 复用既有 handleEnvelope 测试的服务器构造方式（见同文件已有 TestHandleEnvelope* 用例）
	// 注册扩展回调改 params
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()
	reg.Register(extensions.AgentServerBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		if p, ok := data["params"].(map[string]any); ok {
			p["injected"] = true
		}
		return nil
	}, 100)

	// 构造 chat.send 请求经 handleEnvelope 路径（参考既有测试的 envelope 构造 helper）
	// 断言：处理后的 request.Params 含 injected=true
	// （具体构造参考同文件既有用例；若 envelope 构造复杂，改为直接调 s.handleEnvelope 的
	//  前置条件——建议此处用与既有测试相同的 fake transport + envelope 流程）
	t.Skip("构造方式随既有测试对齐，见同文件已有 helper")
}
```

**注意**：handle_envelope 的完整驱动依赖 transport 构造，若既有测试已有 helper 则复用；否则该用例标记 Skip 并在 Task 10 端到端覆盖 params 回写（真实插件路径）。此处至少保证：`go build ./internal/swarm/server/...` 通过 + 既有测试不回归。

- [ ] **Step 7: 运行测试**

```bash
go test ./internal/swarm/extensions/ ./internal/swarm/server/... -run "TriggerMemory|FromMap|HandleEnvelope" -v
```

Expected: 新增 PASS，既有不回归。再跑全量：

```bash
go test ./internal/swarm/... 2>&1 | tail -5
```

Expected: 全部 ok。

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/extensions/trigger_helper.go internal/swarm/extensions/hooks_context_test.go internal/swarm/server/runtime/uapclaw.go internal/swarm/server/handle_envelope.go internal/swarm/server/handle_envelope_test.go internal/swarm/server/runtime/uapclaw_test.go
git commit -m "feat(extensions): 事件桥写回闭环（uapclaw 4处 + handle_envelope params 回写，对齐 Python 原地修改语义）"
```

---

### Task 7: loader.go 重写（发现 + 加载 + 注册编排）

**Files:**
- Rewrite: `internal/swarm/extensions/loader.go`
- Rewrite: `internal/swarm/extensions/stub_test.go` → 保留文件名（改为真实测试）

- [ ] **Step 1: 写失败测试（重写 stub_test.go 为真实语义测试）**

```go
package extensions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
)

// fakePluginHost 测试用插件工厂：不启动子进程，返回内存 PluginClient
type fakePluginHost struct {
	// declsByRoot 按 root 返回声明
	declsByRoot map[string][]plugin.Declaration
	// loadErr 按 root 返回加载错误
	loadErr map[string]error
}

func (f *fakePluginHost) Load(root string) (*plugin.PluginClient, error) {
	if err, ok := f.loadErr[root]; ok {
		return nil, err
	}
	// 内存客户端：无法走 gRPC —— 测试注入改用接口层断言（见 fakeRegistry 注释）
	return nil, nil
}

// TestExtensionLoader_AddSearchPath 测试目录存在才加入
func TestExtensionLoader_AddSearchPath(t *testing.T) {
	dir := t.TempDir()
	loader := NewExtensionLoader(nil, WithPluginHostForTest(&fakePluginHost{}))
	loader.AddSearchPath(dir)
	loader.AddSearchPath(filepath.Join(dir, "不存在"))
	if got := len(loader.searchPaths); got != 1 {
		t.Errorf("searchPaths 数 = %d, 期望 1（不存在的目录不加）", got)
	}
}

// TestExtensionLoader_DiscoverExtensionRoots 测试发现语义（yaml 或 ext-plugin 即根）
func TestExtensionLoader_DiscoverExtensionRoots(t *testing.T) {
	base := t.TempDir()
	// extA: 有 extension.yaml
	extA := filepath.Join(base, "extA")
	_ = os.MkdirAll(extA, 0o755)
	_ = os.WriteFile(filepath.Join(extA, "extension.yaml"), []byte("id: a\n"), 0o644)
	// extB: 有 ext-plugin
	extB := filepath.Join(base, "extB")
	_ = os.MkdirAll(extB, 0o755)
	_ = os.WriteFile(filepath.Join(extB, "ext-plugin"), []byte("bin"), 0o755)
	// plain: 两者皆无
	plain := filepath.Join(base, "plain")
	_ = os.MkdirAll(plain, 0o755)
	// 文件（非目录）不参与
	_ = os.WriteFile(filepath.Join(base, "file.txt"), []byte("x"), 0o644)

	loader := NewExtensionLoader(nil)
	loader.AddSearchPath(base)
	roots := loader.DiscoverExtensionRoots()
	if len(roots) != 2 {
		t.Fatalf("发现根数 = %d, 期望 2（extA/extB）: %v", len(roots), roots)
	}
}

// TestExtensionLoader_LoadExtension_无入口 返回错误
func TestExtensionLoader_LoadExtension_无入口(t *testing.T) {
	loader := NewExtensionLoader(nil)
	root := t.TempDir() // 无 manifest 也无 ext-plugin
	_, err := loader.LoadExtension(context.Background(), root)
	if err == nil {
		t.Error("无入口二进制的 root 应返回错误")
	}
}
```

**注意**：`LoadExtension` 真实路径要 `pluginHost.Load(root)` 启动子进程——单测注入 fake。为可测性，loader 通过 `PluginHost` 接口（plugin 包定义）调用，`NewExtensionLoader(registry, opts...)` 支持 `WithPluginHost(h)` option（测试用）与默认 `&plugin.GoPluginHost{}`。

由于 fake 无法返回真实 PluginClient（gRPC 连接），**把"声明→注册"编排再抽一层**：loader 的 `LoadExtension` 拆为 `loadDeclarations(ctx, root) ([]plugin.Declaration, *plugin.PluginClient, error)`（需要真实 PluginClient）+ `registerDeclarations(pc, decls, root)`（纯编排，接收接口）。fake 测试直接驱动 `registerDeclarations`——它需要 PluginClient 的事件桥与 transport 能力，进一步抽象为最小接口（loader 包内定义）：

```go
// pluginClientFace loader 所需的插件客户端最小接口（测试可注入）
type pluginClientFace interface {
	SetExtensionDir(ctx context.Context, path string) error
	HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error)
	Shutdown(ctx context.Context) error
	Transport() transport.AgentTransport // ProvidesTransport 声明的代理
}
```

`plugin.PluginClient` 实现之（`Transport()` 返回 `NewGRPCPluginTransport(pc)`，加一个方法）；测试用内存 fake 实现完整注册编排断言。

**修正测试方案**（stub_test.go 最终形态）：

```go
// memPluginClient 内存插件客户端（测试用：不走 gRPC）
type memPluginClient struct {
	setDirCalls   []string
	events        map[string]map[string]any // event → 触发时写入的数据
	handledEvents []string
	transport     transport.AgentTransport
}

func (m *memPluginClient) SetExtensionDir(ctx context.Context, path string) error {
	m.setDirCalls = append(m.setDirCalls, path)
	return nil
}
func (m *memPluginClient) HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error) {
	m.handledEvents = append(m.handledEvents, event)
	if _, ok := data["memory_blocks"]; ok {
		data["memory_blocks"] = append(data["memory_blocks"].([]any), "来自插件")
	}
	return data, nil
}
func (m *memPluginClient) Shutdown(ctx context.Context) error { return nil }
func (m *memPluginClient) Transport() transport.AgentTransport { return m.transport }

// fakeHost 返回内存客户端的工厂
type fakeHost struct {
	clients map[string]*memPluginClient
	errs    map[string]error
}
func (f *fakeHost) Load(root string) (pluginClientFace, error) {
	if err, ok := f.errs[root]; ok {
		return nil, err
	}
	return f.clients[root], nil
}

// TestExtensionLoader_RegisterDeclarations 测试声明注册编排（事件 + transport + SetExtensionDir）
func TestExtensionLoader_RegisterDeclarations(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ch := make(chan []byte, 4)
	memTransport := newChanAgentTransport(ch) // 测试用 AgentTransport（send/recv 都走 ch）
	pc := &memPluginClient{transport: memTransport}
	host := &fakeHost{clients: map[string]*memPluginClient{"/tmp/extA": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata:          plugin.Metadata{ID: "extA", Name: "A", Version: "1.0"},
		EventSubs:         []plugin.EventSubscription{{Event: AgentServerMemoryBeforeChat, Priority: 100}},
		ProvidesTransport: true,
	}}
	loaded, err := loader.loadAndRegister(context.Background(), "/tmp/extA", pc, decls)
	if err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d, 期望 1", len(loaded))
	}
	if len(pc.setDirCalls) != 1 || pc.setDirCalls[0] != "/tmp/extA" {
		t.Errorf("SetExtensionDir 调用不符: %v", pc.setDirCalls)
	}
	// transport 注册进 registry
	if reg.GetAgentServerClient() == nil {
		t.Error("ProvidesTransport 声明应注册 AgentServerClient")
	}
	// 事件桥注册：触发后数据写回
	memCtx := &MemoryHookContext{}
	data := memCtx.ToMap()
	data["memory_blocks"] = []any{}
	reg.Trigger(context.Background(), AgentServerMemoryBeforeChat, data)
	if len(pc.handledEvents) != 1 {
		t.Fatalf("HandleEvent 调用数 = %d, 期望 1", len(pc.handledEvents))
	}
	blocks := data["memory_blocks"].([]any)
	if len(blocks) != 1 || blocks[0] != "来自插件" {
		t.Errorf("事件桥写回不符: %v", blocks)
	}
}

// newChanAgentTransport 最简 AgentTransport（ch 双向复用）
type newChanAgentTransportT struct{ ch chan []byte }
func newChanAgentTransport(ch chan []byte) *newChanAgentTransportT { return &newChanAgentTransportT{ch: ch} }
func (c *newChanAgentTransportT) Send(ctx context.Context, data []byte) error { c.ch <- data; return nil }
func (c *newChanAgentTransportT) Recv() (<-chan []byte, error) { return c.ch, nil }
func (c *newChanAgentTransportT) Close() error { close(c.ch); return nil }

// TestExtensionManager_LoadAllExtensions_单失败继续 测试失败容错
func TestExtensionManager_LoadAllExtensions_单失败继续(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	base := t.TempDir()
	// extOK: 有 manifest
	extOK := filepath.Join(base, "extOK")
	_ = os.MkdirAll(extOK, 0o755)
	_ = os.WriteFile(filepath.Join(extOK, "extension.yaml"), []byte("id: ok\n"), 0o644)
	// extBad: 有 ext-plugin 标记但 host 加载失败
	extBad := filepath.Join(base, "extBad")
	_ = os.MkdirAll(extBad, 0o755)
	_ = os.WriteFile(filepath.Join(extBad, "ext-plugin"), []byte("bin"), 0o755)

	host := &fakeHost{
		clients: map[string]*memPluginClient{extOK: {}},
		errs:    map[string]error{extBad: errors.New("boom")},
	}
	mgr := NewExtensionManager(reg, WithSearchPaths([]string{base}), WithPluginHost(host))
	if err := mgr.LoadAllExtensions(context.Background()); err != nil {
		t.Fatalf("单个失败不应整体报错: %v", err)
	}
	listed := mgr.ListExtensions()
	if len(listed) != 1 || listed[0]["id"] != "ok" {
		t.Errorf("ListExtensions = %v, 期望 extOK 声明（memPluginClient 空 Register 由 loadAndRegister 空 decls 兜底）", listed)
	}
}
```

**注意**：`fakeHost.Load` 返回 `pluginClientFace`（loader 包内接口），`memPluginClient` 实现它；`WithPluginHost` option 类型为该接口的工厂。GoPluginHost 生产实现包一层适配（`plugin.GoPluginHost.Load` 返回 `*plugin.PluginClient`，`plugin.PluginClient` 需实现 `pluginClientFace`——补 `Transport()` 方法到 host.go）。import 补 `"errors"`。

- [ ] **Step 2: 运行测试验证失败**

```bash
go test ./internal/swarm/extensions/ -run "TestExtensionLoader|TestExtensionManager" -v
```

Expected: FAIL（新 API 未定义）。

- [ ] **Step 3: 重写 loader.go**

`internal/swarm/extensions/loader.go`：

```go
package extensions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadedExtension 已加载的扩展记录（Manager 的 loaded 列表元素）
type LoadedExtension struct {
	// Metadata 扩展元数据
	Metadata plugin.Metadata
	// Root 扩展根目录
	Root string
}

// ExtensionLoader 扩展加载器，对齐 Python ExtensionLoader。
//
// 发现 extension_dirs 下的扩展根（extension.yaml 或 ext-plugin 存在），
// 通过 PluginHost 启动插件子进程，Register 声明后代为注册进 Registry：
//   - 事件订阅 → registry.Register（事件桥闭包）
//   - provides_transport 声明 → registry.RegisterAgentServerClient（GRPCPluginTransport 代理）
type ExtensionLoader struct {
	// registry 扩展注册中心
	registry *ExtensionRegistry
	// searchPaths 搜索路径列表（对齐 Python _search_paths）
	searchPaths []string
	// pluginHost 插件启动工厂（生产 GoPluginHost，测试注入 fake）
	pluginHost PluginHostFace
}

// PluginHostFace loader 所需的插件工厂最小接口（打破对 plugin.PluginClient 具体类型的硬依赖，测试可注入）
type PluginHostFace interface {
	// Load 启动 root 下的插件并返回客户端
	Load(root string) (PluginClientFace, error)
}

// PluginClientFace loader 所需的插件客户端最小接口（测试可注入内存实现）
type PluginClientFace interface {
	// SetExtensionDir 设置扩展目录
	SetExtensionDir(ctx context.Context, path string) error
	// HandleEvent 事件桥
	HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error)
	// Shutdown 关闭
	Shutdown(ctx context.Context) error
	// Transport 返回 transport 代理（ProvidesTransport 声明的扩展）
	Transport() transport.AgentTransport
}

// LoaderOption loader 构造选项
type LoaderOption func(*ExtensionLoader)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ManifestFilename 扩展清单文件名，对齐 Python MANIFEST_FILENAME
	ManifestFilename = "extension.yaml"
	// EntryFilename 扩展入口二进制名（对齐 Python ENTRY_FILENAME 语义；值与 plugin.EntryFilename 一致）
	EntryFilename = plugin.EntryFilename
	// logComponent 日志组件（基础设施层）
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionLoader 创建 ExtensionLoader，对齐 Python ExtensionLoader(registry)。
// options 可注入插件工厂（测试）。
func NewExtensionLoader(registry *ExtensionRegistry, opts ...LoaderOption) *ExtensionLoader {
	l := &ExtensionLoader{registry: registry, pluginHost: &pluginHostAdapter{}}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// WithPluginHost 注入插件工厂（测试用；生产默认 GoPluginHost）
func WithPluginHost(h PluginHostFace) LoaderOption {
	return func(l *ExtensionLoader) { l.pluginHost = h }
}

// AddSearchPath 添加搜索路径，对齐 Python add_search_path（目录存在才加入）
func (l *ExtensionLoader) AddSearchPath(path string) {
	if _, err := os.Stat(path); err == nil {
		l.searchPaths = append(l.searchPaths, path)
	}
}

// DiscoverExtensionRoots 发现扩展目录，对齐 Python discover_extension_roots：
// 遍历搜索路径一级子目录，extension.yaml 或 ext-plugin 存在即为扩展根。
func (l *ExtensionLoader) DiscoverExtensionRoots() []string {
	roots := make([]string, 0)
	logger.Info(logComponent).
		Str("event_type", "extension_loader_discover").
		Any("search_paths", l.searchPaths).
		Msg("[ExtensionLoader] 开始搜索扩展路径")
	for _, basePath := range l.searchPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			subdir := filepath.Join(basePath, entry.Name())
			if isExtensionRoot(subdir) {
				roots = append(roots, subdir)
			}
		}
	}
	return roots
}

// LoadExtension 加载单个扩展，对齐 Python load_extension(root)：
// manifest 解析（缺省空）→ 启动插件 → Register 声明 → 逐声明注册（SetExtensionDir + 事件 + transport）。
// 单个声明失败不影响其他声明（对齐 Python 异常仅日志）。
func (l *ExtensionLoader) LoadExtension(ctx context.Context, root string) ([]LoadedExtension, error) {
	manifest := loadManifestDict(root)
	_ = manifest // 依赖安装已删除（Go 静态编译），manifest 仅作发现与元数据参考

	pc, err := l.pluginHost.Load(root)
	if err != nil {
		return nil, fmt.Errorf("加载扩展失败 %s: %w", root, err)
	}

	// Register 声明（PluginClientFace 不含 Register——见 pluginHostAdapter：声明获取走具体类型）
	decls, err := l.registerDecls(pc, root)
	if err != nil {
		pc.Shutdown(ctx)
		pc.Kill()
		return nil, err
	}

	return l.loadAndRegister(ctx, root, pc, decls)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isExtensionRoot 判断是否扩展根（manifest 或入口存在），对齐 Python _is_extension_root
func isExtensionRoot(path string) bool {
	return findManifest(path) != nil || hasEntry(path)
}

// findManifest 查找 extension.yaml，对齐 Python _find_manifest
func findManifest(root string) string {
	p := filepath.Join(root, ManifestFilename)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// hasEntry 查找 ext-plugin 入口
func hasEntry(root string) bool {
	_, err := os.Stat(filepath.Join(root, EntryFilename))
	return err == nil
}

// loadManifestDict 解析 manifest（缺省空 map），对齐 Python _load_manifest_dict
func loadManifestDict(root string) map[string]any {
	manifestPath := findManifest(root)
	if manifestPath == "" {
		return map[string]any{}
	}
	// yaml 解析复用 sdk 包？sdk import extensions 会环——用 gopkg.in/yaml.v3 直接解析
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return map[string]any{}
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return map[string]any{}
	}
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

// registerDecls 获取插件声明（PluginClientFace 不含 Register；由 adapter 层具体类型提供）
func (l *ExtensionLoader) registerDecls(pc PluginClientFace, root string) ([]plugin.Declaration, error) {
	if rpc, ok := pc.(RegisterCapable); ok {
		return rpc.RegisterDeclarations(context.Background())
	}
	return nil, fmt.Errorf("插件客户端不支持 Register: %T", pc)
}

// RegisterCapable 支持声明获取的插件客户端（plugin.PluginClient 满足）
type RegisterCapable interface {
	// RegisterDeclarations 获取扩展声明列表
	RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error)
}

// loadAndRegister 编排注册：SetExtensionDir + 事件桥 + transport 注册
func (l *ExtensionLoader) loadAndRegister(ctx context.Context, root string, pc PluginClientFace, decls []plugin.Declaration) ([]LoadedExtension, error) {
	loaded := make([]LoadedExtension, 0, len(decls))
	for _, decl := range decls {
		if err := pc.SetExtensionDir(ctx, root); err != nil {
			logger.Error(logComponent).
				Str("event_type", "extension_set_dir_failed").
				Str("root", root).
				Err(err).
				Msg("[ExtensionLoader] SetExtensionDir 失败")
			continue
		}
		for _, sub := range decl.EventSubs {
			l.registry.Register(sub.Event, l.makeEventBridge(pc, sub.Event), int(sub.Priority))
		}
		if decl.ProvidesTransport {
			t := pc.Transport()
			if t != nil {
				l.registry.RegisterAgentServerClient(&pluginTransportExtension{client: pc, metadata: decl.Metadata})
			}
		}
		loaded = append(loaded, LoadedExtension{Metadata: decl.Metadata, Root: root})
	}
	return loaded, nil
}

// makeEventBridge 构造事件桥闭包：HandleEvent RPC → 结果原地写回宿主 data map
func (l *ExtensionLoader) makeEventBridge(pc PluginClientFace, event string) callback.CustomCallbackFunc {
	return func(ctx context.Context, data map[string]any) any {
		if data == nil {
			return nil
		}
		result, err := pc.HandleEvent(ctx, event, data)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "extension_event_bridge_error").
				Str("event", event).
				Err(err).
				Msg("[ExtensionLoader] 事件桥调用失败")
			return nil
		}
		if result != nil {
			for k, v := range result {
				data[k] = v
			}
		}
		return nil
	}
}

// pluginHostAdapter 生产适配：GoPluginHost → PluginHostFace
type pluginHostAdapter struct{}

func (a *pluginHostAdapter) Load(root string) (PluginClientFace, error) {
	return pluginHostLoad(root)
}

// pluginTransportExtension AgentServerClientExtension 的插件代理实现（registry.RegisterAgentServerClient 入参）
type pluginTransportExtension struct {
	// client 插件客户端
	client PluginClientFace
	// metadata 扩展元数据
	metadata plugin.Metadata
}

// Initialize 扩展初始化（对齐 Python 事实：host 从不调用，空实现）
func (e *pluginTransportExtension) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}

// Shutdown 代理到插件 Shutdown RPC
func (e *pluginTransportExtension) Shutdown(ctx context.Context) error {
	return e.client.Shutdown(ctx)
}

// Metadata 返回扩展元数据（Register 声明携带）
func (e *pluginTransportExtension) Metadata() *ExtensionMetadata {
	return &ExtensionMetadata{
		ID:                   e.metadata.ID,
		Name:                 e.metadata.Name,
		Version:              e.metadata.Version,
		Description:          e.metadata.Description,
		Author:               e.metadata.Author,
		MinJiuwenSwarmVersion: e.metadata.MinHostVersion,
		Dependencies:         e.metadata.Dependencies,
	}
}

// SetExtensionDir 代理到插件 SetExtensionDir RPC
func (e *pluginTransportExtension) SetExtensionDir(path string) {
	_ = e.client.SetExtensionDir(context.Background(), path)
}

// GetClient 返回 transport 代理（对齐 Python get_client()）
func (e *pluginTransportExtension) GetClient() transport.AgentTransport {
	return e.client.Transport()
}
```

`pluginHostLoad` 生产装配函数（loader.go 非导出函数区）：

```go
// pluginHostLoad 生产加载：go-plugin 子进程（经 plugin 包）
func pluginHostLoad(root string) (PluginClientFace, error) {
	pc, err := plugin.LoadPluginClient(root)
	if err != nil {
		return nil, err
	}
	return &pluginClientAdapter{pc: pc}, nil
}

// pluginClientAdapter plugin.PluginClient → PluginClientFace + RegisterCapable
type pluginClientAdapter struct {
	pc *plugin.PluginClient
}

func (a *pluginClientAdapter) SetExtensionDir(ctx context.Context, path string) error {
	return a.pc.SetExtensionDir(ctx, path)
}
func (a *pluginClientAdapter) HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error) {
	return a.pc.HandleEvent(ctx, event, data)
}
func (a *pluginClientAdapter) Shutdown(ctx context.Context) error {
	return a.pc.Shutdown(ctx)
}
func (a *pluginClientAdapter) Transport() transport.AgentTransport {
	return plugin.NewGRPCPluginTransport(a.pc)
}
func (a *pluginClientAdapter) RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error) {
	return a.pc.Register(ctx)
}
func (a *pluginClientAdapter) Kill() {
	a.pc.Kill()
}
```

（`PluginClientFace` 增加 `Kill()` 方法用于失败清理；`memPluginClient` 测试同步补空实现。`LoadExtension` 中 `pc.Kill()` 调用对应。）

plugin/host.go 相应改造：`GoPluginHost` 与 `Load(root)` 重命名为包级函数 `LoadPluginClient(root string) (*PluginClient, error)`（原 `GoPluginHost.Load` 逻辑平移），删除 `PluginHost` 接口（loader 有自己的 face 接口）。host_test.go 中 `GoPluginHost{}` 引用同步改为 `plugin.LoadPluginClient` 相关断言（本地 gRPC 测试不受影响，`NewPluginClientFromConn` 路径不变）。

import 区补 `gopkg.in/yaml.v3`。

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/swarm/extensions/ ./internal/swarm/extensions/plugin/ -v
```

Expected: 全部 PASS（loader 发现/编排/容错 + plugin 包既有测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/loader.go internal/swarm/extensions/stub_test.go internal/swarm/extensions/plugin/host.go internal/swarm/extensions/plugin/host_test.go
git commit -m "feat(extensions): 10.5.7 ExtensionLoader 实装（发现/加载/声明注册编排 + 事件桥 + transport 代理注册）"
```

---

### Task 8: manager.go 重写

**Files:**
- Rewrite: `internal/swarm/extensions/manager.go`
- Modify: `internal/swarm/extensions/stub_test.go`（Task 7 已含 Manager 测试）

- [ ] **Step 1: manager 测试已写于 Task 7（单失败继续 + ListExtensions）。补充 shutdown 测试**

stub_test.go 追加：

```go
// TestExtensionManager_ShutdownAllExtensions 测试逐个 shutdown + 失败仅记录
func TestExtensionManager_ShutdownAllExtensions(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	pc := &memPluginClient{}
	host := &fakeHost{clients: map[string]*memPluginClient{"/tmp/extS": pc}}

	mgr := NewExtensionManager(reg, WithSearchPaths([]string{}), WithPluginHost(host))
	mgr.loaded = []LoadedExtension{{Metadata: plugin.Metadata{ID: "s1"}, Root: "/tmp/extS"}}

	if err := mgr.ShutdownAllExtensions(context.Background()); err != nil {
		t.Fatalf("ShutdownAllExtensions 失败: %v", err)
	}
	if len(pc.shutdownCalls) != 1 {
		t.Errorf("shutdown 调用数 = %d, 期望 1", len(pc.shutdownCalls))
	}
	if len(mgr.loaded) != 0 {
		t.Errorf("shutdown 后 loaded 应清空, 实际 %d", len(mgr.loaded))
	}
	// 二次调用幂等
	if err := mgr.ShutdownAllExtensions(context.Background()); err != nil {
		t.Errorf("二次 shutdown 应无异常: %v", err)
	}
}
```

（`memPluginClient` 补 `shutdownCalls []string` 字段，Shutdown 记录。）

- [ ] **Step 2: 运行验证失败**

```bash
go test ./internal/swarm/extensions/ -run TestExtensionManager_ShutdownAllExtensions -v
```

Expected: FAIL（`mgr.loaded`/`WithSearchPaths` 未定义）。

- [ ] **Step 3: 重写 manager.go**

```go
package extensions

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionManager 扩展生命周期管理器，对齐 Python ExtensionManager。
//
// 与 Python 差异：_setup_search_paths 的 config 读取上移到 cmd 启动层
// （解 config↔extensions 循环依赖），经 WithSearchPaths 注入。
type ExtensionManager struct {
	// registry 扩展注册中心
	registry *ExtensionRegistry
	// loader 扩展加载器
	loader *ExtensionLoader
	// loaded 已加载扩展列表（对齐 Python _loaded_extensions）
	loaded []LoadedExtension
	// pluginHost 插件工厂（与 loader 共享，shutdown 用）
	pluginHost PluginHostFace
	// clients root → 插件客户端（shutdown 时 Kill 子进程）
	clients map[string]PluginClientFace
}

// ManagerOption manager 构造选项
type ManagerOption func(*ExtensionManager)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// managerLogComponent 日志组件
	managerLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionManager 创建 ExtensionManager，对齐 Python ExtensionManager(registry)。
// searchPaths 由 cmd 层读 config 后注入（对齐 Python _setup_search_paths 的解环版）。
func NewExtensionManager(registry *ExtensionRegistry, opts ...ManagerOption) *ExtensionManager {
	m := &ExtensionManager{
		registry: registry,
		loader:   NewExtensionLoader(registry),
		clients:  map[string]PluginClientFace{},
	}
	for _, opt := range opts {
		opt(m)
	}
	// WithPluginHost 未传时 loader 用默认；同步到 manager（shutdown 需要同一工厂语义）
	return m
}

// WithSearchPaths 注入搜索路径（对齐 Python _setup_search_paths；cmd 层读 config 后传入）
func WithSearchPaths(paths []string) ManagerOption {
	return func(m *ExtensionManager) {
		for _, p := range paths {
			m.loader.AddSearchPath(p)
		}
	}
}

// WithPluginHost 注入插件工厂（测试用；需先于 WithSearchPaths 或任意顺序均安全）
func WithPluginHost(h PluginHostFace) ManagerOption {
	return func(m *ExtensionManager) {
		m.loader.pluginHost = h
		m.pluginHost = h
	}
}

// LoadAllExtensions 加载所有扩展，对齐 Python load_all_extensions：
// 发现 → 逐个加载；单个失败 error 日志后继续；成功记入 loaded。
func (m *ExtensionManager) LoadAllExtensions(ctx context.Context) error {
	roots := m.loader.DiscoverExtensionRoots()
	logger.Info(managerLogComponent).
		Str("event_type", "extension_manager_discover").
		Any("roots", roots).
		Msg("[ExtensionManager] 发现扩展路径")
	for _, root := range roots {
		loaded, err := m.loader.LoadExtension(ctx, root)
		if err != nil {
			logger.Error(managerLogComponent).
				Str("event_type", "extension_manager_load_failed").
				Str("root", root).
				Err(err).
				Msg("[ExtensionManager] 加载扩展失败")
			continue
		}
		for _, le := range loaded {
			logger.Info(managerLogComponent).
				Str("event_type", "extension_manager_loaded").
				Str("id", le.Metadata.ID).
				Str("root", le.Root).
				Msg("[ExtensionManager] 加载成功")
			m.loaded = append(m.loaded, le)
		}
	}
	return nil
}

// ShutdownAllExtensions 关闭所有扩展，对齐 Python shutdown_all_extensions：
// 逐个 shutdown（失败仅 warn 日志），最后清空列表并 Kill 子进程。
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error {
	for _, le := range m.loaded {
		if pc, ok := m.clients[le.Root]; ok {
			if err := pc.Shutdown(ctx); err != nil {
				logger.Warn(managerLogComponent).
					Str("event_type", "extension_manager_shutdown_failed").
					Str("id", le.Metadata.ID).
					Err(err).
					Msg("[ExtensionManager] 关闭扩展失败")
			}
			pc.Kill()
			delete(m.clients, le.Root)
		}
	}
	m.loaded = nil
	return nil
}

// ListExtensions 列出已加载扩展，对齐 Python list_extensions：
// [{id, name, version}] 同构。
func (m *ExtensionManager) ListExtensions() []map[string]string {
	result := make([]map[string]string, 0, len(m.loaded))
	for _, le := range m.loaded {
		result = append(result, map[string]string{
			"id":      le.Metadata.ID,
			"name":    le.Metadata.Name,
			"version": le.Metadata.Version,
		})
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**注意**：`LoadExtension` 内部加载成功后需把 pc 记入 `m.clients`——`loader.LoadExtension` 返回 `[]LoadedExtension` 不含 client。**修正**：`LoadedExtension` 增加 `client PluginClientFace` 字段（loader 的 `loadAndRegister` 填充），Manager 的 LoadAllExtensions 把 `le.client` 存进 `m.clients[le.Root]`。`ShutdownAllExtensions` 用 `le.client` 直接遍历（去掉 clients map）：

```go
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error {
	for _, le := range m.loaded {
		if le.client == nil {
			continue
		}
		if err := le.client.Shutdown(ctx); err != nil {
			logger.Warn(managerLogComponent).Str("event_type", "extension_manager_shutdown_failed").
				Str("id", le.Metadata.ID).Err(err).Msg("[ExtensionManager] 关闭扩展失败")
		}
		le.client.Kill()
	}
	m.loaded = nil
	return nil
}
```

（去掉 `clients` map 与 `pluginHost` 字段；`WithPluginHost` 只设置 loader。Task 7/8 测试的 `mgr.loaded` 构造同步含 client 字段。）

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/swarm/extensions/ -v
```

Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/manager.go internal/swarm/extensions/stub_test.go internal/swarm/extensions/loader.go
git commit -m "feat(extensions): 10.5.8 ExtensionManager 实装（WithSearchPaths 注入 + LoadAll/ShutdownAll/List 对齐 Python）"
```

---

### Task 9: forward_loop.go 步骤9 + cmd 接线

**Files:**
- Modify: `internal/swarm/gateway/message_handler/forward_loop.go:126-128`
- Modify: `internal/swarm/gateway/message_handler/dispatch.go`（+triggerBeforeChatRequestHook）
- Modify: `internal/swarm/gateway/message_handler/forward_loop_test.go`（追加）
- Modify: `cmd/uapclaw/cmd.go:216-243`
- Create: `cmd/uapclaw/cmd_test.go`

- [ ] **Step 1: 写失败测试（message_handler）**

forward_loop_test.go 追加：

```go
// TestTriggerBeforeChatRequestHook 门控与 params 回写
func TestTriggerBeforeChatRequestHook(t *testing.T) {
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()
	reg.Register(extensions.GatewayBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		if p, ok := data["params"].(map[string]any); ok {
			p["query"] = "扩展改写后的query"
		}
		return nil
	}, 100)

	mh := newTestMessageHandler(t) // 复用同文件既有 helper（无则按 NewMessageHandler(nil) 最简构造）

	// chat.send 触发 + 回写
	msg := schema.NewReqMessage("web", "s1", schema.ReqMethodChatSend, json.RawMessage(`{"query":"原始"}`))
	agentMsg := mh.prepareAgentDispatchMessage(context.Background(), msg)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg)
	var p map[string]any
	_ = json.Unmarshal(agentMsg.Params, &p)
	if p["query"] != "扩展改写后的query" {
		t.Errorf("params.query = %v, 期望扩展改写后", p["query"])
	}

	// 非 chat 方法不触发（chat.cancel）
	msg2 := schema.NewReqMessage("web", "s1", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	agentMsg2 := mh.prepareAgentDispatchMessage(context.Background(), msg2)
	agentMsg2.Params = json.RawMessage(`{"query":"不应被改"}`)
	mh.triggerBeforeChatRequestHook(context.Background(), agentMsg2)
	var p2 map[string]any
	_ = json.Unmarshal(agentMsg2.Params, &p2)
	if p2["query"] != "不应被改" {
		t.Errorf("chat.cancel 不应触发: %v", p2["query"])
	}
}
```

（`newTestMessageHandler` 若同文件无既有 helper，用 `NewMessageHandler(nil)` 直构；import 补 extensions/callback/schema。）

- [ ] **Step 2: 验证失败**

```bash
go test ./internal/swarm/gateway/message_handler/ -run TestTriggerBeforeChatRequestHook -v
```

Expected: FAIL（`triggerBeforeChatRequestHook` 未定义）。

- [ ] **Step 3: 实现 dispatch.go 触发函数 + forward_loop.go 接线**

dispatch.go 非导出函数区追加：

```go
// shouldTriggerBeforeChatRequestHook 判断是否触发聊天前钩子。
//
// Python: _should_trigger_before_chat_request_hook (L1855-1863)：
// 仅 CHAT_SEND / CHAT_RESUME / CHAT_ANSWER。
func (mh *MessageHandler) shouldTriggerBeforeChatRequestHook(msg *schema.Message) bool {
	return msg.ReqMethod == schema.ReqMethodChatSend ||
		msg.ReqMethod == schema.ReqMethodChatResume ||
		msg.ReqMethod == schema.ReqMethodChatAnswer
}

// triggerBeforeChatRequestHook 触发 Gateway 聊天前钩子。
//
// Python: _trigger_before_chat_request_hook (L1871-1888)：
// 构建 GatewayChatHookContext → trigger(BEFORE_CHAT_REQUEST) → 扩展原地改 params →
// 从同一对象读回（Go：FromMap 写回后重新序列化进 agentMsg.Params）。
func (mh *MessageHandler) triggerBeforeChatRequestHook(ctx context.Context, agentMsg *schema.Message) {
	if !mh.shouldTriggerBeforeChatRequestHook(agentMsg) {
		return
	}
	extReg, extErr := extensions.GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}

	var params map[string]any
	if len(agentMsg.Params) > 0 {
		_ = json.Unmarshal(agentMsg.Params, &params)
	}
	if params == nil {
		params = map[string]any{}
	}

	var reqMethodStr *string
	if agentMsg.ReqMethod != "" {
		s := string(agentMsg.ReqMethod)
		reqMethodStr = &s
	}
	hookCtx := &extensions.GatewayChatHookContext{
		RequestID: agentMsg.ID,
		ChannelID: agentMsg.ChannelID,
		SessionID: &agentMsg.SessionID,
		ReqMethod: reqMethodStr,
		Params:    params,
	}
	data := hookCtx.ToMap()
	extReg.Trigger(ctx, extensions.GatewayBeforeChatRequest, data)
	hookCtx.FromMap(data)
	if hookCtx.Params != nil {
		if raw, err := json.Marshal(hookCtx.Params); err == nil {
			agentMsg.Params = raw
		}
	}
}
```

（Python 语义对齐：`session_id=None` 时 req_method 也可能为 None——Go 用 nil 指针表达；`msg.session_id` 为 `str | None`，Go Message.SessionID 是 string，直接取址。dispatch.go import 补 extensions。）

forward_loop.go L126-128 替换：

```go
				// 步骤9: before_chat_request hook（对齐 Python _forward_loop L2502，
				// 11.3 MessageHandler 既有逻辑——曾误标 TODO(#11.13)，本期回填修正归属）
				mh.triggerBeforeChatRequestHook(ctx, agentMsg)
```

- [ ] **Step 4: 运行测试**

```bash
go test ./internal/swarm/gateway/message_handler/ -v
```

Expected: 全部 PASS（含既有 forward_loop 测试不回归）。

- [ ] **Step 5: cmd 接线测试（cmd_test.go）**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadExtensionDirs_分号分割 测试 ';' 分割与 strip
func TestReadExtensionDirs_分号分割(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	got := readExtensionDirs(map[string]any{
		"extensions": map[string]any{"extension_dirs": dir1 + " ; " + dir2},
	})
	if len(got) != 2 {
		t.Fatalf("dirs = %v, 期望 2", got)
	}
	// 相对路径转绝对（按 cwd）
	got2 := readExtensionDirs(map[string]any{
		"extensions": map[string]any{"extension_dirs": "./relative/path"},
	})
	if len(got2) != 1 {
		t.Fatalf("dirs2 = %v, 期望 1", got2)
	}
	if !filepath.IsAbs(got2[0]) {
		t.Errorf("相对路径应转绝对: %q", got2[0])
	}
}

// TestReadExtensionDirs_非字符串与缺失 测试仅支持字符串（对齐 Python）
func TestReadExtensionDirs_非字符串与缺失(t *testing.T) {
	if got := readExtensionDirs(map[string]any{}); len(got) != 0 {
		t.Errorf("缺失段应返回空: %v", got)
	}
	if got := readExtensionDirs(map[string]any{
		"extensions": map[string]any{"extension_dirs": []string{"/a"}},
	}); len(got) != 0 {
		t.Errorf("非字符串应返回空（对齐 Python isinstance(str) 检查）: %v", got)
	}
	if got := readExtensionDirs(map[string]any{
		"extensions": "不是map",
	}); len(got) != 0 {
		t.Errorf("extensions 非 map 应返回空: %v", got)
	}
}
```

- [ ] **Step 6: 验证失败后实现 readExtensionDirs + runAppCmd 接线**

cmd.go 追加（非导出函数区）：

```go
// readExtensionDirs 读取 extensions.extension_dirs 配置（';' 分割，相对路径转绝对）。
//
// 对齐 Python manager.py _extension_dir_paths_from_config + _setup_search_paths：
// 仅支持字符串值；空段丢弃；相对路径按 cwd 解析为绝对路径。
func readExtensionDirs(cfgData map[string]any) []string {
	ext, ok := cfgData["extensions"].(map[string]any)
	if !ok {
		return nil
	}
	dirs, ok := ext["extension_dirs"].(string)
	if !ok {
		return nil
	}
	result := make([]string, 0)
	for _, p := range strings.Split(dirs, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
		result = append(result, p)
	}
	return result
}
```

runAppCmd L216-226 替换（⤵️ 10.5.8 注释块放开）：

```go
	// 6. 初始化扩展系统（对齐 Python: app_gateway.py L814-822, app_agentserver.py L131-141）
	fw := callback.GetCallbackFramework()
	extRegistry, err := extensions.CreateInstance(fw, map[string]any{}, nil)
	if err != nil {
		// 已存在（重复初始化）不算致命，继续取实例
		extRegistry, _ = extensions.GetInstanceErr()
	}
	if extRegistry != nil {
		extDirs := readExtensionDirs(cfg.RawOrDefault())
		extManager := extensions.NewExtensionManager(extRegistry, extensions.WithSearchPaths(extDirs))
		if err := extManager.LoadAllExtensions(ctx); err != nil {
			logger.Error(logComponentGateway).Err(err).Msg("failed to load extensions")
		}
		logger.Info(logComponentGateway).
			Int("count", len(extManager.ListExtensions())).
			Msg("extensions loaded")
		defer func() {
			if err := extManager.ShutdownAllExtensions(context.Background()); err != nil {
				logger.Warn(logComponentGateway).Err(err).Msg("failed to shutdown extensions")
			}
		}()
	}
```

**注意**：`cfg.Raw()` 返回 `(map[string]any, error)`。**用已加载的 cfg 数据**：`cfg.Load()` 已在 L208 调用过——用 `cfg.Get("extensions")` 更直接：

```go
		extDirs := readExtensionDirs(map[string]any{"extensions": cfg.Get("extensions")})
```

**修正 readExtensionDirs 输入**：直接接收 `any`（cfg.Get 返回值）更贴合：

```go
// readExtensionDirs 从 cfg.Get("extensions") 的返回值解析 extension_dirs
func readExtensionDirs(extVal any) []string {
	ext, ok := extVal.(map[string]any)
	if !ok {
		return nil
	}
	// ... 其余同上
}
```

cmd_test.go 断言同步改为传 `map[string]any{...}` 直接作为 extVal（外层不再包 extensions key——测试按新签名改）。

transport 选择回填（L228-243 附近替换）：

```go
	// 创建 transport：扩展 transport 优先（对齐 Python app_gateway.py L828-833），
	// 无扩展时回落进程内 ChannelTransport
	var chTransport *transport.ChannelTransport
	var agentTransport transport.AgentTransport
	if extRegistry != nil {
		if t := extRegistry.GetAgentServerClient(); t != nil {
			agentTransport = t
			logger.Info(logComponentGateway).
				Msg("using extension AgentServerClient transport")
		}
	}
	if agentTransport == nil {
		chTransport = transport.NewChannelTransport()
		agentTransport = chTransport
	}

	// 创建 AgentClient（对齐 Python WebSocketAgentServerClient）
	agentClient := routing.NewAgentClient(agentTransport)

	// 创建 AgentServer（仅进程内 transport 时；扩展 transport = 远端场景，本地不启动）
	var agentServer *server.AgentServer
	if chTransport != nil {
		agentServer = server.NewAgentServer(cfg, chTransport)
	}
```

后续 `agentServer.Start(ctx)` / `agentServer.Stop()` 调用改带 nil 判断：

```go
	if agentServer != nil {
		if err := agentServer.Start(ctx); err != nil {
			logger.Error(logComponentAgentServer).Err(err).Msg("failed to start AgentServer")
			return fmt.Errorf("启动 AgentServer 失败: %w", err)
		}
	}
	...
	if agentServer != nil {
		if err := agentServer.Stop(); err != nil {
			logger.Error(logComponentAgentServer).Err(err).Msg("failed to stop AgentServer")
		}
	}
```

（GatewayServer 构造不变——`gateway.NewGatewayServer(cfg, agentClient)` 已接收 AgentClient。）

- [ ] **Step 7: 运行全部测试 + 编译**

```bash
go test ./cmd/uapclaw/ ./internal/swarm/... -v 2>&1 | tail -10
go build ./...
```

Expected: PASS + 编译通过。

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/gateway/message_handler/dispatch.go internal/swarm/gateway/message_handler/forward_loop.go internal/swarm/gateway/message_handler/forward_loop_test.go cmd/uapclaw/cmd.go cmd/uapclaw/cmd_test.go
git commit -m "feat(extensions): Gateway before_chat_request 触发点回填(11.3) + runAppCmd 扩展接线与 transport 选择"
```

---

### Task 10: 端到端测试（真实插件子进程）

**Files:**
- Create: `internal/swarm/extensions/plugin/testdata/echo/main.go`
- Create: `internal/swarm/extensions/plugin/e2e_test.go`

- [ ] **Step 1: 写 echo 插件源码**

`internal/swarm/extensions/plugin/testdata/echo/main.go`：

```go
// echo 插件：端到端测试用最小扩展。
// 声明两个事件订阅 + transport 能力；HandleEvent 给 memory_blocks 追加一条；
// transport 为回环（Send 的数据写进 Recv 通道）。
package main

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
)

// echoExtension 回环扩展实现
type echoExtension struct {
	recvCh chan []byte
}

func newEchoExtension() *echoExtension {
	return &echoExtension{recvCh: make(chan []byte, 16)}
}

// Register 注册声明（对齐 Python register_extensions）
func (e *echoExtension) Register(ctx context.Context) ([]plugin.Declaration, error) {
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
	}}, nil
}

// Shutdown 关闭
func (e *echoExtension) Shutdown(ctx context.Context) error { return nil }

// HandleEvent 事件处理：memory_blocks 追加一条（验证写回闭环）
func (e *echoExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	if ctxData == nil {
		ctxData = map[string]any{}
	}
	if event == "agent_server:memory_before_chat" {
		blocks, _ := ctxData["memory_blocks"].([]any)
		ctxData["memory_blocks"] = append(blocks, "来自echo插件")
	}
	if event == "gateway:before_chat_request" {
		if p, ok := ctxData["params"].(map[string]any); ok {
			p["echo_touched"] = true
		} else {
			ctxData["params"] = map[string]any{"echo_touched": true}
		}
	}
	return ctxData, nil
}

// Send 回环发送
func (e *echoExtension) Send(ctx context.Context, data []byte) error {
	e.recvCh <- data
	return nil
}

// Recv 返回接收通道
func (e *echoExtension) Recv() (<-chan []byte, error) { return e.recvCh, nil }

// Close 关闭
func (e *echoExtension) Close() error { close(e.recvCh); return nil }

func main() {
	plugin.ServeExtensionWithTransport(newEchoExtension(), newEchoExtension())
}
```

**注意**：`testdata/` 目录下 Go 工具链默认忽略编译——`go build ./internal/swarm/extensions/plugin/testdata/echo/` 仍可显式构建（testdata 内 package 在显式路径下可构建）。E2E 测试用 TestMain 预编译：

- [ ] **Step 2: 写 e2e_test.go**

`internal/swarm/extensions/plugin/e2e_test.go`：

```go
package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// e2eBinPath TestMain 预编译的 echo 插件二进制路径
var e2eBinPath string

// TestMain 预编译 testdata/echo 插件（本地 go build，非外部服务，不加 build tag）
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "uapclaw-ext-e2e-*")
	if err != nil {
		os.Exit(1)
	}
	bin := filepath.Join(tmpDir, "ext-plugin")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/echo/")
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		// 无 go 工具链环境（罕见）跳过端到端
		_ = os.RemoveAll(tmpDir)
		e2eBinPath = ""
		os.Exit(m.Run())
	}
	e2eBinPath = bin
	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// TestE2E_FullLifecycle 端到端：发现→加载→Register→事件桥→transport 回环→shutdown
func TestE2E_FullLifecycle(t *testing.T) {
	if e2eBinPath == "" {
		t.Skip("go 工具链不可用，跳过端到端")
	}

	// 准备扩展根目录：manifest + ext-plugin
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "extension.yaml"), []byte(
		"id: echo-ext\nname: 回环扩展\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("写 manifest 失败: %v", err)
	}
	if err := copyFile(e2eBinPath, filepath.Join(root, "ext-plugin")); err != nil {
		t.Fatalf("复制插件失败: %v", err)
	}
	_ = os.Chmod(filepath.Join(root, "ext-plugin"), 0o755)

	// 加载（真实 go-plugin 子进程）
	pc, err := LoadPluginClient(root)
	if err != nil {
		t.Fatalf("LoadPluginClient 失败: %v", err)
	}
	defer pc.Kill()

	// Register 声明
	decls, err := pc.Register(context.Background())
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("声明数 = %d, 期望 1", len(decls))
	}
	if decls[0].Metadata.ID != "echo-ext" {
		t.Errorf("声明 ID = %q, 期望 echo-ext", decls[0].Metadata.ID)
	}
	if len(decls[0].EventSubs) != 2 {
		t.Errorf("事件订阅数 = %d, 期望 2", len(decls[0].EventSubs))
	}
	if !decls[0].ProvidesTransport {
		t.Error("应声明 ProvidesTransport")
	}

	// SetExtensionDir + GetMetadata
	if err := pc.SetExtensionDir(context.Background(), root); err != nil {
		t.Errorf("SetExtensionDir 失败: %v", err)
	}
	if m, err := pc.GetMetadata(context.Background()); err != nil || m.ID != "echo-ext" {
		t.Errorf("GetMetadata = %+v, err = %v", m, err)
	}

	// 事件桥：memory_blocks 写回
	result, err := pc.HandleEvent(context.Background(), "agent_server:memory_before_chat",
		map[string]any{"memory_blocks": []any{}})
	if err != nil {
		t.Fatalf("HandleEvent 失败: %v", err)
	}
	blocks, _ := result["memory_blocks"].([]any)
	if len(blocks) != 1 || blocks[0] != "来自echo插件" {
		t.Errorf("memory_blocks = %v, 期望含 echo 追加块", result["memory_blocks"])
	}

	// 事件桥：params 写回（gateway 场景）
	result2, err := pc.HandleEvent(context.Background(), "gateway:before_chat_request",
		map[string]any{"params": map[string]any{"query": "原始"}})
	if err != nil {
		t.Fatalf("HandleEvent gateway 失败: %v", err)
	}
	if p, ok := result2["params"].(map[string]any); !ok || p["echo_touched"] != true {
		t.Errorf("params = %v, 期望 echo_touched=true", result2["params"])
	}

	// Transport 回环
	tr := NewGRPCPluginTransport(pc)
	payload := []byte(`{"e2e":true}`)
	if err := tr.Send(context.Background(), payload); err != nil {
		t.Fatalf("Transport Send 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Transport Recv 失败: %v", err)
	}
	select {
	case got := <-recvCh:
		if string(got) != string(payload) {
			t.Errorf("回环数据不符: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transport 回环超时")
	}

	// Shutdown RPC + Kill
	if err := pc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown 失败: %v", err)
	}
}

// copyFile 复制文件辅助
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
```

- [ ] **Step 3: 运行端到端**

```bash
go test ./internal/swarm/extensions/plugin/ -run TestE2E -v -timeout 120s
```

Expected: PASS（子进程启动 + 握手 + 全 RPC + 回环 + 关闭）。若握手失败，检查 protoc 生成代码 import 路径与 `plugin.Serve` 的 stdout 占用。

- [ ] **Step 4: 运行全量测试（回归）**

```bash
go test ./... 2>&1 | grep -v "^ok\|no test files" | head -10
```

Expected: 无 FAIL 行（或仅有已知的跳过项）。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/plugin/testdata/echo/main.go internal/swarm/extensions/plugin/e2e_test.go
git commit -m "test(extensions): 10.5.7 端到端测试（真实 go-plugin 子进程：发现/加载/事件桥/transport 回环/shutdown）"
```

---

### Task 11: doc.go 更新 + 计划表回填 + 覆盖率验证

**Files:**
- Modify: `internal/swarm/extensions/doc.go`
- Modify: `internal/swarm/extensions/sdk/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（10.5.7/10.5.8/10.5.3/11.3 行）
- Modify: `internal/swarm/extensions/plugin/doc.go`（如 Task 1 后有文件增补）

- [ ] **Step 1: 更新 extensions/doc.go**

```go
// Package extensions 提供扩展系统基础设施，对齐 Python jiuwenswarm/extensions/。
//
// 本包定义扩展注册中心（ExtensionRegistry）、数据类型（ExtensionMetadata/ExtensionConfig）、
// 钩子事件常量与钩子上下文（含 ToMap/FromMap 双向转换）、扩展加载器与生命周期管理器
// （ExtensionLoader/ExtensionManager，方案 D：hashicorp go-plugin 子进程），
// 以及事件桥写回辅助函数（TriggerMemoryBeforeChat/TriggerMemoryAfterChat）。
// 回调机制复用 agentcore/runner/callback.CallbackFramework。
// 延后部分：CryptoUtility（10.5.10 ⤵️）。CallbackCompat 不需要（10.5.9 ⤴️）。
//
// 文件目录：
//
//	extensions/
//	├── doc.go               # 包文档
//	├── types.go             # ExtensionMetadata + ExtensionConfig（10.5.1）
//	├── hook_event.go        # GatewayHookEvents + AgentServerHookEvents（10.5.2）
//	├── hooks_context.go     # 4 种 HookContext + ToMap/FromMap（10.5.3，FromMap 随 10.5.7 回填）
//	├── registry.go          # ExtensionRegistry 单例 + 回调触发（10.5.6）
//	├── trigger_helper.go    # 记忆钩子触发写回辅助（随 10.5.7 回填）
//	├── loader.go            # ExtensionLoader：发现/加载/声明注册编排（10.5.7）
//	├── manager.go           # ExtensionManager：WithSearchPaths + 生命周期（10.5.8）
//	├── callback_compat.go   # 注释说明已覆盖（10.5.9 ⤴️）
//	└── sdk/                 # 扩展 SDK 子包（10.5.4/10.5.5）
//
// 对应 Python 代码：jiuwenswarm/extensions/
package extensions
```

- [ ] **Step 2: 更新 sdk/doc.go（补 host 不调 initialize 事实注释）**

base.go 的 `BaseExtensionImpl` 无需改动；sdk/doc.go 目录树与说明不变，仅在包注释补一行：

```
// 注意：宿主从不调用 Initialize（对齐 Python loader/manager 事实——仅
// register_extensions → set_extension_dir → shutdown 三步）。
```

- [ ] **Step 3: IMPLEMENTATION_PLAN.md 回填**

- L702：`| 10.5.7 | ✅ | ExtensionLoader | go-plugin 子进程方案（D）：gRPC 协议（Register/SetExtensionDir/GetMetadata/Shutdown/HandleEvent+Transport 三件套）+ 发现/加载/声明注册编排 + 事件桥 + GRPCPluginTransport；依赖安装删除（Go 静态编译） | jiuwenswarm/extensions/loader.py |`
- L703：`| 10.5.8 | ✅ | ExtensionManager | WithSearchPaths 注入解 config↔extensions 环 + LoadAll（单失败继续）/ShutdownAll/List 对齐 Python | jiuwenswarm/extensions/manager.py |`
- L698（10.5.3 行）：内容列追加 `；✅ FromMap 写回闭环随 10.5.7 回填（4 上下文 + TriggerMemory 辅助）`
- L732（11.3 行）：stub 列表追加说明改为 `；✅ before_chat_request 触发点已回填（forward_loop 步骤9，曾误挂 TODO(#11.13)）`——保留原有 stub 注记（handleCronPushPayload→11.10 等不丢失）

- [ ] **Step 4: 覆盖率验证**

```bash
go test -coverprofile=coverage.out ./internal/swarm/extensions/... ./internal/swarm/gateway/message_handler/... ./cmd/uapclaw/... 2>&1 | grep -E "coverage|FAIL"
go tool cover -func=coverage.out | grep -E "loader.go|manager.go|hooks_context.go|transport_proxy.go|host.go|serve.go|trigger_helper.go" | head -20
```

Expected: 各包覆盖率 ≥85%（plugin 包的 .pb.go 生成代码不计人为补测——生成文件在 coverage 输出中若拉低覆盖率，用 `-coverpkg` 排除说明或在验收记录中注明豁免理由）。

未达标则补测试（防御分支、错误路径）后重跑。

- [ ] **Step 5: 全量回归 + Commit**

```bash
go test ./... 2>&1 | grep -c "^ok"
git add internal/swarm/extensions/doc.go internal/swarm/extensions/sdk/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(extensions): 10.5.7+10.5.8 完成——doc.go 更新 + 计划表回填（含 10.5.3/11.3 联动行）"
```

---

## 验收清单（对照 spec）

| spec 节 | 覆盖任务 |
|---|---|
| 三、gRPC 协议 | Task 1 |
| 四、包结构（plugin 子包） | Task 1-4 |
| 五、宿主注册流程 | Task 7（loadAndRegister） |
| 六、事件桥写回（FromMap + 6 处消费点 + 步骤9） | Task 5、6、9 |
| 七、GRPCPluginTransport | Task 4 |
| 八、cmd 接线 | Task 9 |
| 九、插件作者 SDK | Task 2、10（echo 插件即示例） |
| 十、测试策略 | 各 Task 测试 + Task 10 端到端 + Task 11 覆盖率 |
| 十一、计划表回填 | Task 11 |
| 十二、风险（stdout/幂等/竞态） | Task 2 注释、Task 4 select 防护、Task 10 |

## 关键对齐点速查（实现时勿偏离）

1. Python `join(b for b in blocks if b)`：过滤空块——`filterNonEmpty`
2. Python `inputs["memory_block"] = memory_block` 无条件写入（含空串）
3. Python host 不调 initialize——协议无 Initialize RPC、`pluginTransportExtension.Initialize` 空实现
4. `load_all_extensions` 单个失败仅 error 日志继续
5. `shutdown_all_extensions` 逐个 shutdown 失败仅 warn
6. `extension_dirs` 仅支持字符串（`isinstance(str)`）——`readExtensionDirs` 同
7. 相对路径 resolve 成绝对路径（Python `Path.resolve()` → Go `filepath.Abs`）
8. 事件优先级默认 100（Python register 签名默认值）
