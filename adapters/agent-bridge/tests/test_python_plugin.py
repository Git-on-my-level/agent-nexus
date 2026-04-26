import sys
from pathlib import Path
from unittest import mock

import pytest

from anx_agent_bridge.adapters.base import AdapterResult
from anx_agent_bridge.adapters.python_plugin import load_plugin_adapter
from anx_agent_bridge.models import WakePacket


def test_load_plugin_strips_whitespace_in_names(tmp_path: Path) -> None:
    plug = tmp_path / "stripn.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="st", native_session_id=None)

def build(cfg):
    return Ad(cfg)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("  stripn ", "  build  ", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_factory_with_config(tmp_path: Path) -> None:
    plug = tmp_path / "myplug.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"adapter_kind": "myplug", "ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="p:" + prompt_text[:10], native_session_id="s1")

def build(cfg):
    return Ad(cfg)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("myplug", "build", {"kind": "python_plugin"})
        assert ad.doctor()["adapter_kind"] == "myplug"
        pkt = WakePacket(
            wakeup_id="w",
            handle="h",
            actor_id="a",
            workspace_id="ws",
            workspace_name="M",
            thread_id="t",
            thread_title="T",
            trigger_event_id="e",
            trigger_created_at="2026-01-01T00:00:00Z",
            trigger_author_actor_id="u",
            trigger_text="x",
            current_summary="s",
            session_key="k",
            anx_base_url="http://x",
            thread_context_url="http://x/c",
            thread_workspace_url="http://x/w",
            trigger_event_url="http://x/e",
            cli_thread_inspect="",
            cli_thread_workspace="",
        )
        r = ad.dispatch(pkt, "hello", "k", None)
        assert r.response_text.startswith("p:")
    finally:
        sys.path.pop(0)


def test_load_plugin_missing_module() -> None:
    with pytest.raises(ValueError, match="failed to import"):
        load_plugin_adapter("nonexistent_module_xyz", "f", {})


def test_load_plugin_keyword_only_config_param(tmp_path: Path) -> None:
    plug = tmp_path / "kwplug.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="kw", native_session_id=None)

