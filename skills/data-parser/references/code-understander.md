---
description: 解析器代码理解 - 专家阅读解析库源码的直觉流
tags: [data-parser, code-understander, source-review, pointer-arithmetic, state-machine]
---

# 解析器代码理解 (Code Understander): 像黑客一样阅读源码

> 你不是在理解业务逻辑，你是在绘制**"数据如何操控内存与执行流"**的地图。
> 阅读解析器源码时，你的眼中只有指针、算术、递归深度和状态转换。

---

## 触发器：什么代码结构让专家立刻锁定关键区域？

### 触发器一：看到指针算术 (`ptr += ...`, `*ptr++`)

**你的第一眼反应**："这个指针的边界在哪里？谁保证它不越界？"

解析器核心是对输入 buffer 的线性扫描。所有漏洞的起点都是指针脱离了安全区域。

**立即追问链**：
1. `ptr` 的起始位置和结束边界（`end` 或 `size`）是否成对出现？
2. 每次 `ptr += n` 前，是否检查了 `ptr + n <= end`？
3. 如果长度字段来自输入，`n` 是否可能为负数（有符号整数下溢导致指针回退）？
4. 循环条件是基于 `while (ptr < end)` 还是基于输入声明的长度？两者是否一致？
5. 是否存在 `ptr[-1]` 这样的回溯访问？回溯前是否确认了 `ptr > start`？
6. 指针算术的类型是 `char*` 还是 `uint32_t*`？步长计算是否与数据类型匹配？

### 触发器二：看到状态机代码（`switch(state)`、`goto`）

**你的第一眼反应**："这个状态机是否穷尽所有可能的输入字节？是否存在非法状态转移？"

解析器本质上是有限状态机。状态转移缺失或错误会导致解析异常或内存问题。

**立即追问链**：
1. `switch` 是否有 `default` 分支？缺失 `default` 意味着非法字节可能引发未定义行为。
2. 状态转移表是否覆盖了所有 `(当前状态, 输入字节)` 组合？
3. 状态机是否使用 `goto` 实现？是否存在向后跳转导致无限循环？
4. 错误状态下是否正确清理了已分配的临时资源（buffer、句柄）？
5. 状态机是否嵌套（子状态机解析字符串/注释）？子状态机的错误是否被父状态机捕获？
6. 是否存在利用状态转换顺序绕过安全检查的可能？如先转入正常状态再转入危险状态。

### 触发器三：看到内存分配与释放逻辑

**你的第一眼反应**："谁分配，谁释放，中间出了错谁负责清理？"

解析器往往涉及大量临时 buffer 和对象树。内存管理错误直接导致崩溃或利用 primitives。

**立即追问链**：
1. 分配失败时，是否跳转到统一的错误清理标签？是否有遗漏的释放？
2. `realloc` 失败后，原指针是否仍然有效？还是已经丢失导致内存泄漏？
3. 解析对象树时，如果中间节点解析失败，子节点的内存是否被正确递归释放？
4. 是否存在 `alloca` 或变长数组（VLA）？输入控制的大小是否可能导致栈溢出？
5. 自定义内存池的分配器中，`free` 是否做了 double-free 检查？
6. 返回给调用者的对象，其内部引用的 buffer 是否仍由解析器管理？生命周期是否清晰？

### 触发器四：看到递归或调用栈管理

**你的第一眼反应**："递归深度由谁控制？调用栈的总消耗是多少？"

JSON/XML/YAML 解析天然需要递归处理嵌套。无限制的递归是栈溢出和 DoS 的直接原因。

**立即追问链**：
1. 递归函数的入口是否检查了全局或局部的深度计数器？
2. 深度计数器的数据类型是什么？`uint16_t` 是否可能被溢出绕过？
3. 每一层递归的栈帧大小是多少？估算最大安全深度：`stack_limit / frame_size`。
4. 是否存在相互递归（如 parse_object 调用 parse_value，parse_value 又调用 parse_object）？
5. 尾递归是否被编译器优化？若未优化，深层结构仍会导致栈溢出。
6. 是否使用显式栈（heap-allocated stack）替代调用栈？显式栈的大小是否有限制？

### 触发器五：看到编码处理路径（UTF-8、Unicode 转义）

