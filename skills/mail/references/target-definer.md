---
description: 邮件系统目标定义 - 攻击面深度测绘
---

# 邮件系统目标定义 (Mail Target Definer)

深度测绘邮件系统的攻击面层次结构。

## 攻击面分层模型

### Layer 0: Pre-Auth 网络协议面 (最高优先级)
无需任何认证即可触达的代码路径：
- **SMTP 接收器**: 监听 25/587/465 端口的代码，从 TCP 连接建立到 `EHLO` 命令处理
- **STARTTLS 握手**: TLS 协商期间的代码路径（降级攻击、命令走私窗口）
- **IMAP/POP3 Banner**: 连接后立即暴露的协议响应代码
- **识别方法**: 搜索 socket accept → 第一个 read → 命令分发的完整链路

### Layer 1: 邮件内容解析面
由外部完全控制的输入（邮件本身）驱动：
- **MIME 多部分解析器**: 递归解析 multipart/mixed, multipart/alternative
- **编码解码器**: Base64, Quoted-Printable, UUencode, yEnc
- **字符集转换器**: charset 声明到内部 Unicode 的转换
- **附件格式处理**: TNEF (winmail.dat), iCal/vCard, S/MIME
- **识别方法**: 搜索 `Content-Type` / `boundary` / `charset` / `Content-Transfer-Encoding` 处理代码

### Layer 2: WebMail 应用面
HTTP 暴露的 Web 层：
- **HTML 邮件渲染**: sanitizer 实现、CSP 配置
- **附件预览**: 服务器端渲染 PDF/Office/图片的代码路径
- **搜索功能**: IMAP SEARCH 命令构造、全文索引查询
- **管理接口**: 用户管理、域管理、全局设置 API
- **识别方法**: 搜索 HTTP 路由表、API 端点定义

### Layer 3: 投递与存储面
邮件落盘和转发的后端逻辑：
- **LDA (Local Delivery Agent)**: 权限切换、Maildir 路径构造
- **过滤引擎**: Sieve 规则解析与执行、SpamAssassin 调用
- **队列管理**: 队列文件格式、bounce 生成逻辑
- **外部集成**: LDAP 认证查询、数据库存储、防病毒调用
- **识别方法**: 搜索 `deliver`, `pipe`, `forward`, `filter`, `sieve` 相关代码

## 组件识别清单

对目标项目执行以下侦察：

1. **入口文件定位**: `main()` / 服务启动函数 → 追踪到 socket listener
2. **协议处理器定位**: 搜索 `EHLO` / `MAIL FROM` / `RCPT TO` / `DATA` / `LOGIN` / `SELECT` 等协议关键词
3. **解析器定位**: 搜索 `mime` / `boundary` / `multipart` / `content-type` / `base64` / `quoted-printable`
4. **权限切换点**: 搜索 `setuid` / `setgid` / `seteuid` / `chroot` / `drop_privileges`
5. **外部命令调用**: 搜索 `exec` / `popen` / `system` / `pipe` / `fork` — 特别关注参数来源
6. **配置解析**: 搜索配置文件读取代码 — 识别可热加载的配置项和致命的错误配置

## 优先级矩阵

| 攻击面 | 触达条件 | 典型影响 | 优先级 |
|--------|---------|---------|--------|
| Pre-auth SMTP 解析 | 网络直达 | RCE | 🔴 最高 |
| MIME 解析器 | 发送邮件 | RCE / 内容注入 | 🔴 最高 |
| WebMail XSS/SSRF | 发送邮件 + 受害者查看 | 会话劫持 / SSRF | 🟠 高 |
| IMAP SEARCH 注入 | 认证用户 | 数据泄露 / 注入 | 🟡 中 |
| Sieve 规则注入 | 认证用户 | 命令执行 / 文件写入 | 🟡 中 |
| 管理接口漏洞 | 管理员 | 全系统控制 | 🟡 中 |

## 输出

- **攻击面地图**: 按 Layer 0-3 分层的组件清单
- **入口点列表**: 每个可触达的代码入口 + 文件:行号
- **优先级排序**: 基于触达条件 × 影响 的优先级排名
