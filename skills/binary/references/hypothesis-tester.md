---
description: Binary 假设验证 - 静态推演与动态验证的双轨方法
tags: [binary, hypothesis-tester, static-analysis, dynamic-debugging, fuzzing, sanitizer]
---

# Binary 假设验证 (Hypothesis Tester): 静态推演与动态验证的双轨方法

> 二进制漏洞的验证有两种武器：静态推演和动态执行。
> 静态推演让你在脑内走通每一条代码路径；动态执行让程序自己承认错误。
> 最好的验证是两者交叉印证。

## 验证方法论：三段式逻辑推演 + 动态验证

每个假设必须经得住：
1. **前提条件是否成立？**（攻击者能否控制输入 X？）
2. **传递机制是否畅通？**（X 能否到达危险函数 Y？）
3. **影响结果是否达成？**（Y 的执行是否导致 Z？）
4. **动态验证**：用实际输入或 sanitizer 证明假设成立

---

## 假设一：栈缓冲区溢出可利用性证明

### 前提条件验证

**问题**：攻击者能否向栈缓冲区写入超过其容量的数据？

**静态推演步骤**：
1. 在反汇编/伪代码中找到目标函数
2. 确定栈帧布局：缓冲区大小、与返回地址的距离
3. 追踪输入数据到缓冲区的路径

```bash
# 使用 rizin 查看栈帧
rizin -A ./target
[0x00000000]> s sym.process_input
[0x00000000]> afvn
[0x00000000]> afvb
# 查看局部变量和缓冲区大小

# 使用 pwndbg 动态查看
pwndbg> disassemble process_input
pwndbg> break *process_input
pwndbg> run
pwndbg> stack 50
# 查看栈上的缓冲区位置和返回地址
```

**逻辑证明**：
```
前提 P1: 函数在栈上分配了 64 字节缓冲区（sub rsp, 0x40）
前提 P2: 缓冲区通过 strcpy(buf, user_input) 写入
前提 P3: user_input 来自 recv(fd, buf, 0x1000)，最大 4096 字节
前提 P4: 无 Stack Canary（checksec 显示 Canary: No）

推导：
  user_input 长度可达 4096 字节
  strcpy 复制直到 NULL 字节
  64 字节缓冲区可被 4096 字节输入完全覆盖
  返回地址在缓冲区 + 72 字节处（64 字节缓冲 + 8 字节对齐）
  攻击者可以覆盖返回地址为任意值

结论: 栈缓冲区溢出可利用性被证明
```

**动态验证**：
```bash
# 构造测试输入
python3 -c "import sys; sys.stdout.buffer.write(b'A'*100)" > payload

# 运行程序并观察崩溃
./target < payload
# 如果程序 segfault 在 0x41414141 ('AAAA') → 返回地址被覆盖

# 使用 GDB 确认
pwndbg> run < payload
pwndbg> info registers rip
# rip = 0x41414141 → 控制流劫持成功
```

---

## 假设二：堆缓冲区溢出可利用性证明

### 前提条件验证

**问题**：攻击者能否向堆缓冲区写入超过其分配大小的数据？

**静态推演步骤**：
1. 找到 `malloc(size)` 调用，确定 size 的来源
2. 找到后续向该缓冲区写入的操作
3. 比较写入长度与分配大小

```bash
# rizin 中搜索 malloc 调用
[0x00000000]> /ad call malloc
[0x00000000]> pd 20 @ sym.process_data
# 查看 malloc 参数和后续 memcpy/read 调用
```

**逻辑证明**：
```
前提 P1: malloc(user_size) 分配堆内存
前提 P2: user_size 来自网络包的 length 字段，最大为 0xFFFFFFFF
前提 P3: 程序执行 memcpy(buf, packet_data, packet_length)
前提 P4: packet_length 也来自网络包，可能不等于 user_size

推导：
  如果攻击者发送 length=8 的包头，但 packet_length=1024
  malloc(8) 分配 8 字节（实际 chunk 大小为 0x20 含元数据）
  memcpy 写入 1024 字节
  覆盖相邻堆块元数据和用户数据

结论: 堆缓冲区溢出可利用性被证明
```

**动态验证**：
```bash
# 编译时开启 AddressSanitizer
clang -fsanitize=address -g target.c -o target_asan

# 运行测试
./target_asan < payload
# ASAN 会报告 heap-buffer-overflow

# 或使用 AFL++ 进行自动化 fuzzing
afl-fuzz -i in/ -o out/ ./target @@
# 观察 crashes 目录中是否出现 heap-buffer-overflow
```

---

## 假设三：Use-After-Free 可利用性证明

### 前提条件验证

**问题**：已释放的内存是否仍被程序解引用？

**静态推演步骤**：
1. 找到 `free(ptr)` 调用
2. 追踪 `ptr` 在后续代码路径中的使用
3. 检查 `ptr` 是否被置 NULL

```bash
# rizin 中搜索 free 调用并追踪后续使用
[0x00000000]> /ad call free
[0x00000000]> axf @ sym.free_callsite
# 查看 free 后哪些代码路径仍引用该指针
```

**动态验证**：
```bash
# 使用 ASAN 检测 UAF
clang -fsanitize=address -g target.c -o target_asan
./target_asan < uaf_trigger_payload

# ASAN 输出示例：
# ERROR: AddressSanitizer: heap-use-after-free
# READ of size 8 at 0x602000000010 thread T0
#     #0 0x555555555234 in process_data target.c:45
#     #1 0x5555555553a1 in main target.c:78
```

