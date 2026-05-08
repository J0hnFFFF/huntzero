---
description: Binary 漏洞挖掘 - 追问链驱动的内存安全攻击链闭合
tags: [binary, vuln-hunter, stack-overflow, heap-overflow, uaf, format-string, integer-overflow, rop]
---

# Binary 漏洞挖掘 (Vuln Hunter): 追问链驱动的内存安全攻击链闭合

> 二进制漏洞不是 fuzzer 撞出来的，是推出来的。
> 你从内存布局出发，提出一连串无法被轻易否定的问题，
> 最终逼迫程序承认：是的，这个写入越界了；是的，这个指针已经死了还在用；
> 是的，这个返回地址可以被覆盖。

## 原理一：栈缓冲区溢出 —— 栈帧的边界失守

### 触发器
看到栈上的数组被不安全的字符串函数写入，或循环写入时边界来自用户输入。

### 追问链（不可绕过的 6 个问题）

1. **目标缓冲区在栈上的偏移量是多少？**
   - 使用 `rizin` 或 `pwndbg` 查看栈帧布局：`ebp - 0x100` 处的缓冲区到返回地址的距离
   - 如果距离 = 0x108（264 字节），那么输入 264 字节刚好覆盖到返回地址
   - 如果有 Stack Canary，Canary 在缓冲区和返回地址之间，需要先泄露或绕过

2. **覆盖源是否可控？**
   - `strcpy(dst, user_input)` → 源完全可控，但受限于 NULL 字节（`strcpy` 在 `\x00` 处停止）
   - `memcpy(dst, user_input, len)` → 如果 `len` 也可控，可以实现任意长度覆盖
   - `read(fd, buf, count)` → 如果 `count` 来自用户，可读取超过缓冲区大小的数据
   - `gets(buf)` → 完全不安全，可读取任意长度直到换行

3. **程序是否开启了 Stack Canary？**
   - `checksec` 查看 Canary 状态
   - 如果有 Canary：需要泄露 Canary 值，或找到不覆盖 Canary 的攻击路径（如劫持函数指针）
   - 如果没有 Canary：直接覆盖返回地址

4. **程序是否开启了 NX？**
   - NX enabled：栈不可执行，不能注入 shellcode 到栈上执行
   - 需要 ROP（返回导向编程）或 ret2libc
   - NX disabled：可以直接在栈上写入并执行 shellcode

5. **程序是否开启了 ASLR + PIE？**
   - ASLR + PIE enabled：代码和库的基地址随机化，ROP gadget 地址未知
   - 需要信息泄露（如格式化字符串、UAF 读取 GOT 表）获取基地址
   - ASLR disabled 或 PIE disabled：地址固定，可直接硬编码 gadget 地址

6. **是否存在更优雅的覆盖目标？**
   - 除了返回地址，栈上是否还有其他高价值目标？
   - `ebp`（栈帧指针）：控制 ebp 可以控制栈帧链，影响后续函数返回
   - 局部函数指针：某些函数在栈上声明了函数指针，覆盖它可在函数返回前劫持控制流
   - `argv` / `envp`：在某些特殊场景下，栈上的环境变量指针可被利用

### 攻击链闭合推演

```
用户输入 300 字节进入栈上 256 字节的缓冲区
    ↓
缓冲区溢出 44 字节：覆盖局部变量 + Canary(8字节) + 保存的 ebp(8字节) + 返回地址(8字节)
    ↓
如果没有 Canary：
    返回地址被覆盖为 ROP chain 的地址
    函数返回时，控制流跳转到 ROP chain
    ROP chain 执行：pop rdi; ret -> /bin/sh 地址; ret -> system@plt
    获得 shell

如果有 Canary：
    需要先泄露 Canary 值（通过格式化字符串或信息泄露漏洞）
    在覆盖返回地址时，将 Canary 原值写回正确位置
    然后覆盖返回地址
```

### 典型代码模式与警觉点

```c
// 🚨 高危：栈缓冲区溢出
void process_input(char *input) {
    char buf[256];
    strcpy(buf, input);  // 如果 input > 256，栈溢出
}

// 🚨 高危：gets 导致溢出
void vulnerable() {
    char buf[64];
    gets(buf);  // 完全不安全，已废弃
}

// 🚨 高危：memcpy 长度来自用户
void copy_data(int len, char *data) {
    char buf[128];
    memcpy(buf, data, len);  // 如果 len > 128，溢出
}

// 🚨 高危：循环边界来自用户
void fill_buffer(int count, char *data) {
    char buf[100];
    for (int i = 0; i < count; i++) {
        buf[i] = data[i];  // 如果 count > 100，溢出
    }
}
```

