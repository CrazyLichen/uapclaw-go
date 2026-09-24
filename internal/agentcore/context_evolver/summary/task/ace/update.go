package ace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadPlaybookOp 从 vector store 加载已有 playbook。
// 对齐 Python LoadPlaybookOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type LoadPlaybookOp struct {
	op.OpBase
}

// ReflectOp 单轨迹反思操作（matts="none"/"sequential"）。
// 对齐 Python ReflectOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type ReflectOp struct {
	op.OpBase
	// useGroundTruth 是否使用 ground truth
	useGroundTruth bool
}

// ParallelReflectOp 多轨迹反思操作（matts="parallel"/"combined"）。
// 对齐 Python ParallelReflectOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type ParallelReflectOp struct {
	op.OpBase
	// useGroundTruth 是否使用 ground truth
	useGroundTruth bool
}

// CurateOp 单轨迹策展操作（matts="none"/"sequential"）。
// 对齐 Python CurateOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type CurateOp struct {
	op.OpBase
}

// ParallelCurateOp 多轨迹策展操作（matts="parallel"/"combined"）。
// 对齐 Python ParallelCurateOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type ParallelCurateOp struct {
	op.OpBase
}

// ApplyDeltaOp 应用 playbook 变更并持久化到 vector store。
// 对齐 Python ApplyDeltaOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type ApplyDeltaOp struct {
	op.OpBase
	// maxBullets playbook 最大 bullet 数量，默认 50
	maxBullets int
}

