package milvus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/milvus-io/milvus/client/v2/column"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// graphWriter 图存储写入器，负责图对象的嵌入、截断、序列化和写入。
//
// 对应 Python: MilvusGraphStore._add_data / add_entity / add_relation / add_episode
type graphWriter struct {
	// client Milvus 客户端
	client milvusClient
	// storageCfg 存储配置（字段长度限制）
	storageCfg *graph.GraphStoreStorageConfig
	// embedder 嵌入模型（可选，为 nil 时跳过嵌入）
	embedder embedding.BaseEmbedding
	// embedDim 嵌入向量维度
	embedDim int
	// batchSize 批量写入大小
	batchSize int
	// sem 并发信号量
	sem chan struct{}
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// defaultGraphBatchSize 默认批量写入大小
	defaultGraphBatchSize = 100
	// defaultEmbedBatchSize 默认嵌入批大小
	defaultEmbedBatchSize = 10
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGraphWriter 创建图存储写入器
func newGraphWriter(client milvusClient, storageCfg *graph.GraphStoreStorageConfig, embedder embedding.BaseEmbedding, embedDim, batchSize, maxConcurrent int) *graphWriter {
	if batchSize <= 0 {
		batchSize = defaultGraphBatchSize
	}
	if maxConcurrent <= 0 {
		maxConcurrent = graph.DefaultWorkerNum
	}
	return &graphWriter{
		client:     client,
		storageCfg: storageCfg,
		embedder:   embedder,
		embedDim:   embedDim,
		batchSize:  batchSize,
		sem:        make(chan struct{}, maxConcurrent),
	}
}

// addEntity 添加实体到 Entity 集合。
// 自动嵌入向量（除非 opts.NoEmbed=true），截断超长字段，批量写入。
//
// 对应 Python: MilvusGraphStore.add_entity
func (w *graphWriter) addEntity(ctx context.Context, entities []*graph.Entity, opts ...graph.Option) error {
	return w.addData(ctx, CollectionEntity, toAnySlice(entities), opts...)
}

// addRelation 添加关系到 Relation 集合。
//
// 对应 Python: MilvusGraphStore.add_relation
func (w *graphWriter) addRelation(ctx context.Context, relations []*graph.Relation, opts ...graph.Option) error {
	return w.addData(ctx, CollectionRelation, toAnySlice(relations), opts...)
}

// addEpisode 添加片段到 Episode 集合。
//
// 对应 Python: MilvusGraphStore.add_episode
func (w *graphWriter) addEpisode(ctx context.Context, episodes []*graph.Episode, opts ...graph.Option) error {
	return w.addData(ctx, CollectionEpisode, toAnySlice(episodes), opts...)
}

// delete 按条件删除图数据。
// 对齐 Python: ids 和 expr 都为 None 时报错。
func (w *graphWriter) delete(ctx context.Context, collection string, opts ...graph.Option) error {
	o := applyGraphOptions(opts...)
	if len(o.IDs) == 0 && o.Expr == nil {
		return fmt.Errorf("删除必须提供 IDs 或过滤表达式")
	}

	var expr string
	if len(o.IDs) > 0 {
		ids := make([]string, 0, len(o.IDs))
		for _, id := range o.IDs {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
		expr = buildIDFilterExpr(ids)
	} else if o.Expr != nil {
		exprVal, exprErr := o.Expr.ToExpr("milvus")
		if exprErr != nil {
			return fmt.Errorf("构建删除表达式失败: %w", exprErr)
		}
		strExpr, ok := exprVal.(string)
		if !ok {
			return fmt.Errorf("milvus 后端应返回 string 类型的表达式")
		}
		expr = strExpr
	}

	if expr == "" {
		return nil
	}

	deleteOpt := milvusclient.NewDeleteOption(collection).WithExpr(expr)
	if _, err := w.client.Delete(ctx, deleteOpt); err != nil {
		logger.Error(logComponent).Err(err).Str("collection", collection).
			Str("expr", expr).Msg("删除数据失败")
		return fmt.Errorf("删除集合 %s 数据失败: %w", collection, err)
	}

	// 刷盘确保删除生效
	if o.Flush {
		if err := w.client.Flush(ctx, milvusclient.NewFlushOption(collection)); err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", collection).Msg("删除后 Flush 失败")
		}
	}

	logger.Info(logComponent).Str("collection", collection).
		Str("expr", expr).Msg("成功删除数据")
	return nil
}

// addData 通用写入流程：
// 1. 收集 EmbedTasks → 2. 批量嵌入 → 3. 截断字段 → 4. 序列化 → 5. 写入 → 6. Flush
//
// 对应 Python: MilvusGraphStore._add_data
func (w *graphWriter) addData(ctx context.Context, collection string, objects []any, opts ...graph.Option) error {
	start := time.Now()
	if len(objects) == 0 {
		return nil
	}

	o := applyGraphOptions(opts...)

	// 步骤1：收集 EmbedTasks
	var allTasks []graph.EmbedTask
	objMaps := make([]map[string]any, 0, len(objects))
	for _, obj := range objects {
		// 收集嵌入任务
		tasks := extractEmbedTasks(obj)
		allTasks = append(allTasks, tasks...)
		// 序列化为 map
		objMaps = append(objMaps, extractToMap(obj))
	}

	// 步骤2：批量嵌入（除非 NoEmbed=true 或无 embedder）
	if !o.NoEmbed && w.embedder != nil && len(allTasks) > 0 {
		if err := w.fetchAndEmbed(ctx, allTasks); err != nil {
			return fmt.Errorf("嵌入向量失败: %w", err)
		}
	}

	// 步骤3：截断超长字段
	for i, m := range objMaps {
		objMaps[i] = w.truncateFields(m, collection)
	}

	// 步骤4+5：批量写入
	batches := graph.Batched(objMaps, w.batchSize)
	for i, batch := range batches {
		if err := w.insertBatch(ctx, collection, batch, o.Upsert); err != nil {
			// 批量失败，尝试逐条回退
			logger.Warn(logComponent).Err(err).Str("collection", collection).
				Int("batch", i).Int("batch_size", len(batch)).Msg("批量写入失败，尝试逐条写入")
			for _, item := range batch {
				if err := w.insertBatch(ctx, collection, []map[string]any{item}, o.Upsert); err != nil {
					if o.SilenceErrors {
						logger.Warn(logComponent).Err(err).Str("collection", collection).Msg("逐条写入失败（静默）")
					} else {
						return fmt.Errorf("逐条写入失败: %w", err)
					}
				}
			}
		}
	}

	// 步骤6：Flush
	if o.Flush {
		if err := w.client.Flush(ctx, milvusclient.NewFlushOption(collection)); err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", collection).Msg("Flush 失败")
		}
	}

	logger.Info(logComponent).Str("collection", collection).
		Int("count", len(objects)).
		Dur("duration", time.Since(start)).
		Msg("成功写入图数据")
	return nil
}

