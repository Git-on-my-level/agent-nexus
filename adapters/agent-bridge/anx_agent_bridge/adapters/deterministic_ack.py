from __future__ import annotations

from dataclasses import dataclass, field

from .base import AdapterResult
from ..models import WakePacket

# Short, operator-grade acknowledgements for local wake QA (no external model).
_LINES = (
    "Acknowledged.",
    "On it.",
    "Routing now.",
    "Copy that.",
    "Standing by.",
)


@dataclass
class DeterministicAckAdapter:
    """Returns a fixed rotation of replies; ignores prompt text."""

    _next: int = field(default=0, repr=False)

    def doctor(self) -> dict:
        return {"adapter_kind": "deterministic_ack", "phrase_count": len(_LINES)}

    def dispatch(
        self,
        packet: WakePacket,
        prompt_text: str,
        session_key: str,
        existing_native_session_id: str | None = None,
    ) -> AdapterResult:
        line = _LINES[self._next % len(_LINES)]
        self._next += 1
        return AdapterResult(
            response_text=line,
            native_session_id=existing_native_session_id or session_key or "deterministic",
        )
