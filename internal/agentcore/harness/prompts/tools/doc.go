// Package tools 提供工具元数据提供者的注册与查询机制。
//
// 每个内置工具实现 ToolMetadataProvider 接口，提供双语描述（cn/en）
// 和参数 JSON Schema。所有工具通过 init() 自注册到全局注册表。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── base.go              # ToolMetadataProvider 接口与校验函数
//	├── registry.go          # 全局注册表（RegisterToolProvider / GetToolProvider / AllProviders）
//	├── agent_mode.go        # switch_mode / enter_plan_mode / exit_plan_mode 工具
//	├── ask_user.go          # ask_user 工具
//	├── audio.go             # audio_transcription / audio_question_answering / audio_metadata 工具
//	├── bash.go              # bash 工具
//	├── code.go              # code 工具
//	├── coding_memory.go     # coding_memory_read / write / edit 工具
//	├── cron.go              # cron 及旧版 cron_* 工具
//	├── enter_worktree.go    # enter_worktree 工具
//	├── exit_worktree.go     # exit_worktree 工具
//	├── filesystem.go        # read_file / write_file / edit_file / glob / list_files / grep 工具
//	├── list_skill.go        # list_skill 工具
//	├── load_tools.go        # load_tools 工具
//	├── lsp_tool.go          # lsp 工具
//	├── mcp.go               # list_mcp_resources / read_mcp_resource 工具
//	├── memory.go            # memory_search / memory_get / write_memory / edit_memory / read_memory 工具
//	├── powershell.go        # powershell 工具
//	├── search_tools.go      # search_tools 工具
//	├── session_tools.go     # sessions_list / sessions_spawn / sessions_cancel 工具
//	├── skill_tool.go        # skill_tool 工具
//	├── task_tool.go         # task_tool 工具
//	├── todo.go              # todo_create / todo_list / todo_modify / todo_get 工具
//	├── video_understanding.go # video_understanding 工具
//	├── vision.go            # image_ocr / visual_question_answering 工具
//	└── web_tools.go         # free_search / paid_search / fetch_webpage 工具
//
// 对应 Python 代码：openjiuwen/harness/prompts/tools/
package tools
