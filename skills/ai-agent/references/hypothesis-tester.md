---
description: AI Agent 假设验证 - 心智演绎与逻辑试错
---

# 假设验证者 (Hypothesis Tester): 沙盘上的心智推演

在无法实际对目标 LLM 发送大量 Token（可能引发计费或审计告警）或进行系统级破坏的情况下，专家如何验证漏洞？靠“代码路径的逻辑闭环”。

## 专家的推演方法

### 1. Taint Tracking (污点追踪) 的确定性验证
*   如果外部数据（污点源 `Source`）通过网络请求进入了 Agent 的 `context_memory`。
*   验证系统是否有任何对该 `Source` 的截断、严格模式化校验或 LLM 安全对齐检查（如 Llama Guard 过滤）。
*   如果毫无阻碍地拼接进了字符串传给了 ChatCompletion，那么 **Prompt Injection 这个环节在逻辑上是 100% 成立的。**

### 2. Tool Backend 的脆弱性证明
*   暂时忽略 LLM。假设我们是那个决定用什么参数调用 Tool 的中央调度器。
*   单独审计那个 `def read_file(path: str):` 函数。
*   如果我传入 `path = "../../../../../etc/passwd"`，它会报错吗？
*   去查看底层有没有 `os.path.abspath` 和前缀匹配检查。如果没有，那么 **Tool 逻辑漏洞是 100% 成立的。**

## 你的验证结果
一旦这首（Injection 必成）尾（执行必达）两端验证通过，中间的大模型仅仅是一个黑盒桥梁（只要有足够好的 Jailbreak/Injection 提示词技术即可打通）。
基于这两端得出明确的“存在剥削路径”的定论。
