"""
tools/finops_monitor.py — kimiSec FinOps 实时遥测与成本监控

功能：
  1. 记录每次 LLM 调用的 token 估算与成本
  2. 监控 Session Pool 复用率
  3. 检测异常流量（单次请求 token 暴增、调用频率激增）
  4. 扫描结束后输出成本报告

成本参考（每百万 token，美元）：
  Moonshot Kimi (kimi-latest)  | Input: $0.15  | Output: $0.60
  Moonshot Kimi (kimi-long-context) | Input: $0.60 | Output: $1.60

Token 估算：
  - 英文：1 token ≈ 4 字符
  - 中文：1 token ≈ 1-2 字符（Kimi 对中文更友好）
  - 实际精确值需从 API 响应头获取，此处使用估算值

使用方式：
  from tools.finops_monitor import finops_monitor

  # 在 kimi_hive.py 扫描结束时
  finops_monitor.print_report()

  # Session Pool 复用率
  finops_monitor.print_pool_stats(pool)
"""
import time
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Any, Optional

# ─────────────────────────────────────────────────────────────────────────────
#  成本常数
# ─────────────────────────────────────────────────────────────────────────────

COST_PER_MILLION = {
    # 格式：(input_cost, output_cost)，美元
    "kimi-latest":        (0.15, 0.60),
    "kimi-long-context":  (0.60, 1.60),
    "default":            (0.15, 0.60),  # 默认用 kimi-latest
}


def estimate_tokens(text: str) -> int:
    """估算文本的 token 数量（保守估计）。"""
    if not text:
        return 0
    # 中文字符单独计数
    chinese_chars = sum(1 for c in text if '\u4e00' <= c <= '\u9fff')
    other_chars = len(text) - chinese_chars
    # 中文：1 token ≈ 1.5 字符；其他：1 token ≈ 4 字符
    return int(chinese_chars / 1.5 + other_chars / 4.0)


def calculate_cost(tokens_in: int, tokens_out: int, model: str = "kimi-latest") -> float:
    """计算单次调用的美元成本。"""
    input_cost, output_cost = COST_PER_MILLION.get(model, COST_PER_MILLION["default"])
    return (tokens_in * input_cost + tokens_out * output_cost) / 1_000_000


# ─────────────────────────────────────────────────────────────────────────────
#  FinOps 记录条目
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class CallRecord:
    role: str           # "cerebrum" | "drone-<role>"
    model: str
    tokens_in: int
    tokens_out: int
    cost_usd: float
    latency_ms: float
    timestamp: float
    hypothesis_count: int = 0   # 添加时的假设总数（用于成本分析）


# ─────────────────────────────────────────────────────────────────────────────
#  FinOps 监控器（单例）
# ─────────────────────────────────────────────────────────────────────────────