// fetchAndEmbed 批量调用嵌入模型，回填向量到图对象。
//
// 对应 Python: MilvusGraphStore._fetch_and_embed
func (w *graphWriter) fetchAndEmbed(ctx context.Context, tasks []graph.EmbedTask) error {
	// 按 EmbedBatchSize 分批嵌入
	batches := graph.Batched(tasks, defaultEmbedBatchSize)
	for _, batch := range batches {
		texts := make([]string, 0, len(batch))
		for _, t := range batch {
			texts = append(texts, t.Text)
		}

		embeddings, err := w.embedder.EmbedDocuments(ctx, texts)
		if err != nil {
			return fmt.Errorf("嵌入文本失败: %w", err)
		}

		// 回填向量到图对象
		for i, t := range batch {
			if i < len(embeddings) {
				setEmbedding(t.Object, t.FieldName, embeddings[i])
			}
		}
	}
	return nil
}

// truncateFields 按 storageConfig 截断超长字段。
//
// 对应 Python: MilvusGraphStore._truncate_fields
func (w *graphWriter) truncateFields(objMap map[string]any, collection string) map[string]any {
	result := make(map[string]any, len(objMap))
	for k, v := range objMap {
		result[k] = v
	}

	// 截断 content
	if content, ok := result["content"].(string); ok && len(content) > w.storageCfg.Content {
		result["content"] = content[:w.storageCfg.Content]
	}
	// 截断 name
	if name, ok := result["name"].(string); ok && len(name) > w.storageCfg.Name {
		result["name"] = name[:w.storageCfg.Name]
	}
	// 截断 user_id
	if uid, ok := result["user_id"].(string); ok && len(uid) > w.storageCfg.UserID {
		result["user_id"] = uid[:w.storageCfg.UserID]
	}
	// 截断 language
	if lang, ok := result["language"].(string); ok && len(lang) > w.storageCfg.Language {
		result["language"] = lang[:w.storageCfg.Language]
	}
	// 截断 obj_type
	if ot, ok := result["obj_type"].(string); ok && len(ot) > w.storageCfg.ObjType {
		result["obj_type"] = ot[:w.storageCfg.ObjType]
	}

	// 截断数组字段
	if relations, ok := result["relations"].([]string); ok && len(relations) > w.storageCfg.Relations {
		result["relations"] = relations[:w.storageCfg.Relations]
	}
	if episodes, ok := result["episodes"].([]string); ok && len(episodes) > w.storageCfg.Episodes {
		result["episodes"] = episodes[:w.storageCfg.Episodes]
	}
	if entities, ok := result["entities"].([]string); ok && len(entities) > w.storageCfg.Entities {
		result["entities"] = entities[:w.storageCfg.Entities]
	}

	return result
}

