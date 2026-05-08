---
description: 基础库漏洞挖掘 — 缓冲区溢出、ReDoS、哈希碰撞、整数溢出、TOCTOU、FFI 边界
tags: [foundation-lib, vuln-hunting, buffer-overflow, ReDoS, hash-collision, integer-overflow, TOCTOU, FFI]
---

# 漏洞猎手 (Vuln Hunter): 输入宇宙辐射数据源

## 专家的直觉触发器 (Triggers)

看到以下代码模式时，你的狩猎本能应当瞬间激活：

- **缓冲区操作链**：`malloc(n)` → `memcpy(dst, src, n)`，其中 `n` 是用户输入的算术结果。只要算术链中有 `+`、`*`、`-` 而无饱和检查，这就是整数溢出 → 堆溢出的标准模板。
- **回溯型正则**：嵌套量词 `(x+)*`、`(a|aa)*` 配合由外部控制的模式字符串。输入端每增加一个字符，匹配路径翻倍。
- **哈希表插入循环**：`for (auto& k : user_keys) map[k] = v;`。如果 `map` 使用弱哈希（如 DJB2、CRC32），且用户知晓哈希算法，碰撞键可使 `O(1)` 退化为 `O(N²)`。
- **并发路径上的 `if (ptr)` 后再 `use(ptr)`**：在检查和使用之间，如果 `ptr` 可能被另一个线程释放，这就是 UAF/Double-Free 的温床。
- **FFI 边界上的指针传递**：Go 的 `C.CString` 生成的内存谁释放？Rust 的 `unsafe` 块里对 C 结构体的字段假设是否对齐？

## 不可跳过的问题链 (Question Chain)

1. **这个函数中，由外部输入参与计算的第一个算术操作是什么？** 它是加法、乘法还是减法？结果是否可能溢出或下溢？
2. **如果向正则引擎传入一个 100 字符的恶意模式，再传入 100 字符的匹配文本，最坏情况下的回溯次数是多少？** 是否超过了 CPU 在 1 秒内能处理的步数？
3. **哈希函数是否是公开已知的？** 攻击者能否离线生成具有相同哈希值的 10 万个不同字符串？插入这些字符串后，单次查询或插入的 CPU 消耗是多少？
4. **在并发容器的 `size()` 返回后、实际写入前，是否存在锁释放？** 另一个线程能否在这段时间内修改结构，使之前的检查失效？
5. **FFI 调用中，哪一方的内存分配器被使用？** C 分配的内存被 Go/Rust 释放，或反之，是否会导致堆元数据损坏？
6. **压缩库的解压逻辑中，声明的解压后大小与实际写入大小是否被分别校验？** 如果声明大小为 4GB，实际只写入 1KB，后续代码是否会按 4GB 继续操作？

## 攻击链闭合 (Attack Chain Closure)

**命题**：基础库的漏洞猎杀，本质上是"在输入空间中构造一个反例，使所有隐式假设同时失效"。

**推理**：
- 每个库函数内部都有一组"开发者认为永远为真"的假设。例如：`buf != NULL`、`len > 0 && len < MAX`、`ptr->magic == VALID_MAGIC`。
- 这些假设在代码中往往以**缺失的校验**形式存在，而不是显式的断言。
- 攻击者的任务是构造一个输入，它满足 API 的语法要求（能通过最外层的解析），却违反最深层的隐式假设。
- 当这个反例到达无防护的代码区域时，UB（未定义行为）被触发。在 C/C++ 中，UB 可能被编译器优化为"不可能发生"，从而**静默删除安全检查**。

**完整攻击链示例：哈希碰撞 DoS**
```
攻击者选择目标服务（如一个使用自定义哈希表的 RPC 框架）
  → 逆向/阅读源码，确认哈希算法为弱哈希（如加法哈希）
  → 本地离线计算生成 10,000 个碰撞键（如 "aaa", "baa", "caa" 等具有相同哈希值）
  → 向服务发送包含这些键的批量请求
  → 服务端哈希表退化为链表，插入/查询变为 O(N²)
  → 单次请求 CPU 占用从微秒级升至秒级
  → 并发请求下线程池耗尽，服务整体不可用
```

## 代码示例: 脆弱模式 vs 安全模式

### 1. 缓冲区溢出：整数溢出引导的堆分配不足 (C)

**脆弱模式**:
```c
typedef struct {
    uint32_t width, height;
    uint8_t  bpp;
} BitmapHeader;

uint8_t* decode_bitmap(const uint8_t* raw, size_t len) {
    BitmapHeader* h = (BitmapHeader*)raw;
    // 乘法溢出: width=0x4000, height=0x4000, bpp=4
    // 乘积 = 0x100000000 → uint32_t 溢出为 0
    uint32_t size = h->width * h->height * h->bpp;
    uint8_t* buf = malloc(size); // malloc(0) 或极小值
    memcpy(buf, raw + sizeof(BitmapHeader), h->width * h->height * h->bpp);
    // 向极小堆块写入巨量的数据 → 堆溢出
    return buf;
}
```

