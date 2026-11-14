package artifactrsi

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ArtifactProvider 统一提供方契约，由 AgentServer 路由消费。
// 实现方拥有任务执行与 run_dir 下的持久化；不得自建事件队列或直接调
// WebSocket/Web 推送；on_event 回调须在对应快照持久化之后触发。
// Python: ArtifactProvider（provider.py，@runtime_checkable Protocol）
type ArtifactProvider interface {
	// ArtifactType 提供方处理的 artifact 种类（program/paper）。
	// Python: artifact_type 属性
	ArtifactType() rsi.ArtifactType
	// ValidateInput 启动任务前校验提供方专属输入，不创建任务快照。
	// program 提供方必须拒绝缺失路径；paper 提供方在公开请求含优化指令时
	// 可接受缺失路径。返回稳定的 code 与人类可读 message。
	ValidateInput(artifactPath *string) *rsi.ArtifactValidationResult
	// Run 启动新任务并返回当前或终态结果。新建根状态/树快照；每个状态、节点、
	// 报告与 artifact 先持久化再发对应事件；先发 running，节点/进度事件随后，
	// 终态最后。NodeStageEvent 只更新既有节点不新建。task_id 与 run_dir 全程一致。
	Run(ctx context.Context, req *ArtifactEngineRequest, onEvent rsi.OnEvent) (*rsi.EngineResult, error)
	// Pause 在安全检查点暂停；先持久化检查点再发节点/进度事件，最后发 paused。
	// 不支持的场景返回相应结果。
	Pause(ctx context.Context, taskID string, onEvent rsi.OnEvent) (*rsi.EngineResult, error)
	// Resume 从检查点恢复同一任务，不新建根节点；请求必须保留原 task_id 与 run_dir。
	// 支持的实现先发 running 并延续既有树；paper 提供方返回 SCENARIO_NOT_SUPPORTED
	// 且不改变任务状态。
	Resume(ctx context.Context, req *ArtifactEngineRequest, onEvent rsi.OnEvent) (*rsi.EngineResult, error)
	// ReadState 读取最新持久化状态（服务重启/丢事件后的恢复查询路径），
	// 不发事件、不启动执行。
	ReadState(taskID string) *rsi.EngineState
	// ReadReport 读取当前/最终报告与完整 artifact 索引——须含任务全部可下载
	// artifact，不止当前采纳节点的。
	ReadReport(taskID string) *rsi.EngineReport
	// GetTree 读取完整持久化树（含被拒绝分支），返回根与全部分支而非仅最优链。
	// 每个节点须与 EventNode 同一 RsiTreeNode 形状。
	GetTree(taskID string) *rsi.TreeResponse
	// LocateArtifact 定位任务或节点 artifact 供 AgentServer 下载处理。
	// artifactID 为 nil 表示任务最终产物。返回提供方本地路径，不是前端 URL；
	// 归属校验与 URL 投影由 AgentServer 负责。查找必须限定 taskID，
	// 不得返回其他任务的 artifact。
	LocateArtifact(taskID string, artifactID *string) *rsi.ArtifactRef
	// Terminate 永久停止任务并返回 terminated 状态；先持久化最终检查点/报告
	// 再发终态事件。终止后的任务不可恢复。
	Terminate(ctx context.Context, taskID string, onEvent rsi.OnEvent) (*rsi.EngineResult, error)
}