// PersistMemoryOp 持久化 ACE 记忆到 JSON 或 Milvus。
// 对齐 Python PersistMemoryOp。
//
// Python: openjiuwen/extensions/context_evolver/summary/task/ace/update.py
type PersistMemoryOp struct {
	op.OpBase
	// helper 持久化助手
	helper *cepersistence.MemoryPersistenceHelper
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// aceAlgoName ACE 算法名称，对齐 Python PersistMemoryOp._ALGO_NAME = "ace"
	aceAlgoName = "ace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoadPlaybookOp 创建 playbook 加载操作。
func NewLoadPlaybookOp(sc *cecontext.ServiceContext) *LoadPlaybookOp {
	return &LoadPlaybookOp{OpBase: *op.NewOpBase(sc)}
}

// NewReflectOp 创建单轨迹反思操作。
// 对齐 Python ReflectOp(use_ground_truth)。
func NewReflectOp(sc *cecontext.ServiceContext, useGroundTruth bool) *ReflectOp {
	return &ReflectOp{
		OpBase:         *op.NewOpBase(sc),
		useGroundTruth: useGroundTruth,
	}
}

// NewParallelReflectOp 创建多轨迹反思操作。
// 对齐 Python ParallelReflectOp(use_ground_truth)。
func NewParallelReflectOp(sc *cecontext.ServiceContext, useGroundTruth bool) *ParallelReflectOp {
	return &ParallelReflectOp{
		OpBase:         *op.NewOpBase(sc),
		useGroundTruth: useGroundTruth,
	}
}

// NewCurateOp 创建单轨迹策展操作。
func NewCurateOp(sc *cecontext.ServiceContext) *CurateOp {
	return &CurateOp{OpBase: *op.NewOpBase(sc)}
}

// NewParallelCurateOp 创建多轨迹策展操作。
func NewParallelCurateOp(sc *cecontext.ServiceContext) *ParallelCurateOp {
	return &ParallelCurateOp{OpBase: *op.NewOpBase(sc)}
}

// NewApplyDeltaOp 应用 playbook 变更操作。
// 对齐 Python ApplyDeltaOp(max_bullets=50)。
func NewApplyDeltaOp(sc *cecontext.ServiceContext, maxBullets int) *ApplyDeltaOp {
	if maxBullets <= 0 {
		maxBullets = 50
	}
	return &ApplyDeltaOp{
		OpBase:     *op.NewOpBase(sc),
		maxBullets: maxBullets,
	}
}

// NewPersistMemoryOp 创建 ACE 记忆持久化操作。
// 对齐 Python PersistMemoryOp(persist_type, persist_path, milvus_host, milvus_port, milvus_collection)。
func NewPersistMemoryOp(sc *cecontext.ServiceContext, helper *cepersistence.MemoryPersistenceHelper) *PersistMemoryOp {
	return &PersistMemoryOp{
		OpBase: *op.NewOpBase(sc),
		helper: helper,
	}
}

// Execute 执行 playbook 加载。
// 对齐 Python LoadPlaybookOp.async_execute(context)。
func (o *LoadPlaybookOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("vector store not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	playbook := NewPlaybook()

	// 对齐 Python: 用 dummy embedding + metadata_filter 搜索 ACE memories
	func() {
		defer func() {
			// 对齐 Python: 加载失败时回退到空 Playbook
			if r := recover(); r != nil {
				logger.Warn(logComponent).
					Any("recover", r).
					Msg("Failed to load playbook. Starting with empty playbook.")
				playbook = NewPlaybook()
			}
		}()

		dummyEmbedding := make([]float64, 2560)
		metadataFilter := map[string]any{
			"workspace_id": userID,
			"type":         "ace_memory",
		}

		nodes, err := vectorStore.Search(ctx, dummyEmbedding, 50, metadataFilter)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("user_id", userID).
				Msg("Failed to load playbook. Starting with empty playbook.")
			return
		}

		// 对齐 Python: 将 VectorNode metadata 转为 Bullet，调用 playbook.load_bullet(bullet)
		for _, node := range nodes {
			metadata := node.Metadata
			bullet := &Bullet{
				ID:        getString(metadata, "id"),
				Section:   getString(metadata, "section"),
				Content:   getString(metadata, "content"),
				Helpful:   getInt(metadata, "helpful"),
				Harmful:   getInt(metadata, "harmful"),
				Neutral:   getInt(metadata, "neutral"),
				CreatedAt: parseTime(metadata["created_at"]),
				UpdatedAt: parseTime(metadata["updated_at"]),
			}
			playbook.LoadBullet(bullet)
		}

		// 对齐 Python: 从所有 bullet ID 中提取最大数字，设置 playbook.set_next_id(max_id)
		// Python: bullet_id.rsplit('-', 1) — 从右侧分割
		maxID := 0
		for _, bulletID := range playbook.BulletIDs() {
			if idx := strings.LastIndex(bulletID, "-"); idx >= 0 {
				var idNum int
				if _, err := fmt.Sscanf(bulletID[idx+1:], "%d", &idNum); err == nil {
					if idNum > maxID {
						maxID = idNum
					}
				}
			}
		}
		playbook.SetNextID(maxID)
	}()

	rc.Set("playbook", playbook)

	logger.Info(logComponent).
		Str("user_id", userID).
		Int("bullet_count", len(playbook.Bullets())).
		Msg("Loaded playbook")

	return nil
}

// Execute 执行单轨迹反思。
// 对齐 Python ReflectOp.async_execute(context)。
func (o *ReflectOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 对齐 Python: 检查 matts 值，非 none/sequential 时跳过
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "none" && matts != "sequential" {
		logger.Info(logComponent).Str("matts", matts).Msg("Skipping ReflectOp for matts mode")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	// 对齐 Python: query = context.query（Python 也获取了 query 但未在 reflector prompt 中使用，
	// 保留此行对齐 Python，后续如果 reflector prompt 需要可立即启用）
	_, _ = cecontext.GetTyped[string](rc, "query")
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")
	playbook, _ := rc.Get("playbook").(*Playbook)
	if playbook == nil {
		playbook = NewPlaybook()
	}
	groundTruth, _ := cecontext.GetTyped[string](rc, "ground_truth")
	feedback, _ := cecontext.GetTyped[[]string](rc, "feedback")

	if len(trajectories) == 0 {
		logger.Warn(logComponent).Msg("No trajectories to reflect on")
		rc.Set("reflection", map[string]any{})
		return nil
	}

	// 对齐 Python: 格式化 trajectory = trajectories[0]
	trajectoryStr := trajectories[0]

	// 对齐 Python: 根据 use_ground_truth 选择 prompt
	var prompt *template.Template
	var data reflectorPromptData
	if o.useGroundTruth && groundTruth != "" && len(feedback) > 0 {
		prompt = ACEPrompts.ACEReflectorPrompt
		data = reflectorPromptData{
			GroundTruth: groundTruth,
			Feedback:    feedback[0],
			Playbook:    playbook.AsPrompt(),
			Trajectory:  trajectoryStr,
		}
	} else {
		prompt = ACEPrompts.ACEReflectorNoGTPrompt
		data = reflectorPromptData{
			Playbook:   playbook.AsPrompt(),
			Trajectory: trajectoryStr,
		}
	}

	logger.Debug(logComponent).Msg("Generating reflection from trajectory...")

	// 渲染提示词
	var buf bytes.Buffer
	if err := prompt.Execute(&buf, data); err != nil {
		return fmt.Errorf("ReflectOp: 渲染提示词失败: %w", err)
	}

	// 调用 LLM
	response, err := llm.Generate(ctx, buf.String())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to generate reflection")
		return fmt.Errorf("ReflectOp: LLM 调用失败: %w", err)
	}

	// 对齐 Python: 用 SafeJSONLoads 解析 JSON 响应
	reflection, err := SafeJSONLoads(response)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to parse reflection")
		rc.Set("reflection", map[string]any{})
		return nil
	}
	rc.Set("reflection", reflection)

	logger.Info(logComponent).Msg("Generated reflection successfully")
	return nil
}

