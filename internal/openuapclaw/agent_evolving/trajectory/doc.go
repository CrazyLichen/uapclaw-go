// Package trajectory 提供自演化系统的规范化执行轨迹：OTLP 不可变值对象、
// 无状态 span 访问/变换层、团队 span 选择器、v2 事件窗口回放、聊天消息投影、
// 进程内 span 扇出 processor 与同步持久化。
// Trajectory 是优化器 backward 阶段的单源真相（single source of truth）。
//
// 核心模块（对齐 Python: openjiuwen/agent_evolving/trajectory/，一文件对一文件）：
//   - model.go：Trajectory = OTLP payload 不可变包装（model.py）
//   - spans.go：规范化 span 的无状态访问/变换/合并/裁剪 + 语义访问器（spans.py）
//   - team.go：团队 span 选择器与森林（team.py）
//   - windows.go：v2 事件窗口回放（windows.py）
//   - messages.go：span → 聊天消息投影（messages.py）
//   - processor.go：进程内 OTel SpanProcessor 扇出与订阅（processor.py）
//   - builder.go：离线规范化组装器（offline/builder.py）
//   - extractor.go：Session tracer → 规范 OTLP 抽取器（offline/extractor.py）
//   - store.go：同步持久化 InMemory/File（store.py）
//   - schema.go：轨迹自有属性键（schema.py）
//   - serialization.go：JSON 兼容值转换（serialization.py）
//
// 12-04 二版：删除 step 模型（TrajectoryStep/LLMCallDetail/ToolCallDetail、
// TeamTrajectoryAggregator、InMemoryTrajectoryRegistry——上游 bd05f826 已删除
// 历史读路径，aggregator/registry 为 Go 侧旧模型遗产），全部消费方迁移到
// span 模型（规则 8）。
//
// 文件目录：
//
//	trajectory/
//	├── doc.go           # 包文档
//	├── schema.go        # 轨迹自有属性键
//	├── serialization.go # JSON 兼容值转换
//	├── model.go         # Trajectory 不可变值对象
//	├── spans.go         # span 无状态访问/变换层
//	├── team.go          # 团队 span 选择器
//	├── windows.go       # v2 事件窗口回放
//	├── messages.go      # 聊天消息投影
//	├── builder.go       # 离线组装器
//	├── extractor.go     # Session 抽取器
//	├── store.go         # 同步持久化
//	├── processor.go     # 进程内 span 扇出 processor（25.2b 新增）
//	└── json_safe.go     # JSONSafe + MessageToDict + responseToText
//
// 对应 Python 代码：openjiuwen/agent_evolving/trajectory/
package trajectory