---

## 原理二：堆缓冲区溢出 —— 堆块的边界失守

### 触发器
看到堆分配的缓冲区被写入超过其分配大小的数据。

### 追问链（不可绕过的 5 个问题）

1. **分配大小是否来自用户输入？**
   - `malloc(user_size)` → 用户可控制分配大小
   - 如果 `user_size` 很大 → 可能导致内存耗尽（DoS）
   - 如果 `user_size` 经过整数溢出计算 → 可能分配远小于预期的大小

2. **写入长度是否与分配大小匹配？**
   - `malloc(64)` 后 `memcpy(buf, data, 128)` → 明显溢出
   - 更隐蔽：`malloc(strlen(input))` 后 `strcpy(buf, input)` → 如果 input 在分配后变化

3. **堆分配器的类型是什么？**
   - ptmalloc2（glibc 默认）：利用方式成熟（fastbin attack、unsorted bin attack、tcache poisoning）
   - jemalloc：利用方式不同，需要特殊技巧
   - Windows Heap：利用方式不同

4. **溢出的数据能否覆盖相邻堆块的元数据？**
   - ptmalloc2 的 chunk 元数据：`prev_size` + `size` + `fd` + `bk`
   - 覆盖 `size` 字段 → 可导致后续 `free` 操作错误地合并 chunk
   - 覆盖 `fd`/`bk` 指针 → 可导致 unsorted bin attack 或 tcache poisoning

5. **程序是否使用堆上的函数指针或 vtable？**
   - C++ 对象的 vtable 指针通常在对象开头（堆上）
   - 堆溢出覆盖 vtable 指针 → 劫持虚函数调用
   - 堆上是否有函数指针数组？覆盖后可在调用时跳转

### 攻击链闭合推演

```
用户控制输入长度，程序分配 64 字节缓冲区
    ↓
用户输入 128 字节，memcpy 写入 128 字节
    ↓
溢出 64 字节，覆盖相邻堆块的元数据
    ↓
相邻堆块的 size 字段被篡改 → free 时发生异常合并
    ↓
攻击者控制 fd/bk 指针 → 实现任意地址写（unsorted bin attack）
    ↓
覆盖 __free_hook 或 __malloc_hook → 后续 free/malloc 执行 shellcode
    ↓
获得 shell
```

### 典型代码模式与警觉点

```c
// 🚨 高危：堆分配大小来自用户
char *buf = malloc(user_size);
read(fd, buf, user_size);  // 如果 user_size 被整数溢出控制为 0，malloc(0) 返回小指针

// 🚨 高危：整数溢出导致分配不足
int total = count * sizeof(Item);  // 如果 count 很大，乘法溢出，total 变小
Item *items = malloc(total);
for (int i = 0; i < count; i++) {
    read(fd, &items[i], sizeof(Item));  // 写入远超分配大小
}

// 🚨 高危：strdup 后无长度校验
char *buf = strdup(user_input);  // 分配 strlen(user_input)+1
strcat(buf, suffix);  // 如果 suffix 较长，堆溢出
```

---

## 原理三：Use-After-Free —— 已死指针的还魂

### 触发器
看到 `free(ptr)` 后，ptr 或它的别名仍被解引用。

### 追问链（不可绕过的 5 个问题）

1. **ptr 被释放后是否被置 NULL？**
   - `free(ptr); ptr = NULL;` → 相对安全（虽然仍可能有 double-free 风险）
   - `free(ptr);` → 未置 NULL，后续 `if (ptr)` 判断仍会通过

2. **是否存在 ptr 的别名？**
   - `char *alias = ptr; free(ptr); strcpy(alias, ...)` → alias 成为悬空指针
   - 容器（链表、数组）中是否保存了 ptr 的副本？

3. **释放后、重用前是否有堆分配发生？**
   - UAF 的危害在于：释放的内存可能被重新分配给其他对象
   - 如果攻击者能控制新分配对象的内容 → 可以控制 UAF 时读取/写入的数据
   - 典型利用：释放一个字符串对象，然后分配一个同样大小的对象（如另一个字符串），新字符串的内容控制旧指针解引用时的行为

