# 邮件系统 Fuzz Harness 生成指南

## 目标
为邮件系统的 C/C++ 解析器生成 libFuzzer harness。此模块负责接收邮件结构（MIME）或特定头字段。

## Harness 模板 (C/C++)

```c
#include <stdint.h>
#include <stddef.h>

// 包含目标解析器的头文件
// #include "mime_parser.h"

#ifdef __cplusplus
extern "C" {
#endif

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    // 1. 初始化目标解析器上下文
    // struct mime_context *ctx = mime_context_new();
    
    // 2. 将 fuzzer 数据喂给解析器
    // mime_parse(ctx, (const char *)data, size);
    
    // 3. 清理所有分配的资源
    // mime_context_free(ctx);
    
    return 0; // 必须返回 0
}

#ifdef __cplusplus
}
#endif
```

## 编译脚本模板 (build.sh)

```bash
#!/bin/bash
set -e

# 在这里写编译目标源码以及 fuzz harness 的指令。
# 你会被放在 /fuzz/job 目录执行。目标源码被挂载在 /fuzz/src 目录（只读）。
# 注意，目标可能很大，请尽可能只编译需要 fuzz 的那几个对应的 .c / .cc 文件。

# 示例:
# 1. 复制所需的源文件到当前可写目录
# cp /fuzz/src/mime_parser.c .
# cp /fuzz/src/mime_parser.h .

# 2. 编译为 fuzzer 二进制 (名为 fuzzer)
# ${CC} ${CFLAGS} -fsanitize=fuzzer,address -g -O1 mime_parser.c fuzz_harness.c -o fuzzer
```

## 关键原则

1. **绝对隔离**: harness 不能有网络/文件系统持久化副作用，必须保证完全状态还原。
2. **确定性**: 相同的输入必须产生相同的执行路径，不能依赖随机数种子。
3. **极简编译**: 在 `build.sh` 中尽量只挑选需要的源文件编译在一起，不要触发整仓 `./configure && make` 除非必须。
4. **ASan 敏感**: 不要为了解决内存泄漏而禁用 ASan 检测，而是应该在代码里正确释放；目标代码的内存泄漏是可以被接受的，如果你无法释放，请在 harness 中小心控制内存分配。
