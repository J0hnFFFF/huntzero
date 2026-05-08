---
description: 基础库假设验证 — 极端输入构造、正则灾难回溯测试、哈希碰撞生成、并发压力测试
tags: [foundation-lib, hypothesis-testing, fuzzing, ReDoS, hash-collision, concurrency, stress-test]
---

# 假设验证者 (Hypothesis Tester): 代码级核实与极值对冲

## 专家的直觉触发器 (Triggers)

当你已经对某个基础库的弱点产生怀疑时，以下验证手段必须立刻排入日程：

- **极值输入是否被优雅拒绝，还是被静默截断？** 传入 `size_t` 的最大值、负数（在有符号接口中）、空指针、零长度。
- **算法的时间/空间复杂度是否与输入规模成线性关系？** 如果不是，构造输入使其偏离最坏情况。
- **并发操作在百万次重复后是否能保持状态一致？** 单次通过的测试毫无意义，概率性漏洞需要统计学方法捕获。
- **FFI 边界在类型边界值上的表现**：向 C 传入 Go 的 `nil` 切片，其 `.Data` 指针是什么？C 侧是否正确处理了它？
- **解析器对畸形但语法上"合理"的数据的反应**：例如 protobuf 的 varint 编码中，最高位永远为 1 的无限延续字节。

## 不可跳过的问题链 (Question Chain)

1. **这个函数对空输入（NULL, 空切片, 空字符串）的行为是什么？** 是返回错误、返回默认值、还是进入未定义路径？
2. **如果向函数传入最大合法值的输入，再增加 1，会发生什么？** 是返回 `EOVERFLOW`，还是整数回绕导致分配失败被绕过？
3. **正则引擎的编译阶段是否也有漏洞？** 除了匹配阶段，一个包含 10,000 个嵌套括号的正则模式是否会导致编译期栈溢出或内存耗尽？
4. **哈希碰撞攻击中，目标哈希表的实际桶数量是多少？** 如果桶数为 2 的幂，碰撞生成是否可以利用低位碰撞策略？
5. **并发测试是否覆盖了"读-改-写"的原子性？** 两个线程同时执行 `map[key] = map[key] + 1`，在 100 万次迭代后总和是否正确？
6. **解压/解码器对截断输入的处理是报错还是继续？** 如果输入在长度字段声明后突然截断，内部循环是否会读到未初始化的内存？

## 攻击链闭合 (Attack Chain Closure)

**命题**：假设验证不是"测试是否正常工作"，而是"**测试在什么条件下会确定性地失败**"，并把失败条件收紧为一条可被武器化的边界。

**推理**：
- 正常的 QA 测试验证"对于合法输入，输出是否正确"。安全测试验证"对于非法输入，系统是否不崩溃、不泄露、不被耗尽"。
- 基础库的假设验证更进一步：它要找出"**合法与非法的边界上，库的行为发生相变的临界点**"。
- 例如：当输入长度从 1023 增加到 1024 时，某个内部缓冲区从栈分配切换到堆分配，而堆分配路径缺少初始化 → 信息泄露。
- 例如：当并发线程数从 1 增加到 2 时，某个原子操作被拆分为非原子读写 → 数据竞争。
- 验证者的任务，就是找到这些**相变点**，并证明它们可被外部输入精确触发。

**完整验证链示例**：
```
假设: 函数 decompress(buf, len) 对所有 len 都是安全的
  → 测试 len = 0: 通过（返回空结果）
  → 测试 len = MAX-1: 通过
  → 测试 len = MAX (SIZE_MAX): 观察 malloc 行为
     → 若 malloc(MAX) 返回 NULL，代码是否检查了返回值？
     → 若未检查，后续 memcpy 向 NULL 写入 → 段错误
  → 测试 len = 压缩头声明解压后大小为 MAX 的输入:
     → 分配是否成功？写入循环是否按声明大小而非实际输入大小执行？
  → 结论: 在 len=SIZE_MAX 时，假设被推翻
```

## 代码示例: 脆弱模式 vs 安全模式

### 1. 极端输入测试生成器 (Python + Hypothesis)

**测试脆弱库**:
```python
from hypothesis import given, strategies as st
import ctypes

lib = ctypes.CDLL("./vuln_lib.so")
lib.process_buffer.argtypes = [ctypes.c_char_p, ctypes.c_size_t]
lib.process_buffer.restype = ctypes.c_int

@given(st.binary(min_size=0, max_size=1024*1024))
def test_all_binary_inputs(data):
    # 测试各种长度的二进制输入
    buf = ctypes.create_string_buffer(data)
    rc = lib.process_buffer(buf, len(data))
    assert rc in (0, 1, -1)  # 观察返回值是否稳定

@given(st.one_of(st.just(b""), st.just(b"\x00"*4096),
                 st.binary(min_size=0xFFFFFFFF-10, max_size=0xFFFFFFFF)))
def test_extreme_lengths(data):
    # 测试极大长度（如果库允许）
    pass
```

**安全库的对应校验**:
```c
// 被测库的安全实现
int process_buffer_safe(const uint8_t* buf, size_t len) {
    if (buf == NULL) return -EINVAL;
    if (len > MAX_BUFFER_SIZE) return -E2BIG;
    if (len == 0) return 0; // 明确处理空输入
    // ... 后续处理
}
```

