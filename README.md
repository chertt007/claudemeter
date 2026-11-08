# ClaudeMeter

A lightweight terminal dashboard for **Claude Code subscription limits**, for several accounts at once.

- Session (5h) and weekly limits per account, with reset times. These are the same numbers Claude Code's `/usage` shows.
- Near real-time: it watches each profile's session logs, and a couple of seconds after Claude answers the bars update.
- Local token tally for the current 5h window, with a sparkline of activity.
- A rifle next to each card: its magazine holds what is left of the 5h limit (30 rounds = untouched), it fires while Claude is working and smokes afterwards. Each account gets its own colour.
- Add and remove subscriptions from the UI. Existing `~/.claude*` profiles are discovered on first run.
- Light on resources. A single native `.exe` uses about 25 MB of RAM. No polling loops: Windows notifies it about file changes, and animation frames run only while something moves.

## Install

**Requirements:** [Claude Code](https://docs.claude.com/en/docs/claude-code) installed and logged in at least once. Windows 10/11 or Linux. macOS is not supported yet, because Claude Code keeps the token in the Keychain there.

### Download (no Go needed)

1. Download the binary from the [latest release](https://github.com/chertt007/claudemeter/releases/latest):
   - Windows: [`claudemeter_windows_amd64.exe`](https://github.com/chertt007/claudemeter/releases/latest/download/claudemeter_windows_amd64.exe) (`_arm64` for ARM laptops)
   - Linux: `claudemeter_linux_amd64` / `claudemeter_linux_arm64`
2. Rename it to `claudemeter.exe`, put it in any folder on your `PATH`, and run `claudemeter` in a terminal. Windows Terminal looks best.

Or in PowerShell:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\ClaudeMeter"
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest https://github.com/chertt007/claudemeter/releases/latest/download/claudemeter_windows_amd64.exe -OutFile "$dir\claudemeter.exe"
& "$dir\claudemeter.exe"
```

The binary is not code-signed, so the first time Windows SmartScreen may say "Windows protected your PC". Click **More info**, then **Run anyway**. Checksums are in `SHA256SUMS.txt` next to each release.

### With Go

```powershell
go install github.com/chertt007/claudemeter@latest
# or build locally
go build -o claudemeter.exe .
```

## Usage

```powershell
claudemeter          # interactive dashboard
claudemeter --once   # print one snapshot and exit
```

| Key | Action |
| --- | --- |
| `↑` `↓` | select account |
| `a` | add subscription |
| `d` | remove selected (the profile itself is not touched) |
| `r` / `R` | refresh selected / all |
| `g` | show / hide the rifles |
| `q` | quit |

### Multiple accounts

Each Claude Code account is a separate profile folder selected with `CLAUDE_CONFIG_DIR`:

```powershell
$env:CLAUDE_CONFIG_DIR = "$HOME\.claude-work"; claude   # then /login
```

Add that folder in ClaudeMeter with `a`. Which project you work in doesn't matter: all projects of a profile are tracked.

## How it works

- **Limits**: `GET https://api.anthropic.com/api/oauth/usage`, called with the OAuth token from `<profile>/.credentials.json`. This endpoint is undocumented and may change. The request does not use any of your quota.
- **When it refreshes**: after Claude Code writes to a transcript, it waits 3 seconds and then refreshes, at most every 30 seconds per account. Otherwise it refreshes every 3 minutes.
- **Tokens**: read incrementally from `<profile>/projects/**/*.jsonl`, with duplicates removed by request id.
- **Credentials are read-only**. ClaudeMeter never refreshes tokens, because that would rotate the refresh token out from under Claude Code. If a token has expired, open `claude` in that profile and the card recovers by itself.

## Config

Stored at `%APPDATA%\ClaudeMeter\config.json`:

```json
{
  "accounts": [
    { "name": "work", "dir": "C:\Users\me\.claude" },
    { "name": "personal", "dir": "C:\Users\me\.claude-personal" }
  ],
  "poll_seconds": 180,
  "min_refresh_seconds": 30,
  "animations": true,
  "gun": true
}
```

## Roadmap

- [x] Terminal dashboard
- [ ] Tray icon with the current session %
