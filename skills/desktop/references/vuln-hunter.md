---
description: 客户端漏洞挖掘 - 文件格式解析与沙箱逃逸的第一性原理追踪
---

# 客户端漏洞挖掘 (Desktop Vuln Hunter)

不止于 Electron XSS → RCE。覆盖原生应用的文件格式解析、沙箱逃逸和特权 API 滥用。

## 核心分析框架

### 原理一：文件格式解析器是最大的 Pre-Auth 攻击面
**关键问题**：用户打开一个文件的动作，触发了多少行不可信输入驱动的代码？

**追踪路径**：
- 识别所有文件格式解析入口（PDF/Office/图片/字体/音视频）
- 每个解析器中追踪：外部输入 → 内存分配大小的控制链
- 检查整数溢出：`size = width * height * channels` 是否有乘法溢出校验
- 检查堆操作：解析器是否信任文件中声明的长度字段来分配/拷贝内存
- 检查递归/嵌套深度限制：嵌套的对象引用、嵌套的容器格式
- **核心方法**：找到文件中一个可控字段，它被用作 `malloc(size)` 或 `memcpy(dst, src, size)` 的参数
- 验证：能否构造一个文件，使解析器分配过小缓冲区然后写入过多数据？

### 原理二：渲染引擎中的脚本执行
**关键问题**：文档格式是否支持内嵌脚本？脚本的执行上下文有什么权限？

**追踪路径 (PDF 专项)**：
- 追踪 Document-Level JavaScript 的加载和执行路径
- 枚举所有可从 JS 调用的 API（特别是: `app.launchURL`, `this.submitForm`, `this.exportDataObject`, `util.printf`）
- 检查特权 API 与非特权 API 之间的边界（`ANShareFile` 类型混淆等）
- 分析 Action 触发器：`/OpenAction`, `/AA` (Additional Actions), `/PointerDown` 等
- 检查跨文档引用：`/GoToR`, `/Launch`, `/URI` action 的 URL 处理

**追踪路径 (Office 专项)**：
- VBA Macro 的自动执行条件 (`Auto_Open`, `Document_Open`)
- OLE 对象嵌入和链接 — 外部引用是否自动获取
- OOXML 的 External Relationship (`Target="http://..."`) SSRF 可能性

### 原理三：沙箱边界与 IPC 通道
**关键问题**：应用的沙箱隔离了什么？IPC 通道传递的消息内容是否被信任？

**追踪路径 (Electron/Tauri)**：
- `nodeIntegration` / `contextIsolation` / `sandbox` 配置检查
- `preload.js` 暴露的所有 API — 每一个都是潜在的沙箱逃逸点
- `ipcMain.handle` / `ipcMain.on` 的所有处理器 — 参数验证
- `shell.openExternal` / `shell.openPath` 的 URL 参数过滤
- `protocol.registerFileProtocol` 自定义协议处理 — 路径穿越

**追踪路径 (原生沙箱)**：
- Chromium Mojo IPC 接口的验证逻辑
- 进程间共享内存区域的访问控制
- Broker 进程对 Renderer 请求的过滤策略

### 原理四：协议处理器 (URI Handler) 的 0-Click 入口
**关键问题**：注册到 OS 的自定义协议，能否从浏览器直接触发且无用户确认？

**追踪路径**：
- 搜索 `registerAsDefaultProtocolClient` / Windows Registry `HKCU\Software\Classes` / macOS `CFBundleURLTypes`
- 追踪协议 URL 进入应用后的解析路径
- 检查 URL 参数是否进入 `exec` / `spawn` / `open` 调用
- 分析参数注入：`myapp://action?file=../../etc/passwd` 或 `myapp://--gpu-launcher=cmd.exe`

### 原理五：自动更新机制的信任链
**关键问题**：更新包的来源和完整性如何验证？MITM 能否替换更新？

**追踪路径**：
- 更新 URL 是否使用 HTTPS 且 pin 了证书
- 更新包的签名验证：是否只验证 hash（不够）还是验证数字签名
- Hash/签名文件和更新包是否从同一来源获取（同源 = 无保护）
- `electron-updater` 的 `allowDowngrade` 配置 — 降级攻击
- 更新的 `yml` 清单文件是否可被篡改

## 高价值目标优先级

```
文件格式解析器 (最高 - 0-click 入口, 内存破坏)
     ↓
文档内嵌脚本引擎 (高 - 逻辑漏洞, 特权 API 滥用)
     ↓
IPC/沙箱边界 (高 - 逃逸后提权)
     ↓
URI 协议处理器 (高 - 浏览器 → 桌面的跨界入口)
     ↓
自动更新机制 (中 - 供应链级别影响)
     ↓
Electron preload API (中 - XSS → RCE 跳板)
```

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [解析器溢出/脚本执行逃逸/IPC信任/协议处理器注入/更新劫持]
- 攻击链: [文件打开 → 解析 → [缓冲区溢出/JS执行] → 代码执行]

### 脆弱路径
- 入口: [文件格式/协议URL/IPC消息]
- 控制点: [哪个文件字段/URL参数/IPC参数被攻击者控制]
- Sink: [malloc/memcpy/exec/eval — 危险操作]

### 验证思路
[构造最小 PoC 文件/URL 触发漏洞]

### 危害本质
- 触发条件: [打开文件/点击链接/自动(0-click)]
- 影响范围: [沙箱内/沙箱外/系统权限]
```
