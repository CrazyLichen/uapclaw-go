#!/bin/sh
# 模拟 ACP 对端（10.6.24d 测试用）：逐行读 JSON-RPC，按 method 回响应。
# initialize → {"protocolVersion":1}
# session/new → {"sessionId":"peer-sess-1"}
# session/prompt → 先发一条越界 fs/read_text_file 对端请求并等待一行响应
#                  （验证 handlePeerRequest 回写响应使对端不阻塞），
#                  再发 session/update 文本，最后回 {"stopReason":"end_turn"}
# 其他方法 → error -32601
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
            printf '{"jsonrpc":"2.0","id":900,"method":"fs/read_text_file","params":{"path":"../outside.go"}}\n'
            read -r reply
            printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"content":{"type":"text","text":"peer-reply"}}}}\n'
            printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$rid"
            ;;
        *)
            printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unsupported"}}\n' "$rid"
            ;;
    esac
done