class FinOpsMonitor:
    """
    FinOps 实时监控器。

    使用方式：
        finops_monitor = FinOpsMonitor()

        # 每次 LLM 调用后记录
        finops_monitor.record(
            role="drone-vuln-hunter",
            tokens_in=1200,
            tokens_out=800,
            model="kimi-latest",
        )

        # 扫描结束时输出报告
        finops_monitor.print_report()
    """

    def __init__(self, daily_budget_usd: float = 20.0):
        self.daily_budget_usd = daily_budget_usd
        self.daily_cost = 0.0
        self.day_start = time.time()
        self.records: list[CallRecord] = []
        self._call_count_window: list[float] = []   # 用于流量激增检测
        self._alert_triggered = False
        self._large_call_warned = False              # Issue-17 修复：在 __init__ 中统一初始化

    def record(
        self,
        role: str,
        tokens_in: int,
        tokens_out: int,
        model: str = "kimi-latest",
        latency_ms: float = 0.0,
        hypothesis_count: int = 0,
    ) -> None:
        """记录一次 LLM 调用。"""
        cost = calculate_cost(tokens_in, tokens_out, model)
        self.daily_cost += cost

        record = CallRecord(
            role=role,
            model=model,
            tokens_in=tokens_in,
            tokens_out=tokens_out,
            cost_usd=cost,
            latency_ms=latency_ms,
            timestamp=time.time(),
            hypothesis_count=hypothesis_count,
        )
        self.records.append(record)

        # 更新流量窗口
        self._call_count_window.append(time.time())
        if len(self._call_count_window) > 1000:
            self._call_count_window = self._call_count_window[-500:]

        # 触发告警检查
        self._check_alerts(record)

    def record_from_text(
        self,
        role: str,
        prompt_text: str,
        response_text: str,
        model: str = "kimi-latest",
        latency_ms: float = 0.0,
    ) -> None:
        """通过文本内容估算 token 并记录（无 SDK token 计数时使用）。"""
        tokens_in = estimate_tokens(prompt_text)
        tokens_out = estimate_tokens(response_text)
        self.record(
            role=role,
            tokens_in=tokens_in,
            tokens_out=tokens_out,
            model=model,
            latency_ms=latency_ms,
        )

    def _check_alerts(self, record: CallRecord) -> None:
        """检查是否触发告警条件。"""
        # 流量激增：15 分钟内 > 100 次调用
        now = time.time()
        recent_calls = [t for t in self._call_count_window if t > now - 900]
        if len(recent_calls) > 100 and not self._alert_triggered:
            self._alert_triggered = True
            self._last_alert = {
                "type": "traffic_spike",
                "calls_15min": len(recent_calls),
                "ts": now,
            }

        # 单次调用成本异常：超过 $0.50
        if record.cost_usd > 0.50 and not getattr(self, "_large_call_warned", False):
            self._large_call_warned = True
            self._last_alert = {
                "type": "large_call",
                "cost_usd": record.cost_usd,
                "role": record.role,
                "ts": now,
            }

    def _reset_daily_if_needed(self) -> None:
        if time.time() - self.day_start > 86400:
            self.daily_cost = 0.0
            self.day_start = time.time()

    def print_report(self, console=None) -> None:
        """在终端输出成本报告。"""
        if console is None:
            try:
                from rich.console import Console
                console = Console()
            except ImportError:
                print("FinOps Report (pip install rich for formatted output)")
                console = None

        self._reset_daily_if_needed()

        if not self.records:
            if console:
                console.print("[dim]暂无遥测数据[/dim]")
            else:
                print("No telemetry data.")
            return

        total_cost = sum(r.cost_usd for r in self.records)
        total_tokens_in = sum(r.tokens_in for r in self.records)
        total_tokens_out = sum(r.tokens_out for r in self.records)
        total_tokens = total_tokens_in + total_tokens_out
        avg_latency = sum(r.latency_ms for r in self.records) / len(self.records) if self.records else 0
        total_calls = len(self.records)

        if console:
            from rich.table import Table
            from rich import box

            table = Table(title="⚡ kimiSec FinOps Report", box=box.ROUNDED)
            table.add_column("指标", style="cyan")
            table.add_column("数值", style="green")
            table.add_column("备注", style="dim")

            budget_pct = total_cost / self.daily_budget_usd * 100
            budget_bar = "█" * int(budget_pct / 5) + "░" * (20 - int(budget_pct / 5))

            table.add_row("总调用次数", str(total_calls), "")
            table.add_row("输入 Token", f"{total_tokens_in:,}", "")
            table.add_row("输出 Token", f"{total_tokens_out:,}", "")
            table.add_row("总 Token", f"{total_tokens:,}", f"≈ ${total_cost:.4f}")
            table.add_row("总成本 (USD)", f"${total_cost:.4f}", f"/ ${self.daily_budget_usd:.0f} 日预算")
            table.add_row("日预算消耗", f"{budget_pct:.1f}%", budget_bar)
            table.add_row("平均延迟", f"{avg_latency:.0f} ms", "")
            table.add_row("每次调用均价", f"${total_cost / total_calls:.6f}", "")

            console.print()
            console.print(table)

            # ── 按角色分组 ──
            role_table = Table(title="按角色分组成本", box=box.SIMPLE)
            role_table.add_column("角色")
            role_table.add_column("调用次数")
            role_table.add_column("总成本 (USD)")
            role_table.add_column("占比")
            role_table.add_column("Token 总量")

            by_role: dict = defaultdict(lambda: {"count": 0, "cost": 0.0, "tokens": 0})
            for r in self.records:
                by_role[r.role]["count"] += 1
                by_role[r.role]["cost"] += r.cost_usd
                by_role[r.role]["tokens"] += r.tokens_in + r.tokens_out

            for role, data in sorted(by_role.items(), key=lambda x: -x[1]["cost"]):
                pct = data["cost"] / total_cost * 100 if total_cost > 0 else 0
                role_table.add_row(
                    role,
                    str(data["count"]),
                    f"${data['cost']:.4f}",
                    f"{pct:.1f}%",
                    f"{data['tokens']:,}",
                )
            console.print()
            console.print(role_table)

            # ── 告警信息 ──
            if getattr(self, "_alert_triggered", False):
                alert = getattr(self, "_last_alert", {})
                alert_type = alert.get("type", "")
                if alert_type == "traffic_spike":
                    console.print(
                        f"\n[bold yellow]⚠️  ALERT: 流量激增检测 — "
                        f"15分钟内 {alert.get('calls_15min')} 次调用（可能为 Bot 或异常）[/]"
                    )
                elif alert_type == "large_call":
                    console.print(
                        f"\n[bold yellow]⚠️  ALERT: 单次大请求 — "
                        f"${alert.get('cost_usd'):.4f} ({alert.get('role')})[/]"
                    )

        else:
            # 无 rich 时的朴素输出
            print(f"\n=== FinOps Report ===")
            print(f"Total calls:    {total_calls}")
            print(f"Total tokens:   {total_tokens:,} (in: {total_tokens_in:,}, out: {total_tokens_out:,})")
            print(f"Total cost:     ${total_cost:.4f} / ${self.daily_budget_usd:.0f}")
            print(f"Avg latency:    {avg_latency:.0f} ms")
            print(f"Per-call cost: ${total_cost / total_calls:.6f}")

    def print_pool_stats(self, pool: Any, console=None) -> None:
        """输出 Session Pool 复用率统计。"""
        if console is None:
            try:
                from rich.console import Console
                console = Console()
            except ImportError:
                console = None

        if pool is None:
            return

        stats = pool.get_stats()
        total_slots = sum(v["total"] for v in stats.values())
        total_uses = sum(v["total_uses"] for v in stats.values())

        if console:
            from rich.table import Table
            from rich import box

            table = Table(title="🔗 Session Pool 复用统计", box=box.ROUNDED)
            table.add_column("Role")
            table.add_column("总 Slots")
            table.add_column("可用")
            table.add_column("复用次数")

            for role, v in sorted(stats.items(), key=lambda x: -x[1]["total_uses"]):
                table.add_row(
                    role,
                    str(v["total"]),
                    str(v["available"]),
                    str(v["total_uses"]),
                )

            console.print()
            console.print(table)
            if total_slots > 0 and total_uses > 0:
                reuse_rate = (total_uses - total_slots) / max(total_uses, 1) * 100
                console.print(
                    f"[dim]复用率: {reuse_rate:.0f}% "
                    f"（{total_slots} 个预热 slot 共承载 {total_uses} 次调用）[/]"
                )
        else:
            print(f"\n=== Session Pool Stats ===")
            print(f"Total slots: {total_slots}, Total uses: {total_uses}")


# ─────────────────────────────────────────────────────────────────────────────
#  全局单例（便于从 Cerebrum/Drone 中调用）
# ─────────────────────────────────────────────────────────────────────────────

_finops_instance: Optional[FinOpsMonitor] = None


def get_finops() -> FinOpsMonitor:
    """获取或创建 FinOps 全局单例。"""
    global _finops_instance
    if _finops_instance is None:
        _finops_instance = FinOpsMonitor()
    return _finops_instance


def get_monitor() -> FinOpsMonitor:
    """获取全局 FinOps 监控器单例（推荐使用此名称，避免与类名 FinOpsMonitor 混淆）。"""
    return get_finops()


def finops_monitor() -> FinOpsMonitor:
    """
    [已废弃] 请改用 get_monitor()。
    此别名仅作向后兼容保留。
    """
    import sys
    print(
        "Warning: finops_monitor() 已废弃，请使用 get_monitor() 替代。",
        file=sys.stderr,
    )
    return get_finops()
