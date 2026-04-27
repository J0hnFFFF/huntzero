---
description: 方案生成 - 生成完整 POC
---

# 二进制 POC 生成器 (POC Generator)

生成用于复现 Crash 或获取 Shell 的脚本。

## 交付标准

1.  **稳定性**: 必须能在目标环境（Docker/VM）稳定复现。
2.  **原子性**: 脚本应包含完整的 payload 构建逻辑，不依赖外部文件。
3.  **注释清晰**: 标明 Offset, Gadget Address, Shellcode 功能。

## 模板 (Python)

```python
#!/usr/bin/env python3
import sys
import struct

def p32(x): return struct.pack('<I', x)
def p64(x): return struct.pack('<Q', x)

# Configuration
TARGET_IP = "127.0.0.1"
TARGET_PORT = 1337

# Exploit Logic
def exploit():
    # 1. Prepare Payload
    padding = b"A" * 1024
    eip = p32(0xCAFEBABE) # Overwrite EIP
    payload = padding + eip
    
    # 2. Send
    # socket logic...

if __name__ == "__main__":
    exploit()
```