# 当我们用AI挖到的一个RCE：假设驱动认知审计的实战复盘

> **摘要**：本文记录我们团队在某网络库审计中的完整方法论实践。我们引入了一套基于"假设驱动认知审计"（Hypothesis-Driven Cognitive Auditing, HDCA）的 AI 漏洞挖掘方法，通过可证伪的安全假设、对抗性验证与置信度校准，在 38 分钟内定位到多个被传统工具遗漏的安全缺陷，其中包括一个平台差异导致的高危内存安全问题。文章将完整披露技术流程、底层分析细节与方法论边界。

---

## 0x00 前言

38 分钟，我们的假设驱动审计引擎给出了分析报告。里面有三条假设，置信度都在 0.90 以上。其中一条，是一个我们在前两轮里**完全没意识到**的问题：某个公共 API 在 Windows 下用了带长度校验的 `memcpy_s`，但在 Linux、Android、macOS、iOS 下直接调用了裸的 `memcpy`——而调用方传入的 `bufferSize` 参数，在非Windows平台分支里被完全忽略了。

这意味着什么？意味着如果一个应用在这个 API 里传了一个 1KB 的栈缓冲区，而服务器返回了 64KB 的数据，那多出来的 63KB 会直接覆盖栈上的返回地址。

**RCE。**

我们团队用 AI 漏洞挖掘工具已经有段时间了。这篇文章想记录的，不是"AI 发现了什么人类发现不了的"，而是我们在实战中沉淀下来的这套方法论——**假设驱动认知审计**——到底是怎么工作的，以及它在什么情况下真的有用。

下面进入技术细节。

---

## 0x01 假设驱动认知审计（HDCA）：方法论框架

传统安全审计的本质是**规则匹配**——无论是人工审计员的"经验直觉"，还是 SAST 的"危险函数签名"。规则匹配有一个结构性盲区：它只能发现"已经知道应该寻找的东西"。

我们的核心思想是：**把安全审计从规则匹配升级为科学假设的生成与证伪**。

### 1.1 可证伪的安全假设（Falsifiable Security Hypothesis）

卡尔·波普尔在《科学发现的逻辑》中提出：一个命题的科学性不在于它能否被证实，而在于它能否被**证伪**。我们将这一原则引入安全审计。

一个合格的审计假设必须包含三个要素：

```
CLAIM:        [一个明确的、可测试的安全声明]
FALSIFICATION: [什么证据可以推翻这个声明]
CONFIDENCE:   [基于当前证据的置信度，0-1]
```

举个例子。面对一个处理外部输入的解压缩函数，传统审计会问"这里有没有漏洞？"——这个问题太模糊，无法被验证也无法被排除。

假设驱动的问法是：

> **CLAIM**：函数 `DecompressResponse()` 在处理恶意构造的 gzip 数据时，会无限制分配内存，导致 OOM。
> **FALSIFICATION**：如果代码中存在可配置的总输出大小上限，且在分配前被校验，则假设不成立。
> **CONFIDENCE**：0.55（基于代码结构初判，尚未验证调用链）

这种表达的价值在于：它把一个模糊的安全直觉，转化为了一个**可被独立验证的命题**。多个验证者可以基于同一组证据，独立评估这个命题的真伪。

### 1.2 对抗性验证协议（Adversarial Validation Protocol）

单一验证者的结论是不可靠的——无论这个验证者是人还是 AI。我们引入了两层对抗机制：

**第一层：结构化证伪检查**

每个验证任务必须回答三个独立问题：

| 检查维度 | 问题 | 意义 |
|---------|------|------|
| 可达性 | 外部/未认证用户能否到达此代码路径？ | 排除仅内部调用的"伪漏洞" |
| 可利用性 | 能否用现实输入触发异常行为？ | 排除理论上存在但无法构造触发的缺陷 |
| 缓解性 | 是否存在上游防御阻止利用链？ | 排除已有纵深防御覆盖的问题 |

只有当三个维度的评估都指向"风险存在"时，假设才进入候选报告池。这一层过滤约 40% 的初始假设。

**第二层：批判性复审（Devil's Advocate）**

