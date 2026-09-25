package ace

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Bullet 单条 playbook 条目。
// 对齐 Python Bullet dataclass。
type Bullet struct {
	// ID 唯一标识，格式 {section_prefix}-{五位数}
	ID string `json:"id"`
	// Section 所属分类
	Section string `json:"section"`
	// Content 条目内容
	Content string `json:"content"`
	// Helpful 有帮助计数
	Helpful int `json:"helpful"`
	// Harmful 有害计数
	Harmful int `json:"harmful"`
	// Neutral 中性计数
	Neutral int `json:"neutral"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// BulletTag bullet 标签。
// 对齐 Python BulletTag dataclass。
type BulletTag struct {
	// ID bullet 标识
	ID string `json:"id"`
	// Tag 标签名
	Tag string `json:"tag"`
}

// DeltaOperation 单条 playbook 变更操作。
// 对齐 Python DeltaOperation dataclass。
type DeltaOperation struct {
	// Type 操作类型
	Type OperationType `json:"type"`
	// Section 目标 section
	Section string `json:"section"`
	// Content 新内容（ADD/UPDATE 时使用）
	Content *string `json:"content,omitempty"`
	// BulletID 目标 bullet 标识（UPDATE/TAG/REMOVE 时必须）
	BulletID *string `json:"bullet_id,omitempty"`
	// Metadata 元数据变更（TAG/UPDATE 时使用）
	Metadata map[string]int `json:"metadata,omitempty"`
}

// DeltaBatch 一组 curator 推理 + 操作。
// 对齐 Python DeltaBatch dataclass。
type DeltaBatch struct {
	// Reasoning 策展推理过程
	Reasoning string `json:"reasoning"`
	// Operations 待应用的操作列表
	Operations []DeltaOperation `json:"operations"`
}

// Playbook ACE 结构化上下文存储。
// 对齐 Python Playbook class。
//
// 内部维护 bullets map + sections 索引 + nextID 计数器。
// nextID 用于生成唯一 bullet ID，格式 {section_prefix}-{五位数}。
type Playbook struct {
	// bullets bullet 存储，key = bullet ID
	bullets map[string]*Bullet
	// sections section 索引，key = section name，value = bullet ID 列表（有序）
	sections map[string][]string
	// nextID 下一个 bullet ID 的数字部分
	nextID int
}

// ──────────────────────────── 枚举 ────────────────────────────

// OperationType playbook 变更操作类型。
// 对齐 Python OperationType = Literal["ADD", "UPDATE", "TAG", "REMOVE"]。
type OperationType int

const (
	// OperationAdd 添加新 bullet
	OperationAdd OperationType = iota
	// OperationUpdate 更新已有 bullet 的内容和/或元数据
	OperationUpdate
	// OperationTag 对已有 bullet 的元数据进行增量标记
	OperationTag
	// OperationRemove 删除已有 bullet
	OperationRemove
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlaybook 构造空 playbook。
// 对齐 Python Playbook.__init__。
func NewPlaybook() *Playbook {
	return &Playbook{
		bullets:  make(map[string]*Bullet),
		sections: make(map[string][]string),
		nextID:   0,
	}
}

// AddBullet 添加 bullet。
// 对齐 Python Playbook.add_bullet(section, content, bullet_id=None, metadata=None)。
func (p *Playbook) AddBullet(section, content string, bulletID *string, metadata map[string]int) *Bullet {
	id := ""
	if bulletID != nil && *bulletID != "" {
		id = *bulletID
	} else {
		id = p.generateID(section)
	}
	if metadata == nil {
		metadata = make(map[string]int)
	}
	bullet := &Bullet{
		ID:        id,
		Section:   section,
		Content:   content,
		Helpful:   0,
		Harmful:   0,
		Neutral:   0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	bullet.ApplyMetadata(metadata)
	p.bullets[id] = bullet
	p.sections[section] = append(p.sections[section], id)
	return bullet
}

// UpdateBullet 更新 bullet，返回 nil 表示未找到。
// 对齐 Python Playbook.update_bullet(bullet_id, content=None, metadata=None)。
func (p *Playbook) UpdateBullet(bulletID string, content *string, metadata map[string]int) *Bullet {
	bullet, ok := p.bullets[bulletID]
	if !ok {
		return nil
	}
	if content != nil {
		bullet.Content = *content
	}
	if metadata != nil {
		bullet.ApplyMetadata(metadata)
	}
	bullet.UpdatedAt = time.Now().UTC()
	return bullet
}

// TagBullet 标记 bullet。
// 对齐 Python Playbook.tag_bullet(bullet_id, tag, increment=1)。
func (p *Playbook) TagBullet(bulletID, tag string, increment int) *Bullet {
	bullet, ok := p.bullets[bulletID]
	if !ok {
		return nil
	}
	if err := bullet.Tag(tag, increment); err != nil {
		return nil
	}
	return bullet
}

// RemoveBullet 删除 bullet。
// 对齐 Python Playbook.remove_bullet(bullet_id)。
func (p *Playbook) RemoveBullet(bulletID string) {
	bullet, ok := p.bullets[bulletID]
	if !ok {
		return
	}
	delete(p.bullets, bulletID)
	sectionList, ok := p.sections[bullet.Section]
	if !ok {
		return
	}
	filtered := make([]string, 0, len(sectionList))
	for _, id := range sectionList {
		if id != bulletID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(p.sections, bullet.Section)
	} else {
		p.sections[bullet.Section] = filtered
	}
}

// GetBullet 获取 bullet。
// 对齐 Python Playbook.get_bullet(bullet_id)。
func (p *Playbook) GetBullet(bulletID string) *Bullet {
	return p.bullets[bulletID]
}

// Bullets 返回所有 bullet。
// 对齐 Python Playbook.bullets()。
func (p *Playbook) Bullets() []*Bullet {
	result := make([]*Bullet, 0, len(p.bullets))
	for _, b := range p.bullets {
		result = append(result, b)
	}
	return result
}

// BulletIDs 返回所有 bullet ID。
// 对齐 Python Playbook.bullet_ids()。
func (p *Playbook) BulletIDs() []string {
	ids := make([]string, 0, len(p.bullets))
	for id := range p.bullets {
		ids = append(ids, id)
	}
	return ids
}

// LoadBullet 加载已有 bullet（反序列化用）。
// 对齐 Python Playbook.load_bullet(bullet)。
func (p *Playbook) LoadBullet(bullet *Bullet) {
	p.bullets[bullet.ID] = bullet
	p.sections[bullet.Section] = append(p.sections[bullet.Section], bullet.ID)
}

// SetNextID 设置 nextID（反序列化用）。
// 对齐 Python Playbook.set_next_id(next_id)。
func (p *Playbook) SetNextID(nextID int) {
	p.nextID = nextID
}

// ToDict 序列化为字典。
// 对齐 Python Playbook.to_dict()。
func (p *Playbook) ToDict() map[string]any {
	bulletsMap := make(map[string]any, len(p.bullets))
	for id, bullet := range p.bullets {
		bulletsMap[id] = bulletToMap(bullet)
	}
	return map[string]any{
		"bullets":  bulletsMap,
		"sections": p.sections,
		"next_id":  p.nextID,
	}
}

// PlaybookFromDict 从字典反序列化。
// 对齐 Python Playbook.from_dict(payload)。
func PlaybookFromDict(payload map[string]any) *Playbook {
	instance := NewPlaybook()
	bulletsPayload, ok := payload["bullets"]
	if ok {
		if bulletsMap, ok := bulletsPayload.(map[string]any); ok {
			for bulletID, bulletValue := range bulletsMap {
				if bv, ok := bulletValue.(map[string]any); ok {
					bullet := bulletFromMap(bv)
					// 确保使用 payload 的 key 作为 ID
					bullet.ID = bulletID
					instance.bullets[bulletID] = bullet
				}
			}
		}
	}
	sectionsPayload, ok := payload["sections"]
	if ok {
		if sectionsMap, ok := sectionsPayload.(map[string]any); ok {
			instance.sections = make(map[string][]string, len(sectionsMap))
			for section, idsVal := range sectionsMap {
				switch ids := idsVal.(type) {
				case []any:
					idList := make([]string, 0, len(ids))
					for _, id := range ids {
						if s, ok := id.(string); ok {
							idList = append(idList, s)
						}
					}
					instance.sections[section] = idList
				case []string:
					instance.sections[section] = ids
				}
			}
		}
	}
	nextIDPayload, ok := payload["next_id"]
	if ok {
		switch v := nextIDPayload.(type) {
		case int:
			instance.nextID = v
		case float64:
			instance.nextID = int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				instance.nextID = int(n)
			}
		}
	}
	return instance
}

// Dumps 序列化为 JSON 字符串。
// 对齐 Python Playbook.dumps()：json.dumps(..., ensure_ascii=False, indent=2)。
func (p *Playbook) Dumps() (string, error) {
	data, err := json.MarshalIndent(p.ToDict(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("Playbook.Dumps: 序列化失败: %w", err)
	}
	return string(data), nil
}

// PlaybookLoads 从 JSON 字符串反序列化。
// 对齐 Python Playbook.loads(data)。
func PlaybookLoads(data string) (*Playbook, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, fmt.Errorf("PlaybookLoads: 反序列化失败: %w", err)
	}
	return PlaybookFromDict(payload), nil
}

// ApplyDelta 应用 DeltaBatch。
// 对齐 Python Playbook.apply_delta(delta)。
func (p *Playbook) ApplyDelta(delta *DeltaBatch) {
	for i := range delta.Operations {
		p.applyOperation(&delta.Operations[i])
	}
}

// AsPrompt 生成 LLM 可读的 playbook 字符串。
// 对齐 Python Playbook.as_prompt()。
func (p *Playbook) AsPrompt() string {
	// 按 section 名排序
	sectionNames := make([]string, 0, len(p.sections))
	for section := range p.sections {
		sectionNames = append(sectionNames, section)
	}
	sort.Strings(sectionNames)

	parts := make([]string, 0, len(p.bullets)+len(p.sections))
	for _, section := range sectionNames {
		parts = append(parts, fmt.Sprintf("## %s", section))
		bulletIDs := p.sections[section]
		for _, bulletID := range bulletIDs {
			bullet, ok := p.bullets[bulletID]
			if !ok {
				continue
			}
			counters := fmt.Sprintf("(helpful=%d, harmful=%d, neutral=%d)", bullet.Helpful, bullet.Harmful, bullet.Neutral)
			parts = append(parts, fmt.Sprintf("- [%s] %s %s", bullet.ID, bullet.Content, counters))
		}
	}
	return strings.Join(parts, "\n")
}

// MakePlaybookExcerpt 生成摘录。
// 对齐 Python Playbook.make_playbook_excerpt(bullet_ids)。
func (p *Playbook) MakePlaybookExcerpt(bulletIDs []string) string {
	lines := make([]string, 0, len(bulletIDs))
	seen := make(map[string]bool, len(bulletIDs))
	for _, bulletID := range bulletIDs {
		if seen[bulletID] {
			continue
		}
		seen[bulletID] = true
		bullet := p.GetBullet(bulletID)
		if bullet != nil {
			lines = append(lines, fmt.Sprintf("[%s] %s", bullet.ID, bullet.Content))
		}
	}
	return strings.Join(lines, "\n")
}

// Stats 返回统计信息。
// 对齐 Python Playbook.stats()。
func (p *Playbook) Stats() map[string]any {
	totalHelpful := 0
	totalHarmful := 0
	totalNeutral := 0
	for _, b := range p.bullets {
		totalHelpful += b.Helpful
		totalHarmful += b.Harmful
		totalNeutral += b.Neutral
	}
	return map[string]any{
		"sections": len(p.sections),
		"bullets":  len(p.bullets),
		"tags": map[string]any{
			"helpful": totalHelpful,
			"harmful": totalHarmful,
			"neutral": totalNeutral,
		},
	}
}

// String 实现 Stringer 接口。
// 对齐 Python OperationType 字面值。
func (t OperationType) String() string {
	switch t {
	case OperationAdd:
		return "ADD"
	case OperationUpdate:
		return "UPDATE"
	case OperationTag:
		return "TAG"
	case OperationRemove:
		return "REMOVE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// ParseOperationType 从字符串解析操作类型。
// 不区分大小写，无效值返回 error。
func ParseOperationType(s string) (OperationType, error) {
	switch strings.ToUpper(s) {
	case "ADD":
		return OperationAdd, nil
	case "UPDATE":
		return OperationUpdate, nil
	case "TAG":
		return OperationTag, nil
	case "REMOVE":
		return OperationRemove, nil
	default:
		return 0, fmt.Errorf("ParseOperationType: 未知的操作类型: %q", s)
	}
}

// ApplyMetadata 应用元数据变更。
// 对齐 Python Bullet.apply_metadata(metadata)：增量更新 helpful/harmful/neutral。
func (b *Bullet) ApplyMetadata(metadata map[string]int) {
	for key, value := range metadata {
		switch key {
		case "helpful":
			b.Helpful += value
		case "harmful":
			b.Harmful += value
		case "neutral":
			b.Neutral += value
		}
	}
}

// Tag 对指定标签（helpful/harmful/neutral）增加计数，并更新 UpdatedAt。
// 对齐 Python Bullet.tag(tag, increment=1)。
func (b *Bullet) Tag(tag string, increment int) error {
	switch tag {
	case "helpful", "harmful", "neutral":
		// 合法标签
	default:
		return fmt.Errorf("Bullet.Tag: 不支持的标签: %q", tag)
	}
	switch tag {
	case "helpful":
		b.Helpful += increment
	case "harmful":
		b.Harmful += increment
	case "neutral":
		b.Neutral += increment
	}
	b.UpdatedAt = time.Now().UTC()
	return nil
}

// ToJSON 序列化为 JSON map。
// 对齐 Python DeltaOperation.to_json()。
func (op *DeltaOperation) ToJSON() map[string]any {
	data := map[string]any{
		"type":    op.Type.String(),
		"section": op.Section,
	}
	if op.Content != nil {
		data["content"] = *op.Content
	}
	if op.BulletID != nil {
		data["bullet_id"] = *op.BulletID
	}
	if len(op.Metadata) > 0 {
		data["metadata"] = op.Metadata
	}
	return data
}

// NewDeltaOperationFromJSON 从 JSON 反序列化。
// 对齐 Python DeltaOperation.from_json(payload)。
func NewDeltaOperationFromJSON(payload map[string]any) *DeltaOperation {
	opType, _ := ParseOperationType(getString(payload, "type"))
	var content *string
	if v, ok := payload["content"]; ok && v != nil {
		if s, ok := v.(string); ok {
			content = &s
		}
	}
	var bulletID *string
	if v, ok := payload["bullet_id"]; ok && v != nil {
		if s, ok := v.(string); ok {
			bulletID = &s
		}
	}
	metadata := make(map[string]int)
	if v, ok := payload["metadata"]; ok && v != nil {
		if m, ok := v.(map[string]any); ok {
			for k, val := range m {
				switch n := val.(type) {
				case int:
					metadata[k] = n
				case float64:
					metadata[k] = int(n)
				case json.Number:
					if i, err := n.Int64(); err == nil {
						metadata[k] = int(i)
					}
				}
			}
		}
		// 支持 map[string]int 直接传入
		if m, ok := v.(map[string]int); ok {
			metadata = m
		}
	}
	return &DeltaOperation{
		Type:     opType,
		Section:  getString(payload, "section"),
		Content:  content,
		BulletID: bulletID,
		Metadata: metadata,
	}
}

// ToJSON 序列化为 JSON map。
// 对齐 Python DeltaBatch.to_json()。
func (b *DeltaBatch) ToJSON() map[string]any {
	ops := make([]any, len(b.Operations))
	for i, op := range b.Operations {
		ops[i] = op.ToJSON()
	}
	return map[string]any{
		"reasoning":  b.Reasoning,
		"operations": ops,
	}
}

// NewDeltaBatchFromJSON 从 JSON 反序列化。
// 对齐 Python DeltaBatch.from_json(payload)。
func NewDeltaBatchFromJSON(payload map[string]any) *DeltaBatch {
	reasoning := getString(payload, "reasoning")
	var operations []DeltaOperation
	if opsVal, ok := payload["operations"]; ok && opsVal != nil {
		switch ops := opsVal.(type) {
		case []any:
			operations = make([]DeltaOperation, 0, len(ops))
			for _, item := range ops {
				if m, ok := item.(map[string]any); ok {
					operations = append(operations, *NewDeltaOperationFromJSON(m))
				}
			}
		case []map[string]any:
			operations = make([]DeltaOperation, 0, len(ops))
			for _, m := range ops {
				operations = append(operations, *NewDeltaOperationFromJSON(m))
			}
		}
	}
	if operations == nil {
		operations = []DeltaOperation{}
	}
	return &DeltaBatch{
		Reasoning:  reasoning,
		Operations: operations,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateID 生成唯一 ID。
// 对齐 Python Playbook._generate_id(section)：{section_prefix}-{nextID:05d}。
func (p *Playbook) generateID(section string) string {
	p.nextID++
	sectionPrefix := "general"
	if section != "" {
		// 取 section 第一个单词（空格分隔）作为前缀，转小写
		parts := strings.Fields(section)
		if len(parts) > 0 {
			sectionPrefix = strings.ToLower(parts[0])
		}
	}
	return fmt.Sprintf("%s-%05d", sectionPrefix, p.nextID)
}

// applyOperation 应用单条操作。
// 对齐 Python Playbook._apply_operation(operation)：按 ADD/UPDATE/TAG/REMOVE 分发。
func (p *Playbook) applyOperation(op *DeltaOperation) {
	switch op.Type {
	case OperationAdd:
		p.AddBullet(op.Section, derefString(op.Content, ""), op.BulletID, op.Metadata)
	case OperationUpdate:
		if op.BulletID == nil {
			return
		}
		p.UpdateBullet(*op.BulletID, op.Content, op.Metadata)
	case OperationTag:
		if op.BulletID == nil {
			return
		}
		for tag, increment := range op.Metadata {
			p.TagBullet(*op.BulletID, tag, increment)
		}
	case OperationRemove:
		if op.BulletID == nil {
			return
		}
		p.RemoveBullet(*op.BulletID)
	}
}

// bulletToMap 将 Bullet 转为 map，用于序列化。
func bulletToMap(b *Bullet) map[string]any {
	return map[string]any{
		"id":         b.ID,
		"section":    b.Section,
		"content":    b.Content,
		"helpful":    b.Helpful,
		"harmful":    b.Harmful,
		"neutral":    b.Neutral,
		"created_at": b.CreatedAt.Format(time.RFC3339),
		"updated_at": b.UpdatedAt.Format(time.RFC3339),
	}
}

// bulletFromMap 从 map 构建 Bullet，用于反序列化。
func bulletFromMap(m map[string]any) *Bullet {
	b := &Bullet{
		ID:      getString(m, "id"),
		Section: getString(m, "section"),
		Content: getString(m, "content"),
		Helpful: getInt(m, "helpful"),
		Harmful: getInt(m, "harmful"),
		Neutral: getInt(m, "neutral"),
	}
	if v, ok := m["created_at"]; ok {
		b.CreatedAt = parseTime(v)
	}
	if v, ok := m["updated_at"]; ok {
		b.UpdatedAt = parseTime(v)
	}
	return b
}

// getString 从 map 中安全获取字符串字段。
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getInt 从 map 中安全获取整数字段。
func getInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return 0
}

// parseTime 从 any 值解析 time.Time。
func parseTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		if t == "" {
			return time.Time{}
		}
		parsed, err := time.Parse(time.RFC3339, t)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// derefString 解引用字符串指针，为零值时返回默认值。
func derefString(s *string, defaultVal string) string {
	if s != nil {
		return *s
	}
	return defaultVal
}
