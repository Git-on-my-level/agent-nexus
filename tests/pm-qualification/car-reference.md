# CAR v3 reusable failure scenarios

Reference revision: `ec2c44a5bb028f02817e8e2cfbd62e845e6006a4`, read from the
GitHub `car-v3` branch on 2026-09-08. These references are design/test inputs,
not copied implementation or evidence that Nexus passes the same scenarios.

| Pinned source | Reusable checks |
| --- | --- |
| [Interaction inbox tests](https://github.com/Git-on-my-level/codex-autorunner/blob/ec2c44a5bb028f02817e8e2cfbd62e845e6006a4/tests/core/test_interaction_inbox.py) | Unauthorized actor; expired prompt; changed answer after answered state; prompt/response persistence after reopening |
| [Delivery recovery tests](https://github.com/Git-on-my-level/codex-autorunner/blob/ec2c44a5bb028f02817e8e2cfbd62e845e6006a4/tests/core/orchestration/test_managed_thread_delivery_recovery.py) | Ordered pending replay; future retry not due; expired claim vs active claim; bounded retry exhaustion; duplicate intent returns existing; terminal replay no-op; idempotency distinguishes surfaces |
| [Delivery lifecycle tests](https://github.com/Git-on-my-level/codex-autorunner/blob/ec2c44a5bb028f02817e8e2cfbd62e845e6006a4/tests/core/pma_domain/test_delivery_lifecycle.py) | Retry backoff; suppressed duplicates; inspectable delivery retry state; changed channel binding |
| [Discord single-owner tests](https://github.com/Git-on-my-level/codex-autorunner/blob/ec2c44a5bb028f02817e8e2cfbd62e845e6006a4/tests/adapters/discord/test_single_owner_invariants.py) | Rejected duplicate never enters scheduler; unbound channel does not route orchestration; missing durable acknowledgement is not delivered |
| [Transport restart duplicate scenario](https://github.com/Git-on-my-level/codex-autorunner/blob/ec2c44a5bb028f02817e8e2cfbd62e845e6006a4/tests/chat_surface_lab/scenarios/restart_window_duplicate_delivery.json) | Repeat exact Telegram update / Discord interaction after transport restart; durable duplicate rejection on each surface |

The product-specific extension for Nexus is to test the *work/action meaning*,
not only message deduplication: a duplicate transport event must not create a
second commitment, approval or downstream mutation. An uncertain network result
cannot safely be treated as an ordinary retryable failure for every provider.
