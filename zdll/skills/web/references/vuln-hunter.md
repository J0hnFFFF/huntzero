---
description: Web 漏洞挖掘 - 追问链驱动的攻击链闭合
tags: [web, vuln-hunter, csrf, idor, race-condition, ssrf, path-traversal, oauth-hijack]
---

# Web 漏洞挖掘 (Vuln Hunter): 追问链驱动的攻击链闭合

> Web 漏洞不是扫描器扫出来的，是推出来的。
> 你从第一性原理出发，提出一连串无法被轻易否定的问题，最终逼迫系统承认：
> 是的，这个请求一旦到达，副作用就会发生，而服务器从未确认过用户的真实意志。

## 原理一：请求即代理的意志不确定性 —— CSRF 与不可逆操作劫持

### 触发器
看到涉及资金、权限变更、数据销毁的端点，且该端点可通过简单的 HTTP 请求触发。

### 追问链（不可绕过的 6 个问题）

1. **这个端点是否接受来自任意 Origin 的请求？**
   - 检查 Cookie 的 `SameSite` 属性：`None`（高危）、`Lax`（中危）、`Strict`（相对安全）
   - 检查是否依赖 `Referer`/`Origin` 头做校验（这些头可被浏览器插件或隐私模式移除）
   - 如果没有 Origin 校验 → 任何网站都可以诱导用户向该端点发送请求

2. **是否存在预执行确认机制？能否被绕过？**
   - 二次确认页面：攻击者可以直接 POST 确认接口，跳过前端页面
   - 短信/邮件验证码：检查验证码是否与操作绑定（还是通用验证码）
   - CSRF Token：检查 Token 是否与 Session 绑定、是否可预测、是否在一次请求后失效
   - 检查确认 Token 是否在 URL 中传递（可被 Referer 泄露）

3. **攻击者能否构造一个让用户在不知情的情况下触发的请求？**
   - `POST` 请求：通过自动提交的表单 `<form action="https://victim.com/api/transfer" method="POST">`
   - `GET` 请求：通过 `<img src="https://victim.com/api/delete?id=123">`
   - JSON 请求：通过 `fetch()` 配合 CORS 预检绕过（如果预检配置错误）
   - 如果是 `application/json` 的 POST，检查是否可以通过 `text/plain` 或 form data 降级提交

4. **端点是否可被预检请求（Preflight）绕过？**
   - CORS 简单请求（GET/POST + 特定 Content-Type）不触发预检
   - 如果服务器对简单请求和复杂请求的处理逻辑不一致 → 攻击者用简单请求绕过复杂请求的安全校验

5. **是否存在可被重放的请求？**
   - 转账接口是否幂等？如果用户不小心点击两次，是否会产生两笔转账？
   - 攻击者能否利用重放机制累积效应？（如重复提交积分兑换）

6. **用户触发后是否有明确的反馈？**
   - 如果攻击者通过隐藏 iframe 触发请求，用户完全无感知
   - 如果操作后没有邮件/短信通知，用户可能数天后才发现

### 攻击链闭合推演

```
攻击者构造恶意页面 attacker.com/evil.html
    ↓
页面包含自动提交的表单，目标为 victim.com/api/transfer
    ↓
已登录 victim.com 的用户访问 attacker.com（钓鱼邮件、社交工程、XSS）
    ↓
浏览器自动携带 victim.com 的 Cookie 发送请求
    ↓
victim.com 服务器收到请求：Cookie 有效 → 身份验证通过
    ↓
服务器未校验请求来源 Origin → 认为是用户主动发起的操作
    ↓
转账/权限变更/数据删除执行
    ↓
用户无感知，直到发现账户异常

结论：意志不确定性被利用，攻击链闭合
```

### 典型代码模式与警觉点

```java
// 🚨 高危：无 CSRF 保护，Cookie SameSite=None
@PostMapping("/api/transfer")
public Response transfer(@RequestBody TransferRequest req) {
    // 仅验证 Cookie/Token，未校验请求来源
    return transferService.execute(req);
}
```

