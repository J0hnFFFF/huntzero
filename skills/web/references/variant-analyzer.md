---
description: Web 变体分析 - 同源漏洞在代码库中的系统性扩散
tags: [web, variant-analyzer, copy-paste, cross-endpoint, schema-leak, graphql]
---

# Web 变体分析 (Variant Analyzer): 同源漏洞在代码库中的系统性扩散

> 在 Controller A 发现了一个 IDOR？
> 一个成熟的安全者知道：这个 DAO 层的 `findById` 被 N 个 Controller 调用，
> 而且其他实体（User、Document、Invoice）用了同样的模式。
> 你要绘制一张"同源推断缺陷分布图"。

## 变体分析核心方法论：Web 同源漏洞辐射定律

**定律一：DAO 层缺陷守恒定律**
> 如果一个 `findById` 方法未绑定 user_id，
> 那么所有调用该方法的资源端点都存在 IDOR。
> 不是 N 个漏洞，是 1 个缺陷 × N 个暴露面。

**定律二：中间件顺序传染定律**
> 如果 CORS 中间件放在认证中间件之后，
> 那么所有受该中间件链保护的端点都存在同样的预检绕过问题。

**定律三：框架默认配置陷阱定律**
> 如果项目使用了某个框架的默认安全配置（如 Django DEBUG=True、Express 默认 CORS），
> 那么该框架的所有默认脆弱点都可能存在于项目中。

---

## 变体分析一：推断链同构扩散 (Inference Chain Isomorphism)

### 场景
在 `OrderController` 中发现 `orderRepository.findById(id)` 未绑定 user_id。

### 辐射检查清单

**Step 1：DAO 层同源搜索**

```bash
# 搜索所有未绑定用户身份的 findById/findOne 方法
grep -r "findById\|findOne\|getById" --include="*.java" --include="*.py" --include="*.js" src/

# 对每个结果，检查查询条件是否包含 userId/ownerId/tenantId
grep -B2 -A2 "findById" src/repository/*.java
```

**Step 2：Controller 层辐射分析**

找到所有使用同一 DAO 方法的 Controller：

```
OrderController   → orderRepository.findById(id)    ← 已发现 IDOR
InvoiceController → invoiceRepository.findById(id)  ← 待检查
DocumentController→ documentRepository.findById(id) ← 待检查
UserController    → userRepository.findById(id)     ← 待检查
```

对每个 Controller 追问：
1. 是否在调用前做了所有权校验？
2. 校验逻辑是否与 OrderController 相同（可被同样绕过）？
3. 是否有 Admin 接口、内部 API、批量接口绕过了该校验？

**Step 3：批量操作接口检查**

```bash
# 搜索所有 bulk-/batch- 接口
grep -r "bulk\|batch\|multi" --include="*.java" --include="*.py" src/controller/

# 检查是否对每个 ID 做了所有权校验
```

### 变体发现模板

```
## 同源推断缺陷变体报告

### 原始缺陷
- 位置: OrderController.java:45
- 类型: IDOR（失忆困境）
- 根因: orderRepository.findById(id) 未绑定 user_id

### 辐射检查范围
- 扫描的 DAO 方法: [N] 个 findById/findOne
- 扫描的 Controller: [N] 个
- 发现的同源缺陷: [N] 个

### 变体详情
| 变体ID | Controller | DAO 方法 | 所有权校验 | 风险等级 |
|--------|-----------|---------|-----------|---------|
| VAR-01 | InvoiceController | invoiceRepository.findById(id) | 无 | High |
| VAR-02 | DocumentController| documentRepository.findById(id) | 在 Service 层 | Medium |
| VAR-03 | UserController    | userRepository.findById(id) | 有，但 Admin 接口绕过 | High |

### 爆炸半径
- 直接影响: [N] 个资源端点可被遍历
- 潜在影响: 所有使用该 DAO 模式的未来功能
```

---

## 变体分析二：跨端点同构缺陷 (Cross-Endpoint Isomorphism)

### 场景
`POST /api/transfer` 存在 CSRF 漏洞（无 Token、SameSite=None）。

### 辐射检查清单

**Step 1：同源模式搜索**

搜索所有使用相同认证模式且执行状态变更的端点：

```bash
# 搜索所有 @PostMapping / @PatchMapping / @DeleteMapping
grep -r "@PostMapping\|@PatchMapping\|@DeleteMapping" --include="*.java" src/controller/

# 对每个端点，检查：
# 1. 是否有 CSRF Token 校验？
# 2. Cookie 的 SameSite 属性是什么？
# 3. 是否校验 Origin/Referer？
```