**你的第一眼反应**："这段代码是在操作字节还是在操作字符？两个长度是否一致？"

编码处理是解析器中最容易出错的区域之一。字节索引与字符索引的错位会导致 OOB。

**立即追问链**：
1. UTF-8 解码是否验证了多字节序列的合法性？如 4 字节序列是否以 `11110xxx` 开头？
2. 解码失败时，是报错、跳过、还是替换？替换字符是否改变了长度计算？
3. `\uXXXX` 转义解码后，生成的码点是否验证了代理对范围（U+D800-U+DFFF）？
4. 编码转换的输出 buffer 大小是否按最坏情况（1 字节 → 4 字节 UTF-8）计算？
5. 规范化（NFC/NFD）是否在安全检查之后进行？规范化后是否产生新的攻击面？
6. 字符串截断逻辑是否按字节截断，导致多字节字符被切开？

---

## 攻击链闭合：从源码阅读到漏洞确认

```
[定位到 ptr += read_length(stream)]
    → [确认 read_length 返回值来自未校验的输入字段]
    → [发现指针检查仅存在于初始分支，后续分支缺失]
    → [构造输入使 ptr 越过 end 进入未映射内存或相邻堆块]
    → [触发 OOB Read/Write]
    → [利用信息泄露或堆元数据覆盖完成提权]
```

**闭合条件**：边界检查不完整 + 输入可控制指针偏移量 + 越界后存在高价值目标。

---

## 典型代码模式与警觉点

### 脆弱模式：不安全的指针推进
```c
// 脆弱：只检查一次，后续推进无边界约束
char *ptr = buffer;
char *end = buffer + len;
if (ptr >= end) return ERR;
uint32_t field_len = read_u32(&ptr);
// 注意：此时 ptr 已经移动了 4 字节，但 field_len 未被约束
memcpy(target, ptr, field_len);   // ← field_len 可能极大，ptr 可能越界
ptr += field_len;                 // ← 再次越界
```

### 安全模式：饱和边界检查
```c
// 安全：每次推进前校验剩余空间
char *ptr = buffer;
char *end = buffer + len;
if (ptr + 4 > end) return ERR;
uint32_t field_len = read_u32(&ptr);
if (ptr + field_len > end) return ERR;
memcpy(target, ptr, field_len);
ptr += field_len;
```

### 脆弱模式：缺失 default 的状态机
```c
// 脆弱：非法字节导致未定义行为
switch (state) {
    case STATE_KEY:
        if (c == '"') state = STATE_COLON;
        break;
    case STATE_VALUE:
        if (c == '"') state = STATE_END;
        break;
    // ← 没有 default，非法字节不做任何处理，状态机卡住或越界
}
```

### 安全模式：完备状态机
```c
// 安全：穷尽所有输入
switch (state) {
    case STATE_KEY:   /* ... */ break;
    case STATE_VALUE: /* ... */ break;
    default:
        return ERR_INVALID_STATE;
}
if (ptr >= end && state != STATE_COMPLETE) {
    return ERR_TRUNCATED;
}
```

### 脆弱模式：递归无深度限制
```c
// 脆弱：无限递归解析
void parse_json_value(json_parser *p) {
    if (*p->ptr == '{') {
        p->ptr++;
        while (*p->ptr != '}') {
            parse_json_value(p);  // ← 深度可达数百万，爆栈
        }
    }
}
```

### 安全模式：显式深度控制
```c
// 安全：深度限制与显式栈
int parse_json_value(json_parser *p, unsigned depth) {
    if (depth > MAX_DEPTH) return ERR_TOO_DEEP;
    if (*p->ptr == '{') {
        p->ptr++;
        while (*p->ptr != '}') {
            int r = parse_json_value(p, depth + 1);
            if (r < 0) return r;
        }
    }
    return 0;
}
```

---

## 输出要求

1. **关键代码段清单**：标注所有指针算术、分配逻辑、递归入口、状态转移的源码位置（文件:行号）。
2. **边界检查矩阵**：每个指针/缓冲区是否有前置校验、后置校验、饱和校验。
3. **状态机转移图**：如果存在状态机，画出简化版状态转移图，标注非法输入路径。
4. **资源生命周期图**：分配 → 使用 → 释放 的完整路径，标注可能的泄漏和 UAF 点。
