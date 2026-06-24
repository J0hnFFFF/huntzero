---
description: Binary 变体分析 - 同源漏洞在不同代码路径中的扩散
tags: [binary, variant-analyzer, cross-function, shared-library, copy-paste]
---

# Binary 变体分析 (Variant Analyzer): 同源漏洞在不同代码路径中的扩散

> 在 `process_png` 中发现了 `strcpy` 溢出？
> 一个成熟的安全者知道：这个程序很可能还处理 JPEG、GIF、BMP——
> 而且那些解析器用了同样的代码模式。
> 你要绘制一张"同源内存缺陷分布图"。

## 变体分析核心方法论：Binary 同源漏洞辐射定律

**定律一：解析器同构定律**
> 如果程序支持多种文件格式/协议，
> 那么不同格式的解析器很可能共享相同的错误模式（如不安全的字符串拷贝、未校验的长度字段）。

**定律二：危险函数扩散定律**
> 如果程序调用了 `strcpy`、`sprintf`、`gets` 等危险函数，
> 那么所有调用这些函数的代码路径都应该被审计。

**定律三：库依赖传染定律**
> 如果程序链接了某个存在已知漏洞的第三方库（如旧版 zlib、libpng），
> 那么该漏洞的所有利用路径都可能存在于程序中。

---

## 变体分析一：跨格式/跨协议同构缺陷

### 场景
在 PNG 解析器中发现了整数溢出导致的堆溢出。

### 辐射检查清单

**Step 1：识别所有解析器入口**

```bash
# 搜索文件签名/Magic Number 检查
strings ./target | grep -i -E "png|jpg|gif|bmp|pdf|zip|magic"

# 搜索所有 read/recv 后的处理函数
rizin -A ./target
[0x00000000]> /ad call read
[0x00000000]> /ad call recv
# 追踪每个 read/recv 后的代码路径
```

**Step 2：检查同构模式**

```
PNG 解析器: read_png_header() → malloc(width * height * 4) → 整数溢出
JPEG 解析器: read_jpeg_header() → malloc(width * height * 3) → 同样可能溢出
BMP 解析器:  read_bmp_header()  → malloc(file_size)        → 可能未校验 file_size
GIF 解析器:  read_gif_header()  → malloc(width * height)    → 同样可能溢出
```

**关键洞察**：所有图像解析器都遵循"读取头部 → 解析尺寸 → 分配内存 → 读取数据"的模式。
如果 PNG 解析器的尺寸校验有漏洞，其他解析器很可能也有同样的漏洞。

---

## 变体分析二：危险函数调用扩散

### 场景
在 `process_request` 中发现了 `strcpy` 溢出。

### 辐射检查清单

**Step 1：搜索所有危险函数调用**

```bash
# 使用 rizin 搜索导入的函数
rizin -A ./target
[0x00000000]> ii~strcpy,sprintf,gets,scanf,memcpy,system

# 对每个危险函数，查看所有调用点
[0x00000000]> /ad call strcpy
[0x00000000]> /ad call sprintf
```

**Step 2：分析每个调用点的安全性**

| 函数 | 调用点 | 目标缓冲区 | 源/长度 | 风险 |
|------|--------|-----------|---------|------|
| strcpy | 0x401234 | 栈 buf[64] | 用户输入 | High |
| strcpy | 0x401567 | 堆 buf | 配置文件 | Medium |
| sprintf | 0x401890 | 栈 buf[128] | 用户输入 | High |
| memcpy | 0x402000 | 堆 buf | 用户控制的长度 | High |

**关键洞察**：`strcpy` 的调用点 0x401567 看似安全（配置文件），
但如果配置文件可被用户修改（如通过符号链接、路径穿越），则同样危险。

---

## 变体分析三：第三方库漏洞传染

### 场景
程序链接了旧版 libpng（存在已知 CVE）。

### 辐射检查

1. **识别所有第三方库**：
   ```bash
   ldd ./target
   readelf -d ./target | grep NEEDED
   ```

2. **检查库版本和已知 CVE**：
   ```bash
   # 检查 libpng 版本
   strings ./target | grep -i "libpng version"
   
   # 对比 CVE 数据库
   # 某已知漏洞: libpng 1.6.36 之前的 png_image_free 中存在 UAF
   # 如果程序链接了受影响版本 → 需要检查是否调用了 png_image_free
   ```

3. **检查库的编译选项**：
   - 如果第三方库编译时未开启 Stack Canary → 所有通过该库的漏洞利用更容易
   - 如果第三方库编译时开启了 RELRO Full → GOT 表不可写，利用难度增加

---

## 变体分析四：跨版本/跨架构的同构缺陷

### 场景
在 x86-64 版本中发现漏洞，检查 x86、ARM、MIPS 版本是否存在同样问题。

### 检查清单

1. **代码共享程度**：
   - 不同架构是否共享相同的 C 源码？（通常是的，由编译器生成不同汇编）
   - 如果是，漏洞在所有架构中都存在

2. **利用方式差异**：
   - x86-64：ROP gadget 丰富，寄存器传参
   - x86：栈传参，ROP 方式不同
   - ARM：LR 寄存器存储返回地址，利用方式不同
   - MIPS：延迟槽指令，利用方式不同

3. **安全机制差异**：
   - 某些架构可能不支持 NX 或 ASLR
   - 嵌入式设备（MIPS/ARM）通常安全机制较弱

---

## 专家变体分析输出模板

```
## 变体分析报告

### 原始漏洞
[描述最初发现的漏洞]

### 辐射分析
- 根因类型: [危险函数/整数溢出/设计模式]
- 扫描范围: [函数/文件格式/第三方库]
- 发现的同源模式数量: [N]

### 变体清单
| 变体ID | 位置 | 漏洞类型 | 根因相同度 | 风险等级 | 修复优先级 |
|--------|------|---------|-----------|---------|-----------|
| VAR-01 | JPEG 解析器 | 整数溢出→堆溢出 | 完全相同 | High | P0 |
| VAR-02 | BMP 解析器  | 未校验长度字段 | 模式相同 | High | P0 |
| VAR-03 | 配置文件读取 | strcpy 溢出 | 函数相同 | Medium | P1 |

### 爆炸半径
- 直接受影响的代码路径: [N]
- 间接受影响的代码路径（共享函数）: [N]
- 第三方库影响: [库名 + CVE 列表]
- 跨架构影响: [x86/x64/ARM/MIPS]

### 修复建议
- 立即: 修复所有 P0 变体
- 短期: 全局替换危险函数为安全版本
- 长期: 升级第三方库 / 引入静态分析工具 / 架构安全审查
```
