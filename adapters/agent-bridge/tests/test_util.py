import os
from pathlib import Path

from oar_agent_bridge.util import atomic_write_json


def test_atomic_write_json_uses_owner_only_permissions(tmp_path: Path):
    path = tmp_path / "auth.json"

    atomic_write_json(path, {"secret": "value"})

    assert path.exists()
    assert (path.stat().st_mode & 0o777) == 0o600
