"""engine/llm_config.py 统一 LLM Config 构建（SPEC 5.2，Task 7 字面量断言版）。

两处旧实现（kimi_hive.build_config 与 server.core 变体）已删除，等价性
改为逐字段字面量钉住。注意：pydantic 会静默丢弃 provider dict 中的
timeout 键（LLMProvider 无该字段，registry #12 已查明无影响），故不对
timeout 做任何断言。
"""

import pytest

from engine.llm_config import build_llm_config


@pytest.fixture
def clean_env(monkeypatch):
    monkeypatch.delenv("KIMI_BASE_URL", raising=False)
    monkeypatch.delenv("KIMI_MODEL_NAME", raising=False)


def test_default_base_url_literal(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert cfg.providers["kimi"].base_url == "https://api.kimi.com/coding/v1"


def test_default_model_literal(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert cfg.default_model == "kimi-for-coding"
    assert set(cfg.models) == {"kimi-for-coding"}
    assert cfg.models["kimi-for-coding"].model == "kimi-for-coding"


def test_max_context_size_literal(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert cfg.models["kimi-for-coding"].max_context_size == 262144


def test_provider_type_kimi(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert set(cfg.providers) == {"kimi"}
    assert cfg.providers["kimi"].type == "kimi"


def test_api_key_passed_through(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert cfg.providers["kimi"].api_key.get_secret_value() == "sk-test-key"


def test_model_provider_link(clean_env):
    cfg = build_llm_config("sk-test-key")
    assert cfg.models["kimi-for-coding"].provider == "kimi"


def test_env_override_base_url(monkeypatch):
    monkeypatch.setenv("KIMI_BASE_URL", "https://gateway.example.com/v1")
    monkeypatch.delenv("KIMI_MODEL_NAME", raising=False)
    cfg = build_llm_config("sk-test-key")
    assert cfg.providers["kimi"].base_url == "https://gateway.example.com/v1"


def test_env_override_model(monkeypatch):
    monkeypatch.delenv("KIMI_BASE_URL", raising=False)
    monkeypatch.setenv("KIMI_MODEL_NAME", "custom-model")
    cfg = build_llm_config("sk-test-key")
    assert cfg.default_model == "custom-model"
    assert set(cfg.models) == {"custom-model"}
    assert cfg.models["custom-model"].model == "custom-model"
    assert cfg.models["custom-model"].provider == "kimi"
