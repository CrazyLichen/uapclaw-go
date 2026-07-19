package tool_call

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBeamSearchMethod 用于测试的模拟 BeamSearchMethod
type mockBeamSearchMethod struct {
	// stepFunc 自定义 Step 实现
	stepFunc func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error)
	// examplesFunc 自定义 GetExamples 实现
	examplesFunc func(ctx context.Context, tool map[string]any) any
	// stepCalls 记录 Step 调用次数
	stepCalls int64
}

// Step 实现 BeamSearchMethod 接口
func (m *mockBeamSearchMethod) Step(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
	atomic.AddInt64(&m.stepCalls, 1)
	if m.stepFunc != nil {
		return m.stepFunc(ctx, tool, examples, prevOutputs, it)
	}
	return fmt.Sprintf("output-%d", it), fmt.Sprintf("data-%d", it), float64((it + 1) * 10), nil
}

// GetExamples 实现 BeamSearchMethod 接口
func (m *mockBeamSearchMethod) GetExamples(ctx context.Context, tool map[string]any) any {
	if m.examplesFunc != nil {
		return m.examplesFunc(ctx, tool)
	}
	return "mock-examples"
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewBeamSearch_默认值 测试默认参数创建
func TestNewBeamSearch_默认值(t *testing.T) {
	m := &mockBeamSearchMethod{}
	bs := NewBeamSearch(m)

	if bs.beamWidth != 1 {
		t.Errorf("beamWidth = %d, want 1", bs.beamWidth)
	}
	if bs.expandNum != 1 {
		t.Errorf("expandNum = %d, want 1", bs.expandNum)
	}
	if bs.maxDepth != 3 {
		t.Errorf("maxDepth = %d, want 3", bs.maxDepth)
	}
	if bs.numWorkers != 1 {
		t.Errorf("numWorkers = %d, want 1", bs.numWorkers)
	}
	if bs.earlyStop != true {
		t.Errorf("earlyStop = %v, want true", bs.earlyStop)
	}
	if bs.maxScore != 100.0 {
		t.Errorf("maxScore = %v, want 100.0", bs.maxScore)
	}
	if bs.topK != 1 {
		t.Errorf("topK = %d, want 1", bs.topK)
	}
}

// TestNewBeamSearch_自定义选项 测试 Functional Options
func TestNewBeamSearch_自定义选项(t *testing.T) {
	m := &mockBeamSearchMethod{}
	bs := NewBeamSearch(m,
		WithBeamWidth(5),
		WithExpandNum(3),
		WithMaxDepth(10),
		WithNumWorkers(4),
		WithVerbose(true),
		WithEarlyStop(false),
		WithCheckValid(true),
		WithMaxScore(50.0),
		WithTopK(3),
		WithTimeout(120.0),
		WithNumRetry(5),
	)

	if bs.beamWidth != 5 {
		t.Errorf("beamWidth = %d, want 5", bs.beamWidth)
	}
	if bs.expandNum != 3 {
		t.Errorf("expandNum = %d, want 3", bs.expandNum)
	}
	if bs.maxDepth != 10 {
		t.Errorf("maxDepth = %d, want 10", bs.maxDepth)
	}
	if bs.numWorkers != 4 {
		t.Errorf("numWorkers = %d, want 4", bs.numWorkers)
	}
	if bs.verbose != true {
		t.Errorf("verbose = %v, want true", bs.verbose)
	}
	if bs.earlyStop != false {
		t.Errorf("earlyStop = %v, want false", bs.earlyStop)
	}
	if bs.checkValid != true {
		t.Errorf("checkValid = %v, want true", bs.checkValid)
	}
	if bs.maxScore != 50.0 {
		t.Errorf("maxScore = %v, want 50.0", bs.maxScore)
	}
	if bs.topK != 3 {
		t.Errorf("topK = %d, want 3", bs.topK)
	}
	if bs.timeout != 120.0 {
		t.Errorf("timeout = %v, want 120.0", bs.timeout)
	}
	if bs.numRetry != 5 {
		t.Errorf("numRetry = %d, want 5", bs.numRetry)
	}
}

// TestTreeNode_基本操作 测试 TreeNode 创建和基本操作
func TestTreeNode_基本操作(t *testing.T) {
	node := newTreeNode("root-data", 90.0, "root-result", nil)
	if node.Data != "root-data" {
		t.Errorf("Data = %v, want root-data", node.Data)
	}
	if node.Score != 90.0 {
		t.Errorf("Score = %v, want 90.0", node.Score)
	}
	if node.Results != "root-result" {
		t.Errorf("Results = %v, want root-result", node.Results)
	}
	if len(node.History) != 1 || node.History[0] != "root-result" {
		t.Errorf("History = %v, want [root-result]", node.History)
	}
	if node.Parent != nil {
		t.Errorf("Parent should be nil")
	}
	if node.GetDepth() != 0 {
		t.Errorf("GetDepth() = %d, want 0", node.GetDepth())
	}
}

// TestTreeNode_父子关系 测试 TreeNode 父子关系和深度计算
func TestTreeNode_父子关系(t *testing.T) {
	root := newTreeNode("root", 80.0, "r0", nil)
	child := newTreeNode("child", 90.0, "c0", root.History)
	child.Parent = root
	root.Children = append(root.Children, child)

	if child.GetDepth() != 1 {
		t.Errorf("child GetDepth() = %d, want 1", child.GetDepth())
	}
	if len(root.Children) != 1 {
		t.Errorf("root Children len = %d, want 1", len(root.Children))
	}
	if len(child.History) != 2 {
		t.Errorf("child History len = %d, want 2", len(child.History))
	}
}

// TestTreeNode_History继承 测试 History 正确继承并追加
func TestTreeNode_History继承(t *testing.T) {
	root := newTreeNode("d0", 50.0, "r0", nil)                 // History: [r0]
	child := newTreeNode("d1", 60.0, "r1", root.History)       // History: [r0, r1]
	grandChild := newTreeNode("d2", 70.0, "r2", child.History) // History: [r0, r1, r2]

	if len(root.History) != 1 {
		t.Errorf("root History len = %d, want 1", len(root.History))
	}
	if len(child.History) != 2 {
		t.Errorf("child History len = %d, want 2", len(child.History))
	}
	if len(grandChild.History) != 3 {
		t.Errorf("grandChild History len = %d, want 3", len(grandChild.History))
	}
	if grandChild.History[0] != "r0" || grandChild.History[1] != "r1" || grandChild.History[2] != "r2" {
		t.Errorf("grandChild History = %v, want [r0 r1 r2]", grandChild.History)
	}
}

// TestTreeNode_String 测试 TreeNode 字符串表示
func TestTreeNode_String(t *testing.T) {
	root := newTreeNode("d0", 80.0, "r0", nil)
	child := newTreeNode("d1", 90.0, "r1", root.History)
	child.Parent = root
	root.Children = append(root.Children, child)

	s := root.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
	// 根节点应该在第一行
	if len(s) > 0 && s[0] == ' ' {
		t.Error("Root node String should not start with space")
	}
}

// TestTreeNode_HistoryCopy 测试 History 是独立副本，修改不影响原始
func TestTreeNode_HistoryCopy(t *testing.T) {
	origHistory := []any{"a", "b"}
	node := newTreeNode("d", 50.0, "c", origHistory)
	// 修改原始 slice 不应影响 node.History
	origHistory[0] = "changed"
	if node.History[0] == "changed" {
		t.Error("History should be a copy, not a reference to the original")
	}
}

// TestSearch_基本流程 测试基本搜索流程
func TestSearch_基本流程(t *testing.T) {
	m := &mockBeamSearchMethod{}
	bs := NewBeamSearch(m,
		WithMaxDepth(2),
		WithBeamWidth(2),
		WithExpandNum(1),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	tool := map[string]any{"name": "test-tool"}
	results, err := bs.Search(ctx, tool)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() should return at least one result")
	}
	// 每个结果应该有历史路径
	for i, history := range results {
		if len(history) == 0 {
			t.Errorf("result[%d] history should not be empty", i)
		}
	}
}

// TestSearch_早停 测试早停机制
// 对齐 Python：当 top-k 节点分数均达到 maxScore 时触发早停
// 注意：早停发生在 expand 之前，首次触发时若 root 已达 maxScore，则无 depth>0 节点，结果为空
func TestSearch_早停(t *testing.T) {
	t.Run("根节点即达满分_结果为空", func(t *testing.T) {
		m := &mockBeamSearchMethod{
			stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
				return "output", "data", 100.0, nil
			},
		}
		bs := NewBeamSearch(m,
			WithMaxDepth(5),
			WithBeamWidth(2),
			WithExpandNum(1),
			WithEarlyStop(true),
			WithMaxScore(100.0),
		)
		ctx := context.Background()
		results, err := bs.Search(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("Search() error: %v", err)
		}
		// 根节点 score=100.0，depth=0 时即触发早停，无 depth>0 节点
		if len(results) != 0 {
			t.Errorf("results count = %d, want 0 (early stop at root, no depth>0 nodes)", len(results))
		}
	})

	t.Run("扩展后触发早停", func(t *testing.T) {
		m := &mockBeamSearchMethod{
			stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
				// depth 0 返回低分，depth 1 返回满分
				if it == 0 {
					return "output", "data", 50.0, nil
				}
				return "output", "data", 100.0, nil
			},
		}
		bs := NewBeamSearch(m,
			WithMaxDepth(5),
			WithBeamWidth(2),
			WithExpandNum(1),
			WithEarlyStop(true),
			WithMaxScore(100.0),
		)
		ctx := context.Background()
		results, err := bs.Search(ctx, map[string]any{})
		if err != nil {
			t.Fatalf("Search() error: %v", err)
		}
		// depth 0 score=50 不触发早停 → expand 到 depth 1 score=100 → 下轮早停
		if len(results) == 0 {
			t.Fatal("Search() should return results after expand+early_stop at depth 1")
		}
	})
}

