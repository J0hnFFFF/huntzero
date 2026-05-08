---
description: 基础库代码理解 — 解析 unsafe 块、指针运算、循环不变量、终止条件、分配路径、FFI 胶水
tags: [foundation-lib, code-reading, unsafe, pointer-arithmetic, loop-invariant, FFI, lifetime]
---

# 代码理解者 (Code Understander): 剖析物理合约的致命空窗

## 专家的直觉触发器 (Triggers)

阅读基础库源码时，以下视觉模式应像警报灯一样闪烁：

- **`unsafe { ... }` 块的出现频率**：在 Rust 中，每个 `unsafe` 块都是一份契约，声明"调用者已保证前置条件"。如果该块位于公共函数深处，而前置条件并未被外层校验，这就是定时炸弹。
- **指针运算的密度**：`ptr + offset`、`*(ptr + i)`、`*((T*)raw_bytes)`。每一行都在假设内存布局、对齐方式和生命周期的正确性。
- **循环中的终止条件依赖输入数据**：`while (*p != '\0')`、`for (i=0; i<hdr.len; i++)`。如果 `p` 指向的数据没有终止符，或 `hdr.len` 被伪造，循环就是无底洞。
- **分配路径上的 `goto` 或错误处理不完整**：某条错误分支忘记释放已分配内存，或某条成功分支重复释放。
- **FFI 胶水层的类型转换**：`C.String` → `*C.char`、`uintptr_t` → `unsafe.Pointer`。这些转换抹去了类型系统的保护。
- **`#[repr(C)]` 或 `packed` 结构体**：手动内存布局意味着编译器不再保证对齐。未对齐访问在 x86 上可能只是慢，在 ARM 上直接 `SIGBUS`。

## 不可跳过的问题链 (Question Chain)

1. **这个 `unsafe` 块内部，哪些指针被解引用？它们的生命周期由谁保证？** 如果指针来自外部输入，且代码未检查非空，NULL 解引用就在眼前。
2. **循环的终止条件是否完全由可信数据控制？** 如果终止条件依赖解析出的长度字段，而该字段本身未经校验，循环能否被设置为永不终止？
3. **这个函数中有多少条分配路径？** 每条路径上，是否有对应的释放操作？是否存在某条错误返回路径导致内存泄漏？
4. **指针运算中是否有潜在的整数溢出？** 例如 `(uint8_t*)base + offset * sizeof(T)`，如果 `offset` 来自外部且极大，加法可能环绕到更低的地址。
5. **FFI 层的结构体布局是否与 C 侧完全一致？** 如果 Rust/Go 侧的结构体添加了新字段而 C 侧未更新，或对齐方式不同，字段错位会导致静默的数据损坏。
6. **代码中是否有"借用"或"零拷贝"语义？** 返回的切片/指针是否依附于一个可能在调用者使用期间被修改或释放的内部缓冲区？

## 攻击链闭合 (Attack Chain Closure)

**命题**：理解基础库代码，就是逆向工程师的"契约逆向"——找出开发者认为"显然成立"却未写下来的条件，然后构造违反这些条件的输入。

**推理**：
- 所有高效的基础库都基于**隐式契约**运作。例如："调用者保证传入的缓冲区至少比输入大 N 字节""这个指针在函数返回前始终有效""输入字符串以 NULL 结尾"。
- 这些契约在文档中可能只有一行注释，或者什么都没有。
- 代码审查者的任务，是把每一条隐式契约显式化，并追问："**如果这条契约为假，会发生什么？**"
- 在 C/C++ 中，假契约 → UB。在 Rust `unsafe` 中，假契约 → 编译器已优化掉的内存安全检查被实际绕过。

**完整逻辑链示例**：
```
函数: fn parse_header(buf: &[u8]) -> &str
  → 内部: unsafe { std::str::from_utf8_unchecked(buf) }
  → 隐式契约: buf 总是合法的 UTF-8
  → 攻击者传入包含 0xFF 字节的 buf
  → from_utf8_unchecked 返回非法 UTF-8 的 &str
  → 后续代码将该 &str 传给假设 UTF-8 的字符串处理函数
  → 该函数使用 chars().next() 遍历，遇到非法序列行为未定义
  → 在 Rust 标准库中可能导致 panic，在下游 FFI 中可能导致崩溃
```

## 代码示例: 脆弱模式 vs 安全模式

### 1. Unsafe 块与指针算术 (Rust)

**脆弱模式**:
```rust
pub fn parse_u32s(data: &[u8]) -> Vec<u32> {
    let mut result = Vec::new();
    let ptr = data.as_ptr() as *const u32;
    let len = data.len() / 4;
    unsafe {
        for i in 0..len {
            result.push(*ptr.add(i)); // 假设 data.len() 是 4 的倍数
            // 如果 data 未对齐，未对齐读取 → UB
        }
    }
    result
}
```

