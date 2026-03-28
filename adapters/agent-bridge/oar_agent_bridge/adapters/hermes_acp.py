from __future__ import annotations

import logging
import os
import shutil
from pathlib import Path

from ..models import WakePacket
from .acp_client import ACPProcessClient
from .base import AdapterResult

LOGGER = logging.getLogger(__name__)


class HermesACPAdapter:
    def __init__(
        self,
        *,
        command: list[str],
        cwd_default: str,
        workspace_map: dict[str, str] | None = None,
        env: dict[str, str] | None = None,
        auto_select_permission: bool = True,
        permission_option_id: str | None = None,
    ) -> None:
        self.command = command
        self.cwd_default = cwd_default
        self.workspace_map = workspace_map or {}
        self.env = env
        self.auto_select_permission = auto_select_permission
        self.permission_option_id = permission_option_id
        self._client: ACPProcessClient | None = None

    def doctor(self) -> dict[str, str | int]:
        if not self.command:
            raise ValueError("Hermes ACP command is required")
        executable = self.command[0]
        if os.path.sep in executable:
            if not Path(executable).exists():
                raise ValueError(f"Hermes ACP executable path does not exist: {executable}")
        elif shutil.which(executable) is None:
            raise ValueError(f"Hermes ACP executable not found on PATH: {executable}")
        default_path = Path(self.cwd_default)
        if not default_path.is_absolute():
            raise ValueError(f"Hermes ACP cwd_default must be absolute, got {self.cwd_default!r}")
        if not default_path.exists():
            raise ValueError(f"Hermes ACP cwd_default does not exist: {self.cwd_default}")
        for workspace_id, path_value in self.workspace_map.items():
            path = Path(path_value)
            if not path.is_absolute():
                raise ValueError(f"Hermes workspace path must be absolute for {workspace_id}: {path_value!r}")
            if not path.exists():
                raise ValueError(f"Hermes workspace path does not exist for {workspace_id}: {path_value}")
        return {
            "adapter_kind": "hermes_acp",
            "command": " ".join(self.command),
            "cwd_default": self.cwd_default,
            "workspace_map_entries": len(self.workspace_map),
        }

    def _cwd_for(self, packet: WakePacket) -> str:
        return self.workspace_map.get(packet.workspace_id, self.cwd_default)

    def _ensure_client(self, cwd: str) -> ACPProcessClient:
        if self._client is None or self._client.cwd != cwd:
            if self._client is not None:
                self._client.close()
            self._client = ACPProcessClient(
                command=self.command,
                cwd=cwd,
                env=self.env,
                auto_select_permission=self.auto_select_permission,
                permission_option_id=self.permission_option_id,
            )
        self._client.ensure_started()
        return self._client

    def dispatch(self, packet: WakePacket, prompt_text: str, session_key: str, existing_native_session_id: str | None = None) -> AdapterResult:
        cwd = self._cwd_for(packet)
        if not Path(cwd).is_absolute():
            raise ValueError(f"Hermes ACP cwd must be absolute, got {cwd!r}")
        client = self._ensure_client(cwd)
        native_session_id = client.get_or_create_session(existing_native_session_id, cwd)
        response_text = client.prompt(native_session_id, prompt_text)
        if not response_text:
            LOGGER.warning("Hermes ACP returned an empty response for session %s", native_session_id)
        return AdapterResult(response_text=response_text, native_session_id=native_session_id, metadata={"cwd": cwd})