// TestSearch_不早停 测试禁用早停
func TestSearch_不早停(t *testing.T) {
	callCount := int64(0)
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			atomic.AddInt64(&callCount, 1)
			return "output", "data", 100.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(3),
		WithBeamWidth(1),
		WithExpandNum(1),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	_, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	// 禁用早停，应该遍历到 maxDepth
	// 1(root) + 1(depth1) + 1(depth2) + 1(depth3) = 4
	if callCount != 4 {
		t.Errorf("callCount = %d, want 4", callCount)
	}
}

// TestSearch_根节点生成失败 测试根节点生成重试
func TestSearch_根节点生成失败(t *testing.T) {
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			if it == 0 {
				return nil, nil, -1, nil
			}
			return "output", "data", 50.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(1),
		WithCheckValid(true),
		WithNumRetry(3),
	)

	ctx := context.Background()
	_, err := bs.Search(ctx, map[string]any{})
	// 因为 it==0 时 score=-1 且 checkValid=true，重试3次后应返回错误
	if err == nil {
		t.Error("Search() should return error when root node generation fails")
	}
}

// TestSearch_根节点生成部分成功 测试根节点重试最终成功
func TestSearch_根节点生成部分成功(t *testing.T) {
	callCount := int64(0)
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			count := atomic.AddInt64(&callCount, 1)
			if it == 0 && count <= 2 {
				return nil, nil, -1, nil
			}
			return "output", "data", 80.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(1),
		WithCheckValid(true),
		WithNumRetry(5),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	_, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() should succeed after retries, got error: %v", err)
	}
}