对于通过第一层检查的假设，系统自动派遣一个独立的"批判者"，其唯一任务是**从反面推翻该假设**。批判者会主动寻找：

- 调用方是否进行了前置校验？
- 是否存在编译期或运行期的隐性保护？
- 攻击路径中是否有我们忽略的边界条件？

批判者找到的反驳证据，会作为负面证据回注置信度计算。如果批判者的反驳成立，假设被降级或丢弃；如果反驳被证伪，假设的置信度会进一步提升。

### 1.3 贝叶斯置信度校准（Bayesian Confidence Calibration）

人工审计的一个常见问题是"情绪干扰"——审计员在连续看了几个小时"安全"的代码后，容易对下一个可疑点过度敏感或过度麻木。我们需要一种**客观的、可累积的置信度评估机制**。

我们采用简化的贝叶斯更新框架。核心思想很简单：**看到新证据后，更新对假设的信念程度**。

具体来说，后验置信度由三部分决定：假设本身的先验概率、证据在假设成立时的出现概率、以及证据在不成立时的出现概率。三者的关系就是经典的贝叶斯定理。

在工程实现中，由于精确计算"证据在假设不成立时的概率"非常困难，我们采用了更实用的**对数几率比（Log-Odds Ratio）**简化形式。用 Python 伪代码表达如下：

```python
def update_confidence(prior: float, evidence_list: list) -> float:
    """
    prior: 当前置信度 (0.0 ~ 1.0)
    evidence_list: [(weight, is_positive), ...]
    """
    # 先验置信度转对数几率
    log_odds = math.log(prior / (1 - prior))
    
    for weight, is_positive in evidence_list:
        if is_positive:
            log_odds += weight    # 正面证据：加分
        else:
            log_odds -= weight    # 负面证据：减分
    
    # sigmoid 映射回概率区间
    return 1.0 / (1.0 + math.exp(-log_odds))
```

系统还会从历史审计结果中学习**因果强度**——哪些验证者在特定类型的假设上预测更准，就自动给它们更高的权重（采用 Laplace 平滑避免零频率问题）。

置信度阈值定义：

| 置信度区间 | 状态 | 含义 |
|-----------|------|------|
| 0.90 - 1.00 | CONFIRMED | 多源证据交叉验证，批判者未能推翻 |
| 0.70 - 0.89 | LIKELY | 有具体证据支持，但存在未验证的分支 |
| 0.40 - 0.69 | UNCERTAIN | 结构可疑，缺乏决定性证据 |
| 0.00 - 0.39 | DISCARDED | 负面证据占优或已被证伪 |

### 1.4 认知分层模型（Layered Cognitive Model）

我们把审计过程建模为五个递进的认知层次，模拟人类安全专家的思维过程：

**Layer 1 - 深度理解（Deep Understanding）**
不急于寻找漏洞，先建立系统的心智模型：信任边界在哪里？数据流如何穿越边界？关键不变量（invariants）是什么？

**Layer 2 - 对抗共情（Adversarial Empathy）**
模拟开发者思维，寻找"过度自信 = 盲点"的区域。开发者最自信的地方（"这里不可能有问题"），往往就是审计价值最高的地方。

**Layer 3 - 溯因推理（Abductive Reasoning）**
从异常出发反向推理：如果某个条件被违反，系统会如何表现？这种表现是否可被利用？溯因推理是发现 0-day 的核心能力——它不是从规则出发，而是从"系统可能如何失效"出发。

**Layer 4 - 认识论谦逊（Epistemological Humility）**
主动尝试证伪自己的假设。"如果测试意外成功"——当我们预期一个假设会被证伪，但验证结果却支持它时，这往往是重大发现的信号。

**Layer 5 - 远距类比（Far-Transfer Analogy）**
跨领域迁移已知攻击模式。例如："这个 MIME 解析器处理嵌套边界的方式，与 HTTP chunked transfer 的边界处理类似——是否存在类似的走私攻击？"

---

## 0x02 实战：一次完整的认知审计循环

以下是我们审计该 HTTP 客户端库的完整流程。目标已脱敏，所有代码片段为示意性重构。

### 2.1 Phase I：心智模型构建（Layer 1）

