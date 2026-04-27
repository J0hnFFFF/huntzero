---
description: 假设验证 - 快速验证假设过滤假阳性
---

# 邮服假设验证 (Hypothesis Tester)

快速验证关于邮件协议交互的猜想。

## 验证目标

1.  **状态机猜想**: "在未发送 `EHLO` 的情况下直接发送 `MAIL FROM`，服务器会怎么响应？"
2.  **格式兼容猜想**: "如果我把 MIME boundary 设置为极长字符串，解析器会崩溃还是截断？"
3.  **权限猜想**: "这个 `EXPN` 指令是否仅对 localhost 开放？"

## 验证方法

### 1. 交互探测 (Interactive Probing)
使用 `nc` 或 Python `smtplib`：
*   手动发送畸形指令序列。
*   观察返回码 (250 vs 5xx) 和错误信息。

### 2. 侧信道验证 (Side-channel)
*   **Timing**: 解析大文件的时间差异。
*   **DNS**: 触发 OOB DNS 请求验证命令执行。

## 输出格式
```
## 验证报告

### 假设
[描述假设]

### 验证过程
- 工具: [Frida/GDB]
- 脚本: [Script snippet]
- 观察: [Log output]

### 结论
[Validated / Refuted]
```