---
description: 基础库目标定义 — 锁定加密、压缩、正则、并发容器、序列化等核心攻击面
tags: [foundation-lib, target-surface, crypto, compression, regex, concurrency, serialization, ffi]
---

# 目标定义者 (Target Definer): 标记承压中枢结构

## 专家的直觉触发器 (Triggers)

当你面对一个声称"高性能""零拷贝""通用"的基础库时，以下信号应让每一根神经立刻紧绷：

- **看到 `unsafe` / `extern "C"` / `cgo` / `#[no_mangle]`**：边界即深渊。跨语言调用的翻译层是误差的放大器。
- **看到 `memcpy(dst, src, len)` 且 `len` 来自外部输入**：长度参数是生死状。一旦 `len` 的计算链上有任何截断或溢出，这就是经典的堆/栈溢出温床。
- **看到正则表达式被编译后缓存复用**：如果正则本身来自用户配置，`(a+)+` 这种嵌套量词就是一枚定时炸弹。
- **看到哈希表作为公共 API 的输入容器**：若键类型是字符串且哈希算法非密码学强哈希，碰撞攻击可将其退化为准链表。
- **看到 `while(*ptr)` 或 `for(;;)` 且无显式步进上限**：终止条件就是生命线。对畸形输入而言，`ptr` 可能永不为零。
- **看到 `size_t` 或 `usize` 参与乘法后再分配**：`width * height * channels` 的整数溢出会导向微小的堆分配与巨大的写入。

## 不可跳过的问题链 (Question Chain)

1. **这个库的公共 API 中，哪些函数直接接收原始字节流或长度参数？** 长度参数在进入库体后，是否经历了任何 `int`→`size_t`、`uint32_t`→`int` 的转换？
2. **库内部是否存在从用户输入推导出的循环终止条件？** 例如解析协议头中的长度字段，再按该字段循环读取。如果该字段被篡改为 `0xFFFFFFFF`，循环会做什么？
3. **库的并发容器（如线程安全队列、跳表、无锁哈希）在 `size()` 和 `push()` 之间是否存在可被抢占的窗口？** 这个窗口是否足以让两个线程同时通过容量检查，却只有一个能安全写入？
4. **库是否暴露了对正则表达式的编译或匹配接口？** 用户能否控制正则字符串本身？如果可以，是否提供了超时或回溯深度限制？
5. **压缩/解压接口在处理字典大小、窗口大小、块大小时，是否对非法组合进行了拒绝？** 例如 `zlib` 的 `windowBits` 为负数时的不同语义，是否被调用者正确理解？
6. **序列化库（protobuf/msgpack）在解析未知字段或嵌套深度时，是否有预算上限？** 一个 1MB 的嵌套 msgpack 数组是否能在解析时耗尽栈空间？

## 攻击链闭合 (Attack Chain Closure)

**命题**：一个基础库的目标定义，本质上是定义"输入契约的破坏路径"。

**推理**：
- 基础库的特点是**无差别服务**——它不假设调用者的善恶，却只假设调用者的"正确"。
- 当攻击者作为"错误调用者"入场时，所有隐式契约（"这里不会传 NULL""这个长度总是合法的"）全部崩塌。
- 因此，目标定义的终点不是"找到了一个 memcpy"，而是："**从公共 API 的哪一个参数出发，经过哪一段无校验的计算链，最终触发了哪一个未定义行为（UB）或资源耗尽点。**"

**示例闭合**：
```
入口: parse_packet(buf, len) 
  → len 被强转为 uint16_t (截断)
  → 截断后的 len 用于 malloc(len + header_size) (整数溢出)
  → 得到 8 字节的堆块
  → memcpy(dst, buf, original_len) (原始长度写入)
  → 堆溢出覆写相邻 chunk metadata
  → 下一步 free() 时控制 RIP
```

## 代码示例: 脆弱模式 vs 安全模式

### 1. 加密库接口: OpenSSL 风格的长度陷阱 (C)

**脆弱模式**:
```c
// 截断漏洞：len 是 int，但外部传入 size_t
int EVP_CipherUpdate(EVP_CIPHER_CTX *ctx, unsigned char *out,
                     int *outl, const unsigned char *in, int inl) {
    // 如果 inl 来自 (int)user_len，而 user_len > INT_MAX，截断为负数
    // 内部可能将负数视为极大正数处理，或导致后续计算下溢
    if (!ctx->encrypt) {
        memcpy(ctx->buf + ctx->buf_len, in, inl); // inl 为负时灾难
    }
}
```

