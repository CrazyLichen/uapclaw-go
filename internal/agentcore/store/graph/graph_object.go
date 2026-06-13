package graph

import "sort"

// ──────────────────────────── 结构体 ────────────────────────────

// EmbedTask 嵌入任务（对象 + 目标字段名 + 待嵌入文本）
type EmbedTask struct {
	// Object 指向图对象的指针
	Object any
	// FieldName 目标字段名（"content_embedding" 或 "name_embedding"）
	FieldName string
	// Text 待嵌入文本
	Text string
}

// BaseGraphObject 图对象基础结构
type BaseGraphObject struct {
	// UUID 唯一标识（32位十六进制）
	UUID string `json:"uuid"`
	// CreatedAt 创建时间戳（UTC秒级）
	CreatedAt int64 `json:"created_at"`
	// UserID 用户标识
	UserID string `json:"user_id"`
	// ObjType 对象类型
	ObjType string `json:"obj_type"`
	// Language 语言标识（cn/en）
	Language string `json:"language"`
	// Metadata 附加属性
	Metadata map[string]any `json:"metadata,omitempty"`
	// Content 文本内容
	Content string `json:"content"`
	// ContentEmbedding 内容向量
	ContentEmbedding []float64 `json:"content_embedding,omitempty"`
	// ContentBM25 内容BM25稀疏向量
	ContentBM25 map[uint32]float32 `json:"content_bm25,omitempty"`
}

// NamedGraphObject 具名图对象（嵌入 BaseGraphObject + Name 字段）
type NamedGraphObject struct {
	BaseGraphObject
	// Name 名称
	Name string `json:"name"`
	// NameEmbedding 名称向量
	NameEmbedding []float64 `json:"name_embedding,omitempty"`
}

