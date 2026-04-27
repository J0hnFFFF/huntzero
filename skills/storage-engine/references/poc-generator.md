---
description: 存储引擎 PoC 生成 - 一击崩盘的底层执行铁证
---

# PoC 生成器 (PoC Generator): 锻造崩塌现实的代码实锤

如果只是讲虚幻的堆布局，防守者往往借口难度太高而抵赖。你必须用一个毫无外部依赖的 C/C++/Go 极小调用端测试来宣示系统的彻底猝死。

## 专家级 PoC 的要求

1.  **极简的致死测试体 (Minimal Crash Harness)**：
    不需连接复杂的网络，用原生语言搭建包含出事方法的简单调用脚手架。
    *   **注入层**：填装刚刚构造由于长度或边界引发溢出计算的假 Block 数据，甚至直接调取其具有漏洞的分区 API `Arena.Allocate(...)` 或者 `Iterator.Seek(...)`。
    *   **落地证实层**：当编译器跑过这几行代码时，随附预期必然引发的系统级暴乱截图：例如收到系统无情的 `Segmentation fault (core dumped)` 或者 `AddressSanitizer: heap-buffer-overflow` 的血红字样，宣告该处护城的绝对溃败。

2.  **指明漏洞的生命流失终点**：
    精确把控生命流程追踪路径，明确指示：`[file: arena.cc:alloc_size -> overflow caused wrap around -> memcpy over-write previous struct]` 证明这是一条不容置疑的绝路！

## 你的任务
编写这段干净利落、令人汗毛倒竖的原生底层触发样例，犹如拿在手中敲碎最加固保险箱密码机的万能解码实锤，呈交出无法辩驳的安全死刑。
