[日本語](README.md) | [中文](README.zh-CN.md) | [Deutsch](README.de.md) | [English](README.en.md)

# perfmode-osd

**Liquid-Glass-OSD-Anzeige für den MECHREVO WUJIE14 PRO (无界 14 Pro).**

Beim Druck der Fn-Taste erscheint oben links eine Acrylglas-Karte
(Leistungsmodus / Tastaturbeleuchtung / Touchpad) mit Echtzeit-Anzeige von
Temperatur, zwei Lüfterdrehzahlen und CPU-Leistung; nach einigen Sekunden
verschwindet sie sanft ausgeblendet.

[![UI: mygo](https://img.shields.io/badge/UI-mygo-purple)](https://github.com/egoist/mygo)
[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](https://go.dev)

## Funktionen

- **Ereignisgesteuert**, kein Polling: Abonniert die Ereignisklasse
  `HID_EVENT20` aus `root\WMI` — **exakt derselbe Kanal** wie der
  BLD-Hotkey-Dienst des Herstellers. Ohne Tastendruck erscheint nie ein
  Popup, null CPU-Last, keine Fehlauslösungen
- **Liquid-Glass-UI**: mygo-Framework + DWM-Acrylic-Material,
  `WM_NCACTIVATE`-Hook verhindert das Ausgrauen beim Fokusverlust;
  `Panel`-Fenster erscheinen nicht in Taskleiste / Alt-Tab
- **Echtzeit-Sensoren**: CPU-Temperatur, zwei Lüfterdrehzahlen (direkt aus
  dem EC), RAPL-Paketleistung
- **Tray-Menü**: drei Leistungsstufen, vier Tastaturbeleuchtungsstufen,
  Touchpad Ein/Aus, Autostart, Beenden
- **Tray-Icon folgt dem Modus**: Performance (rot) / Balance (türkis) / Silent (blau)
- **Elegantes Ausblenden**: Popup-Verweildauer (Standard 4 s) → 14-Stufen-Weichblende →
  automatisch ausblenden; bei wiederholtem Tastendruck wird die Verweildauer neu
  gestartet, nicht vom vorherigen Timer abgeschnitten
- Automatische UAC-Erhöhung (EC-Zugriff erfordert Administratorrechte)

## Funktionsweise

### Fn-Hotkey → Ereignis

```
Fn gedrückt → EC → WMI HID_EVENT20-Instanz erstellt → root\WMI Ereignisabonnement
```

Die Ereignis-Nutzlast `EventDetail` ist 8 Bytes: `[01, Ereigniscode, Stufenwert, 0,0,0,0,0]`

| Ereigniscode | Taste | Bedeutung | Stufenwert |
| ------------ | ----- | --------- | ---------- |
| `0F` | Fn+F1 | Leistungsmodus | 0/1/2 |
| `05` | Fn+F7 | Tastaturbeleuchtung | 0/1/2/3 |
| `06` | Fn+F4 | Touchpad | 0/1 |

> ⚠️ **Der BLD-Hotkey-Dienst des Herstellers muss deaktiviert werden**
> (sonst konkurriert er mit diesem Programm um die Ereignisse) — als
> Administrator in PowerShell ausführen:
>
> ```powershell
> Stop-Service BLDHotKeyService -Force; Set-Service BLDHotKeyService -StartupType Disabled
> ```

### Direkter EC-Lesenzugriff

Temperatur `0x18`, zwei Lüfter `0x9B/0x9C`+`0x9D/0x9E`, Beleuchtung `0x9A`,
Leistungsmodus `0xE4` (0=Performance 65W / 1=Balance 54W / 2=Silent 45W),
Touchpad `0x25`/`0x27` (00/00=Ein, 01/02=Aus — gegen die Intuition, per
Messung kalibriert).

Echtzeit-Leistung über den Windows-Performance-Zähler
`\Energy Meter(rapl_package0_pkg)\Power` (mW).

### Oberfläche

Rendering mit [mygo](https://github.com/egoist/mygo) (reines Go, null CGO):
`WindowOptions.Panel` (nicht in der Taskleiste),
`TitleBarHiddenInset + VibrancyAcrylic` (Glasmaterial), Subklassifizierung des
Fensterprozesses zum Abfangen von `WM_NCACTIVATE` (kein Ausgrauen bei
Fokusverlust), `SetOpacity` fürs Ausblenden.

## Bauen

Go 1.27+ erforderlich (kein Node / CGO / mygo CLI — reines Native-UI):

```powershell
go build -ldflags "-s -w -H=windowsgui" -o perfmode-osd.exe
```

`-H=windowsgui` macht die EXE zu einem GUI-Untersystem — Doppelklick öffnet kein Terminal.

## Verwendung

Mit Administratorrechten ausführen (EC-Portzugriff nötig; das Programm fragt beim
Start automatisch nach UAC-Erhöhung).

```
perfmode-osd.exe            # Standard: Deckkraft 0.95, Verweildauer 4 s
perfmode-osd.exe 0.9 6000   # 1. Param = Deckkraft (0-1), 2. Param = Verweildauer in ms
```

### Autostart

Im Tray-Menü „Autostart" anhaken, oder manuell:

```powershell
schtasks /Create /TN perfmode-osd /TR "C:\path\to\perfmode-osd.exe" /SC ONLOGON /RL HIGHEST /F
```

Der geplante Task läuft mit höchsten Rechten, ohne UAC beim Starten.

## Voraussetzungen / Unterstützter Bereich

Dieses Programm ist **kein universelles Werkzeug** und nur in der unten
geprüften Umgebung verifiziert. EC-Register-Offsets, WMI-Ereignisklassen und
Leistungszähler können je nach BIOS/Modell völlig ungültig sein — **vor
Umstellung selbst kalibrieren**.

| Punkt | Gemessene Umgebung (einzige geprüfte Konfiguration) |
| ----- | --------------------------------------------------- |
| Modell | MECHREVO WUJIE14 PRO (无界 14 Pro)<br>Mainboard: WUJIE14 Series-HPT |
| **BIOS-Version** | **T140_HPT_V09** (INSYDE)<br>Andere BIOS-Versionen **ungeprüft**; EC-Layout kann sich ändern |
| CPU | AMD Ryzen 7 8845HS (8 Kerne) |
| Betriebssystem | Windows 11 Pro |
| Hotkey-Firmwarekanal | ACPI `PNP0C14\MIFS_0` → WMI-Ereignisklasse `HID_EVENT20` |
| Leistungszähler | `\Energy Meter(rapl_package0_pkg)\Power` (RAPL, mW) |

### Was bei anderem BIOS/Modell ungültig wird

Alles Folgende ist **modellspezifisches Reverse-Engineering** ohne Portabilität —
bei einem Umgebungswechsel muss neu untersucht werden:

- **Fn-Hotkey-Ereigniscodes** (`0F`/`05`/`06`) — s. Ereignistabelle oben,
  Firmware-abhängig, kann völlig anders sein
- **EC-Register-Offsets** (`0xE4` Modus, `0x9A` Beleuchtung, `0x25`/`0x27` Touchpad,
  `0x18` Temperatur, `0x9B-0x9E` Lüfter) — Abweichung kann zu Falschschreiben
  an die EC-Schaden richten
- **Touchpad-Semantik invertiert** (`00/00`=Ein, `01/02`=Aus) — Messergebnis dieses Modells
- **RAPL-Zähler-Instanzname** (`rapl_package0_pkg`) — hängt von AMD-Plattform + lokaler ACPI ab
- **Autostart hängt** an der Deaktivierung des BLD-Dienstes (s. oben)

### Untersuchungswerkzeuge (bei Umgebungswechsel)

EC-Snapshot-Differenzmethode zum Auffinden von Registern: zwei EC-RAM-Snaps vor
und nach dem Tastendruck aufnehmen, geänderte Bytes diffen.

## Danksagungen

- [mygo](https://github.com/egoist/mygo) — reines-Go-Framework
- [perfmode](https://github.com/adfnekc/perfmode) (Vorgängerprojekt) — EC-Lese/Schreib-Kern `core/`-Paket
- tuxedo-drivers / mech-forza-control — Uniwill-EC-Plattform-Reverse-Engineering-Referenz

## Lizenz

MIT
