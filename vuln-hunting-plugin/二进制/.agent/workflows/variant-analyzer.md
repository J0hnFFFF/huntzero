---
description: 变体分析 - 二进制模式匹配与伪代码搜索
---

# 二进制变体分析 (Binary Variant Analyzer)

在没有源码的情况下，如何举一反三？

## 变体类型

### 1. 伪代码同源性 (Pseudocode Homology)

利用 LLM 对“逻辑相似性”的理解：
*   **逻辑特征**: 寻找具有相似控制流或数据处理逻辑的代码块。
*   **搜索策略**: 让 Rizin 遍历关键函数，提取伪代码，LLM 识别是否存在逻辑结构相似的 API 调用。

### 2. 二进制签名 (Binary Signatures)

利用字节码特征：
*   **特征码**: 导致漏洞的特定汇编指令序列 (e.g. 特定的检查逻辑被编译器生成的字节码)。
*   **YARA 规则**: 编写 YARA 规则扫描整个二进制文件（或同目录下的其他 DLL）。

### 3. 相似函数识别 (Function Similary)

利用 `Bndiff` 或 `Diaphora` 的思想：
*   **控制流指纹**: 寻找 CFG 结构相似的函数。
*   **常数指纹**: 寻找使用相同 Magic Number 或 加密常数的函数。

## 自动化策略

1.  **Xref 遍历**: 既然 `sub_vuln` 被 `recv_handler_A` 调用了，那么 `recv_handler_B` 是否也调用了类似的逻辑？
2.  **字符串交叉引用**: 搜索所有引用了 "%s" (危险格式化) 的代码位置。

## 输出格式
```
## 变体分析报告

### 原始特征
- 逻辑特征: [Pseudocode Pattern]
- 汇编特征: [Instruction Sequence]

### 疑似变体
#### 候选 #N
- 地址: [Address]
- 相似度: [High/Medium]
- 验证思路: [Check same input vector]
```