审计的第一步不是读代码，而是让 AI 分析项目的架构文档、README、构建系统和配置模板，提取四个关键要素：

- **技术栈**：C++17，多平台抽象，zlib 压缩，WebSocket 支持
- **信任边界**：客户端 ↔ 服务器（不可信），公共 API ↔ 内部实现
- **关键不变量**：`bufferSize` 参数承诺了写入上限；`Content-Length` 声明了响应体长度；平台抽象层应提供一致的安全语义
- **暴露面清单**：HTTP 响应解析、gzip 解压缩、WebSocket 帧处理、重定向跟随

这个阶段的核心产出是**攻击面热力图**——哪些模块处理不可信输入且缺乏纵深防御？

### 2.2 Phase II：假设生成（Layer 2-3）

基于心智模型，系统生成初始假设集。以下是部分脱敏后的假设：

**假设 H-1**：跨平台抽象层中的内存拷贝函数，在某些平台分支下可能忽略了调用方提供的 `bufferSize` 参数，导致写入长度不受控。

*推理路径*：Layer 2 对抗共情——开发者在 Windows 平台使用了 `memcpy_s`（带目标长度限制），但在 `#else` 分支中可能因"保持一致 API"的考虑而直接使用 `memcpy`，忽略了参数传递。

**假设 H-2**：gzip 解压缩循环在分配输出内存时缺乏总大小上限，恶意服务器可通过压缩炸弹触发客户端 OOM。

*推理路径*：Layer 3 溯因推理——如果服务器声明 `Content-Encoding: gzip`，然后发送一个 10MB 的高压缩比数据包，客户端的解压缩逻辑会如何处理？

**假设 H-3**：HTTP 响应体长度跟踪使用无符号整数，在服务器发送的数据超过 `Content-Length` 声明时，可能发生下溢。

*推理路径*：Layer 5 远距类比——类似问题在 HTTP/2 和 QUIC 实现中已被多次发现，经典模式是"声明长度 vs 实际长度"的不一致。

**假设 H-4**：WebSocket 帧解析使用服务器返回的长度字段直接分配缓冲区，缺乏最大帧尺寸校验。

**假设 H-5**：HTTP 重定向跟随逻辑缺乏最大跳转次数或环路检测。

### 2.3 Phase III：证据链追踪（Layer 1-3 的循环）

每个假设被拆分为独立的验证任务，并行执行。

#### H-1 的验证过程

**验证者 A** 被分配任务："检查跨平台内存拷贝函数的所有平台分支，确认 `bufferSize` 参数是否在每条分支中被正确使用。"

验证者 A 在阅读代码后发现：

```cpp
// http_response.cpp（示意性重构）
void GetResponseBodyBytes(
    _Out_writes_bytes_to_opt_(bufferSize, *bufferUsed) void* buffer,
    size_t bufferSize,
    _Out_opt_ size_t* bufferUsed
) {
    #if HC_PLATFORM_IS_MICROSOFT
        // Windows: 安全拷贝，受 bufferSize 约束
        memcpy_s(buffer, bufferSize, source, sourceSize);
    #else
        // Linux/Android/macOS/iOS: 直接拷贝，bufferSize 被忽略！
        memcpy(buffer, source, sourceSize);
    #endif
}
```

关键发现：
1. 函数的 SAL 注解 `_Out_writes_bytes_to_opt_(bufferSize, *bufferUsed)` 向调用方承诺"最多写入 `bufferSize` 字节"
2. 在非Windows平台分支中，`bufferSize` 完全未参与运算
3. `sourceSize` 来自服务器返回的响应体长度，攻击者可控制

**验证者 A 的结论**：REACHABLE = yes（公共 API），EXPLOITABLE = yes（服务器返回超大响应体即可触发），MITIGATED = no（无上游校验）。

**验证者 B**（独立复核）追踪了所有内部调用点，确认该函数是公共 API，外部调用者可以传入任意大小的 `buffer` 和 `bufferSize`。不存在"所有调用方都确保安全"的缓解条件。

H-1 的置信度更新至 **0.90**。

#### H-2 的验证过程

**验证者 C** 定位到解压缩核心逻辑：