// Entity 实体（知识图谱节点）
type Entity struct {
	NamedGraphObject
	// Relations 关联的关系UUID列表
	Relations []string `json:"relations"`
	// Episodes 关联的片段UUID列表
	Episodes []string `json:"episodes"`
	// Attributes 实体属性
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Relation 关系（知识图谱边）
type Relation struct {
	NamedGraphObject
	// ValidSince 有效起始时间戳
	ValidSince int64 `json:"valid_since"`
	// ValidUntil 有效终止时间戳
	ValidUntil int64 `json:"valid_until"`
	// OffsetSince 起始时区偏移（15分钟为单位）
	OffsetSince int8 `json:"offset_since"`
	// OffsetUntil 终止时区偏移（15分钟为单位）
	OffsetUntil int8 `json:"offset_until"`
	// LHS 左侧实体UUID
	LHS string `json:"lhs"`
	// RHS 右侧实体UUID
	RHS string `json:"rhs"`
}

// Episode 片段（对话片段）
type Episode struct {
	BaseGraphObject
	// ValidSince 有效起始时间戳
	ValidSince int64 `json:"valid_since"`
	// Entities 关联的实体UUID列表
	Entities []string `json:"entities"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseGraphObject 创建带默认值的 BaseGraphObject
func NewBaseGraphObject() *BaseGraphObject {
	return &BaseGraphObject{
		UUID:      GetUUID(),
		CreatedAt: GetCurrentUTCTimestamp(),
		UserID:    "default_user",
		Language:  "cn",
		Metadata:  make(map[string]any),
	}
}

// EmbedTasks 返回该对象需要执行的嵌入任务列表
func (b *BaseGraphObject) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: b, FieldName: "content_embedding", Text: b.Content},
	}
}

// ToMap 将图对象序列化为 Milvus 插入所需的 map[string]any
func (b *BaseGraphObject) ToMap() map[string]any {
	m := map[string]any{
		"uuid":       b.UUID,
		"created_at": b.CreatedAt,
		"user_id":    b.UserID,
		"obj_type":   b.ObjType,
		"language":   b.Language,
		"content":    b.Content,
	}
	if b.Metadata != nil {
		m["metadata"] = b.Metadata
	}
	if b.ContentEmbedding != nil {
		m["content_embedding"] = b.ContentEmbedding
	}
	if b.ContentBM25 != nil {
		m["content_bm25"] = b.ContentBM25
	}
	return m
}

// NewNamedGraphObject 创建带默认值的 NamedGraphObject
func NewNamedGraphObject() *NamedGraphObject {
	return &NamedGraphObject{
		BaseGraphObject: *NewBaseGraphObject(),
	}
}

// EmbedTasks 返回 content_embedding + name_embedding 两个任务
func (n *NamedGraphObject) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: n, FieldName: "content_embedding", Text: n.Content},
		{Object: n, FieldName: "name_embedding", Text: n.Name},
	}
}

// ToMap 序列化 NamedGraphObject
func (n *NamedGraphObject) ToMap() map[string]any {
	m := n.BaseGraphObject.ToMap()
	m["name"] = n.Name
	if n.NameEmbedding != nil {
		m["name_embedding"] = n.NameEmbedding
	}
	return m
}

// NewEntity 创建带默认值的 Entity
func NewEntity() *Entity {
	e := &Entity{
		NamedGraphObject: *NewNamedGraphObject(),
	}
	e.ObjType = "Entity"
	return e
}

// EmbedTasks Entity 覆写：返回 content + name 两个嵌入任务
func (e *Entity) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: e, FieldName: "content_embedding", Text: e.Content},
		{Object: e, FieldName: "name_embedding", Text: e.Name},
	}
}

// ToMap 序列化 Entity（Relations/Episodes 去重排序后写入）
func (e *Entity) ToMap() map[string]any {
	m := e.NamedGraphObject.ToMap()
	m["relations"] = uniqueSortedStrings(e.Relations)
	m["episodes"] = uniqueSortedStrings(e.Episodes)
	if e.Attributes != nil {
		m["attributes"] = e.Attributes
	}
	return m
}

// NewRelation 创建带默认值的 Relation
func NewRelation() *Relation {
	r := &Relation{
		NamedGraphObject: *NewNamedGraphObject(),
		ValidSince:       -1,
		ValidUntil:       -1,
	}
	r.ObjType = "Relation"
	return r
}

// EmbedTasks Relation 覆写：返回 content + name 两个嵌入任务
func (r *Relation) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: r, FieldName: "content_embedding", Text: r.Content},
		{Object: r, FieldName: "name_embedding", Text: r.Name},
	}
}

// ToMap 序列化 Relation
func (r *Relation) ToMap() map[string]any {
	m := r.NamedGraphObject.ToMap()
	m["lhs"] = r.LHS
	m["rhs"] = r.RHS
	m["valid_since"] = r.ValidSince
	m["valid_until"] = r.ValidUntil
	m["offset_since"] = r.OffsetSince
	m["offset_until"] = r.OffsetUntil
	return m
}

// UpdateConnectedEntities 将自身 UUID 添加到 lhs/rhs 实体的 Relations 中
func (r *Relation) UpdateConnectedEntities(lhs, rhs *Entity) {
	if lhs != nil {
		for _, rel := range lhs.Relations {
			if rel == r.UUID {
				goto checkRhs
			}
		}
		lhs.Relations = append(lhs.Relations, r.UUID)
	}
checkRhs:
	if rhs != nil {
		for _, rel := range rhs.Relations {
			if rel == r.UUID {
				return
			}
		}
		rhs.Relations = append(rhs.Relations, r.UUID)
	}
}

// NewEpisode 创建带默认值的 Episode
func NewEpisode() *Episode {
	p := &Episode{
		BaseGraphObject: *NewBaseGraphObject(),
		ValidSince:      -1,
	}
	p.ObjType = "Episode"
	return p
}

// EmbedTasks Episode 仅返回 content_embedding 任务
func (p *Episode) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: p, FieldName: "content_embedding", Text: p.Content},
	}
}

// ToMap 序列化 Episode（Entities 去重排序后写入）
func (p *Episode) ToMap() map[string]any {
	m := p.BaseGraphObject.ToMap()
	m["valid_since"] = p.ValidSince
	m["entities"] = uniqueSortedStrings(p.Entities)
	return m
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// uniqueSortedStrings 去重并排序字符串切片
func uniqueSortedStrings(ss []string) []string {
	if len(ss) == 0 {
		return ss
	}
	seen := make(map[string]struct{}, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result
}