```java
// 🚨 高危：CSRF Token 在 URL 中传递（可被 Referer 泄露）
@GetMapping("/api/confirm")
public Response confirm(@RequestParam String token) {
    // 确认逻辑
}
// 用户点击确认链接后，浏览器可能将包含 token 的 URL 作为 Referer 发送到第三方资源
```

```java
// ✅ 相对安全：SameSite=Strict + CSRF Token + Origin 校验
@PostMapping("/api/transfer")
public Response transfer(@RequestBody TransferRequest req,
                         @RequestHeader("Origin") String origin,
                         @CookieValue("csrf_token") String csrfToken) {
    if (!origin.equals("https://victim.com")) throw new SecurityException();
    if (!csrfToken.equals(req.getCsrfToken())) throw new SecurityException();
    return transferService.execute(req);
}
```

---

## 原理二：无状态协议的失忆困境 —— IDOR 与状态推断劫持

### 触发器
看到资源 ID 来自请求参数，且服务器未验证用户对该资源的所有权。

### 追问链（不可绕过的 6 个问题）

1. **资源 ID 是否可预测？**
   - 自增整数（`1, 2, 3...`）→ 攻击者可以遍历所有 ID
   - UUID → 难以遍历，但如果 ID 在 URL/响应中泄露，仍可被利用
   - 检查 API 响应中是否泄露了其他用户的资源 ID

2. **所有权验证在哪一层？是否可能被绕过？**
   - Controller 层验证：较安全，但需确保所有入口都经过该 Controller
   - Service 层验证：安全，但需确保 DAO 查询都带上用户 ID 条件
   - DAO 层验证：最安全（`WHERE owner_id = ? AND id = ?`）
   - 无验证：高危

3. **是否存在批量操作接口？**
   - `POST /api/bulk-delete` 接受 `{"ids": [1, 2, 3]}`
   - 如果批量接口未对每个 ID 做所有权验证 → 攻击者可以批量删除他人资源
   - 检查 GraphQL 查询：一个查询能否同时获取/修改多个用户的资源？

4. **业务流程状态是否可被客户端伪造？**
   - 请求 Body 中有 `status=completed` → 攻击者可以直接将订单标记为已完成
   - 请求中有 `price=0.01` → 攻击者可以修改支付金额
   - 服务器是否独立查询数据库验证实际状态？还是直接信任客户端声明？

5. **并发请求下状态推断是否可能冲突？**
   - 条件竞争（Race Condition）：请求 A 和请求 B 同时检查余额充足，然后同时扣款
   - 检查是否有乐观锁（版本号）或悲观锁（数据库锁）
   - 优惠券/库存扣减接口是否线程安全？

6. **是否存在缓存层导致的状态不一致？**
   - CDN 缓存：基于 URL 缓存响应，如果 URL 不包含用户身份 → 用户 A 可能看到用户 B 的缓存内容
   - Redis 缓存：缓存键是否包含用户 ID？如果仅基于资源 ID 缓存 → IDOR 影响被放大
   - 浏览器缓存：`Cache-Control` 配置是否导致敏感数据被缓存？

### 攻击链闭合推演

```
用户 A 登录系统，访问 /api/orders/1001
    ↓
URL 中 order_id=1001 属于用户 A，服务器返回订单详情
    ↓
用户 A 修改为 /api/orders/1002
    ↓
服务器查询：SELECT * FROM orders WHERE id = 1002（未加 user_id 条件）
    ↓
返回用户 B 的订单详情（包含地址、电话、购买内容）
    ↓
如果是 /api/orders/1002/download → 用户 A 下载了用户 B 的发票
    ↓
如果是 POST /api/orders/1002/refund → 用户 A 为用户 B 申请了退款

结论：失忆困境被利用，攻击链闭合
```

