---
description: 邮件系统假设验证 — 手工构造 SMTP 会话、MIME 边界模糊测试、中继探测、STARTTLS 剥离测试
tags: [mail, hypothesis-testing, smtp, netcat, mime, fuzzing, relay, starttls]
---

# 邮服假设验证 (Mail Hypothesis Tester)

## 触发警觉的信号 (Triggers)

- **发送 `HELP` 命令后，服务器返回了非标准扩展命令列表**——非标准命令通常测试覆盖率低
- **`EHLO` 响应中包含 `PIPELINING`，但测试发现某些命令组合流水线发送时行为异常**——竞态条件或解析边界
- **发送畸形 MIME（如缺少结束 boundary）后，服务器没有返回错误而是静默接受**——解析器容错过度可能隐藏漏洞
- **`STARTTLS` 后发送明文风格的命令仍被接受**——TLS 状态隔离可能不完整
- **向外部 Gmail 地址发送测试邮件未收到 `550 Relay denied`**——开放中继的直接证据
- **使用超长 boundary（>1000 字符）时服务器响应时间显著增加**——可能存在线性扫描或缓冲区复制
- **发送 `AUTH PLAIN` 后故意发送畸形 Base64，服务器返回 `334` 而非 `501`**——状态机可能进入未定义中间态

## 不可跳过的问题链 (Question Chain)

1. **如果我用 netcat 连接 SMTP 端口，不发送 EHLO 直接发送 `MAIL FROM:<a@b.com>`，服务器返回什么？** 如果是 `250` 而非 `503`，状态机假设已被推翻。
2. **如果在 `RCPT TO` 参数中插入 `\r\n` 后跟另一个 SMTP 命令，服务器是将 `\r\n` 视为地址的一部分拒绝，还是将其解释为命令分隔符？** 后者意味着命令注入可行。
3. **构造一封邮件，Content-Type 中 boundary 参数声明为 `abc`，但实际正文中使用 `--abcd` 作为分隔符，解析器是否错误匹配？** 前缀匹配是边界混淆的温床。
4. **在未认证状态下向外部域（如 `@gmail.com`）发送 `RCPT TO`，服务器在哪个阶段拒绝？** 如果延迟到 `DATA` 阶段才拒绝，中继策略检查可能不完整。
5. **发送 `STARTTLS` 后立即在同一 TCP 包中发送 `AUTH PLAIN`（不等待 TLS 握手完成），加密层启动前的 AUTH 数据是否被丢弃？** 如果没有被丢弃，存在命令走私窗口。
6. **在 `DATA` 阶段发送一行仅包含 `.` 的行后立刻发送 `RSET`，再发送 `MAIL FROM`，服务器是否进入了一个"半重置"状态？** 半重置状态可能遗留发件人/收件人信息。

## 攻击链闭合 (Attack Chain Closure)

**完整逻辑证明：从假设验证到漏洞确认**

### 验证路径 A：SMTP 状态机乱序

```
假设："服务器要求必须先 EHLO 才能 MAIL FROM"
    → 实验：nc target 25 → 直接发送 "MAIL FROM:<a@b.com>\r\n"
        → 观察响应：
            ├─ 250 OK → 假设被推翻，状态机不完备
            ├─ 503 Bad sequence → 假设成立，继续测试边界
            └─ 其他响应 → 记录并分析语义差异
                → 若 250 OK：进一步测试是否可直接 RCPT TO、DATA
                    → 若全部通过：确认匿名投递无需任何前置握手
                        → 进一步测试向外部域投递
                            → 若外部域也成功：确认开放中继
```

### 验证路径 B：MIME 边界解析差异

```
假设："安全网关和目标 MUA 对同一 MIME 结构解析一致"
    → 实验：构造双 boundary 声明邮件
        Content-Type: multipart/mixed; boundary="safe"
                     ; boundary="evil"
        → 分别投递到安全网关和目标邮箱
            → 提取网关解析结果（附件列表、MIME 类型）
            → 提取 MUA 解析结果
                → 对比两者是否一致
                    → 若不一致：确认解析差异可被利用于网关绕过
                        → 构造 payload：网关 sees text/plain, MUA sees application/x-sh
```

