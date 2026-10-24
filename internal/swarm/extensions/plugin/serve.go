package plugin

import (
	"context"
	"errors"
	"sync"

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
	// ProvidesCrypto 是否为 crypto 提供者（true 时须实现 CryptoImpl）
	ProvidesCrypto bool
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

// extensionServer gRPC 服务端：把 ExtensionImpl 适配到 proto 服务
type extensionServer struct {
	extproto.UnimplementedExtensionServer
	// impl 插件实现
	impl ExtensionImpl
	// transportImpl transport 实现（ProvidesTransport 时非 nil）
	transportImpl TransportImpl
	// cryptoImpl crypto 实现（ProvidesCrypto 时非 nil）
	cryptoImpl CryptoImpl
	// extensionDir 扩展目录（SetExtensionDir RPC 设置）
	extensionDir string
	// metadataCache GetMetadata 缓存（T-06，对齐 Python sdk/base.py _metadata_cache：
	// metadata property 只算一次；SetExtensionDir 时失效重算——yaml 元数据依赖目录）
	metadataCache *Metadata
	// metadataMu 保护 metadataCache 的读写锁（GetMetadata RPC 可并发）
	metadataMu sync.Mutex
}

// connDialer GRPCClient 返回的包装类型（宿主通过 Conn() 拿 gRPC 连接）
type connDialer struct {
	// conn 底层 gRPC 连接
	conn *grpc.ClientConn
}

// ExtensionGRPCPlugin go-plugin 插件包装（实现 plugin.GRPCPlugin 接口）：
// 嵌入 NetRPCUnsupportedPlugin 以满足 Server/Client 两个 net/rpc 方法（返回 ErrNetRPCUnsupported）
type ExtensionGRPCPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	// impl gRPC 服务端实现
	impl *extensionServer
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
	// defaultEventPriority 事件订阅默认优先级
	// （对齐 Python registry.py register(event, handler, priority: int = 100)）
	defaultEventPriority = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrNoTransport 插件未实现 TransportImpl 却被调用 transport RPC
var ErrNoTransport = errors.New("扩展未提供 transport 能力")

// ErrNoCrypto 插件未实现 CryptoImpl 却被调用 crypto RPC
var ErrNoCrypto = errors.New("扩展未提供 crypto 能力")

// ──────────────────────────── 导出函数 ────────────────────────────

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

// Conn 返回底层 gRPC 连接
func (c *connDialer) Conn() *grpc.ClientConn { return c.conn }

// GRPCServer 注册 gRPC 服务（go-plugin 插件侧）
func (p *ExtensionGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	extproto.RegisterExtensionServer(s, p.impl)
	return nil
}

// GRPCClient 宿主侧：返回 conn 包装（host.go dialPlugin 断言使用）
func (p *ExtensionGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &connDialer{conn: c}, nil
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
		for _, sub := range d.EventSubs {
			priority := sub.Priority
			// T-07：对齐 Python register(event, handler, priority=100) 默认值。
			// proto int32 无法区分"未设置"（零值 0）与显式 0，而 priority 语义
			// 为正值（显式 0 无意义），故 0 归一为 100，避免忘设 priority 的
			// 扩展回调排到默认扩展之前。
			if priority == 0 {
				priority = defaultEventPriority
			}
			pb.EventSubs = append(pb.EventSubs, &extproto.EventSubscription{
				Event:    sub.Event,
				Priority: priority,
			})
		}
		resp.Decls = append(resp.Decls, pb)
	}
	return resp, nil
}

// SetExtensionDir 处理设置扩展目录 RPC
func (s *extensionServer) SetExtensionDir(ctx context.Context, req *extproto.SetDirRequest) (*extproto.SetDirResponse, error) {
	s.extensionDir = req.GetPath()
	// 对齐 Python sdk/base.py set_extension_dir：目录变更后元数据缓存失效
	// （_metadata_cache = None），下次 GetMetadata 依新目录重算（T-06）
	s.metadataMu.Lock()
	s.metadataCache = nil
	s.metadataMu.Unlock()
	return &extproto.SetDirResponse{}, nil
}

// GetMetadata 处理元数据查询 RPC。
//
// T-06：对齐 Python sdk/base.py metadata property 的缓存语义——
// Register 有副作用（声明注册）时，每次 RPC 重执行会被重复触发；
// 结果缓存后仅首次执行，SetExtensionDir 时失效重算。
func (s *extensionServer) GetMetadata(ctx context.Context, _ *extproto.GetMetadataRequest) (*extproto.GetMetadataResponse, error) {
	s.metadataMu.Lock()
	cached := s.metadataCache
	s.metadataMu.Unlock()
	if cached != nil {
		return &extproto.GetMetadataResponse{Metadata: metadataToProto(*cached)}, nil
	}
	decls, err := s.impl.Register(ctx)
	if err != nil || len(decls) == 0 {
		// Register 失败不缓存：下次调用可重试（对齐 Python 异常不落 _metadata_cache）
		return &extproto.GetMetadataResponse{}, err
	}
	md := decls[0].Metadata
	s.metadataMu.Lock()
	s.metadataCache = &md
	s.metadataMu.Unlock()
	return &extproto.GetMetadataResponse{Metadata: metadataToProto(md)}, nil
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
