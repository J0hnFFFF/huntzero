---
description: 撰写基础设施漏洞报告：集群沦陷的业务影响、CVSS 评估与缓解措施（OPA/Gatekeeper、PSS、网络策略、镜像签名）。
tags: [infra, kubernetes, report, mitigation, cvss]
---

# 报告生成 (Report Generator)

## 1. 专家直觉触发器 (Triggers)

专家在确认漏洞后准备报告时，会立即关注以下要素：

- **业务关键性**：该集群是否运行生产交易服务、支付网关或核心数据库？集群沦陷是否等于业务停摆？
- **数据敏感度**：集群内是否存储 PII、支付卡号、医疗记录或企业核心知识产权？
- **影响可量化**：能否计算潜在的泄露记录数、宕机时长（SLA 违约）或财务损失？
- **修复可行性**：缓解措施是修改一个 NetworkPolicy，还是需要重建整个集群并轮换所有证书？
- **合规风险**：是否符合等保 2.0、GDPR、HIPAA 或 PCI-DSS 要求？漏洞是否会导致审计失败？
- **攻击者画像**：是内部员工、多租户邻居，还是外部匿名攻击者？利用复杂度是高还是低？

这些触发器决定了报告的受众（开发、运维、管理层、合规）和语气（技术细节 vs 业务风险）。

## 2. 不可跳过的问题链 (Question Chain)

在撰写报告前，必须回答：

1. **漏洞的精确触发条件是什么？**  
   是哪个 RBAC 角色？哪个 Helm Chart 版本？哪个 Controller 的哪一行代码？
2. **影响的爆炸半径有多大？**  
   是单个命名空间、整个集群，还是所有共享同一镜像仓库或 Terraform State 的集群？
3. **利用路径的复杂度如何？**  
   是否需要身份认证？是否需要多个步骤组合？是否能在无交互的情况下自动化利用？
4. **现有安全控制为何失效？**  
   `PodSecurityStandard` 是否未启用？`NetworkPolicy` 是否默认允许？镜像签名是否未校验？
5. **推荐的缓解措施优先级？**  
   哪些是短期可落地的（如删除危险 RoleBinding）？哪些是长期架构改进（如引入 OPA/Gatekeeper）？
6. **验证修复是否有效的测试方法？**  
   修复后，原有的 PoC 是否仍然成功？是否引入了新的拒绝服务风险？

## 3. 攻击链闭合 (Attack Chain Closure)

报告的核心是呈现“从漏洞到灾难”的完整因果链，并给出可执行的打断点。以下是报告中的攻击链叙述模板：

**攻击链概述**：

攻击者以拥有 `dev` 命名空间 `edit` 角色的普通开发者身份入手。通过审计发现，该角色虽然禁止直接 `exec` 到 Pod，但允许创建任意规格的 Pod。集群未启用 `Pod Security Standards`，也未部署 OPA/Gatekeeper 策略。

**步骤 1**：攻击者创建了一个包含 `hostPID: true` 和 `hostPath: {path: /}` 的 Pod。
**步骤 2**：Pod 启动后，攻击者通过读取 Pod 日志（`pods/logs` 权限通常伴随 `edit` 角色）观察到宿主主机名。
**步骤 3**：攻击者利用 `hostPID` 进入宿主命名空间，读取 `/var/lib/kubelet/kubeconfig`。
**步骤 4**：使用节点凭证，攻击者以 Node 身份列出所有命名空间的 Secret，包括 `kube-system` 的 `cluster-admin` ServiceAccount Token。
**步骤 5**：攻击者利用 Token 创建了持久的 `ClusterRoleBinding`，并修改了 `coredns` Deployment 的镜像指向投毒仓库。
**步骤 6**：从节点访问 `169.254.169.254`，窃取 AWS IAM 凭证，进而访问 S3 中存储的备份数据。

**逻辑闭环**：由于权限粒度过粗、准入控制缺失、网络未隔离元数据，攻击者仅用 `create pods` 权限就实现了从命名空间到云账户的六级跳。任何一级的缺失都不会阻止整体链条，但会提升攻击成本。

## 4. 代码示例 (Code Examples)

### 4.1 报告中的漏洞修复：OPA/Gatekeeper 策略

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sPSPPrivilegedContainer
metadata:
  name: psp-privileged-container
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    excludedNamespaces: ["kube-system"]
  parameters:
    exemptImages: []
```

**说明**：此 ConstraintTemplate 禁止在所有命名空间（除系统例外）创建特权容器。报告应建议立即部署此类策略。

### 4.2 报告中的 Pod Security Standard 标签

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

**说明**：建议在所有业务命名空间强制 `restricted` 标准，作为纵深防御的第一层。

### 4.3 报告中的网络策略模板

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-metadata
  namespace: production
spec:
  podSelector: {}
  policyTypes:
  - Egress
  egress:
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
        - 169.254.0.0/16
```

**说明**：通过 CIDR 排除元数据网段，阻断云元数据 SSRF 路径。应在报告中强调此策略需在每一个命名空间落地。

### 4.4 报告中的镜像签名验证（Kyverno 策略示例）

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-image-signature
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-signature
    match:
      resources:
        kinds:
        - Pod
    verifyImages:
    - imageReferences:
      - "*"
      attestors:
      - entries:
        - keys:
            publicKeys: |
              -----BEGIN PUBLIC KEY-----
              MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
              -----END PUBLIC KEY-----
```

**说明**：要求报告中建议引入 Kyverno 或 OPA 对镜像签名进行强制校验，防止供应链投毒。

### 4.5 报告中的 CVSS 评分要素

| 指标 | 取值 | 说明 |
|------|------|------|
| Attack Vector | Network | 通过网络即可利用 |
| Attack Complexity | Low | 无需复杂条件 |
| Privileges Required | Low | 仅需命名空间 edit 权限 |
| User Interaction | None | 无需用户交互 |
| Scope | Changed | 可突破命名空间边界 |
| Confidentiality | High | 可读取所有 Secret 和云凭证 |
| Integrity | High | 可修改集群配置和镜像 |
| Availability | High | 可导致服务中断 |

**说明**：基于上述指标，此类漏洞通常评分为 **CVSS 3.1 9.9 (Critical)**。报告中必须明确评分依据，避免与业务方产生争议。
