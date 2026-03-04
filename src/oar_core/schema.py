"""Utilities for reading shared schema metadata."""

from __future__ import annotations

import re
from pathlib import Path

DEFAULT_SCHEMA_PATH = Path(__file__).resolve().parents[2] / "contracts" / "oar-schema.yaml"
_VERSION_PATTERN = re.compile(r'^\s*version\s*:\s*["\']?([^"\']+)["\']?\s*$')


def read_schema_version(schema_path: Path = DEFAULT_SCHEMA_PATH) -> str:
    """Return the top-level schema version from contracts/oar-schema.yaml."""
    if not schema_path.exists():
        raise FileNotFoundError(f"Schema file not found: {schema_path}")

    for line in schema_path.read_text(encoding="utf-8").splitlines():
        match = _VERSION_PATTERN.match(line)
        if match:
            return match.group(1).strip()

    raise ValueError(f"No top-level version key found in schema file: {schema_path}")
