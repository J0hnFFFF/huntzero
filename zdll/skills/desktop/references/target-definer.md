---
description: 桌面应用目标定义 - 专家看到 Electron/Tauri 时的第一直觉
tags: [desktop, target-definer, attack-surface, electron, tauri, ipc, preload]
---

# 桌面应用目标定义 (Target Definer): 专家看到桌面应用时的第一直觉

> 桌面应用不是网页，也不是纯二进制。
> 它是穿着 Web 外衣的奇美拉，拥有系统级权限的心脏。
> 你的任务是标记出从渲染层到系统层的每一条权限跃迁边界。

---

## 触发器：什么景象让桌面应用专家立刻警觉？

### 触发器一：看到 `nodeIntegration: true` 或 `contextIsolation: false`

**你的第一眼反应**："渲染进程可以直接访问 Node.js API，XSS = RCE。"

Electron 的配置项是安全的第一道防线。错误配置意味着 Web 层的任何注入点都是系统级入口。

**立即追问链**：
1. `nodeIntegration` 是全局开启还是仅针对特定 `BrowserWindow`？后者是否可被绕过？
2. `contextIsolation` 关闭后，预加载脚本的 `window` 对象是否与渲染页面共享？
3. 是否同时存在 `allowRunningInsecureContent: true`？这意味着 HTTPS 页面可加载 HTTP 恶意脚本。
4. `sandbox` 选项是否开启？即使 `nodeIntegration: false`，未沙箱的 renderer 仍可通过 IPC 利用主进程。
5. 是否使用了 `webview` 标签？`webview` 的 `nodeintegration` 属性是否单独配置？
6. 开发者是否使用了 `nodeIntegrationInSubFrames: true`？iframe 中的注入同样获得 Node 权限。

### 触发器二：看到 `ipcMain.handle` / `ipcRenderer.invoke` 模式

**你的第一眼反应**："这是暴露给不可信渲染进程的无鉴权 RPC 接口。"

IPC 是桌面应用的阿喀琉斯之踵。任何未经验证的 IPC 消息都可能是沙箱逃逸的跳板。

**立即追问链**：
1. IPC handler 是否校验了消息发送者的身份？（`event.senderFrame` / `event.sender`）
2. IPC 通道名是否可预测？如 `app:open-file` 是否可被任意 renderer 调用？
3. IPC 参数是否经过严格的类型和范围验证？还是直接解构后传入系统 API？
4. 是否存在 "万能 IPC" 通道？如 `execute` 通道直接执行传入的命令字符串。
5. IPC 返回值是否包含敏感信息？如文件内容、环境变量、密钥，可被泄露给恶意 renderer。
6. 是否使用了 `ipcMain.on` 而非 `ipcMain.handle`？`on` 模式下的异步回复是否可被伪造？

### 触发器三：看到自定义协议注册（`protocol.registerFileProtocol` / OS URI Handler）

**你的第一眼反应**："操作系统级别的 0-Click 攻击面。浏览器 iframe 可以直接唤醒应用。"

自定义协议（`myapp://`）将攻击面从应用本身扩展到了整个操作系统和浏览器生态。

**立即追问链**：
1. 应用向 OS 注册了哪些自定义 URI scheme？（检查 Info.plist、注册表、.desktop 文件）
2. 协议 URL 的参数是否经过严格的校验和清洗？是否直接传入 `shell.openPath` 或 `exec`？
3. 协议处理代码是在主进程还是渲染进程中执行？渲染进程中执行意味着二次利用。
4. 是否支持通过协议触发敏感 IPC 通道？如 `myapp://action?type=save-file&path=/etc/passwd`
5. 浏览器访问包含 `<iframe src="myapp://...">` 的页面时，应用是否自动激活？
6. 多个应用是否注册了相同的协议 scheme？（协议劫持 / hijacking）

### 触发器四：看到 `preload.js` 暴露 API

**你的第一眼反应**："preload 是渲染层与主进程的桥梁，桥梁上的每块木板都可能是短板。"

预加载脚本的 API 设计决定了即使 `nodeIntegration: false`，渲染层仍然能做什么。

**立即追问链**：
1. preload 暴露了哪些 API？`openExternal`、`readFile`、`writeFile`、`execute`？
2. 这些 API 的参数是否做了白名单限制？如 `openExternal` 是否只允许 http/https？
3. preload 是否通过 `contextBridge.exposeInMainWorld` 暴露 API？还是直接修改 `window`？
4. 如果 `contextIsolation: false`，渲染层是否可以通过原型链污染（Prototype Pollution）篡改 preload API？
5. preload 脚本中是否引入了第三方 npm 包？这些包是否已被审计？
6. preload 是否缓存了主进程返回的数据？缓存是否可能被污染或篡改？

### 触发器五：看到自动更新机制（`autoUpdater` / `checkForUpdates`）

**你的第一眼反应**："更新通道是完美的供应链投毒入口，没有签名校验就等于裸奔。"

