package tool_call

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BeamSearchMethod Beam Search 所需的方法接口。
// 对齐 Python: BeamSearch.method — 需实现 step() 和 get_examples()
type BeamSearchMethod interface {
	// Step 执行单步扩展，返回 output/data/score。
	// 对齐 Python: method.step(tool, examples, prev_outputs, it)
	Step(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (output any, data any, score float64, err error)
	// GetExamples 获取工具的示例数据。
	// 对齐 Python: method.get_examples(tool)
	GetExamples(ctx context.Context, tool map[string]any) any
}

// TreeNode Beam Search 树节点。
// 对齐 Python: TreeNode
type TreeNode struct {
	// Data 节点数据
	Data any
	// Score 节点分数
	Score float64
	// Results 节点结果
	Results any
	// History 历史结果路径
	History []any
	// Parent 父节点
	Parent *TreeNode
	// Children 子节点列表
	Children []*TreeNode
}

// BeamSearch Beam Search 搜索器。
// 对齐 Python: BeamSearch
type BeamSearch struct {
	// method 搜索方法
	method BeamSearchMethod
	// beamWidth Beam 宽度
	beamWidth int
	// expandNum 每个节点扩展数量
	expandNum int
	// maxDepth 最大深度
	maxDepth int
	// numWorkers 并行 worker 数量
	numWorkers int
	// verbose 是否打印详细信息
	verbose bool
	// earlyStop 是否启用早停
	earlyStop bool
	// checkValid 是否检查有效性（score == -1 视为无效）
	checkValid bool
	// maxScore 最大分数阈值（早停用）
	maxScore float64
	// topK 返回最优的 top-K 结果
	topK int
	// timeout 超时时间（秒）
	timeout float64
	// numRetry 重试次数
	numRetry int
}

// BeamSearchOption BeamSearch 函数选项。
type BeamSearchOption func(*BeamSearch)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultBeamSearchTimeout 默认超时时间（秒）
	defaultBeamSearchTimeout float64 = 600
	// defaultBeamSearchNumRetry 默认重试次数
	defaultBeamSearchNumRetry int = 1
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBeamSearch 创建 BeamSearch 实例。
// 对齐 Python: BeamSearch.__init__
func NewBeamSearch(method BeamSearchMethod, opts ...BeamSearchOption) *BeamSearch {
	bs := &BeamSearch{
		method:     method,
		beamWidth:  1,
		expandNum:  1,
		maxDepth:   3,
		numWorkers: 1,
		verbose:    false,
		earlyStop:  true,
		checkValid: false,
		maxScore:   100.0,
		topK:       1,
		timeout:    defaultBeamSearchTimeout,
		numRetry:   defaultBeamSearchNumRetry,
	}
	for _, opt := range opts {
		opt(bs)
	}
	return bs
}

// WithBeamWidth 设置 Beam 宽度。
func WithBeamWidth(w int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.beamWidth = w }
}

// WithExpandNum 设置每个节点扩展数量。
func WithExpandNum(n int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.expandNum = n }
}

// WithMaxDepth 设置最大深度。
func WithMaxDepth(d int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.maxDepth = d }
}

// WithNumWorkers 设置并行 worker 数量。
func WithNumWorkers(n int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.numWorkers = n }
}

// WithVerbose 设置是否打印详细信息。
func WithVerbose(v bool) BeamSearchOption {
	return func(bs *BeamSearch) { bs.verbose = v }
}

// WithEarlyStop 设置是否启用早停。
func WithEarlyStop(v bool) BeamSearchOption {
	return func(bs *BeamSearch) { bs.earlyStop = v }
}

// WithCheckValid 设置是否检查有效性。
func WithCheckValid(v bool) BeamSearchOption {
	return func(bs *BeamSearch) { bs.checkValid = v }
}

// WithMaxScore 设置最大分数阈值。
func WithMaxScore(s float64) BeamSearchOption {
	return func(bs *BeamSearch) { bs.maxScore = s }
}

// WithTopK 设置返回最优的 top-K 结果。
func WithTopK(k int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.topK = k }
}

// WithTimeout 设置超时时间（秒）。
func WithTimeout(t float64) BeamSearchOption {
	return func(bs *BeamSearch) { bs.timeout = t }
}

// WithNumRetry 设置重试次数。
func WithNumRetry(n int) BeamSearchOption {
	return func(bs *BeamSearch) { bs.numRetry = n }
}