**安全模式**:
```c
int EVP_CipherUpdate_safe(EVP_CIPHER_CTX *ctx, unsigned char *out,
                          int *outl, const unsigned char *in, size_t inl) {
    if (inl > INT_MAX / 2) {
        ERR_raise(ERR_LIB_EVP, EVP_R_TOO_LARGE);
        return 0;
    }
    // 所有长度计算使用 size_t，仅在最后检查边界
    size_t needed = ctx->buf_len + inl;
    if (needed > ctx->buf_size) { /* realloc 或报错 */ }
    memcpy(ctx->buf + ctx->buf_len, in, inl);
}
```

### 2. 压缩库接口: 字典大小与整数溢出 (C++)

**脆弱模式**:
```cpp
bool ZstdDecompress(const char* src, uint32_t src_len, char* dst, uint32_t dst_cap) {
    // 攻击者传入 src_len = 0xFFFFFFFF, dst_cap = 0
    // 内部计算 uncompressed_size = ReadLE32(src); // 用户控制的伪造头
    // 若 uncompressed_size > dst_cap，但未检查即写入
    return ZSTD_decompress(dst, dst_cap, src, src_len) == 0;
}
```

**安全模式**:
```cpp
bool ZstdDecompress_safe(const char* src, size_t src_len, std::vector<char>& dst) {
    if (src_len < 4) return false;
    uint32_t uncompressed_size = ReadLE32(src);
    if (uncompressed_size > ZSTD_MAX_INPUT_SIZE) return false; // 硬性上限
    dst.resize(uncompressed_size); // vector 会处理分配失败
    size_t rc = ZSTD_decompress(dst.data(), dst.size(), src + 4, src_len - 4);
    return !ZSTD_isError(rc);
}
```

### 3. 正则引擎接口: ReDoS 暴露面 (Rust)

**脆弱模式**:
```rust
pub fn match_user_pattern(text: &str, pattern: &str) -> bool {
    let re = regex::Regex::new(pattern).unwrap(); // 用户控制 pattern
    re.is_match(text) // (a+)+ 对 aaaaaab 可造成指数级回溯
}
```

**安全模式**:
```rust
use regex::RegexBuilder;

pub fn match_user_pattern_safe(text: &str, pattern: &str) -> Result<bool, &'static str> {
    let re = RegexBuilder::new(pattern)
        .size_limit(1 << 20)   // 编译期大小限制
        .dfa_size_limit(1 << 20)
        .build()
        .map_err(|_| "Invalid regex")?;
    // 对极长输入增加超时或长度截断
    if text.len() > 10_000 { return Err("Input too long"); }
    Ok(re.is_match(text))
}
```

### 4. 并发容器: TOCTOU 窗口 (Go)

**脆弱模式**:
```go
type ConcurrentMap struct {
    mu sync.Mutex
    m  map[string][]byte
}

func (c *ConcurrentMap) PutIfNotFull(key string, val []byte, max int) {
    c.mu.Lock()
    if len(c.m) < max { // 检查...
        c.mu.Unlock()
        // ...此处被抢占，另一个线程也通过了检查
        c.mu.Lock()
        c.m[key] = val // 两个线程同时写入，超出预期容量
    }
    c.mu.Unlock()
}
```

**安全模式**:
```go
func (c *ConcurrentMap) PutIfNotFull_safe(key string, val []byte, max int) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    if len(c.m) >= max {
        return false
    }
    c.m[key] = val // 检查与操作在同一临界区
    return true
}
```

### 5. 序列化: 深度炸弹 (Python)

**脆弱模式**:
```python
import msgpack

def parse_request(data):
    # 默认 unpack 无递归深度限制
    return msgpack.unpackb(data, raw=False) 
    # 攻击者发送 100 万层嵌套数组，导致 CPython 栈溢出 (RecursionError 或段错误)
```

**安全模式**:
```python
import msgpack

def parse_request_safe(data):
    return msgpack.unpackb(
        data, raw=False,
        strict_map_key=True,       # 禁止非字符串键
        max_array_len=10000,
        max_map_len=10000,
        max_str_len=10_000_000,
        max_bin_len=10_000_000,
        max_ext_len=10_000_000
    )
```

## 你的目标输出

直接输出该基础库工程的所有高危点坐标（文件:行号）与暴露的本质弱点，将其连成一片随时可供数学打击的高价值战略点分布图。对每个点标注：
- **入口函数**
- **危险参数**
- **转换/计算链路**
- **最终崩溃/可利用类型**
