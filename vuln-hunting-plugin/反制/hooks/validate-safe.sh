#!/bin/bash
# 阻止危险命令
if echo "$COMMAND" | grep -E '(rm\s|sudo\s|nc\s|curl.*http|wget|pip install|apt|yum)' ; then
    echo "BLOCKED: 禁止执行危险命令: $COMMAND" >&2
    exit 2
fi

# 仅允许只读操作（针对 Bash 工具）
if [[ "$TOOL" == "Bash" ]] && echo "$COMMAND" | grep -E '(>|>>|echo.*>|\swrite)' ; then
    echo "BLOCKED: 禁止写文件操作" >&2
    exit 2
fi

exit 0