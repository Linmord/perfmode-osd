[日本語](README.md) | [中文](README.zh-CN.md) | [Deutsch](README.de.md) | [English](README.en.md)

# perfmode-osd

A **liquid-glass OSD indicator** for the MECHREVO WUJIE14 PRO (无界 14 Pro).

Pressing a Fn hotkey pops an acrylic-glass card in the top-left corner
(performance mode / keyboard backlight / touchpad), showing temperature,
dual-fan RPM and CPU power in real time; it lingers a few seconds then
smoothly fades out.

[![UI: mygo](https://img.shields.io/badge/UI-mygo-purple)](https://github.com/egoist/mygo)
[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](https://go.dev)

## Features

- **Event-driven**, not polling: subscribes to the `HID_EVENT20` event class
  in `root\WMI` — **exactly the same channel** as the OEM BLD hotkey service.
  Never pops up without a keypress, zero CPU idle burn, no false triggers
- **Liquid-glass UI**: mygo framework + DWM Acrylic material,
  `WM_NCACTIVATE` hook keeps it from greying out on focus loss;
  `Panel` windows stay out of the taskbar / Alt-Tab
- **Live sensors**: CPU temperature, dual-fan RPM (read directly from the EC),
  RAPL package power
- **Tray menu**: switch 3 performance modes, 4 backlight levels, touchpad
  toggle, autostart, exit
- **Tray icon follows the mode**: performance (red) / balance (teal) / silent (blue)
- **Elegant fade-out**: popup dwell (default 4s) → 14-step smooth fade → auto-hide;
  repeated keypresses restart the dwell instead of being cut short by the
  previous timer
- Automatic UAC elevation (EC access needs administrator rights)

## How it works

### Fn hotkey → event

```
Fn pressed → EC → WMI HID_EVENT20 instance created → root\WMI event subscription
```

The event payload `EventDetail` is 8 bytes: `[01, event code, level value, 0,0,0,0,0]`

| Event code | Key | Meaning | Level value |
| ---------- | --- | ------- | ----------- |
| `0F` | Fn+F1 | Performance mode | 0/1/2 |
| `05` | Fn+F7 | Keyboard backlight | 0/1/2/3 |
| `06` | Fn+F4 | Touchpad | 0/1 |

> ⚠️ **The OEM BLD hotkey service must be disabled** (otherwise it fights
> this program for the events) — run as administrator in PowerShell:
>
> ```powershell
> Stop-Service BLDHotKeyService -Force; Set-Service BLDHotKeyService -StartupType Disabled
> ```

### Direct EC reads

Temperature `0x18`, dual fans `0x9B/0x9C`+`0x9D/0x9E`, backlight `0x9A`,
performance mode `0xE4` (0=performance 65W / 1=balance 54W / 2=silent 45W),
touchpad `0x25`/`0x27` (00/00=on, 01/02=off — counter-intuitive, calibrated by measurement).

Live power comes from the Windows performance counter
`\Energy Meter(rapl_package0_pkg)\Power` (mW).

### UI

Rendered with [mygo](https://github.com/egoist/mygo) (pure Go, zero CGO):
`WindowOptions.Panel` (out of the taskbar),
`TitleBarHiddenInset + VibrancyAcrylic` (glass material), subclassing the
window procedure to intercept `WM_NCACTIVATE` (no greying on focus loss),
`SetOpacity` for the fade.

## Build

Requires Go 1.27+ (no Node / CGO / mygo CLI — pure native UI):

```powershell
go build -ldflags "-s -w -H=windowsgui" -o perfmode-osd.exe
```

`-H=windowsgui` makes the exe a GUI subsystem binary — double-click opens no terminal.

## Usage

Run as administrator (EC port I/O needs it; the app requests UAC elevation on start).

```
perfmode-osd.exe            # default: opacity 0.95, dwell 4 seconds
perfmode-osd.exe 0.9 6000   # arg1 = opacity (0-1), arg2 = dwell milliseconds
```

### Autostart

Check "Autostart" in the tray menu, or manually:

```powershell
schtasks /Create /TN perfmode-osd /TR "C:\path\to\perfmode-osd.exe" /SC ONLOGON /RL HIGHEST /F
```

The scheduled task runs with highest privileges — no UAC prompt at boot.

## Requirements / Support scope

This is **not a universal tool** — it is only verified in the environment below.
EC register offsets, WMI event classes and power counters may differ
completely across BIOS/models; **calibrate yourself before switching**.

| Item | Verified environment (the only tested configuration) |
| ---- | ----------------------------------------------------- |
| Model | MECHREVO WUJIE14 PRO (无界 14 Pro)<br>Motherboard: WUJIE14 Series-HPT |
| **BIOS version** | **T140_HPT_V09** (INSYDE)<br>Other BIOS versions **unverified**; EC layout may change |
| CPU | AMD Ryzen 7 8845HS (8 cores) |
| OS | Windows 11 Pro |
| Hotkey firmware channel | ACPI `PNP0C14\MIFS_0` → WMI event class `HID_EVENT20` |
| Power counter | `\Energy Meter(rapl_package0_pkg)\Power` (RAPL, mW) |

### What breaks on another BIOS / model

Everything below is **model-specific reverse engineering** with no cross-model
portability — re-probe when you switch:

- **Fn hotkey event codes** (`0F`/`05`/`06`) — see event table above,
  may differ entirely per firmware
- **EC register offsets** (`0xE4` mode, `0x9A` backlight, `0x25`/`0x27` touchpad,
  `0x18` temperature, `0x9B-0x9E` fans) — deviation can write to the wrong
  address and corrupt EC state
- **Touchpad semantics inverted** (`00/00`=on, `01/02`=off) — measured on this model
- **RAPL counter instance name** (`rapl_package0_pkg`) — depends on AMD platform + local ACPI
- **Autostart relies on** the BLD hotkey service being disabled (see above),
  re-handle it on another machine

### Probe tools (when switching)

EC snapshot diff method to locate registers: take two EC RAM snapshots before
and after a keypress, diff the changed bytes.

## Acknowledgements

- [mygo](https://github.com/egoist/mygo) — pure-Go framework
- [perfmode](https://github.com/adfnekc/perfmode) (predecessor) — EC read/write core `core/` package
- tuxedo-drivers / mech-forza-control — Uniwill EC platform reverse-engineering references

## License

MIT
