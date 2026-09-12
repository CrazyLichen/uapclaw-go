// Package sharing 提供跨 Agent 经验共享功能。
//
// 本包实现了经验的上传、下载、搜索和质量筛选，
// 让不同用户的技能演进经验可以共享和复用。
//
// 两条数据路径：
//   - 上传路径：本地已审批的 EvolutionRecord → ShareStager QC 筛选 →
//     KeywordExtractor 关键词提取 → 打包为 SharedSkillBundle → 上传到 SharingBackend
//   - 下载路径：KeywordExtractor 从对话摘录提取检索关键词 →
//     ExperienceHubClient 搜索/下载 → EvolutionStore 安装到本地
//
// 文件目录：
//
//	sharing/
//	├── doc.go                  # 包文档
//	├── types.go                # 共享数据类型（SharingMeta/SharedExperience/SharedSkillBundle 等）
//	├── keyword_extractor.go    # 关键词提取器（上传解析 + 下载 LLM 提取）
//	├── experience_sharer.go    # 上传/下载门面 + 待上传队列
//	├── share_stager.go         # QC 筛选 + 入队
//	├── hub_client.go           # 搜索 + 安装高层封装
//	└── backend/                # 后端接口和实现
//	    ├── doc.go              # 子包文档
//	    ├── interface.go        # SharingBackend 抽象接口
//	    └── local_file.go       # LocalFileBackend 本地文件实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/sharing/
package sharing
