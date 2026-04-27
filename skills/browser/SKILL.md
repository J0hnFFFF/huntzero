---
description: 浏览器引擎领域情报简报 (V7 Domain Intelligence Brief)
---

# 浏览器引擎地形情报 (Browser Engine Domain Brief)

> [!IMPORTANT]
> V7 模式：本文件是 **领域地形知识**，不是执行步骤。
> Cerebrum 读取后应基于第一性原理自主决定分析路径。

## 浏览器引擎的本质结构

浏览器引擎是一类极度复杂的**多进程状态机**系统，其核心特征是：

- **主要 Invariant**：沙箱隔离保证渲染进程对主机资源无直接访问权。任何跨进程资源访问都必须通过严格的 IPC 信道（Mojo/IPDL）以权限校验完成。
- **核心状态**：对象生命周期（GC Epoch）、JIT 编译管道状态（Typer/Optimizer 阶段）、DOM 树结构（Layout 计算状态）
- **核心转换**：JS 执行推动对象图变化，事件回调在 DOM 操作期间插入异步转换，IPC 消息在进程边界传递状态

## 已知的结构性地形

### 1. JIT 编译管道（类型系统与优化假设边界）
- **Typer 阶段**：编译器对变量类型作出假设，若运行时类型违反这些假设但未被 DCHECK 捕获，将进入未定义状态
- **Range Analysis**：对整数范围的假设可能在特定输入下产生越界的数组访问
- **优化阶段内联**：函数内联后，原本分离的对象在同一范围内，生命周期假设需同步更新

### 2. 对象生命周期（GC 与手动引用计数边界）
- **GC 时间点**：JS 回调在 GC 期间触发时，被 GC 标记回收的对象可能仍被 C++ 引用
- **Blink 对象绑定**：C++ 侧与 V8 JS 侧各有独立引用计数，双侧计数归零时机不同步时产生悬空引用
- **DOM 操作与事件回调交织**：布局计算期间触发的 JS 回调可能将 DOM 树置于不一致的中间态

### 3. 跨进程 IPC 边界（Mojo 接口状态机）
- **接口状态顺序**：受信端（Browser Process）对 Mojo 接口的消息期望有特定顺序，若渲染进程违反顺序仍被接受则是状态机漏洞
- **指针传递语义**：通过 Mojo 传递的句柄和接口指针，在接收端的合法性校验是否完整？

## 已知的 Invariant 失效模式

| Invariant 声明 | 常见失效原因 | 表现区域 |
|----------------|-------------|---------|
| "JIT 类型假设与运行时一致" | 特定 JS 模式绕过类型反馈机制 | TurboFan Range Analysis |
| "GC 对象存活期内引用有效" | C++ 侧在 GC trigger 点保留了裸指针 | Blink/WebCore DOM 操作 |
| "沙箱内无直接系统调用" | Mojo 接口校验逻辑缺失 | Browser Process Mojo handler |
| "JS 无法访问进程外对象" | 通过 SharedArrayBuffer 侧信道泄露地址 | 内存信息泄露场景 |

## 目标代码定位参考

```
# JIT 相关（V8）
src/compiler/typer.cc
src/compiler/turbofan-graph-verifier.cc

# DOM / Blink 对象生命周期
third_party/blink/renderer/core/

# Mojo IPC 接口
*.mojom 文件 + 对应 *_impl.cc
```

## 标准分析产物路径
- 发现写入：`.audit_notes.md`
- 崩溃现场：工作目录 crash dump
- 验证脚本：`local_workspace/` 下的 HTML/JS PoC