---

## 假设四：格式化字符串漏洞可利用性证明

### 前提条件验证

**问题**：`printf` 的格式参数是否来自用户可控输入？

**静态推演步骤**：
1. 找到所有 `printf` 族函数调用
2. 确定第一个参数（格式字符串）的来源
3. 如果是栈/堆上的缓冲区，追踪其内容来源

```bash
# rizin 中搜索 printf 调用
[0x00000000]> /ad call printf
[0x00000000]> pd 5 @ sym.printf_callsite
# 查看 rdi 寄存器（第一个参数）的值从哪里来
```

**动态验证**：
```bash
# 构造格式化字符串测试输入
echo '%p %p %p %p %p %p %p %p' | ./target
# 如果输出包含栈地址（如 0x7ffe... 或 0x55...）→ 格式化字符串漏洞存在

# 进一步测试写入能力
echo 'AAAA%8$n' | ./target
# 如果程序崩溃或行为异常 → %n 可利用

# 使用 GDB 观察
pwndbg> run
%p %p %p %p %p %p %p %p
# 查看输出中是否泄露了 Canary、libc 地址、栈地址
```

---

## 假设五：整数溢出可利用性证明

### 前提条件验证

**问题**：整数运算是否可能导致缓冲区分配不足？

**静态推演步骤**：
1. 找到涉及用户输入的整数运算
2. 分析运算结果是否可能溢出
3. 追踪运算结果是否被用于 `malloc` 或数组索引

```c
// 伪代码中的警觉模式
int total = count * sizeof(Item);  // count 来自用户
char *buf = malloc(total);         // total 可能溢出为负数/小正数
```

**动态验证**：
```bash
# 构造触发整数溢出的输入
# 如果 sizeof(Item) = 8，count = 0x20000000
# total = 0x20000000 * 8 = 0x100000000 → 32 位 int 溢出为 0
# malloc(0) 返回小指针，后续写入大量数据导致溢出

python3 -c "
import struct
import sys
count = 0x20000000
sys.stdout.buffer.write(struct.pack('<I', count))
sys.stdout.buffer.write(b'A' * 1024)
" > payload

./target < payload
# 观察是否崩溃或 ASAN 报告
```

---

## 专家推演工具箱

### 静态分析工具链

```bash
# 快速查看文件信息和安全机制
checksec --file=./target
readelf -h ./target        # 文件头
readelf -s ./target | grep -E "strcpy|sprintf|gets|scanf|system|exec"  # 危险函数

# 反汇编和伪代码
rizin -A ./target          # 自动分析
r2 -A ./target             # 同上（rizin 的旧名）
# 在 rizin 中：
# s sym.main              # 跳转到 main 函数
# pdf                     # 显示函数反汇编
# pdc                     # 显示伪代码
# axf @ sym.vuln_func     # 查看交叉引用

# Ghidra 无头模式（批量分析）
analyzeHeadless /tmp/ghidra_project project_name \
  -import ./target \
  -postScript /path/to/script.py
```

### 动态调试工具链

```bash
# GDB + pwndbg
gdb ./target
pwndbg> start              # 启动并在入口暂停
pwndbg> break *vuln_func   # 在漏洞函数设置断点
pwndbg> run < payload      # 运行并传入 payload
pwndbg> stack 30           # 查看栈上 30 个 QWORD
pwndbg> heap               # 查看堆状态（pwndbg heap 命令）
pwndbg> telescope $rsp 20  # 查看 rsp 指向的内存

# ltrace / strace（观察库调用和系统调用）
ltrace ./target < payload  # 观察 strcpy、malloc、free 等调用
strace ./target < payload  # 观察 read、write、mmap 等系统调用
```

### Fuzzing 工具链

```bash
# AFL++
afl-cc target.c -o target_afl  # 使用 AFL 编译器插桩
mkdir in out
afl-fuzz -i in/ -o out/ ./target_afl @@

# LibFuzzer（函数级 fuzzing）
clang -fsanitize=fuzzer,address fuzz_harness.cc target.o -o fuzzer
./fuzzer -max_total_time=300

# Honggfuzz
honggfuzz -P -i in/ -- ./target ___FILE___
```

### Sanitizer 配置

```bash
# AddressSanitizer (ASAN): 检测内存错误（溢出、UAF、双重释放）
clang -fsanitize=address -g target.c -o target_asan

# MemorySanitizer (MSAN): 检测未初始化内存使用
clang -fsanitize=memory -g target.c -o target_msan

# UndefinedBehaviorSanitizer (UBSAN): 检测整数溢出、移位异常等
clang -fsanitize=undefined -g target.c -o target_ubsan

# ThreadSanitizer (TSAN): 检测数据竞争
clang -fsanitize=thread -g target.c -o target_tsan
```

## 输出要求

1. **三段式逻辑证明**：前提条件 → 传递机制 → 影响结果，每步标注"成立/不成立/需验证"
2. **静态分析证据**：反汇编/伪代码片段，标注关键指令和内存地址
3. **动态验证命令**：可直接执行的 gdb/ltrace/fuzzing 命令
4. **反证思考**：在什么条件下这个假设不成立？（如：开启了相关安全机制）