桌面应用的自动更新器往往被忽视，但它拥有写入应用二进制和系统启动项的最高权限。

**立即追问链**：
1. 更新包是否经过代码签名（Code Signing）验证？使用的是哪种签名算法？
2. 更新元数据（如 latest.yml、RELEASES 文件）是否通过 HTTPS 获取？是否校验证书 pinning？
3. 更新服务器是否可枚举？如 `https://update.example.com/latest.yml` 是否可被中间人替换？
4. 是否存在降级攻击（Rollback）防护？攻击者能否让客户端安装旧版本已知漏洞？
5. 更新包下载后、安装前，是否校验哈希（SHA256）与签名的一致性？
6. 更新过程是否需要用户确认？静默更新是否意味着任何拥有更新服务器权限的人都可以下发恶意代码？

---

## 攻击面地图：桌面应用全链条审计清单

| 链条环节 | 目标位置 | 审计焦点 | 典型漏洞 |
|---------|---------|---------|---------|
| **渲染层注入** | DOM XSS、URL 参数、LocalStorage | 不可信数据进入 innerHTML、eval | XSS |
| **Node 权限** | `nodeIntegration`、`sandbox` | 渲染进程是否可直接访问 fs/child_process | XSS→RCE |
| **Preload 桥梁** | `preload.js`、`contextBridge` | API 暴露范围、参数校验、原型污染 | 权限提升 |
| **IPC 通道** | `ipcMain.handle` / `ipcRenderer.send` | 消息鉴权、参数校验、敏感信息泄露 | IPC 伪造→RCE |
| **导航边界** | `will-navigate`、`new-window` | URL 校验、特权继承、打开外部链接 | 导航劫持 |
| **协议处理** | OS URI Handler、`protocol.register` | 参数清洗、协议劫持、命令注入 | 0-Click RCE |
| **更新机制** | `autoUpdater`、签名验证 | HTTPS、签名、哈希校验、降级防护 | 供应链投毒 |
| **文件系统** | `dialog.showOpenDialog`、拖放 | 路径遍历、符号链接、恶意文件解析 | LFI / RCE |

---

## 攻击链闭合：从网页注入到系统 RCE 的完整证明

```
[攻击者构造恶意网页 / 本地 HTML 文件]
    → [触发 XSS：innerHTML / eval / URL hash 注入点]
    → [探测 nodeIntegration：typeof require !== 'undefined']
    → [如果 nodeIntegration:true：require('child_process').exec('calc')]
    → [如果 nodeIntegration:false：通过 IPC 伪造调用 preload 暴露的 API]
    → [IPC 调用 'open-file' 并传入 '../../etc/crontab']
    → [主进程盲目信任，向系统文件写入攻击者内容]
    → [系统级 RCE]
```

**闭合条件**：渲染层存在注入点 + 存在权限跃迁路径（Node 或 IPC） + 主进程缺乏校验。

---

## 典型代码模式与警觉点

### 脆弱模式：过度放权的 Electron 配置
```javascript
// 脆弱：nodeIntegration 开启，contextIsolation 关闭
const win = new BrowserWindow({
    webPreferences: {
        nodeIntegration: true,        // ← XSS 直接获得 Node API
        contextIsolation: false,      // ← 渲染层可污染 preload
        sandbox: false,               // ← 无沙箱
        allowRunningInsecureContent: true  // ← 可加载 HTTP 内容
    }
});
```

### 安全模式：最小权限 Electron 配置
```javascript
// 安全：最小权限原则
const win = new BrowserWindow({
    webPreferences: {
        nodeIntegration: false,
        contextIsolation: true,       // ← preload 与渲染层隔离
        sandbox: true,                // ← 启用 Chromium 沙箱
        preload: path.join(__dirname, 'preload.js'),
        allowRunningInsecureContent: false
    }
});
```

### 脆弱模式：未经验证的 IPC Handler
```javascript
// 脆弱：直接信任 IPC 参数
ipcMain.handle('file:write', async (event, filePath, content) => {
    await fs.promises.writeFile(filePath, content);  // ← 路径穿越
    return 'ok';
});
```

### 安全模式：严格校验的 IPC Handler
```javascript
// 安全：白名单 + 路径规范化
const ALLOWED_DIR = path.resolve('/app/safe_writes');
ipcMain.handle('file:write', async (event, filePath, content) => {
    const resolved = path.resolve(ALLOWED_DIR, filePath);
    if (!resolved.startsWith(ALLOWED_DIR + path.sep)) {
        throw new Error('path traversal detected');
    }
    await fs.promises.writeFile(resolved, content);
    return 'ok';
});
```

---

## 输出要求

1. **攻击面地图**：标记所有渲染层注入点、Node/Preload/IPC 权限边界、协议与更新入口。
2. **配置审计清单**：`webPreferences` 的每个安全选项及其当前值。
3. **高价值目标排序**：按 XSS→RCE 最短路径排序的代码位置和函数列表。
4. **攻击链草图**：从初始注入到最终系统控制的最小步骤图。
