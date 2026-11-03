// Package extproto 提供扩展插件（go-plugin）的 gRPC 协议生成代码。
//
// 本包内容全部由 protoc 从 extension.proto 生成并提交入库，仓库构建不依赖 protoc：
//   - extension.pb.go      protoc-gen-go 生成的消息类型（RegisterRequest 等）
//   - extension_grpc.pb.go protoc-gen-go-grpc 生成的客户端/服务端桩（ExtensionClient/ExtensionServer）
//
// 协议语义对齐 Python jiuwenswarm/extensions/loader.py：
// Register ≈ register_extensions(registry)、SetExtensionDir ≈ set_extension_dir、
// Shutdown ≈ shutdown，无 Initialize RPC。
//
// 修改协议时必须同步重新生成两个 .pb.go 文件，禁止手工编辑生成代码。
//
// 文件目录：
//
//	proto/
//	├── doc.go                   # 包文档
//	├── extension.proto          # 协议源定义（9 个 RPC）
//	├── extension.pb.go          # 生成：消息类型
//	└── extension_grpc.pb.go     # 生成：gRPC 客户端/服务端桩
//
// 对应 Python 代码：jiuwenswarm/extensions/loader.py（协议语义来源）
package extproto