4. **被释放的对象类型是什么？**
   - 如果是 C++ 对象 → vtable 指针可被覆盖，虚函数调用被劫持
   - 如果是包含函数指针的结构体 → 函数指针可被覆盖
   - 如果是普通字符串 → 可用于信息泄露或控制流程

5. **程序是否有内存回收机制（如垃圾回收）？**
   - 手动管理（C/C++）：UAF 直接可利用
   - 引用计数：如果引用计数管理错误，可能导致提前释放或内存泄漏
   - 垃圾回收（Go/Rust）：UAF 通常不可利用，但可能存在 FFI 边界漏洞

### 攻击链闭合推演

```
程序分配 Object A（包含函数指针 callback）
    ↓
程序释放 Object A，但指针 ptr 未被置 NULL
    ↓
攻击者触发代码路径，分配 Object B（同样大小）
    ↓
堆分配器重用了 Object A 的内存给 Object B
    ↓
攻击者控制 Object B 的内容，覆盖了原来的 callback 指针
    ↓
程序通过 ptr 调用 callback（UAF）
    ↓
控制流跳转到攻击者指定的地址
    ↓
获得 shell
```

### 典型代码模式与警觉点

```c
// 🚨 高危：释放后未置 NULL
void process() {
    char *buf = malloc(64);
    read_input(buf);
    free(buf);
    // buf 未置 NULL
    if (buf[0] == 'A') {  // UAF！
        process_buf(buf);
    }
}

// 🚨 高危：容器缓存了原始指针
struct list_node *node = find_node(list, id);
free(node->data);  // 释放了数据
// 但 list 中仍保存着 node，node->data 仍是悬空指针
```

---

## 原理四：格式化字符串漏洞 —— 格式化函数的背叛

### 触发器
看到 `printf(user_input)`、`fprintf(file, user_input)` 等模式。

### 追问链（不可绕过的 5 个问题）

1. **格式参数是否来自用户可控输入？**
   - `printf(buf)` → 完全可控
   - `printf("%s", buf)` → 安全，格式参数固定
   - `syslog(LOG_INFO, buf)` → 同样危险

2. **程序是 32 位还是 64 位？**
   - 32 位：参数全部通过栈传递，格式化字符串利用简单直接
   - 64 位：前 6 个参数通过寄存器传递（RDI, RSI, RDX, RCX, R8, R9），格式化字符串需要更多技巧

3. **攻击者能读取多少栈内存？**
   - `%s`：读取栈上的指针指向的字符串
   - `%p`：读取栈上的指针值（可用于泄露地址）
   - `%x` / `%lx`：读取栈上的整数值
   - `%n`：向栈上的指针指向的地址写入已输出字符数（任意地址写）

4. **是否有 FORTIFY_SOURCE 保护？**
   - FORTIFY_SOURCE=2：某些 `printf` 调用被编译器替换为 `__printf_chk`，检查格式参数
   - 但不是所有调用都会被替换，间接调用（如函数指针）不会被替换

5. **利用目标是什么？**
   - 信息泄露：泄露 Canary、libc 地址、栈地址、PIE 基地址
   - 任意地址写：通过 `%n` / `%hn` / `%hhn` 写入 GOT 表、函数指针、返回地址

### 攻击链闭合推演

```
用户输入 "%p %p %p %p %p %p %p %p %p %p"
    ↓
printf(user_input) 执行
    ↓
输出栈上的 10 个指针值
    ↓
攻击者分析泄露的值：
    - 如果看到 0x7f... → 可能是 libc 地址 → 计算 libc 基地址
    - 如果看到 0x55... → 可能是 PIE 代码地址 → 计算 PIE 基地址
    - 如果看到高位随机值 → 可能是 Stack Canary
    ↓
攻击者构造更精确的 payload：
    - 写入目标：GOT 表中 free 的地址
    - 写入值：system 的地址
    - 使用 %n 或 %hhn 实现逐字节写入
    ↓
后续调用 free 时，实际执行 system("/bin/sh")
    ↓
获得 shell
```

### 典型代码模式与警觉点

```c
// 🚨 高危：格式化字符串完全可控
void log_message(char *msg) {
    printf(msg);  // 如果 msg 包含 %n，可向任意地址写入
}

// 🚨 高危：syslog 同样危险
void audit_log(char *event) {
    syslog(LOG_INFO, event);  // event 中的格式说明符被解释
}

// ✅ 安全：格式参数固定
void log_message_safe(char *msg) {
    printf("%s", msg);
}
```