### 典型代码模式与警觉点

```java
// 🚨 高危：未验证所有权
@GetMapping("/api/orders/{id}")
public Order getOrder(@PathVariable Long id) {
    return orderRepository.findById(id);  // 查询条件未绑定 userId
}
```

```java
// 🚨 高危：批量操作未逐条验证
@PostMapping("/api/orders/bulk-delete")
public void bulkDelete(@RequestBody List<Long> ids) {
    orderRepository.deleteAllById(ids);  // 未检查这些订单是否属于当前用户
}
```

```java
// ✅ 相对安全：DAO 层绑定用户 ID
@GetMapping("/api/orders/{id}")
public Order getOrder(@PathVariable Long id, @AuthenticationPrincipal User user) {
    return orderRepository.findByIdAndUserId(id, user.getId());
    // SQL: SELECT * FROM orders WHERE id = ? AND user_id = ?
}
```

---

## 原理三：同源策略与跨域需求的结构性张力 —— OAuth 劫持与信任链污染

### 触发器
看到 OAuth 回调、postMessage、CORS 配置、跳转参数。

### 追问链（不可绕过的 6 个问题）

1. **OAuth 的 `redirect_uri` 是否严格匹配？**
   - 检查是否使用精确字符串匹配（`https://victim.com/callback`）而非前缀匹配（`https://victim.com/`）
   - 前缀匹配允许 `https://victim.com.attacker.com/callback`
   - 检查是否允许通配符子目录：`https://victim.com/*` → `https://victim.com/attacker/callback`
   - 检查是否允许不同端口：`https://victim.com:8080/callback`

2. **`state` 参数是否被正确验证？**
   - 是否完全省略 `state`？（高危，可被 CSRF 攻击绑定攻击者账号）
   - `state` 是否可预测？（如时间戳、用户 ID）
   - `state` 是否在一次 OAuth 流程后失效？（可被重放）
   - `state` 是否在 URL 中传递？（可被 Referer 泄露）

3. **postMessage 的 origin 校验是否严格？**
   - 是否使用 `event.origin !== 'https://trusted.com'` 而非 `event.origin === 'https://trusted.com'`？（逻辑错误）
   - 是否使用 `event.origin.includes('trusted.com')`？（可被 `https://trusted.com.attacker.com` 绕过）
   - 接收消息后是否无条件信任消息内容执行操作？

4. **CORS 配置是否过度放权？**
   - `Access-Control-Allow-Origin: *` + `Access-Control-Allow-Credentials: true` = 高危
   - 是否动态反射 `Origin` 头？（`Access-Control-Allow-Origin: ${req.headers.origin}`）
   - 如果动态反射 Origin，攻击者网站可以发送携带用户凭证的跨域请求
   - 预检响应（OPTIONS）的 `Access-Control-Max-Age` 是否过长？（配置变更后客户端仍使用旧配置）

5. **跳转参数的目标域名是否被白名单校验？**
   - `?next_url=https://attacker.com` → 攻击者诱导用户点击，登录后跳转到钓鱼站点
   - 白名单校验是否使用 `endsWith(".victim.com")`？（可被 `https://attacker.victim.com` 绕过，如果攻击者能注册子域名）
   - 是否允许 `javascript:`、`data:` 等伪协议？

6. **子域名是否存在接管风险？**
   - 检查 DNS CNAME 记录：是否有指向已过期云服务（AWS S3、Heroku、GitHub Pages）的 CNAME？
   - 被遗弃的子域名是否仍有有效 SSL 证书？（攻击者注册后可立即启用 HTTPS）
   - 内部子域名（如 `staging.victim.com`）的 DNS 是否指向已删除的资源？

### 攻击链闭合推演

