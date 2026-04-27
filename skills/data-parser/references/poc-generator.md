---
description: 解析库 PoC 生成 - 一击必现的绞刑架
---

# PoC 生成器 (PoC Generator): 锻造微缩处决阵

为了让维护者认可这是一个致命问题而不是“理论可能性”，必须用一段毫无外部依赖的最小代码提供致命一击的复现。

## 专家级 PoC 的要求

1.  **脱水的 Fuzz Harness（测试架）模拟**：
    在 Python/Go/C++ 当中用短短数十行代码调用该受测解析器的入口：
    *   **步骤一**：直接硬编码你的恶魔 Payload（如超长嵌套字符串或变体二进制 Hex 转 byte[]）。
    *   **步骤二**：调用发生漏洞的函数（如 `JSON.parse()`, `proto.Unmarshal()` ）。
    *   **步骤三**：明确标注预期在这里，会收到一个令人汗毛直竖的表现形式（如操作系统丢出的 `Segmentation fault (core dumped)`，或者是 OOB Read 返回的长达数十 KB 带有乱码及其它进程内存碎片的错误数据）。

2.  **CLAIM CHAIN 必须要硬碰硬**：
    在提供指控链时，不得含糊其词：
    *   必须细化到行数（例如：`[file: parser.c:134] —— size calculation integer overflow caused by lack of INT_MAX check`）。
    *   必须清楚其从输入到内存出错的全程偏移传递。

## 你的任务
写出能拿给 C/C++/Java 底层工程师看一眼就会背后发凉的代码用例，干净利落地宣判程序的死亡机制。