// Execute 执行多轨迹反思。
// 对齐 Python ParallelReflectOp.async_execute(context)。
func (o *ParallelReflectOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 对齐 Python: 检查 matts 值，非 parallel/combined 时跳过
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "parallel" && matts != "combined" {
		logger.Info(logComponent).Str("matts", matts).Msg("Skipping ParallelReflectOp for matts mode")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")
	playbook, _ := rc.Get("playbook").(*Playbook)
	if playbook == nil {
		playbook = NewPlaybook()
	}
	groundTruth, _ := cecontext.GetTyped[string](rc, "ground_truth")
	feedback, _ := cecontext.GetTyped[[]string](rc, "feedback")

	if len(trajectories) < 2 {
		logger.Warn(logComponent).
			Int("trajectory_count", len(trajectories)).
			Msg("Expected at least 2 trajectories for parallel mode")
		rc.Set("reflection", map[string]any{})
		return nil
	}

	// 对齐 Python: 动态格式化多轨迹
	trajectoryParts := make([]string, 0, len(trajectories)*3)
	for i, traj := range trajectories {
		trajectoryParts = append(trajectoryParts, fmt.Sprintf("<TRAJECTORY %d>", i+1))
		// 对齐 Python: 如果有对应的 feedback，附上 TEST_REPORT
		if i < len(feedback) && feedback[i] != "" {
			trajectoryParts = append(trajectoryParts, "TEST_REPORT_START")
			trajectoryParts = append(trajectoryParts, feedback[i])
			trajectoryParts = append(trajectoryParts, "TEST_REPORT_END")
		}
		trajectoryParts = append(trajectoryParts, traj)
		trajectoryParts = append(trajectoryParts, "") // 空行分隔
	}
	trajectoriesStr := strings.Join(trajectoryParts, "\n")

	// 对齐 Python: 根据 use_ground_truth 选择 prompt
	var prompt *template.Template
	var data reflectorScalingPromptData
	if o.useGroundTruth && groundTruth != "" {
		prompt = ACEPrompts.ACEReflectorScalingPrompt
		data = reflectorScalingPromptData{
			GroundTruth:  groundTruth,
			Playbook:     playbook.AsPrompt(),
			Trajectories: trajectoriesStr,
		}
	} else {
		prompt = ACEPrompts.ACEReflectorScalingNoGTPrompt
		data = reflectorScalingPromptData{
			Playbook:     playbook.AsPrompt(),
			Trajectories: trajectoriesStr,
		}
	}

	logger.Debug(logComponent).
		Int("trajectory_count", len(trajectories)).
		Msg("Generating parallel reflection from trajectories...")

	var buf bytes.Buffer
	if err := prompt.Execute(&buf, data); err != nil {
		return fmt.Errorf("ParallelReflectOp: 渲染提示词失败: %w", err)
	}

	response, err := llm.Generate(ctx, buf.String())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to generate parallel reflection")
		return fmt.Errorf("ParallelReflectOp: LLM 调用失败: %w", err)
	}

	reflection, err := SafeJSONLoads(response)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to parse parallel reflection")
		rc.Set("reflection", map[string]any{})
		return nil
	}
	rc.Set("reflection", reflection)

	logger.Info(logComponent).Msg("Generated parallel reflection successfully")
	return nil
}