**安全模式**:
```c
uint8_t* decode_bitmap_safe(const uint8_t* raw, size_t len) {
    if (len < sizeof(BitmapHeader)) return NULL;
    BitmapHeader h;
    memcpy(&h, raw, sizeof(h)); // 避免对齐问题
    
    uint64_t size = (uint64_t)h.width * h.height * h.bpp;
    if (size > SIZE_MAX || size > len - sizeof(BitmapHeader)) {
        return NULL; // 溢出或尺寸不匹配
    }
    uint8_t* buf = malloc((size_t)size);
    if (!buf) return NULL;
    memcpy(buf, raw + sizeof(BitmapHeader), (size_t)size);
    return buf;
}
```

### 2. ReDoS：指数级回溯 (Python)

**脆弱模式**:
```python
import re

def sanitize_username(username):
    # 用户提供的 username 被直接嵌入正则
    pattern = re.compile(rf"^({re.escape(username)}+)+$")
    # 若 username 为 "(a+)+"，匹配文本为 "aaaaab"
    return pattern.match("aaaaaab") is not None
```

**安全模式**:
```python
import re

def sanitize_username_safe(username, text):
    # 绝不将用户输入作为正则语法的一部分
    literal = re.escape(username)
    # 使用固定复杂度模式，或禁止嵌套量词
    pattern = re.compile(rf"^{literal}$")
    # 增加超时（Python 3.11+ 可用 regex 模块的超时功能）
    return pattern.match(text) is not None
```

### 3. 哈希碰撞 DoS (Go)

**脆弱模式**:
```go
// 自定义简单哈希表，用于反序列化用户数据
type SimpleMap struct {
    buckets [][]pair
}

func (m *SimpleMap) hash(s string) uint32 {
    var h uint32
    for i := 0; i < len(s); i++ {
        h = h*31 + uint32(s[i]) // 弱哈希，易碰撞
    }
    return h % uint32(len(m.buckets))
}
```

**安全模式**:
```go
import "crypto/sha256"

func (m *SafeMap) hash(s string) uint64 {
    // 使用密码学强哈希或 SipHash，且密钥随机化
    sum := sha256.Sum256([]byte(s))
    return binary.BigEndian.Uint64(sum[:8]) % uint64(len(m.buckets))
}
// 更优: 直接使用 Go 内置 map，它自带哈希随机化和碰撞防护
```

### 4. TOCTOU：并发写放大 (C++)

**脆弱模式**:
```cpp
class BufferPool {
public:
    char* acquire() {
        std::lock_guard<std::mutex> lk(mtx);
        if (!pool.empty()) {
            char* p = pool.back();
            pool.pop_back();
            return p; // 返回后由调用者使用
        }
        return new char[4096];
    }
    void release(char* p) {
        std::lock_guard<std::mutex> lk(mtx);
        if (pool.size() < max_size) {
            pool.push_back(p);
        } else {
            delete[] p; // 问题：acquire 与 release 的配对由调用者保证
        }
    }
private:
    std::vector<char*> pool;
    std::mutex mtx;
    size_t max_size = 100;
};
// 如果线程 A acquire 后，线程 B 错误地 release 了不属于池的指针 → 双重释放
```

**安全模式**:
```cpp
class BufferPool_safe {
public:
    std::shared_ptr<char> acquire() {
        std::lock_guard<std::mutex> lk(mtx);
        if (!pool.empty()) {
            auto p = std::move(pool.back());
            pool.pop_back();
            return p;
        }
        return std::shared_ptr<char>(new char[4096],
            [](char* p){ delete[] p; });
    }
    void release(std::shared_ptr<char> p) {
        std::lock_guard<std::mutex> lk(mtx);
        if (pool.size() < max_size) {
            pool.push_back(std::move(p)); // 所有权转移
        }
        // 超出容量时，shared_ptr 离开作用域自动释放
    }
private:
    std::vector<std::shared_ptr<char>> pool;
    std::mutex mtx;
    size_t max_size = 100;
};
```

### 5. FFI 边界断裂：Go → C (Go)

**脆弱模式**:
```go
// #cgo LDFLAGS: -lcustom
// #include <custom.h>
import "C"
import "unsafe"

func Process(data []byte) {
    // C.CString 在 C 堆分配，需要 C.free
    cstr := C.CString(string(data))
    // 忘记调用 C.free(cstr) → 内存泄漏
    // 若 data 包含 \0，C 字符串提前终止，C 侧按 strlen 处理出错
    C.process_data(cstr)
}
```

**安全模式**:
```go
func Process_safe(data []byte) error {
    if len(data) == 0 { return fmt.Errorf("empty data") }
    // 显式传递长度，不依赖 C 字符串的 \0 终止
    cptr := C.CBytes(data) // 返回 void*，需手动 free
    defer C.free(cptr)
    C.process_data_with_len(cptr, C.size_t(len(data)))
    return nil
}
```

## 你的目标输出

产出具体的利用场景剧本。不是笼统地说"可能溢出"，而是像数学公式验算般精确：

```
变量 A (用户输入 width) = 0x4000
变量 B (用户输入 height) = 0x4000
变量 C (bpp) = 4
运算: A * B * C = 0x100000000
类型: uint32_t → 溢出为 0
后果: malloc(0) 返回最小 chunk (通常 0x10 或 0x20)
写入量: 0x100000000 字节
结果: 堆溢出，覆写下一个 chunk 的 size/prev_inuse/fd/bk
可信度: 100% 可复现
```
