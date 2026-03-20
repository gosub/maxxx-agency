# Nudgent — Implementation Plan

Intelligent nudge agent. Runs as a single Go binary, communicates via Telegram, powered by OpenRouter models.

The agent tracks tasks (one-off and recurrent), knows the user's schedule, wakes up every N minutes, and decides when and how to nudge. All task management happens through natural language chat; the LLM interprets intent and returns structured actions the bot executes.

---

## Project Structure

```
nudgent/
├── main.go              # Entry point, env + TOML loading, wiring
├── bot/
│   ├── bot.go           # Telegram polling, message routing
│   ├── handlers.go      # Command and chat handlers
│   ├── nudge.go         # Nudge engine (scheduled goroutine)
│   └── bot_test.go
├── agent/
│   ├── agent.go         # OpenRouter HTTP client
│   ├── prompt.go        # System prompt builder
│   ├── actions.go       # Action types and JSON parsing
│   └── agent_test.go
├── store/
│   ├── store.go         # SQLite: tasks, prefs
│   └── store_test.go
├── lang/
│   └── strings.go       # Bilingual string tables (en, it)
├── log/
│   └── log.go           # zerolog wrapper
├── config.toml
├── .env
├── .env.example
├── .gitignore
├── PLAN.md
├── Makefile
├── go.mod
└── go.sum
```

Note: `coach/` renamed to `agent/`.

---

## Secrets & Configuration

### `.env` (gitignored)

```
TELEGRAM_TOKEN=
OPENROUTER_KEY=
ALLOWED_USER_ID=
```

### `config.toml` (committed)

```toml
telegram_token_env  = "TELEGRAM_TOKEN"
openrouter_key_env  = "OPENROUTER_KEY"
allowed_user_id_env = "ALLOWED_USER_ID"
timezone            = "Europe/Rome"
model               = "openai/gpt-4o-mini"
language            = "en"
nudge_interval_m    = 30
```

---

## Database Schema (SQLite)

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    description   TEXT    NOT NULL,   -- freeform, as understood by the LLM
    next_nudge_at TEXT,               -- ISO 8601; set by LLM, used by scheduler
    done          INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS prefs (
    user_id          INTEGER PRIMARY KEY,
    language         TEXT    NOT NULL DEFAULT 'en',
    nudge_interval_m INTEGER NOT NULL DEFAULT 30,
    schedule         TEXT    NOT NULL DEFAULT '',  -- freeform, e.g. "weekdays 9-13 and 15-19"
    last_wakeup_at   TEXT                          -- last successful scheduler tick (ISO 8601)
);
```

### Design rationale

- Task descriptions are freeform text. Recurrence, priority, context, and any other metadata live inside the description as natural language. The LLM re-interprets them on each interaction.
- `next_nudge_at` is the only structured field on tasks. It lets the scheduler do a simple DB query without an LLM call on every tick. The LLM sets it — accounting for the user's schedule — whenever it processes a task.
- Schedule is a single freeform string in `prefs`, injected into every LLM call as global context. It is not parsed by application code.
- `last_wakeup_at` records the last successful scheduler tick. On restart, the scheduler resumes from there, catching any nudges that should have fired during downtime (their `next_nudge_at` will be in the past and will be picked up naturally).

---

## LLM Action Protocol

Every user chat message is sent to the LLM with the system prompt below. The LLM always responds with a JSON envelope:

```json
{
  "reply": "Got it. Added 'Call dentist' for tomorrow at 15:00.",
  "actions": [
    { "type": "add_task",        "description": "Call dentist — tomorrow 15:00, working hours" },
    { "type": "set_next_nudge",  "id": 12, "next_nudge_at": "2026-03-21T15:00:00" },
    { "type": "update_task",     "id": 5,  "description": "Gym — Monday and Thursday mornings" },
    { "type": "complete_task",   "id": 3 },
    { "type": "delete_task",     "id": 7 },
    { "type": "update_schedule", "schedule": "weekdays 9-13 and 15-19" }
  ]
}
```

- `reply` is always present and shown to the user verbatim.
- `actions` may be empty.
- The bot executes actions against the store, then sends `reply`.
- If JSON parsing fails, the raw LLM text is sent with no actions executed.

### Chat System Prompt

```
You are Nudgent, an intelligent task and nudge assistant.
You help the user track tasks, remember commitments, and get things done.
Be concise and direct. Always respond in {language}.