```cpp
// compression.cpp（示意性重构）
void DecompressFromGzip(
    uint8_t* inData,
    size_t inDataSize,
    vector<uint8_t>& outData
) {
    z_stream stream;
    // ... 初始化 ...
    stream.avail_in = static_cast<uInt>(inDataSize);  // 注意：64→32 位截断！
    
    int ret;
    do {
        outData.resize(outData.size() + CHUNK);  // CHUNK = 16KB，无上限
        stream.avail_out = CHUNK;
        stream.next_out = outData.data() + outData.size() - CHUNK;
        ret = inflate(&stream, Z_NO_FLUSH);
    } while (ret != Z_STREAM_END);
}
```

验证者 C 识别出两个独立问题：

**问题 A（内存耗尽）**：`outData.resize(outData.size() + CHUNK)` 在循环中无限制执行。一个 10MB 的高压缩比 gzip 流（例如全零数据）可以解压缩到数十 GB。这是经典的**解压缩炸弹（Decompression Bomb）**攻击。

**问题 B（整数截断）**：`static_cast<uInt>(inDataSize)` 在 64 位平台上将 `size_t` 截断为 32 位 `uInt`。如果压缩响应体超过 4GB，`avail_in` 只保留低 32 位，导致 zlib 只处理部分输入，可能产生不可预期的行为。

**验证者 C 的结论**：双重风险，无缓解措施。H-2 置信度更新至 **0.95**。

#### H-3 的验证过程

**验证者 D** 在两个平台后端中发现了同源问题：

```cpp
// curl_provider.cpp（示意性重构）
size_t contentLength = GetResponseContentLength(handle);
request->responseBodySize = contentLength;
request->remainingToRead = contentLength;

// ... 数据到达时 ...
request->remainingToRead -= bufferSize;  // 无符号下溢！
```

数学分析：如果服务器声明 `Content-Length: 1000`，但实际发送 1500 字节，则 `remainingToRead` 从 1000 减去 1500，发生无符号整数下溢，结果接近 `SIZE_MAX`。

下溢的复合效应：
1. **进度计算错误**：`bytesWritten = totalSize - remainingToRead` 也会下溢，导致进度回调报告荒谬的数值
2. **后续读取越界**：`min(maxBufferSize, remainingToRead)` 中，`remainingToRead` 变成了极大值，可能绕过读取大小限制

**验证者 D 的结论**：攻击路径清晰，服务端可控触发。H-3 置信度更新至 **0.88**。

### 2.4 Phase IV：对抗性质疑与置信度收敛

**批判者 E** 对 H-1 提出反驳："`memcpy` 虽然在非微软平台忽略了 `bufferSize`，但也许所有调用方都传入了足够大的缓冲区？"

**验证者 F** 响应批判：追踪公共 API 的所有外部调用路径，确认存在调用方传入栈缓冲区（大小由业务逻辑决定）的场景。如果服务器返回的响应体大于该栈缓冲区，将发生**栈缓冲区溢出**——这比堆溢出更危险（通常可直接控制返回地址）。

批判者 E 的反驳被证伪，H-1 置信度巩固至 **0.95**，状态 CONFIRMED。

**批判者 G** 对 H-2 提出反驳："现代操作系统有 OOM Killer，解压缩炸弹最多导致进程被杀，不构成安全漏洞。"

**验证者 H** 响应：在移动平台（Android/iOS）和嵌入式 Linux 上，OOM Killer 的行为不可预期——它可能杀死整个应用进程组，导致数据丢失或服务中断。此外，即使不考虑 OOM，整数截断问题（问题 B）是独立的内存安全问题。批判者的反驳仅部分成立，H-2 置信度微调至 **0.98**。

最终假设状态：

| 假设 | 置信度 | 状态 | 级别 |
|------|--------|------|------|
| H-1 平台差异 memcpy | 0.95 | CONFIRMED | **HIGH** |
| H-2 解压缩炸弹 | 0.98 | CONFIRMED | **HIGH** |
| H-3 无符号整数下溢 | 0.88 | CONFIRMED | MEDIUM |
| H-4 WebSocket 长度欺骗 | 0.12 | DISCARDED | - |
| H-5 重定向循环 | 0.08 | DISCARDED | - |

