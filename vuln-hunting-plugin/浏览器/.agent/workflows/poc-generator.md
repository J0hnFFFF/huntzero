---
description: 方案生成 - 生成完整 POC
---

# 浏览器 POC 生成器 (POC Generator)

生成用于触发 Crash 或验证 R/W 原语的 HTML 文件。

## 交付标准

1.  **极简主义**: 移除所有不必要的 CSS/JS，只保留触发逻辑。
2.  **原语证明**: 最好能展示 AddrOf/FakeObj 的效果，而不仅仅是 Crash。

## 模板 (HTML/JS)

```html
<html>
<body>
<script>
// V8 Exploit Helper Functions
function gc() { /* ... */ }

function trigger() {
    // 1. Setup Objects
    let arr = [1.1, 2.2, 3.3];
    
    // 2. Trigger JIT Optimization
    for (let i = 0; i < 10000; i++) {
        opt_func(arr);
    }
    
    // 3. Trigger Bug
    // ...
}

trigger();
</script>
</body>
</html>
```