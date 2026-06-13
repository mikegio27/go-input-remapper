# nereus

Monitor and remap inputs, define macros, and switch profiles on Linux. A
persistent **daemon** reads TOML config files and executes the remapping; a
**TUI** edits those files and drives the daemon. Built on
[go-evdev](https://github.com/mikegio27/go-evdev).

The config files are the source of truth. The daemon watches them and hot-reloads
on change; the TUI is just a convenient editor plus a live control client.

## How it works

```
  TUI / CLI ──unix socket──▶ daemon ──grabs──▶ /dev/input/eventX (real device)
      │                        │
      └──writes TOML──▶ config  └──uinput──▶ virtual device ──▶ system
                       (source of truth, watched + hot-reloaded)
```

For each configured device the daemon grabs it exclusively, transforms its event
stream (key swaps, suppression, chord-triggered macros), and re-emits the result
through a uinput virtual device. Devices are matched by stable identity
(uniq / vendor+product+name / phys), not the volatile `/dev/input/eventX` path,
so remaps survive reboots and hotplugs.

## Commands

```
nereus            # open the TUI (default)
nereus daemon     # run the daemon (usually via systemd)
nereus list [-r]  # list devices; -r shows remap recommendations
nereus status     # daemon's active profile + bound devices
nereus reload     # tell the daemon to re-read its config
nereus validate   # check config files without applying them
```

Global flags: `--config-dir` (default `$XDG_CONFIG_HOME/nereus`),
`--socket` (default `$XDG_RUNTIME_DIR/nereus.sock`), `-v` verbose.

## TUI

Everything can be done from the TUI — create profiles, edit remaps and macros,
switch profiles, and start/stop the daemon — without leaving it (and without
hand-editing files, though you still can).

- **Devices** — remappable devices (keyboards/mice/gamepads) by default; press
  `a` to show all input devices (touchpads, unknown, virtual). Each row shows its
  classification and whether the daemon has it bound. A physical device often
  exposes several `/dev/input/eventX` nodes (only one carries normal typing); the
  likeliest one to edit is tagged **`★ likely`**, and opening a secondary node
  points you back at it. `enter` opens the remap editor, `m` the macro recorder.
- **Remap editor** — `a` add, `d` delete. While adding, press `c` to *learn a
  key*: the daemon streams the grabbed device's keypresses so you capture a key by
  pressing it. Empty "to" suppresses the key. `s` saves (writes TOML + reloads the
  daemon).
- **Macro recorder** — `n` new: name it, capture the trigger chord, then capture
  ordered tap steps (they run top-to-bottom, one after another). Each step can be a
  single key or a chord — hold several keys together when capturing (e.g. Shift+3)
  and they're emitted as one combined tap. Then pick a repeat mode (`m` cycles
  none/hold/toggle/count); `s` saves.
- **Profiles** — `n` create, `d` delete, `enter` activate (persisted + applied
  live). Creating the first profile activates it automatically.
- **Mappings** — a profile-wide table of every remap and macro across all devices
  in the active profile. `enter` on a row jumps straight into the remap editor (or
  macro recorder) for that device, so you can review and re-edit everything from
  one place. Press `a` to add a new mapping from here: pick a device, then choose
  remap or macro.
- **Status** — daemon connection, active profile, bound devices. `d` starts the
  daemon (as a detached background process; logs to `<config-dir>/daemon.log`),
  `k` stops one the TUI started.

`tab`/`shift+tab` switch screens, `r` refreshes, `q` quits.

## Config

Layout under the config directory:

```
config.toml            # active_profile, virtual_prefix
profiles/<name>.toml   # device bindings: matchers, remaps, macros
```

Example `profiles/default.toml`:

```toml
name = "default"

[[device]]
  [device.match]
  name    = "AT Translated Set 2 keyboard"
  vendor  = "0001"   # hex, like lsusb
  product = "0001"

  [[device.remap]]
  from = "KEY_CAPSLOCK"
  to   = "KEY_ESC"

  [[device.remap]]
  from = "KEY_SCROLLLOCK"
  to   = ""          # empty = suppress

  [[device.macro]]
  name    = "copy-paste"
  trigger = ["KEY_LEFTCTRL", "KEY_J"]   # chord
    [[device.macro.step]]
    key  = "KEY_LEFTCTRL"
    hold = true
    [[device.macro.step]]
    key = "KEY_C"
    [[device.macro.step]]
    delay_ms = 100
    key      = "KEY_V"
    [[device.macro.step]]
    keys = ["KEY_LEFTSHIFT", "KEY_3"]   # chord: types "#"
    [[device.macro.step]]
    key     = "KEY_LEFTCTRL"
    release = true

  [[device.macro]]
  name         = "rapid-fire"
  trigger      = ["KEY_F"]
  repeat       = "hold"   # re-run while the trigger is held
  repeat_ms    = 40       # interval between runs
    [[device.macro.step]]
    key = "KEY_SPACE"
```

