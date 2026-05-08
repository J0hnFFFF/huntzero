---
description: 邮件系统攻击面深度测绘 — SMTP/IMAP/POP3 命令解析器、MIME 解析、队列文件、SASL 认证与中继策略
tags: [mail, smtp, imap, pop3, mime, attack-surface, sasl, relay]
---

# 邮件系统目标定义 (Mail Target Definer)

## 触发警觉的信号 (Triggers)

邮件安全专家看到以下特征会立即警觉：

- **SMTP 端口 25/587/465 直接暴露在互联网**，且 Banner 返回具体软件名与版本号
- **EHLO 响应中包含大量扩展能力**（如 `PIPELINING`、`CHUNKING`、`BINARYMIME`、`STARTTLS`）——扩展越多，解析面越大
- **配置文件中出现 `smtpd_recipient_restrictions = permit` 或 `mynetworks = 0.0.0.0/0`**——开放中继的红灯
- **代码中存在手动实现的 MIME boundary 匹配逻辑**，而非使用经过审计的库
- **Queue 文件路径由用户可控输入拼接而成**——如 `snprintf(path, sizeof(path), "%s/%s", queue_dir, msg_id)`
- **SASL 实现中同时支持 PLAIN/LOGIN 与 NTLM/GSSAPI**，且认证失败后的代码路径未做状态重置
- **LDA 组件以 root 权限运行**，或在 `setuid()` 之前未严格校验目标用户存在性

## 不可跳过的问题链 (Question Chain)

1. **Pre-auth 状态下，服务器接受了哪些命令？** 是否存在 AUTH 之前就能触发复杂解析的命令（如 `STARTTLS`、`XCLIENT`、`ETRN`）？
2. **MIME multipart 的 boundary 匹配是前缀匹配、精确匹配还是正则匹配？** 对 boundary 中出现的空格、引号、转义字符、重复定义如何处理？
3. **Queue 文件路径的构造逻辑中，Message-ID 或主机名是否经过净化？** 能否通过构造特殊 Message-ID 导致路径穿越？
4. **SASL 认证失败时，会话状态机是否回退到未认证状态？** 还是保留了部分认证上下文（如已选中的 mechanism、部分凭据）？
5. **中继策略检查（Relay Policy）是在 `RCPT TO` 阶段执行，还是在 `DATA` 阶段才执行？** 两个阶段之间是否存在状态窗口可被利用？
6. **LDA 写入 Maildir 时，`tmp/` → `new/` 的链接操作是否原子？** 文件名中的时间戳、PID、序号是否由外部可控输入推导而来？

## 攻击链闭合 (Attack Chain Closure)

**完整逻辑证明：从网络触达到系统影响**

```
外部攻击者 → TCP 25/587 连接建立 → SMTP Banner 响应
    → 命令解析器处理 EHLO/MAIL FROM/RCPT TO/DATA
        ├─ 若命令解析存在缺陷：命令注入 / 状态机乱序绕过
        ├─ 若中继策略配置错误：开放中继 → 匿名邮件投递 → 钓鱼 / 垃圾邮件
        ├─ 若 STARTTLS 实现缺陷：降级攻击 → 明文凭据嗅探
        └─ 若 MIME 解析缺陷：内容注入 / 附件绕过 / 解析器崩溃
            → Queue 文件写入阶段
                ├─ 若路径构造缺陷：路径穿越 → 任意文件写入
                └─ 若队列格式解析缺陷：内存损坏 / 信息泄露
                    → LDA 投递阶段
                        ├─ 若权限切换缺陷：特权提升
                        └─ 若 Sieve/过滤器缺陷：命令执行
```

**Invariant 失效证明**：
- 假设 "只有认证用户才能投递邮件" 在 `smtpd_recipient_restrictions` 配置为 `permit_mynetworks` 且源 IP 可被伪造时失效
- 假设 "协议命令必须按序执行" 在实现未严格校验状态时失效——如未发送 EHLO 就直接接受 MAIL FROM
- 假设 "MIME boundary 明确划分内容" 在 boundary 字符串解析存在歧义时失效

## 代码示例 (Code Examples)

### 漏洞模式 1：Queue 文件路径穿越

```c
// 脆弱代码：Postfix 风格队列路径构造
void queue_file_enter(const char *msg_id, const char *queue_dir) {
    char path[256];
    // msg_id 来自远程 SMTP 会话，未经净化直接拼接
    snprintf(path, sizeof(path), "%s/incoming/%s", queue_dir, msg_id);
    int fd = open(path, O_CREAT | O_WRONLY, 0644);
    // 若 msg_id = "../../../etc/cron.d/exploit"，则路径穿越
}
```

```c
// 安全代码：严格的白名单校验
void queue_file_enter_safe(const char *msg_id, const char *queue_dir) {
    char path[256];
    // 只允许字母数字、连字符、下划线
    if (strspn(msg_id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != strlen(msg_id)) {
        log_error("Invalid msg_id characters");
        return;
    }
    // 禁止任何路径分隔符或穿越序列
    if (strstr(msg_id, "/") || strstr(msg_id, "\\") || strstr(msg_id, "..")) {
        log_error("Path traversal attempt in msg_id");
        return;
    }
    snprintf(path, sizeof(path), "%s/incoming/%s", queue_dir, msg_id);
    // 进一步使用 openat(dirfd, msg_id, ...) 限制在 queue_dir 内
    int fd = openat(dirfd(queue_dir), msg_id, O_CREAT | O_WRONLY, 0644);
}
```

