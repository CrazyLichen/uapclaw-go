#!/bin/sh
# 模拟 ACP 对端（10.6.24d 测试用）：逐行读 JSON-RPC，按 method 回响应。
# 本脚本验证 handlePeerRequest 的 fs 读写/权限/未知方法分支。
# ACP fs 请求路径须为绝对路径（客户端按其进程 cwd 解析相对路径，与 Python
# os.path.abspath 语义一致）；本脚本 cwd 即会话根，用 $(pwd) 构造。
#   session/prompt 依次发：
#     1) fs/read_text_file（root 内，line/limit 切片）
#     2) fs/write_text_file（root 内，落盘验证）
#     3) fs/read_text_file（params 非 dict → -32602）
#     4) session/request_permission（auto approve → allow-once）
#     5) 未知方法（→ -32601）
#   每个请求后读一行我方响应（仅消费，内容由测试侧经文件系统验证）。
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid"
            ;;
        session/new)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"peer-sess-1"}}\n' "$rid"
            ;;
        session/prompt)
            printf '{"jsonrpc":"2.0","id":800,"method":"fs/read_text_file","params":{"path":"%s/in_root.txt","line":1,"limit":2}}\n' "$(pwd)"
            read -r reply
            printf '{"jsonrpc":"2.0","id":801,"method":"fs/write_text_file","params":{"path":"%s/written.txt","content":"hello-acp"}}\n' "$(pwd)"
            read -r reply
            printf '{"jsonrpc":"2.0","id":802,"method":"fs/read_text_file","params":"not-a-dict"}\n'
            read -r reply
            printf '{"jsonrpc":"2.0","id":803,"method":"session/request_permission","params":{"toolCall":{"title":"t"}}}\n'
            read -r reply
            printf '{"jsonrpc":"2.0","id":804,"method":"some/unknown_method","params":{}}\n'
            read -r reply
            printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$rid"
            ;;
        *)
            printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unsupported"}}\n' "$rid"
            ;;
    esac
done
