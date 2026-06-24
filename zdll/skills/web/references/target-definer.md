---
description: Web 目标定义 - 专家看到路由和控制器时的第一直觉
tags: [web, target-definer, idor, csrf, oauth, race-condition, path-traversal]
---

# Web 目标定义 (Target Definer): 专家看到路由和控制器时的第一直觉

> 在 Web 的世界里，你看到的不是一个 REST API，而是一个**不断失忆又被迫重建记忆的对话系统**。
> 你的任务不是审计每一行代码，是标记出那些"一发即中"的端点——
> 一个精心构造的请求一旦到达，副作用就已经发生，而服务器从未问过用户："这真的是你的意志吗？"

## 触发器：什么景象让 Web 专家立刻警觉？

### 触发器一：看到不可逆操作端点（资金、权限、销毁）

**你的第一眼反应**："这个端点一旦被命中，能否回滚？攻击者能否让用户在不知情的情况下触发它？"

看到 `POST /api/transfer`、`DELETE /api/account`、`PATCH /api/users/{id}/role` 这类端点——这是意志不确定性的温床。

**立即追问链**：
1. 这个操作是否涉及资金动账？（转账、支付、退款、提现）
2. 这个操作是否涉及权限变更？（角色授予、API Key 生成、密码修改）
3. 这个操作是否涉及数据销毁？（账户删除、记录清空、退订服务）
4. 是否存在预执行确认机制？（二次确认页面、短信验证码、邮件确认链接）
5. 确认机制能否被绕过？（直接 POST 确认接口、跳过前端校验、伪造确认 Token）
6. 这个端点是否接受跨域请求？Cookie 的 SameSite 属性是什么？
7. 攻击者能否通过 CSRF、XSS、恶意链接诱导已登录用户访问此端点？

### 触发器二：看到资源 ID 直接来自请求参数

**你的第一眼反应**："服务器从请求中推断'用户想操作资源 X'，但它验证过用户真的拥有资源 X 吗？"

看到 `GET /api/orders/{order_id}`、`POST /api/documents/{doc_id}/share`——这是失忆困境的经典信号。

**立即追问链**：
1. 资源 ID 来自哪里？URL 路径参数？Query String？Body JSON？Header？
2. 服务器在操作前是否查询数据库验证"当前用户 = 资源所有者"？
3. 验证逻辑是在 Controller 层、Service 层、还是 DAO 层？哪一层可能被绕过？
4. ID 是否可预测？（自增整数 vs UUID）可预测 ID 意味着攻击者可以遍历他人资源
5. 是否存在批量操作接口？`POST /api/bulk-delete` 是否接受 ID 数组？
6. 并发场景下，两个请求同时操作同一资源，服务器的状态推断是否可能冲突？
7. 是否有缓存层（Redis/CDN）基于请求特征缓存响应？缓存键是否包含用户身份？

### 触发器三：看到跨域交互点（OAuth、postMessage、CORS）

**你的第一眼反应**："一个携带用户凭证的请求，能否被诱导流向攻击者控制的端点？"

看到 `/oauth/callback`、`window.addEventListener('message')`、`Access-Control-Allow-Origin` 配置——这是信任重构的温床。

**立即追问链**：
1. OAuth 回调中 `redirect_uri` 是否严格匹配？是否允许子目录通配符或端口变化？
2. `state` 参数是否被验证？验证逻辑是什么？是否可预测或可重用？
3. postMessage 的目标 `origin` 是否严格校验？是否使用 `event.origin !== 'https://trusted.com'`？
4. postMessage 接收后，消息内容是否被无条件信任执行？
5. CORS 配置中 `Access-Control-Allow-Origin` 是否设置为 `*`？是否同时允许 `withCredentials: true`？
6. 是否存在 `next_url`、`return_to`、`redirect` 参数？目标域名是否被白名单校验？
7. 子域名是否存在被遗弃的 CNAME？攻击者能否注册该子域名并接收原本定向到该域的流量？
8. JSONP 接口是否还存在？回调函数名是否被过滤？

### 触发器四：看到用户输入进入文件系统或命令执行

**你的第一眼反应**："安全检查在 A 层基于某种解释做判断，但实际操作在 B 层执行——这两层的解释一致吗？"

