---
description: 邮服代码深度理解 — 命令分发器、状态机、MIME 解析逻辑、队列文件格式、权限分离边界
tags: [mail, code-analysis, smtp, state-machine, mime, queue, privilege-separation]
---

# 邮服代码理解者 (Mail Code Understander)

## 触发警觉的信号 (Triggers)

- **看到一个巨大的 `switch(cmd)` 或 `if-else` 链处理 SMTP/IMAP 命令**，且每个分支对当前状态的检查不一致——状态机碎片化是漏洞根源
- **`parse_mime_header()` 函数递归调用自身且没有深度限制**——栈溢出或拒绝服务
- **队列文件使用固定大小的栈缓冲区（如 `char path[256]`）拼接路径**——经典的栈溢出/路径截断
- **`setuid()` 调用之前没有 `setgid()` 和 `initgroups()` 的配套调用**——提权后仍保留敏感组权限
- **认证代码中 `memcmp(password_hash, computed_hash, len)` 没有恒定时间比较**——时序攻击泄露密码
- **MIME 解码函数中 `malloc(size)` 的 `size` 来自邮件头中的 `Content-Length` 或 Base64 解码后的长度计算**——整数溢出导致堆分配不足
- **命令分发器在 `read()` 之后直接使用 `strtok()`/`strsep()` 分割缓冲区**，且没有检查剩余未处理字节——命令走私/注入

## 不可跳过的问题链 (Question Chain)

1. **命令分发器将输入字节流映射到命令 ID 的过程中，是否区分大小写？** `ehlo` 和 `EHLO` 是否等价？如果等价，是在哪个阶段做的统一转换？转换前是否已进行了长度校验？
2. **状态机的实现是集中式（一个大的状态转换表）还是分散式（每个命令处理函数自行检查并修改状态）？** 分散式实现中，是否存在某个命令处理函数忘记检查前置状态？
3. **MIME 解析器对 `Content-Type` 头中的 `boundary` 参数提取逻辑是怎样的？** 是否处理了引号包裹、RFC 2047 编码、多行折叠（line folding）？这些边缘情况是否引入了解析差异？
4. **队列文件的格式是文本行（如 Postfix）还是二进制结构（如 Qmail）？** 如果是文本行，行尾是 `\n` 还是 `\r\n`？换行符处理是否统一？是否存在行尾注入？
5. **权限分离边界上（如 smtpd → cleanup → qmgr → LDA），数据是通过什么 IPC 传递的？** 是 Unix domain socket、共享内存、还是管道？传递过程中是否校验了发送者身份（如 SCM_CREDENTIALS）？
6. **SASL 库（如 Cyrus SASL）与邮件服务器主代码之间的接口中，认证成功后的用户名字符串（authid）是否经过规范化（canonicalization）？** 大小写敏感的用户名在后续授权检查中是否保持一致？

## 攻击链闭合 (Attack Chain Closure)

**完整逻辑证明：从代码结构缺陷到可利用漏洞**

```
代码审计发现命令分发器使用分散式状态检查
    → 某个命令（如 XCLIENT）的处理函数未检查 AUTH 前置状态
        → 攻击者在未认证状态下发送 XCLIENT 并伪造客户端属性
            → 服务器将连接标记为来自信任网络（mynetworks）
                → 后续 MAIL FROM/RCPT TO 通过中继检查
                    → 开放中继利用成功
```

```
代码审计发现 MIME 解析器递归解析 multipart 且无深度限制
    → 攻击者构造 10000 层嵌套的 multipart
        → 递归调用 parse_mime() 耗尽栈空间
            → 栈溢出或段错误（SIGSEGV）
                → 如果栈上有返回地址或 canary 被破坏 → 可能 RCE
                    → 或至少造成拒绝服务
```

```
代码审计发现 LDA 在 setuid(target_user) 之前 open() 了目标 Maildir 路径
    → open() 时仍为 root 权限
        → 如果路径被符号链接重定向到 /etc/shadow
            → root 以写权限打开 /etc/shadow
                → 后续 write() 覆盖系统密码文件
                    → 本地提权
```

## 代码示例 (Code Examples)

