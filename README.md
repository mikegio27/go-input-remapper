# go-input-remapper

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
go-input-remapper            # open the TUI (default)
go-input-remapper daemon     # run the daemon (usually via systemd)
go-input-remapper list [-r]  # list devices; -r shows remap recommendations
go-input-remapper status     # daemon's active profile + bound devices
go-input-remapper reload     # tell the daemon to re-read its config
go-input-remapper validate   # check config files without applying them
```

Global flags: `--config-dir` (default `$XDG_CONFIG_HOME/go-input-remapper`),
`--socket` (default `$XDG_RUNTIME_DIR/go-input-remapper.sock`), `-v` verbose.

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
  keys as ordered tap steps (they run top-to-bottom, one after another), then pick
  a repeat mode (`m` cycles none/hold/toggle/count); `s` saves.
- **Profiles** — `n` create, `d` delete, `enter` activate (persisted + applied
  live). Creating the first profile activates it automatically.
- **Mappings** — a profile-wide table of every remap and macro across all devices
  in the active profile. `enter` on a row jumps straight into the remap editor (or
  macro recorder) for that device, so you can review and re-edit everything from
  one place.
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
(or use `hold`/`release`); `text` types a literal string (US layout); `delay_ms`
pauses before the step. Key names are evdev codes (`KEY_*`, `BTN_*`); `validate`
checks them.

By default a macro runs its steps once per trigger. Set `repeat` to make it loop:

- `repeat = "hold"` — re-run every `repeat_ms` while the trigger chord stays held.
- `repeat = "toggle"` — press the trigger to start repeating every `repeat_ms`,
  press it again to stop.
- `repeat = "count"` — run `repeat_count` times total, `repeat_ms` apart.

`repeat_ms` is required whenever `repeat` is set; `repeat_count` is required for
`"count"`.

## Install (system service)

```
sudo packaging/install.sh
sudo usermod -aG input $USER   # so the TUI works without sudo; then re-login
```

This builds the binary to `/usr/local/bin`, installs udev rules
(`packaging/99-go-input-remapper.rules`) granting the `input` group access to
`/dev/input/event*` and `/dev/uinput`, loads the `uinput` module, and installs +
enables the systemd unit (`packaging/go-input-remapper.service`).

When run with `sudo`, the installer points the root service at the **invoking
user's** config (`~/.config/go-input-remapper`) — the same files the TUI edits —
and relaxes `ProtectHome` so the daemon can read them. (Installed straight as root
with no `sudo` user, it uses the system-wide `/etc/go-input-remapper` instead.)

The daemon needs read on `/dev/input/event*` and write on `/dev/uinput`; the udev
rules provide both via the `input` group. The root daemon listens on
`/run/go-input-remapper.sock`; the TUI/CLI auto-detect it (a per-user daemon's
socket takes precedence if one is running), so you normally don't pass `--socket`.
Being in the `input` group is what lets your user reach that socket.

After pulling new code, update an existing install with `sudo packaging/update.sh`
— it stops the service, rebuilds the binary, and restarts (re-run the full
`install.sh` only when the udev rules or uinput module config change).

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