// Execute 执行单轨迹策展。
// 对齐 Python CurateOp.async_execute(context)。
func (o *CurateOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 对齐 Python: 检查 matts 值，非 none/sequential 时跳过
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "none" && matts != "sequential" {
		logger.Info(logComponent).Str("matts", matts).Msg("Skipping CurateOp for matts mode")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	reflection, _ := rc.Get("reflection").(map[string]any)
	playbook, _ := rc.Get("playbook").(*Playbook)
	if playbook == nil {
		playbook = NewPlaybook()
	}
	query, _ := cecontext.GetTyped[string](rc, "query")
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")

	if len(reflection) == 0 {
		logger.Warn(logComponent).Msg("No reflection to curate from")
		rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: []DeltaOperation{}})
		return nil
	}

	// 对齐 Python: 格式化 trajectory = trajectories[0]
	trajectoryStr := ""
	if len(trajectories) > 0 {
		trajectoryStr = trajectories[0]
	}

	// 对齐 Python: reflection JSON 序列化
	reflectionJSON, err := json.Marshal(reflection)
	if err != nil {
		return fmt.Errorf("CurateOp: reflection 序列化失败: %w", err)
	}

	data := curatorPromptData{
		QuestionContext: query,
		Playbook:        playbook.AsPrompt(),
		Trajectory:      trajectoryStr,
		Reflection:      string(reflectionJSON),
	}

	logger.Debug(logComponent).Msg("Generating playbook operations from reflection...")

	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorPrompt.Execute(&buf, data); err != nil {
		return fmt.Errorf("CurateOp: 渲染提示词失败: %w", err)
	}

	response, err := llm.Generate(ctx, buf.String())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to generate curation")
		return fmt.Errorf("CurateOp: LLM 调用失败: %w", err)
	}

	// 对齐 Python: SafeJSONLoads + DeltaBatch.from_json
	curationDict, err := SafeJSONLoads(response)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to parse curation")
		rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: []DeltaOperation{}})
		return nil
	}
	delta := NewDeltaBatchFromJSON(curationDict)
	rc.Set("delta", delta)

	logger.Info(logComponent).
		Int("operation_count", len(delta.Operations)).
		Msg("Generated playbook operations")

	return nil
}

// Execute 执行多轨迹策展。
// 对齐 Python ParallelCurateOp.async_execute(context)。
func (o *ParallelCurateOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 对齐 Python: 检查 matts 值，非 parallel/combined 时跳过
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "parallel" && matts != "combined" {
		logger.Info(logComponent).Str("matts", matts).Msg("Skipping ParallelCurateOp for matts mode")
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	reflection, _ := rc.Get("reflection").(map[string]any)
	playbook, _ := rc.Get("playbook").(*Playbook)
	if playbook == nil {
		playbook = NewPlaybook()
	}
	query, _ := cecontext.GetTyped[string](rc, "query")
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")

	if len(reflection) == 0 {
		logger.Warn(logComponent).Msg("No reflection to curate from")
		rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: []DeltaOperation{}})
		return nil
	}

	if len(trajectories) < 2 {
		logger.Warn(logComponent).
			Int("trajectory_count", len(trajectories)).
			Msg("Expected at least 2 trajectories for parallel mode")
		rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: []DeltaOperation{}})
		return nil
	}

	// 对齐 Python: 动态格式化多轨迹
	trajectoryParts := make([]string, 0, len(trajectories)*3)
	for i, traj := range trajectories {
		trajectoryParts = append(trajectoryParts, fmt.Sprintf("<TRAJECTORY %d>", i+1))
		trajectoryParts = append(trajectoryParts, traj)
		trajectoryParts = append(trajectoryParts, "")
	}
	trajectoriesStr := strings.Join(trajectoryParts, "\n")

	reflectionJSON, err := json.Marshal(reflection)
	if err != nil {
		return fmt.Errorf("ParallelCurateOp: reflection 序列化失败: %w", err)
	}

	data := curatorScalingPromptData{
		QuestionContext: query,
		Playbook:        playbook.AsPrompt(),
		Trajectories:    trajectoriesStr,
		Reflection:      string(reflectionJSON),
	}

	logger.Debug(logComponent).Msg("Generating playbook operations from parallel reflection...")

	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorScalingPrompt.Execute(&buf, data); err != nil {
		return fmt.Errorf("ParallelCurateOp: 渲染提示词失败: %w", err)
	}

	response, err := llm.Generate(ctx, buf.String())
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to generate parallel curation")
		return fmt.Errorf("ParallelCurateOp: LLM 调用失败: %w", err)
	}

	curationDict, err := SafeJSONLoads(response)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to parse parallel curation")
		rc.Set("delta", &DeltaBatch{Reasoning: "", Operations: []DeltaOperation{}})
		return nil
	}
	delta := NewDeltaBatchFromJSON(curationDict)
	rc.Set("delta", delta)

	logger.Info(logComponent).
		Int("operation_count", len(delta.Operations)).
		Msg("Generated playbook operations (parallel)")

	return nil
}

