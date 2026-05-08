---
description: 解析器假设测试 - 验证解析器安全假设的实验方法
tags: [data-parser, hypothesis-tester, fuzzing, malformed-input, boundary-testing]
---

# 解析器假设测试 (Hypothesis Tester): 用畸形输入验证安全边界

> 你的假设不是猜出来的，是**构造输入、观察行为、推翻假设**试出来的。
> 每个安全声明（"我们限制了深度"、"我们检查了长度"）都是待证伪的靶子。

---

## 触发器：什么场景让测试专家立刻设计实验？

### 触发器一：看到"已修复整数溢出"的声明

**你的第一眼反应**："真的修复了吗？在 32 位和 64 位下表现一致吗？"

整数溢出修复往往只覆盖了最明显的情况，边缘案例（如 `SIZE_MAX/2 + 1`）或类型转换链中仍可能存在问题。

**立即追问链**：
1. 修复代码使用的是 `if (a > SIZE_MAX / b)` 还是有符号整数比较？
2. 是否存在多层计算？如 `count * size + header_size`，整体是否做了饱和检查？
3. 32 位编译时和 64 位编译时的行为是否一致？攻击者能否强制 32 位模式？
4. 如果乘法使用 `calloc(n, size)`，某些 libc 的 calloc 内部是否仍可能溢出？
5. 长度字段为 0 或最大值时，后续逻辑是否进入特殊分支导致跳过校验？
6.  fuzzing 时是否覆盖了 `0x7FFFFFFF`、`0x80000000`、`0xFFFFFFFF` 等魔法值？

### 触发器二：看到"已限制嵌套深度"的声明

**你的第一眼反应**："深度限制是在哪一层做的？绕过路径存在吗？"

深度限制可能在解析器入口处有效，但通过引用/别名、预处理展开或某些特殊语法可能绕过。

**立即追问链**：
1. 深度计数器是全局的还是每棵语法树的？并发解析时是否共享计数器？
2. 别名引用（YAML `&a [*a]`）或实体展开（XML `<!ENTITY>`）是否在深度检查之后？
3. 字符串转义是否被计入深度？如 `\\[` 是否被误判为嵌套开始？
4. 如果深度限制为 N，构造 N+1 层时解析器是优雅报错还是崩溃？
5. 深度限制是否可通过 API 参数或环境变量被外部覆盖？
6. 在达到限制前的最后一层，解析器是否会分配大量临时对象导致内存耗尽？

### 触发器三：看到"已禁用自动类型反序列化"的声明

**你的第一眼反应**："黑名单还是白名单？有没有默认允许的内部类？"

安全修复往往采用黑名单，而黑名单在 Java/Python 等语言中几乎总是可绕过。

**立即追问链**：
1. 反序列化配置是白名单还是黑名单？白名单是否使用字符串前缀匹配？
2. 是否允许数组类型？如 `[Lcom.sun.rowset.JdbcRowSetImpl;` 是否能绕过类名检查？
3. 是否支持内部类？`Outer$Inner` 的检查是否与普通类一致？
4. 是否使用了 `ObjectInputStream.resolveClass` 的自定义实现？是否调用了父类实现？
5. 某些框架（如 Jackson）的默认类型推断（`defaultTyping`）是否仍在特定条件下生效？
6. 是否存在通过 `java.lang.invoke` 或 `Proxy` 类动态生成的类型绕过检查？

### 触发器四：看到"已处理编码错误"的声明

**你的第一眼反应**："处理方式是拒绝、替换还是截断？每种方式都安全吗？"

编码错误处理往往引入新的长度错位或状态异常。

**立即追问链**：
1. 遇到无效 UTF-8 序列时，解析器是报错、跳过字节、还是插入替换字符（U+FFFD）？
2. 如果插入替换字符，逻辑长度增加了但原 buffer 不变，后续长度计算是否脱节？
3. `\uXXXX` 转义解码为代理对时，是否允许孤立的高代理或低代理？
4. 编码转换失败时，错误码是否被忽略？如 `iconv` 返回 `-1` 但程序继续运行。
5. 字符串截断逻辑是否在编码验证之前？截断是否切开了多字节字符？
6. 是否测试了过长的合法编码序列？如 UTF-8 中 6 字节编码 U+0000（非标准但某些解码器接受）。

### 触发器五：看到"使用安全 API（如 safe_load）"的声明

