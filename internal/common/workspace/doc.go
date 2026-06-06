// Package workspace 提供工作区路径管理与初始化功能。
//
// 本包实现了领域1第1.7节"工作区管理"的完整逻辑，包括：
//   - 路径解析：用户目录、配置目录、Agent工作区、资源目录，支持未初始化时的回退逻辑
//   - 工作区初始化：从资源模板复制配置文件、创建目录结构、语言选择
//   - 命名实例管理：多实例配置（instances.yaml）、端口分配、bootstrap .env 生成
//   - 文件变更追踪：增量/覆盖复制，记录新增目录、新增文件、覆盖文件
//
// # 路径体系
//
// 工作区涉及三个核心路径概念：
//
//	WorkspaceDir()  → 数据根目录 ~/.uapclaw/（永不回退）
//	ConfigDir()     → 配置目录 ~/.uapclaw/config/（未初始化时回退到 ResourcesDir）
//	AgentWorkspaceDir() → Agent工作区 ~/.uapclaw/agent/workspace/（未初始化时回退到 ResourcesDir）
//
// ConfigDir 和 AgentWorkspaceDir 的回退逻辑与 Python 版本一致：当 config/ 目录不存在时，
// 临时指向资源目录，使程序无需 init 即可运行（读取内置默认配置）。
//
// # 资源目录
//
// ResourcesDir() 按优先级查找：
//  1. UAPCLAW_RESOURCES_DIR 环境变量
//  2. 可执行文件同级 resources/ 目录
//  3. 当前工作目录 resources/ 目录
//
// 资源目录是模板仓库，仅在 init 时复制到用户目录，以及未初始化时作为配置回退源。
//
// # 命名实例
//
// 默认实例使用 ~/.uapclaw/，命名实例使用 ~/.uapclaw-instances/<name>/。
// 每个实例拥有独立的工作区、端口分配（基础端口 + 索引×1000）和 bootstrap .env。
// 实例配置统一存储在 ~/.uapclaw/instances.yaml 中。
//
// # 初始化流程
//
// Init() 函数执行完整的初始化流程：
//  1. 验证命名实例名称
//  2. 确定目标目录（默认或命名实例）
//  3. 覆盖确认（交互式）
//  4. 语言选择（交互式，支持 zh/en）
//  5. Prepare() 复制模板文件、创建目录、写入语言偏好
//  6. 命名实例额外：更新 instances.yaml + 生成 bootstrap .env
//
// # 文件目录
//
//	workspace/
//	├── doc.go           # 包文档
//	├── paths.go         # 路径解析与回退逻辑、18个路径辅助函数
//	├── init.go          # 工作区初始化（Init/Prepare/语言选择/多语言文件映射）
//	├── instance.go      # 命名实例：配置模型、名称验证、端口分配、instances.yaml 管理
//	├── bootstrap.go     # 实例 bootstrap .env 生成
//	└── copydiff.go      # 文件/目录复制与变更追踪
//
// 对应 Python 代码：
//   - jiuwenswarm/common/utils.py（路径管理、_resolve_paths、prepare_workspace）
//   - jiuwenswarm/init_workspace.py（init 命令入口）
//   - jiuwenswarm/instance_manager/（命名实例配置、yaml、bootstrap、锁、状态）
package workspace
