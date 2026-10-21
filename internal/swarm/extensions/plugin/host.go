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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EntryFilename 扩展入口二进制文件名（对齐 Python ENTRY_FILENAME 语义）
	EntryFilename = "ext-plugin"
	// logComponent 日志组件（基础设施层）
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPluginClientFromConn 从已有 gRPC 连接构造（测试与本地进程内场景）
func NewPluginClientFromConn(conn *grpc.ClientConn) *PluginClient {
	return &PluginClient{protoClient: extproto.NewExtensionClient(conn), conn: conn}
}

// LoadPluginClient 启动 root 目录下的插件子进程并完成 gRPC 握手（生产入口）。
//
// go-plugin 流程：NewClient(Cmd=ext-plugin) → Client() 握手 → Dispense("extension")
// → ExtensionGRPCPlugin.GRPCClient 返回 connDialer → 取原始 *grpc.ClientConn 直连。
func LoadPluginClient(root string) (*PluginClient, error) {
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
	conn, err := dialPlugin(client)
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

// dialPlugin 通过 go-plugin 的 Dispense 拿到 connDialer，返回原始 gRPC 连接
func dialPlugin(client *plugin.Client) (*grpc.ClientConn, error) {
	rpcClient, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("插件握手失败: %w", err)
	}
	raw, err := rpcClient.Dispense("extension")
	if err != nil {
		return nil, fmt.Errorf("Dispense extension 失败: %w", err)
	}
	// ExtensionGRPCPlugin.GRPCClient 返回 *connDialer 包装
	dialer, ok := raw.(interface{ Conn() *grpc.ClientConn })
	if !ok {
		return nil, fmt.Errorf("插件 Dispense 返回类型非预期: %T", raw)
	}
	return dialer.Conn(), nil
}