### 漏洞模式 2：SMTP 命令状态机缺失

```python
# 脆弱代码：自定义 SMTP 服务器状态机不完整
class SMTPServer:
    def __init__(self):
        self.state = "INIT"  # INIT -> EHLO -> AUTH -> MAIL -> RCPT -> DATA
    
    def handle_mail_from(self, sender):
        # 缺陷：未检查是否已发送 EHLO
        self.sender = sender
        self.state = "MAIL"
        return "250 OK"
    
    def handle_rcpt_to(self, recipient):
        # 缺陷：未检查 AUTH 状态就允许 RCPT TO
        self.recipients.append(recipient)
        return "250 OK"
```

```python
# 安全代码：严格的状态转换校验
VALID_TRANSITIONS = {
    "INIT": ["EHLO", "QUIT"],
    "EHLO": ["AUTH", "STARTTLS", "QUIT"],
    "AUTH": ["MAIL", "QUIT"],
    "MAIL": ["RCPT", "RSET", "QUIT"],
    "RCPT": ["RCPT", "DATA", "RSET", "QUIT"],
    "DATA": ["EHLO", "QUIT"]  # DATA 结束后必须重新 EHLO
}

class SMTPServerSafe:
    def handle_command(self, cmd, arg):
        if cmd not in VALID_TRANSITIONS.get(self.state, []):
            return f"503 Bad sequence of commands (current: {self.state})"
        # ... 处理命令 ...
        self.state = NEXT_STATE[cmd]
```

### 漏洞模式 3：中继策略配置错误

```ini
# 脆弱配置：Postfix main.cf
smtpd_recipient_restrictions = permit_mynetworks, permit
# 缺陷：permit 作为最后一个规则无条件允许所有邮件通过
# 攻击者只需伪造 mynetworks 内的 IP，或利用 permit 的兜底放行
```

```ini
# 安全配置：Postfix main.cf
smtpd_recipient_restrictions = 
    reject_invalid_hostname,
    reject_non_fqdn_hostname,
    reject_non_fqdn_sender,
    reject_non_fqdn_recipient,
    reject_unknown_sender_domain,
    reject_unknown_recipient_domain,
    permit_mynetworks,
    reject_unauth_destination,
    reject_rbl_client zen.spamhaus.org,
    permit
# 关键：reject_unauth_destination 明确拒绝未授权的目标域投递
# permit 仅在通过所有检查后才执行
```

### 漏洞模式 4：SASL 认证状态残留

```c
// 脆弱代码：认证失败后未清理机制选择
void sasl_auth_continue(const char *mech, const char *response) {
    if (strcmp(mech, "PLAIN") == 0) {
        ctx->selected_mech = MECH_PLAIN;
    } else if (strcmp(mech, "NTLM") == 0) {
        ctx->selected_mech = MECH_NTLM;
    }
    
    if (!verify_credentials(response)) {
        send_reply("535 Authentication failed");
        // 缺陷：未重置 selected_mech，下一次命令可能复用认证上下文
        return;
    }
    ctx->authenticated = 1;
}
```

```c
// 安全代码：认证失败完全回滚
void sasl_auth_continue_safe(const char *mech, const char *response) {
    // 无论认证成功与否，函数退出时状态必须确定
    int success = 0;
    
    if (verify_credentials(response)) {
        ctx->authenticated = 1;
        success = 1;
    }
    
    if (!success) {
        ctx->authenticated = 0;
        ctx->selected_mech = MECH_NONE;
        memset(ctx->auth_buffer, 0, sizeof(ctx->auth_buffer));
        ctx->auth_len = 0;
        send_reply("535 Authentication failed");
    } else {
        send_reply("235 Authentication succeeded");
    }
}
```

## 攻击面分层速查表

| 层级 | 组件 | 输入源 | 典型 Invariant 失效 | 影响 |
|------|------|--------|-------------------|------|
| Layer 0 | SMTP 命令解析器 | TCP 字节流 | 状态机不完备、命令注入 | RCE / 未授权投递 |
| Layer 0 | STARTTLS 握手 | TCP 字节流 | 降级攻击、命令走私 | 凭据泄露 / 中间人 |
| Layer 1 | MIME multipart | 邮件体 | Boundary 解析歧义 | 恶意附件绕过 |
| Layer 1 | 编码解码器 | Content-Transfer-Encoding | 多次解码差异 | 内容注入 |
| Layer 2 | Queue 文件处理 | Message-ID / 主机名 | 路径穿越 | 任意文件写入 |
| Layer 2 | SASL 认证 | AUTH 命令序列 | 状态残留、机制降级 | 认证绕过 |
| Layer 3 | LDA 投递 | 信封地址 / 邮件头 | 权限切换缺陷、路径注入 | 本地提权 |
| Layer 3 | Sieve/过滤规则 | 邮件内容 / 用户配置 | 命令注入 | 以用户权限执行命令 |
