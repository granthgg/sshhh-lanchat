# lanchat — ephemeral encrypted terminal chat for your LAN

[![CI](https://github.com/granthgg/sshhh-lanchat/actions/workflows/ci.yml/badge.svg)](https://github.com/granthgg/sshhh-lanchat/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/granthgg/sshhh-lanchat?sort=semver)](https://github.com/granthgg/sshhh-lanchat/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/granthgg/sshhh-lanchat)](https://goreportcard.com/report/github.com/granthgg/sshhh-lanchat)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Talk to people on the **same Wi-Fi** from a terminal. No server, no account, no
database, nothing written to disk. Messages exist only while your window is
open — close it and they're gone. It looks like log output, and one keystroke
turns the screen into a fake build so a glance over your shoulder reads as
"compiling," not "chatting."

It's a single ~3 MB binary with no runtime dependencies. Everyone who runs it
with the same **room** and **passphrase** is in the same conversation, like
tuning a walkie-talkie to a channel.

```
  lanchat 2.2.0 — ephemeral encrypted LAN chat
  ────────────────────────────────────────────────────────────────
  room   "lobby" · OPEN — anyone on this Wi-Fi can read it
  you    "granth" · rename with /nick <name>
  send   type a message and press Enter — the room sees it live
  keys   /help · /who · /clear · /quit · Ctrl-B = instant hide
  saved  nothing — messages exist only while the window is open
  ────────────────────────────────────────────────────────────────
  tip: add -k "secret" to go private — share room + passphrase
  waiting for messages…

14:22:03       granth │ hello, anyone around?
14:22:04        alice │ hey! yes
» ▏
```

Names sit right-aligned and bold against a dimmed `│` gutter, so who-said-what
is readable at a glance even without `-color` (which additionally gives every
person a stable color).

## Contents

- [Use it in 30 seconds](#use-it-in-30-seconds)
- [Install](#install)
- [Commands](#commands)
- [Flags](#flags)
- [Stealth: the boss key](#stealth-the-boss-key)
- [How it works](#how-it-works)
- [Security model — read this](#security-model--read-this)
- [Troubleshooting](#troubleshooting)
- [Limitations (by design)](#limitations-by-design)
- [Development](#development)
- [License](#license)

## Use it in 30 seconds

```sh
lanchat
```

That's it — you're in the room called **"lobby"**. Now just **type a message and
press Enter**; anyone else on your Wi-Fi who also ran `lanchat` sees it. Type
`/quit` (or press Ctrl-C) to leave.

**To make it private**, pick a room name and a shared password. Everyone who
wants in uses the **same two things**:

```sh
lanchat -r team -k coffee123
```

Only people who type that exact room + password can read the messages. Anyone
else on the network just sees encrypted noise.

> Prefer not to put the password on the command line? Use `lanchat -r team -ask`
> (it asks you and doesn't show it) or set `CHAT_KEY` in your environment.

## Install

### Option A — Download a prebuilt binary (no Go, easiest)

Grab the file for your system from the **[latest release »](https://github.com/granthgg/sshhh-lanchat/releases/latest)**:

| Your system | File to download |
|---|---|
| Windows (most PCs) | `lanchat-windows-amd64.exe` |
| Windows (ARM) | `lanchat-windows-arm64.exe` |
| Mac (Apple Silicon · M1–M4) | `lanchat-macos-arm64` |
| Mac (Intel) | `lanchat-macos-amd64` |
| Linux (x86-64) | `lanchat-linux-amd64` |
| Linux (ARM) | `lanchat-linux-arm64` |

**Windows** — rename it to `lanchat.exe` and run it. If SmartScreen warns, click
**More info → Run anyway** (the file is unsigned, not malicious).

**macOS / Linux** — in the terminal, in your downloads folder:

```sh
chmod +x lanchat-*                                     # make it runnable
xattr -d com.apple.quarantine lanchat-* 2>/dev/null    # macOS only: clear the "unverified developer" block
./lanchat-macos-arm64                                  # run it (use your file's name)
```

To type just `lanchat` from any folder, move it onto your PATH and rename it,
e.g. `mv lanchat-macos-arm64 ~/.local/bin/lanchat` — or use **Option C**, which
does that for you.

### Option B — `go install` (one command, needs Go 1.25+)

```sh
go install github.com/granthgg/sshhh-lanchat/cmd/lanchat@latest
```

This drops `lanchat` in `$(go env GOPATH)/bin`. Make sure that directory is on
your PATH.

### Option C — Build from source with the installer (needs Go 1.25+)

<details>
<summary><b>How to install Go</b></summary>

- **macOS (Homebrew):** `brew install go`
- **Windows / Linux:** the official installer from the [Go downloads page](https://go.dev/dl/).
- **Ubuntu / Debian:** `sudo apt update && sudo apt install golang-go`
</details>

**macOS / Linux**

```sh
git clone https://github.com/granthgg/sshhh-lanchat.git
cd sshhh-lanchat && ./scripts/install.sh
```

**Windows (PowerShell)**

```powershell
git clone https://github.com/granthgg/sshhh-lanchat.git
cd sshhh-lanchat
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

The installer drops `lanchat` into a directory already on your PATH (like
`/opt/homebrew/bin`), or installs to `~/.local/bin` and adds that to your PATH
by editing your shell startup file for you — so **you can run `lanchat` from any
directory** with no manual setup.

> If the *same* terminal you installed from still says `command not found`, it
> cached the old PATH — open a new terminal (or run `hash -r`).

### Share with friends

Easiest: send them the **[release link](https://github.com/granthgg/sshhh-lanchat/releases/latest)** —
they download one file and run it. No Go, no build, no `git clone`. (Or just
hand them the binary directly over AirDrop / USB / Slack.)

## Commands

| Command | Action |
|---------|--------|
| *(just type)* | send a message |
| `/who` | list who's here right now |
| `/nick <name>` | change your display name |
| `/me <action>` | send an action, e.g. `* alice waves` |
| `/clear` | clear the screen |
| `/boss` | hide (fake build output) |
| `/quit` | leave |
| **Ctrl-B** | **instant boss key** — hide immediately, any key restores |

Editing keys: arrows (move / history), Home/End, Ctrl-A/E, Ctrl-U (clear line),
Ctrl-W (delete word), Ctrl-L (clear screen), Backspace/Delete.

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
| `-version` | Print version | |

> **Passphrases:** prefer `-ask` or the `CHAT_KEY` environment variable over
> `-k`. Anything on the command line is visible in your shell history and to
> other users via the process list.

## Stealth: the boss key

Press **Ctrl-B** and the screen is instantly replaced with plausible build
output, ending at a shell prompt. While hidden, **incoming messages are
suppressed** (not just scrolled off) so nothing pops up to give you away — you're
told how many you missed when you come back. Any keystroke restores the chat.
`/boss` does the same thing if you prefer a command. Run with `-stealth` to make
the normal prompt look like a shell too — in stealth mode chat lines also drop
the bold/gutter styling and render as flat, logger-style output.

## How it works

There is **no host and no server**. Every instance sends and receives on a UDP
**multicast** group derived from the room name. That single choice gives you:

- **Nothing to keep running.** Anyone can join or leave at any time; the
  conversation never "goes down" because there's no one holding it up.
- **Truly ephemeral.** UDP is stateless. You only receive datagrams while you're
  listening, so nothing sent before you opened your window is visible, and
  nothing is ever stored.
- **Tiny footprint.** No connections to track; idle CPU ~0, memory a few MB.
- **Privacy on the wire.** Every datagram is encrypted with AES-256-GCM using a
  key derived from your room + passphrase (PBKDF2). People without the
  passphrase — including whoever runs Wireshark on the office network — see only
  ciphertext. Traffic uses multicast **TTL 1** by default, so it never leaves
  the local segment.

Real-world networks get in the way of naive multicast, so delivery is layered
to work on messy LANs — office Wi-Fi, mesh networks, machines with several
network interfaces:

- Multicast is sent on **every usable interface**, so a machine on both
  Ethernet and Wi-Fi reaches peers on either segment.
- A **directed-broadcast** copy (e.g. `192.168.1.255`) is sent per subnet as a
  fallback for networks that filter multicast (common on corporate Wi-Fi with
  IGMP snooping). Duplicates are de-duplicated automatically.
- Interfaces are **re-scanned every 20 s**: roaming between access points,
  waking from sleep, or switching networks re-establishes multicast membership
  without a restart.
- **VPN tunnels are skipped** during auto-detection so chat traffic stays on
  the LAN instead of disappearing into the tunnel (pin one explicitly with
  `-iface` to override).
- Messages are size-bounded so a frame **never fragments** — fragments are the
  first thing unreliable Wi-Fi gear drops.

For the wire format, package layout, and threading model, see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Security model — read this

**What it protects against:** casual shoulder-surfing (the boss key and the
log-like format) and passive network sniffing of a **private** room (AES-256-GCM;
without the passphrase, captured packets are unreadable).

**What it does _not_ do:**

- **Open rooms are not private.** With no passphrase, the key is derived from the
  room name alone, which anyone can do. Treat an open room as a public channel on
  that LAN. The startup banner tells you which mode you're in.
- **No identity / no spoofing protection.** Anyone with the passphrase can use any
  nickname. There is no proof that "alice" is Alice.
- **No forward secrecy, no message signing.** It's a lightweight LAN toy, not
  Signal. Don't send anything you'd be unwilling to say out loud in the office.
- Text from the network is stripped of control characters before display, so a
  peer can't inject terminal escape sequences — but a shared passphrase still
  means shared trust.

## Troubleshooting

**`command not found: lanchat`** — almost always just the terminal you
installed from caching its old PATH. **Open a new terminal** (or run `hash -r`)
and try again. If it still fails, the installer told you where it put the binary;
run it by full path once (e.g. `/opt/homebrew/bin/lanchat` or
`~/.local/bin/lanchat`) to confirm it's there.

**We're on the same Wi-Fi but can't see each other.**

1. **Same room *and* passphrase?** A different passphrase = a different key = you
   simply can't read each other. Open and private rooms of the same name don't mix.
2. **Firewall.** First run, macOS asks to allow incoming connections and Windows
   prompts to allow the app — say yes (Private networks on Windows).
3. **"AP isolation" / guest Wi-Fi.** Many guest and public networks block clients
   from talking to each other. Nothing on the device can fix that — use a trusted
   network.
4. **Multicast-filtering office networks.** Corporate Wi-Fi often filters
   multicast; lanchat detects this ("couldn't join multicast" in the banner) and
   automatically falls back to directed broadcast per subnet. If your network
   blocks *both*, that's AP isolation in practice — see point 3.
5. **Different subnets behind different routers.** A campus "same Wi-Fi" can
   actually be several routed subnets. By default traffic stays on one segment
   (TTL 1, a privacy feature). If — and only if — your network routes multicast
   between subnets, `-ttl 4` lets frames cross.
6. **VPN.** Auto-detection skips VPN tunnels, so an active VPN no longer
   swallows chat traffic. If you *want* to chat over a tunnel that supports
   multicast, pin it explicitly with `-iface utun3` (find names via
   `ifconfig` / `ip addr` / `ipconfig`).
7. **See what's arriving:** run with `CHAT_DEBUG=1` to print received-packet
   diagnostics to stderr.

## Limitations (by design)

- **Best-effort delivery.** UDP can drop a packet on a congested network; a
  missed line is simply missed (it matches the "ephemeral, no storage" model).
  Messages are sent over multicast on every interface plus a broadcast copy per
  subnet to make loss rare.
- **Single LAN segment by default.** TTL 1 means it won't cross routers/subnets.
  That's intentional — it's a *local* chat (see `-ttl` if your network routes
  multicast).
- **Multiple instances on one machine (macOS):** works, but which window receives
  a given looped-back packet can be unreliable due to how macOS load-balances a
  shared socket. Normal use — one instance per machine — is unaffected.

## Development

```sh
make build      # build ./lanchat for this machine
make test       # unit tests (crypto round-trip, room isolation, dedup, sanitizer)
make vet        # go vet ./...
make fmt        # gofmt the tree
make cross      # build binaries for all desktop targets into dist/
```

The project follows the standard Go layout:

| Path | Responsibility |
|------|----------------|
| `cmd/lanchat/` | CLI entry point — flags, usage, key resolution, wiring |
| `internal/chat/` | composition root: builds a session and runs the loops |
| `internal/crypto/` | key derivation, AES-256-GCM sealing, wire framing |
| `internal/proto/` | message record, dedup, sequence numbers, sanitizer |
| `internal/roster/` | presence tracking |
| `internal/transport/` | UDP multicast + broadcast, interface selection, sockopts |
| `internal/ui/` | raw-mode line editor, thread-safe printer, boss-key decoy |

Zero third-party crypto — encryption is Go's standard library. The only
dependencies are the official `golang.org/x/{net,term,sys}` packages for
cross-platform multicast and terminal handling. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for a deeper tour.

Releases are cut by pushing a version tag (`git tag v2.2.0 && git push origin
v2.2.0`); CI cross-compiles every target and attaches the binaries to a GitHub
Release automatically.

## License

[MIT](LICENSE) © Granth Gaurav
