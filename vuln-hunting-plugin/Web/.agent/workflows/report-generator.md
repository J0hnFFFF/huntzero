---
description: 报告生成 - 合成专业报告
---

# Web 分析报告 (Report Generator)

## 报告结构

### 1. 概览
*   应用指纹 (CMS版本/中间件)
*   测试覆盖的端点数
*   发现的高危逻辑漏洞

### 2. 漏洞详情
*   **Request/Response**: 完整的 HTTP 交互数据包截图/文本。
*   **Business Impact**: 具体的业务影响（如：导致任意用户密码重置）。
*   **Reproduction**: 详细的点击流或 cURL 命令。

### 3. 可利用性评估
*   是否需要登录态？
*   是否受 WAF 影响？

### 4. 修复建议
*   业务逻辑修正 (e.g. 增加后端权限校验)。
*   参数过滤建议 (e.g. 使用 PreparedStatement)。