// Execute 执行 playbook 变更应用。
// 对齐 Python ApplyDeltaOp.async_execute(context)。
func (o *ApplyDeltaOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	delta, _ := rc.Get("delta").(*DeltaBatch)
	playbook, _ := rc.Get("playbook").(*Playbook)
	if playbook == nil {
		playbook = NewPlaybook()
	}
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 对齐 Python: delta 为空或无 operations 时直接返回
	if delta == nil || len(delta.Operations) == 0 {
		logger.Info(logComponent).Msg("No delta operations to apply")
		rc.Set("memories", []ceschema.ACEMemory{})
		return nil
	}

	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("vector store not configured in ServiceContext")
	}
	embeddingModel := o.EmbeddingModel()
	if embeddingModel == nil {
		return fmt.Errorf("embedding model not configured in ServiceContext")
	}

	// 对齐 Python: 计算 add_count + current_count - max_bullets
	addCount := 0
	for _, operation := range delta.Operations {
		if operation.Type == OperationAdd {
			addCount++
		}
	}
	currentCount := len(playbook.Bullets())
	removeCount := addCount + currentCount - o.maxBullets

	// 对齐 Python: 按 score=helpful-harmful 升序排列，删除最低分
	affectedBulletIDs := make(map[string]bool)
	removedBulletIDs := make(map[string]bool)

	if removeCount > 0 {
		bulletsList := playbook.Bullets()
		// 对齐 Python: sorted(bullets_list, key=lambda b: (b.helpful - b.harmful, b.updated_at), reverse=False)
		sort.Slice(bulletsList, func(i, j int) bool {
			scoreI := bulletsList[i].Helpful - bulletsList[i].Harmful
			scoreJ := bulletsList[j].Helpful - bulletsList[j].Harmful
			if scoreI != scoreJ {
				return scoreI < scoreJ
			}
			return bulletsList[i].UpdatedAt.Before(bulletsList[j].UpdatedAt)
		})
		for i := 0; i < removeCount && i < len(bulletsList); i++ {
			removedBulletIDs[bulletsList[i].ID] = true
			playbook.RemoveBullet(bulletsList[i].ID)
			logger.Info(logComponent).
				Str("bullet_id", bulletsList[i].ID).
				Msg("Removed low-scoring bullet")
		}
	}

	// 对齐 Python: 逐条应用 DeltaOperation
	for i := range delta.Operations {
		operation := &delta.Operations[i]
		switch operation.Type {
		case OperationAdd:
			bullet := playbook.AddBullet(
				operation.Section,
				derefString(operation.Content, ""),
				operation.BulletID,
				operation.Metadata,
			)
			if bullet != nil {
				affectedBulletIDs[bullet.ID] = true
			}

		case OperationUpdate:
			if operation.BulletID != nil {
				updatedBullet := playbook.UpdateBullet(*operation.BulletID, operation.Content, operation.Metadata)
				if updatedBullet != nil {
					affectedBulletIDs[*operation.BulletID] = true
				} else if operation.Content != nil {
					// 对齐 Python: UPDATE 找不到 bullet 且有 content 时降级为 ADD
					logger.Info(logComponent).
						Str("bullet_id", *operation.BulletID).
						Msg("UPDATE operation converted to ADD: bullet not found, creating new bullet")
					section := operation.Section
					if section == "" && operation.BulletID != nil {
						// 对齐 Python: 从 bullet_id 提取 section（rsplit('-', 1) 从右侧分割）
						if idx := strings.LastIndex(*operation.BulletID, "-"); idx >= 0 {
							section = strings.ReplaceAll((*operation.BulletID)[:idx], "_", " ")
						}
					}
					if section == "" {
						section = "general"
					}
					bullet := playbook.AddBullet(section, *operation.Content, nil, operation.Metadata)
					if bullet != nil {
						affectedBulletIDs[bullet.ID] = true
					}
				} else {
					logger.Warn(logComponent).
						Str("bullet_id", *operation.BulletID).
						Msg("UPDATE operation failed: bullet not found and no content provided")
				}
			}

		case OperationTag:
			if operation.BulletID != nil {
				if playbook.GetBullet(*operation.BulletID) != nil {
					affectedBulletIDs[*operation.BulletID] = true
					for tag, increment := range operation.Metadata {
						playbook.TagBullet(*operation.BulletID, tag, increment)
					}
				} else {
					logger.Warn(logComponent).
						Str("bullet_id", *operation.BulletID).
						Msg("TAG operation failed: bullet not found")
				}
			}

		case OperationRemove:
			if operation.BulletID != nil {
				removedBulletIDs[*operation.BulletID] = true
				playbook.RemoveBullet(*operation.BulletID)
			}
		}
	}

	// 对齐 Python: 删除 removed bullets 从 vector store
	for bulletID := range removedBulletIDs {
		nodeID := fmt.Sprintf("ace_%s_%s", userID, bulletID)
		_, err := vectorStore.Delete(ctx, nodeID)
		if err != nil {
			logger.Warn(logComponent).
				Str("bullet_id", bulletID).
				Err(err).
				Msg("Failed to delete bullet from vector store")
		} else {
			logger.Debug(logComponent).
				Str("bullet_id", bulletID).
				Msg("Deleted bullet from vector store")
		}
	}

	// 对齐 Python: 更新 affected bullets 到 vector store
	memories := make([]ceschema.ACEMemory, 0, len(affectedBulletIDs))
	for bulletID := range affectedBulletIDs {
		bullet := playbook.GetBullet(bulletID)
		if bullet == nil {
			continue
		}
		// 对齐 Python: 创建 ACEMemory → to_vector_node → embed content → upsert
		aceMemory := ceschema.ACEMemory{
			BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
			ID:         bullet.ID,
			Section:    bullet.Section,
			Content:    bullet.Content,
			Helpful:    bullet.Helpful,
			Harmful:    bullet.Harmful,
			Neutral:    bullet.Neutral,
			CreatedAt:  bullet.CreatedAt,
			UpdatedAt:  bullet.UpdatedAt,
		}
		vectorNode := aceMemory.ToVectorNode()
		embedding, err := embeddingModel.Embed(ctx, aceMemory.Content)
		if err != nil {
			logger.Warn(logComponent).
				Str("bullet_id", bullet.ID).
				Err(err).
				Msg("Failed to embed bullet content")
			continue
		}
		vectorNode.Embedding = embedding
		if err := vectorStore.Upsert(ctx, vectorNode); err != nil {
			logger.Warn(logComponent).
				Str("node_id", vectorNode.ID).
				Err(err).
				Msg("Failed to upsert vector node")
			continue
		}
		memories = append(memories, aceMemory)
	}

	rc.Set("memories", memories)

	logger.Info(logComponent).
		Int("operation_count", len(delta.Operations)).
		Int("updated_count", len(affectedBulletIDs)).
		Int("removed_count", len(removedBulletIDs)).
		Msg("Applied delta operations")

	return nil
}