**你的第一眼反应**："safe 的具体定义是什么？是否仍允许某些危险构造？"

API 名称中的 "safe" 往往是相对概念。需要明确其禁止列表和实际行为。

**立即追问链**：
1. `safe_load` 禁止了哪些标签/类型？是否仍允许本地文件引用或自定义标签？
2. 如果禁止了 `!!python/object`，是否也禁止了 `!!python/name` 或 `!!python/module`？
3. 是否存在通过多文档流（`---` 分隔）在第二个文档中注入危险构造？
4. 解析后的对象是否包含可执行的代码片段（如 lambda 表达式字符串）？
5. 是否允许自定义解析器（Custom Loader/Resolver）？这些扩展点是否继承了安全限制？
6. 通过模糊测试（AFL/libFuzzer）运行 24 小时后，是否发现了解析器崩溃或内存异常？

---

## 攻击链闭合：从假设到证伪的完整实验流程

```
[假设：解析器已安全处理大长度字段]
    → [构造输入：length=0xFFFFFFFF, body=4 bytes]
    → [观察行为：崩溃 / 内存飙升 / 优雅报错]
    → [如果崩溃：定位崩溃点，确认是整数溢出还是 OOB]
    → [如果内存飙升：确认是否无限分配或缺少长度约束]
    → [如果报错：构造边界值 length=SIZE_MAX/2, length=SIZE_MAX/2+1 再次测试]
    → [记录最小触发输入（MTE）]
```

**闭合条件**：实验可复现 + 输入最小化 + 崩溃点与源码缺陷对应。

---

## 典型代码模式与警觉点

### 脆弱测试场景：边界值未覆盖
```python
# 脆弱：测试只覆盖了正常值
def test_parse_length():
    assert parse_length(b"\x00\x00\x00\x10") == 16   # 只测了正常值
    # 未测：0x7FFFFFFF, 0x80000000, 0xFFFFFFFF
```

### 安全测试场景：系统化边界测试
```python
# 安全：覆盖所有边界和魔法值
BOUNDARY_VALUES = [
    0, 1, 0x7F, 0x80, 0xFF,
    0x7FFF, 0x8000, 0xFFFF,
    0x7FFFFFFF, 0x80000000, 0xFFFFFFFF,
    0x7FFFFFFFFFFFFFFF, 0x8000000000000000, 0xFFFFFFFFFFFFFFFF
]
def test_parse_length_boundaries():
    for val in BOUNDARY_VALUES:
        payload = struct.pack(">I", val) + b"A" * min(val, 1024)
        result = parse_length(payload)
        assert result in (OK, ERR_OVERFLOW, ERR_TRUNCATED)
```

### 脆弱测试场景： fuzzing 覆盖率不足
```bash
# 脆弱：只跑了 5 分钟，未触发深层代码路径
./fuzzer -max_total_time=300 corpus/
# 深层嵌套和别名展开需要特定语料种子才能到达
```

### 安全测试场景：结构化 fuzzing
```python
# 安全：使用结构化 fuzzer（如 libprotobuf-mutator）生成语法合法但语义畸形的输入
def FuzzJSON(data):
    try:
        obj = json.loads(data)
        # 额外验证：检查解析后的对象大小是否在合理范围
        assert compute_size(obj) < MAX_OBJ_SIZE
    except json.JSONDecodeError:
        pass  # 非法输入应被正常拒绝
```

### 脆弱测试场景：未验证递归限制
```python
# 脆弱：假设 1000 层足够安全
def test_nesting():
    data = "[" * 1000 + "]" * 1000
    parse(data)  # 但攻击者可能通过 alias 绕过
```

### 安全测试场景：绕过路径测试
```python
# 安全：测试绕过路径
def test_alias_bypass():
    data = "&a [" * 5000 + "]" * 5000 + "\n*b: *a"
    # 测试 alias 引用是否能绕过独立的深度计数器
    with pytest.raises(ParseError):
        yaml.load(data, Loader=yaml.Loader)
```

---

## 输出要求

1. **测试矩阵**：每个安全声明对应的输入构造策略与预期结果。
2. **边界值清单**：整数、长度、深度、编码相关的系统化测试值集合。
3. **Fuzzing 配置**：语料种子构造方法、字典文件、覆盖目标。
4. **崩溃报告模板**：输入（hexdump）、崩溃地址、栈回溯、最小化后的 PoC。
