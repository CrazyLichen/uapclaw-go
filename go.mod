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
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/amikos-tech/chroma-go v0.4.1
	github.com/fsnotify/fsnotify v1.10.1
	github.com/getkin/kin-openapi v0.140.0
	github.com/glebarez/sqlite v1.11.0
	github.com/google/uuid v1.6.0
	github.com/mark3labs/mcp-go v0.54.1
	github.com/milvus-io/milvus-sdk-go/v2 v2.4.2
	github.com/redis/go-redis/v9 v9.20.0
	github.com/rs/zerolog v1.35.1
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	go.etcd.io/bbolt v1.4.3
	golang.org/x/text v0.32.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/gorm v1.31.1
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/amikos-tech/chroma-go-local v0.3.4 // indirect
	github.com/amikos-tech/pure-onnx v0.0.1 // indirect
	github.com/amikos-tech/pure-tokenizers v0.1.5 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cockroachdb/errors v1.9.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20211118104740-dabe8e521a4f // indirect
	github.com/cockroachdb/redact v1.1.3 // indirect
	github.com/creasty/defaults v1.8.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.12 // indirect
	github.com/getsentry/sentry-go v0.12.0 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/milvus-io/milvus-proto/go-api/v2 v2.4.10-0.20240819025435-512e3b98866a // indirect
	github.com/oasdiff/yaml v0.1.0 // indirect
	github.com/oasdiff/yaml3 v0.0.13 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	google.golang.org/genproto v0.0.0-20220503193339-ba3ae3f07e29 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	modernc.org/libc v1.22.5 // indirect
	modernc.org/mathutil v1.5.0 // indirect
	modernc.org/memory v1.5.0 // indirect
	modernc.org/sqlite v1.23.1 // indirect
)
