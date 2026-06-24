"""Compatibility helpers for kimi-agent-sdk / kimi-cli version skew."""

from __future__ import annotations

import inspect
from typing import Any


def _patch_typing_generic_alias() -> None:
    """Work around CPython 3.12.0 frozen+slots dataclass bug.

    In Python 3.12.0, a frozen ``@dataclass(slots=True)`` generates an
    ``__setattr__`` whose closure captures a stale class object. When
    ``typing._GenericAlias.__call__`` later tries to set
    ``result.__orig_class__ = self`` on an instance of such a class, the
    generated ``__setattr__`` calls ``super(type, obj)`` with the stale
    class and raises ``TypeError: super(type, obj): obj must be an instance
    or subtype of type``.

    This breaks imports of ``kimi_cli`` / ``kimi_agent_sdk`` because
    ``kimi_cli.utils.slashcmd.SlashCommand`` is exactly such a dataclass.

    The patch widens the existing ``AttributeError`` swallow in
    ``typing._GenericAlias.__call__`` to also ignore ``TypeError``.
    ``__orig_class__`` is a runtime introspection helper and is safe to
    skip.
    """
    try:
        import typing
    except Exception:
        return

    alias_cls = getattr(typing, "_GenericAlias", None)
    if alias_cls is None:
        return

    original_call = alias_cls.__call__
    if getattr(original_call, "_kimisec_patched", False):
        return

    def __call__(self, *args: Any, **kwargs: Any) -> Any:
        if not self._inst:
            raise TypeError(
                f"Type {self._name} cannot be instantiated; "
                f"use {self.__origin__.__name__}() instead"
            )
        result = self.__origin__(*args, **kwargs)
        try:
            result.__orig_class__ = self
        except AttributeError:
            pass
        except TypeError:
            # CPython 3.12.0 frozen+slots dataclass bug.
            pass
        return result

    __call__._kimisec_patched = True
    alias_cls.__call__ = __call__


# Apply immediately at import time so the workaround is active before any
# downstream module imports kimi_agent_sdk / kimi_cli.
_patch_typing_generic_alias()


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
