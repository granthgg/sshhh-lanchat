# lanchat — ephemeral terminal chat for your LAN

Talk to people on the **same Wi-Fi** from a terminal. No server, no account, no
database, nothing written to disk. Messages exist only while your window is
open — close it and they're gone. It looks like log output, and one keystroke
turns the screen into a fake build so a glance over your shoulder reads as
"compiling," not "chatting."

It's a single ~4 MB binary. Everyone who runs it with the same **room** and
**passphrase** is in the same conversation, like tuning a walkie-talkie to a
channel.

---

## Use it in 30 seconds

```sh
lanchat
```

That's it — you're in the room called **"lobby"**. Now just **type a message and
press Enter**; anyone else on your Wi-Fi who also ran `lanchat` sees it. Type
`/quit` (or press Ctrl-C) to leave.

```
welcome to lanchat 2.1.0
you're in room "lobby" as "granth"
→ type a message and press Enter to send it
→ everyone on this Wi-Fi in the same room sees it in real time
→ /help = commands   /nick = rename   /quit = leave   Ctrl-B = quick-hide
» hello, anyone around?
14:22:04 alice        hey! yes
» ▏
```

**To make it private**, pick a room name and a shared password. Everyone who
wants in uses the **same two things**:

```sh
lanchat -r team -k coffee123
```

Only people who type that exact room + password can read the messages. Anyone
else on the network just sees encrypted noise.

> Prefer not to put the password on the command line? Use `lanchat -r team -ask`
> (it asks you and doesn't show it) or set `CHAT_KEY` in your environment.

---

## Install

### Option A — Download and run (no Go, easiest)

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
e.g. `mv lanchat-macos-arm64 ~/.local/bin/lanchat` — or use **Option B**, which
does that for you.

### Option B — Build from source (needs Go 1.25+)

<details>
<summary><b>How to install Go</b></summary>

- **macOS (Homebrew):** `brew install go`
- **Windows / Linux:** the official installer from the [Go downloads page](https://go.dev/dl/).
- **Ubuntu / Debian:** `sudo apt update && sudo apt install golang-go`
</details>

**macOS / Linux**

```sh
git clone https://github.com/granthgg/sshhh-lanchat.git
cd sshhh-lanchat && ./install.sh
```

**Windows (PowerShell)**

```powershell
git clone https://github.com/granthgg/sshhh-lanchat.git
cd sshhh-lanchat
powershell -ExecutionPolicy Bypass -File install.ps1
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

---

## Why is it called `lanchat` and not `chat`?

Because macOS and most Linuxes already ship a program called **`chat`** (the old
PPP modem-dialer at `/usr/sbin/chat`). If this tool were also called `chat`,
typing `chat` might run *that* one instead — it exits silently and looks broken.
`lanchat` avoids the collision so `lanchat` always means this program.

---

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

### Flags

| Flag | Meaning | Default |
|------|---------|---------|
| `-r`, `-room <name>` | Channel to join | `lobby` |
| `-n`, `-nick <name>` | Your display name | your username |
| `-k`, `-key <phrase>` | Room passphrase (see note) | none (open room) |
| `-ask` | Prompt for the passphrase without echoing it | off |
| `-iface <name>` | Pin a network interface | auto-detect |
| `-color` | Colorize nicknames | off |
| `-stealth` | Disguise the prompt as a shell `$ ` | off |
| `-prompt <str>` | Custom input prompt | `» ` |
| `-no-broadcast` | Disable the broadcast fallback | off |
| `-version` | Print version | |

> **Passphrases:** prefer `-ask` or the `CHAT_KEY` environment variable over
> `-k`. Anything on the command line is visible in your shell history and to
> other users via the process list.

---

## Stealth: the boss key

Press **Ctrl-B** and the screen is instantly replaced with plausible build
output, ending at a shell prompt. While hidden, **incoming messages are
suppressed** (not just scrolled off) so nothing pops up to give you away — you're
told how many you missed when you come back. Any keystroke restores the chat.
`/boss` does the same thing if you prefer a command. Run with `-stealth` to make
the normal prompt look like a shell too.

---

## How it works

There is **no host and no server**. Every instance sends and receives on a UDP
**multicast** group derived from the room name (with a broadcast copy as a
fallback for networks that drop multicast). That single choice gives you:

- **Nothing to keep running.** Anyone can join or leave at any time; the
  conversation never "goes down" because there's no one holding it up.
- **Truly ephemeral.** UDP is stateless. You only receive datagrams while you're
  listening, so nothing sent before you opened your window is visible, and
  nothing is ever stored.
- **Tiny footprint.** No connections to track; idle CPU ~0, memory a few MB.
- **Privacy on the wire.** Every datagram is encrypted with AES-256-GCM using a
  key derived from your room + passphrase (PBKDF2). People without the
  passphrase — including whoever runs Wireshark on the office network — see only
  ciphertext. Traffic uses multicast **TTL 1**, so it never leaves the local
  segment.

---

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

---

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
4. **VPN.** An active VPN can capture the default route; pin your real interface
   with `-iface en0` (find it via `ifconfig` / `ip addr` / `ipconfig`).
5. **See what's arriving:** run with `CHAT_DEBUG=1` to print received-packet
   diagnostics to stderr.

---

## Limitations (by design)

- **Best-effort delivery.** UDP can drop a packet on a congested network; a
  missed line is simply missed (it matches the "ephemeral, no storage" model).
  Messages are sent over both multicast and broadcast to make loss rare.
- **Single LAN segment.** TTL 1 means it won't cross routers/subnets. That's
  intentional — it's a *local* chat.
- **Multiple instances on one machine (macOS):** works, but which window receives
  a given looped-back packet can be unreliable due to how macOS load-balances a
  shared socket. Normal use — one instance per machine — is unaffected.

---

## Development

```sh
go build -o lanchat .    # build
go test ./...            # unit tests (crypto round-trip, room isolation, dedup, sanitizer)
go vet ./...
make cross               # all desktop targets
```

Layout:

| File | Responsibility |
|------|----------------|
| `main.go` | flags, lifecycle, receive/presence loops, commands |
| `transport.go` | UDP multicast + broadcast, interface selection |
| `crypto.go` | key derivation, AES-256-GCM sealing, wire framing |
| `proto.go` | message record, dedup, sanitizer |
| `roster.go` | presence tracking |
| `ui.go` | raw-mode line editor + thread-safe printer |
| `stealth.go` | the boss-key decoy screen |
| `sockopt_*.go` | per-OS socket options (`SO_REUSEPORT`, broadcast) |
| `legacy/tchat.go` | the original TCP-relay prototype, kept for reference |

Zero third-party crypto — encryption is Go's standard library. The only
dependencies are the official `golang.org/x/{net,term,sys}` packages for
cross-platform multicast and terminal handling.
