// Package config 提供 YAML 配置管理和热重载能力。
//
// 核心能力：
//   - 读写 config.yaml（基于 gopkg.in/yaml.v3）
//   - 环境变量解析（${VAR:-default} 语法），递归处理 map/slice
//   - 敏感字段自动解密（通过 DecryptFunc 钩子）
//   - 配置后处理（通过 NormalizeFunc 钩子）
//   - 基于 fsnotify 的配置文件热重载
//   - 配置迁移合并（DeepMerge/MigrateFromTemplate）
//   - 专用分段方法（Server/Logging/Workspace 等）
//   - 并发安全（sync.RWMutex）
//
// 文件目录：
//
//	config/
//	├── config.go         # Config 核心结构体：New/Load/Save/Get/Set/Reload
//	├── envvar.go         # 环境变量解析：ResolveEnvVars + DecryptFunc
//	├── normalize.go      # 配置后处理：NormalizeConfig + ParseCustomHeaders
//	├── sections.go       # 专用分段方法：Server/Logging/Workspace
//	├── reloader.go       # fsnotify 热重载：Reloader/OnReload/Start/Stop
//	├── merge.go          # 配置迁移：DeepMerge/MigrateFromTemplate
//	├── doc.go            # 包文档
//	├── config_test.go    # 核心配置测试
//	├── envvar_test.go    # 环境变量解析测试
//	├── normalize_test.go # 配置后处理测试
//	├── sections_test.go  # 分段方法测试
//	├── reloader_test.go  # 热重载测试
//	├── merge_test.go     # 迁移合并测试
//	└── testdata/         # 测试数据
//	    ├── config.yaml       # 示例配置
//	    ├── envvar_config.yaml # 环境变量测试配置
//	    └── template.yaml     # 模板配置
//
// 对应 Python 代码：jiuwenswarm/common/config.py
package config