Macro steps run **in order, top to bottom**, one after another: `key` taps a key
(or use `hold`/`release`); `keys` taps several keys together as a chord (pressed
in order, released in reverse — e.g. `keys = ["KEY_LEFTSHIFT", "KEY_3"]` types
`#`, and `hold`/`release` apply to the whole set); `text` types a literal string
(US layout); `delay_ms` pauses before the step. Key names are evdev codes
(`KEY_*`, `BTN_*`); `validate` checks them.

By default a macro runs its steps once per trigger. Set `repeat` to make it loop:

- `repeat = "hold"` — re-run every `repeat_ms` while the trigger chord stays held.
- `repeat = "toggle"` — press the trigger to start repeating every `repeat_ms`,
  press it again to stop.
- `repeat = "count"` — run `repeat_count` times total, `repeat_ms` apart.

`repeat_ms` is required whenever `repeat` is set; `repeat_count` is required for
`"count"`.

## Install (system service)

One line — no clone, and no Go toolchain needed (it downloads a prebuilt binary):

```
curl -fsSL https://raw.githubusercontent.com/mikegio27/nereus/main/install.sh | sudo bash
sudo usermod -aG input $USER   # so the TUI works without sudo; then re-login
```

The installer downloads the prebuilt binary for your architecture (linux
amd64/arm64) from the latest [GitHub Release](https://github.com/mikegio27/nereus/releases)
and verifies its checksum. If no prebuilt binary is available for your platform,
it falls back to building from source (which needs Go installed). It then installs
the binary to `/usr/local/bin`, the udev rules
(`packaging/99-nereus.rules`) granting the `input` group access to
`/dev/input/event*` and `/dev/uinput`, loads the `uinput` module, and installs +
enables the systemd unit (`packaging/nereus.service`).

From a checkout you can run the same installer locally: `sudo ./install.sh`.
Useful overrides: `GIR_VERSION=vX.Y.Z` to pin a release, or
`GIR_BUILD_FROM_SOURCE=1` to always build from source.

When run with `sudo`, the installer points the root service at the **invoking
user's** config (`~/.config/nereus`) — the same files the TUI edits —
and relaxes `ProtectHome` so the daemon can read them. (Installed straight as root
with no `sudo` user, it uses the system-wide `/etc/nereus` instead.)

The daemon needs read on `/dev/input/event*` and write on `/dev/uinput`; the udev
rules provide both via the `input` group. The root daemon listens on
`/run/nereus.sock`; the TUI/CLI auto-detect it (a per-user daemon's
socket takes precedence if one is running), so you normally don't pass `--socket`.
Being in the `input` group is what lets your user reach that socket.

`nereus version` (or `--version`) prints the installed version.

### Updating

Re-run the curl installer to pull the latest release binary, or from a checkout
run `sudo packaging/update.sh` — it stops the service, installs the latest binary
(prebuilt release when available, else a source build), and restarts. Re-run the
full `install.sh` only when the udev rules or uinput module config change.

To test local, unreleased changes, build straight from your checkout:

```bash
sudo packaging/update.sh --source       # build & deploy THIS checkout
sudo packaging/update.sh --version v1.2.0  # or pin a specific release tag
```

Prefer these flags over the `GIR_BUILD_FROM_SOURCE` / `GIR_VERSION` env vars —
`sudo` resets the environment, so a var exported in your shell won't reach the
script unless you set it on the `sudo` command line itself.

### Uninstalling

```
sudo packaging/uninstall.sh            # removes the service, binary, udev rule, module config
sudo packaging/uninstall.sh --purge    # also deletes your config (profiles)
```

It keeps your config by default. The uninstaller is self-contained, so you can
also run it straight from the web:

```
curl -fsSL https://raw.githubusercontent.com/mikegio27/nereus/main/packaging/uninstall.sh | sudo bash
```

It leaves the `uinput` module loaded (other software may use `/dev/uinput`) and
your `input` group membership intact.

### Releases

Tagged releases are cut automatically from [Conventional Commits](https://www.conventionalcommits.org)
on `main`: `release-please` opens a release PR that bumps the version and
changelog, and merging it publishes a GitHub Release with prebuilt linux
amd64/arm64 binaries (built by [GoReleaser](https://goreleaser.com)) plus
checksums. See `.github/workflows/`.

## Limitations

- **EV_ABS is not supported** (a go-evdev limitation): gamepad analog sticks and
  triggers, and touchpad/touchscreen absolute axes, can't be remapped — gamepad
  *buttons* work, analog axes pass through untouched.
- Text macros assume a **US QWERTY** layout.
- Chord triggers fire on the key that completes the chord; held modifier keys in
  a trigger still reach the system.

## Development

```
go build ./...
go test ./...
```

Tests are hardware-independent (pure transform/matcher/config/protocol logic and
synthetic-message TUI tests). End-to-end remapping needs `/dev/input` + `/dev/uinput`
access — run the daemon under the system service or with sufficient privileges.

Learn-a-key capture and macro recording also need a running daemon with device
access: the daemon serves the grabbed device's keypresses to the TUI over the
socket, so capture won't produce anything if the daemon isn't running or can't
read the device. If a capture fails, the TUI now reports why (and the daemon logs
it).