```
攻击者注册 attacker.com
    ↓
发现 victim.com 的 OAuth 配置使用 redirect_uri 前缀匹配：https://victim.com/
    ↓
攻击者构造 OAuth URL：
  https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://victim.com.attacker.com/callback&state=yyy
    ↓
诱导用户点击（钓鱼邮件、XSS、社工）
    ↓
用户登录 victim.com，授权应用
    ↓
victim.com 将用户重定向到 https://victim.com.attacker.com/callback?code=AUTH_CODE
    ↓
攻击者获得 AUTH_CODE，可换取 Access Token
    ↓
攻击者获得用户在该应用下的所有权限

结论：信任重构被利用，攻击链闭合
```

### 典型代码模式与警觉点

```javascript
// 🚨 高危：CORS 动态反射 Origin + 允许凭证
app.use((req, res, next) => {
    res.setHeader('Access-Control-Allow-Origin', req.headers.origin || '*');
    res.setHeader('Access-Control-Allow-Credentials', 'true');
    next();
});
```

```javascript
// 🚨 高危：postMessage 未校验 origin
window.addEventListener('message', (event) => {
    // 未检查 event.origin
    executeAction(event.data);  // 直接信任并执行
});
```

```javascript
// 🚨 高危：跳转目标仅校验包含关系
const nextUrl = req.query.next_url;
if (nextUrl.includes('victim.com')) {
    res.redirect(nextUrl);  // https://attacker.com?victim.com 也会通过
}
```

```python
# ✅ 相对安全：精确 redirect_uri 匹配
allowed_uris = ["https://victim.com/callback", "https://app.victim.com/oauth"]
if redirect_uri not in allowed_uris:
    raise OAuthError("Invalid redirect_uri")
```

---

## 原理四：输入到状态转换的语义断层 —— 路径穿越与类型混淆

### 触发器
看到用户输入进入文件路径拼接、命令执行、SQL 拼接，或 JSON/表单解析后的类型转换。

### 追问链（不可绕过的 5 个问题）

1. **文件路径在多层处理中的解释是否一致？**
   - URL 层：`/files/..%2f..%2fetc%2fpasswd` → URL decode 1 次 → `/files/../../etc/passwd`
   - 如果框架自动 URL decode 1 次，应用层又 decode 1 次 → `..%252f` 可绕过
   - 路径规范化：`/../../etc/passwd` vs `/var/www/../../../etc/passwd`
   - 空字节截断：`file.txt%00.jpg` → 某些 C 库在 `%00` 处截断

2. **Unicode 等价字符是否被正确处理？**
   - 规范化前：`%C0%AF` → 规范化后：`/`（某些旧版 JVM/Apache）
   - Unicode 等价字符：`../` = `../`（在某些解析器中）
   - 组合字符：`é` = `e` + `´`（组合字符），在某些文件名比较中被视为不同字符

3. **类型转换是否存在陷阱？**
   - JSON：`{"is_admin": "true"}` → 某些弱类型语言中 `"true"` == `true`
   - 表单：`key[]=value1&key[]=value2` → 后端期望字符串但得到数组 → 类型混淆
   - 数字：`{"id": 1e10}` → 某些解析器溢出或精度丢失
   - 布尔值：`{"active": "false"}` → 字符串 `"false"` 在条件判断中为 truthy

4. **长度限制是否在正确的层面？**
   - 前端限制 50 字符，后端限制 50 字节 → 多字节字符（如中文 3 字节/字符）可绕过
   - 数据库存储限制 255 字节，但应用层限制 255 字符 → 截断导致语义变化
   - 显示层截断：用户名 `admin` + 大量空格 + `attacker` → 显示为 `admin` 但实际存储不同

5. **命令拼接是否存在注入？**
   - `exec("convert " + user_input)` → 用户输入 `file.jpg; cat /etc/passwd`
   - `eval("obj." + user_input + "()")` → 用户输入 `__class__`
   - SQL 拼接：`"SELECT * FROM users WHERE id = " + user_input` → `1 OR 1=1`

### 攻击链闭合推演