def build(*, config):
    return Ad(config)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("kwplug", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_positional_with_optional_kwonly(tmp_path: Path) -> None:
    plug = tmp_path / "kwopt.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="ko", native_session_id=None)

def build(cfg, *, option=False):
    return Ad(cfg)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("kwopt", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_positional_wins_over_optional_kwonly_cfg_name(tmp_path: Path) -> None:
    plug = tmp_path / "datacfg.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="dc", native_session_id=None)

def build(data, *, cfg=None):
    return Ad(data)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("datacfg", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_var_positional_passes_config(tmp_path: Path) -> None:
    plug = tmp_path / "varp.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="vp", native_session_id=None)

def build(*args):
    return Ad(args[0])
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("varp", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_class_factory_one_positional_cfg(tmp_path: Path) -> None:
    plug = tmp_path / "clsone.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="c1", native_session_id=None)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("clsone", "Ad", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_class_factory_kwargs_only(tmp_path: Path) -> None:
    plug = tmp_path / "clsplug.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, **kwargs):
        self.cfg = kwargs
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="cl", native_session_id=None)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("clsplug", "Ad", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_class_factory_kwonly_config_and_kwargs(tmp_path: Path) -> None:
    plug = tmp_path / "clskw.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, **kwargs):
        self.cfg = kwargs
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="ck", native_session_id=None)

class Ad2:
    def __init__(self, *, config=None, **kwargs):
        self.cfg = config if config is not None else kwargs
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="ck2", native_session_id=None)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("clskw", "Ad2", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_positional_plus_var_keyword(tmp_path: Path) -> None:
    plug = tmp_path / "pvk.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="pvk", native_session_id=None)

def build(cfg, **kwargs):
    return Ad(cfg)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("pvk", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_rejects_duplicate_kwonly_config_param_names(tmp_path: Path) -> None:
    plug = tmp_path / "dupkw.py"
    plug.write_text(
        """
def build(*, config, cfg):
    return object()
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with pytest.raises(ValueError, match="more than one"):
            load_plugin_adapter("dupkw", "build", {"kind": "python_plugin"})
    finally:
        sys.path.pop(0)


def test_load_plugin_rejects_duplicate_kwonly_config_with_var_keyword(tmp_path: Path) -> None:
    plug = tmp_path / "dupvkw.py"
    plug.write_text(
        """
def build(*, config, cfg, **kwargs):
    return object()
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with pytest.raises(ValueError, match="more than one"):
            load_plugin_adapter("dupvkw", "build", {"kind": "python_plugin"})
    finally:
        sys.path.pop(0)


def test_load_plugin_var_keyword_passes_config(tmp_path: Path) -> None:
    plug = tmp_path / "vkplug.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, cfg):
        self.cfg = cfg
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="vk", native_session_id=None)

def build(**kwargs):
    return Ad(kwargs)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        ad = load_plugin_adapter("vkplug", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_signature_failure_does_not_swallow_factory_typeerror(tmp_path: Path) -> None:
    plug = tmp_path / "sigbad.py"
    plug.write_text(
        """
def build(**kwargs):
    raise TypeError("internal validation failed")
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with mock.patch("anx_agent_bridge.adapters.python_plugin.inspect.signature", side_effect=ValueError("nope")):
            with pytest.raises(TypeError, match="internal validation"):
                load_plugin_adapter("sigbad", "build", {"kind": "python_plugin"})
    finally:
        sys.path.pop(0)


def test_load_plugin_signature_failure_still_invokes_kwargs_factory(tmp_path: Path) -> None:
    plug = tmp_path / "sigfail.py"
    plug.write_text(
        """
from anx_agent_bridge.adapters.base import AdapterResult

class Ad:
    def __init__(self, **kwargs):
        self.cfg = kwargs
    def doctor(self):
        return {"ok": True}
    def dispatch(self, packet, prompt_text, session_key, existing_native_session_id=None):
        return AdapterResult(response_text="sf", native_session_id=None)

def build(**kwargs):
    return Ad(**kwargs)
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with mock.patch("anx_agent_bridge.adapters.python_plugin.inspect.signature", side_effect=ValueError("nope")):
            ad = load_plugin_adapter("sigfail", "build", {"kind": "python_plugin"})
        assert ad.cfg["kind"] == "python_plugin"
    finally:
        sys.path.pop(0)


def test_load_plugin_rejects_two_positionals_with_var_keyword(tmp_path: Path) -> None:
    plug = tmp_path / "mixkw.py"
    plug.write_text(
        """
def build(ctx, other, **kwargs):
    return object()
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with pytest.raises(ValueError, match=r"cannot mix.*\*\*kwargs"):
            load_plugin_adapter("mixkw", "build", {"kind": "python_plugin"})
    finally:
        sys.path.pop(0)


def test_load_plugin_rejects_zero_arg_factory(tmp_path: Path) -> None:
    plug = tmp_path / "zero.py"
    plug.write_text(
        """
def build():
    return object()
""",
        encoding="utf-8",
    )
    sys.path.insert(0, str(tmp_path))
    try:
        with pytest.raises(ValueError, match="must accept"):
            load_plugin_adapter("zero", "build", {"kind": "python_plugin"})
    finally:
        sys.path.pop(0)


def test_load_plugin_missing_factory(tmp_path: Path) -> None:
    plug = tmp_path / "empty.py"
    plug.write_text("x = 1\n", encoding="utf-8")
    sys.path.insert(0, str(tmp_path))
    try:
        with pytest.raises(ValueError, match="not found"):
            load_plugin_adapter("empty", "nope", {})
    finally:
        sys.path.pop(0)
