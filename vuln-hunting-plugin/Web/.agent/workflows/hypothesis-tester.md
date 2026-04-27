---
description: 假设验证 - 快速验证假设过滤假阳性
---

# Web 假设验证 (Hypothesis Tester)

快速验证关于 Web 应用行为的猜想。

## 验证目标

在不进行完整攻击的情况下，验证：
1.  **逻辑猜想**: "如果我删除 Cookie 里的 token，依然能访问这个接口吗？"
2.  **注入猜想**: "如果我在 id 参数后加个单引号，响应不仅是 500 报错，而且内容长度会变吗？"
3.  **条件猜想**: "这个上传接口是否真的只检查了文件后缀，而没检查 MIME type？"

## 验证方法

### 1. 响应差异分析 (Response Diffing)
*   **状态码**: 200 vs 403 vs 500。
*   **长度/时间**: 响应包大小的变化或处理时间的显著延迟（Time-based Blind）。
*   **DOM树**: 页面结构是否发生了非预期的变化。

### 2. 无害化探测 (Benign Probing)
*   使用 `sleep(1)` 代替 `drop table`。
*   使用 `<h1>test</h1>` 代替 `<script>alert(1)</script>`。

## 输出格式
```
## 验证报告

### 假设
[描述假设]

### 验证过程
- 工具: [Frida/GDB]
- 脚本: [Script snippet]
- 观察: [Log output]

### 结论
[Validated / Refuted]
```