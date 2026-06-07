module github.com/uapclaw/uapclaw-go

go 1.25.5

// 后续领域依赖（按需添加）
//
// 领域一：基础设施（待 1.4/1.5 实现后添加）
// - gopkg.in/yaml.v3        YAML 配置管理
// - github.com/fsnotify/fsnotify  配置热重载
// - github.com/rs/zerolog   结构化日志
//
// 领域二：LLM 基础层
// - (无额外依赖，使用 net/http)
//
// 领域三：工具系统
// - github.com/mark3labs/mcp-go  MCP 协议客户端
//
// 领域四：存储层
// - github.com/mattn/go-sqlite3       SQLite
// - github.com/jackc/pgx/v5          PostgreSQL
// - github.com/redis/go-redis/v9     Redis
// - github.com/milvus-io/milvus-sdk-go/v2  Milvus
//
// 领域五：会话与上下文
// - github.com/pkoukk/tiktoken-go    Tiktoken
//
// 领域六：Agent 核心
// - (无额外依赖)
//
// 领域八：工作流
// - (无额外依赖)
//
// 领域十：AgentServer
// - github.com/gorilla/websocket     WebSocket
// - github.com/go-chi/chi/v5         HTTP 路由
//
// 领域十一：Gateway + IM
// - github.com/go-telegram-bot-api/telegram-bot-api/v5  Telegram
// - github.com/bwmarrin/discordgo    Discord
// - github.com/larksuite/oapi-sdk-go/v3  飞书
//
// 领域十二：沙箱
// - github.com/landlock-lsm/go-landlock/landlock  Landlock

require (
	github.com/fsnotify/fsnotify v1.10.1
	github.com/google/uuid v1.6.0
	github.com/rs/zerolog v1.35.1
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.29.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
