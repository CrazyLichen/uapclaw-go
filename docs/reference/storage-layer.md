# 领域四：存储层参考文档

> 本文档详细介绍存储层全部 9 个组件的接口定义、实现变体、Key 命名规则，
> 以及每个组件在 LLM 交互流程中的具体用法和示例。
>
> 对应 Python 源码：`openjiuwen/core/foundation/store/`
> 对应 Go 实现计划：`IMPLEMENTATION_PLAN.md` 步骤 4.1-4.19

---

## 目录

- [1. 全景概览](#1-全景概览)
- [2. BaseKVStore — 键值存储](#2-basekvstore--键值存储)
- [3. BaseVectorStore — 向量存储](#3-basevectorstore--向量存储)
- [4. BaseDbStore — 关系数据库引擎](#4-basedbstore--关系数据库引擎)
- [5. BaseMessageStore — 消息持久化](#5-basemessagestore--消息持久化)
- [6. BaseMemoryIndex — 统一记忆索引](#6-basememoryindex--统一记忆索引)
- [7. Embedding — 嵌入模型](#7-embedding--嵌入模型)
- [8. Reranker — 重排序器](#8-reranker--重排序器)
- [9. GraphStore — 知识图谱存储](#9-graphstore--知识图谱存储)
- [10. ObjectStorage — S3 对象存储](#10-objectstorage--s3-对象存储)
- [11. 完整 LLM 对话的存储流转](#11-完整-llm-对话的存储流转)
- [12. 实现优先级建议](#12-实现优先级建议)

---

## 1. 全景概览

### 架构定位

```
领域四（存储层）的消费者关系：

  ┌─────────────┐
  │ 领域7 记忆系统 │ ← 最重消费者，依赖全部 7 个存储基类
  └──────┬──────┘
         │
  ┌──────┴──────┐
  │ 领域5 会话系统 │ ← 依赖 BaseKVStore（Checkpointer 持久化）
  └──────┬──────┘
         │
  ┌──────┴──────┐
  │ 领域9 DeepAgent│ ← 依赖 EmbeddingConfig + 间接通过 Memory 使用存储
  └──────┬──────┘
         │
  ┌──────┴──────┐
  │ 领域10-11 Swarm│ ← 依赖 BaseKVStore（持久化 Checkpointer）+ EmbeddingConfig
  └─────────────┘

  领域6（Agent Core）和领域5（上下文引擎）→ 无直接存储依赖
```

### 各组件影响范围

| 存储组件 | 核心消费者 | 缺失时的后果 |
|----------|-----------|-------------|
| **BaseKVStore** | 会话 Checkpointer、长期记忆、变量管理、SimpleMemoryIndex | **致命** — Agent 状态无法持久化，会话恢复失败，所有记忆存储断裂 |
| **BaseVectorStore** | 语义记忆（SemanticStore）、SimpleMemoryIndex、图记忆 | **致命** — 语义检索完全失效，记忆搜索退化为 KV 全扫描 |
| **BaseDbStore** | 关系型记忆表（UserMessage/ScopeMapping/MemoryMeta） | **严重** — 聊天历史无法 SQL 持久化，记忆元数据丢失 |
| **BaseMessageStore** | MessageManager → 消息持久化 | **严重** — 对话历史无法存储检索 |
| **Embedding** | 语义存储写入/搜索、图记忆、MemoryRail、CodingMemoryRail | **致命** — 向量集合无法填充，所有语义检索链路断裂 |
| **BaseMemoryIndex** | SearchManager/WriteManager/SummaryManager/FragmentMemoryManager | **致命** — 统一记忆索引接口消失，所有 Manager 失效 |
| **Reranker** | 仅 GraphMemory 使用 | **低** — 图记忆搜索质量降级，但系统仍可运行 |
| **GraphStore** | 图记忆（实体/关系/三元组） | **高** — 图记忆无法运作 |
| **ObjectStorage** | 当前无直接消费者 | **无** — 预留基础设施 |

---

## 2. BaseKVStore — 键值存储

> Python 源码：`openjiuwen/core/foundation/store/base_kv_store.py`

### 接口定义

| 方法 | 说明 |
|------|------|
| `set(key, value)` | 存入/覆盖键值对 |
| `exclusive_set(key, value, expiry?)` | 原子设置（仅当 key 不存在时成功），支持 TTL |
| `get(key)` | 获取值，不存在返回 None |
| `exists(key)` | 检查 key 是否存在 |
| `delete(key)` | 删除 key |
| `get_by_prefix(prefix)` | 按前缀批量获取 |
| `delete_by_prefix(prefix, batch_size?)` | 按前缀批量删除 |
| `mget(keys)` | 批量获取 |
| `batch_delete(keys, batch_size?)` | 批量删除，返回删除数量 |
| `pipeline()` | 创建批量操作管道 |

### 三种实现

| 实现 | 存储介质 | 持久化 | 外部依赖 | 适用场景 |
|------|---------|--------|---------|---------|
| **InMemoryKVStore** | 进程内存 `dict` | ❌ 进程退出丢失 | 无 | 开发/测试、临时信号 |
| **DbBasedKVStore** | SQLAlchemy `AsyncEngine` (SQLite/MySQL) | ✅ | SQLAlchemy + aiosqlite | **生产默认** |
| **ShelveStore** | Python `shelve` 本地文件 | ✅ | 无（标准库） | 轻量本地持久化 |

### 选择路由

```
CheckpointerFactory.from_config():
  db_type="sqlite"  → DbBasedKVStore(aiosqlite engine)   ← 生产默认
  db_type="shelve"  → ShelveStore(db_path)               ← 轻量备选

OpenJiuwenMemoryProvider._create_kv_store():
  backend="memory"  → InMemoryKVStore()                  ← 开发/测试
  backend="sqlite"  → DbBasedKVStore(aiosqlite engine)   ← 生产默认
  backend="shelve"  → ShelveStore(db_path)               ← 轻量备选
```

### Key 命名规则

#### 会话 Checkpointer

Key 用 `:` 分隔，格式 `{session_id}:{namespace}:{entity_id}:{suffix}`：

| 命名空间 | Key 模式 | 示例 | 值内容 |
|---------|---------|------|-------|
| `agent` | `{sid}:agent:{aid}:agent_state_blobs` | `sess-001:agent:react-1:agent_state_blobs` | pickle 序列化的 Agent 图状态 |
| `agent` | `{sid}:agent:{aid}:agent_state_blobs_dump_type` | `sess-001:agent:react-1:agent_state_blobs_dump_type` | `"pickle"` |
| `agent-team` | `{sid}:agent-team:{tid}:agent_team_state_blobs` | `sess-001:agent-team:team-xyz:agent_team_state_blobs` | pickle 序列化的团队全局状态 |
| `workflow` | `{sid}:workflow:{wid}:workflow_state_blobs` | `sess-001:workflow:wf-999:workflow_state_blobs` | pickle 序列化的工作流状态 |
| `workflow` | `{sid}:workflow:{wid}:workflow_update_blobs` | `sess-001:workflow:wf-999:workflow_update_blobs` | pickle 序列化的待更新状态 |
| `workflow-graph` | `{sid}:workflow-graph:{ns}:checkpoint_data_value` | `sess-001:workflow-graph:wf-999:checkpoint_data_value` | pickle 序列化的图检查点 |

#### 记忆系统

| Key 模式 | 示例 | 值内容 |
|---------|------|-------|
| `UMD/{uid}/{sid}/{mem_id}` | `UMD/user-123/scope-7/a1b2c3d4e5f6` | JSON：`{id, user_id, scope_id, mem(AES加密), mem_type, timestamp, source_id}` |
| `UMD/{uid}/{sid}/ids` | `UMD/user-123/scope-7/ids` | 连接的 24 字符 hex ID 串 |
| `UMD/{uid}/{sid}/{mem_type}/ids` | `UMD/user-123/scope-7/user_profile/ids` | 按类型的 ID 索引 |
| `UMD/{uid}/{sid}/UPT/ids` | `UMD/user-123/scope-7/UPT/ids` | 全部片段记忆 ID 索引 |
| `user_var/{uid}/{sid}/{var_name}` | `user_var/user-123/scope-7/preferred_language` | AES 加密的变量值 |
| `session_var/{uid}/{sid}/{sess_id}/{var_name}` | `session_var/user-123/scope-7/sess-abc/current_topic` | AES 加密的会话变量值 |
| `memory_scope_config/{scope_id}` | `memory_scope_config/scope-7` | JSON 加密的作用域配置 |
| `_lock/user/{user_id}` | `_lock/user/user-123` | UUID 锁令牌，TTL=10s |

### LLM 交互示例：会话中断恢复

```
用户: "帮我订一张明天去北京的机票"
LLM:  调用订票工具，发现需要确认 → 中断，等待用户确认
      ↓ 此时 Agent 状态被序列化存入 KV：

      Key:   sess-001:agent:react-agent-1:agent_state_blobs
      Value: pickle({
          "messages": [
              {"role":"user", "content":"帮我订一张明天去北京的机票"},
              {"role":"assistant", "tool_calls": [{"name":"book_ticket","args":{...}}]}
          ],
          "next": "tool"
      })

--- 用户第二天回来 ---
用户: "确认，订吧"
      ↓ 从 KV 恢复 Agent 状态，注入用户确认，继续执行

      pipeline = kv_store.pipeline()
      pipeline.get("sess-001:agent:react-agent-1:agent_state_blobs")  → 恢复
      pipeline.execute()
```

### LLM 交互示例：记忆内容存储

```
用户: "我叫小明，是个后端开发"
LLM:  调用记忆写入工具
      ↓ LongTermMemory 提取记忆后写入 KV：

      Key:   UMD/user-123/scope-7/a1b2c3d4e5f6
      Value: {
          "id":       "a1b2c3d4e5f6",
          "user_id":  "user-123",
          "scope_id": "scope-7",
          "mem":      "AES_ENCRYPTED(用户叫小明，是后端开发)",
          "mem_type": "user_profile",
          "timestamp":"2026-06-08 14:30:00",
          "source_id":"msg_xyz789"
      }

      同时写入向量存储（见 BaseVectorStore）
```

### Go 实现建议

| Python 实现 | Go 等价 | 说明 |
|-------------|--------|------|
| `InMemoryKVStore` | `sync.Map` 或 `map + sync.RWMutex` | 最简单 |
| `DbBasedKVStore` | SQLite via `modernc.org/sqlite` | 需要异步引擎封装 |
| `ShelveStore` | BoltDB/BadgerDB 或砍掉 | shelve 无 Go 等价物，可考虑直接砍掉只保留两种实现 |

---

## 3. BaseVectorStore — 向量存储

> Python 源码：`openjiuwen/core/foundation/store/base_vector_store.py`

### 接口定义

| 方法 | 说明 |
|------|------|
| `create_collection(name, schema)` | 创建集合（定义字段） |
| `delete_collection(name)` | 删除集合 |
| `collection_exists(name)` | 检查集合是否存在 |
| `add_docs(collection_name, docs)` | 插入文档（id + embedding + 可选 metadata） |
| `search(collection_name, query_vector, vector_field, top_k, filters)` | 向量相似度搜索 |
| `delete_docs_by_ids(collection_name, ids)` | 按 ID 删除 |
| `delete_docs_by_filters(collection_name, filters)` | 按条件删除 |
| `update_schema(collection_name, operations)` | Schema 迁移 |

### 集合命名规则

```
uid_{user_id}_gid_{scope_id}_mtype_{mem_type}

mem_type 取值：
  user_profile      — 用户画像（姓名、偏好）
  semantic_memory   — 语义记忆（知识点）
  episodic_memory   — 情景记忆（事件、经历）
  summary           — 对话摘要
```

**示例**：
- `uid_user-123_gid_scope-7_mtype_user_profile`
- `uid_user-123_gid_scope-7_mtype_episodic_memory`

### 向量 Schema（每个集合只有两个字段）

```python
schema.add_field(FieldSchema(name="id",       dtype=VARCHAR,    max_length=256, is_primary=True))
schema.add_field(FieldSchema(name="embedding", dtype=FLOAT_VECTOR, dim=embedding_dim))
```

**重要**：向量集合只存 `id` + `embedding`，不存文本。实际内容在 KV 里。
向量维度由嵌入模型决定，惰性获取（首次调用 API 后缓存）。

### 三种实现

| 实现 | 后端 | 适用场景 |
|------|------|---------|
| **ChromaVectorStore** | ChromaDB（本地嵌入式） | 开发/单机部署 |
| **MilvusVectorStore** | Milvus（分布式） | 生产集群 |
| **GaussVectorStore** | GaussDB | 华为云 |

### LLM 交互示例：搜索相关记忆

```
用户: "我之前跟你说过什么关于项目的事？"
      ↓
      ┌─────────────── 搜索阶段 ───────────────┐
      │                                          │
      │  1. 嵌入查询：                            │
      │     embed_query("项目的事")               │
      │     → [0.023, -0.015, 0.042, ...] 768维  │
      │                                          │
      │  2. 向量搜索（按记忆类型查不同集合）：       │
      │     collection: uid_user-123_gid_scope-7_mtype_user_profile │
      │     → hits: [{id:"a1b2", score:0.92}]    │
      │                                          │
      │     collection: uid_user-123_gid_scope-7_mtype_episodic_memory │
      │     → hits: [{id:"c3d4", score:0.78}]    │
      │                                          │
      │  3. 从 KV 取完整内容：                     │
      │     mget(["UMD/user-123/scope-7/a1b2",   │
      │           "UMD/user-123/scope-7/c3d4"])   │
      │     → 解密 AES → 得到文本                  │
      └──────────────────────────────────────────┘
      ↓
      注入 LLM System Prompt：

      <memory-context>
      ## 相关记忆
      - [user_profile] 用户叫小明，是后端开发 (score: 0.92)
      - [episodic_memory] 用户提到Phoenix项目使用React (score: 0.78)
      </memory-context>
      ↓
LLM:  "根据之前的对话，你提到你在做 Phoenix 项目，使用的是 React 技术栈..."
```

### LLM 交互示例：写入记忆（双写）

```
用户: "我在做 Phoenix 项目，用的 Go 语言"
LLM:  "好的，我记下了"
      ↓ add_messages() 自动触发记忆提取：

      1. LLM 二次调用 → 提取记忆单元：
         "用户在做Phoenix项目，使用Go语言" → mem_type: "episodic_memory"

      2. 双写：
         embed_documents(["用户在做Phoenix项目，使用Go语言"])
         → [0.056, 0.078, ...]

         VectorStore.add_docs("uid_user-123_gid_scope-7_mtype_episodic_memory",
                              [{id:"e5f6", embedding:[0.056, 0.078, ...]}])
         KVStore.set("UMD/user-123/scope-7/e5f6", AES_ENCRYPTED_JSON)
```

### KV + Vector 双存储架构

```
写入：
  memory text → embed_documents() → vector_store.add_docs(id + embedding)  [向量库]
                                 → kv_store.set(key, JSON(content))      [KV 库]

搜索：
  query text → embed_query() → vector_store.search() → hit IDs + scores
                                                      → kv_store.mget(hit IDs) → 完整内容
                                                      → 合并返回
```

**设计意义**：向量库只存 ID + 向量（不含敏感文本，可部署第三方服务如 Milvus），
KV 库存完整加密内容（本地管控），实现安全与性能的分离。

---

## 4. BaseDbStore — 关系数据库引擎

> Python 源码：`openjiuwen/core/foundation/store/base_db_store.py`

### 接口定义

```python
class BaseDbStore(ABC):
    @abstractmethod
    def get_async_engine(self) -> AsyncEngine:
        """返回 SQLAlchemy AsyncEngine"""
```

**核心职责**：只做一件事——提供 SQLAlchemy `AsyncEngine`，是 SQL 世界的"门禁卡"。

### 实现

| 实现 | 说明 |
|------|------|
| **DefaultDbStore** | 简单包装，持有 `AsyncEngine` 并返回 |

### SqlDbStore（基于 BaseDbStore 的 CRUD 层）

| 方法 | 说明 |
|------|------|
| `write(table, data)` | INSERT |
| `get(table, record_id, columns)` | SELECT by id |
| `get_with_sort(table, filters, sort_by, order, limit)` | SELECT + WHERE + ORDER + LIMIT |
| `condition_get(table, conditions, columns)` | SELECT with IN |
| `update(table, conditions, data)` | UPDATE |
| `delete(table, conditions)` | DELETE |

### 三张表

#### user_message（对话消息）

| 列 | 类型 | 说明 |
|----|------|------|
| `message_id` | VARCHAR(64) PK | 消息 ID，格式 `msg_{hash[:16]}_{ts_millis}` |
| `user_id` | VARCHAR(64) | 用户 ID |
| `scope_id` | VARCHAR(64) | 作用域 ID |
| `content` | VARCHAR(4096) | **AES 加密**的消息内容 |
| `session_id` | VARCHAR(64) | 会话 ID |
| `role` | VARCHAR(32) | 角色：user / assistant |
| `timestamp` | VARCHAR(32) | 时间戳 |

#### scope_user_mapping（用户-作用域映射）

| 列 | 类型 | 说明 |
|----|------|------|
| `user_id` | VARCHAR(64) PK | 用户 ID |
| `scope_id` | VARCHAR(64) PK | 作用域 ID |

#### memory_meta（迁移元数据）

| 列 | 类型 | 说明 |
|----|------|------|
| `table_name` | VARCHAR(64) PK | 表名 |
| `schema_version` | VARCHAR(64) | Schema 版本号 |

### LLM 交互中的角色（间接）

```
BaseDbStore → 提供引擎 → SqlDbStore → SqlMessageStore → MessageManager → LongTermMemory

具体链路：
  用户消息 → MessageManager.add() → SqlMessageStore.add_message()
                                     ↓
                                     AES 加密 content → INSERT INTO user_message
```

---

## 5. BaseMessageStore — 消息持久化

> Python 源码：`openjiuwen/core/foundation/store/base_message_store.py`

### 接口定义

| 方法 | 说明 |
|------|------|
| `add_message(message_add)` | 存储单条消息，返回 message_id |
| `add_messages(message_adds)` | 批量存储 |
| `get_message_by_id(message_id)` | 按 ID 获取 → `(BaseMessage, MessageMetadata)` |
| `get_messages(filter, limit, order_by, order_direction)` | 条件查询 + 分页 |
| `update_message(message_id, content)` | 更新内容 |
| `delete_message_by_id(message_id)` | 删除单条 |
| `delete_messages(filter)` | 按条件删除 |
| `count_messages(filter)` | 计数 |
| `get_schema_version()` / `set_schema_version(version)` | Schema 版本管理 |

### 实现：SqlMessageStore

**写入时加密**：
```python
content = self._codec.encode(message.content)   # AES-256-GCM 加密
data = {'message_id': message_id, 'content': content, ...}
await self.sql_db_store.write('user_message', data)
```

**读取时解密**：
```python
content = self._codec.decode(message_data['content'])  # AES-256-GCM 解密
base_msg = BaseMessage(content=content, role=message_data['role'])
```

### MessageMetadata

```python
class MessageMetadata:
    message_id: str
    user_id: str
    scope_id: str
    session_id: str
    timestamp: datetime
    message_type: str
```

### LLM 交互示例：记忆提取需要历史上下文

```
用户: "我喜欢用 VSCode"
LLM:  "好的"
      ↓ add_messages() 内部流程：

      1. 先取最近 2 条历史消息（给 LLM 提取器看）：
         MessageManager.get(user_id, scope_id, session_id, message_len=2)
         → SqlMessageStore.get_messages()
           → SELECT * FROM user_message WHERE user_id='user-123'
             AND scope_id='scope-7' ORDER BY timestamp DESC LIMIT 2
           → AES 解密 content
         → 返回: [
               BaseMessage(role="user", content="我在做Phoenix项目"),
               BaseMessage(role="assistant", content="好的，我记下了")
           ]

      2. 把当前消息 + 历史一起给 LLM 记忆提取器：
         "根据以下对话，提取用户的记忆单元..."

      3. 把当前消息加密存入 user_message 表
```

### LLM 交互示例：下次对话获取历史

```
LongTermMemory.get_recent_messages(user_id, scope_id, session_id, num=10)
  → MessageManager.get(user_id, scope_id, session_id, message_len=10)
    → SqlMessageStore.get_messages(filter, limit=10, order_direction='desc')
      → SELECT ... ORDER BY timestamp DESC LIMIT 10
      → AES 解密每条 content
      → 反转为时间正序
    → 返回 list[BaseMessage]
```

---

## 6. BaseMemoryIndex — 统一记忆索引

> Python 源码：`openjiuwen/core/foundation/store/base_memory_index.py`

### MemoryDoc 数据模型

```python
class MemoryDoc(BaseModel):
    id: str = ""                      # 唯一标识
    text: str = ""                    # 文本内容（存入时 AES 加密）
    type: str = ""                    # "user_profile" | "semantic_memory" | "episodic_memory" | "summary"
    timestamp: datetime               # 创建时间
    fields: dict[str, Any] = {}       # 扩展字段 {"source_id": "msg_xyz789"}
```

### 接口定义

| 方法 | 说明 |
|------|------|
| `set_storage_codec(codec)` | 注入 AES 加解密器 |
| `add_memories(user_id, scope_id, memories)` | 添加 MemoryDoc 列表 |
| `update_memories(user_id, scope_id, memories)` | 更新 MemoryDoc 列表 |
| `delete_memories(user_id, scope_id, ids)` | 按 ID 删除 |
| `delete_by_user(user_id)` | 删除用户全部记忆 |
| `delete_by_scope(scope_id)` | 删除作用域全部记忆 |
| `search(user_id, scope_id, query, mem_types, top_k)` | 语义搜索 → `list[tuple[MemoryDoc, float]]` |
| `get_by_id(user_id, scope_id, mem_id)` | 按 ID 获取 |
| `list_memories(user_id, scope_id, offset, limit, mem_types)` | 分页列举 |

### 实现：SimpleMemoryIndex（KV + Vector + Embedding 编排）

#### 搜索流程

```
search(query, user_id, scope_id, mem_types, top_k)
  │
  ├─ 1. embed_query(query) → query_vector
  │
  ├─ 2. 对每个 mem_type 并行搜索向量库：
  │     for mt in [user_profile, semantic_memory, episodic_memory]:
  │       col = "uid_{uid}_gid_{sid}_mtype_{mt}"
  │       if exists(col):
  │         vector_store.search(col, query_vector, top_k) → hit_ids + scores
  │
  ├─ 3. 从 KV 批量取完整文档：
  │     keys = ["UMD/{uid}/{sid}/{id1}", "UMD/{uid}/{sid}/{id2}", ...]
  │     kv_store.mget(keys) → raw JSON
  │
  ├─ 4. 解密 + 反序列化 → MemoryDoc 列表
  │
  └─ 5. 按 score 降序排列，取 top_k 返回
```

#### 写入流程（带去重检查）

```
add_memories(user_id, scope_id, memories=[MemoryDoc(...)])
  │
  ├─ 1. 按类型分组
  ├─ 2. embed_documents([doc.text, ...]) → embeddings
  ├─ 3. 确保向量集合存在（不存在则创建）
  ├─ 4. vector_store.add_docs(col, [{id, embedding}, ...])
  ├─ 5. kv_store.set("UMD/uid/sid/mem_id", AES_ENCRYPTED(json))
  └─ 6. 更新 ID 跟踪索引
```

#### 去重流程

```
写入前检查：
  1. 对每条新记忆文本做 search(query=new_mem, top_k=5)
  2. 收集 score > 0.75 的旧记忆（UPDATE_CHECK_OLD_MEMORY_RELEVANCE_THRESHOLD）
  3. 调用 LLM: MemUpdateChecker.check(new_memories, old_memories)
     → 返回 action_items: [{action: "ADD"} | {action: "DELETE", target: old_mem_id}]
  4. ADD → 执行写入；DELETE → 删除冲突旧记忆后写入
```

### 上层消费者

| Manager | 使用的 MemoryIndex 方法 | 说明 |
|---------|------------------------|------|
| **FragmentMemoryManager** | search, add_memories, update_memories, delete_memories | 用户画像/语义/情景记忆 |
| **SummaryManager** | search, add_memories, update_memories | 对话摘要 |
| **WriteManager** | 通过子 Manager 间接使用 | 写入协调 |
| **SearchManager** | 通过子 Manager 间接使用 | 搜索协调 |

---

## 7. Embedding — 嵌入模型

> Python 源码：`openjiuwen/core/foundation/store/base_embedding.py`
> API 实现：`openjiuwen/core/retrieval/embedding/api_embedding.py`

### 接口定义

| 方法 | 说明 |
|------|------|
| `embed_query(text) → List[float]` | 嵌入单条查询文本 |
| `embed_documents(texts, batch_size?) → List[List[float]]` | 批量嵌入文档文本 |
| `dimension → int` | 向量维度（惰性获取，首次调用 API 后缓存） |

### 实现：APIEmbedding

- HTTP POST 到 OpenAI 兼容的 Embedding API
- 请求格式：`{"model": model_name, "input": text_or_list}`
- 响应格式兼容：`{"embedding": [...]}` / `{"embeddings": [...]}` / `{"data": [{"embedding": [...]}]}`
- 内置重试（3 次）、SSL 配置、并发控制（信号量 + 批次分割）

### EmbeddingConfig

```python
class EmbeddingConfig(BaseModel):
    model_name: str
    base_url: str
    api_key: Optional[str]
```

### LLM 交互中的两次调用

```
┌─── 写入时 ─────────────────────────────────────────┐
│                                                     │
│  用户对话 → LLM 提取记忆 → "用户叫小明，是后端开发"   │
│                            ↓                        │
│  embed_documents(["用户叫小明，是后端开发"])           │
│  → [0.012, -0.045, 0.078, ...] 768维               │
│  → 存入向量库                                       │
└─────────────────────────────────────────────────────┘

┌─── 搜索时 ─────────────────────────────────────────┐
│                                                     │
│  用户提问 → "小明是做什么的？"                        │
│              ↓                                      │
│  embed_query("小明是做什么的？")                      │
│  → [0.015, -0.040, 0.082, ...] 768维               │
│  → 向量相似度搜索 → 命中记忆                          │
└─────────────────────────────────────────────────────┘
```

### 作用域级嵌入模型

每个 scope 可以有自己的嵌入模型配置，`LongTermMemory` 在每次操作前切换：

```python
async def _apply_scope_embedding(self, scope_id):
    scope_embed = await self._get_scope_embedding_model(scope_id)
    if scope_embed:
        self.memory_index.set_embedding_model(scope_embed)  # 切换模型
    else:
        self.memory_index.set_embedding_model(self._base_embed)  # 回退默认
```

此方法在 `add_messages()`、`search_user_mem()`、`update_mem_by_id()` 开始时调用。

### EmbeddingConfig 的传播链

```
Swarm/Harness CLI → 解析环境变量/配置 → EmbeddingConfig
  → MemoryRail / CodingMemoryRail (构造器参数)
  → OpenJiuwenMemoryProvider._create_embedding(config) → APIEmbedding(config)
  → LongTermMemory._base_embed / _scope_embedding
  → SimpleMemoryIndex._embedding_model
```

---

## 8. Reranker — 重排序器

> Python 源码：`openjiuwen/core/foundation/store/base_reranker.py`

### 接口定义

```python
class Reranker(ABC):
    async def rerank(query, docs, instruct=True) → dict[str, float]
    def rerank_sync(query, docs, instruct=True) → dict[str, float]
```

- 输入：查询文本 + 文档列表
- 输出：`{文档ID: 相关性分数}`
- `instruct`：是否在查询前添加指令（默认 "Given a search query, retrieve relevant candidates that answer the query."）

### 三种实现

| 实现 | API 端点 | 特点 |
|------|---------|------|
| **StandardReranker** | `/rerank`（vLLM 风格） | 标准重排序 API |
| **DashscopeReranker** | DashScope 精排 API | 支持多模态输入 |
| **ChatReranker** | `/chat/completions` | 实验性：用 Chat 模型 + logit_bias 计算 P("yes") 概率分 |

### 使用场景：仅 GraphMemory 搜索

**当前只在图记忆搜索中使用**，普通向量记忆搜索不经过 Reranker。

在 `MilvusGraphStore.search()` 中的流程：

```
1. Milvus 混合搜索（三路并行）：
   - name_embedding 密集搜索   (权重 0.15)
   - content_embedding 密集搜索 (权重 0.60)
   - content_bm25 稀疏搜索     (权重 0.25)

2. 加权融合 → 候选集

3. Reranker 精排（如果提供了 reranker）

4. BFS 图扩展（从命中实体出发 1 跳取关联关系和邻居）

5. 返回精排后的结果
```

### LLM 交互示例

```
用户: "Alice 的职位是什么？"
      ↓ 向量搜索（粗筛）返回：

      Doc A: "Alice is a software engineer"     score=0.85
      Doc B: "Alice prefers dark mode"          score=0.72
      Doc C: "The Alice project uses React"     score=0.68

      ↓ Reranker 精排（交叉编码器重新打分）：

      Doc A: "Alice is a software engineer"     score=0.95  ← 直接回答了问题，大幅提升
      Doc B: "Alice prefers dark mode"          score=0.30  ← 偏好无关，大幅下降
      Doc C: "The Alice project uses React"     score=0.10  ← 另一个 Alice，排除

      ↓ Top-1 注入 LLM 上下文
```

---

## 9. GraphStore — 知识图谱存储

> Python 源码：`openjiuwen/core/foundation/store/graph/base_graph_store.py`
> 图对象模型：`openjiuwen/core/foundation/store/graph/graph_object.py`
> Milvus 实现：`openjiuwen/core/foundation/store/graph/milvus/milvus_support.py`

### 三种图对象

#### Entity（实体/节点）

```python
class Entity:
    uuid: str                       # 唯一标识
    name: str                       # "Alice"
    obj_type: str                   # "Entity" | "Human" | "AI" | "Organization" | "Project"
    content: str                    # "A senior software engineer at Huawei"
    attributes: dict                # {"department": "Engineering", "tenure_years": 5}
    relations: list[str]            # 关联的 Relation UUID 列表
    episodes: list[str]             # 关联的 Episode UUID 列表
    name_embedding: list[float]     # 名称的嵌入向量
    content_embedding: list[float]  # 内容的嵌入向量
    content_bm25: list[float]       # BM25 稀疏向量
```

#### Relation（关系/边）

```python
class Relation:
    uuid: str                       # 唯一标识
    name: str                       # "works_at" | "works_on" | "manages"
    content: str                    # "Alice works at Huawei as a senior engineer since 2020"
    lhs: str                        # 源实体 UUID
    rhs: str                        # 目标实体 UUID
    valid_since: int                # 有效起始时间戳
    valid_until: int                # 有效结束时间戳 (-1=至今)
```

#### Episode（对话/文档片段）

```python
class Episode:
    uuid: str                       # 唯一标识
    content: str                    # 原始文本片段
    valid_since: int                # 时间戳
    entities: list[str]             # 提及的实体 UUID 列表
```

### GraphStore 协议

| 方法 | 说明 |
|------|------|
| `add_entity(entity)` | 添加实体（自动嵌入） |
| `add_relation(relation)` | 添加关系 |
| `add_episode(episode)` | 添加片段 |
| `search(query, k, collection, ranker_config, reranker, bfs_depth, bfs_k)` | 混合搜索 + 可选 BFS + 精排 |
| `query(collection, ids, expr)` | 精确查询 |
| `delete(collection, ids, expr)` | 删除 |

### Milvus 实现：三个集合

| 集合 | 字段 | 说明 |
|------|------|------|
| `entity` | uuid, name, obj_type, content, attributes, name_embedding, content_embedding, content_bm25 | 实体节点 |
| `relation` | uuid, name, content, lhs, rhs, valid_since, valid_until, content_embedding, content_bm25 | 关系边 |
| `episode` | uuid, content, entities, valid_since, content_embedding, content_bm25 | 对话片段 |

### LLM 交互示例：从对话构建知识图谱

```
用户: "Alice 是华为的高级工程师，她在做 Phoenix 项目"
LLM:  "我记下了"
      ↓ GraphMemory.add_memory() 自动触发多步 LLM 调用：

      Step 1: 提取实体
        LLM → [Entity(name="Alice", type="Human"),
               Entity(name="华为", type="Organization"),
               Entity(name="Phoenix", type="Project")]

      Step 2: 嵌入实体名称
        embed_documents(["Alice", "华为", "Phoenix"]) → vectors

      Step 3: 提取关系
        LLM → [Relation(lhs=Alice, rhs=华为, name="works_at", content="高级工程师"),
               Relation(lhs=Alice, rhs=Phoenix, name="works_on", content="开发")]

      Step 4: 去重（与已有实体比对）
        LLM → "Alice" 与已有实体 "Alice" 匹配 → 合并

      Step 5: 丰富属性
        LLM → Entity(Alice, attributes={"department":"Engineering", "tenure_years":5})

      Step 6: 持久化到 Milvus（entity/relation/episode 三个集合）
```

### 搜索流程：混合搜索 + BFS 图扩展 + 精排

```
search(query="Alice 的职位", k=5)
  │
  ├─ 1. embed_query("Alice 的职位") → query_vector
  │
  ├─ 2. Milvus 混合搜索（三路并行）：
  │     - name_embedding 密集搜索   (权重 0.15)
  │     - content_embedding 密集搜索 (权重 0.60)
  │     - content_bm25 稀疏搜索     (权重 0.25)
  │     → 加权融合 → 候选集
  │
  ├─ 3. Reranker 精排（可选）
  │
  ├─ 4. BFS 图扩展：从命中实体出发，1 跳取关联关系和邻居实体
  │     Entity "Alice" → Relation "works_at" → Entity "华为"
  │                     → Relation "works_on" → Entity "Phoenix"
  │
  └─ 5. 返回: {
           "entity":   [(score, Entity), ...],
           "relation": [(score, Relation), ...],
           "episode":  [(score, Episode), ...]
       }
```

---

## 10. ObjectStorage — S3 对象存储

> Python 源码：`openjiuwen/core/foundation/store/object/base_storage_client.py`

### 接口定义

| 方法 | 说明 |
|------|------|
| `upload_file(bucket, object_name, file_path)` | 上传文件 |
| `download_file(bucket, object_name, file_path)` | 下载文件 |
| `delete_object(bucket, object_name)` | 删除对象 |
| `create_bucket(bucket, location)` | 创建桶 |
| `delete_bucket(bucket)` | 删除桶 |
| `list_objects(bucket, prefix, max_objects)` | 列出对象 |

### 实现：AioBotoClient

基于 `aioboto3` 的 S3 兼容客户端，凭证从构造器或环境变量获取：
- `OBS_ACCESS_KEY_ID`
- `OBS_SECRET_ACCESS_KEY`
- `OBS_SERVER`
- `OBS_REGION`

### 当前状态

**不在 LLM 交互主链路上**。是通用基础设施组件，当前代码中无直接消费者。潜在用途：
- 存储用户上传的文件
- 存储对话日志
- 存储模型产物

---

## 11. 完整 LLM 对话的存储流转

```
用户: "我叫小明，在做 Phoenix 项目，用的 Go"
  │
  ├─① 消息持久化 ──────────────────────────────────────┐
  │  MessageManager.add() → SqlMessageStore              │
  │    → AES 加密 → INSERT INTO user_message             │ BaseDbStore
  │    (同时取出最近2条历史消息给提取器)                    │ BaseMessageStore
  │                                                       │
  ├─② 记忆提取 ──────────────────────────────────────┐  │
  │  Generator.gen_all_memory()                        │  │ Embedding (嵌入查询)
  │    → LLM 二次调用 → 提取记忆单元                     │  │
  │    → user_profile: "用户叫小明"                      │  │
  │    → episodic_memory: "在做Phoenix项目，用Go"        │  │
  │                                                     │  │
  ├─③ 记忆写入 ──────────────────────────────────────┐  │  │
  │  WriteManager → FragmentMemoryManager              │  │  │
  │    → Embedding.embed_documents()  ──────────────→ │  │  │
  │    → VectorStore.add_docs(id + embedding)  ─────→ │  │  │ BaseVectorStore
  │    → KVStore.set("UMD/uid/sid/mem_id", AES_JSON)─→│  │  │ BaseKVStore
  │    → 更新 ID 跟踪索引                              │  │  │ BaseMemoryIndex
  │                                                     │  │  │
  └─────────────────────────────────────────────────────┘  │  │
                                                           │  │
下次对话: "我之前说过什么项目？"                              │  │
  │                                                        │  │
  ├─④ 记忆搜索 ─────────────────────────────────────────┐  │  │
  │  LongTermMemory.search_user_mem()                     │  │  │
  │    → Embedding.embed_query("项目")  ───────────────→ │  │  │
  │    → VectorStore.search(col, query_vec, top_k) ────→ │  │  │
  │    → KVStore.mget([UMD/.../mem_ids]) ─────────────→ │  │  │
  │    → AES 解密 → MemoryDoc 列表                        │  │  │
  │                                                       │  │  │
  ├─⑤ 注入上下文 ──────────────────────────────────────┐  │  │  │
  │  ExternalMemoryRail.before_model_call()              │  │  │  │
  │    → 搜索结果注入 System Prompt:                      │  │  │  │
  │      <memory-context>                                │  │  │  │
  │      [episodic] 在做Phoenix项目，用Go (score: 0.92)   │  │  │  │
  │      </memory-context>                               │  │  │  │
  │                                                       │  │  │  │
  └─⑥ LLM 生成回复 ────────────────────────────────────┘  │  │  │
     "你之前提到你在做 Phoenix 项目，使用 Go 语言"          │  │  │
                                                           │  │  │
  同时: Checkpointer 保存 Agent 状态到 KV ────────────────→ │  │  │
    Key: sess-001:agent:react-1:agent_state_blobs          │  │  │
    (下次中断恢复时读取)                                    ┘  ┘  ┘
```

### 9 个组件在对话中的角色

| 组件 | 角色 | 是否必须 |
|------|------|---------|
| **BaseKVStore** | 存 Agent 状态 + 记忆内容 + 变量 + 锁 | ✅ 核心 |
| **BaseVectorStore** | 语义搜索的记忆索引 | ✅ 语义搜索必须 |
| **BaseDbStore** | 提供 SQL 引擎给消息表 | ✅ 消息持久化必须 |
| **BaseMessageStore** | 存对话历史（AES 加密） | ✅ 上下文必须 |
| **BaseMemoryIndex** | 编排 KV + Vector + Embedding | ✅ 统一接口必须 |
| **Embedding** | 文本→向量，搜索和写入都需要 | ✅ 语义搜索必须 |
| **Reranker** | 图记忆搜索精排 | ⚠️ 仅 GraphMemory 用 |
| **GraphStore** | 知识图谱存储 | ⚠️ 高级功能，非必须 |
| **ObjectStorage** | S3 文件存储 | ❌ 当前不在主链路 |

---

## 12. 实现优先级建议

| 阶段 | 必须实现 | 可延后 |
|------|---------|-------|
| **MVP** | BaseKVStore + InMemoryKVStore（会话内存持久化） | 其余全部 |
| **生产基础** | DbBasedKVStore、BaseDbStore + DefaultDbStore、BaseMessageStore + SqlMessageStore、Embedding + APIEmbedding | 向量存储、图存储 |
| **生产高级** | BaseVectorStore + Chroma/Milvus、BaseMemoryIndex + SimpleMemoryIndex、Reranker | GraphStore、ObjectStorage |
| **扩展** | GraphStore + Milvus、ObjectStorage + S3 | 更多后端实现 |