// TestSearch_根节点Step错误 测试 Step 返回 error 时的重试
func TestSearch_根节点Step错误(t *testing.T) {
	callCount := int64(0)
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			count := atomic.AddInt64(&callCount, 1)
			if it == 0 && count <= 1 {
				return nil, nil, 0, fmt.Errorf("transient error")
			}
			return "output", "data", 80.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(1),
		WithNumRetry(3),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	_, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() should succeed after Step error retries, got: %v", err)
	}
}

// TestSearch_超时 测试超时机制
func TestSearch_超时(t *testing.T) {
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			time.Sleep(100 * time.Millisecond)
			return "output", "data", 50.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(100),
		WithBeamWidth(1),
		WithExpandNum(1),
		WithTimeout(0.3), // 0.3 秒超时
		WithEarlyStop(false),
	)

	ctx := context.Background()
	start := time.Now()
	results, err := bs.Search(ctx, map[string]any{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	// 应该在合理时间内返回（超时机制生效）
	if elapsed > 2*time.Second {
		t.Errorf("Search() took too long: %v, timeout should have kicked in", elapsed)
	}
	if len(results) == 0 {
		t.Fatal("Search() should return at least one result even on timeout")
	}
}

// TestSearch_TopK 测试 Top-K 结果
func TestSearch_TopK(t *testing.T) {
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			// 根据 prevOutputs 长度递增分数，模拟不同分数的节点
			score := float64(len(prevOutputs)*10 + 50)
			return fmt.Sprintf("output-%d-%d", it, len(prevOutputs)), fmt.Sprintf("data-%d", it), score, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(2),
		WithBeamWidth(3),
		WithExpandNum(2),
		WithTopK(2),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	results, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("results count = %d, want at most 2 (topK=2)", len(results))
	}
}

