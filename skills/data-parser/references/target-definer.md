---
description: 解析器目标定义 - 专家看到解析库时的第一直觉
tags: [data-parser, target-definer, attack-surface, parser, deserialization]
---

# 解析器目标定义 (Target Definer): 专家看到解析库时的第一直觉

> 解析器是将不受信字节流转换为受信内存对象的编译器。
> 专家的第一眼永远不看业务逻辑，只看**数据如何操控内存分配与代码执行流**。

---

## 触发器：什么景象让解析器专家立刻警觉？

### 触发器一：看到长度前缀字段（Length-Prefix Field）

**你的第一眼反应**："声明长度与实际缓冲区长度之间，必然存在不一致。"

二进制协议（Protobuf、Thrift、MessagePack）或文本协议（HTTP Content-Length）中，数据体前带有长度字段。攻击者可以声明 2GB 却只发送 10 字节，或声明 0 却继续发送数据。

**立即追问链**：
1. 解析器在读取 length 字段后，是否用 `min(declared_length, actual_remaining_bytes)` 做边界约束？
2. 如果 length 被用于 `malloc(length)`，是否校验了上限？负值或超大会发生什么？
3. 指针偏移计算 `ptr += length` 时，ptr 是否会越过分配区或映射区边界？
4. 如果 length 为 0，后续循环或递归是否会进入未定义行为？
5. 多字节长度字段（如 4 字节 little-endian）是否考虑了对齐和截断？
6. 是否存在二次解析：先读 header 分配 buffer，再读 body 写入，两次读之间 buffer 状态是否一致？

### 触发器二：看到嵌套结构支持（Nested Structures）

**你的第一眼反应**："任何允许递归下降的数据结构，都是栈溢出或算法级 DoS 的温床。"

JSON 数组/对象嵌套、XML 实体嵌套、YAML 块嵌套、Protobuf message 嵌套——这些都要求解析器维护递归状态。

**立即追问链**：
1. 嵌套深度是否有硬上限？是全局计数器还是每帧计数器？
2. 达到上限时，解析器是优雅报错还是静默截断？
3. 状态维护在栈上还是堆上？栈上的递归深度通常受 8MB 限制。
4. 是否存在通过引用/别名绕过深度检查的可能？
5. 每个嵌套层级的内存开销是多少？100 万层嵌套是否会耗尽物理内存？
6. 如果解析器使用尾递归优化，深层嵌套是否仍会消耗其他资源（如句柄、临时对象）？

### 触发器三：看到类型提示或类名反序列化（Type Hints / Polymorphic Deserialization）

**你的第一眼反应**："外部数据决定实例化哪个类，等于把 RCE 的钥匙交给了攻击者。"

fastjson 的 `@type`、Jackson 的 `defaultTyping`、YAML 的 `!!python/object`、Python pickle 的 `GLOBAL` 操作码——都是让输入数据选择运行时代码路径的机制。

**立即追问链**：
1. 类型名称是否来自完全不受信的数据流？
2. 类加载器是否限制了可实例化的类白名单？
3. 即使限制了白名单，白名单内的类是否存在危险构造函数或 setter（Gadget Chain）？
4. 反序列化过程中是否会触发 `__init__`、`__setstate__`、`readObject` 等回调？
5. 是否存在通过内部类、数组类或代理类绕过白名单的路径？
6. 类型信息在跨语言传输时（如 Java → Python）是否会被重新解释，导致类型混淆？

### 触发器四：看到编码转换或字符串处理路径

**你的第一眼反应**："逻辑长度与字节长度一旦错位，过滤器和执行器看到的就是两个不同的世界。"

UTF-8 代理对、Unicode 转义 `\uXXXX`、HTML 实体编码、Base64、Percent-Encoding——这些路径上的长度不一致是绕过过滤器的经典手段。

**立即追问链**：
1. 安全过滤器（如长度限制、字符黑名单）是在解码前还是解码后生效？
2. 是否存在 `\x00` 空字节导致 C 风格字符串截断，而上层逻辑仍认为数据完整？
3. Unicode 规范化（NFC/NFD）是否会将两个字符合并为一个，从而改变过滤逻辑？
4. 多字节字符在截断时是否会产生无效序列，导致解析器异常？
5. 编码层报告的长度与内存分配层使用的长度是否一致？
6. 是否存在从 UTF-8 到 UTF-16/32 转换时，字节数膨胀导致的整数溢出？