**Step 2：权限升级路径分析**

```
POST /api/transfer      ← 已发现 CSRF（资金操作）
POST /api/orders/refund ← 待检查（资金操作）
PATCH /api/users/role   ← 待检查（权限变更）
DELETE /api/account     ← 待检查（数据销毁）
POST /api/api-keys      ← 待检查（凭证生成）
```

**关键洞察**：如果 `transfer` 无 CSRF 保护，同一项目的其他不可逆操作端点很可能也有同样问题。

---

## 变体分析三：GraphQL / API  Schema 级扩散

### 场景
GraphQL 查询允许批量获取多个用户的资源。

### 辐射检查

1. **内省查询暴露**：
   ```graphql
   { __schema { queryType { fields { name args { name } } } } }
   ```
   - 是否暴露了 Admin-only 的查询/变更？
   - 是否暴露了内部调试接口？

2. **深度/复杂度限制**：
   - 是否存在查询深度限制？（无限制 → 可被递归查询 DoS）
   - 是否存在复杂度评分？（无限制 → 可被嵌套查询拖垮）

3. **批量查询越权**：
   ```graphql
   {
     users(ids: [1,2,3,4,5]) { email phone orders { total } }
   }
   ```
   - 如果 `users` 查询未校验当前用户是否有权查看这些用户 → 批量 IDOR

4. **变更操作的副作用**：
   ```graphql
   mutation {
     updateUser(id: 2, role: "admin") { id role }
   }
   ```
   - 是否校验了当前用户有权限修改目标用户？

---

## 变体分析四：框架/依赖级同构缺陷

### 场景
项目使用了某个存在已知漏洞的框架版本。

### 检查清单

1. **框架默认配置检查**：
   - Django: `DEBUG=True`？`ALLOWED_HOSTS=['*']`？
   - Express: `body-parser` 未限制大小？默认 CORS？
   - Spring Boot: Actuator 端点暴露？
   - Laravel: Debug 模式？ Telescope 暴露？

2. **依赖 CVE 检查**：
   ```bash
   # npm
   npm audit
   # Python
   pip-audit
   # Java
   owasp-dependency-check
   ```
   - 每个 Critical CVE 是否与项目的使用场景匹配？
   - 例如：Jackson Databind 反序列化 CVE → 如果项目接收 JSON 输入 → 高危

3. **第三方 SDK/库的同构问题**：
   - 支付 SDK 的回调验签逻辑是否在所有支付渠道一致？
   - OAuth 客户端库是否在各处使用相同的 redirect_uri 校验？

---

## 变体分析五：跨项目/跨团队的同构缺陷

### 场景
公司内多个项目使用相同的技术栈和架构模式。

### 检查清单

1. **共享库检查**：
   - 公司内部的 `common-auth` 库是否使用了相同的 `findById` 模式？
   - 共享的 `cors-config` 库是否动态反射 Origin？
   - 共享的 `upload-utils` 是否做了路径规范化？

2. **模板项目检查**：
   - 新项目是否基于有漏洞的模板创建？
   - `cookiecutter` / `create-react-app` / `spring-initializr` 模板是否有默认脆弱配置？

3. **代码评审习惯**：
   - 如果团队习惯在 Controller 层做权限校验（而非 DAO 层）→ 所有新项目都可能存在 IDOR
   - 如果团队习惯信任前端校验 → 所有新项目都可能存在状态伪造

---

## 专家变体分析输出模板

```
## 变体分析报告

### 原始漏洞
[描述最初发现的漏洞及根因]

### 辐射分析
- 根因位置: [DAO/中间件/框架配置/共享库]
- 扫描范围: [代码文件范围]
- 发现的同源模式数量: [N]

### 变体清单
| 变体ID | 位置 | 漏洞类型 | 根因相同度 | 风险等级 | 修复优先级 |
|--------|------|---------|-----------|---------|-----------|
| VAR-01 | InvoiceController | IDOR | 完全相同 | High | P0 |
| VAR-02 | DocumentController | IDOR | 校验在 Service 层 | Medium | P1 |
| VAR-03 | GraphQL users 查询 | 批量越权 | 模式相同 | High | P0 |

### 爆炸半径
- 直接受影响的端点: [N]
- 间接受影响的端点（共享 DAO/中间件）: [N]
- 跨项目影响: [N 个项目使用相同模式]

### 根治建议
- 立即: 在 DAO 层添加 user_id 绑定（修复所有同源 IDOR）
- 短期: 审查所有 Controller 的权限校验位置
- 长期: 建立共享库的安全基线，模板项目安全审查
```