// TestSearch_并行扩展 测试并行扩展模式
func TestSearch_并行扩展(t *testing.T) {
	callCount := int64(0)
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			atomic.AddInt64(&callCount, 1)
			return fmt.Sprintf("output-%d", atomic.LoadInt64(&callCount)), "data", 80.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(2),
		WithBeamWidth(2),
		WithExpandNum(2),
		WithNumWorkers(4),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	results, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() should return at least one result")
	}
}

// TestSearch_串行与并行结果一致 测试串行和并行模式产生相同数量的结果
func TestSearch_串行与并行结果一致(t *testing.T) {
	serialCount := int64(0)
	parallelCount := int64(0)

	serialMethod := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			atomic.AddInt64(&serialCount, 1)
			return "output", "data", float64(it*10 + 50), nil
		},
	}
	parallelMethod := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			atomic.AddInt64(&parallelCount, 1)
			return "output", "data", float64(it*10 + 50), nil
		},
	}

	bsSerial := NewBeamSearch(serialMethod,
		WithMaxDepth(2),
		WithBeamWidth(2),
		WithExpandNum(2),
		WithNumWorkers(1),
		WithEarlyStop(false),
	)
	bsParallel := NewBeamSearch(parallelMethod,
		WithMaxDepth(2),
		WithBeamWidth(2),
		WithExpandNum(2),
		WithNumWorkers(4),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	serialResults, err := bsSerial.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Serial Search() error: %v", err)
	}
	parallelResults, err := bsParallel.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Parallel Search() error: %v", err)
	}

	if len(serialResults) != len(parallelResults) {
		t.Errorf("serial results count = %d, parallel results count = %d, want equal",
			len(serialResults), len(parallelResults))
	}
	if serialCount != parallelCount {
		t.Errorf("serial callCount = %d, parallel callCount = %d, want equal",
			serialCount, parallelCount)
	}
}

// TestCheckEarlyStop 测试早停检查
func TestCheckEarlyStop(t *testing.T) {
	bs := NewBeamSearch(&mockBeamSearchMethod{})

	// beam_list 不足 k 个
	nodes := []*TreeNode{
		newTreeNode("d1", 100.0, "r1", nil),
	}
	if bs.checkEarlyStop(nodes, 100.0, 2) {
		t.Error("should not early stop when beam_list < k")
	}

	// 有节点分数不足
	nodes = []*TreeNode{
		newTreeNode("d1", 100.0, "r1", nil),
		newTreeNode("d2", 90.0, "r2", nil),
	}
	if bs.checkEarlyStop(nodes, 100.0, 2) {
		t.Error("should not early stop when some nodes < maxScore")
	}

	// 所有 top-k 节点分数达标
	nodes = []*TreeNode{
		newTreeNode("d1", 100.0, "r1", nil),
		newTreeNode("d2", 100.0, "r2", nil),
	}
	if !bs.checkEarlyStop(nodes, 100.0, 2) {
		t.Error("should early stop when all top-k nodes >= maxScore")
	}
}

// TestPrune 测试剪枝
func TestPrune(t *testing.T) {
	bs := NewBeamSearch(&mockBeamSearchMethod{}, WithBeamWidth(2))

	nodes := []*TreeNode{
		newTreeNode("d1", 50.0, "r1", nil),
		newTreeNode("d2", 90.0, "r2", nil),
		newTreeNode("d3", 70.0, "r3", nil),
		newTreeNode("d4", 60.0, "r4", nil),
	}

	pruned := bs.prune(nodes)
	if len(pruned) != 2 {
		t.Fatalf("pruned len = %d, want 2", len(pruned))
	}
	// 应保留分数最高的两个
	if pruned[0].Score != 90.0 {
		t.Errorf("pruned[0].Score = %v, want 90.0", pruned[0].Score)
	}
	if pruned[1].Score != 70.0 {
		t.Errorf("pruned[1].Score = %v, want 70.0", pruned[1].Score)
	}
}

// TestPrune_节点数不足BeamWidth 测试节点数少于 beamWidth 时不越界
func TestPrune_节点数不足BeamWidth(t *testing.T) {
	bs := NewBeamSearch(&mockBeamSearchMethod{}, WithBeamWidth(10))

	nodes := []*TreeNode{
		newTreeNode("d1", 50.0, "r1", nil),
		newTreeNode("d2", 90.0, "r2", nil),
	}

	pruned := bs.prune(nodes)
	if len(pruned) != 2 {
		t.Fatalf("pruned len = %d, want 2", len(pruned))
	}
}

