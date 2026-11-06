# Lingon

<img align="right" src="logos/lingon-190x120.png" width="190" height="120" alt="Lingon">

Lingon is a secure, multi-client terminal relay with a CLI, Web UI, and Android app.
It supports these roles and clients:

- **Interactive host (default CLI)**: runs a real terminal session and streams it to the relay.
- **Attach**: connects to a session (TUI or programmatic SDK) to view/control it.
- **Serve**: runs the relay server (HTTPS + WSS + Web UI).
- **Web UI**: browser-based client served by the relay.
- **Android**: native client for Android 8.0 (API 26) and newer.

The host/attach terminal UI paths use a full-framebuffer MVU runtime
(`internal/mvu`) as the canonical renderer and state-transition model.

Host and attach preserve single, double, curly, dotted and dashed underlines,
including their independent colors, in live views and scrollback. This keeps
Neovim spelling marks distinct from ordinary underlined text. The outer terminal
must support the [styled-underline sequences](https://sw.kovidgoyal.net/kitty/underlines/).
For remote sessions, update both the host and attach client so the host captures
these attributes before sending them.

![Lingon](lingon.gif)

## Install

Releases provide CLI archives for Linux, FreeBSD and macOS, each on amd64 and
arm64, plus a signed Android APK. Each archive contains a versioned root
directory with `bin/lingon`, a `bin/lingonx` symlink, and
`share/doc/lingon/LICENSE`. Extract the archive and add its `bin` directory to
your `PATH`. See [RELEASE.md](RELEASE.md#artifact-contract) for artifact names
and checksums.

To build the CLI from source, use Go 1.26 or newer and Make:

```bash
make build
./bin/lingon --help
# Optional: install under a user-owned prefix.
mkdir -p "$HOME/.local/bin"
make install PREFIX="$HOME/.local"
```

The install target also creates `lingonx`; add `$HOME/.local/bin` to `PATH` when
using that prefix. Android setup is documented in [android/README.md](android/README.md).

For a system-wide installation:

```bash
make build
sudo make install
```

`make install` copies the existing binary and creates `lingonx`; it does not
build or run verification.

## Quick start

### 1) Bootstrap config + TLS

```bash
lingon bootstrap
```

This creates a default config at `~/.lingon/config.yaml` and a TLS directory at
`~/.lingon/tls`. The Lingon config root defaults to `~/.lingon`; override it
with `-C/--config-dir` or `LINGON_CONFIG_DIR` when you want every default
config/data path rooted somewhere else. Paths inside the config are relative by
default (resolved against the config file location). Use `-c/--config` to write
one specific config file elsewhere, `--set` to override values, `--force` (or
`-f`) to overwrite, and `--regenerate-certificates` to rotate TLS assets.

Examples:

```bash
lingon bootstrap -c /app/config/config.yaml -s server.base=/lingon -s server.tls.dir=/app/certs
lingon bootstrap -f -c /app/config/config.yaml -s server.base=/lingon
lingon bootstrap -c /app/config/config.yaml --regenerate-certificates
lingon bootstrap -s server.tls.hostname=lingon.my.domain.tld -s server.listen=:8443
```

The last example generates a CA and server cert for `lingon.my.domain.tld`
(CN/SAN) and configures the relay to listen on all interfaces at port 8443 when
you run `lingon serve`. Typically you want to
change the listen address to something other than `127.0.0.1:...` to serve over
the network.

### 2) Create a user

```bash
lingon users add alice
```

By default Lingon generates a password and prints it along with the TOTP seed.
Use `--prompt` if you want to enter a custom password.

User administration keeps the same commands and output while the relay is running.
Use the relay's `server.data_dir` and `server.users_file` configuration (or the
matching `--users-file` override). The running relay applies mutations through a
private local socket so its current tokens, revocations and sessions stay intact.
When the relay is offline, the CLI acquires exclusive state ownership before
editing files. If a live owner's local administration is unavailable, the command
reports that error; retry after the relay is ready.

### 3) Start the relay server

```bash
lingon serve
```

By default this listens on `127.0.0.1:12843` with base path `/v1`.

Each data directory has one active relay owner. The private `relay-state.lock`
and `.admin/socket` coordinate the relay and administrative CLI; the ownership
file remains after shutdown, while the kernel lock is released. The state
directory must not be writable by other users, and `.admin` is private. A stopped
owner cannot persist later writes after another owner starts.

### 4) Log in (client side)

```bash
lingon login -e https://localhost:12843/v1
```

Tokens are stored at `~/.lingon/auth.json` by default.
The file stores auth per normalized endpoint key, so switching `-e/--endpoint`
reuses the matching cached refresh/access tokens.

An established `lingon attach` keeps its live terminal connections when credential
refresh encounters a temporary network, proxy, or server failure (such as HTTP
502). Saved credentials remain intact; the normal refresh/reconnect schedule can
recover without logging in again. A failed attempt to open another tab leaves
the current tab visible and usable. An expired or rejected refresh token still
requires authentication. Refresh POSTs are not blindly replayed on server errors.

Log out from an endpoint (remote revoke + local token removal):

```bash
lingon logout -e https://localhost:12843/v1
```

### Endpoint selection

When `-e/--endpoint` is omitted, Lingon resolves the endpoint in this order:

1. `--endpoint`
2. explicitly configured `client.endpoint` (config file or `LINGON_CLIENT_ENDPOINT`)
3. single stored endpoint in `~/.lingon/auth.json` when the command is using stored auth
4. built-in default endpoint (`https://localhost:12843/v1`)

Important exceptions:

- `lingon login` never depends on `auth.json`; without `--endpoint` it uses the configured endpoint or the built-in default.
- Explicit `--token` / `--access-token` disables auth-file endpoint inference. Those flows use `--endpoint`, configured `client.endpoint`, or the built-in default.
- Share tokens with an embedded endpoint use that embedded endpoint unless `--endpoint` is explicitly passed. Bare share tokens use `--endpoint`, configured `client.endpoint`, or the built-in default.
- If stored auth contains multiple endpoints, auto-inference fails with an ambiguity error until you pass `--endpoint` or set `client.endpoint`.

### Config root and derived paths

By default Lingon stores config, auth, TLS assets, server data, local headless
state, and logs under `~/.lingon`. It does not use `XDG_CONFIG_HOME` for this
default. Use `-C/--config-dir` or `LINGON_CONFIG_DIR` to move the whole Lingon
root for commands and tests:

```bash
lingon --config-dir /tmp/lingon-dev serve
LINGON_CONFIG_DIR=/tmp/lingon-dev lingon login
```

The explicit `-c/--config` flag still points at a single config file. It does
not, by itself, move other default paths unless those values are relative in
that config file or you also set the config root.

### 5) Start a host session (default CLI)

```bash
lingon -e https://localhost:12843/v1 -s my-session

# Or more commonly: use config and no -s (will generate a session ID)
lingon
```

Lingon uses prefix keystroke `Ctrl+l` as *command introducer*. Press `Ctrl+l h`
(`ctrl+l` followed by `h`), or `Ctrl+l Ctrl+h`, for help.

![ctrl-l h help](lingon-ctrl-l-h.gif)

### 6) Attach from another terminal

```bash
lingon attach -e https://localhost:12843/v1 my-session
```

With an X11 image in `CLIPBOARD`, press **Ctrl+V** inside an attach terminal or
a remote tab in the host UI to transfer it to the target application. Local
interactive host tabs forward Ctrl+V unchanged: the app already shares the
host's clipboard. Image transfer also works inside tmux and with local
`attach -x`. The originating Lingon process needs access to
`DISPLAY` and its Xauthority cookie; use a key binding that sends the actual
Ctrl+V byte, rather than the terminal emulator's text-paste shortcut.
XQuartz socket-path displays are supported directly on macOS.

PNG is preferred, with JPEG as the fallback. Original encoded bytes are
preserved, up to 32 MiB per image. The entire image is validated before delivery;
decoded images are limited to 33,554,432 pixels and 32,768 pixels per side to
bound decoder allocation. The target clipboard is populated before
Ctrl+V reaches the application. Softline's chat prompt can then read the image
normally and insert its own cached image path. Codex reads PNG from X11, so a
JPEG-only source must be converted to PNG before copying for Codex. When the
source clipboard has no image, Ctrl+V passes through unchanged; bracketed text
paste is unchanged.

Targets with an existing `DISPLAY` use that X11 clipboard. On a headless target,
Lingon gives each session a private, authenticated, loopback X11 clipboard
endpoint and supplies its `DISPLAY`/`XAUTHORITY` to the shell. This endpoint
supports clipboard reads, not graphical X applications. Its socket and cookie
are removed when the session ends. Clipboard access uses native Go, without
CGO, libX11, `xclip`, or a target-side X server.

On an existing X server, Lingon serves up to eight concurrent large-image reads.
A read that makes no progress for 30 seconds is discarded; the selection remains
available to other readers.

Images travel in bounded internal protocol chunks with upload flow control,
the same control authorization as keyboard input, and no terminal replay
storage. The source and target each buffer a bounded image. Android image
upload UI is planned separately; this transfer operation provides its host-side
foundation.

### 7) Open the Web UI

```text
https://localhost:12843/v1/
```

The Web UI is served by the relay at the base path.

## Personas and workflows

### Rendering architecture (CLI TUI)

Lingon CLI host/attach rendering follows a single MVU pipeline:

- **Model**: typed runtime state (`mvu.State`) for tabs, overlays, connectivity, help, wall, scrollback status.
- **Update**: typed actions via `Runtime.ApplyAction(...)`.
- **View**: full-framebuffer composition + delta emission from composed snapshots.
- **Effects**: overlay/tab timers scheduled through `mvu.EffectScheduler`.

This replaced the previous hybrid/non-framebuffer compositor implementation.
`internal/compositor` has been removed; new TUI behavior should integrate
through `internal/mvu`.

### Interactive host (default command)

Run a terminal host that becomes a shared session:

```bash
lingon -e https://relay.example.com/v1 -s prod-shell
```

Interactive host options (selection):

- `-e, --endpoint` relay endpoint (https/wss base URL; assumes `https://` if omitted)
- `-C, --config-dir` Lingon config root directory (default `~/.lingon`; env `LINGON_CONFIG_DIR`)
- `-s, --session` session id
- `--token` access token (overrides stored auth)
- `--auth-file` path to auth file
- `--shell` override login shell path
- `-g, --geometry` terminal size as `COLSxROWS`, for example `80x24`; resizes the interactive terminal window and PTY; overrides `--cols`/`--rows`
- `--cols`, `--rows` terminal size (`0` means auto-detect for interactive host; built-in default for headless)
- `--scrollback-lines` scrollback lines
- `--term` TERM value for the PTY
- `-R, --respawn` respawn the shell on exit
- `-r, --restore` restore the terminal window to each selected session's size
- `-o, --offline` start offline; toggle relay publishing with `Ctrl+l o`
- `--hostname-only` show hostname-only reconnect banners
- `--theme` TUI chrome theme (`lingon themes`)
- `--disable-desktop-notifications` disable best-effort local desktop notifications for inactivity walls
- `-k, --insecure` skip TLS verification
- `--log-file` client log file path (empty disables)
- `--trace`, `--trace-file` write a JSONL trace

### Headless local PTY host

Run a detached local PTY host (no local interactive host TUI), accessible through
local unix-socket attach/send/sessions commands:

```bash
lingon -x -s local-shell
```

You can also invoke the same mode by binary name:

```bash
lingonx -s local-shell
```

(`lingonx` includes symlink invocations where `argv[0]` basename is `lingonx`.)

Headless local mode notes:

- `-x, --headless` switches root/attach/send/sessions to local headless mode.
- `-g, --geometry COLSxROWS` sets the headless PTY size; `0` dimensions use built-in defaults because detached headless sessions have no foreground terminal to auto-detect.
- Metadata and sockets are stored under `<config-dir>/headless/`.
- `lingon attach -x` works even when relay publishing is offline/disconnected.
- In `attach -x`, `Ctrl+l o` is local-host control and is allowed.

### Attach

`a` is an alias for `attach`, for example `lingon a -r prod-shell`.

Attach to a session over a TUI client:

```bash
lingon attach -e https://relay.example.com/v1 prod-shell
```

Attach to a share token:

```bash
lingon attach --token "$SHARE_TOKEN"
```

If the share token embeds an endpoint, attach uses it automatically unless you
also pass `--endpoint`. Bare share tokens do not infer from `auth.json`; they
use `--endpoint`, configured `client.endpoint`, or the built-in default.

When switching local headless tabs, a session awaiting its first snapshot keeps
the displayed contents under a loading banner. Its first snapshot replaces the
contents; tab switching does not clear the screen.

#### Browse live scrollback

Press `Ctrl+l [` in the interactive host or `lingon attach` to browse scrollback.
Output keeps filling the buffer while the rows you are viewing stay in place.
Navigation moves that anchor; `End` or `G` jumps to the newest content without
leaving scrollback mode. Later output accumulates below that position. If the
history limit removes the anchored text, the view clamps to the oldest retained
rows. Press `q` or `Q` to return to the live screen.

For line copying, press lowercase `v`. The cursor starts at the middle-left;
vertical navigation moves it and scrolls the viewport only at its edges.
Space starts a full-line selection in inverted colors. Space or Enter copies the
inclusive selection and clears its inversion, leaving the visual cursor active.
`v` cancels an unfinished selection and returns to ordinary scrollback; `q`
returns to the live screen. Uppercase `V` is reserved; horizontal movement is
disabled while the visual cursor is active. Enter without an active selection
does nothing; use Space to start one.

Press `Ctrl+l ]` to paste into the focused terminal, including a popup. The text
clipboard is shared across tabs in this one interactive Lingon process, held
only in memory, and independent of the OS/image clipboard and other Lingon
processes. Copy includes entire physical source rows, even when wider than the
viewport, removes right-edge padding, and ends each row with a newline. Paste
honors the destination application's bracketed-paste mode; otherwise row endings
are sent as Enter. Starting or cancelling a new selection preserves the previous
clipboard until Space or Enter commits a replacement.

#### Find a session

In the interactive host and `lingon attach`, press `Ctrl+l /` to open a search
field at the bottom left. In important-tab mode, `/` searches only marked tabs;
`Ctrl+l ?` always searches all tabs. The field shows `/` or `?` to match the
invoking key. Type a case-sensitive session-name prefix; the rest of the suggested
name appears in gray. Enter accepts the suggestion and opens
that exact session. Tab first fills the suggestion; further Tab presses cycle
through session names in tab order, wrapping at the end. Tab on an empty field
starts with the tab after the current session. Shift+Tab cycles in reverse;
on an empty field it starts with the tab before the current session. Both wrap
around the tab list. Like Tab, Shift+Tab first fills an incomplete suggestion.
An exact typed name takes precedence over longer prefix matches. Duplicate names
remain separate selectable sessions.

Search keeps the current session active until Enter. Escape or Ctrl+C cancels;
Backspace edits the prefix. Enter with no matching session leaves the field
open. This is a desktop terminal feature; Android is unchanged.

The search candidate is marked with brackets in the tab bar. Brackets replace
the label's surrounding spaces, keeping its text and neighboring tabs in place.
The bar scrolls to keep the candidate visible as you type or cycle with Tab.
Search temporarily shows the bar even when it is hidden. Cancellation restores
the active tab and prior bar visibility. Outside search, the bar scrolls
automatically to keep the active tab visible when switching tabs or resizing
the window.

#### Hyperlinks

The desktop host and `lingon attach` preserve [OSC 8 hyperlinks](https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda)
through snapshots, updates, tab switches, overlays and scrollback. Link labels
keep the application's styling; ordinary underlines do not become hyperlinks.
Use your outer terminal emulator's usual hyperlink gesture to open a target.
The publishing host or `lingonx` and attach must both run a build with this support;
an older host has already discarded the targets before attach receives the screen.

#### Mark important tabs

In the interactive host or `lingon attach`, `Ctrl+l i` opens the `i` prompt.
An unmarked current tab is prefilled with a trailing space; Enter marks it.
If the current tab is already important, the prompt starts empty so adding other
tabs preserves its mark. Empty Enter closes the prompt without changes. To remove
an existing mark, explicitly include that tab's name in the list.
`Ctrl+l Ctrl+i` is an alias (the second key is also Tab). To toggle several
tabs together, type a name prefix to see its gray completion. Tab fills that
completion without adding a space; further Tab presses cycle through all tabs
within the same entry. Shift+Tab cycles backwards. On each empty entry, Tab
starts after the active tab, or before it with Shift+Tab, wrapping at either end.
Typing a prefix instead starts completion from that prefix. Space accepts
the current suggestion and starts the next entry. Enter applies the entire list,
including a completed final entry without a trailing space. Ctrl+W deletes
one whole space-separated name, including dots, hyphens and underscores. Escape
or Ctrl+C cancels without changing marks. Enter toggles each selected tab once
without switching tabs. Missing or ambiguous unresolved names keep the prompt
open and apply nothing; entries accepted with Tab or Space retain their session
identity even when names are duplicated.

The first mark enters important-tab mode: the bar and unmarked tabs use a darker
variant of the selected theme, while marked tabs keep their normal colors. The active
non-important tab has a brighter background and readable text, with bold text
to make focus clear. Inactive non-important tabs stay dark. Marking an
already important tab removes its mark; removing the last mark leaves the mode.
`Ctrl+l I` clears every mark and restores the normal bar.

While important mode is active, `Ctrl+l n` and `Ctrl+l p` cycle only marked tabs
(also `Ctrl+l Ctrl+n` / `Ctrl+l Ctrl+p`). From an unmarked tab they select the
next or previous marked tab in the full tab order. A single remaining important
tab stays selected; if no marked tabs are present, cycling keeps the current
session. Resetting the mode restores ordinary cycling through all tabs.

`Ctrl+l /` limits search to important tabs while this mode is active. Tab and
Shift+Tab cycle through marked tabs relative to the current tab, including when
the current tab is unmarked. `Ctrl+l ?` always searches all tabs. Outside
important-tab mode, both commands search all tabs.

Marks belong to this UI process and session identities: they survive tab switches,
renames, theme changes and reconnects, without affecting another host/attach UI.
Marking from a popup marks its parent tab. A new session reusing a marked name
does not inherit the mark. Closing marked tabs does not reset the mode; use
`Ctrl+l I` to reset it. Marks are not saved across process restarts. Android and
the Web UI do not expose this mode.

#### Restore the remote terminal size

`lingon attach -g 100x50 [session-id]` (also `lingon a`) requests that size
for both the local terminal window and remote session. `--cols` and `--rows`
set individual dimensions; an unspecified or zero dimension uses the local
terminal size. Geometry overrides those flags. Explicit sizing also resizes an
interactive remote host's terminal window, overriding its usual viewport
protection. It applies to the selected tab and follows tab switches; after the
local window acknowledges the request, manual resizing resumes. If a terminal
refuses the request, Lingon warns and preserves the requested remote size.
Both ends must run a version supporting explicit geometry.

`lingon -g 100x50`, `--cols` and `--rows` likewise request the interactive host
window size. Detached headless startup sets only its PTY size.

`lingon attach -r [session-id]` (or `--restore`) requests a local terminal window
matching the selected session's viewport. It works with relay sessions,
share tokens and local `attach -x`. Press `Ctrl+l !` to toggle the mode while
attached. Restoration runs on entry and when switching tabs. Manually resizing
your window updates only the selected remote session, including interactive
hosts; switching away and back restores that session's new size. Other sessions
keep their own dimensions. Acknowledging Lingon's window request does not resize
the remote session. Turning restoration off resumes the usual resize policy:
remote headless sessions follow the local viewport; interactive hosts retain
their own size.

`lingon -r` / `lingon --restore` enables the same behavior in the interactive
host's tab UI. Switching to an attached remote session restores its dimensions;
returning to a local host tab restores that tab's initial or manually resized
size. Manual resizing changes only the selected session. `Ctrl+l !` toggles
restoration in both host and attach. It is off by default and has no window to
resize in detached headless startup. Host shell respawn uses `-R/--respawn`;
`Ctrl+l r` remains the respawn toggle.

When combined with `--restore`, geometry, `--cols` and `--rows` initialize the
initially selected remote session first. Lingon waits for its resized grid, then
restores the local window to that size and continues as an ordinary restore
session. Other tabs retain their own dimensions; the startup geometry is not
reapplied when switching back. `--geometry` still overrides `--cols`/`--rows`,
and an unspecified or zero axis uses the initial local terminal size.

The request is the xterm window operation `CSI 8 ; rows ; columns t`, in character
cells. Lingon checks the resulting local size and warns if it still differs;
the attachment stays usable and the remote size stays intact. Resizing is a
request to the terminal emulator and window manager, so it cannot force an
unsupported or restricted window to resize.

| Terminal/layer | Resize support |
| --- | --- |
| [Xterm](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html) | Supports the sequence; enable it with `XTerm*allowWindowOps: true` when window operations are disabled. `disallowedWindowOps` can restrict individual operations. |
| [XFCE Terminal](https://github.com/xfce-mirror/xfce4-terminal/blob/master/terminal/terminal-screen.c) | Handles VTE's resize request for the active terminal; ignores it when maximized or fullscreen. |
| [iTerm2](https://iterm2.com/documentation-preferences-profiles-terminal.html) | Enable “A session may cause the window to resize” in the terminal profile. |
| [Tmux](https://github.com/tmux/tmux/blob/master/input.c) | Consumes the ordinary resize sequence; use a direct terminal for window restoration. Split panes also constrain the available grid. |
| Other terminals / window managers | Support varies. A reported `TERM=xterm-256color` alone does not establish window-resize support. Tiling, fullscreen and screen-size constraints may prevent an exact fit. |

#### Change a session's launch directory

`Ctrl+l f` opens **working directory:** at the lower left, prefilled with the
focused session's full launch path. It edits the directory on that session's
owning machine, including through `lingon attach` and `attach -x`. Enter saves;
Escape or Ctrl+C cancels. Ctrl+U clears the field; Ctrl+W deletes the last path
component. Paths can contain spaces and may be absolute, relative to the current
launch directory, or start with `~/` for the owner's home directory.

Type a directory prefix to see a gray completion. Tab completes it and cycles
through matching directories at that level; Shift+Tab cycles backwards. Type
more after the completed trailing slash to descend another level. Files are
excluded, symlinks to directories are included, and completion never scans a
subtree. Missing or inaccessible paths leave the prompt open with an error.

This is a per-session runtime setting: it survives client reconnects, affects
future `Ctrl+l c` sessions, new `Ctrl+l j` popups, and application respawns.
The currently running application keeps its own cwd. Existing child sessions
keep their own setting, and hiding/showing a popup does not restart it. The
setting does not change the attach process's cwd and is not saved after the
owning Lingon process exits. Share clients cannot inspect or change directories.

#### Create and rename sessions

- In `lingon attach` (including `attach -x`), `Ctrl+l c` starts and selects a
  detached terminal on the selected session's machine, equivalent to `lingon -x`.
- In ordinary interactive `lingon`, `Ctrl+l c` retains its existing behavior:
  create another tab in that host process.
- In either CLI, `Ctrl+l e` opens a rename prompt. Enter saves; Escape cancels.
- Android exposes **New terminal** and **Rename session** in Settings. The Web
  UI exposes them in its session menu.

Remote management requires the session's owner account and a connected host;
it does not require the terminal control lease. Share sessions cannot create or
rename terminals. Local `attach -x` management works while publishing is offline.
New detached terminals inherit their source session's current launch directory
and terminal configuration, and survive the creating client or host exiting. Lingon does not
track the running shell's current directory.

Names are display metadata: renaming preserves the session ID, socket filename,
connections, and share tokens. Use the original ID for CLI session arguments.
Names support 1–80 Unicode characters without control characters. Spaces are
automatically replaced with underscores when starting or renaming sessions,
including names loaded from local or relay state. Headless names
persist locally and are advertised again on relay reconnect. Interactive names
last for that host process's lifetime. If a request's response is lost, refresh
the session list before trying again; clients do not automatically repeat
uncertain operations. See [session management](SESSION_MANAGEMENT.md).
The Web UI recovers an explicit 401 with one credential refresh and one retry
using the same request ID. Attach bounds credential refresh and the operation
together to 15 seconds; a timeout leaves the terminal available for further input.

Attach options (selection):

- `-e, --endpoint` relay endpoint (https/wss base URL; assumes `https://` if omitted)
- `-C, --config-dir` Lingon config root directory (default `~/.lingon`; env `LINGON_CONFIG_DIR`)
- `-x, --headless` attach via local headless unix socket sessions
- `-t, --token` share token for anonymous attach
- `--access-token` access token for authenticated attach
- `--request-control` request controller lease on connect
- `-r, --restore` restore each selected session's window size; manual resizing updates that session
- `-g, --geometry COLSxROWS` resize the local terminal and remote session; applied before `--restore`
- `--cols`, `--rows` size individual dimensions (`0` uses local size); applied before `--restore`
- `--pick` interactively pick a session
- `--hostname-only` show hostname-only reconnect banners
- `--theme` TUI chrome theme (`lingon themes`)
- `--disable-desktop-notifications` disable best-effort local desktop notifications for inactivity walls
- `-k, --insecure` skip TLS verification
- `--auth-file` path to auth file
- `--log-file` client log file path (empty disables)
- `--trace`, `--trace-file` write a JSONL trace

### Wall messages and inactivity alerts

Broadcast a wall message to your active sessions:

```bash
lingon wall hello from ops
lingon wall --quiet hello from automation
```

Wall messages are delivered through the relay to connected clients. CLI host and
attach clients show wall messages in the TUI. Local interactive host and attach
clients can also emit best-effort desktop notifications for inactivity walls;
disable that per command with `--disable-desktop-notifications` or in config
with `terminal.disable_desktop_notifications: true`.

`Ctrl+l w` cycles wall inactivity levels for the active session. Configure the
cycle with `--wall-inactive-after` or `terminal.wall_inactive_after`; the
default is `2m,5m,15m`. The relay uses `--wall-timeout` / `server.wall.timeout`
for wall banner display duration.

### Serve

Run the relay server:

```bash
lingon serve
```

Common flags:

- `--listen` listen address (default `127.0.0.1:12843`)
- `--base` base path (default `/v1`)
- `--data-dir` data directory
- `--users-file` user store
- `--connect-limit-count`, `--connect-limit-burst`, `--connect-limit-window`, `--connect-limit-disable` global connection rate limiting
- `--replay-history-bytes` replay history retained for reconnecting clients
- `--tls-mode` `auto|bundle|acme`
- `--tls-dir` local TLS directory (auto mode)
- `--tls-bundle` PEM bundle file (bundle mode)
- `--tls-hostname` hostname for ACME or server cert
- `--tls-cache-dir` ACME cache directory
- `--wall-timeout`, `--wall-inactive-after` wall banner behavior
- `-n, --no-banner` remove login-page branding from served Web UI assets

Reconnect replay is delivered in order by each connection's writer. Large
replays do not consume one live queue slot per frame or hold the session replay
lock during network delivery. Each connection admits at most one queued replay
in addition to the replay being written; a saturated live queue still disconnects
the slow receiver.

Credential changes and user deletion revoke account session-list WebSockets as
well as session participants. Session-list updates also check that the original
account authority remains valid before publishing.

**Reverse proxy note:** WebSocket endpoints (`.../ws/...`) must be proxied to
the relay over HTTP/1.1. If your reverse proxy negotiates HTTP/2 to the
upstream, force HTTP/1.1 for the WebSocket routes (or disable h2 to the relay).

### Web UI

The Web UI is served by the relay server at the base path. Login uses the same
user credentials as the CLI. You can also attach directly from the login form
using a share token (token mode disables username/password/TOTP fields). The
existing URL flow still works via `?token=<share-token>`. Session switching,
sharing, resize control, and themes are all accessible from the menu.

### Android app

The Android app provides a native client with session switching, sharing,
cert management, UI themes, per-session zoom, smooth scrollback panning, and
optional background wall notifications.

Build and run from the root:

```bash
make android-sdk
make android-avd PRESET=medium
make android-emulator PRESET=medium
```

The emulator stays in the foreground. In a second terminal at the repository
root, build and install:

```bash
make android-build
make android-install
```

`make android-install` installs a debug build. To install the release build on
a connected phone, run:

```bash
make android-install-release
```

If more than one device is connected, pass `ADB_SERIAL=<device-serial>` to
`android-install-release`. The target builds the current release before
installing it. Use `APK=/path/to/app-release.apk` to install a specific APK
without rebuilding.

The equivalent targets are available from `android/`:

```bash
cd android
make help
```

The Android `Makefile` includes targets for release builds, AVD setup, and
integration tests.

Android wall behavior:

- Foreground wall messages are shown in-app and do not post Android system notifications.
- Background wall notifications can be enabled from the menu; Android then keeps
  a foreground service active while backgrounded and posts relay wall messages
  as system notifications.
- The app polls for background wall messages only while backgrounded.

## Known caveats and parity notes

- Windows is not a supported Lingon platform. Windows-specific build tags and
  stubs exist only to keep shared packages buildable; they do not establish
  runtime compatibility or support for detached headless mode.
- The CLI no-flicker/tab-overlay invariants are enforced by integration and
  architecture tests in `internal/session`, `internal/attach`, and `internal/mvu`.
- Web UI and Android do not use the `internal/mvu` renderer directly; they use
  the same relay/session protocol but maintain independent UI renderers.
  Minor visual/timing differences across clients can still occur.
- CLI tab switching must preserve the no-flicker invariant, including local and
  remote tabs: no full-screen clear and no separately visible base/tab-overlay
  repaint phases.

## TLS and certificates

Lingon supports three server TLS modes:

- `auto`: generates and stores a local CA and server cert in `tls.dir`.
- `bundle`: loads a PEM bundle (cert + key) from `tls.bundle`.
- `acme`: uses ACME (TLS-ALPN-01) with `tls.hostname` and `tls.cache_dir`.

### ACME mode

When `server.tls.mode` (or `serve --tls-mode`) is set to `acme`, Lingon uses
ACME TLS-ALPN-01 to obtain and renew certificates automatically. This requires:

- `server.tls.hostname` / `--tls-hostname` (public DNS name to request certs for).
- `server.tls.cache_dir` / `--tls-cache-dir` (directory for cert/issuer cache).

The relay must be reachable at the configured hostname over TLS for ACME to
validate the domain (typically on port 443). Certificates are cached under
`tls.cache_dir` and renewed automatically by the ACME manager.

### Export the CA for clients

If you use `auto` mode, export the CA and trust it on clients:

```bash
lingon tls export -o ./lingon-ca.pem
```

Import the CA into your OS/browser trust store for the Web UI. For Android,
import the PEM through the app's certificate-management menu for the relay
endpoint. CLI clients on the bootstrap machine use the CA in their configured
TLS directory; clients on other machines need that CA too, or an OS-trusted
server certificate. The CLI's `--insecure` flag does not apply to the Android app
or browser.

### Insecure mode (not recommended)

You can skip TLS verification with `-k/--insecure` on client commands. This is
useful for quick local testing only.

## Configuration (excerpt)

```yaml
server:
  listen: 127.0.0.1:12843
  base: /v1
  data_dir: .
  users_file: users.json
  tls:
    mode: auto
    dir: tls
    cache_dir: tls/cache
client:
  endpoint: https://localhost:12843/v1
  auth_file: auth.json
terminal:
  term: xterm-256color
  scrollback_lines: 1337
  disable_desktop_notifications: false
  wall_inactive_after: 2m,5m,15m
```

Relative paths are resolved relative to the config file. `~` is not expanded in
config values; use absolute paths or relative paths instead.

## CLI reference (high level)

- `lingon bootstrap` - create config + TLS assets
- `lingon serve` - run relay server
- `lingon login` - authenticate and cache tokens
- `lingon` - start an interactive host session (default command)
- `lingon -x` / `lingonx` - start a detached local headless PTY host session
- `lingon attach` - attach to a session
- `lingon attach -x` - attach to a local headless PTY session
- `lingon send` - send input to a session
- `lingon send -x` - send input to a local headless PTY session
- `lingon wall` - broadcast a wall message to active sessions
- `lingon users` - manage users
- `lingon sessions` - list sessions
- `lingon sessions -x` - list local headless PTY sessions
- `lingon detach <session-id>` - force-stop a local headless PTY session
- `lingon share` - create/revoke share tokens
- `lingon tls` - manage TLS assets
- `lingon completion` - generate shell completion scripts
- `lingon version` - print version/build metadata

Run `lingon <command> --help` for details.

## Makefile

Top-level `Makefile` targets:

- `make build` - build `bin/lingon`
- `make test` - run unit tests
- `make test-webui` - run Web UI integration tests
- `make test-android` - run the Android end-to-end class (other instrumentation classes require explicit selection)
- `make lint` - go vet + golint + golangci-lint
- `make prerelease` - full serial local CI and artifact rehearsal
- `make release VERSION=X.Y.Z RELEASE_MESSAGE='type(scope): change'` - verified squash/tag and clean release build
- `make publish` - publish verified Git/GitHub artifacts, then push the versioned Podman image
- `make push-container` - retry only the final Docker Hub push

See [RELEASE.md](RELEASE.md) for the platform matrix, immutable release contract,
verification gates, artifact layout and Podman Kube development workflow.

Agent-driven checks must run through `bash scripts/verify-contained.sh` or a
target that invokes it. For example, `bash scripts/verify-contained.sh make build`
contains compilation as well as its hygiene check. Run checks serially within
the shared 4 CPU / 4 GiB RAM limit with zero swap. Use `make golangci-lint` for
the repository-pinned linter. `make prerelease` is the full release rehearsal;
focused development checks do not establish release proof.

## SDK

The SDK lives in the `lingon` Go package and mirrors CLI functionality.
See `doc.go` for examples.

> ### Trivia
>
> Lingon is not Klingon. The name comes from the idea that the terminal is a
> place where humans *talk to machines*, using a shared *lingo* the machine
> understands. Lingon makes that lingo usable across *n* platforms and *n*
> simultaneous clients—whether they act as viewers or controllers.
