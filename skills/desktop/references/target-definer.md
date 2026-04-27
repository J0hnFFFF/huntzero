---
description: 桌面应用目标定义 - 勾勒沙箱边界与 IPC 命脉
---

# 目标定义者 (Target Definer): 绘制跨维度逃逸图景

不要把精力浪费在审查常规的 HTTP 网络请求。在研究一个混合桌面应用 (Electron等) 时，第一步是框定“主进程（Main）”与“渲染层（Renderer）”的警戒线。

## 专家的审计锁定点

你必须精准查找并在代码中定位以下“高压红线”：

### 1. 甄别沙箱核心防线 (The Sandbox Bastions)
*   全局检索创建主窗口的代码（如 `new BrowserWindow`、Tauri 的 `Builder`）。
*   审视核心布尔值开关，这是决定我们能否**降维打击**的关键因素：
    *   `nodeIntegration`: 是 `true` 还是 `false`？
    *   `contextIsolation`: 是 `true` 还是 `false`？
    *   `sandbox`: 是否显式地被关闭？
    *   *只要其中有任何一个未采纳最佳实践，漏洞利用成本将呈断崖式下跌。*

### 2. 枚举 IPC 暴露锚点 (The IPC Handlers)
*   全局提取所有的 `ipcMain.on`, `ipcMain.handle` 或 Tauri 的 `#[tauri::command]` 装饰器。
*   建立清单：梳理哪些 IPC 接口底层绑定了 `fs.*` (文件系统), `child_process.exec` (命令执行), 或者向操作系统的全局广播接口。这些就是你的弹药库。

### 3. 勘探暴露于外网的零点击钩子 (The URI Schemes)
*   扫描代码中注册操作：`app.setAsDefaultProtocolClient` 或者 `protocol.register...`。
*   这决定了你能不能通过在外部网页简单挂载 `myapp://` 来唤起这个程序。

## 你的目标输出
建立一张明确的沙箱架构全景图：
**[Renderer (带有什么等级的 Node 特权?)]**  <==== IPC 互通 ====>  **[Main Process (有什么高危的后门处理函数?)]**。
并标记出可能成为最终逃逸枢纽的文件与代码行。
