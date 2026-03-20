# Maxxx Agency — Implementation Plan

Personal agency coach bot. Runs as a single Go binary, communicates via Telegram, powered by OpenRouter free-tier models.

The bot's knowledge base is derived from the [Agency Compendium](./AGENCY-COMPENDIUM.md) — a comprehensive synthesis of ideas on building personal agency. The compendium is injected into the system prompt and referenced by the coach during conversations.

---

## Project Structure

```
nudgent/
├── main.go              # Entry point, env + TOML loading, wiring
├── bot/
│   └── bot.go           # Telegram polling, commands, scheduler
├── coach/
│   ├── prompts.go       # System prompt builder (language + tone + plan)
│   └── coach.go         # OpenRouter API calls
├── store/
│   └── store.go         # SQLite: user state, conversation history
├── lang/
│   └── strings.go       # Bilingual string tables (en, it)
├── config.toml          # Config (committed)
├── .env                 # Secrets (gitignored)
├── .env.example         # Example env file (committed)
├── .gitignore           # .env, binary, agency.db
├── PLAN.md              # This file
├── AGENCY-COMPENDIUM.md # Agency knowledge base (injected into system prompt)
├── go.mod
└── go.sum
```

## Secrets & Configuration

### `.env` (gitignored)

```
TELEGRAM_TOKEN=
OPENROUTER_KEY=
ALLOWED_USER_ID=
```

### `.env.example` (committed)

```
TELEGRAM_TOKEN=
OPENROUTER_KEY=
ALLOWED_USER_ID=
```

### `config.toml` (committed)

```toml
telegram_token_env = "TELEGRAM_TOKEN"
openrouter_key_env = "OPENROUTER_KEY"
allowed_user_id_env = "ALLOWED_USER_ID"
daily_checkin_hour = 9
timezone = "Europe/Rome"
model = "google/gemma-3-1b-it:free"
language = "it"
tone = "warm"
bot_name = "Maxxx Agency"
```

Config references env var **names** (not values). At startup, `os.Getenv()` resolves them. Fail fast if missing.

### `.gitignore`

```
.env
nudgent
agency.db
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/BurntSushi/toml` | TOML config parsing |
| `github.com/joho/godotenv` | .env file loading |
| `github.com/go-telegram-bot-api/telegram-bot-api/v5` | Telegram bot API |
| `modernc.org/sqlite` | Pure Go SQLite (no CGO) |

## Database Schema (SQLite)

```sql
CREATE TABLE IF NOT EXISTS state (
    user_id              INTEGER PRIMARY KEY,
    current_phase        INTEGER DEFAULT 0,
    current_goals        TEXT DEFAULT '[]',
    rejection_log        TEXT DEFAULT '[]',
    last_checkin         TEXT,
    conversation_history TEXT DEFAULT '[]',
    config_notes         TEXT DEFAULT '',
    language             TEXT DEFAULT 'it',
    tone                 TEXT DEFAULT 'warm'
);
```

Single table, one row per user.

## Agent Identity

- **Name:** Maxxx Agency
- **Welcome (it):** `"Ciao, sono Maxxx Agency. Il tuo coach personale per costruire agency. Dove vuoi iniziare?"`
- **System prompt preamble:** *"You are Maxxx Agency, a personal agency coach. You are concise, direct, and push when needed. Never preachy."*

## Language

- `language` in config: `"it"` or `"en"`
- All bot-side strings in `lang/strings.go` keyed by language code
- System prompt adds: *"Always respond in {language}."*
- `/lang it` or `/lang en` to switch at runtime (persists to SQLite)

### String Table Example (`lang/strings.go`)

```go
var strings = map[string]map[string]string{
    "en": {
        "welcome":           "Welcome back. Where are you in the plan?",
        "status_header":     "Your current status:",
        "rejection_logged":  "Logged! Total: %d. Keep going.",
        "tone_current":      "Current tone: %s",
        "lang_current":      "Current language: %s",
    },
    "it": {
        "welcome":           "Bentornato. Dove sei nel piano?",
        "status_header":     "Il tuo stato attuale:",
        "rejection_logged":  "Registrato! Totale: %d. Continua così.",
        "tone_current":      "Tono attuale: %s",
        "lang_current":      "Lingua attuale: %s",
    },
}
```

## Tone

`tone` in config: `"warm"`, `"direct"`, `"drill-sergeant"`

Injected into system prompt as behavioral modifier:

| Tone | System prompt addition |
|------|----------------------|
| `warm` | *"Be warm and encouraging. Celebrate small wins. Gentle nudges."* |
| `direct` | *"Be direct and efficient. Short responses. No fluff. Get to the point."* |
| `drill-sergeant` | *"Be intense and demanding. Push hard. No excuses. Tough love."* |

`/tone` (no args) — show current tone and available options.
`/tone warm|direct|drill-sergeant` — switch tone, persists to SQLite.

## Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message in current language |
| `/status` | Phase, goals, rejections, tone, language |
| `/rejection` | Quick-log a rejection |
| `/goal` | Add / view / complete a goal |
| `/skip` | Skip today's check-in |
| `/lang` | Show current language |
| `/lang it` | Switch to Italian |
| `/lang en` | Switch to English |
| `/tone` | Show current tone |
| `/tone warm` | Set tone to warm |
| `/tone direct` | Set tone to direct |
| `/tone drill-sergeant` | Set tone to drill-sergeant |
| `/reset` | Clear conversation context |
| `/help` | List commands in current language |

## Interaction Modes

### 1. Daily Check-in (scheduled)

- Goroutine with 1-minute ticker, timezone-aware
- Sends at configured hour (e.g. 9 AM)
- Skips if already sent today (`last_checkin` check)
- Short, casual, phase-appropriate. Examples:
  - "Day 5 of the rejection game. Got any for me today?"
  - "You've been on Phase 2 for 2 weeks. Ready for Phase 3?"
- No follow-up if user doesn't respond — not nosy

### 2. On-demand (user messages bot)

- Full conversation mode with recent context
- AI sees: current phase, active goals, rejection count, tone, language
- Can review progress, suggest actions, log rejections, adjust goals, work through blockers
- Conversation history capped at last 20 messages (fits free model context window)

## System Prompt Structure

The AI receives:

1. **Identity:** "You are Maxxx Agency..."
2. **Language instruction:** "Always respond in Italian."
3. **Tone instruction:** "Be warm and encouraging..."
4. **The agency framework** — the full [Agency Compendium](./AGENCY-COMPENDIUM.md) is read at startup and included in the system prompt as reference material for the coach
5. **Current state:**
   - Phase: 2 (Mindset Shifts)
   - Active goals: ["Read Dune", "Start rejection log"]
   - Rejections logged: 3
   - Last check-in: 2026-03-17
6. **Behavioral rules:**
   - Ask one good question at a time
   - Don't be nosy — brief check-ins, go deeper only if user engages
   - Push gently when detecting avoidance or procrastination
   - Celebrate rejections and small wins
   - Suggest next phase when current tasks are done

## Agency Framework (Embedded Reference)

### Phase 0: Substrate (Weeks 1-2)
- Fix sleep, diet, exercise
- 5-min daily meditation
- Read one inspiring book

### Phase 1: Mindset Shifts (Weeks 2-4)
- Detect imaginary rules
- Identify petty tyrants
- Expand option spaces

### Phase 2: Action Habits (Weeks 4-8)
- Ask for one thing per week
- Rejection logging game
- "Do it 100 times" challenge
- Create serendipity vehicles

### Phase 3: Strategic Integration (Ongoing)
- Join/build communities
- Seek forgiveness not permission
- Cross-pollinate ideas
- Quarterly review

### Key Principles
1. Thinking about agency increases it — but don't overthink, act
2. Community is an agency multiplier
3. Rejection is signal, not failure
4. You have more freedom than you think
5. The option space is wider than it appears
6. Sustainability > intensity
7. Cross-pollination of ideas

## Security

- Only `allowed_user_id` gets responses — all other Telegram users ignored silently
- Secrets in `.env`, never committed
- Config only references env var names, never values
- No logging of message contents or API keys

## Deployment

- `go build -o nudgent` — single static binary, no CGO
- Runs as: `./nudgent` (reads `config.toml` + `.env` from working dir)
- Creates `agency.db` on first run
- Can run with `systemd`, `screen`, `nohup`
- Graceful shutdown on SIGINT/SIGTERM (flushes conversation history to SQLite)

## Build Order

1. **Project init:** `go mod init`, create all files, `.env`, `.env.example`, `config.toml`, `.gitignore`
2. **`main.go`:** load `.env` via godotenv, parse TOML, resolve env vars, validate, print startup
3. **`store/store.go`:** SQLite init + CRUD for user state
4. **`lang/strings.go`:** bilingual string tables (en + it)
5. **`coach/prompts.go`:** system prompt builder with language/tone/phase/goals injection
6. **`coach/coach.go`:** OpenRouter HTTP POST, retry logic, response parsing
7. **`bot/bot.go`:** Telegram polling, message routing, all commands, daily scheduler goroutine
8. **Wiring in `main.go`:** connect store, coach, bot; graceful shutdown
9. **Test:** run locally, message on Telegram, verify responses
10. **Polish:** error handling, logging, edge cases

## Testing Checklist

- [ ] Bot starts, prints welcome, no panics
- [ ] `/start` works, prints in Italian
- [ ] Send a message, get a coherent AI response in Italian
- [ ] `/rejection` logs and confirms
- [ ] `/goal add "Read Dune"` adds a goal
- [ ] `/status` shows correct state
- [ ] `/tone direct` switches tone, `/status` reflects it
- [ ] `/lang en` switches to English, responses now in English
- [ ] Daily check-in fires at configured hour
- [ ] `/skip` suppresses next check-in
- [ ] Messages from non-allowed user IDs are ignored
- [ ] Kill process (Ctrl+C), restart — state is preserved
