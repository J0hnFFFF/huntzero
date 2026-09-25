# huntzero 使用指南

> 面向操作者。安装、配置、扫描参数、报告解读与故障排查。
> 引擎内部原理见 [architecture.md](architecture.md)。

## 1. 安装与配置

```bash
pip install -e ".[dev]"          # Python ≥ 3.11
export KIMI_API_KEY=sk-...       # 唯一必需的配置
```

可选配置（完整清单见 `.env.example`）：

```bash
export KIMI_BASE_URL=https://api.kimi.com/coding/v1   # 自定义端点
export KIMI_MODEL_NAME=kimi-for-coding                # 模型名
export OSV_SCANNER_PATH=/usr/local/bin/osv-scanner    # 依赖扫描（可选）
```

依赖扫描二进制可用 `python tools/install_osv.py` 安装到 `./bin/`；缺失时引擎自动跳过该阶段，不影响主流程。

## 2. 第一次扫描

```bash
huntzero scan https://github.com/owner/repo
huntzero scan /path/to/local/project
```

运行中终端实时打印推理轮次与发现；结束时打印分级摘要，并自动落盘：

```
reports/<target>_<timestamp>.md     # 完整报告
<work-dir>/.blackboard.json         # 全套状态（假设树/任务/发现）
<work-dir>/.audit_notes.md          # 分析轨迹
```

## 3. 参数详解

| 参数 | 默认 | 调大的后果 | 何时调整 |
|---|---|---|---|
| `--workers/-w N` | 5 | 并发工蜂更多，速度更快、瞬时成本更高 | 大目标提速；速率受限时调小 |
| `--max-rounds/-R N` | 30 | 主脑推理轮次更多，分析更深 | 复杂目标调大；摸底调小（如 10） |
| `--max-tasks/-T N` | 200 | 允许执行的微任务更多 | 与 `--max-rounds` 配合控制成本上限 |
| `--max-time/-t S` | 7200 | 墙钟上限（轮边界判定） | CI/定时任务设小值；深度审计设大 |
| `--stagnation/-s N` | 3 | 连续无新发现轮数更多才停 | 长尾发现多时调大（如 5） |
| `--work-dir/-d PATH` | `./local_workspace` | — | 隔离不同任务的产物目录 |
| `--resume/-r` | 关 | 从 `.blackboard.json` 恢复续跑 | 中断后继续、增量复扫 |
| `--auto-approve/--no-auto-approve` | 开 | `--no-` 时不可逆操作需人工按 Y 确认 | 人工值守的敏感目标 |
| `--output/-o PATH` | 自动 | 指定 `.md` 或 `.json` 报告路径 | 流水线集成 |
| `--no-report` | 关 | 不落盘报告（只看终端与黑板） | 快速试跑 |

**成本三要素**：`max-rounds × max-tasks × workers` 共同决定 LLM 调用量的上界。摸底用 `-R 10 -T 50`；深度审计用 `-R 60 -T 400 -t 21600`。

## 4. 场景配方

**快速摸底**（几分钟内看目标有没有明显问题）
```bash
huntzero scan /path/to/repo -R 10 -T 50 -w 4
```

**深度审计**（大型项目、可接受小时级时长）
```bash
huntzero scan /path/to/repo -R 60 -T 400 -t 21600 -w 8 -s 5
```

**大项目**（文件数多时引擎自动进入 Sector 分区模式，无需手动干预；
分区内预算按优先级自动分配，观察终端 Sector 摘要即可）

**中断恢复**
```bash
# 第一次扫描被 Ctrl+C 或超时中断
huntzero scan /path/to/repo -d ./local_workspace -r
```

**无人值守 / 流水线**
```bash
huntzero scan https://github.com/owner/repo -o /tmp/report.json --no-report \
  -R 30 -T 200 -t 3600
echo "exit=$?"
```

## 5. 报告解读

**Severity 分级**：critical / high / medium / low。报告优先展示 critical/high。

**Finding 结构**：标题、severity、置信度、目标位置（file:line）、证据链（工蜂取证过程）、
影响与利用条件、复现步骤（若验证过 PoC）。

**利用链图**：报告目录同时导出 `.dot` 与 `.graphml`，展示跨模块的利用链
（入口点 → 数据流 → 危险 sink）。用 Graphviz 渲染：`dot -Tsvg x.dot -o x.svg`。

**发现的可信度**：每条 Finding 都经过对抗性审查（防守方视角反驳）后才入报告；
被反驳但仍高置信的会带 `[auto-promoted]` 标记，表示"审查未通过但证据充分，建议人工复核"。

## 6. 工作目录与产物

```
<work-dir>/
├── projects/<name>/          # clone 下来的目标（URL 输入时）
├── .blackboard.json          # 假设树 + 任务 + 发现（续跑依据，可人工查阅）
├── .audit_notes.md           # 引擎增量写入的分析轨迹
├── tmp/.drone_sandboxes/     # 工蜂沙盒（运行中存在，结束清理）
└── reports/                  # 报告（也可用 --output 指定别处）
```

`.blackboard.json` 是 JSON 格式，可以直接用 `jq` 查看当时的假设树与发现：
```bash
jq '.findings[] | {severity, title}' local_workspace/.blackboard.json
```

## 7. 故障排查

| 现象 | 原因与处理 |
|---|---|
| 启动即报缺少 API Key | 未设置 `KIMI_API_KEY`；`huntzero scan --help` 前先 `export` |
| clone 失败 / 超时 | 目标需要访问凭据或网络受限；改用本地已 clone 的路径 |
| 日志出现 `Event loop is closed` 噪音 | 已知的 CPython `__del__` 时序噪音，引擎已做 stderr 过滤；出现少量无害 |
| 扫描停滞感明显但无输出 | 工蜂在沙盒中执行长任务（如模糊测试跑满预算）；结束或 Ctrl+C 后 `--resume` 续跑 |
| 依赖扫描阶段被跳过 | 未找到 osv-scanner（可选功能）；`python tools/install_osv.py` 后重试 |
| 成本超预期 | 收紧 `-R/-T`；cost 明细见扫描末端统计（`tools/finops_monitor.py`） |
| 中断后想接着跑 | 用同一 `-d` 目录加 `-r`，从黑板恢复全部状态 |
| 大项目跑很久 | 已自动进入 Sector 模式；可减少 `-R` 或提高 `-t`，观察扇区摘要 |

## 8. 引擎行为纪律（运维视角）

- **人工介入点**：仅在不可逆操作边界（默认 `--auto-approve` 自动放行）；
  设 `--no-auto-approve` 后终端会弹出仲裁请求等待 Y/N
- **安全性**：模糊测试与 PoC 执行全部在断网隔离容器中进行，不会访问外部网络
- **退出**：`Ctrl+C` 优雅退出并保留黑板状态，不会丢已完成的推理