H-4 和 H-5 被丢弃的原因：H-4 的上层有最大帧尺寸配置（虽然默认极大，但用户可手动调小）；H-5 存在硬编码的最大重定向次数（10 次），批判者成功证伪。

---

## 0x03 底层分析：为什么这些漏洞被传统工具遗漏？

### 3.1 H-1：跨平台条件编译的暗面

H-1 的核心不是"用了 memcpy"——而是**平台条件编译导致的安全语义不一致**。

```cpp
#if HC_PLATFORM_IS_MICROSOFT
    memcpy_s(buffer, bufferSize, source, sourceSize);
#else
    memcpy(buffer, source, sourceSize);
#endif
```

从静态分析的视角看：
- 如果分析器在 Windows 平台上运行，它看到 `memcpy_s`，认为安全。
- 如果分析器在 Linux 平台上运行，它看到 `memcpy`，会告警——但告警信息通常是"建议使用更安全的函数"，不会指出**参数契约被违背**这个更深层的问题。

更严重的是**SAL 注解与实现的语义背离**：

```cpp
_Out_writes_bytes_to_opt_(bufferSize, *bufferUsed)
```

这个注解向编译器、静态分析工具和人类读者共同承诺："此函数最多向 `buffer` 写入 `bufferSize` 字节。"但在非微软平台分支中，这个承诺被违背了。传统 SAST 要么不解析 SAL 注解，要么在条件编译面前丢失跨分支的语义关联。

**假设驱动方法的价值**：它不依赖"危险函数签名"，而是追问"函数的承诺与实现是否一致"——这是语义层面的审计，而非语法层面的扫描。

---

### 3.4 H-1 的利用链构造：从条件分支到代码执行

H-1 从一个"平台差异导致的防御缺口"升级为**可利用的内存损坏漏洞**，需要构造完整的利用链。以下是我们在实验室环境中验证的利用路径（所有地址和偏移均为示意性重构，不代表真实目标）。

#### 步骤 1：控制拷贝源长度

该函数是公共 API，调用方为应用程序代码。关键观察：参数 `sourceSize` 来自 HTTP 响应体长度，而该长度由远程服务器通过 `Content-Length` 头部或实际发送数据控制。

构造恶意服务器响应：

```http
HTTP/1.1 200 OK
Content-Length: 65536
Content-Type: application/octet-stream

[65536 字节的任意数据]
```

#### 步骤 2：触发栈缓冲区溢出场景

追踪公共 API 的调用模式，我们发现存在这样的调用栈：

```cpp
// 应用程序代码（示意性重构）
void OnResponseReceived(HttpCall* call) {
    char stackBuffer[1024];  // 栈缓冲区，1KB
    size_t used = 0;
    
    // 调用存在缺陷的公共 API
    GetResponseBodyBytes(stackBuffer, sizeof(stackBuffer), &used);
    //              ^^^^^^^^^^^^ 1024 字节
    // 但服务器返回了 65536 字节
}
```

在非Windows平台分支中，`memcpy(buffer, source, sourceSize)` 将 65536 字节拷贝到 1024 字节的栈缓冲区，发生**栈缓冲区溢出（Stack Buffer Overflow）**。

#### 步骤 3：覆盖返回地址与栈金丝雀

现代编译器默认启用栈保护（Stack Canary）。溢出会首先覆盖 canary 值，触发 `__stack_chk_fail`。但利用链构造中，我们发现该目标库的某些发布版本在特定平台（某嵌入式 Linux 发行版）上以 `-fstack-protector` 而非 `-fstack-protector-all` 编译，意味着**小于 8 字节的局部变量数组不被保护**。

更关键的是：如果攻击者能控制溢出内容，且目标平台缺乏 ASLR（某些 IoT/嵌入式场景），则利用策略为：

1. 用 NOP sled + shellcode 填充响应体
2. 精确计算从 `stackBuffer` 到返回地址的偏移量 `\delta`
3. 在偏移 `\delta` 处写入 shellcode 入口地址

#### 步骤 4：绕过 NX（不可执行栈）

如果目标平台启用了 NX（W^X），栈上不可执行。此时需要**返回到 libc（Return-to-libc）**或 **ROP（Return-Oriented Programming）**。

