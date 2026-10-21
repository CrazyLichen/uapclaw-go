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
//	├── host.go               # 握手常量 + PluginClient + LoadPluginClient
//	├── transport_proxy.go    # GRPCPluginTransport 实现 transport.AgentTransport
//	├── serve.go              # 插件作者 SDK：ServeExtension + ExtensionImpl 接口
//	└── testdata/echo/        # 端到端测试用 echo 插件源码（TestMain 编译）
//
// 对应 Python 代码：jiuwenswarm/extensions/loader.py（加载语义）+ sdk/base.py（扩展形态）
//
// 覆盖率说明：ServeExtension/ServeExtensionWithTransport（插件侧 main 入口）
// 调用 plugin.Serve 会阻塞并劫持 stdout，无法在测试进程内安全调用，
// 由 e2e_test.go 的真实 echo 子进程端到端覆盖（92.3% 覆盖率豁免此项）。
package plugin
