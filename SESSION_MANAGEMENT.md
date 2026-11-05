# Session creation and display names

## Lifecycle contract

Owner clients can create a detached terminal on a connected session's machine,
rename that session, or force-close it. Local headless clients use the existing owner-permissioned
Unix socket; remote clients use authenticated `POST /sessions/manage`. Share
credentials and other accounts cannot manage the session, regardless of their
terminal control lease. The relay does not execute programs on its own machine.

Creation reexecutes the host's Lingon executable with `-x`, using Go's `os/exec`
and a separate Unix process session. Standard input/output/error point to
`/dev/null`; a child-owned pipe reports successful socket readiness. The child
survives the creating client and original host. Startup failure or cancellation
before readiness terminates the child. No in-process fork is needed.

The host constructs all executable arguments. It forwards the selected config
root/file, shell, terminal type, current geometry and publishing state,
respawn/scrollback settings, relay authentication and TLS trust directory.
The child inherits the source session's launch directory (initially the owning
process's startup cwd, editable with `Ctrl+l f`), rather than tracking the
running shell's cwd. Embedded hosts can specify
`InteractiveOptions.HeadlessExecutable` when their own executable is not the
Lingon CLI; `ConfigDir` and `ConfigFile` select the child configuration.

Child creation reuses the host's selected auth file. Token-only hosts pass the
current credential through a private inherited pipe; the child consumes and
closes it before starting the terminal. Generated child arguments and environment
contain no access token. Explicit user-supplied CLI token flags retain their
existing semantics.

Ordinary headful `lingon` retains its in-process `Ctrl+l c` tabs. Attached clients
create detached sessions with `Ctrl+l c`. CLI rename is `Ctrl+l e`; Android puts
both actions in Settings, and the Web UI puts them in its session menu. Only the
creating client selects the new session; other clients discover it normally.

`Ctrl+l j` creates or toggles a popup terminal for the selected tab in host and
attach. It covers the lower part of the viewport without resizing the parent.
The covered area, including its themed name/hint bar, is at most 24 rows or half
the viewport, whichever is smaller. Each tab keeps its own popup. The originating
UI omits its popup sessions from tab navigation and search; other viewers see
ordinary sessions. Hiding keeps the terminal running. If its parent closes, the
popup survives and becomes a normal tab. `Ctrl+l J` forcefully terminates the
popup, even while hidden.

After `Ctrl+l`, `Ctrl+j` is an alias for `j` (show/hide popup), `Ctrl+n` for `n`
(next tab), `Ctrl+p` for `p` (previous tab), `Ctrl+t` for `t` (next theme), and
`Ctrl+h` for `h` (help).
These control keys pass through normally without the prefix. Session search
uses `Ctrl+l /` for important tabs in imode (all tabs otherwise), and
`Ctrl+l ?` for all tabs at all times.

Popup programs start with the content box's width and height, excluding the bar.
Creation can supply initial columns and rows; without them, a new session inherits
the source session's geometry. The PTY is sized before the program starts, and
enclosing-terminal `LINES`/`COLUMNS` exports are removed so programs query their
own terminal. Taking control or typing into a popup keeps its bounded geometry.
The parent retains normal window-resize behavior.

`Ctrl+l [` browses the focused popup's own scrollback within its content box.
The separator stays above it; `g` reaches the oldest retained row, `G` returns
to the newest rows, and `q` returns to the live popup. This also works in offline
local headless attach.

`Ctrl+l Q` force-closes the focused terminal, including a shown popup, without
depending on terminal input or EOF handling. Owner-authorized remote close uses
the management transport; its acknowledgement is written before the host closes
the session transport. `Ctrl+l q` quits attach without closing any terminals.
Remote force-termination requires an updated running host and, when using a
relay, an updated relay. Older runtimes reject the operation with
`unknown session operation`; updating only the attach client is insufficient.
Raw Ctrl+D leaves attach from an ordinary tab. In a focused popup it passes
through as normal terminal input, allowing shell logout to close the popup.
Once focus returns to the parent, the next Ctrl+D detaches attach. Hidden popups
do not change the parent's detach behavior. `Ctrl+l d` still sends an explicit
Ctrl+D to the focused terminal.

The application retains its own PTY settings when control changes. Explicit
Ctrl+D is byte `0x04`: with the usual canonical EOF setting it flushes a partial
line, or produces EOF on an empty line; in raw mode the application receives the
byte unchanged. Lingon does not enable/disable the PTY's EOF character around the
write. This behavior requires an updated running host, not just a new attach
client.