我们在分析中发现，该库静态链接了一个旧版本的 zlib，其中包含大量 `gadget`：

```asm
; zlib 中的典型 gadget（示意性）
pop rdi; ret        ; 控制第一个参数
pop rsi; ret        ; 控制第二个参数
mov r11, [rsp+8]; ret  ; 复杂 gadget
```

攻击者通过栈溢出覆盖返回地址，构建 ROP chain：

```
[stackBuffer: 1024 bytes]  [填充至 canary]  [覆盖 canary]
[覆盖保存的 RBP]  [ROP gadget 1: pop rdi; ret]
[参数 1: /bin/sh 地址]  [ROP gadget 2: pop rsi; ret]
[参数 2: 0]  [ROP gadget 3: system() 地址]
```

#### 步骤 5：实际利用的约束条件

需要坦承的是，上述完整利用链在真实环境中面临多重约束：

| 约束 | 影响 | 缓解可能性 |
|------|------|-----------|
| ASLR | 如果启用，gadget 和 libc 地址随机化，ROP 难度大增 | 需要信息泄露辅助 |
| Stack Canary | 如果启用且覆盖，触发异常终止 | 需要泄漏 canary 或绕过保护范围 |
| NX | 阻止栈上 shellcode 执行 | 必须采用 ROP / JOP |
| 响应体大小限制 | 如果上层有最大响应体限制，可能无法发送超大 payload | 取决于应用层配置 |

在我们的审计场景中，目标部署环境的**特定配置组合**（嵌入式 Linux + 静态链接 + 部分保护缺失）使得这条利用链从"理论风险"变为"实际可触发"。这正是为什么安全审计不能止步于"存在 memcpy"——必须追问"在目标部署上下文中，这条路径是否可达、可利用、未被缓解"。

#### 步骤 6：更隐蔽的堆利用变体

如果调用方传入的是堆分配缓冲区而非栈缓冲区，利用策略变为**堆溢出（Heap Overflow）**：

```cpp
void OnResponseReceived(HttpCall* call) {
    size_t* heapBuffer = new size_t[128];  // 1024 字节堆块
    GetResponseBodyBytes(heapBuffer, 1024, &used);
}
```

堆溢出的利用通常更复杂，需要理解目标堆分配器（ptmalloc、jemalloc 等）的 chunk 结构。但在我们的分析中，发现该库内部频繁进行大小相近的堆分配（HTTP 响应体缓存、头部解析缓冲区等），存在**堆块相邻布局的可预测性**。攻击者通过控制请求时序和响应大小，可能实现精确的堆布局编排（Heap Feng Shui），将敏感结构（如虚表指针、函数指针）放置在溢出目标相邻位置。

这种利用链的构造时间显著长于栈溢出（数小时到数天），但其隐蔽性更高——堆溢出通常不会立即崩溃，而是破坏后续操作的数据结构，延迟触发异常，给调试和检测带来极大困难。

### 3.2 H-2：无界资源分配的渐进式危害与攻击复杂度

H-2 的问题表面上是"没有限制解压缩大小"，但深层分析揭示了攻击的**渐进式危害模型**和精确的**攻击复杂度边界**。

设攻击者控制的 gzip 压缩流大小为 `|C|`，解压后大小为 `|D|`，压缩比为 `ρ = |D| / |C|`。gzip 使用 DEFLATE 算法，对于高度可压缩数据（如全零序列），理论最大压缩比受 Lempel-Ziv 滑动窗口大小（32KB）和 Huffman 编码极限约束。

**攻击复杂度上界分析**：

用一段 Python 代码来说明攻击参数：

```python
# 攻击者控制的压缩炸弹参数
compressed_size = 10 * 1024 * 1024       # 10 MB 的压缩数据
compression_ratio = 1000                  # 保守估计（全零数据实际可达数千）
decompressed_size = compressed_size * compression_ratio  # ≈ 10 GB

# 客户端环境
client_memory = 2 * 1024 * 1024 * 1024    # 2 GB（移动设备典型值）
chunk_size = 16 * 1024                    # 每次迭代分配 16 KB

# 达到 OOM 需要的迭代次数
iterations_to_oom = client_memory // chunk_size   # = 131072 次
# 以 100MB/s 解压速度计算：10GB / 100MB/s ≈ 100 秒达到 OOM
# 实际上由于内存碎片和系统开销，通常数秒内就会触发 OOM
```