### 验证路径 C：STARTTLS 降级

```
假设："STARTTLS 命令后服务器只接受加密流量"
    → 实验：使用 openssl s_client -starttls smtp 连接，但拦截 STARTTLS 响应
        → 或：使用 telnet 发送 STARTTLS 后不执行 TLS 握手，继续发送明文命令
            → 观察服务器是否：
                ├─ 仍然接受明文 AUTH → 降级可行，凭据可被嗅探
                ├─ 静默断开 → 部分保护，但可尝试中间人阻止 STARTTLS
                └─ 返回 554 TLS handshake required → 相对安全
                    → 进一步：在 TLS 握手过程中发送未加密命令（流水线攻击）
                        → 若命令被处理：确认 TLS 启动窗口存在命令走私
```

## 代码示例 (Code Examples)

### 验证脚本 1：SMTP 状态机乱序探测

```python
# 脆弱假设验证：不发送 EHLO 直接发送后续命令
import socket
import sys

def test_state_machine(target, port=25):
    commands = [
        ("MAIL FROM:<test@example.com>\r\n", "INIT->MAIL"),
        ("RCPT TO:<victim@target.com>\r\n", "INIT->RCPT"),
        ("AUTH PLAIN dGVzdA==\r\n", "INIT->AUTH"),
        ("DATA\r\n", "INIT->DATA"),
        ("RSET\r\n", "INIT->RSET"),
    ]
    
    for cmd, transition in commands:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.connect((target, port))
        banner = s.recv(1024)
        # 故意跳过 EHLO
        s.send(cmd.encode())
        resp = s.recv(1024).decode()
        s.close()
        
        if resp.startswith("250") or resp.startswith("235"):
            print(f"[ALERT] {transition}: ACCEPTED (state violation)")
            print(f"         Response: {resp.strip()}")
        else:
            print(f"[OK] {transition}: REJECTED ({resp.strip()})")

if __name__ == "__main__":
    test_state_machine(sys.argv[1])
```

### 验证脚本 2：MIME 边界模糊测试

```python
import smtplib
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText

def craft_ambiguous_mime(boundary_strategy="duplicate"):
    msg = MIMEMultipart()
    msg['Subject'] = 'MIME Boundary Test'
    msg['From'] = 'attacker@evil.com'
    msg['To'] = 'victim@target.com'
    
    # 策略 1: 重复 boundary 声明
    if boundary_strategy == "duplicate":
        msg.set_param('boundary', 'safe_boundary')
        # 手动添加第二个 boundary 参数（某些解析器可能取最后一个）
        msg['Content-Type'] = msg['Content-Type'].replace(
            'boundary="safe_boundary"',
            'boundary="safe_boundary"; boundary="evil_boundary"'
        )
    
    # 策略 2: boundary 前缀匹配测试
    elif boundary_strategy == "prefix":
        msg.set_param('boundary', 'abc')
        # 正文使用 --abcd 而非 --abc--
        payload = ("--abc\r\n"
                   "Content-Type: text/plain\r\n\r\n"
                   "Safe content\r\n"
                   "--abcd\r\n"  # 前缀匹配测试
                   "Content-Type: application/x-sh\r\n\r\n"
                   "#!/bin/sh\r\necho pwned\r\n"
                   "--abc--\r\n")
        # 替换默认生成的 MIME 结构
        return msg.as_string().split('\r\n\r\n', 1)[0] + '\r\n\r\n' + payload
    
    msg.attach(MIMEText('Safe text'))
    return msg.as_string()

def send_mime_test(target, strategy):
    msg_body = craft_ambiguous_mime(strategy)
    with smtplib.SMTP(target, 25) as s:
        s.sendmail('attacker@evil.com', 'victim@target.com', msg_body)
    print(f"[SENT] MIME test with strategy: {strategy}")
```

### 验证脚本 3：开放中继探测