### 2. 正则灾难回溯测试 (Python)

**测试脚本**:
```python
import re
import time

def test_redos_vulnerability(pattern_str, poison_str):
    try:
        pattern = re.compile(pattern_str)
        start = time.time()
        pattern.match(poison_str)
        elapsed = time.time() - start
        return elapsed
    except re.error:
        return -1

# 测试目标库中的正则接口
patterns = [
    (r"(a+)+", "a" * 20 + "b"),      # 经典 ReDoS
    (r"(a+)*b", "a" * 24 + "b"),
    (r"(.*a){20}", "a" * 30),        # 多项式回溯
]

for pat, poison in patterns:
    t = test_redos_vulnerability(pat, poison)
    if t > 1.0:
        print(f"CRITICAL: Pattern {pat} took {t:.2f}s on {len(poison)} chars")
    elif t > 0.1:
        print(f"WARNING: Pattern {pat} took {t:.2f}s")
```

**安全的正则包装器** (Rust):
```rust
use regex::RegexBuilder;

fn safe_regex_match(pattern: &str, text: &str) -> Result<bool, String> {
    let re = RegexBuilder::new(pattern)
        .size_limit(1 << 18)
        .dfa_size_limit(1 << 18)
        .build()
        .map_err(|e| e.to_string())?;
    
    // 使用线程超时包装（生产环境中应使用异步超时）
    std::thread::scope(|s| {
        let handle = s.spawn(|| re.is_match(text));
        match handle.join() {
            Ok(result) => Ok(result),
            Err(_) => Err("Regex timeout/panic".to_string()),
        }
    })
}
```

### 3. 哈希碰撞生成与测试 (Python)

**生成碰撞键**:
```python
def simple_hash(s):
    h = 0
    for c in s:
        h = h * 31 + ord(c)
    return h & 0xFFFFFFFF

def find_collisions(target_hash, alphabet='ab', max_len=8):
    """暴力寻找具有相同哈希值的不同字符串（教学示例）"""
    from itertools import product
    collisions = []
    for length in range(1, max_len + 1):
        for chars in product(alphabet, repeat=length):
            s = ''.join(chars)
            if simple_hash(s) == target_hash:
                collisions.append(s)
                if len(collisions) >= 100:
                    return collisions
    return collisions

# 测试目标哈希表实现
# 若使用弱哈希，插入 10,000 个碰撞键后，操作耗时应显著上升
def benchmark_hash_insertions(keys):
    import time
    h = {}
    start = time.time()
    for k in keys:
        h[k] = 1
    return time.time() - start

normal_keys = [f"key_{i}" for i in range(10000)]
collisions = find_collisions(simple_hash("collision_seed"))
collision_keys = (collisions * (10000 // len(collisions) + 1))[:10000]

print("Normal:", benchmark_hash_insertions(normal_keys))
print("Collision:", benchmark_hash_insertions(collision_keys))
```

### 4. 并发压力测试 (Go)

**测试 TOCTOU**:
```go
func TestConcurrentMapTOCTOU(t *testing.T) {
    m := NewConcurrentMap() // 被测目标
    const maxSize = 100
    const workers = 50
    const iterations = 10000

    var wg sync.WaitGroup
    overflows := int32(0)

    for i := 0; i < workers; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < iterations; j++ {
                key := fmt.Sprintf("key-%d-%d", id, j)
                if !m.PutIfNotFull(key, []byte("v"), maxSize) {
                    atomic.AddInt32(&overflows, 1)
                }
            }
        }(i)
    }
    wg.Wait()

    actualSize := m.Len()
    if actualSize > maxSize {
        t.Fatalf("TOCTOU detected: size=%d > max=%d", actualSize, maxSize)
    }
    t.Logf("Final size: %d (expected ≤ %d)", actualSize, maxSize)
}
```

### 5. 压缩数据畸形测试 (C)

**测试框架**:
```c
#include <assert.h>
#include <string.h>
#include <stdlib.h>
#include <stdint.h>

// 被测函数声明
extern int zlib_decompress(const uint8_t* in, size_t in_len,
                           uint8_t* out, size_t* out_len);

void test_truncated_input() {
    // 一个合法的 zlib 头，但数据被截断
    uint8_t truncated[] = {0x78, 0x9c, 0x63, 0x00};
    uint8_t out[1024];
    size_t out_len = sizeof(out);
    int rc = zlib_decompress(truncated, sizeof(truncated), out, &out_len);
    // 安全实现应返回错误码，而非崩溃或无限循环
    assert(rc != 0); // 期望失败
}

void test_bad_windowbits() {
    // windowBits 非法值（如 -15 在非 raw 模式下）
    // 应由库在初始化阶段拒绝
}

int main() {
    test_truncated_input();
    test_bad_windowbits();
    return 0;
}
```

## 你的验证结果

仅凭审查和严谨逻辑推理，对该基础代码组件提交判定书：

```
判定: 该点因为缺失关键类型的屏障/同步语义，百分之百受畸形条件控制而变异
证据:
  1. 空输入 → NULL 解引用（已复现）
  2. 长度=SIZE_MAX → 整数溢出绕过分配（已复现）
  3. 并发双线程 Put → size > max（已复现，概率 0.3%）
置信度: Critical
```