### 触发器五：看到零拷贝（Zero-Copy）或 StringView/Slice 设计

**你的第一眼反应**："不复制数据意味着对象的生命周期与原 buffer 绑定，buffer 一释放，对象就是幽灵指针。"

高性能解析器（simdjson、Cap'n Proto、FlatBuffers）让解析结果直接指向原始输入 buffer，而非复制数据。

**立即追问链**：
1. 解析出的对象（StringView/Slice）持有的是谁分配的 buffer 指针？
2. 原 buffer 的生命周期由谁管理？是否在解析完成后立即被释放或复用？
3. 如果 buffer 来自网络接收循环中的固定缓冲区，下一次 recv 是否会覆盖 Slice 指向的内存？
4. 业务代码是否误以为 Slice 是独立内存，在 buffer 释放后继续使用？
5. 是否存在并发场景：一个线程释放 buffer，另一个线程读取 Slice？
6. 解析器是否提供了深拷贝 API？开发者是否误用了零拷贝 API？

---

## 攻击面地图：解析器全链条审计清单

| 链条环节 | 目标位置 | 审计焦点 | 典型漏洞 |
|---------|---------|---------|---------|
| **输入边界** | `read()`/`recv()`、文件映射、stdin | 数据来源、最大尺寸、截断策略 | OOB Read、信息泄露 |
| **长度解析** | header 解码、varint 解码 | 整数溢出、负数处理、边界校验 | 整数溢出 → 堆溢出 |
| **内存分配** | `malloc`/`realloc`、自定义分配器 | count*sizeof 溢出、0 长度处理 | 堆溢出、双重释放 |
| **递归下降** | 状态机、递归函数、栈帧增长 | 深度限制、栈溢出、循环引用 | 栈溢出、DoS |
| **类型系统** | 类名解析、反序列化构造器 | 白名单、Gadget Chain、回调触发 | RCE |
| **编码路径** | UTF-8、转义序列、规范化 | 长度错位、截断、无效序列 | 过滤器绕过、OOB |
| **对象生命周期** | StringView、Slice、零拷贝 | buffer 释放时机、别名指针 | UAF、信息泄露 |

---

## 攻击链闭合：从畸形输入到系统崩溃的完整证明

```
[攻击者构造畸形输入]
    → [长度字段欺骗或嵌套深度超限]
    → [解析器整数溢出分配过小 buffer 或栈溢出]
    → [OOB Write / Heap Corruption / Stack Smash]
    → [控制 PC 寄存器或破坏堆元数据]
    → [RCE 或拒绝服务]
```

**闭合条件**：输入可控 + 解析器缺乏边界校验 + 内存状态可被破坏。

---

## 典型代码模式与警觉点

### 脆弱模式：信任长度字段
```c
// 脆弱：直接相信输入声明的长度
uint32_t len = read_u32_be(src);
char *buf = malloc(len);          // ← len 可能极大或为 0
read(src, buf, len);              // ← 如果实际数据不足，可能读取越界或阻塞
```

### 安全模式：双重校验
```c
// 安全：用实际剩余长度约束读取行为
uint32_t declared_len = read_u32_be(src);
size_t actual_len = remaining_bytes(src);
size_t to_read = declared_len < actual_len ? declared_len : actual_len;
char *buf = malloc(to_read + 1);  // ← +1 用于 null 终止
if (!buf) return ERR_NOMEM;
read(src, buf, to_read);
buf[to_read] = '\0';
```

### 脆弱模式：无限制的递归
```python
# 脆弱：无嵌套深度限制的 JSON 解析
def parse_object(stream):
    while stream.peek() != '}':
        parse_value(stream)       # ← 深层嵌套直接爆栈
```

### 安全模式：深度计数器
```python
# 安全：带硬限制的递归
MAX_DEPTH = 10000
def parse_value(stream, depth):
    if depth > MAX_DEPTH:
        raise ParseError("nesting too deep")
    if stream.peek() == '{':
        parse_object(stream, depth + 1)
```

---

## 输出要求

1. **攻击面地图**：列出所有输入边界和格式支持特性，标注风险等级（P0/P1/P2）。
2. **Taint Source 清单**：标记所有用户可控的输入点（文件/网络/CLI）及其数据类型。
3. **高价值目标排序**：按 RCE 潜力 > DoS 潜力 > 信息泄露排序的函数/代码块列表。
4. **攻击链草图**：从输入入口到最终影响的最短逻辑路径。
