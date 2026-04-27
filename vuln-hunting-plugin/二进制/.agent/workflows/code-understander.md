---
description: 二进制代码理解 - 基于 Rizin 的静态转译与切片
---

# 二进制代码理解者 (Binary Code Understander)

专注于将二进制指令“翻译”为人类与 AI 可理解的形式。

## 核心思想：分层解构 (Layered Deconstruction)

不试图一次性理解整个程序，而是按需解构：
1.  **宏观层**: 文件头、段信息、导入导出表 -> 理解程序**大致功能**。
2.  **微观层**: 函数控制流图 (CFG)、伪代码 -> 理解特定**逻辑细节**。

## 关键关注点

*   **数据流切片 (Data Flow Slicing)**:
    *   不阅读完整的大函数。
    *   只关注：`Source` (输入) 如何流向 `Sink` (危险操作)。
*   **代码归一化 (Normalization)**:
    *   消除编译器优化带来的噪音。
    *   将复杂汇编结构（如循环展开）还原为高级语言结构（Loop）。

## 工具链集成 (Rizin + rz-ghidra)

*   **反汇编**: `rizin -c "pdf @ main"`
*   **反编译 (Pseudocode)**: `rizin -c "pdg @ main"` (需安装 rz-ghidra)
*   **字符串提取**: `rizin -c "iz ~password"` (寻找敏感字符串)
*   **函数列表**: `rizin -c "afl"`

## 思考路径

1.  **Checksec**: 首先看能利用什么保护机制？(NX? PIE?)
2.  **Strings**: 有没有硬编码密码？有没有 `system` cmd 字符串？
3.  **Imports**: 它能联网吗 (`ws2_32`)？能操作文件吗 (`CreateFile`)？
4.  **Decompile**: 提取关键函数的伪代码，进行变量名推断 (Variable Renaming)。
