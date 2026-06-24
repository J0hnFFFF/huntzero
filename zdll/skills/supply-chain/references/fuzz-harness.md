---
description: 供应链系统 Fuzz Harness 生成指南
tags: [supply-chain, fuzz, libfuzzer, harness]
---

# 供应链系统 Fuzz Harness 生成指南

## 目标
为包管理器（`npm/yarn/pip` 的 C/C++ 扩展）、项目构建工具或依赖解析器生成 libFuzzer harness。主要寻找在解析恶意的 `.json/toml/yaml` 文件格式或者恶意的长文件名时，出现的内存溢出及逻辑越界行为。

## Harness 模板 (C/C++)

```c
#include <stdint.h>
#include <stddef.h>

// 包含目标构建解析器的头文件
// 比如解析 package.json 的 C addon 或者是 yaml/toml 解析库
// #include "dep_parser.h"

#ifdef __cplusplus
extern "C" {
#endif

// libFuzzer 的核心入口
int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    // 供应链库通常需要以文件路径或者 buffer 形式传参。
    // 如果必须以文件形式传递可以通过往 /tmp 下的固定文件里写入
    // 但推荐直接寻找到接受 buffer 参数的函数 (如 parse_toml_buffer)
    
    // 初始化隔离的虚拟机/解析器上下文
    // struct parser_ctx *ctx = init_parser_context();
    
    // 执行真正的解析作业
    // parse_dependency_file(ctx, (const char *)data, size);
    
    // 必须清理，禁止产生任何残留以防影响接下来的千百万次迭代
    // free_parser_context(ctx);
    
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

# 示例:
# 1. 如果这是一个 npm 的 C++ addon，通常使用 node-gyp。
# 但对于 fuzz，我们要跳过 JS 壳子，直接将核心 C/C++ 模块用 clang 编起来。
# cp /fuzz/src/src/parser.cpp .
# cp /fuzz/src/include/parser.h .

# 2. 编译为 fuzzer 二进制 (名为 fuzzer)
# ${CXX} ${CXXFLAGS} -fsanitize=fuzzer,address,undefined -g -O1 -I. parser.cpp fuzz_harness.cpp -o fuzzer
```

## 关键原则

1. **剔除解释器**: 如果供应链目标包含 Python/Node 层，不能直接 Fuzz Python VM。必须在代码树中找到实际负责处理字节和解析依赖图的 **Native C/C++ 核心代码**并单独作为 lib 链入。
2. **结构化变形要求**: 很多供应链解析器要求有基础的 JSON / YAML 括号格式才会进入深层处理。如果 Drone 可以同时输出 `corpus/seed.json` 供 fuzzer 参考，效果会呈指数级提升。
3. **无系统残留副作用**: 禁止解析过程中修改真正的磁盘（例如进行真实的 `git clone`）。应当对那些外连模块进行 Mock 掉或者使用纯离线解析函数。