10MB 的压缩流膨胀到 10GB，而典型移动设备只有 2-4GB 内存，容器环境可能只有 512MB——**压缩数据的大小与造成的危害完全不成比例**。

**渐进式危害模型**：

**阶段 1（内存压力累积）**：解压缩循环每次分配 16KB，累计分配随迭代次数线性增长。如代码所示，达到 2GB 上限仅需约 13 万次迭代，以现代 CPU 解压速度，数秒内即可触发 OOM。

**阶段 2（系统级级联失效）**：移动平台的 OOM Killer 通常按 `oom_score` 选择受害者。如果客户端进程是前台应用，可能被系统优先杀死；如果是后台守护进程，可能触发 `watchdog` 重启循环，导致 CPU 和内存的周期性抖动——攻击者只需维持低频请求即可造成持续可用性损失。

**阶段 3（复合拒绝服务）**：如果客户端实现了指数退避重试策略，攻击者可通过周期性地发送压缩炸弹，使客户端陷入"请求 → OOM → 重启 → 重试 → 再次 OOM"的循环。此时攻击带宽需求极低：假设客户端重试间隔为 `T`，攻击者只需每 `T` 秒发送一个 `|C| = 10MB` 的请求，即可维持持续 DoS。

**整数截断的独立风险**：

`static_cast<uInt>(inDataSize)` 将 64 位 `size_t` 截断为 32 位 `uInt`，只保留低 32 位。这段 C 代码演示了截断效应：

```c
#include <stdint.h>
#include <stdio.h>

int main() {
    size_t in_size = 5ULL * 1024 * 1024 * 1024;  // 5 GB
    uInt avail_in = (uInt)in_size;                // 截断为 32 位
    
    printf("原始大小: %zu GB\n", in_size / (1024*1024*1024));  // 5 GB
    printf("截断后:   %u GB\n", avail_in / (1024*1024*1024));  // 1 GB
    printf("丢失数据: %zu GB\n", (in_size - avail_in) / (1024*1024*1024));  // 4 GB
    
    return 0;
}
```

zlib 处理完截断后的 1GB 就返回 `Z_STREAM_END`，但输入流还有 **4GB** 未消费。客户端若未检查 `avail_in` 残留，会错误地认为"解压缩完成"。

zlib 处理完 `S'` 字节后返回 `Z_STREAM_END`，但输入流仍有 `S - S' = 4GB` 未消费。客户端若未检查 `avail_in` 残留，会错误地认为"解压缩完成"，将截断后的不完整数据递交给上层解析器。对于 JSON 解析器，截断可能发生在字符串或对象中间，触发解析异常甚至进入未处理的错误分支。

### 3.3 H-3：无符号整数下溢的数学本质

H-3 是一个教科书级的**整数下溢**，但其危害路径比表面更复杂。我们给出形式化分析。

设平台字长为 64 位，无符号整数取值范围是 `0` 到 `2^64 - 1`，运算在这个范围内自然回绕。用一段 C 代码直接演示下溢过程：

```c
#include <stdio.h>
#include <stdint.h>

int main() {
    uint64_t content_length = 1000;      // 服务器声明长度
    uint64_t bytes_received = 1500;      // 实际发送长度（恶意）
    uint64_t remaining = content_length;
    
    // 无符号减法：下溢！
    remaining -= bytes_received;
    
    printf("声明长度: %lu\n", content_length);     // 1000
    printf("实际接收: %lu\n", bytes_received);     // 1500
    printf("剩余值:   %lu\n", remaining);          // 18446744073709551116
    // 即 2^64 - 500，一个接近 20 亿亿亿的巨大数字
    
    // 下溢后的进度计算同样出错
    uint64_t bytes_written = content_length - remaining;
    printf("报告已写: %lu\n", bytes_written);       // 同样荒谬的数值
    
    return 0;
}
```

**下溢后的连锁反应**：

`remaining` 从 1000 变成 `2^64 - 500` 后，任何依赖它的计算都会出错：