看到文件上传、文件下载、路径拼接、命令执行——这是语义断层的信号。

**立即追问链**：
1. 文件上传后的存储路径是否基于用户可控的文件名？是否做了路径规范化？
2. URL 中的文件路径参数是否被 URL decode 多次？每次 decode 由哪一层处理？
3. 文件名中是否可能存在 Unicode 等价字符？（如 `%C0%AF` 替代 `/`）
4. JSON Body 中的字段类型是否被严格校验？`"0"` 和 `0` 在不同层的解释是否一致？
5. 表单提交中的数组表示法 `key[]` 是否被后端正确解析？是否可能导致参数污染？
6. 长度限制是在字符层面还是字节层面？多字节字符是否可绕过长度限制？
7. 是否存在命令拼接？`exec("convert " + user_input)` 中的 `user_input` 是否可被注入？

### 触发器五：看到业务状态由客户端声明

**你的第一眼反应**："客户端告诉服务器'现在状态是 X'，服务器就信了？"

看到请求 Body 中有 `status=completed`、`step=3`、`is_paid=true`——这是失忆困境的另一面。

**立即追问链**：
1. 这个状态字段是否来自客户端可控的输入？
2. 服务器在更新前是否查询数据库验证实际状态？
3. 业务流程是否有严格的状态机？能否从 `pending` 直接跳到 `completed`？
4. 价格/金额是否由客户端提供？服务器是否独立计算并校验？
5. 时间戳、nonce、签名是否由客户端生成？服务器如何验证其有效性？

## 攻击面地图：Web 全链条审计清单

| 链条环节 | 目标文件/位置 | 审计焦点 | 对应原理 |
|---------|-------------|---------|---------|
| **路由定义** | `routes.py`, `@RequestMapping`, `router.get()`, OpenAPI/Swagger | 不可逆操作端点、资源 ID 暴露面、批量操作 | 原理一、原理二 |
| **控制器/Handler** | Controller, View, Handler | 身份校验位置、权限校验逻辑、状态推断链 | 原理二 |
| **中间件/拦截器** | Middleware, Filter, Interceptor, Gateway | CORS 配置、认证前置/后置、请求解析层 | 原理三 |
| **Service 层** | Service, UseCase, Business Logic | 业务状态机、所有权验证、并发控制 | 原理二 |
| **数据访问层** | Repository, DAO, ORM Query | SQL 注入、IDOR（查询条件是否绑定用户 ID） | 原理二、原理四 |
| **配置与部署** | `application.yml`, Nginx, CDN, WAF | CORS 头、同源策略、缓存策略、SSL/TLS | 原理三 |
| **第三方集成** | OAuth Client, Payment SDK, Analytics | redirect_uri 校验、postMessage origin、回调签名 | 原理三 |
| **前端交互** | HTML Form, JavaScript Fetch, iframe | CSRF Token、SameSite Cookie、postMessage 监听 | 原理一、原理三 |

## 项目类型快速校准

| 项目类型 | P0 关注 | P1 关注 | P2 关注 |
|---------|--------|--------|--------|
| **高价值交易类**（支付、转账） | 不可逆操作 + CSRF | 状态推断 + 并发竞争 | 语义断层 |
| **多租户 SaaS**（云盘、协作） | IDOR + 权限推断 | 跨租户数据隔离 | CORS 配置 |
| **开放平台**（OAuth、API 网关） | redirect_uri 劫持 | Token 作用域越界 | 第三方脚本嵌入 |
| **自托管开源**（CMS、论坛） | 默认配置脆弱性 | 插件/主题供应链 | 管理员接口暴露 |
| **Web 框架/库** | 输入验证 bypass | 中间件顺序问题 | 文档误导性示例 |

## 输出要求

1. **Attack Surface Map**: 用文本/ASCII 画出应用的所有端点，按四大原理分类标注风险等级
2. **状态推断链分析**: 对每个 P0/P1 端点，画出 `请求参数 → 服务器假设 → 实际执行` 的完整推断链，标记断裂点
3. **跨域信任流向图**: 标注所有跨域交互点，画出凭证/Token 的流动路径
4. **高价值端点清单**: 列出最可能中招的 3-5 个端点，每个附带具体的路由路径和控制器方法签名
