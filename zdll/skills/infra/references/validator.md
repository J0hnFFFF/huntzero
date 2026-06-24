---
description: 验证基础设施漏洞影响：从命名空间逃逸到节点沦陷、集群接管乃至云账户接管。
tags: [infra, kubernetes, impact-assessment, cloud-security]
---

# 影响验证 (Validator)

## 1. 专家直觉触发器 (Triggers)

专家在评估基础设施漏洞影响时，会立即关注以下升级信号：

- **命名空间边界消失**：攻击者在 `dev` 命名空间内读取到了 `kube-system` 的 Secret，或能创建跨命名空间的资源。
- **节点文件系统被访问**：容器内可以读取 `/etc/kubernetes/manifests`、`/var/lib/kubelet` 或宿主 `/root/.ssh`。
- **集群级凭证泄露**：获取了 `cluster-admin` 的 ServiceAccount Token、etcd 客户端证书或 API Server 的 `kubeconfig`。
- **云元数据响应成功**：从集群内部成功访问 `169.254.169.254` 并拿到 IAM 临时凭证。
- **控制平面组件可写**：能够修改 API Server 的静态 Pod 清单、CoreDNS 配置或 etcd 数据目录。

这些触发器标志着漏洞已从“应用层问题”升级为“基础设施灾难”。

## 2. 不可跳过的问题链 (Question Chain)

在给出最终影响评级前，必须回答：

1. **数据泄露的边界在哪里？**  
   单个 Secret 泄露还是整个 etcd 数据库可读取？泄露的数据是否包含云厂商 API 密钥？
2. **横向移动的潜力有多大？**  
   获得一个节点后，是否能通过 DaemonSet 或节点共享的网络存储感染所有节点？
3. **持久化是否难以清除？**  
   攻击者是否修改了静态 Pod、植入了 Webhook、创建了不可见的 `shadow` API Server？
4. **业务连续性与合规影响？**  
   集群失控是否导致生产服务中断？是否涉及 GDPR、等保或 SOC2 范围内的敏感数据？
5. **修复的复杂度与成本？**  
   是否需要轮换全部证书、重建所有节点、重新签发云 IAM 凭证？平均恢复时间（MTTR）是多少？
6. **是否存在级联效应？**  
   该集群是否管理其他集群（如 Fleet、Rancher、EKS Anywhere）？是否共享 VPC、IAM 角色或镜像仓库？

## 3. 攻击链闭合 (Attack Chain Closure)

影响验证必须给出从“初始访问”到“完全控制”的完整证明。以下是标准的四级影响模型：

**Level 1：命名空间逃逸 (Namespace Escape)**  
攻击者在受限命名空间内通过创建特权 Pod 挂载 `hostPath` 或利用 `hostPID`，获得了对宿主节点的文件系统或进程空间的读取权限。此时影响仅限于单个节点，但已经打破了 K8s 最核心的安全边界。

**Level 2：节点沦陷 (Node Compromise)**  
攻击者在节点上读取了 kubelet 的 `kubeconfig` 或启动的静态 Pod 定义，从而以节点身份与 API Server 通信。节点身份通常拥有列出所有 Pod、读取节点级 Secret 的权限。

**Level 3：集群接管 (Cluster Takeover)**  
利用节点身份或直接从 API Server 窃取的 `cluster-admin` Token，攻击者创建了持久的 `ClusterRoleBinding`、修改了关键 Controller（如 `coredns`）的镜像，或注册了恶意的 MutatingWebhook，实现了对整个集群的持久化控制。

**Level 4：云账户接管 (Cloud Account Takeover)**  
从节点或特权 Pod 访问云元数据服务，获取节点实例的 IAM 临时凭证。利用该凭证调用云 API（如 AWS EC2、IAM、S3），攻击者可修改安全组、访问其他实例、甚至创建新的管理员用户，从而跳出 K8s 边界，控制底层云账户。

**逻辑闭环**：如果攻击者能在集群内执行代码，且集群网络未禁止访问元数据服务，则 Level 4 是几乎必然的结果。因此，**集群内 RCE + 元数据可达 = 云账户沦陷**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的网络策略（漏洞模式）

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-all
  namespace: default
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - {}
  egress:
  - {}
```

**风险**：`{}` 表示允许所有入站和出站流量。Pod 可以不受限制地访问云元数据 IP、内部数据库和其他命名空间的服务。

### 4.2 安全的网络策略（缓解模式）

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-metadata
  namespace: default
spec:
  podSelector: {}
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: kube-system
    ports:
    - protocol: UDP
      port: 53
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
        - 169.254.169.254/32
```

**缓解**：默认拒绝出站流量，仅允许 DNS（kube-system）和除元数据 IP 外的公网访问。从网络层阻断 SSRF 到云元数据的路径。

### 4.3 危险的 Pod 安全标准豁免（漏洞模式）

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: insecure
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: restricted
```

**风险**：将命名空间标记为 `privileged`，允许任何用户在该命名空间创建特权 Pod，直接摧毁节点隔离。

### 4.4 安全的 Pod 安全标准（缓解模式）

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: secure
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: latest
```

**缓解**：强制使用 `restricted` 标准，禁止特权容器、hostPath、hostPID 和 root 用户，从根本上消除 Level 1 逃逸面。

### 4.5 云元数据影响验证脚本

```bash
#!/bin/bash
# validate_cloud_impact.sh
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600" 2>/dev/null)
if [ -z "$TOKEN" ]; then
  echo "[-] Metadata service not reachable or IMDSv2 enforced"
  exit 1
fi
ROLE=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/)
echo "[!] Compromised role: $ROLE"
curl -s -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/iam/security-credentials/$ROLE" | jq .
```

**用途**：在已逃逸的节点或 Pod 内运行，量化云凭证泄露的直接影响。若能获取临时凭证，则影响评级必须上升至 Level 4。

## 5. 影响量化矩阵

| 影响等级 | 技术表现 | 业务影响 | 恢复成本 | 示例 |
|---------|---------|---------|---------|------|
| Level 1 | 命名空间逃逸 | 单个团队数据泄露 | 删除恶意 Pod，审计 RBAC | 读取同节点其他 Pod 的内存 |
| Level 2 | 节点沦陷 | 该节点所有服务受影响 | 驱逐节点，重建 kubelet 凭证 | 使用 kubeconfig 访问 API |
| Level 3 | 集群接管 | 全集群配置、密钥泄露 | 轮换所有证书，重建控制平面 | 创建后门 ClusterRoleBinding |
| Level 4 | 云账户接管 | 跨服务、跨区域数据泄露 | 轮换云 IAM，审计所有资源 | 窃取 S3 数据，修改安全组 |

**说明**：报告应明确标注漏洞可达的最高等级。即使当前仅验证到 Level 2，也必须在报告中指出 Level 4 的理论可能性，并建议相应的网络隔离与 IAM 加固措施。