Current time: {ISO datetime with timezone}

User's schedule:
{prefs.schedule — or "not set" if empty}

Active tasks:
{id}. {description} — next nudge: {next_nudge_at or "not set"}
...
(done tasks omitted)

Respond ONLY with a JSON object: {"reply": "...", "actions": [...]}
No text outside the JSON. If no actions are needed, use "actions": [].

When setting next_nudge_at, use ISO 8601 and respect the user's schedule.
```

---

## Nudge Engine

A goroutine wakes every `nudge_interval_m` minutes:

1. Load `last_wakeup_at` from prefs.
2. Query tasks where `done = 0 AND next_nudge_at <= now`.
3. If none, update `last_wakeup_at` and sleep.
4. Send the nudge prompt to the LLM with the matching tasks.
5. If `reply` is non-empty, send it to the user.
6. Execute any actions returned (e.g. `set_next_nudge` to advance a recurrent task).
7. Update `last_wakeup_at = now`.

### Nudge System Prompt

```
You are Nudgent, a nudge agent.
Current time: {datetime}. User's schedule: {schedule}.

The following tasks are due for a nudge:
{id}. {description}
...

Send the user a short nudge message. One task, one sentence, no fluff.
If multiple tasks are due, pick the most urgent one.

Respond: {"reply": "...", "actions": [...]}
Use actions to update next_nudge_at for recurrent tasks after nudging.
```

---

## Bot Commands

Slash commands are minimal. All real interaction is natural language chat.

| Command | Description |
|---------|-------------|
| `/tasks` | List active tasks with next nudge times |
| `/help`  | Show usage |

---

## Interaction Examples

```
User: add call dentist before end of March, working hours
Bot:  Added: "Call dentist — before end of March, working hours".

User: done with the gym
Bot:  Marked "Gym" as done.

User: push the report to Thursday morning
Bot:  Rescheduled "Write report" to Thursday 09:00.

User: my schedule is weekdays 9 to 1 and 3 to 7
Bot:  Got it. I'll only nudge you during those hours.

[nudge, unprompted]
Bot:  "Call dentist" is overdue — end of March is tomorrow.
```

---

## Build Order

1. **store:** new `store/store.go` — tasks + prefs CRUD, drop all agency tables
2. **agent:** rename `coach/` → `agent/`, update import paths; rewrite `prompt.go` and add `actions.go`
3. **bot/handlers:** strip agency commands, add `/tasks`, route all chat through agent
4. **bot/nudge:** replace `dailyScheduler` with new `nudge.go`
5. **main.go:** update `Config` struct, wire new store + agent
6. **cleanup:** remove `AGENCY-COMPENDIUM.md`, dead lang strings, old handler tests
7. **tests:** update mock store interface, rewrite handler + action parsing tests

---

## Testing Checklist

- [ ] Bot starts, no panics, logs startup config
- [ ] `/tasks` returns empty list on first run
- [ ] "add buy milk tomorrow 9am" → task appears in `/tasks`
- [ ] "done with milk" → task removed
- [ ] "push milk to Friday" → next_nudge_at updated
- [ ] Recurrent task: after nudge, next_nudge_at advances to next occurrence
- [ ] Nudge fires when next_nudge_at <= now
- [ ] No nudge when task list is empty
- [ ] Bot restarts with pending nudge in the past → nudge fires on next tick
- [ ] Schedule set via chat → injected into subsequent LLM calls
- [ ] Messages from non-allowed user IDs are silently ignored
- [ ] Kill + restart — tasks and prefs persist
