[日本語](README.md) | [中文](README.zh-CN.md) | [Deutsch](README.de.md) | [English](README.en.md)

# perfmode-osd

無界 14 Pro（MECHREVO WUJIE14 PRO）向けの**リキッドガラス OSD 提示プログラム**。

Fn ホットキーを押すと、画面左上にアクリリックガラスのカードがポップアップ
（性能モード / キーボードバックライト / タッチパッド）し、温度・二基ファン回転数・
CPU 消費電力をリアルタイム表示、数秒停留してから滑らかにフェードアウトします。

[![UI: mygo](https://img.shields.io/badge/UI-mygo-purple)](https://github.com/egoist/mygo)
[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](https://go.dev)

## 特徴

- **イベントドリブン**（ポーリングではない）：`root\WMI` の `HID_EVENT20`
  イベントクラスを購読——オリジナル BLD ホットキーサービスと**完全に同一の経路**。
  キーを押さなければ絶対にポップアップせず、CPU アイドル消費ゼロ、誤検知なし
- **リキッドガラス UI**：mygo フレームワーク + DWM Acrylic 素材、
  `WM_NCACTIVATE` フックでフォーカス喪失時もグレー化しない；
  `Panel` ウィンドウはタスクバー / Alt-Tab に出ない
- **リアルタイムセンサー**：CPU 温度、二基ファン回転数（EC 直接読み取り）、
  RAPL パッケージ消費電力
- **トレイメニュー**：三段階性能切替、キーボードバックライト 4 段階、
  タッチパッド ON/OFF、自動起動、終了
- **トレイアイコンはモードで変化**：性能（赤）/ バランス（シアン）/ 静音（青）
- **エレガントなフェードアウト**：ポップアップ停留（既定 4 秒）→ 14 段階平滑フェード → 自動非表示；
  連続キー押下時は停留時間をリセットされず、前のタイマーに中断されない
- 自動 UAC 提升（EC アクセスには管理者権限が必要）

## 仕組み

### Fn ホットキー → イベント

```
Fn 押下 → EC → WMI HID_EVENT20 インスタンス生成 → root\WMI イベント購読
```

イベントペイロード `EventDetail` は 8 バイト：`[01, イベントコード, 段階値, 0,0,0,0,0]`

| イベントコード | キー | 意味 | 段階値 |
| -------------- | ---- | ---- | ------ |
| `0F` | Fn+F1 | 性能モード | 0/1/2 |
| `05` | Fn+F7 | キーボードバックライト | 0/1/2/3 |
| `06` | Fn+F4 | タッチパッド | 0/1 |

> ⚠️ **オリジナル BLD ホットキーサービスを無効化する必要があります**
> （無効にしないと本プログラムとイベントを奪い合う）：
> 管理者 PowerShell で実行
>
> ```powershell
> Stop-Service BLDHotKeyService -Force; Set-Service BLDHotKeyService -StartupType Disabled
> ```

### EC 直接読み取り

温度 `0x18`、二基ファン `0x9B/0x9C`+`0x9D/0x9E`、バックライト `0x9A`、
性能モード `0xE4`（0=性能 65W / 1=バランス 54W / 2=静音 45W）、
タッチパッド `0x25`/`0x27`（00/00=ON、01/02=OFF——直感と逆、実測で校正）。

リアルタイム消費電力は Windows パフォーマンスカウンタ
`\Energy Meter(rapl_package0_pkg)\Power`（mW）を使用。

### UI

レンダリングは [mygo](https://github.com/egoist/mygo)（純 Go・CGO ゼロのデスクトップフレームワーク）：
`WindowOptions.Panel`（タスクバーに出ない）、`TitleBarHiddenInset + VibrancyAcrylic`
（ガラス素材）、ウィンドウプロシージャのサブクラス化で `WM_NCACTIVATE` を傍受
（フォーカス喪失時もグレー化しない）、`SetOpacity` でフェードアウト。

## ビルド

Go 1.27+ 必要（Node / CGO / mygo CLI 不要——純ネイティブ UI）：

```powershell
go build -ldflags "-s -w -H=windowsgui" -o perfmode-osd.exe
```

`-H=windowsgui` で exe は GUI サブシステムになり、ダブルクリックでターミナルが出ない。

## 使い方

管理者権限で実行（EC ポート読み書きに必要；起動時に自動で UAC 提升を要求）。

```
perfmode-osd.exe            # 既定: 透明度 0.95、停留 4 秒
perfmode-osd.exe 0.9 6000   # 第 1 引数=透明度(0-1)、第 2 引数=停留ミリ秒
```

### 自動起動

トレイメニューの「自動起動」にチェック、または手動：

```powershell
schtasks /Create /TN perfmode-osd /TR "C:\path\to\perfmode-osd.exe" /SC ONLOGON /RL HIGHEST /F
```

タスクスケジューラは最高権限で実行し、起動時に UAC は出ない。

## 要件 / サポート範囲

本プログラムは**汎用ツールではありません**。以下の実測環境でのみ検証済みです。
EC レジスターオフセット、WMI イベントクラス、消費電力カウンタは
BIOS/機種により完全に無効化される可能性があります。**変更前に自己校正してください**。

| 項目 | 実測環境（唯一検証済み構成） |
| ---- | ---------------------------- |
| 機種 | MECHREVO WUJIE14 PRO（無界 14 Pro）<br>マザーボード：WUJIE14 Series-HPT |
| **BIOS バージョン** | **T140_HPT_V09**（INSYDE）<br>他の BIOS は**未検証**；EC レイアウトが変わる可能性あり |
| CPU | AMD Ryzen 7 8845HS（8 コア） |
| OS | Windows 11 Pro |
| ホットキーファームウェア経路 | ACPI `PNP0C14\MIFS_0` → WMI イベントクラス `HID_EVENT20` |
| 消費電力カウンタ | `\Energy Meter(rapl_package0_pkg)\Power`（RAPL、mW） |

### BIOS/機種を変えると無効になる部分

以下はすべて**本機種専用のリバースエンジニアリング結果**で、機種間の移植性はありません。
環境変更時は再探测が必要です：

- **Fn ホットキーアベントコード**（`0F`/`05`/`06`）——上記イベント表参照、ファームウェアにより完全に異なる可能性
- **EC レジスターオフセット**（`0xE4` モード、`0x9A` バックライト、`0x25`/`0x27` タッチパッド、
  `0x18` 温度、`0x9B-0x9E` ファン）——ずれると誤ったアドレスに書き込み EC 状態を壊す可能性
- **タッチパッドの意味論反転**（`00/00`=ON、`01/02`=OFF）——本機種の実測結果
- **RAPL カウンタインスタンス名**（`rapl_package0_pkg`）——AMD プラットフォーム + 本機 ACPI 依存
- **自動起動は** BLD ホットキー無効化に依存（上記参照）、他機種では再処理が必要

### 探測ツール（環境変更時）

EC スナップショット差分法でレジスターを特定：キー押下前後の 2 枚の EC RAM スナップショットを撮り、変化バイトを diff。

## 謝辞

- [mygo](https://github.com/egoist/mygo) — 純 Go フレームワーク
- [perfmode](https://github.com/adfnekc/perfmode)（前身プロジェクト）——EC 読み書きコア `core/` パッケージ
- tuxedo-drivers / mech-forza-control — Uniwill EC プラットフォーム リバースエンジニアリング参考

## ライセンス

MIT
