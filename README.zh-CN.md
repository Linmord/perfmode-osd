[日本語](README.md) | [中文](README.zh-CN.md) | [Deutsch](README.de.md) | [English](README.en.md)

# perfmode-osd

无界 14 Pro（MECHREVO WUJIE14 PRO）的**液态玻璃 OSD 提示程序**。

Fn 热键按下时，在屏幕左上角弹出亚克力玻璃卡片（性能模式 / 键盘背光 / 触摸板），
实时显示温度、双风扇转速和 CPU 功耗，停留数秒后平滑渐隐。

[![UI: mygo](https://img.shields.io/badge/UI-mygo-purple)](https://github.com/egoist/mygo)
[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](https://go.dev)

## 特性

- **事件驱动**，不是轮询：订阅 `root\WMI` 的 `HID_EVENT20` 事件类——和原厂
  BLD 热键服务**完全同一通道**。没按键绝不弹窗，零 CPU 空转，无误报
- **液态玻璃 UI**：mygo 框架 + DWM Acrylic 材质，`WM_NCACTIVATE` hook
  保持失焦不变灰；`Panel` 窗口不进任务栏 / Alt-Tab
- **实时传感器**：CPU 温度、双风扇转速（EC 直读）、RAPL 封装功耗
- **托盘菜单**：切三档性能、键盘背光四档、触摸板开关、开机启动、退出
- **托盘图标随档位变**：性能（红）/ 均衡（青）/ 静音（蓝）
- **优雅的渐隐**：弹出停留（默认 4s）→ 14 步平滑淡出 → 自动隐藏；
  连续按键时停留时间重新计，不会被上一次的计时截胡
- 自动 UAC 提权（EC 访问需要管理员）

## 工作原理

### Fn 热键 → 事件

```
Fn 按下 → EC → WMI HID_EVENT20 实例创建 → root\WMI 事件订阅
```

事件负载 `EventDetail` 是 8 字节：`[01, 事件码, 档位值, 0,0,0,0,0]`

| 事件码 | 按键  | 含义     | 档位值  |
| ------ | ----- | -------- | ------- |
| `0F`   | Fn+F1 | 性能模式 | 0/1/2   |
| `05`   | Fn+F7 | 键盘背光 | 0/1/2/3 |
| `06`   | Fn+F4 | 触摸板   | 0/1     |

> ⚠️ **必须停用原厂 BLD 热键服务**（否则它和本程序抢事件）：
> 管理员 PowerShell 运行
>
> ```powershell
> Stop-Service BLDHotKeyService -Force; Set-Service BLDHotKeyService -StartupType Disabled
> ```

### EC 直读

温度 `0x18`、双风扇 `0x9B/0x9C`+`0x9D/0x9E`、背光 `0x9A`、
性能模式 `0xE4`（0=性能 65W / 1=均衡 54W / 2=静音 45W）、
触摸板 `0x25`/`0x27`（00/00=开，01/02=关——与直觉相反，实测校准）。

实时功耗走 Windows 性能计数器
`\Energy Meter(rapl_package0_pkg)\Power`（mW）。

### 界面

渲染用 [mygo](https://github.com/egoist/mygo)（纯 Go、零 CGO 的桌面框架）：
`WindowOptions.Panel`（不进任务栏）、`TitleBarHiddenInset + VibrancyAcrylic`
（玻璃材质）、子类化窗口过程拦截 `WM_NCACTIVATE`（失焦不变灰）、
`SetOpacity` 渐隐。

## 构建

需要 Go 1.27+（无需 Node / CGO / mygo CLI——纯原生 UI）：

```powershell
go build -ldflags "-s -w -H=windowsgui" -o perfmode-osd.exe
```

`-H=windowsgui` 让 exe 是 GUI 子系统，双击不弹终端窗口。

## 使用

管理员权限运行（EC 端口读写需要；程序启动会自动请求 UAC 提权）。

```
perfmode-osd.exe            # 默认 0.95 透明度、停留 4 秒
perfmode-osd.exe 0.9 6000   # 第 1 参=透明度(0-1)，第 2 参=停留毫秒数
```

### 开机启动

托盘菜单勾选「开机启动」，或手动：

```powershell
schtasks /Create /TN perfmode-osd /TR "C:\path\to\perfmode-osd.exe" /SC ONLOGON /RL HIGHEST /F
```

任务计划以最高权限运行，开机不弹 UAC。

## 环境要求 / 支持范围

本程序**不是通用工具**，只在以下实测环境验证通过。EC 寄存器偏移、WMI 事件类、
功耗计数器都可能因 BIOS/机型不同而完全失效，**换环境前请先自行校准**。

| 项            | 实测环境（唯一验证过的配置）                                            |
| ------------- | ----------------------------------------------------------------------- |
| 机型          | MECHREVO WUJIE14 PRO（无界 14 Pro）<br>主板：WUJIE14 Series-HPT         |
| **BIOS 版本** | **T140_HPT_V09**（INSYDE）<br>其他 BIOS 版本**未验证**；EC 布局可能变动 |
| CPU           | AMD Ryzen 7 8845HS（8 核)                                               |
| 操作系统      | Windows 11 Pro                                                          |
| 热键固件通道  | ACPI `PNP0C14\MIFS_0` → WMI 事件类 `HID_EVENT20`                        |
| 功耗计数器    | `\Energy Meter(rapl_package0_pkg)\Power`（RAPL，mW）                    |

### 换 BIOS/机型会失效的部分

以下全部是**本机型专属逆向结果**，没有跨机型可移植性，换环境必须重新探测：

- **Fn 热键事件码**（`0F`/`05`/`06`）——见上文事件表，不同固件可能完全不同
- **EC 寄存器偏移**（`0xE4` 模式、`0x9A` 背光、`0x25`/`0x27` 触摸板、
  `0x18` 温度、`0x9B-0x9E` 风扇）——偏离可能导致写错地址损坏 EC 状态
- **触摸板语义反转**（`00/00`=开、`01/02`=关）——本机型实测结论
- **RAPL 计数器实例名**（`rapl_package0_pkg`）——依赖 AMD 平台 + 本机 ACPI 导出
- **开机自启依赖** BLD 热键服务已停用（见上文），换机器后需重新处理

### 探测工具（换环境时用）

EC 快照对照法定位寄存器：拍按键前后两张 EC RAM 快照，diff 出变化的字节

## 致谢

- [mygo](https://github.com/egoist/mygo) — 纯 Go 框架
- [perfmode](https://github.com/adfnekc/perfmode)（前身项目）——EC 读写核心 `core/` 包
- tuxedo-drivers / mech-forza-control —— Uniwill EC 平台逆向参考

## License

MIT
