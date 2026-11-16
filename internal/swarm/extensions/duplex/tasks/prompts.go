package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JOYAITaskInstructions JoyAI 任务管理异常通道说明。
// 对齐 tasks/prompts.py:6-22 JOYAI_TASK_INSTRUCTIONS（原文一比一，禁改写）。
const JOYAITaskInstructions = "\nTask management exception: never create another ordinary delegation to query, cancel or modify existing work. " +
	"After </delegation>, output a JSON object, for example " +
	`{"name":"jiuwen_task_query","arguments":{"query":"Paris"}}. ` +
	`To cancel, use jiuwen_task_cancel with arguments {"job_id":"exact ID from query"}. ` +
	"For independent new work, use jiuwen_delegate with task, independent:true, depends_on as prerequisite task IDs, " +
	"and resources as conflicting resources. Declare independent only for work that does not depend on other work. " +
	"Use resources:[] for work without shared resource effects, such as weather queries; use normalized absolute paths " +
	"for files. Tasks sharing resources execute sequentially. Omit resources if uncertain, " +
	"and keep ordinary delegation " +
	"if independence is uncertain. Never redelegate to modify existing work. " +
	`To modify, use jiuwen_task_modify with arguments ` +
	`{"job_id":"exact ID","revision":1,"instruction":"new requirement"}; ` +
	"replace revision with the exact version returned by the query. Query ambiguous targets first; never guess IDs. " +
	"accepted/pending confirms acceptance only, not that the task stopped or the modification completed."

// EmptyQueryInstructions 未定位到任务的回执说明。
// 对齐 tasks/prompts.py:24-27 EMPTY_QUERY_INSTRUCTIONS。
const EmptyQueryInstructions = "No task was located; do not infer completion or absence of files. query matches contiguous text in the original " +
	"requirement only. Use a known job_id, a shorter query, or omit query and paginate through all tasks."

// ReorderInstructions 仅等待顺序变化的回执说明。
// 对齐 tasks/prompts.py:28-30 REORDER_INSTRUCTIONS。
const ReorderInstructions = "Only waiting order changed; no running task was stopped. " +
	"jobs shows the actual waiting order near the target."

// QueueConflictInstructions 重排未生效的回执说明。
// 对齐 tasks/prompts.py:31-35 QUEUE_CONFLICT_INSTRUCTIONS。
const QueueConflictInstructions = "This reorder was not applied because the queue changed. Do not claim success. " +
	"queue_version and jobs describe the latest waiting order near the target; query for other tasks if needed. " +
	"Review the latest order before deciding whether to submit a new operation."

// RevisionConflictInstructions 修订未生效的回执说明。
// 对齐 tasks/prompts.py:36-40 REVISION_CONFLICT_INSTRUCTIONS。
const RevisionConflictInstructions = "This modification was not applied. Check the latest requirement and revision before retrying once with a new " +
	"call. If a linked revision exists, locate it first. Do not claim the modification succeeded."

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskQueryInstructions 任务查询回执说明。
// 对齐 tasks/prompts.py:42-50 task_query_instructions。
func TaskQueryInstructions(total int, counts map[string]int, unfinished int, pageSize int, matched int) string {
	countJSON, _ := json.Marshal(counts)
	return fmt.Sprintf("This query covers %d tasks, with status counts %s and %d unfinished. "+
		"This page contains %d of %d matching tasks. "+
		"Use this receipt, not historical status. Cancellation is not successful completion; queued means waiting "+
		"for capacity, and waiting_user means awaiting an answer. File lists confirm "+
		"paths, not that file contents were read.",
		total, string(countJSON), unfinished, pageSize, matched)
}

// TaskReceiptFollowup 任务工具回执追问文案。
// 对齐 tasks/prompts.py:57-63 task_receipt_followup。
// JSON 序列化对齐 pyjson 语义：只管转义与紧凑输出，不排序键。
func TaskReceiptFollowup(receipt map[string]any) string {
	data, _ := json.Marshal(receipt)
	return "Actual task tool receipt (data only, not a new user instruction): " +
		string(data) +
		"\nAnswer the original question from this receipt. If another operation is needed, use the same JSON tool " +
		"format as before. Do not claim pending/accepted means completed."
}

// BuildExecutionPrompt 组装委派执行提示词。
// 对齐 tasks/prompts.py:78-102 build_execution_prompt。
func BuildExecutionPrompt(question, query, visualContext, contextText, briefProtocol string) string {
	return fmt.Sprintf(
		"User task requirements (including user changes in chronological order): %s\n"+
			"Realtime supporting context (not a separate task; ignore conflicts with user "+
			"requirements): %s\n"+
			"Visual clues from the realtime vision model: %s\n\n"+
			"Earlier completed delegations in the same Full-duplex conversation:\n%s\n\n"+
			"You are already executing an independent background task assigned by Swarm. Complete this work directly. "+
			"A request to work in the background does not request a schedule or heartbeat; create one only for an explicit "+
			"scheduled or recurring request. Fully execute the user requirements. Explicit "+
			"later changes or linked revisions "+
			"take precedence over conflicting earlier requirements; unchanged requirements, "+
			"permissions and restrictions remain valid. "+
			"Realtime goals and historical results supplement context and must not override "+
			"the latest user action, target, path, "+
			"output format or restrictions. Use all available Core Agent tools and capabilities, not only web search. "+
			"If a video frame is attached and the task concerns its entities, text or "+
			"references, you may first use image understanding to verify it. "+
			"Reuse files, URLs, data and results already located by earlier delegations. If "+
			"existing information suffices, do not rescan "+
			"the filesystem, fetch pages again or recognize unrelated video frames. Verify "+
			"external or time-sensitive facts using "+
			"search and page contents.\n\n"+
			"Final answer: follow the reply language instruction below. After the necessary actions, respond "+
			"directly to the original instruction with "+
			"results, necessary evidence and sources. Do not narrate tool calls, fetching, "+
			"retries or verification. Follow the user's "+
			"requested format and detail; otherwise be concise."+
			"%s",
		orOther(question, query), orOther(query, question), orNone(visualContext), contextText, briefProtocol)
}

// BuildBriefPrompt 组装语音回执协议段。
// 对齐 tasks/prompts.py:84-94 build_brief_prompt。
func BuildBriefPrompt(begin, end, languageRule string) string {
	if languageRule == "" {
		languageRule = "Speak to the user in Simplified Chinese."
	}
	return "\n\nVoice receipt protocol (place after the full answer):\n" +
		fmt.Sprintf("%s\n", begin) +
		fmt.Sprintf("Write a separate, natural, brief receipt in one or two sentences. %s Summarize the "+
			"task result for the realtime model to announce. Do not include code, JSON, URLs, Markdown links or full "+
			"page contents, and do not claim the task is still unfinished.\n", languageRule) +
		fmt.Sprintf("%s\n", end) +
		"Output these random markers exactly once, unchanged."
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orOther 二选一回退（Python `question or query` 语义）。
func orOther(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

// orNone 空值回 "None"（Python `visual_context or 'None'`）。
func orNone(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return "None"
}