// insertBatch 将一批 map 数据插入 Milvus。
func (w *graphWriter) insertBatch(ctx context.Context, collection string, batch []map[string]any, upsert bool) error {
	cols, err := mapsToColumns(batch)
	if err != nil {
		return fmt.Errorf("转换列数据失败: %w", err)
	}

	// NewColumnBasedInsertOption 返回的 *columnBasedDataOption 同时实现 InsertOption 和 UpsertOption
	opt := milvusclient.NewColumnBasedInsertOption(collection, cols...)
	if upsert {
		_, err = w.client.Upsert(ctx, opt)
	} else {
		_, err = w.client.Insert(ctx, opt)
	}
	return err
}

// extractEmbedTasks 从图对象中提取嵌入任务
func extractEmbedTasks(obj any) []graph.EmbedTask {
	switch v := obj.(type) {
	case *graph.Entity:
		return v.EmbedTasks()
	case *graph.Relation:
		return v.EmbedTasks()
	case *graph.Episode:
		return v.EmbedTasks()
	default:
		return nil
	}
}

// extractToMap 从图对象中提取序列化 map
func extractToMap(obj any) map[string]any {
	switch v := obj.(type) {
	case *graph.Entity:
		return v.ToMap()
	case *graph.Relation:
		return v.ToMap()
	case *graph.Episode:
		return v.ToMap()
	default:
		return nil
	}
}

// setEmbedding 将嵌入向量回填到图对象
func setEmbedding(obj any, fieldName string, emb []float64) {
	switch v := obj.(type) {
	case *graph.Entity:
		switch fieldName {
		case "content_embedding":
			v.ContentEmbedding = emb
		case "name_embedding":
			v.NameEmbedding = emb
		}
	case *graph.Relation:
		if fieldName == "content_embedding" {
			v.ContentEmbedding = emb
		}
	case *graph.Episode:
		if fieldName == "content_embedding" {
			v.ContentEmbedding = emb
		}
	}
}