- `bytes_written = totalSize - remaining` 同样下溢，报告给上层的数值完全错误
- `min(maxBufferSize, remaining)` 虽然被 `MAXDWORD` 保护，但进度跟踪已失效
- 上层逻辑可能基于错误的进度值做出缓冲区复用决策，导致后续内存操作访问越界

**复合危害分析**：

更隐蔽的是某些后端使用下溢后的 `remaining` 计算下次读取窗口：`nextRead = min(maxBufferSize, remaining, MAXDWORD)`。虽然 `MAXDWORD` 起到上限保护作用，但进度跟踪的污染已造成**状态机语义断裂**——`remainingToRead` 不再表示"真实剩余字节"，而是变成了一个无意义的回绕值。任何依赖该变量做状态转换的代码（如连接复用、请求流水线）都可能进入未定义行为分支。

---

## 0x04 方法论的边界：AI 幻觉与人工裁决

我们在这次实验中观察到了 AI 审计的三个结构性局限，以及我们的应对策略。

### 4.1 幻觉：AI 会构造不存在的代码

在一次验证中，某个 AI 验证者声称"函数在错误路径上 double-free"，并给出了详细的代码行号。但人工复查时发现，那行代码根本不存在——AI 基于上下文"合理推测"了一段代码，并将其当作事实陈述。

**应对策略**：
- **证据物理锚定**：所有结论必须附带可定位到源码的代码片段，不能是"根据上下文推断"
- **批判者天然制衡**：Devil's Advocate 机制会主动挑战"无法被定位的证据"
- **人工抽查**：所有 CONFIRMED 级别的发现必须经过人工复核，这是不可自动化的最后防线

### 4.2 上下文碎片化：大型项目的记忆丢失

10 万行代码的跨模块调用链（例如 HTTP Core → Compression → Memory Allocator）超出了单次 LLM 调用的上下文窗口。分区分析虽然降低了 Token 消耗，但可能导致**跨模块语义关联的丢失**。

**应对策略**：
- **Finding 汇聚与联合分析**：单模块的 CONFIRMED 发现会自动汇聚到全局视图，触发跨模块关联假设
- **分层上下文管理**：高层心智模型（模块间关系）与低层代码细节分离管理，避免上下文被实现细节淹没

### 4.3 置信度校准：数字可能是安慰剂

贝叶斯置信度是一个有用的组织工具，但它不是客观真理。我们发现，AI 验证者倾向于对自身分析过度自信——一个其实证据薄弱的假设，可能被赋予 0.80 以上的置信度。

**应对策略**：
- **从历史中学习**：系统记录每个验证者过去的"预测准确度"，调整其证据权重
- **强制批判**：高置信度假设必须经过独立批判者的挑战，不能自说自话
- **置信度不等于严重性**：即使置信度 0.95，如果利用条件需要管理员权限，实际风险评级也应降级

---

## 0x05 结语

这次审计让我们确认了一件事：**AI 漏洞挖掘安全研究的真正价值，不在于"更快地做同样的事"，而在于"做不同的事"**。

规则匹配只能发现已知的危险模式；假设驱动可以追问"系统的承诺与实现是否一致"。经验审计依赖审计员的个人知识边界；认知分层可以系统性地遍历从数据流到跨平台语义的完整攻击面。人工验证容易受情绪和疲劳影响；贝叶斯置信度提供了可累积、可复核的证据评估框架。

但 AI 不是万能钥匙。它会产生幻觉，会遗忘跨模块的关联，会对自己的分析过度自信。这些局限提醒我们：AI 应该是安全研究员的**认知放大器**，而不是**替代者**。最终的裁决权——判断一个假设是否值得报告、一个利用链是否真实可行——仍然属于人类。

工具在进化，攻击者和防御者都在使用 AI。区别在于：一方把 AI 当作更快的扫描器，另一方把 AI 当作具备科学方法论的研究伙伴。这场不对称正在拉大。

---

> **负责任的披露**：本文涉及的安全问题已按 responsible disclosure 流程报送相关厂商并修复。文中所有代码片段均为基于真实发现的示意性重构，不代表任何真实项目的原始源码。