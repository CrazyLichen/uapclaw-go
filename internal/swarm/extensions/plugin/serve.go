package plugin

import (
	"context"
	"errors"

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
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrNoTransport 插件未实现 TransportImpl 却被调用 transport RPC
var ErrNoTransport = errors.New("扩展未提供 transport 能力")

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
		}
		if s.transportImpl == nil && d.ProvidesTransport {
			// 声明与实现不符：降级为非 transport（忽略该能力声明）
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
