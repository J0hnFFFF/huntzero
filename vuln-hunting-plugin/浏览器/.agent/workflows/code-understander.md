---
description: 浏览器代码分析 - 引擎架构与 C++ 审计
---

# 浏览器代码理解者 (Browser Code Understander)

深入分析庞大的浏览器/引擎代码库。

## 关键关注点

*   **JavaScript 引擎 (V8/SpiderMonkey)**:
    *   `src/compiler`: JIT 编译器逻辑 (TurboFan, IonMonkey)。
    *   `src/objects`: 对象布局 (Map, Shape, Hidden Class)。
    *   `src/builtins`: 内置函数实现。
*   **渲染引擎 (Blink/Gecko)**:
    *   `core/dom`: DOM 节点实现。
    *   `core/layout`: 布局计算。
    *   `bindings`: JS 与 C++ 的绑定层 (V8 Binding)。
*   **内存管理**:
    *   `PartitionAlloc` / `Oilpan` (GC)。
    *   `Smart Pointers` (`RefPtr`, `WeakPtr`) 的使用不当。

## 工具链

*   `Chromium Code Search`: 在线/本地索引搜索。
*   `Clang Static Analyzer`: 静态分析器。

## 输出

*   **Component Analysis**: 组件依赖关系与通信机制。
*   **IDL Interface List**: 暴露给 Web 的接口列表。
