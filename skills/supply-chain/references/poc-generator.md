---
description: 供应链 PoC 生成 - 一击必碎的打包机命门重现
---

# PoC 生成器 (PoC Generator): 锻造撕破工场帷幕的代码实锤

供应链的安全报告如果没有直击心脏的证明，往往会被认为是杞人忧天。你必须给出一个可自动化再现、立见血封喉的证据。

## 专家级 PoC 的要求

1.  **沙箱环境内的截断与盗取微演习 (Minimal CI Theft Harness)**：
    不需在正式机器上破坏，而是让维护者克隆你的本地极简脚本：
    *   **注入步骤**：提供一个带有所述命令注入 `Payload` 的脚本参数，触发存在漏洞那段一模一样的本地模拟 CI `exec` 或 YAML 执行流。
    *   **落地证实层**：当执行器在装配的过程中，能够向控制台显赫地抛映出 `Environment variables dumped (showing fake AWS Tokens)...` 或者本应锁在安全箱中的内容，甚至是无阻碍写向一个外接 Mock Webhook 端点的请求记录。
    *   **无可辩驳的宣告**：“瞧，在这短暂的几十秒构建期间，核心机密像流水一样倒给了外界。”

2.  **CLAIM CHAIN 中致命的盲信判定**：
    精准在定罪链中指认：`[file: .github/workflows/build.yml:14 -> Untrusted string interpolation to bash Context -> Resulting in Privileged Command Execute]` 证明此处对于非受信内容直接宏替换做法为最高级别的架构死刑！

## 你的任务
输出这串不加任何冗余、用事实扇向供应链粗糙信任链的代码鞭笞，以极为硬核的数据宣示构建者对其所造系统的悲惨失守。
