from __future__ import annotations

import json

from .models import WakePacket


def build_wake_prompt(packet: WakePacket) -> str:
    payload = json.dumps(packet.to_content(), indent=2, sort_keys=True)
    return (
        "You were tagged in an Agent Nexus topic or card.\n\n"
        "Act on the tagged message in the context of the workspace and subject. "
        "Post any user-facing response directly back into the same backing thread with the Agent Nexus CLI when you can. "
        "If you cannot post directly, return the exact fallback message that should be posted for you. "
        "Do not include scratchpad, progress narration, or private reasoning in user-facing responses. "
        "Stay grounded in the wake packet. If more context is needed, say exactly what to fetch.\n\n"
        "<wake_packet>\n"
        f"{payload}\n"
        "</wake_packet>\n"
    )
