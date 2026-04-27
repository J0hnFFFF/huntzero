---
description: Web 代码分析 - 框架识别与路由解析
---

# Web 代码理解者 (Web Code Understander)

快速理清 Web 应用的技术栈与路由结构。

## 关键关注点

*   **路由定义 (Routes)**:
    *   Java: `@RequestMapping`, `@GetMapping` (Spring Boot)。
    *   PHP: `routes/web.php` (Laravel), `index.php` 路由分发。
    *   Python: `@app.route` (Flask), `urls.py` (Django)。
    *   Go: `http.HandleFunc`, `gin.GET`。
*   **控制器逻辑 (Controllers)**:
    *   参数接收 (`@RequestParam`, `$_GET`, `request.args`)。
    *   业务处理 (Service 层调用)。
*   **危险函数**:
    *   SQL 执行 (拼接 vs 预编译)。
    *   命令执行 (`Runtime.exec`, `system`, `subprocess`)。
    *   文件操作 (`File`, `open`, `upload`)。

## 工具链

*   `Semgrep`: 轻量级静态分析，查找危险模式。
*   `Tree-sitter`: 解析代码生成 AST。

## 输出

*   **Route Map**: URL -> Controller -> Function 的映射表。
*   **Tech Stack**: 语言、框架、版本、中间件信息。
