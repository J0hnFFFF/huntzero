"""统一的 LLM Config 构建（SPEC 5.2：合并 kimi_hive 与 server.core 的重复实现）。

行为契约（与两处旧实现逐字段等价）：
- provider: type="kimi", base_url=os.environ["KIMI_BASE_URL"] 或默认, timeout=3600.0
- model: os.environ["KIMI_MODEL_NAME"] 或 "kimi-for-coding", max_context_size=262144

注意：kimi-agent-sdk 的 Config 是 pydantic 模型，LLMProvider 没有 timeout 字段，
provider dict 中的 timeout 键会被静默丢弃。保留该键仅为与旧实现字段形态一致
（registry #12 已查明无运行时影响），调用方不得依赖它。
"""

from __future__ import annotations

import os

from kimi_agent_sdk import Config

DEFAULT_BASE_URL = "https://api.kimi.com/coding/v1"
DEFAULT_MODEL = "kimi-for-coding"


def build_llm_config(api_key: str) -> Config:
    """构建 kimi-agent-sdk Config（字段与旧 build_config 完全一致）。"""
    base_url = os.environ.get("KIMI_BASE_URL", DEFAULT_BASE_URL)
    model_name = os.environ.get("KIMI_MODEL_NAME", DEFAULT_MODEL)
    return Config(
        default_model=model_name,
        providers={
            "kimi": {
                "type": "kimi",
                "base_url": base_url,
                "api_key": api_key,
                "timeout": 3600.0,
            }
        },
        models={
            model_name: {
                "provider": "kimi",
                "model": model_name,
                "max_context_size": 262144,
            }
        },
    )
