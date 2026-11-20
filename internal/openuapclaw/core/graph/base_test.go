package graph

import (
	"testing"

	pregel "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
)

// 编译期断言：Router 别名指向 pregel.Selector（spec 拍板 1）
func TestRouterAlias(t *testing.T) {
	var _ Router = pregel.SelectorFunc(nil)
}

// 编译期断言：*PregelGraph 满足 Graph 接口（Task 2/3/4 落地后应通过）
func TestPregelGraphImplementsGraph(t *testing.T) {
	var _ Graph = (*PregelGraph)(nil)
}