```
攻击者上传文件，文件名为 "../../../app/config.py\x00.jpg"
    ↓
前端校验：以 .jpg 结尾 → 通过
    ↓
后端校验：filename.endswith('.jpg') → 通过（因为 \x00 后的内容被忽略）
    ↓
文件保存路径：/var/www/uploads/ + filename
    ↓
操作系统/API 在 \x00 处截断 → 实际写入 /var/www/app/config.py
    ↓
应用配置被覆盖 → 服务器重启后加载恶意配置

结论：语义断层被利用，攻击链闭合
```

### 典型代码模式与警觉点

```python
# 🚨 高危：路径拼接无规范化
file_path = "/var/www/uploads/" + request.files['upload'].filename
# 攻击者上传文件名 "../../../etc/passwd"
```

```python
# 🚨 高危：SQL 拼接
query = "SELECT * FROM users WHERE id = " + request.args.get('id')
# id = "1 OR 1=1"
```

```javascript
// 🚨 高危：类型混淆
if (req.body.is_admin) {  // req.body = {"is_admin": "false"}
    grantAdmin();  // 字符串 "false" 是 truthy！
}
```

```python
# ✅ 相对安全：路径规范化 + 白名单
from pathlib import Path
upload_dir = Path("/var/www/uploads").resolve()
target = (upload_dir / filename).resolve()
if not str(target).startswith(str(upload_dir)):
    raise SecurityError("Path traversal detected")
```

---

## 高命中率漏洞 Pattern 速查表

| Pattern | 检测方式 | 典型目标 | 对应原理 |
|---------|---------|---------|---------|
| 无 CSRF 保护的 POST 端点 | 搜索 `@PostMapping`、`app.post()`，检查是否有 CSRF Token 或 SameSite | 所有 Web 应用 | 原理一 |
| IDOR（可预测 ID + 无所有权验证） | 搜索 `findById`、`SELECT * FROM ... WHERE id = ?`（无 user_id 条件） | 多租户应用 | 原理二 |
| CORS 动态反射 Origin | 搜索 `Access-Control-Allow-Origin` 设置为请求头值 | API 服务 | 原理三 |
| OAuth redirect_uri 前缀匹配 | 搜索 `redirect_uri` 校验逻辑，检查是否用 `startsWith` | OAuth 实现 | 原理三 |
| 路径拼接无规范化 | 搜索 `+ filename`、`os.path.join(upload_dir, user_input)` | 文件上传功能 | 原理四 |
| 客户端声明业务状态 | 搜索请求 Body 中的 `status=`、`step=`、`is_paid=` | 电商、工作流 | 原理二 |
| postMessage 无 origin 校验 | 搜索 `addEventListener('message')`，检查是否有 `event.origin` 校验 | 嵌入第三方组件 | 原理三 |
| 并发竞争（优惠券/库存） | 搜索 `SELECT balance` → `UPDATE balance`（无锁） | 金融、电商 | 原理二 |
| 跳转参数无白名单 | 搜索 `redirect=`、`next_url=`、`return_to=` | 登录、支付回调 | 原理三 |
| 类型混淆（JSON 字符串/布尔） | 搜索弱类型语言中未做严格类型校验的字段 | Node.js、PHP、Python | 原理四 |

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [意志不确定/失忆困境/信任重构/语义断层]
- 推断断裂点: [描述服务器做出的错误假设]
- 攻击链: [从入口到影响的完整路径]

### 脆弱路径
- 入口: [URL/接口/参数]
- 状态推断链: [请求参数 → 服务器假设 → 实际执行]
- 攻击者控制点: [哪个字段/参数/头]

### 验证思路
[如何构造最小请求使服务器的推断出错，使用无害 payload]

### 危害本质
- 影响范围: [单个用户/所有用户/全平台]
- 可利用性: [0-Click/低交互/需社工]
- 业务影响: [资金损失/数据泄露/权限提升/服务破坏]
```