```bash
#!/bin/bash
# 脆弱假设验证：服务器是否允许未认证用户向外部域中继
TARGET=${1:-localhost}
PORT=${2:-25}
EXTERNAL="test.relay.probe@gmail.com"

echo "Testing open relay on ${TARGET}:${PORT}..."
{
    sleep 1
    echo "HELO relaytest"
    sleep 0.5
    echo "MAIL FROM:<relaytest@evil.com>"
    sleep 0.5
    echo "RCPT TO:<${EXTERNAL}>"
    sleep 0.5
    echo "DATA"
    sleep 0.5
    echo "Subject: Relay Test"
    echo ""
    echo "This is a relay probe."
    echo "."
    sleep 0.5
    echo "QUIT"
} | nc -q 5 ${TARGET} ${PORT}

# 分析输出：若 RCPT TO 返回 250 而非 554，则存在中继可能
```

### 验证脚本 4：STARTTLS 降级探测

```python
import socket
import ssl

def test_starttls_downgrade(target, port=587):
    # 阶段 1：正常 STARTTLS 流程
    s_plain = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s_plain.connect((target, port))
    banner = s_plain.recv(1024)
    s_plain.send(b"EHLO tester\r\n")
    ehlo_resp = s_plain.recv(4096)
    
    if b"STARTTLS" not in ehlo_resp:
        print("[INFO] STARTTLS not advertised")
        return
    
    # 阶段 2：发送 STARTTLS 但不完成握手，直接发送 AUTH
    s_plain.send(b"STARTTLS\r\n")
    tls_resp = s_plain.recv(1024)
    print(f"[STARTTLS] {tls_resp.strip()}")
    
    # 故意不执行 TLS 握手，发送明文 AUTH
    s_plain.send(b"AUTH PLAIN dGVzdAB0ZXN0AHRlc3Q=\r\n")
    auth_resp = s_plain.recv(1024)
    
    if auth_resp.startswith(b"235") or auth_resp.startswith(b"334"):
        print(f"[VULNERABLE] AUTH accepted over plaintext after STARTTLS!")
        print(f"             Response: {auth_resp.strip()}")
    else:
        print(f"[SAFE] AUTH rejected: {auth_resp.strip()}")
    
    s_plain.close()
    
    # 阶段 3：流水线攻击测试（STARTTLS + AUTH 在同一包）
    s_pipe = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s_pipe.connect((target, port))
    s_pipe.recv(1024)  # banner
    s_pipe.send(b"EHLO tester\r\n")
    s_pipe.recv(4096)
    
    # 同时发送 STARTTLS 和 AUTH（流水线）
    s_pipe.send(b"STARTTLS\r\nAUTH PLAIN dGVzdAB0ZXN0AHRlc3Q=\r\n")
    responses = s_pipe.recv(4096)
    print(f"[PIPELINE] Responses: {responses}")
    s_pipe.close()
```

## 验证方法论速查表

| 假设类型 | 验证工具 | 关键观察指标 | 阳性判定标准 |
|---------|---------|------------|------------|
| 状态机完备性 | netcat / Python socket | 命令响应码（250 vs 503） | 未满足前置条件却返回 2xx |
| MIME 解析一致性 | 手工构造邮件 + 多解析器对比 | 附件 MIME 类型识别差异 | 网关与 MUA 对同一附件类型判定不同 |
| 命令注入 | netcat + `\r\n` 注入 | 注入后的命令是否被执行 | `RCPT TO` 注入后返回 250 |
| 开放中继 | swaks / 自定义脚本 | RCPT TO 外部域的响应 | 未认证状态下外部域返回 250 |
| STARTTLS 降级 | Python socket / openssl | TLS 握手前命令是否被接受 | STARTTLS 后明文 AUTH 仍成功 |
| SASL 状态残留 | Python smtplib | RSET/QUIT 后旧机制是否有效 | RSET 后无需重新 AUTH 即可发送 |
| 边界解析歧义 | 自定义 MIME 生成器 | 超长/重复 boundary 的处理 | 前缀匹配、参数覆盖行为异常 |
