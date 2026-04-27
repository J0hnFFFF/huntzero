---
description: 评估验证 - Crash 验尸与可利用性判定
---

# 二进制评估验证 (Binary Validator)

并非所有的 Crash 都是漏洞，并非所有的漏洞都能利用。

## 评估维度

### 1. 崩溃验尸 (Crash Triage)

核心问题：Crash 的瞬间，程序的状态是什么？

*   **控制流劫持 (EIP/RIP Control)**: 崩溃时指令指针是否指向非法地址（如 `0x41414141`）？如果是，极大概率可利用。
*   **写原语 (Write Primitive)**: 崩溃指令是否是 `mov [eax], edx`？如果是，我们能控制写地址 (`eax`) 和写内容 (`edx`) 吗？
*   **读越界 (Read AV)**: 仅仅是读不可访问内存？通过堆风水能否转化为信息泄露？

### 2. 利用环境 (Exploit Context)

核心问题：系统保护机制对利用的阻碍。

*   **ASLR/PIE**: 我们有 Info Leak 吗？没有的话，无法定位 Gadget。
*   **NX/DEP**: 堆栈不可执行。需要构建 ROP 链。
*   **Canary**: 栈溢出前是否需要先泄露或爆破 Canary？

### 3. 可达性与稳定性

*   **远程可达**: 该 Crash 路径是否可以通过网络数据包触发？
*   **复现率**: 是必现 Crash，还是依赖特定堆状态的概率性 Crash？

## 验证方法论

### 自动化调试验证
Agent 调用调试器 (GDB/WinDbg) 进行验尸：
1.  **重现**: `r < crash_input`
2.  **回溯**: `bt` / `k` 查看调用栈。
3.  **指令分析**: `x/i $pc` 查看崩溃指令。
4.  **寄存器分析**: `i r` 查看寄存器是否被污染 (Tainted)。

### `!exploitable` 判断
使用类似 GDB `exploitable` 插件的逻辑：
*   **Exploitable**: PC 被覆盖，或向任意地址写。
*   **Probably Exploitable**: 栈被破坏，无法回溯。
*   **Probably Low/Not**: 空指针解引用 (通常不可利用，除非内核是 Null 映射)。

## 输出格式
```
## 评估报告

### 崩溃现场
- 信号/异常: [SIGSEGV / Access Violation]
- 崩溃指令: [Assembly]

### 可利用性判定
- 结论: [Exploitable / DoS only]
- 理由: [EIP Control / Heap Corruption / ...]

### 缓解措施建议
[开启 NX/Canary 等]
```
