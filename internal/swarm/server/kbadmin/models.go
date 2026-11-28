package kbadmin

// ──────────────────────────── 结构体 ────────────────────────────

// KBInfo 知识库元数据（kb_info，一行一 KB）
type KBInfo struct {
	// ID 知识库唯一标识
	ID string `gorm:"primaryKey;size:64" json:"id"`
	// Name 名称
	Name string `gorm:"size:255;not null" json:"name"`
	// Description 描述（可 AI 生成，WeKnora generated_profile 对应物）
	Description string `gorm:"size:2048" json:"description"`
	// Type 库类型：document / graph（MultiKB 以聚合配置表达）
	Type string `gorm:"size:32;not null;default:document" json:"type"`
	// ConfigJSON embedding 模型、chunker 类型/参数、检索默认参数、生成问题开关
	ConfigJSON string `gorm:"type:text" json:"config_json"`
	// FolderPath 文件夹树路径
	FolderPath string `gorm:"size:1024" json:"folder_path"`
	// Pinned 是否置顶
	Pinned bool `gorm:"not null;default:false" json:"pinned"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `gorm:"not null" json:"created_at"`
	// UpdatedAt 更新时间（Unix 秒）
	UpdatedAt int64 `gorm:"not null" json:"updated_at"`
}

// TableName 表名
func (KBInfo) TableName() string { return "kb_info" }

// KBDoc 文档元数据（kb_doc，一行一文档）
type KBDoc struct {
	// ID 文档唯一标识
	ID string `gorm:"primaryKey;size:64" json:"id"`
	// KBID 归属知识库
	KBID string `gorm:"index;size:64;not null" json:"kb_id"`
	// FileName 文件名
	FileName string `gorm:"size:512;not null" json:"file_name"`
	// FolderPath 文档夹路径
	FolderPath string `gorm:"size:1024" json:"folder_path"`
	// ParseStatus 解析状态：pending/processing/completed/failed/cancelled
	ParseStatus string `gorm:"size:32;not null;default:pending;index" json:"parse_status"`
	// StagesJSON 阶段明细（parsing/chunking/embedding/extracting 各阶段起止与失败信息）
	StagesJSON string `gorm:"type:text" json:"stages_json"`
	// LastActivityAt 最后活动时间（停滞判定）
	LastActivityAt int64 `gorm:"not null;default:0" json:"last_activity_at"`
	// StallState 停滞标记：normal / suspected_stalled
	StallState string `gorm:"size:32;not null;default:normal" json:"stall_state"`
	// ErrorMsg 失败原因
	ErrorMsg string `gorm:"size:4096" json:"error_msg"`
	// RetryCount 重试计数
	RetryCount int `gorm:"not null;default:0" json:"retry_count"`
	// Summary AI 摘要
	Summary string `gorm:"size:8192" json:"summary"`
	// TagsJSON AI 标签
	TagsJSON string `gorm:"type:text" json:"tags_json"`
	// EnhanceState LLM 二阶段状态：none/succeeded/failed（不阻塞 completed）
	EnhanceState string `gorm:"size:32;not null;default:none" json:"enhance_state"`
	// MetaJSON 格式/大小/页数
	MetaJSON string `gorm:"type:text" json:"meta_json"`
	// ContentHash 内容哈希（同 KB 重复上传冲突判据）
	ContentHash string `gorm:"size:128;index" json:"content_hash"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `gorm:"not null" json:"created_at"`
	// UpdatedAt 更新时间（Unix 秒）
	UpdatedAt int64 `gorm:"not null" json:"updated_at"`
}

// TableName 表名
func (KBDoc) TableName() string { return "kb_doc" }

// KBChunkMeta 分块管理元数据（kb_chunk_meta，一行一分块；chunk 本体在向量库）
type KBChunkMeta struct {
	// ID 分块唯一标识（= 向量库 chunk_id）
	ID string `gorm:"primaryKey;size:64" json:"id"`
	// DocID 归属文档
	DocID string `gorm:"index;size:64;not null" json:"doc_id"`
	// KBID 归属知识库
	KBID string `gorm:"index;size:64;not null" json:"kb_id"`
	// ChunkType 类型：text / image_ocr / image_caption（对齐 WeKnora 类型集）
	ChunkType string `gorm:"size:32;not null;default:text" json:"chunk_type"`
	// Enabled 启停开关（检索侧过滤）
	Enabled bool `gorm:"not null;default:true" json:"enabled"`
	// Revision 乐观锁版本号
	Revision int64 `gorm:"not null;default:1" json:"revision"`
	// ContentHash 内容哈希
	ContentHash string `gorm:"size:128" json:"content_hash"`
	// LocatorsJSON 原文定位（pdf 页码+bbox / docx 段落号 / 字符区间；30.3 回填写入链）
	LocatorsJSON string `gorm:"type:text" json:"locators_json"`
	// QuestionsJSON 生成问题列表及 embedding 存储状态（30.4 回填）
	QuestionsJSON string `gorm:"type:text" json:"questions_json"`
}

// TableName 表名
func (KBChunkMeta) TableName() string { return "kb_chunk_meta" }

// KBChunkRevision 分块编辑历史（kb_chunk_revision，一行一版本）
type KBChunkRevision struct {
	// ID 版本记录唯一标识
	ID string `gorm:"primaryKey;size:64" json:"id"`
	// ChunkID 分块标识
	ChunkID string `gorm:"index;size:64;not null" json:"chunk_id"`
	// Revision 版本号（与 KBChunkMeta.Revision 对齐）
	Revision int64 `gorm:"not null" json:"revision"`
	// ContentJSON 该版本 chunk 内容快照（diff/回滚数据源）
	ContentJSON string `gorm:"type:text;not null" json:"content_json"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `gorm:"not null" json:"created_at"`
}

// TableName 表名
func (KBChunkRevision) TableName() string { return "kb_chunk_revision" }

// KBTask 解析任务状态机（kb_task，一行一任务）
type KBTask struct {
	// ID 任务唯一标识
	ID string `gorm:"primaryKey;size:64" json:"id"`
	// DocID 目标文档
	DocID string `gorm:"index;size:64;not null" json:"doc_id"`
	// KBID 归属知识库
	KBID string `gorm:"index;size:64;not null" json:"kb_id"`
	// Status 状态：pending/processing/completed/failed/cancelled
	Status string `gorm:"size:32;not null;default:pending;index" json:"status"`
	// Progress 进度 0-100
	Progress int `gorm:"not null;default:0" json:"progress"`
	// StageDesc 当前阶段描述
	StageDesc string `gorm:"size:255" json:"stage_desc"`
	// CancelReason 取消原因（server_restart 等）
	CancelReason string `gorm:"size:255" json:"cancel_reason"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `gorm:"not null" json:"created_at"`
	// StartedAt 开始时间（Unix 秒）
	StartedAt int64 `gorm:"not null;default:0" json:"started_at"`
	// FinishedAt 结束时间（Unix 秒）
	FinishedAt int64 `gorm:"not null;default:0" json:"finished_at"`
}

// TableName 表名
func (KBTask) TableName() string { return "kb_task" }
