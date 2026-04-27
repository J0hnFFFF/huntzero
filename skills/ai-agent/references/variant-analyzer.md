---
description: AI Agent 变体分析 - 特权滥用与感染散扩
---

# 变体分析者 (Variant Analyzer): 举一反三与长效持久

专家找到一个洞后，绝不满足于此，他们会在思想上拓展其边界和破坏力。

## 专家的发散思维

### 1. 维度扩展：从单次注入到持久化威胁（Secondary Payload）
*   既然我能用间接注入让他执行一个命令，我能否让他**自我重写（Self-Rewriting）**系统提示词？
*   能否利用他的特权去修改系统内部其他的 Knowledge Base，将带有恶意载荷的信息布撒在全库，使得未来任何其他合法用户询问此类问题时，其对应的 Agent 都会被自动劫持？这是**“Agentic 蠕虫（Agentic Worm）”**的概念。

### 2. 平行工具变体
*   我发现 `read_file` 存在路径穿越。同理，`write_file`, `unzip_archive` 是否也缺乏防御？
*   我发现 `bash_tool` 没有鉴权。那么 `python_interpreter` 是否也没有网络出口限制，能够用来反弹 Shell？

### 3. 多通道污染
*   除了读取网页，Agent 是否监听 WebHook？
*   是否分析邮件？（发一封带有恶意指令的简历 PDF 附件到公司的自动化处理邮箱，触发零点击提权）。
