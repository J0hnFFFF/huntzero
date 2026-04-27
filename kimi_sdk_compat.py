"""Compatibility helpers for kimi-agent-sdk / kimi-cli version skew."""

from __future__ import annotations

import inspect
from typing import Any


_PATCH_FLAG = "_kimisec_skills_dir_compat"


def patch_kimi_agent_sdk() -> None:
    """Patch kimi-agent-sdk 0.0.x for kimi-cli versions using skills_dirs."""
    try:
        import kimi_agent_sdk._session as sdk_session
    except Exception:
        return

    kimi_cli = getattr(sdk_session, "KimiCLI", None)
    if kimi_cli is None:
        return

    original_create = getattr(kimi_cli, "create", None)
    if original_create is None or getattr(original_create, _PATCH_FLAG, False):
        return

    try:
        params = inspect.signature(original_create).parameters
    except (TypeError, ValueError):
        return

    if "skills_dir" in params:
        return

    supports_skills_dirs = "skills_dirs" in params

    async def create_compat(*args: Any, **kwargs: Any) -> Any:
        skills_dir = kwargs.pop("skills_dir", None)
        if supports_skills_dirs and skills_dir is not None and "skills_dirs" not in kwargs:
            if isinstance(skills_dir, (list, tuple)):
                kwargs["skills_dirs"] = list(skills_dir)
            else:
                kwargs["skills_dirs"] = [skills_dir]
        return await original_create(*args, **kwargs)

    setattr(create_compat, _PATCH_FLAG, True)
    setattr(kimi_cli, "create", staticmethod(create_compat))
