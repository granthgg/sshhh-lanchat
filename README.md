<div align="center">

# sshhh-lanchat

**Ephemeral, encrypted terminal chat for your LAN.**

No server · no accounts · nothing on disk — close the window and it never happened.

[![CI](https://github.com/granthgg/sshhh-lanchat/actions/workflows/ci.yml/badge.svg)](https://github.com/granthgg/sshhh-lanchat/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/granthgg/sshhh-lanchat?sort=semver)](https://github.com/granthgg/sshhh-lanchat/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/granthgg/sshhh-lanchat)](https://goreportcard.com/report/github.com/granthgg/sshhh-lanchat)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

🌐 **[sshhh-lanchat.tech](https://sshhh-lanchat.tech/)** · [Install](#install) · [Commands & keys](#commands--keys) · [How it works](#how-it-works) · [Security](#security-model)

</div>

Talk to people on the **same Wi-Fi** straight from a terminal. **sshhh-lanchat**
is a single ~3 MB binary — the `lanchat` command — with no runtime
dependencies. Everyone who runs it with the same
**room** and **passphrase** is in the same conversation, like tuning a
walkie-talkie to a channel. Messages are encrypted on the wire and exist only
while your window is open; the UI reads like log output, and one keystroke
swaps it for a fake build if someone walks by.

```
  lanchat 2.4.0 — ephemeral encrypted LAN chat
  ────────────────────────────────────────────────────────────────
  room   "lobby" · OPEN — anyone on this Wi-Fi can read it
  you    "granth" · rename with /nick <name>
  send   type a message and press Enter — the room sees it live
  keys   /help · /quit · Tab = complete · Ctrl-B = instant hide
  saved  nothing — messages exist only while the window is open
  ────────────────────────────────────────────────────────────────
  tip: add -k "secret" to go private — share room + passphrase
  waiting for messages…

14:22:03       granth │ hello, anyone around?
14:22:04        naman │ hey! yes
» ▏
```

## Quick start

```sh
lanchat                        # join the open room "lobby"
lanchat -r team -k coffee123   # private room — only room + passphrase holders can read it
```

Type a message and press Enter; `/quit` (or Ctrl-C) leaves. Add `-color` to
give every person their own stable color — no two people in the room share
one, even with the same name. Prefer not to put the passphrase on the command
line? Use `-ask` (prompts without echoing) or the `CHAT_KEY` environment
variable.

## Install

**One command (recommended)** — fetches the latest release binary for your
machine, **verifies its SHA-256 checksum**, and puts `lanchat` on your PATH.
No Go, no git, no SmartScreen/Gatekeeper popups:

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/granthgg/sshhh-lanchat/main/scripts/get.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/granthgg/sshhh-lanchat/main/scripts/get.ps1 | iex
```

Sharing with friends? Send them the one-liner for their OS — one paste in a
terminal installs it, checksum-verified.

<details>
<summary><b>Manual download</b> — grab a binary from the latest release</summary>

Download the file for your system from the **[latest release »](https://github.com/granthgg/sshhh-lanchat/releases/latest)**:

| Your system | File to download |
|---|---|
| Windows (most PCs) | `lanchat-windows-amd64.exe` |
| Windows (ARM) | `lanchat-windows-arm64.exe` |
| Mac (Apple Silicon · M1–M4) | `lanchat-macos-arm64` |
| Mac (Intel) | `lanchat-macos-amd64` |
| Linux (x86-64) | `lanchat-linux-amd64` |
| Linux (ARM) | `lanchat-linux-arm64` |

**Windows** — rename it to `lanchat.exe` and run it. When SmartScreen warns,
click **More info → Run anyway**.

**macOS / Linux** — in your downloads folder:

```sh
chmod +x lanchat-*                                     # make it runnable
xattr -d com.apple.quarantine lanchat-* 2>/dev/null    # macOS only: clear the "unverified developer" block
mv lanchat-macos-arm64 ~/.local/bin/lanchat            # optional: put it on your PATH (use your file's name)
```

> **Why does the OS warn "unknown publisher"?** The binaries aren't
> code-signed (that requires paid identity verification). Unsigned ≠ unsafe:
> every release is built by GitHub CI from the public, tagged source, and
> `checksums.txt` on the release page lets you verify your download
> byte-for-byte — `Get-FileHash <file>` on Windows, `shasum -a 256 <file>` on
> macOS/Linux. The one-line installer above skips the popup entirely (it only
> screens browser downloads) and checks the checksum for you.
</details>

<details>
<summary><b>Go install / build from source</b> — needs Go 1.25+</summary>

**`go install`** (drops `lanchat` into `$(go env GOPATH)/bin` — make sure it's on your PATH):

```sh
go install github.com/granthgg/sshhh-lanchat/cmd/lanchat@latest
```

**Build from source with the installer** (puts `lanchat` on your PATH for you):

```sh
git clone https://github.com/granthgg/sshhh-lanchat.git
cd sshhh-lanchat && ./scripts/install.sh
# Windows: powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

If the *same* terminal you installed from still says `command not found`, it
cached the old PATH — open a new terminal (or run `hash -r`).
</details>

## Commands & keys

| Command | Action |
|---------|--------|
| *(just type)* | send a message |
| `/who` | list who's here right now |
| `/nick <name>` | change your display name (keeps your color) |
| `/me <action>` | send an action, e.g. `* alice waves` |
| `/snooze [time\|off]` | pause the message bell — `/snooze` = 15 min, `/snooze 1h`, `/snooze off` |
| `/clear` | clear the screen |
| `/boss` | hide behind fake build output |
| `/quit` | leave |
| **Tab** | complete nicknames and commands — press again to cycle matches |
| **Ctrl-B** | **instant boss key** — hide immediately, any key restores |

Lines mentioning **your name** are shown in bold, and every arriving message
rings the **terminal bell** — in a background window that badges the Dock icon
(macOS), marks the tab (iTerm2, tmux), or flashes the taskbar (Windows
Terminal). `/snooze` quiets it for a while; `-no-bell` for the whole session.
The usual editing keys work: arrows (move / history), Home/End, Ctrl-A/E,
Ctrl-U, Ctrl-W, Ctrl-L.

## Flags

| Flag | Meaning | Default |
|------|---------|---------|
| `-r`, `-room <name>` | Channel to join | `lobby` |
| `-n`, `-nick <name>` | Your display name | your username |
| `-k`, `-key <phrase>` | Room passphrase (see note) | none (open room) |
| `-ask` | Prompt for the passphrase without echoing it | off |
| `-iface <name>` | Pin a network interface | auto-detect |
| `-ttl <n>` | Multicast TTL; raise above 1 only on LANs that route multicast between subnets | `1` |
| `-color` | Colorize nicknames | off |
| `-stealth` | Disguise the prompt as a shell `$ ` | off |
| `-prompt <str>` | Custom input prompt | `» ` |
| `-no-broadcast` | Disable the broadcast fallback | off |
| `-no-bell` | Don't ring the terminal bell on new messages | off |
| `-version` | Print version | |

> **Passphrases:** prefer `-ask` or the `CHAT_KEY` environment variable over
> `-k` — anything on the command line is visible in shell history and the
> process list.

## The boss key

Press **Ctrl-B** (or type `/boss`) and the screen is instantly replaced with
plausible build output ending at a shell prompt. While hidden, nothing pops up
and nothing beeps — incoming messages are held in memory (never on disk) and
**replayed the moment you come back** (last 500 lines). Any keystroke restores
the chat. Run with `-stealth` to disguise the normal prompt as a shell too —
chat lines then render as flat, logger-style output and the bell stays off.

## How it works

There is **no host and no server**. Every instance sends and receives on a UDP
**multicast** group derived from the room name — so there's nothing to keep
running, nothing sent before you joined is visible, nothing is ever stored,
and the idle footprint is ~0 CPU and a few MB. Every datagram is encrypted
with **AES-256-GCM** using a key derived from room + passphrase (PBKDF2), and
traffic uses TTL 1 by default so it never leaves the local segment.

Delivery is layered to survive messy real-world LANs:

- Multicast goes out on **every usable interface**, so a machine on Ethernet and Wi-Fi reaches peers on either segment.
- A **directed-broadcast** copy per subnet is the fallback for networks that filter multicast; duplicates are de-duplicated automatically.
- Interfaces are **re-scanned every 20 s** — roaming between access points, waking from sleep, or switching networks needs no restart.
- **VPN tunnels are skipped** during auto-detection so chat stays on the LAN (`-iface` overrides).
- Messages are size-bounded so a frame **never fragments** — fragments are the first thing unreliable Wi-Fi drops.

Wire format, package layout, and threading model: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Security model

**Protects against:** casual shoulder-surfing (the boss key, the log-like
format) and passive sniffing of a **private** room — without the passphrase,
captured packets are unreadable ciphertext.

**Does *not* provide:**

- **Privacy in open rooms.** With no passphrase, the key derives from the room name alone — treat an open room as a public channel on that LAN. The startup banner tells you which mode you're in.
- **Identity.** Anyone with the passphrase can use any nickname; there is no proof that "alice" is Alice.
- **Forward secrecy or message signing.** It's a lightweight LAN toy, not Signal — don't send anything you wouldn't say out loud in the office.

Incoming text is stripped of control characters before display, so a peer
can't inject terminal escape sequences — but a shared passphrase still means
shared trust.

## Troubleshooting

**`command not found: lanchat`** right after installing — the terminal cached
its old PATH. Open a new terminal (or run `hash -r`).

**Same Wi-Fi but can't see each other?**

1. **Same room *and* passphrase?** A different passphrase is a different key — open and private rooms of the same name don't mix.
2. **Firewall.** Say yes when macOS/Windows asks on first run (Private networks on Windows).
3. **AP isolation / guest Wi-Fi** blocks clients from talking to each other — nothing on the device can fix that; use a trusted network.
4. **Multicast-filtering office Wi-Fi** is detected ("couldn't join multicast" in the banner) and sshhh-lanchat falls back to directed broadcast automatically. If both are blocked, that's AP isolation — see 3.
5. **Different subnets.** TTL 1 keeps traffic on one segment by design; if your network routes multicast between subnets, `-ttl 4` lets frames cross.
6. **VPN.** Auto-detection skips tunnels; to chat over a multicast-capable tunnel, pin it with `-iface utun3` (names via `ifconfig` / `ip addr` / `ipconfig`).
7. **Debug:** run with `CHAT_DEBUG=1` to print received-packet diagnostics to stderr.

## Limitations (by design)

- **Best-effort delivery.** UDP can drop a packet on a congested network; a missed line is simply missed — it matches the ephemeral, no-storage model, and the multicast + broadcast layering makes loss rare.
- **Single LAN segment by default.** TTL 1 won't cross routers — it's a *local* chat (see `-ttl`).
- **Multiple instances on one macOS machine** share a socket, so which window receives a looped-back packet can be unreliable. One instance per machine — normal use — is unaffected.

## Development

```sh
make build      # build ./lanchat for this machine
make test       # unit tests (crypto round-trip, room isolation, dedup, sanitizer)
make vet        # go vet ./...
make cross      # binaries for all desktop targets into dist/
```

Standard Go layout: `cmd/lanchat/` is the CLI entry point, and
`internal/{chat,crypto,proto,roster,transport,ui}` hold the session loop,
key derivation + AES-GCM sealing, message records/dedup, presence tracking,
UDP multicast/broadcast, and the raw-mode terminal UI. Zero third-party
crypto — encryption is Go's standard library; `golang.org/x/{net,term,sys}`
are the only dependencies. Deeper tour in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md); the release process (push a
version tag, CI builds + checksums + publishes) in
[docs/RELEASING.md](docs/RELEASING.md).

## License

[MIT](LICENSE) © Granth Gaurav
