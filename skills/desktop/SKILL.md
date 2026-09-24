---
description: 桌面混合应用领域安全第一性原理简报 (V7 Domain Intelligence Brief)
---

# 桌面应用领域地形情报 (Desktop Domain Brief)

> [!IMPORTANT]
> 在现代桌面应用（Electron / Tauri / CEF）的世界里，你面对的不是一个网页，也不是一个纯二进制。
> 你在对付一只“奇美拉（Chimera）”——它拥有 Web 技术的表皮，却连接着宿主操作系统的最高权限心脏。
> 你的身份不是找漏洞的前端小子，而是试图利用通信桥梁完成**跨维度逃逸（Escape）**的主机黑客。

## 第一性原理：黑客眼中的混合架构本质

混合架构的核心矛盾，是**不可信任的渲染呈现层 (Renderer)** 与 **拥有系统无上权力的主控宿主层 (Main Process / Core)** 之间的冲突。在这个架构下，真正的漏洞挖掘专家看到的是：

### 原理一：Web 漏洞的质变 (The Chimera Duality)
在纯 Web 环境下，XSS（跨站脚本）的上限是窃取 Cookie 或 CSRF。但在桌面应用中，**XSS 只是获取宿主底层 RCE（远程代码执行）的跳板**。
*   黑客首先寻找能执行任意 JS 的注射点。
*   随后，他们不偷数据，而是立刻探测环境：是否存在 `nodeIntegration: true`？如果存在，一个被执行的 `require('child_process').exec('calc')` 就能让 Web 漏洞瞬间质变为系统后门。

### 原理二：IPC 桥接的天真信任 (The Fallacy of the IPC Bridge)
因为沙箱隔离，渲染器不能随意写文件。它必须通过 IPC（进程间通信，如 `ipcRenderer.send`）向主进程打报告请求执行。
*   **黑客的视角**：主进程中的 `ipcMain.handle` 监听器，本质上是**暴露给不可信渲染器的无鉴权 RPC 接口**。
*   如果渲染器被攻陷，黑客会伪造 IPC 消息，例如发送 `['save-file', {path: '../../Startup/trojan.exe'}]`。如果主进程未经检查就盲目信任同源传递来的路径，隔离机制便宣告彻底崩溃。

### 原理三：预加载脚本越权与污染 (The Preload Treachery)
为了部分放权，开发者编写 `preload.js` 充当中间人暴露 API。
*   黑客寻找**“过度放权的 API”**。开发者可能无意中暴露了 `api.openExternal(url)`。黑客如果传入 `file:///C:/Windows/System32/cmd.exe`，就能唤起系统进程。
*   如果开发者漏配了 `contextIsolation`（上下文隔离），意味着预加载脚本的 V8 环境和渲染器页面的环境共享。黑客会使用前端的**原型链污染（Prototype Pollution）**，神不知鬼不觉地篡改预加载脚本内部依赖的方法，完成隐匿的沙箱逃逸。

### 原理四：领航权的劫持 (The Navigation Collapse)
应用总要加载外部链接。
*   如果应用的 `will-navigate` 或新窗口创建未做强校验，黑客可以将主渲染视图导航到 `http://attacker.com`。
*   一旦框架没有剥离新域名在这套视图下的特权，攻击者的网页就会莫名其妙地继承本属于官方界面的所有高权限预留通道。

### 原理五：协议处理引擎的隐形大门 (The Invisible URI Backdoor)
桌面应用通常会向 OS 注册自定义伪协议（如 `myapp://`）。
*   **0-Click 攻击面**：如果用户用浏览器访问一个含有了 `<iframe src="myapp://exec?cmd=bash">` 的攻击者网页，浏览器会在后台唤醒该桌面应用。
*   如果应用启动代码在解析此命令参数时发生注入且未经清洗进入了 `exec`，即实现了毫无感知的 RCE。

### 原理六：供应链更新的换心手术 (Supply Chain Replacement)
应用的自动更新端点至关重要。
*   如果 AutoUpdater 通过不安全的网络或只比对从同一个未受保护的服务器下载的 yaml/sha512 文件，而不做本地端强公钥签名校验（如 Ed25519 或 Apple/CodeSign 强制校验），这就是完美的投毒下发口。

---

## 你的行为准则
作为 huntzero，在此领域：
1. 取消你对待传统网页那种“测试输入框弹 alert(1)” 的心态。
2. 你的目光永远锁定在**状态与权限的跃迁边界**上。你要证明：一个在 DOM 里不起眼的流浪注入，如何一步步经过 Preload、越过 IPC、最终通过 Main 进程的执行点在目标操作系统上扎根！
