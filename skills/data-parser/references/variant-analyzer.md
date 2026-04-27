---
description: 解析库变体分析 - 数据类型的同源扩散
---

# 变体分析者 (Variant Analyzer): 追剿同源代码癌细胞

专家挖掘到一种数学陷阱后，其深知写代码的人一定会把同一个逻辑复制粘贴到同项目的其它地方。这叫同形态变体扩散。

## 专家的发散思维

### 1. 同构拓展 (Struct to Map to Array)
*   如果你发现 `parse_array` 函数里处理元素的 `count` 存在乘法溢出，那么请立刻翻看 `parse_map/dict/object` 函数。它们的 Key/Value 对数解析中，很大概率有一样的、没用防溢出宏保护的乘法分配。

### 2. 同源数据宽度拓展 (Bit-width Escalation)
*   如果解析 `int32` 类型的函数因为有符号扩展（Sign Extension）在强转到大类型时出错了，那么请立刻去看 `int16`, `int8`, `uint32` 的读取函数，同理可证它们极大概率也有此隐患。

### 3. 解码器的双面性
*   你找到了在“字符串解析（反序列化）”过程中的越界写漏洞。
*   请反过来看“序列化（Dump/ToString）”过程。在生成输出往 buffer 里写数据的时候，如果处理超长字符串截断，是否也存在边界外的一个 Null Terminator 写入（OOB 写入单字节 `0x00`）。别小看这个单字节，它就能导致进程 RCE（Off-By-One attack）。
