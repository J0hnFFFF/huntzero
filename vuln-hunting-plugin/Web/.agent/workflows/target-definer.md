---
description: Web 目标定义 - 敏感端点与业务流程
---

# Web 目标定义者 (Web Target Definer)

筛选出最具攻击价值的 Web 业务端点。

## 识别策略

1.  **高危功能**:
    *   **上传**: 文件上传接口 (Potential Shell Upload)。
    *   **查询**: 搜索框、高级查询 (Potential SQLi)。
    *   **执行**: 插件安装、脚本运行 (Potential RCE)。
    *   **管理**: `/admin`, `/console`, `/api/system`。
    *   **⚠️ 导入/导出**: **ZIP/JSON/YAML/CSV 导入** (极易被忽视的高危面，参考 Ghost 路径遍历案例)。
    *   **配置管理**: 路由配置、主题上传、重定向配置等 (可能触发代码执行)。
2.  **业务流程**:
    *   **支付**: `order`, `pay`, `callback`。
    *   **认证**: `login`, `register`, `forget_password`, `oauth`。
3.  **数据流向**:
    *   用户输入直接进入 sink (如 SQL 查询、HTML 渲染) 的路径。

## 优先级排序

*   **P0**: 未授权 RCE / SQL注入 / 任意文件读写。
*   **P1**: 越权操作核心数据 / 存储型 XSS。
*   **P2**: 反射型 XSS / 信息泄露 / CSRF。

## 输出

*   **Attack Verification Plan**: 针对每个高危端点的具体验证计划。
*   **Coverage Report**: 覆盖的 API 数量与比例。