### 漏洞模式 1：分散式状态机缺失

```c
// 脆弱代码：每个命令自行管理状态，存在遗漏
void smtp_cmd_mailfrom(struct session *s, const char *arg) {
    // 某些路径忘记检查 s->state
    s->sender = parse_address(arg);
    s->state = STATE_MAIL;
    reply(s, "250 OK");
}

void smtp_cmd_auth(struct session *s, const char *arg) {
    if (s->state != STATE_EHLO) {
        reply(s, "503 Bad sequence");
        return;
    }
    // ... 认证逻辑 ...
}

// 遗漏：XCLIENT 命令没有状态检查
void smtp_cmd_xclient(struct session *s, const char *arg) {
    // 缺陷：任何状态下都可以执行，包括未发送 EHLO 或已认证后
    parse_xclient_attributes(s, arg);
    s->state = STATE_EHLO;  // 强制重置状态！
    reply(s, "250 OK");
}
```

```c
// 安全代码：集中式状态转换表
static const struct state_transition transitions[] = {
    {STATE_INIT,   "EHLO",     STATE_EHLO},
    {STATE_INIT,   "QUIT",     STATE_QUIT},
    {STATE_EHLO,   "AUTH",     STATE_AUTH},
    {STATE_EHLO,   "STARTTLS", STATE_TLS},
    {STATE_EHLO,   "QUIT",     STATE_QUIT},
    {STATE_AUTH,   "MAIL",     STATE_MAIL},
    {STATE_MAIL,   "RCPT",     STATE_RCPT},
    {STATE_RCPT,   "RCPT",     STATE_RCPT},
    {STATE_RCPT,   "DATA",     STATE_DATA},
    // XCLIENT 明确不允许
};

void dispatch_command(struct session *s, const char *cmd, const char *arg) {
    for (size_t i = 0; i < ARRAY_SIZE(transitions); i++) {
        if (s->state == transitions[i].from &&
            strcasecmp(cmd, transitions[i].cmd) == 0) {
            transitions[i].handler(s, arg);
            s->state = transitions[i].to;
            return;
        }
    }
    reply(s, "503 Bad sequence of commands");
}
```

### 漏洞模式 2：MIME 递归解析无深度限制

```c
// 脆弱代码：无限制递归
typedef struct mime_part {
    struct mime_part *children;
    size_t num_children;
    char *content_type;
    char *body;
} mime_part_t;

mime_part_t *parse_mime(const char *data, size_t len, const char *boundary) {
    mime_part_t *part = calloc(1, sizeof(*part));
    part->content_type = extract_content_type(data, len);
    
    if (strncasecmp(part->content_type, "multipart/", 10) == 0) {
        char *child_boundary = extract_boundary(part->content_type);
        // 缺陷：递归调用，没有任何深度限制
        part->children = parse_mime_children(data, len, child_boundary, &part->num_children);
    }
    return part;
}
```

```c
// 安全代码：强制深度限制 + 循环替代递归
#define MAX_MIME_DEPTH 32

mime_part_t *parse_mime_safe(const char *data, size_t len, const char *boundary, int depth) {
    if (depth > MAX_MIME_DEPTH) {
        log_error("MIME nesting depth exceeded");
        return NULL;  // 拒绝过深嵌套，而非崩溃
    }
    
    mime_part_t *part = calloc(1, sizeof(*part));
    part->content_type = extract_content_type(data, len);
    
    if (strncasecmp(part->content_type, "multipart/", 10) == 0) {
        char *child_boundary = extract_boundary(part->content_type);
        // 传递 depth + 1，确保嵌套层数可控
        part->children = parse_mime_children(data, len, child_boundary, 
                                               &part->num_children, depth + 1);
    }
    return part;
}
```

### 漏洞模式 3：权限切换时序错误（TOCTOU）

```c
// 脆弱代码：检查与使用时序竞态
void deliver_to_maildir(const char *user, const char *mail_data) {
    char path[256];
    snprintf(path, sizeof(path), "/var/mail/%s/Maildir/tmp/", user);
    
    // 阶段 1：以 root 打开/创建目录（TOCTOU 窗口）
    mkdir_p(path, 0700);
    
    // 阶段 2：切换用户
    struct passwd *pw = getpwnam(user);
    setuid(pw->pw_uid);
    
    // 阶段 3：写入——但路径可能已被攻击者修改
    int fd = open(path, O_WRONLY | O_CREAT, 0600);
    write(fd, mail_data, strlen(mail_data));
    close(fd);
}
```