// applyGraphOptions 应用图存储选项
func applyGraphOptions(opts ...graph.Option) graph.Options {
	var o graph.Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// toAnySlice 将类型化切片转为 []any
func toAnySlice[T any](items []T) []any {
	result := make([]any, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

// buildIDFilterExpr 构建 UUID 列表的过滤表达式
func buildIDFilterExpr(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return fmt.Sprintf("uuid in [%s]", strings.Join(quoted, ", "))
}

// mapsToColumns 将 []map[string]any 转换为 Milvus 列格式
func mapsToColumns(docs []map[string]any) ([]column.Column, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// 收集所有字段名
	fieldSet := make(map[string]bool)
	for _, doc := range docs {
		for k := range doc {
			fieldSet[k] = true
		}
	}

	// 为每个字段收集值
	fieldValues := make(map[string][]any)
	for name := range fieldSet {
		fieldValues[name] = make([]any, 0, len(docs))
	}
	for _, doc := range docs {
		for name := range fieldSet {
			val, ok := doc[name]
			if ok {
				fieldValues[name] = append(fieldValues[name], val)
			} else {
				fieldValues[name] = append(fieldValues[name], nil)
			}
		}
	}

	// 转换为 column.Column
	result := make([]column.Column, 0, len(fieldValues))
	for fieldName, values := range fieldValues {
		col := inferGraphColumn(fieldName, values)
		if col != nil {
			result = append(result, col)
		}
	}
	return result, nil
}

// inferGraphColumn 从值推断列类型并创建 column.Column（图存储版本）
func inferGraphColumn(fieldName string, values []any) column.Column {
	if len(values) == 0 {
		return nil
	}

	for _, v := range values {
		if v == nil {
			continue
		}
		switch v.(type) {
		case []float64:
			vecs := make([][]float32, 0, len(values))
			dim := 0
			for _, val := range values {
				if f64, ok := val.([]float64); ok {
					dim = len(f64)
					f32 := make([]float32, len(f64))
					for i, f := range f64 {
						f32[i] = float32(f)
					}
					vecs = append(vecs, f32)
				} else if val == nil && dim > 0 {
					vecs = append(vecs, make([]float32, dim))
				}
			}
			if dim > 0 && len(vecs) > 0 {
				return column.NewColumnFloatVector(fieldName, dim, vecs)
			}
		case string:
			strs := make([]string, 0, len(values))
			for _, val := range values {
				if s, ok := val.(string); ok {
					strs = append(strs, s)
				} else {
					strs = append(strs, "")
				}
			}
			return column.NewColumnVarChar(fieldName, strs)
		case int64:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int64); ok {
					ints = append(ints, i)
				} else {
					ints = append(ints, 0)
				}
			}
			return column.NewColumnInt64(fieldName, ints)
		case int:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int); ok {
					ints = append(ints, int64(i))
				} else {
					ints = append(ints, 0)
				}
			}
			return column.NewColumnInt64(fieldName, ints)
		case int32:
			ints := make([]int32, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int32); ok {
					ints = append(ints, i)
				} else {
					ints = append(ints, 0)
				}
			}
			return column.NewColumnInt32(fieldName, ints)
		case []string:
			// Array 类型（如 relations/episodes/entities）
			arr := make([][]string, 0, len(values))
			for _, val := range values {
				if s, ok := val.([]string); ok {
					arr = append(arr, s)
				} else {
					arr = append(arr, []string{})
				}
			}
			return column.NewColumnVarCharArray(fieldName, arr)
		case map[string]any:
			// JSON 类型（如 metadata/attributes），对齐 Python: DataType.JSON
			jsonBytes := make([][]byte, 0, len(values))
			for _, val := range values {
				if m, ok := val.(map[string]any); ok {
					b, err := json.Marshal(m)
					if err != nil {
						jsonBytes = append(jsonBytes, []byte("{}"))
					} else {
						jsonBytes = append(jsonBytes, b)
					}
				} else {
					jsonBytes = append(jsonBytes, []byte("{}"))
				}
			}
			return column.NewColumnJSONBytes(fieldName, jsonBytes)
		}
	}
	return nil
}