// TestSearch_过滤根节点 测试返回结果不包含 depth=0 的根节点
func TestSearch_过滤根节点(t *testing.T) {
	m := &mockBeamSearchMethod{}
	bs := NewBeamSearch(m,
		WithMaxDepth(2),
		WithBeamWidth(1),
		WithExpandNum(1),
		WithEarlyStop(false),
		WithTopK(5),
	)

	ctx := context.Background()
	results, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	for _, history := range results {
		if len(history) == 0 {
			t.Error("result history should not be empty (root node should be filtered out)")
		}
	}
}

// TestSearch_GetExamples被调用 测试 GetExamples 在搜索中被调用
func TestSearch_GetExamples被调用(t *testing.T) {
	examplesCalled := false
	m := &mockBeamSearchMethod{
		examplesFunc: func(ctx context.Context, tool map[string]any) any {
			examplesCalled = true
			return "test-examples"
		},
	}
	bs := NewBeamSearch(m, WithMaxDepth(1), WithEarlyStop(false))

	ctx := context.Background()
	_, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if !examplesCalled {
		t.Error("GetExamples should have been called")
	}
}

// TestSearch_并发安全 测试并发扩展的安全性
func TestSearch_并发安全(t *testing.T) {
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			// 模拟一些处理延迟
			time.Sleep(time.Millisecond)
			return "output", "data", float64(it*10 + 50), nil
		},
	}

	bs := NewBeamSearch(m,
		WithMaxDepth(3),
		WithBeamWidth(3),
		WithExpandNum(2),
		WithNumWorkers(8),
		WithEarlyStop(false),
	)

	ctx := context.Background()
	results, err := bs.Search(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() should return at least one result")
	}
}

// TestHistoriesFromNodes 测试从节点提取历史
func TestHistoriesFromNodes(t *testing.T) {
	nodes := []*TreeNode{
		newTreeNode("d1", 50.0, "r1", nil),
		newTreeNode("d2", 60.0, "r2", []any{"prev"}),
	}

	histories := historiesFromNodes(nodes)
	if len(histories) != 2 {
		t.Fatalf("len = %d, want 2", len(histories))
	}
	if len(histories[0]) != 1 || histories[0][0] != "r1" {
		t.Errorf("histories[0] = %v, want [r1]", histories[0])
	}
	if len(histories[1]) != 2 || histories[1][0] != "prev" || histories[1][1] != "r2" {
		t.Errorf("histories[1] = %v, want [prev r2]", histories[1])
	}
}

// TestSortAndTakeTopK 测试排序取 Top-K
func TestSortAndTakeTopK(t *testing.T) {
	bs := NewBeamSearch(&mockBeamSearchMethod{})

	nodes := []*TreeNode{
		newTreeNode("d1", 30.0, "r1", nil),
		newTreeNode("d2", 90.0, "r2", nil),
		newTreeNode("d3", 50.0, "r3", nil),
		newTreeNode("d4", 70.0, "r4", nil),
	}

	top := bs.sortAndTakeTopK(nodes, 2)
	if len(top) != 2 {
		t.Fatalf("len = %d, want 2", len(top))
	}
	if top[0].Score != 90.0 {
		t.Errorf("top[0].Score = %v, want 90.0", top[0].Score)
	}
	if top[1].Score != 70.0 {
		t.Errorf("top[1].Score = %v, want 70.0", top[1].Score)
	}
}

// TestSearch_Context取消 测试 context 取消时 Search 能正常处理
func TestSearch_Context取消(t *testing.T) {
	m := &mockBeamSearchMethod{
		stepFunc: func(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error) {
			// 检查 context 是否已取消
			select {
			case <-ctx.Done():
				return nil, nil, 0, ctx.Err()
			default:
			}
			return "output", "data", 50.0, nil
		},
	}
	bs := NewBeamSearch(m,
		WithMaxDepth(5),
		WithBeamWidth(1),
		WithExpandNum(1),
		WithEarlyStop(false),
		WithNumRetry(3),
	)

	ctx, cancel := context.WithCancel(context.Background())
	// 在第一次调用后取消 context
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Search 应该能正常结束（不 panic），可能返回错误或部分结果
	bs.Search(ctx, map[string]any{})
}
