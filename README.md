# Maxxx Agency

Personal agency coach bot. Runs as a single Go binary, communicates via Telegram, powered by OpenRouter free-tier models.

## Features

- Daily check-in messages at a configured hour
- On-demand coaching conversations with AI
- Rejection logging game
- Goal tracking (add, list, complete)
- Bilingual support (English and Italian)
- Configurable tone (warm, direct, drill-sergeant)
- Conversation context preserved across sessions
- Single binary, no CGO, SQLite for state

## Quick Start

1. Clone the repo

```
git clone https://github.com/gosub/nudgent.git
cd nudgent
```

2. Copy the example env file and fill in your secrets

```
cp .env.example .env
```

Edit `.env` with your values:

```
TELEGRAM_TOKEN=your-telegram-bot-token
OPENROUTER_KEY=your-openrouter-api-key
ALLOWED_USER_ID=your-telegram-numeric-id
```

3. Adjust `config.toml` to your preferences

```toml
daily_checkin_hour = 9
timezone = "Europe/Rome"
model = "google/gemma-3-1b-it:free"
language = "it"
tone = "warm"
bot_name = "Maxxx Agency"
```

4. Build and run

```
make build
./nudgent
```

## Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message |
| `/status` | Show current phase, goals, rejections |
| `/rejection` | Log a rejection |
| `/goal add <text>` | Add a goal |
| `/goal list` | List goals |
| `/goal done <n>` | Complete a goal |
| `/skip` | Skip today's check-in |
| `/lang it` or `/lang en` | Switch language |
| `/tone warm\|direct\|drill-sergeant` | Switch tone |
| `/reset` | Clear conversation context |
| `/help` | List all commands |

## Project Structure

```
nudgent/
├── main.go              # Entry point, config loading, wiring
├── bot/
│   └── bot.go           # Telegram polling, commands, scheduler
├── coach/
│   ├── prompts.go       # System prompt builder
│   └── coach.go         # OpenRouter API calls
├── store/
│   └── store.go         # SQLite state management
├── lang/
│   └── strings.go       # Bilingual string tables (en, it)
├── config.toml          # Configuration
├── .env.example         # Example secrets file
├── AGENCY-COMPENDIUM.md # Agency knowledge base
├── Makefile
└── PLAN.md              # Full implementation plan
```

## Agency Framework

The bot's coaching is based on the Agency Compendium, a comprehensive framework for building personal agency through four phases:

- **Phase 0: Substrate** - Fix sleep, diet, exercise, start meditation
- **Phase 1: Mindset Shifts** - Detect imaginary rules, identify blockers
- **Phase 2: Action Habits** - Rejection logging, weekly asks, 100x challenges
- **Phase 3: Strategic Integration** - Communities, cross-pollination, quarterly review

See `AGENCY-COMPENDIUM.md` for the full reference.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/BurntSushi/toml` | TOML config parsing |
| `github.com/joho/godotenv` | .env file loading |
| `github.com/go-telegram-bot-api/telegram-bot-api/v5` | Telegram bot API |
| `modernc.org/sqlite` | Pure Go SQLite |

## License

This project is licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE) for details.
