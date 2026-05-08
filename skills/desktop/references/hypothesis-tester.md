---
description: 桌面应用假设测试 - 验证桌面应用安全假设的实验方法
tags: [desktop, hypothesis-tester, fuzzing, ipc, xss, protocol, updater]
---

# 桌面应用假设测试 (Hypothesis Tester): 用实验推翻安全声明

> 每一个 "我们已经隔离了渲染层" 的声明，都是等待被证伪的假设。
> 你的任务是用最小成本、最高确定性的实验，戳破这些安全幻觉。

---

## 触发器：什么声明让测试专家立刻设计实验？

### 触发器一：看到"已禁用 nodeIntegration"

**你的第一眼反应**："真的完全禁用了吗？子窗口、iframe、webview、new-window 是否继承了相同的配置？"

`nodeIntegration` 可能在主窗口被禁用，但在弹出窗口、`<webview>` 或 `window.open` 中重新开启。

**立即追问链**：
1. 在 DevTools Console 中执行 `typeof require`，返回值是否为 `"undefined"`？
2. 打开应用中的弹窗或二级窗口，再次执行 `typeof require`，结果是否一致？
3. 应用中是否存在 `<webview>` 标签？其 `nodeintegration` 属性是否设置为 `on`？
4. 通过 `window.open` 打开的新窗口，其 `webPreferences` 是继承自父窗口还是使用了默认值？
5. 是否存在通过 `<iframe>` 加载的第三方内容？iframe 的权限是否受 `nodeIntegrationInSubFrames` 控制？
6. 是否可以通过导航（`location.href = 'javascript:alert(typeof require)'`）在现有窗口中探测？

### 触发器二：看到"IPC 通道已做鉴权"

**你的第一眼反应**："鉴权的具体实现是什么？伪造来源是否可行？"

IPC 鉴权往往依赖于 `event.sender` 或 `event.frameId`，但这些标识可能被伪造或在特定条件下失效。

**立即追问链**：
1. 鉴权逻辑是基于 `event.sender.getURL()` 还是固定白名单？URL 是否可被 DNS 劫持或页面内篡改？
2. 如果鉴权使用 `event.frameId`，是否测试了通过 `iframe` 内嵌可信页面后发送跨 frame 消息？
3. 是否可以从 preload 脚本中直接调用 `ipcRenderer.send`？如果 preload 被污染，鉴权是否失效？
4. 如果应用有多个 renderer 进程（如多窗口），一个被攻破的窗口是否可以向所有通道发送消息？
5. 使用 frida 或 lldb 附加到 renderer 进程，是否可以直接调用 `ipcRenderer.send` 而绕过前端逻辑？
6. IPC 消息是否可被重放？捕获一次合法消息后，在无交互情况下重复发送是否仍被接受？

### 触发器三：看到"自定义协议参数已清洗"

**你的第一眼反应**："清洗逻辑是针对标准输入还是所有可能的编码变形？"

协议参数清洗往往只覆盖最常见的情况，URL 编码、Unicode 规范化、大小写变换、空字节都可能绕过。

**立即追问链**：
1. 协议 handler 中是否使用了 `decodeURIComponent`？如果输入已经是解码后的字符串，双重解码会发生什么？
2. 是否测试了 URL 编码变形？如 `%2e%2e%2f` 对应 `../`，`%252e` 对应双重编码。
3. 是否测试了空字节注入？`myapp://open?file=secret.txt%00.exe` 在某些字符串处理中会被截断。
4. 协议参数是否经过 Unicode 规范化？如 `ℂ`（U+2102）在某些文件系统中对应 `C`。
5. 是否可以通过协议参数注入换行符（`%0a`、`%0d`）或引号（`%22`、`%27`）？
6. 如果协议触发文件操作，是否测试了符号链接（symlink）？`file` 参数指向一个指向 `/etc/passwd` 的 symlink 会怎样？

### 触发器四：看到"自动更新已签名验证"

**你的第一眼反应**："签名验证覆盖了哪些文件？增量更新、配置文件、脚本是否也在验证范围内？"

签名验证往往只覆盖主二进制文件，而周边的配置文件、脚本或插件目录可能被忽略。

**立即追问链**：
1. 更新包中哪些文件经过了签名验证？是否包括 DLL/SO 依赖、配置文件、资源文件？
2. 签名验证失败时，更新器是拒绝安装还是回退到旧版本？回退逻辑是否安全？
3. 是否可以通过 DNS 劫持或 hosts 篡改将更新请求重定向到攻击者服务器？
4. 更新服务器是否使用 HTTPS？是否校验证书？是否支持自签名证书？
5. 是否存在本地缓存的更新包？如果攻击者替换本地缓存，更新器是否会直接安装？
6. 更新过程中是否会执行 PowerShell/Bash 脚本？这些脚本是否也经过签名？

