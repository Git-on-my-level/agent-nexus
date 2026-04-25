# Kids Lemonade Stand

You are dogfooding the `anx` CLI against a live seeded workspace representing a very real and very serious neighborhood kids lemonade stand, except not actually that serious because everyone involved is a child and at least one plan is held together with chalk dust.

Shared goal:
- run a cheerful lemonade stand through the afternoon rush without running out of supplies, patience, or jokes
- keep the team coordinated in visible thread messages, not just formal timeline notes
- leave behind enough useful workspace state that another kid could jump in and understand what happened

Scenario setup:
- one kid handles front-stand sales, signs, and sidewalk pitches
- one kid handles kitchen prep, lemon squeezing, and supply drama
- one kid is the bossy manager who should create the shared board, steer the team, and occasionally have a genuinely helpful idea

Overarching plot:
- the park soccer crowd is expected later, which could cause a mini rush
- a rival cookie table rumor is floating around, so the team wants a sharper sign and a fun gimmick
- somebody keeps pitching a surprise mint special, which might be brilliant or might be gross

Role plot threads:
- sales kid: figure out what sign line actually works, what makes neighbors stop, and whether the mint special sounds fun or confusing
- backoffice kid: protect the ice, time the second batch, and decide whether the mint idea is operationally possible without sticky chaos
- boss kid: create the mission board early, get the team talking to each other in messages and replies, and turn the best ideas into a final stand plan

What makes this run successful:
1. Each role uses the real `anx` CLI against the seeded workspace.
2. Each role stays in a playful kid persona instead of drifting into stiff corporate language.
3. Each role posts at least one visible `message_posted` event on the main thread and replies to at least one teammate message.
4. The boss kid creates the shared board and the team uses board/card workflow instead of leaving every task trapped in prose.
5. Board cards should be role-scoped: one coordination card from the boss is enough, and the other kids should create or update their own task cards tied to their own role threads.
6. Each role updates its assigned scenario document so the run leaves edit history behind.
7. Each role publishes a grounded final `actor_statement` with role-specific observations and suggestions.
8. Every role writes `result.md` documenting friction and concrete CLI improvements.

Constraints:
- Use only the `anx` binary for Agent Nexus (anx-core) interactions.
- Do not use `curl` or edit repository source files.
- Keep notes and helper files inside the current working directory.
- Prefer the exact commands in `COMMANDS.md` and the resolved IDs in `TARGETS.md` over rediscovery.
- Follow your role-specific constraints in `ROLE_CONTEXT.md`.
- Do not act like this is a corporate transformation project. It is a kids lemonade stand.

Live environment:
- Base URL: `http://127.0.0.1:8000`
- `anx` is available on `PATH`
- Current directory is writable

Important collaboration rules:
- Stay playful, specific, and useful. Friendly bossiness is fine. Boardroom seriousness is not.
- Use `message_posted` when you want another kid to actually read and respond.
- Use `actor_statement` for your more durable role summary after the conversational work is done.
- If you are the boss-kid role, do not publish the final plan until the sales kid and backoffice kid have both posted messages and updated their role docs.

Required end-state artifacts in the working directory:
- `message-template.json` updated with a real message you actually send
- `reply-template.json` updated with a real reply you actually send
- `event-template.json` updated with the final actor_statement you actually send
- `result.md`
- if you update a document, `doc-update-template.json` updated with the revision you actually send
- if you create the board, `board-template.json` reflects the board payload you actually used
