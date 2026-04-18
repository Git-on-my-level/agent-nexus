from anx_agent_bridge.models import WakePacket


def test_roundtrip_packet_content():
    packet = WakePacket(
        wakeup_id="wake_1",
        handle="hermes",
        actor_id="actor_1",
        workspace_id="ws_main",
        workspace_name="Main",
        thread_id="thread_1",
        thread_title="Example",
        trigger_event_id="event_1",
        trigger_created_at="2026-03-23T00:00:00Z",
        trigger_author_actor_id="actor_user",
        trigger_text="@hermes please help",
        current_summary="summary",
        session_key="oar:ws_main:thread_1:hermes",
        anx_base_url="http://localhost:8080",
        thread_context_url="http://localhost:8080/threads/thread_1/context",
        thread_workspace_url="http://localhost:8080/threads/thread_1/workspace",
        trigger_event_url="http://localhost:8080/events/event_1",
        cli_thread_inspect="anx threads inspect --thread-id thread_1 --json",
        cli_thread_workspace="anx threads workspace --thread-id thread_1 --json",
        topic_workspace_url="http://localhost:8080/topics/topic_1/workspace",
        cli_topic_workspace="anx topics workspace --topic-id topic_1 --json",
        subject_ref="topic:topic_1",
        resolved_subject={
            "ref": "topic:topic_1",
            "kind": "topic",
            "title": "Example topic",
            "thread_id": "thread_1",
        },
    )
    restored = WakePacket.from_content(packet.to_content())
    assert restored.handle == packet.handle
    assert restored.trigger_text == packet.trigger_text
    assert restored.session_key == packet.session_key
    assert restored.subject_ref == packet.subject_ref
    assert restored.resolved_subject == packet.resolved_subject
    assert restored.topic_workspace_url == packet.topic_workspace_url
    assert restored.cli_topic_workspace == packet.cli_topic_workspace
    assert restored.to_content()["context_fetch"]["preferred"] == "topics.workspace"