---

## 原理五：整数溢出 —— 数学边界的幻影

### 触发器
看到长度计算、索引计算、大小字段涉及用户输入和整数运算。

### 追问链（不可绕过的 4 个问题）

1. **运算中是否存在有符号/无符号混用？**
   - `size_t len = (size_t)user_count * sizeof(Item);` → 如果 user_count 是 int 且为负数，转换为 size_t 后变成极大正数
   - `if (len > MAX_SIZE)` → 如果 len 是有符号负数，比较结果可能与预期相反

2. **乘法/加法是否可能溢出？**
   - `malloc(count * sizeof(Item))` → 如果 count = 0x40000000 且 sizeof(Item) = 4，乘积溢出为 0
   - `offset + length` → 如果两者都接近最大值，和溢出为很小的数

3. **截断是否导致信息丢失？**
   - `int len = user_provided_long;` → 如果 user_provided_long > INT_MAX，截断为负数
   - `short index = user_int;` → 截断后可能变为负数，数组索引越界

4. **溢出后的值是否被用于内存操作？**
   - 溢出后分配极小缓冲区 → 后续写入大量数据 → 缓冲区溢出
   - 溢出后索引为负数 → 数组越界访问栈/堆上的其他数据

### 典型代码模式与警觉点

```c
// 🚨 高危：乘法溢出
void *alloc_array(int count) {
    return malloc(count * sizeof(Item));  // count 大时溢出
}

// 🚨 高危：有符号/无符号混用
void copy(int len, char *src) {
    char buf[64];
    if (len < 64) {  // len 是有符号 int，如果是负数，条件成立！
        memcpy(buf, src, len);  // len 被转换为 size_t（极大正数），溢出
    }
}

// 🚨 高危：截断导致负数索引
void process(short index, int *data) {
    int value = data[index];  // index 为负数时，越界读取
}
```

---

## 高命中率漏洞 Pattern 速查表

| Pattern | 检测方式 | 典型目标 | 利用前提 |
|---------|---------|---------|---------|
| `strcpy`/`sprintf` 到栈缓冲区 | 反汇编/伪代码中搜索这些函数 | 所有接收字符串输入的程序 | 无 Canary 或 Canary 可泄露 |
| `malloc` 大小来自用户输入 | 搜索 malloc 调用，追踪 size 参数来源 | 解析复杂格式的程序 | 整数溢出或分配后大写入 |
| `free` 后未置 NULL | 搜索 free 调用，检查后续是否有解引用 | 使用复杂数据结构的管理程序 | 存在别名或容器缓存 |
| `printf(user_input)` | 搜索 printf 族函数调用 | 日志系统、错误处理 | 格式参数可控 |
| `memcpy`/`read` 长度来自用户 | 搜索这些函数，检查 len/count 参数 | 网络服务、文件解析器 | 长度 > 目标缓冲区 |
| `system`/`popen` 用户输入 | 搜索这些函数调用 | 处理外部命令的程序 | 命令注入过滤可被绕过 |
| 循环边界来自用户 | 搜索 for/while 循环条件 | 解析数组/列表的程序 | 边界值 > 缓冲区大小 |
| 有符号/无符号比较 | 搜索长度校验中的类型混用 | 所有涉及长度计算的地方 | 传入负数值 |
| vtable / 函数指针在堆上 | 搜索 C++ 类、回调函数指针 | 面向对象程序 | 存在堆溢出或 UAF |
| `gets` / `scanf("%s")` | 搜索这些已废弃的函数 | 旧代码、教学代码 | 几乎无前提，直接可利用 |

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [栈溢出/堆溢出/UAF/格式化字符串/整数溢出/命令注入]
- 漏洞函数: [函数名/地址]
- 根因: [描述代码中的错误假设]

### 脆弱路径
- 入口: [文件/网络/CLI/环境变量]
- 污点传播: [用户输入 → 哪个变量 → 哪个函数 → 哪个内存操作]
- 最终影响: [代码执行/信息泄露/DoS/权限提升]

### 利用条件
- 安全机制: [NX/ASLR/PIE/Canary/RELRO 状态]
- 利用难度: [直接覆盖返回地址/需要 ROP/需要信息泄露]
- 利用路径: [栈溢出→ROP / 堆溢出→house of xxx / UAF→vtable hijack]

### 验证思路
[如何构造输入触发崩溃，或使用什么 sanitizer 检测]
```