**安全模式**:
```rust
pub fn parse_u32s_safe(data: &[u8]) -> Result<Vec<u32>, &'static str> {
    if data.len() % 4 != 0 {
        return Err("Length not aligned to 4");
    }
    if data.as_ptr() as usize % align_of::<u32>() != 0 {
        return Err("Pointer not aligned");
    }
    let len = data.len() / 4;
    let mut result = Vec::with_capacity(len);
    unsafe {
        // 前置条件已校验，可以安全读取
        let ptr = data.as_ptr() as *const u32;
        for i in 0..len {
            result.push(ptr.add(i).read_unaligned()); // 或使用已对齐的校验
        }
    }
    Ok(result)
}
```

### 2. 循环不变量与终止条件 (C)

**脆弱模式**:
```c
// 解析以 \0 结尾的字符串列表
void parse_strings(const uint8_t* data, size_t len) {
    const uint8_t* end = data + len;
    while (data < end) {
        printf("%s\n", (const char*)data);
        // 如果 data 中没有 \0，strlen 将越界读到 end 之外
        data += strlen((const char*)data) + 1;
    }
}
```

**安全模式**:
```c
void parse_strings_safe(const uint8_t* data, size_t len) {
    const uint8_t* end = data + len;
    while (data < end) {
        const uint8_t* nul = memchr(data, '\0', end - data);
        if (!nul) {
            fprintf(stderr, "Unterminated string\n");
            break;
        }
        printf("%.*s\n", (int)(nul - data), (const char*)data);
        data = nul + 1;
    }
}
```

### 3. 分配路径与生命周期 (C++)

**脆弱模式**:
```cpp
std::vector<uint8_t> decompress(const uint8_t* in, size_t in_len) {
    size_t out_len = peek_size(in); // 从输入头读取声明大小
    uint8_t* buf = new uint8_t[out_len]; // 可能 new(0) 或极大值失败抛异常
    if (!decompress_impl(in, in_len, buf, out_len)) {
        // 错误分支忘记 delete[] buf → 内存泄漏
        return {};
    }
    std::vector<uint8_t> result(buf, buf + out_len);
    delete[] buf;
    return result;
}
```

**安全模式**:
```cpp
std::vector<uint8_t> decompress_safe(const uint8_t* in, size_t in_len) {
    size_t out_len = peek_size(in);
    if (out_len > MAX_DECOMPRESS_SIZE) return {};
    std::vector<uint8_t> result(out_len); // RAII 管理内存
    if (!decompress_impl(in, in_len, result.data(), result.size())) {
        return {}; // result 自动释放，无泄漏
    }
    return result;
}
```

### 4. FFI 胶水层布局错位 (Rust ↔ C)

**脆弱模式**:
```rust
#[repr(C)]
pub struct Packet {
    pub flags: u8,
    pub length: u32, // C 侧 struct { uint8_t flags; uint32_t length; };
    // Rust 默认无 packed，编译器会在 flags 后插入 3 字节 padding
    // C 编译器在特定平台也可能插入 padding，如果假设一致...
}

#[no_mangle]
pub extern "C" fn process_packet(ptr: *const Packet) {
    unsafe {
        let pkt = &*ptr;
        let slice = std::slice::from_raw_parts(
            (ptr as *const u8).add(5), // 假设 1+4=5 偏移，但 padding 可能使其为 8
            pkt.length as usize
        );
    }
}
```

**安全模式**:
```rust
#[repr(C, packed)] // 显式 packed，与 C 侧保持一致
pub struct Packet {
    pub flags: u8,
    pub length: u32,
}

#[no_mangle]
pub extern "C" fn process_packet_safe(ptr: *const Packet) {
    if ptr.is_null() { return; }
    unsafe {
        let pkt = ptr.read_unaligned(); // packed 结构需 unaligned 读取
        if pkt.length > MAX_PACKET_LEN { return; }
        // 使用 offsetof 宏生成的常量，而非硬编码偏移
        let data_ptr = (ptr as *const u8).add(offset_of!(Packet, length) + 4);
    }
}
```

### 5. 零拷贝与借用陷阱 (Go)

**脆弱模式**:
```go
type Parser struct {
    buf []byte // 内部缓冲区
}

func (p *Parser) NextToken() []byte {
    // 返回 buf 的子切片，调用者持有引用
    tok := p.buf[:p.findDelim()]
    p.buf = p.buf[len(tok)+1:] // 推进内部指针
    // 如果调用者保存了 tok，而 Parser 被 Reset 重用缓冲区 → tok 指向无效内存
    return tok
}
```

**安全模式**:
```go
func (p *Parser) NextToken_safe() []byte {
    delim := p.findDelim()
    // 返回拷贝，断开与内部缓冲的生命周期绑定
    tok := make([]byte, delim)
    copy(tok, p.buf[:delim])
    p.buf = p.buf[delim+1:]
    return tok
}
// 或在 API 文档中明确声明: "返回的切片仅在下次 Parse 调用前有效"
```

## 你的目标输出

精准揭露这套看似自洽的库代码里，那层隐形的、假设调用方必定"善良而正确"的纸糊的护甲。输出格式：

```
文件: src/core/parser.c:145
模式: while(*ptr) { ... ptr++; }
隐式契约: ptr 指向的缓冲区在数据结束处有终止符
破坏方式: 传入截断数据（无终止符）
后果: 读取越界，信息泄露或段错误
安全重写建议: 使用 memchr/memchr 限定搜索范围
```