`Ctrl+l C` sends exactly carriage return followed by `~.` to disconnect SSH;
lowercase `c` remains new session.

In scrollback (`Ctrl+l [`), lowercase `v` enables a line-copy cursor at the
middle-left of the viewport. Existing vertical navigation moves that cursor;
the viewport scrolls when it reaches an edge. Horizontal movement is disabled
in visual mode. Space starts an inclusive full-line selection, shown with
inverted colors. Space or Enter copies those lines and clears the selection while
keeping the visual cursor. Live output continues without moving the selected
rows. `v` cancels an unfinished selection and returns to ordinary scrollback;
`q` leaves scrollback. Enter does nothing without an active selection; Space
starts selection. Uppercase `V` is reserved.

`Ctrl+l ]` pastes the copied text into the focused terminal. The clipboard is
shared by tabs of that interactive Lingon process and lasts only in its memory;
it is never synchronized to the host, other Lingon processes, or the OS
clipboard. Copying includes full source rows, even when wider than the viewport,
trims terminal padding at the right edge, and ends each physical row with a
newline. Paste honors the destination application's bracketed-paste mode.

Important-tab marks are UI-local, keyed by immutable session ID and shared by
the local and remote tabs displayed in that UI. `Ctrl+l i` (also
`Ctrl+l Ctrl+i`) opens a batch prompt prefilled with the parent tab and a trailing
space. Enter toggles all listed tabs once without switching; Escape or Ctrl+C
cancels. Tab completes the current entry without adding a space, then cycles
through tabs in that same entry; Shift+Tab cycles backwards. Each empty entry
starts after the active tab with Tab, or before it with Shift+Tab, wrapping at
the ends. A typed prefix takes precedence over that starting position. Space
accepts the current completion and begins another entry. Enter includes a completed final
entry even without a trailing space. Ctrl+W deletes a whole space-separated
name, preserving punctuation within remaining names.
Typed names must resolve uniquely (exact names take precedence over prefixes);
ambiguous or missing entries keep the prompt open without partially applying
marks. Initial, Tab-completed and Space-accepted entries retain session IDs,
including duplicate
display names. `Ctrl+l I` resets all marks outside the prompt. Removing
the last mark leaves the mode. Unmarked tabs and the bar use the selected theme's
darker terminal palette, preserving
the active-tab style and search brackets. The active unmarked tab uses a brighter
background, stronger text contrast and bold text. `Ctrl+l n/p` and their
control-key aliases cycle only marked IDs while important mode is active.
`Ctrl+l /` restricts suggestions and Tab/Shift+Tab cycling to marked IDs,
retaining the current tab's place in the
full tab order as the cycling origin. It searches all tabs when the mode is
inactive; `?` always searches all tabs. Rename and reconnect do not alter marks.
Removed IDs do not transfer their marks to a new session with the same name.
Marks can be toggled individually or cleared with reset; exiting the UI discards
them. Neither the backend nor
Android/Web clients store this UI preference.

## Launch directories

`Ctrl+l f` edits the focused session's future launch directory through the same
owner-only management route as creation/rename. Read-only `directory` requests
return the current path and one level of matching subdirectories; `chdir` commits
a validated absolute path. Neither operation changes a process-wide cwd or sends
shell commands into the running application. Child sessions copy the source's
setting at creation, and respawns read their own session's latest setting.
These runtime settings survive attach/relay reconnects but not an owner restart.

Completions include directory symlinks, exclude ordinary files and terminal
control characters, and never traverse the whole tree. Oversized result sets
produce an explicit request to refine the prefix. Directory and save requests
are bounded by the input owner's injected clock. Typing refines a cached or
in-flight lookup at the same level; changing levels starts a new asynchronous
query. Tab/Enter wait for their result before later keys run,
with WebSocket acknowledgement reads and rendering independent of that wait.
Generation checks discard cancelled and superseded replies. Invalid targets
keep the field open; a timed-out save requires reloading to confirm its outcome.

## Identity and retries

Display names are independent of immutable session IDs and socket filenames.
Renaming changes neither connections nor share tokens. Headless names persist
in local state; interactive names last for the running host process. Publishers
register their current name on reconnect. Names contain 1–80 Unicode code points,
with spaces automatically replaced by underscores and control/format characters
rejected. This normalization applies at startup, rename and persisted metadata
loading; it never changes IDs, socket paths or share tokens. UIs may trim
surrounding whitespace before submitting a name.
Duplicate display names are allowed; IDs disambiguate sessions.