```c
// 安全代码：切换用户后再操作目标路径，使用 O_NOFOLLOW
void deliver_to_maildir_safe(const char *user, const char *mail_data) {
    struct passwd *pw = getpwnam(user);
    if (!pw) {
        log_error("Invalid user");
        return;
    }
    
    // 先切换权限
    if (initgroups(pw->pw_name, pw->pw_gid) != 0 ||
        setgid(pw->pw_gid) != 0 ||
        setuid(pw->pw_uid) != 0) {
        log_error("Privilege drop failed");
        _exit(1);  // 失败时立即终止，不继续以高权限运行
    }
    
    // 以目标用户权限构造并打开文件
    char path[256];
    snprintf(path, sizeof(path), "/var/mail/%s/Maildir/tmp/%lld.%d.%s",
             user, (long long)time(NULL), getpid(), gethostname());
    
    // O_NOFOLLOW 防止符号链接劫持
    int fd = open(path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if (fd < 0) {
        log_error("Failed to create mail file");
        return;
    }
    write(fd, mail_data, strlen(mail_data));
    close(fd);
}
```

### 漏洞模式 4：队列文件格式解析注入

```c
// 脆弱代码：队列文件以文本行存储，直接格式化输出
void queue_write_header(int fd, const char *sender, const char *recipient) {
    char buf[1024];
    // 如果 sender 包含换行符，队列文件格式被破坏
    snprintf(buf, sizeof(buf), "S:%s\nR:%s\n", sender, recipient);
    write(fd, buf, strlen(buf));
}

void queue_read_header(int fd) {
    char line[1024];
    while (fgets(line, sizeof(line), fd)) {
        if (line[0] == 'S') {
            // 解析 sender
            parse_sender(line + 2);
        } else if (line[0] == 'R') {
            // 解析 recipient
            parse_recipient(line + 2);
        }
        // 攻击者注入的伪造行被错误解析
    }
}
```

```c
// 安全代码：使用结构化存储 + 长度前缀 + 禁止换行
void queue_write_header_safe(int fd, const char *sender, const char *recipient) {
    // 拒绝包含控制字符（包括换行）的输入
    if (strpbrk(sender, "\r\n\0") || strpbrk(recipient, "\r\n\0")) {
        log_error("Invalid characters in queue address");
        return;
    }
    
    // 使用长度前缀的二进制格式，而非文本行
    uint32_t sender_len = strlen(sender);
    uint32_t recipient_len = strlen(recipient);
    write(fd, &sender_len, sizeof(sender_len));
    write(fd, sender, sender_len);
    write(fd, &recipient_len, sizeof(recipient_len));
    write(fd, recipient, recipient_len);
}
```

## 代码审计检查清单

| 审计区域 | 关键函数 / 模式 | 风险 | 检查方法 |
|---------|---------------|------|---------|
| 命令分发 | `switch(cmd)` / `dispatch[]` | 状态检查遗漏 | 确保每个分支都有前置状态校验 |
| MIME 解析 | `parse_mime()` 递归调用 | 栈溢出、边界混淆 | 检查深度限制和 boundary 匹配精度 |
| 队列文件 | `open()` / `mkdir()` 路径拼接 | 路径穿越、TOCTOU | 检查路径净化和 `O_NOFOLLOW` |
| 权限切换 | `setuid()` / `setgid()` | 提权、权限残留 | 确认切换时机和 `initgroups()` 配套 |
| SASL 接口 | `sasl_server_step()` 返回值 | 认证绕过 | 检查所有失败路径的状态回滚 |
| 字符串处理 | `strcpy`/`sprintf`/`strcat` | 缓冲区溢出 | 替换为 `strlcpy`/`snprintf` |
| 内存分配 | `malloc(len)` 来自外部输入 | 整数溢出 | 检查 `len` 的上下界 |
