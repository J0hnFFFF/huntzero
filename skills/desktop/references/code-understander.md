---
description: 桌面应用代码理解 - 审计 Electron/Tauri 源码的直觉流
tags: [desktop, code-understander, audit, ipc, preload, context-isolation, csp]
---

# 桌面应用代码理解 (Code Understander): 像黑客一样审计混合架构源码

> 你阅读桌面应用源码时，不是在读业务逻辑，
> 你是在追踪**权限如何在渲染层、预加载层、主进程层、操作系统层之间流动**。

---

## 触发器：什么代码结构让审计专家立刻锁定关键区域？

### 触发器一：看到 `new BrowserWindow({ webPreferences: {...} })`

**你的第一眼反应**："这是整个应用安全模型的根基，先看配置再看代码。"

`webPreferences` 决定了渲染进程的基础权限。任何错误配置都会向下传导至所有业务代码。

**立即追问链**：
1. `nodeIntegration` 是 `true` 还是 `false`？如果是 `true`，所有后续 DOM XSS 都是 RCE。
2. `contextIsolation` 是 `true` 还是 `false`？如果是 `false`，preload 脚本与渲染页面共享上下文，原型污染可篡改 API。
3. `sandbox` 是否开启？`sandbox: true` 时即使 `nodeIntegration: false`，renderer 仍受 Chromium 沙箱保护。
4. `preload` 脚本路径是什么？该脚本中暴露了哪些 API？这些 API 的参数校验逻辑在哪里？
5. `allowRunningInsecureContent` 和 `webSecurity` 的值是什么？它们决定是否允许加载 HTTP 内容或跨域请求。
6. 是否使用了 `webview`？`webview` 的 `webpreferences` 属性是否独立设置？是否比主窗口更宽松？

### 触发器二：看到 `contextBridge.exposeInMainWorld('api', {...})`

**你的第一眼反应**："每一个暴露的方法都是潜在的 RPC 入口，要看它背后调用了什么系统 API。"

`contextBridge` 是 renderer 与 main 之间的官方桥梁。审计的重点是桥的另一端。

**立即追问链**：
1. 暴露的 API 名称空间是什么？`window.api` 还是 `window.electron`？是否容易被猜测和遍历？
2. 每个暴露的方法在 `ipcMain` 中的 handler 实现是什么？是否做了参数校验？
3. 暴露的方法是否返回 Promise？Promise 的 resolve 值是否包含敏感信息？
4. 是否存在通过多个 API 调用组合实现危险操作的可能？如 `readFile` + `writeFile` 实现复制到任意路径。
5. preload 脚本是否引入了第三方 npm 包？这些包的版本是否存在已知漏洞？
6. preload 脚本是否处理了错误？错误信息是否泄露了文件系统结构或内部路径？

### 触发器三：看到 `ipcMain.handle` / `ipcMain.on` 的定义

**你的第一眼反应**："这些 handler 是主进程的软肋，因为它们必须假设所有输入都是恶意的。"

主进程的 IPC handler 是沙箱逃逸后的首要目标。审计重点是输入验证和权限控制。

**立即追问链**：
1. handler 的第一个参数 `event` 是否被使用？`event.sender` 或 `event.frameId` 是否用于来源校验？
2. handler 是否区分了不同 renderer 的来源？如主窗口 vs 弹窗 vs webview？
3. 参数解构后是否立即进行 `typeof` 和范围校验？还是直接传入后续函数？
4. handler 中是否使用了 `dialog.showOpenDialog` 或 `dialog.showSaveDialog`？用户选择的结果是否被后续逻辑盲目信任？
5. handler 的返回值是否包含绝对路径、环境变量、或密钥？这些信息是否可以被恶意 renderer 读取？
6. 是否存在异步操作中的竞态条件？如 handler 开始处理后被导航事件中断，导致状态不一致。

### 触发器四：看到 `protocol.registerFileProtocol` 或 `app.setAsDefaultProtocolClient`

**你的第一眼反应**："协议处理代码在主进程中的位置在哪里？参数解析是手动字符串操作吗？"

自定义协议的处理逻辑往往涉及原始字符串解析，容易引入命令注入和路径穿越。

**立即追问链**：
1. 协议 handler 接收到的 URL 是否经过 `new URL(url)` 解析？还是手动 `split('?')` 和 `split('=')`？
2. URL 参数值是否经过 `decodeURIComponent`？解码前后的长度是否一致？
3. 参数值是否直接拼接进文件路径或命令字符串？如 `path.join(base, param)` 或 `` `cmd ${param}` ``
4. 协议 handler 是否返回了本地文件？返回的文件路径是否做了基目录限制？
5. 是否可以通过协议参数触发 IPC 调用？如 `myapp://action?ipc=save-file&path=...`
6. 协议注册是否在安装时完成？如果用户从旧版本升级，是否残留了旧的、不安全的协议处理逻辑？

### 触发器五：看到 `Content-Security-Policy` 或 `<meta>` 标签配置

**你的第一眼反应**："CSP 是渲染层的最后一道防线，看它有没有被故意放宽。"

