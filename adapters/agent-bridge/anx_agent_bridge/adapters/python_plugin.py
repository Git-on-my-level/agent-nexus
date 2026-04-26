"""Load an in-process adapter from an explicit module:factory callable.

``plugin_module`` / ``plugin_factory`` are trusted: they resolve to arbitrary importable code via
``importlib.import_module``. Use only with operator-controlled bridge config (not mergeable or
writable by untrusted tenants).
"""

from __future__ import annotations

import importlib
import inspect
from typing import Any

_CONFIG_PARAM_NAMES = ("config", "adapter_config", "cfg")

# Heuristic when inspect.signature fails: only retry a different call shape if TypeError
# looks like a parameter-binding error, not user code inside the factory.
_ARITY_TYPEERROR_MARKERS = (
    "got an unexpected keyword argument",
    "got multiple values for argument",
    "missing 1 required positional argument",
    "missing 2 required positional arguments",
    "missing 1 required keyword-only argument",
    "takes no positional arguments",
    "takes no keyword arguments",
    "takes from 0 to",
    "takes from 1 to",
    "positional argument but",
)


def _likely_arity_typeerror(exc: TypeError) -> bool:
    msg = str(exc).lower()
    if any(m in msg for m in _ARITY_TYPEERROR_MARKERS):
        return True
    # Common CPython suffix for arity errors, without matching loose "takes"/"missing" alone.
    return "was given" in msg and ("takes" in msg or "positional" in msg)


def load_plugin_adapter(module_name: str, factory_name: str, adapter_config: dict[str, Any]) -> Any:
    module_name = module_name.strip()
    factory_name = factory_name.strip()
    if not module_name:
        raise ValueError("python_plugin requires non-empty plugin_module")
    if not factory_name:
        raise ValueError("python_plugin requires non-empty plugin_factory")
    try:
        module = importlib.import_module(module_name)
    except ImportError as exc:
        raise ValueError(f"failed to import plugin_module {module_name!r}: {exc}") from exc
    factory = getattr(module, factory_name, None)
    if factory is None:
        raise ValueError(f"plugin_factory {factory_name!r} not found in module {module_name!r}")
    if not callable(factory):
        raise ValueError(f"plugin_factory {factory_name!r} is not callable")
    try:
        sig = inspect.signature(factory)
    except (TypeError, ValueError):
        try:
            return factory(**adapter_config)
        except TypeError as exc:
            if not _likely_arity_typeerror(exc):
                raise
            try:
                return factory(adapter_config)
            except TypeError as exc2:
                if _likely_arity_typeerror(exc2):
                    raise ValueError(
                        "python_plugin could not invoke factory "
                        "(inspect.signature failed; tried **config and one positional argument)"
                    ) from exc2
                raise

    params = list(sig.parameters.values())
    if inspect.isclass(factory) and params and params[0].name == "self":
        params = params[1:]

    if any(p.kind == inspect.Parameter.VAR_KEYWORD for p in params):
        non_var_kw = [p for p in params if p.kind != inspect.Parameter.VAR_KEYWORD]
        if len(non_var_kw) == 0:
            return factory(**adapter_config)
        if all(p.kind == inspect.Parameter.KEYWORD_ONLY for p in non_var_kw):
            names = {p.name for p in non_var_kw}
            targets = [n for n in _CONFIG_PARAM_NAMES if n in names]
            if len(targets) > 1:
                raise ValueError(
                    "python_plugin factory cannot declare more than one of "
                    "`config`, `adapter_config`, and `cfg`"
                )
            for name in _CONFIG_PARAM_NAMES:
                if name in names:
                    return factory(**{name: adapter_config})
        if len(non_var_kw) == 1 and non_var_kw[0].kind in (
            inspect.Parameter.POSITIONAL_ONLY,
            inspect.Parameter.POSITIONAL_OR_KEYWORD,
        ):
            return factory(adapter_config)
        raise ValueError(
            "python_plugin factory cannot mix `**kwargs` with other parameters "
            "(except keyword-only `config` / `adapter_config` / `cfg`, or one positional config); "
            "otherwise use only `**kwargs`, a single positional config, `*args`, or `*, config=`"
        )

    keyword_only = {p.name for p in params if p.kind == inspect.Parameter.KEYWORD_ONLY}
    positional = [
        p
        for p in params
        if p.kind in (inspect.Parameter.POSITIONAL_ONLY, inspect.Parameter.POSITIONAL_OR_KEYWORD)
    ]
    var_positional = [p for p in params if p.kind == inspect.Parameter.VAR_POSITIONAL]
    if var_positional:
        if len(positional) > 0:
            raise ValueError(
                "python_plugin factory cannot mix extra positional parameters with `*args`; "
                "use a single `*args` bucket or one positional config parameter"
            )
        return factory(adapter_config)

    if len(positional) > 1:
        raise ValueError(
            "python_plugin factory must take at most one positional config argument "
            "(or use only `**kwargs` / `*, config=` / `*args`)"
        )
    if len(positional) == 1:
        return factory(adapter_config)

    ko_targets = [n for n in _CONFIG_PARAM_NAMES if n in keyword_only]
    if len(ko_targets) > 1:
        raise ValueError(
            "python_plugin factory cannot declare more than one of "
            "`config`, `adapter_config`, and `cfg`"
        )
    for name in _CONFIG_PARAM_NAMES:
        if name in keyword_only:
            return factory(**{name: adapter_config})

    raise ValueError(
        "python_plugin factory must accept the `[adapter]` config dict via one positional argument, "
        "`*args`, keyword-only `config` / `adapter_config` / `cfg`, or only `**kwargs`"
    )
