# Installation Guide / 安装指南

[English](#english) | [中文](#中文)

---

<a id="english"></a>

## English

### Prerequisites

- Agentic AI environment (Claude, etc.)
- Git

### Dependencies by Module

#### Binary Module
- Rizin: `apt install rizin` or `brew install rizin`
- rz-ghidra plugin
- Frida: `pip install frida`
- PwnTools: `pip install pwntools`

#### Web Module
- Python 3.8+
- requests: `pip install requests`
- BeautifulSoup: `pip install beautifulsoup4`

#### Browser Module
- Node.js (for V8/JS engine analysis)
- Frida

### Installation Steps

```bash
# Clone the repository
git clone https://github.com/lielingxyz/vuln-hunting-plugin.git

# Navigate to your target module
cd vuln-hunting-plugin/Web  # or Counter, Browser, Mail, Binary

# Start your Agent and load the module
# Follow the entry command for your module
```

---

<a id="中文"></a>

## 中文

### 前置要求

- Agentic AI 环境 (Claude 等)
- Git

### 各模块依赖

#### 二进制模块
- Rizin: `apt install rizin` 或 `brew install rizin`
- rz-ghidra 插件
- Frida: `pip install frida`
- PwnTools: `pip install pwntools`

#### Web 模块
- Python 3.8+
- requests: `pip install requests`
- BeautifulSoup: `pip install beautifulsoup4`

#### 浏览器模块
- Node.js (用于 V8/JS 引擎分析)
- Frida

### 安装步骤

```bash
# 克隆仓库
git clone https://github.com/lielingxyz/vuln-hunting-plugin.git

# 进入目标模块目录
cd vuln-hunting-plugin/Web  # 或 Counter, 浏览器, 邮服, 二进制

# 启动 Agent 并加载模块
# 按照模块的入口命令操作
```

---

## License / 许可证

Open Source under [lieling.xyz](https://lieling.xyz)