### 触发器五：看到"CSP 已配置阻止 XSS"

**你的第一眼反应**："CSP 是否可以被 HTML meta 标签覆盖？是否存在 CSP bypass 的脚本加载点？"

CSP 在桌面应用中可能因为本地文件加载、`file://` 协议或 `<meta>` 标签而被削弱或绕过。

**立即追问链**：
1. 应用的 CSP 是在 HTTP 响应头中设置，还是在 HTML 的 `<meta>` 标签中设置？后者优先级更低且可被覆盖。
2. 是否允许 `file://` 协议的脚本加载？桌面应用中本地文件往往被视为同源。
3. 是否允许 `blob:` 或 `data:` URI？攻击者可以构造 `data:text/html,<script>alert(1)</script>` 并加载。
4. 是否存在通过 `<object>`、`<embed>` 或 `<applet>` 标签加载不受限制内容的路径？
5. 是否允许 `javascript:` 伪协议？如 `<a href="javascript:alert(1)">` 是否可执行？
6. 如果应用内嵌了第三方 iframe（如 YouTube、地图），这些 iframe 的 CSP 是否独立于主页面？

---

## 攻击链闭合：从假设到证伪的实验流程

```
[假设：nodeIntegration 已全局禁用]
    → [实验 1：主窗口 DevTools 执行 typeof require → undefined]
    → [实验 2：打开应用内置的 "设置" 弹窗，执行 typeof require → function]
    → [证伪：设置弹窗的 webPreferences 独立配置，nodeIntegration 为 true]
    → [实验 3：在设置弹窗中执行 require('child_process').exec('calc')]
    → [结果：计算器弹出，假设彻底证伪]
    → [最小复现：一段 3 行 JavaScript，在设置弹窗中运行即可 RCE]
```

**闭合条件**：实验设计可复现 + 结果明确推翻假设 + 最小复现路径清晰。

---

## 典型代码模式与警觉点

### 脆弱测试：仅检查主窗口
```javascript
// 脆弱：测试只验证了主窗口
console.log("Main window require:", typeof require);  // undefined
// 但未检查弹窗、iframe、webview
```

### 安全测试：系统化权限探测
```javascript
// 安全：遍历所有可能的执行环境
function probeNodeIntegration() {
    const contexts = [
        () => typeof require,
        () => { const w = window.open('about:blank'); return w && typeof w.require; },
        () => Array.from(document.querySelectorAll('webview')).map(v => v.getWebContentsId()),
        () => { try { return eval('typeof require'); } catch(e) { return 'blocked'; } }
    ];
    return contexts.map((fn, i) => ({ context: i, result: fn() }));
}
```

### 脆弱测试：仅发送合法 IPC 消息
```javascript
// 脆弱：只测试了正常流程
window.api.saveFile('/tmp/test.txt', 'hello');  // 正常执行
// 未测试路径穿越、类型混淆、参数缺失
```

### 安全测试：IPC 模糊测试
```javascript
// 安全：系统化变异 IPC 参数
const payloads = [
    ['../../../etc/passwd', ''],           // 路径穿越
    ['/tmp/test.txt', 12345],              // 类型错误
    ['/tmp/test.txt'],                      // 参数缺失
    [Array(10000).join('A'), ''],          // 超长路径
    ['/tmp/test.txt', { toString: () => { throw new Error('pollution'); } }]
];
for (const p of payloads) {
    try { await window.api.saveFile(...p); } catch(e) { console.log('Blocked:', e.message); }
}
```

### 脆弱测试：只验证正常更新流程
```bash
# 脆弱：只跑了正常更新
./app --check-for-updates
# 正常下载并安装，签名验证通过
```

### 安全测试：更新管道攻击面测试
```python
# 安全：模拟中间人攻击更新流程
import mitmproxy, ssl
# 拦截 HTTPS 更新请求，返回自签名证书
# 篡改 latest.yml 中的 URL 指向恶意服务器
# 观察 app 是否拒绝安装或静默安装恶意包
```

---

## 输出要求

1. **假设清单与证伪实验**：每个安全声明对应的实验设计、预期结果、实际结果。
2. **探测脚本**：自动化探测 nodeIntegration、IPC 通道、协议处理的 JavaScript/Python 工具。
3. **边界值测试集**：路径、URL、编码、长度相关的系统化测试用例。
4. **中间人测试配置**：更新流程的 MITM 拦截和篡改方案。
