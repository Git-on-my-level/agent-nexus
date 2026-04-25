from __future__ import annotations

import json

from .models import WakePacket


def build_wake_prompt(packet: WakePacket) -> str:
    payload = json.dumps(packet.to_content(), indent=2, sort_keys=True)
    return (
        "You were tagged in an Agent Nexus topic or card.\n\n"
        "Act on the tagged message in the context of the workspace and subject. "
        "Reply with the exact message that should be posted back into the same backing thread. "
        "Stay grounded in the wake packet. If more context is needed, say exactly what to fetch.\n\n"
        "<wake_packet>\n"
        f"{payload}\n"
        "</wake_packet>\n"
    )