Each operation has a client-generated request ID. A host serializes lifecycle
work, coalesces matching duplicates and retains the last 256 completed receipts.
Conflicting reuse of an ID is rejected. Creation derives a stable child ID from
the source session and request ID and checks existing live headless records.
This is bounded deduplication, not an indefinitely durable transaction journal.
Clients never automatically retry an uncertain mutation. A missing response
requires refreshing session discovery before another user action.
An explicit HTTP 401 precedes execution: the Web UI may refresh owner credentials
and retry once with the same request ID and payload. Failed refresh, a second
401, other HTTP errors, and missing or malformed responses do not retry mutations.
Attach gives refresh and mutation one 15-second budget using its injected clock;
operation timeout does not cancel the terminal session.

Management messages use a host-only protocol envelope and never enter terminal
replay history. Historical rename receipts acknowledge the old request without
overwriting newer display metadata. Host disconnect/replacement releases pending
relay waiters with an uncertain-outcome result. Queues, waiter counts and response
sizes are bounded, and a separate publisher worker keeps process startup outside
the terminal input reader.

## Window sizes

Interactive host window restoration (`lingon -r/--restore`) keeps each local
and attached tab's own dimensions. Switching tabs requests that tab's window
size; manual resizing updates only the selected session. `Ctrl+l !` toggles
restoration in host and attach. Shell respawn uses `-R/--respawn`; the interactive
respawn shortcut remains `Ctrl+l r`.

## Verification

The feature has unit coverage for validation, busy/conflicting requests, bounded
receipts, duplicate coalescing, interrupted requests, stale host responses,
historical rename acknowledgements, owner authorization and uncertain-response
handling. Name editing never renders pasted control/format characters. Local
selection sorts display names with stable IDs as tie-breakers. Host registration
installs initial metadata before reading runtime name updates, and remains able
to answer WebSocket pings while disk persistence is blocked. A real HTTP/WebSocket
test holds the lifecycle worker blocked and proves
terminal input reaches the host before that worker is released.

Actual CLI tests use `ptytest`-owned child PTYs to exercise local/relay creation,
Unicode rename with stable sockets, duplicate creation, reconnect metadata,
selected-child input and child survival after parent exit. Browser tests run the
real detached executable and relay, exercise menu/dialog actions, and verify
selection and readiness. Android targeted instrumentation exercises both actions,
Unicode rename, terminal input after creation/rename and hidden share-session
actions. Existing no-flicker regression assertions remain intact.

### Focused checks

Run the lifecycle regression tests, replay benchmark and UI checks serially:

```sh
bash scripts/verify-contained.sh go test -tags=integration -p 1 ./integration/pty/attach -run '^TestSessionManagement' -count=1 -v
bash scripts/verify-contained.sh go test ./internal/relay -run '^$' -bench '^BenchmarkWSReplay$' -benchmem -count=3 -benchtime=1s
bash scripts/verify-contained.sh go test -tags=integration -p 1 ./integration/webui -run '^TestWebUISessionManagement$' -count=1
LINGON_IT_ONLY=settings_create_terminal_and_rename_session,settings_session_management_hidden_for_share bash scripts/verify-contained.sh bash android/scripts/run-integration-tests.sh
```

PTY interaction is isolated inside test-owned terminals. The Android runner owns
and cleans up its harness, detached terminals, emulator and adb server; the above
commands share the enforced aggregate ceiling of 4 CPUs / 4 GiB RAM and zero swap.

`TestSessionManagementStartupPerformance` compares five alternating direct
`lingon -x` launches and routed creations of the same binary. The optional
`TestSessionManagementStartupBaselineComparison` requires a separate baseline
executable through `LINGON_TEST_STARTUP_BASELINE_BINARY`. Benchmark samples
describe the current host and load, not a latency guarantee. Keep measurements
and run results under ignored `build/verification/`, rather than in this document.

These focused commands do not replace the full [release gate](RELEASE.md).

Desktop input/display checks cover visual line selection and paste, important
tabs and filtered search, help headers, and native EOF delivery. They exercise
actual keys in owned PTYs, including real CLI connections over local sockets and
the relay. Run them serially:

```sh
bash scripts/verify-contained.sh go test -race ./internal/ptytest -run '^Test(ScrollbackVisual|Important|HelpCopyright)' -count=1
bash scripts/verify-contained.sh go test -race -tags=integration ./integration/pty/attach -run '^Test(ScrollbackCopyPaste|ImportantTabsLocalAndRelayCLI|ApplicationEOFNativeTermiosAndPartialLineCLI)' -count=1
```
