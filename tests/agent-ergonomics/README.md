# Agent ergonomics eval

Measures what the `anx` CLI costs an agent that is trying to get ordinary work done: how many commands it needs, how many fail, how much output it has to read, and whether the work ends up right. Run it before and after a CLI change to see whether the change helped.

## What it does

For each task the harness:

1. Builds the CLI and core from this checkout and starts core on a free loopback port with an empty workspace under `.tmp/agent-ergonomics/`.
2. Seeds the same small workspace every time: two initiatives, a few cards, one open ask from a teammate and one answered ask of the agent's own (`harness/seed.py`).
3. Gives a model the task in plain words and one tool, `anx`. The model has no shell and no other access.
4. Reads workspace state afterwards to decide whether the task was done (`harness/tasks.py`).
5. Stops core.

Each task also carries an oracle, the shortest command sequence that solves it. Oracle mode replays those sequences with no model, which proves the fixture and the checks still work.

## Run it

Oracle mode needs Go and Python 3 only:

```sh
make agent-ergonomics-check
```

Model mode needs any OpenAI-compatible chat completions endpoint that supports tool calls:

```sh
export ANX_EVAL_API_BASE=https://<host>/v1
export ANX_EVAL_MODEL=<model id>
export ANX_EVAL_API_KEY_FILE=<path to a file holding the key>   # or ANX_EVAL_API_KEY
python3 tests/agent-ergonomics/run.py --mode model --repeats 3
```

Useful flags:

| Flag                       | Effect                                                                                                     |
| -------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `--task <id>`              | Run one task; repeatable.                                                                                  |
| `--repeats <n>`            | Runs per task. Models vary between runs, so compare medians over at least three.                           |
| `--skill`                  | Put the bundled participant skill in the agent's prompt. Without it the agent starts cold with `anx help`. |
| `--compare <results.json>` | Show an earlier run beside this one.                                                                       |
| `--min-pass <n>`           | Exit 1 when fewer runs pass.                                                                               |
| `--max-turns <n>`          | Model turns allowed per task (default 25).                                                                 |

`ANX_EVAL_EXTRA_BODY` takes a JSON object merged into every model request, for provider settings such as a reasoning level.

## Dogfood with a full agent

`run.py` gives a model one tool. To let a coding agent with a shell use the CLI the way it would in real work, start the same seeded workspace and leave it running:

```sh
python3 tests/agent-ergonomics/sandbox.py --dir /tmp/anx-sandbox
```

The directory gets an `anx` wrapper pinned to the sandbox workspace and the `eval` identity, a `human` wrapper that calls the workspace as the person who runs it, and the two bundled skills. Point any agent at that directory, give it tasks in plain words, and ask what got in its way. `touch /tmp/anx-sandbox/STOP` stops core. Nothing outside the directory is reachable through the wrappers.

## Reading the report

Each run writes `summary.md`, `results.json` and one `transcript.jsonl` per episode under `.tmp/agent-ergonomics/<timestamp>/`.

- **Tasks passed** is the outcome. A cheap model is the point: a task only a strong model can finish is a CLI problem.
- **Commands per oracle command** is the effort. 1.0 means the agent took the shortest path.
- **Failed commands** and the **friction** table are what to fix. Each row is a command agents tried that the CLI refused, with the error they saw.
- **CLI output read** is the context the CLI spent. It counts every character returned to the agent.

Results from different models, or with and without `--skill`, are not comparable with each other.

## Limits

- The agent cannot run commands that change the machine (`update`, `bridge`, `install`, `host` and similar) or override the workspace and identity flags; it is told so if it tries.
- The human side is simulated through the dev sign-in route, so the eval covers the agent's half of asks and answers and not the web UI.
- Checks read state through the CLI and the HTTP API. A change that breaks a read the checks use shows up as an oracle failure.

## Adding a task

Add a `Task` to `harness/tasks.py` with a prompt that names things the way a person would, an oracle, and a check that reads state. Run `make agent-ergonomics-check`; it must pass before the task is used with a model.
