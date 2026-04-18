from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from ..models import WakePacket


@dataclass(slots=True)
class AdapterResult:
    response_text: str
    native_session_id: str | None = None
    metadata: dict | None = None


class Adapter(Protocol):
    def doctor(self) -> dict:
        ...

    def dispatch(self, packet: WakePacket, prompt_text: str, session_key: str, existing_native_session_id: str | None = None) -> AdapterResult:
        ...