CSP 在桌面应用中的作用常被误解。实际上，它可以有效阻止 XSS 载荷的执行。

**立即追问链**：
1. CSP 策略字符串是什么？`default-src 'self'` 是最安全的起点，任何 `'unsafe-inline'` 或 `*` 都是红旗。
2. CSP 是在 HTTP 头中设置还是在 `<meta>` 标签中设置？某些框架可能覆盖了其中之一。
3. 是否允许 `eval` 或 `Function` 构造器？`'unsafe-eval'` 会使基于字符串的 XSS 载荷更容易执行。
4. 是否允许加载外部脚本（`https://*`）？攻击者可以托管恶意脚本并注入引用。
5. `blob:` 和 `data:` URI 是否被允许？它们可用于创建内联 Worker 或 iframe 绕过限制。
6. 是否配置了 `upgrade-insecure-requests`？在桌面应用中，这可能强制外部 HTTP 请求升级为 HTTPS，但也可能暴露网络行为。

---

## 攻击链闭合：从源码审计到漏洞确认的完整流程

```
[审计 BrowserWindow 配置]
    → [发现 nodeIntegration: false, contextIsolation: true → XSS 不能直接 RCE]
    → [审计 preload.js]
    → [发现暴露了 api.openFile(path) 方法]
    → [审计 ipcMain.handle('open-file')]
    → [发现 path 参数仅做了 typeof === 'string' 检查，无路径规范化]
    → [确认可通过 IPC 传入 '../../../etc/passwd' 读取任意文件]
    → [如果同时存在 XSS，可通过 XSS 伪造 IPC 消息调用此 handler]
    → [攻击链闭合：XSS → IPC 伪造 → 任意文件读取 → 信息泄露 / 进一步利用]
```

**闭合条件**：渲染层可控 + 存在 IPC 通道 + 通道缺乏输入校验 + 通道执行敏感操作。

---

## 典型代码模式与警觉点

### 脆弱模式：宽松的 webPreferences
```javascript
// main.js - 脆弱配置
const win = new BrowserWindow({
    width: 800, height: 600,
    webPreferences: {
        nodeIntegration: true,        // ← 渲染层获得 Node API
        contextIsolation: false,      // ← 无隔离
        webSecurity: false            // ← 允许跨域
    }
});
win.loadURL('https://example.com');  // ← 外部网页拥有 Node 权限
```

### 安全模式：最小权限 webPreferences
```javascript
// main.js - 安全配置
const win = new BrowserWindow({
    width: 800, height: 600,
    webPreferences: {
        nodeIntegration: false,
        contextIsolation: true,
        sandbox: true,
        preload: path.join(__dirname, 'preload.js'),
        webSecurity: true
    }
});
```

### 脆弱模式：preload 过度暴露
```javascript
// preload.js - 脆弱：暴露文件系统 API
const { contextBridge, ipcRenderer } = require('electron');
contextBridge.exposeInMainWorld('fs', {
    readFile: (path) => ipcRenderer.invoke('fs:read', path),
    writeFile: (path, data) => ipcRenderer.invoke('fs:write', path, data),
    deleteFile: (path) => ipcRenderer.invoke('fs:delete', path)
});
// ← 渲染层可通过 window.fs.readFile('/etc/passwd') 读取任意文件
```

### 安全模式：preload 最小暴露
```javascript
// preload.js - 安全：仅暴露必要且受控的 API
const { contextBridge, ipcRenderer } = require('electron');
contextBridge.exposeInMainWorld('api', {
    openDocument: () => ipcRenderer.invoke('app:open-document'),
    saveDocument: (data) => ipcRenderer.invoke('app:save-document', data)
});
// 路径选择由主进程的 dialog 完成，渲染层不直接控制路径
```

### 脆弱模式：IPC handler 未校验来源
```javascript
// main.js - 脆弱：未校验消息来源
ipcMain.handle('app:save-document', async (event, data) => {
    const win = BrowserWindow.getFocusedWindow();
    const { filePath } = await dialog.showSaveDialog(win);
    fs.writeFileSync(filePath, data);  // ← 任何 renderer 都可触发保存
});
```

### 安全模式：IPC handler 校验来源
```javascript
// main.js - 安全：校验发送者身份
const TRUSTED_ORIGINS = new Set(['https://app.example.com']);
ipcMain.handle('app:save-document', async (event, data) => {
    const senderUrl = new URL(event.sender.getURL());
    if (!TRUSTED_ORIGINS.has(senderUrl.origin)) {
        throw new Error('untrusted origin');
    }
    // ... 后续处理
});
```

---

## 输出要求

1. **关键配置审计表**：`webPreferences`、CSP、协议注册、更新配置的逐项审计结果。
2. **API 暴露清单**：preload 暴露的所有方法及其对应的 IPC handler 位置。
3. **输入验证矩阵**：每个 IPC handler / 协议 handler 的参数校验覆盖情况。
4. **权限跃迁图**：渲染层 → preload → IPC → 主进程 → 操作系统 的权限流动路径。