// GetDepth 返回节点深度。
// 对齐 Python: TreeNode.get_depth
func (n *TreeNode) GetDepth() int {
	if n.Parent == nil {
		return 0
	}
	return n.Parent.GetDepth() + 1
}

// String 返回节点的树形字符串表示。
// 对齐 Python: TreeNode.__repr__
func (n *TreeNode) String() string {
	depth := n.GetDepth()
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "    "
	}
	s := fmt.Sprintf("%sit=%d score=%.1f data=\"%v\"", indent, depth, n.Score, n.Data)
	for _, child := range n.Children {
		s += "\n" + child.String()
	}
	return s
}

// Search 执行 Beam Search，返回 top-K 节点的历史路径。
// 对齐 Python: BeamSearch.search
func (bs *BeamSearch) Search(ctx context.Context, tool map[string]any) ([][]any, error) {
	startTime := time.Now()

	// 获取示例
	var examples any
	if bs.method != nil {
		examples = bs.method.GetExamples(ctx, tool)
	}

	// 生成根节点
	root, err := bs.generateRoot(ctx, tool, examples)
	if err != nil {
		return nil, err
	}

	beamList := []*TreeNode{root}
	bestNodes := []*TreeNode{root}

	// 迭代扩展和剪枝
	for depth := 1; depth <= bs.maxDepth; depth++ {
		// 超时检查
		if time.Since(startTime).Seconds() > bs.timeout {
			logger.Warn(logComponent).
				Float64("elapsed_seconds", time.Since(startTime).Seconds()).
				Float64("timeout", bs.timeout).
				Int("depth", depth).
				Msg("Beam Search 超时，返回当前最优结果")
			nodesSorted := bs.sortAndTakeTopK(bestNodes, bs.topK)
			return historiesFromNodes(nodesSorted), nil
		}

		// 早停检查
		if bs.earlyStop && bs.checkEarlyStop(beamList, bs.maxScore, bs.topK) {
			logger.Info(logComponent).
				Int("depth", depth).
				Msg("Beam Search 早停触发")
			break
		}

		// 扩展
		beamList, err = bs.expand(ctx, beamList, tool, examples, depth)
		if err != nil {
			return nil, err
		}

		// 剪枝
		beamList = bs.prune(beamList)

		bestNodes = append(bestNodes, beamList...)
	}

	// 过滤 depth > 0 的节点，按分数降序排列，取 top-K
	filtered := make([]*TreeNode, 0, len(bestNodes))
	for _, node := range bestNodes {
		if node.GetDepth() > 0 {
			filtered = append(filtered, node)
		}
	}
	nodesSorted := bs.sortAndTakeTopK(filtered, bs.topK)
	return historiesFromNodes(nodesSorted), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTreeNode 创建树节点。
// 对齐 Python: TreeNode.__init__
func newTreeNode(data any, score float64, results any, history []any) *TreeNode {
	h := make([]any, len(history))
	copy(h, history)
	h = append(h, results)
	return &TreeNode{
		Data:    data,
		Score:   score,
		Results: results,
		History: h,
	}
}

// generateRoot 尝试生成根节点。
// 对齐 Python: BeamSearch.search 中根节点生成循环
func (bs *BeamSearch) generateRoot(ctx context.Context, tool map[string]any, examples any) (*TreeNode, error) {
	for i := 0; i < bs.numRetry; i++ {
		output, data, score, err := bs.method.Step(ctx, tool, examples, nil, 0)
		if err != nil {
			logger.Error(logComponent).Err(err).Int("retry", i).Msg("根节点 Step 调用失败")
			continue
		}
		if bs.checkValid && score == -1 {
			logger.Warn(logComponent).
				Float64("score", score).
				Int("retry", i).
				Msg("根节点校验无效，重试")
			continue
		}
		root := newTreeNode(data, score, output, nil)
		return root, nil
	}
	return nil, fmt.Errorf("重试 %d 次后仍无法生成有效根节点", bs.numRetry)
}

// expand 扩展当前 Beam 列表。
// 对齐 Python: BeamSearch.expand
func (bs *BeamSearch) expand(ctx context.Context, beamList []*TreeNode, tool map[string]any, examples any, depth int) ([]*TreeNode, error) {
	if bs.numWorkers <= 1 {
		return bs.expandSerial(ctx, beamList, tool, examples, depth)
	}
	return bs.expandParallel(ctx, beamList, tool, examples, depth)
}

// expandSerial 串行扩展。
func (bs *BeamSearch) expandSerial(ctx context.Context, beamList []*TreeNode, tool map[string]any, examples any, depth int) ([]*TreeNode, error) {
	var newBeamList []*TreeNode
	for _, node := range beamList {
		for j := 0; j < bs.expandNum; j++ {
			newNode, err := bs.expandSingleStep(ctx, node, tool, examples, depth)
			if err != nil {
				logger.Error(logComponent).Err(err).
					Int("depth", depth).
					Int("expand_index", j).
					Msg("串行扩展单步失败")
				continue
			}
			node.Children = append(node.Children, newNode)
			newBeamList = append(newBeamList, newNode)
		}
	}
	return newBeamList, nil
}

// expandParallel 并行扩展（goroutine + channel）。
// 对齐 Python: ThreadPoolExecutor + as_completed
func (bs *BeamSearch) expandParallel(ctx context.Context, beamList []*TreeNode, tool map[string]any, examples any, depth int) ([]*TreeNode, error) {
	// 计算总任务数
	totalTasks := len(beamList) * bs.expandNum
	type expandResult struct {
		parent *TreeNode
		node   *TreeNode
		err    error
	}
	ch := make(chan expandResult, totalTasks)

	var wg sync.WaitGroup
	// 使用 semaphore 控制并发数
	sem := make(chan struct{}, bs.numWorkers)

	for _, node := range beamList {
		for j := 0; j < bs.expandNum; j++ {
			wg.Add(1)
			sem <- struct{}{} // 获取信号量
			go func(n *TreeNode, expandIdx int) {
				defer wg.Done()
				defer func() { <-sem }() // 释放信号量
				newNode, err := bs.expandSingleStep(ctx, n, tool, examples, depth)
				if err != nil {
					logger.Error(logComponent).Err(err).
						Int("depth", depth).
						Int("expand_index", expandIdx).
						Msg("并行扩展单步失败")
				}
				ch <- expandResult{parent: n, node: newNode, err: err}
			}(node, j)
		}
	}

	// 后台等待所有任务完成后关闭 channel
	go func() {
		wg.Wait()
		close(ch)
	}()

	// 收集结果，统一设置父子关系（避免 DATA RACE）
	var newBeamList []*TreeNode
	for res := range ch {
		if res.err != nil {
			continue
		}
		res.parent.Children = append(res.parent.Children, res.node)
		newBeamList = append(newBeamList, res.node)
	}
	return newBeamList, nil
}

// expandSingleStep 扩展单步。
// 对齐 Python: expand_single_step
// 注意：并行调用时不能修改 node.Children，由调用方统一设置父子关系。
func (bs *BeamSearch) expandSingleStep(ctx context.Context, node *TreeNode, tool map[string]any, examples any, depth int) (*TreeNode, error) {
	var newNode *TreeNode
	for i := 0; i < bs.numRetry; i++ {
		output, data, score, err := bs.method.Step(ctx, tool, examples, node.History, depth)
		if err != nil {
			continue
		}
		if bs.checkValid && score == -1 {
			continue
		}
		newNode = newTreeNode(data, score, output, node.History)
		newNode.Parent = node
		break
	}
	if newNode == nil {
		return nil, fmt.Errorf("扩展单步失败：重试 %d 次后仍无法生成有效节点", bs.numRetry)
	}
	return newNode, nil
}

// prune 剪枝，保留 beamWidth 个最高分节点。
// 对齐 Python: BeamSearch.prune
func (bs *BeamSearch) prune(beamList []*TreeNode) []*TreeNode {
	sorted := make([]*TreeNode, len(beamList))
	copy(sorted, beamList)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	if len(sorted) > bs.beamWidth {
		return sorted[:bs.beamWidth]
	}
	return sorted
}

// checkEarlyStop 检查是否满足早停条件。
// 对齐 Python: BeamSearch.check_early_stop
func (bs *BeamSearch) checkEarlyStop(beamList []*TreeNode, maxScore float64, k int) bool {
	if len(beamList) < k {
		return false
	}
	for _, node := range beamList[:k] {
		if node.Score < maxScore {
			return false
		}
	}
	return true
}

// sortAndTakeTopK 按分数降序排列并取 top-K。
func (bs *BeamSearch) sortAndTakeTopK(nodes []*TreeNode, k int) []*TreeNode {
	sorted := make([]*TreeNode, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	if len(sorted) > k {
		return sorted[:k]
	}
	return sorted
}

// historiesFromNodes 提取节点的历史路径。
func historiesFromNodes(nodes []*TreeNode) [][]any {
	result := make([][]any, len(nodes))
	for i, node := range nodes {
		result[i] = node.History
	}
	return result
}
