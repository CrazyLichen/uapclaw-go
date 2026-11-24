// ReActAgentComp 组合面（Python react_component.py 26 行一比一）。
package react

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// AddComponent 组合面基座承载（Python add_component L22-124：
// graph.add_node(node_id, self.executable, wait_for_all=wait_for_all) →
// WorkflowComponent.AddComponent 同语义）。
// 编译期断言：组合面实现 ComponentComposable 接口。
var _ components.ComponentComposable = (*ReActAgentComp)(nil)

// toExecutable 已在 react_executable.go 落位（toExecutableCached）；
// GeneralExecutor 面由 WorkflowComponent 基座承载。
var _ executable.GeneralExecutor = (*ReActExecutable)(nil)