// Execute 执行 ACE 记忆持久化。
// 对齐 Python PersistMemoryOp.async_execute(context)。
func (o *PersistMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("vector store not configured in ServiceContext")
	}

	// 对齐 Python: 获取所有 ACE 记忆节点
	metadataFilter := map[string]any{
		"workspace_id": userID,
		"type":         "ace_memory",
	}
	allNodes := vectorStore.GetAll(metadataFilter)

	if len(allNodes) == 0 {
		logger.Info(logComponent).
			Str("user_id", userID).
			Msg("PersistMemoryOp (ACE): no memories to persist")
		rc.Set("persist_count", 0)
		return nil
	}

	// 对齐 Python: nodes_dict = {node.id: node.to_dict() for node in all_nodes}
	nodesDict := make(map[string]any, len(allNodes))
	for _, node := range allNodes {
		nodesDict[node.ID] = node.ToDict()
	}

	// 对齐 Python: self._helper.save(user_id, self._ALGO_NAME, nodes_dict)
	if o.helper != nil {
		if err := o.helper.Save(userID, aceAlgoName, nodesDict); err != nil {
			return fmt.Errorf("PersistMemoryOp (ACE): 持久化失败: %w", err)
		}
	}

	rc.Set("persist_count", len(nodesDict))

	helperType := "none"
	if o.helper != nil {
		helperType = o.helper.PersistType()
	}

	logger.Info(logComponent).
		Int("count", len(nodesDict)).
		Str("user_id", userID).
		Str("helper_type", helperType).
		Msg("PersistMemoryOp (ACE): persisted memories")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 辅助函数 derefString/getString/getInt/parseTime 已在 playbook.go 中定义，
// 同包可直接使用，此处不再重复定义。
