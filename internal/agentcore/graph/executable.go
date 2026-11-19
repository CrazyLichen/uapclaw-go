// 本文件为可执行体契约的锚点别名层（偏差 D-17-8 破环迁移）：
// 契约本体（StreamChunk/BaseExecutable/Executable/GeneralExecutor）已迁至
// graph/executable 叶包（workflow/components 需实现该契约但不引 graph 根，
// 否则 components→graph→session→stream_actor→components 成环）。
// 泛型类型别名（Go 1.24+）保持 graph.Executable 等锚点不变，
// 与 executable.Executable 为同一类型身份。
package graph

import "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"

// StreamChunk 流式元素（本体见 executable 叶包）
type StreamChunk[O any] = executable.StreamChunk[O]

// BaseExecutable Executable 嵌入基座（本体见 executable 叶包）
type BaseExecutable[I, O any] = executable.BaseExecutable[I, O]

// Executable 可执行节点契约（本体见 executable 叶包）
type Executable[I, O any] = executable.Executable[I, O]

// GeneralExecutor 最一般的执行器（本体见 executable 叶包）
type GeneralExecutor = executable.GeneralExecutor
