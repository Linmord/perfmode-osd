// OSD 液态玻璃：左上角 + 温度/双风扇/实时功耗 + 两路提示 + 弹出停留后渐隐 + 托盘
package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"

	"perfmode-osd/btbat"
	"perfmode-osd/core"
)

//go:embed assets/tray.png
var trayIconPNG []byte

//go:embed assets/tray_turbo.png
var trayTurboPNG []byte

//go:embed assets/tray_balance.png
var trayBalancePNG []byte

//go:embed assets/tray_silence.png
var traySilencePNG []byte

//go:embed assets/tray_app.png
var trayAppPNG []byte

var trayRef *mygo.Tray // 持有托盘引用，防止被 GC 回收

// setTrayMode 按模式换托盘图标 + 悬停提示
func setTrayMode(mode byte) {
	if trayRef == nil {
		return
	}
	var icon []byte
	var name string
	switch mode {
	case 0:
		icon, name = trayTurboPNG, "性能模式"
	case 1:
		icon, name = trayBalancePNG, "均衡模式"
	case 2:
		icon, name = traySilencePNG, "静音模式"
	default:
		icon, name = trayAppPNG, "perfmode"
	}
	_ = trayRef.SetIcon(icon, true)
	trayRef.SetToolTip("perfmode — " + name)
}

// FnEvent 是一次 Fn 热键事件（事件驱动，来自 WMI HID_EVENT20）
type FnEvent struct {
	Code byte // 0x0F=性能模式 0x05=背光 0x06=触摸板
	Val  byte // 对应档位值
}

// startFnWatcher 订阅 root\WMI 的 HID_EVENT20（和 BLD 热键服务同一通道），
// 收到 Fn 键事件就通过 ch 发出。事件驱动，没按键绝不发事件，无需轮询/去抖。
func startFnWatcher(ch chan<- FnEvent) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return fmt.Errorf("CoInitialize: %w", err)
	}
	locUnknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return fmt.Errorf("CreateObject SWbemLocator: %w", err)
	}
	defer locUnknown.Release()
	wmi, err := locUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("QueryInterface: %w", err)
	}
	defer wmi.Release()

	svcRaw, err := oleutil.CallMethod(wmi, "ConnectServer", ".", `root\WMI`)
	if err != nil {
		return fmt.Errorf("ConnectServer: %w", err)
	}
	svc := svcRaw.ToIDispatch()
	defer svc.Release()

	// BLD 的原话 WQL：直接订阅事件类，不包 __InstanceCreationEvent
	evtSrcRaw, err := oleutil.CallMethod(svc, "ExecNotificationQuery", "SELECT * FROM HID_EVENT20")
	if err != nil {
		return fmt.Errorf("ExecNotificationQuery: %w", err)
	}
	evtSrc := evtSrcRaw.ToIDispatch()
	defer evtSrc.Release()
	log.Println("WMI 事件订阅成功：HID_EVENT20")

	// 循环阻塞等事件；NextEvent 带超时，超时只是没有事件，继续等
	for {
		evtRaw, err := oleutil.CallMethod(evtSrc, "NextEvent", 1000)
		if err != nil {
			continue // 超时/无事件
		}
		evt := evtRaw.ToIDispatch()
		if evt == nil {
			continue
		}
		detRaw, derr := oleutil.GetProperty(evt, "EventDetail")
		if derr != nil {
			evt.Release()
			continue
		}
		// EventDetail 是 VT_ARRAY|VT_VARIANT（每元素一个 VARIANT 装 1 字节）
		vals := detRaw.ToArray().ToValueArray()
		evt.Release()
		if len(vals) < 3 {
			continue
		}
		b0, ok0 := vals[0].(byte)
		code, ok1 := vals[1].(byte)
		val, ok2 := vals[2].(byte)
		if !ok0 || !ok1 || !ok2 || b0 != 0x01 {
			continue
		}
		ch <- FnEvent{Code: code, Val: val}
	}
}

var textState struct {
	title, sub string
	temp       int
	fan1, fan2 int
	power      float64
	accent     string // 模式强调色
	glyph      string // 左侧图标字

	// 蓝牙耳机电量（-1 表示未连接/读不到）
	btBattery int
	btName    string
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc   = user32.NewProc("CallWindowProcW")
	procSendMessage      = user32.NewProc("SendMessageW")
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procShellExecute     = shell32.NewProc("ShellExecuteW")
)

const (
	gwlpWndProc        = ^uintptr(3)
	wmWindowPosChanged = 0x0047
	wmNCActivate       = 0x0086
	swpShowWindow      = 0x0040
)

type windowPos struct {
	HWnd, InsertAfter uintptr
	X, Y, CX, CY      int32
	Flags             uint32
}

var (
	origWndProc uintptr
	hookOnce    sync.Once
	hookCB      uintptr
)

func keepActiveProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch uint32(msg) {
	case wmNCActivate:
		r, _, _ := procCallWindowProc.Call(origWndProc, hwnd, msg, 1, lp)
		return r
	case wmWindowPosChanged:
		r, _, _ := procCallWindowProc.Call(origWndProc, hwnd, msg, wp, lp)
		if lp != 0 && ((*windowPos)(unsafe.Pointer(lp))).Flags&swpShowWindow != 0 {
			procSendMessage.Call(hwnd, wmNCActivate, 1, ^uintptr(0))
		}
		return r
	}
	r, _, _ := procCallWindowProc.Call(origWndProc, hwnd, msg, wp, lp)
	return r
}

func installKeepActive(hwnd uintptr) {
	if hwnd == 0 || origWndProc != 0 {
		return
	}
	hookOnce.Do(func() { hookCB = syscall.NewCallback(keepActiveProc) })
	old, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, hookCB)
	origWndProc = old
	procSendMessage.Call(hwnd, wmNCActivate, 1, ^uintptr(0))
}

func view(c *ui.Context) {
	// 材质只在未画背景处透出，root 保持透明
	c.Root().Background(ui.Transparent)

	// 强调色：按模式给左侧图标块和数值上色
	accent := parseColor(textState.accent, ui.Hex("#3b82f6"))

	ui.Row(c).Fill().Padding(16, 18).AlignItems(ui.Center).Gap(14).Children(func() {
		// 左侧渐变图标块
		ui.Box(c).Size(52, 52).Radius(15).Shrink(0).
			Gradient(ui.Hex("#ffffff").Alpha(0.16), accent.Alpha(0.55), 135).
			Border(1, ui.Hex("#ffffff").Alpha(0.14)).
			Center().Children(func() {
			ui.Text(c, textState.glyph).FontSize(26).Bold().TextColor(ui.Hex("#ffffff"))
		})

		// 右侧：标题 / 副标题 / 分隔线 / 三栏数据
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, textState.title).FontSize(23).Bold().SingleLine().TextColor(ui.Hex("#ffffff"))
			ui.Text(c, textState.sub).FontSize(12).SingleLine().TextColor(ui.RGBA(255, 255, 255, 0.66))

			ui.Divider(c).Margin(2, 0, 1, 0)

			ui.Row(c).Gap(10).Children(func() {
				dataCell(c, "温度", fmt.Sprintf("%d°", textState.temp), ui.Hex("#ff9f43"))
				ui.Box(c).Width(1).Height(24).Background(ui.Hex("#ffffff").Alpha(0.10))
				dataCell(c, "风扇1/2", fmt.Sprintf("%d/%d", textState.fan1, textState.fan2), ui.Hex("#54a0ff"))
				ui.Box(c).Width(1).Height(24).Background(ui.Hex("#ffffff").Alpha(0.10))
				dataCell(c, "功耗", fmt.Sprintf("%.1fW", textState.power), accent)
				// 耳机电量：只有连着才显示，标签用真实设备名
				if textState.btBattery >= 0 {
					ui.Box(c).Width(1).Height(24).Background(ui.Hex("#ffffff").Alpha(0.10))
					btColor := ui.Hex("#2ed573")
					if textState.btBattery <= 10 {
						btColor = ui.Hex("#ff4757")
					} else if textState.btBattery <= 40 {
						btColor = ui.Hex("#ffa502")
					}
					label := textState.btName
					if label == "" {
						label = "耳机"
					}
					dataCell(c, label, fmt.Sprintf("%d%%", textState.btBattery), btColor)
				}
			})
		})
	})
}

// dataCell 一个数据格：上小标签，下大数值（数值长时自动缩字号）
func dataCell(c *ui.Context, label, value string, col ui.Color) {
	fontSize := float32(15)
	if len(value) > 6 {
		fontSize = 11.5
	}
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
		ui.Text(c, label).FontSize(10).SingleLine().TextColor(ui.RGBA(255, 255, 255, 0.45))
		ui.Text(c, value).FontSize(fontSize).Bold().SingleLine().FontFeatures("tnum").TextColor(col)
	})
}

// parseColor 解析 #RRGGBB，失败用 fallback
func parseColor(s string, fallback ui.Color) ui.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return fallback
	}
	r, e1 := strconv.ParseUint(s[0:2], 16, 8)
	g, e2 := strconv.ParseUint(s[2:4], 16, 8)
	b, e3 := strconv.ParseUint(s[4:6], 16, 8)
	if e1 != nil || e2 != nil || e3 != nil {
		return fallback
	}
	return ui.RGB(uint8(r), uint8(g), uint8(b))
}

// isElevated 检查当前进程是否有管理员权限
func isElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

// relaunchElevated 用 UAC 提权重启自己，旧进程退出
func relaunchElevated() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	// ShellExecuteW(hwnd, "runas", exe, NULL, NULL, SW_SHOWNORMAL=5)
	procShellExecute.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(exePtr)), 0, 0, 5)
}

// setAutoStart 通过任务计划程序注册开机自启（最高权限，开机不弹 UAC）
// on=true 创建任务；false 删除任务。返回是否成功。
func setAutoStart(on bool) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	var cmd *exec.Cmd
	if on {
		// /RL HIGHEST = 最高权限运行；/SC ONLOGON = 登录时启动；/F 强制覆盖
		cmd = exec.Command("schtasks.exe",
			"/Create", "/TN", "perfmode-osd",
			"/TR", `"`+exe+`"`,
			"/SC", "ONLOGON", "/RL", "HIGHEST", "/F")
	} else {
		cmd = exec.Command("schtasks.exe", "/Delete", "/TN", "perfmode-osd", "/F")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // 隐藏窗口
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("设置开机自启失败: %v %s", err, out)
		return false
	}
	return true
}

// isAutoStart 查询开机自启任务是否存在
func isAutoStart() bool {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", "perfmode-osd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	err := cmd.Run()
	return err == nil
}

// readPowerWatt 通过 Windows 性能计数器读 RAPL package 实时功耗，返回瓦特
func readPowerWatt() float64 {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-Counter '\Energy Meter(rapl_package0_pkg)\Power' -ErrorAction SilentlyContinue).CounterSamples[0].CookedValue`)
	// GUI 子系统下子进程默认弹窗，CREATE_NO_WINDOW 隐藏 PowerShell 窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return -1
	}
	return v / 1000.0 // mW -> W
}

func main() {
	// 提权检查：EC 读写 + WMI 订阅需要管理员权限，没有就自动 UAC 重启
	if !isElevated() {
		relaunchElevated()
		return
	}

	opacity := 0.95
	stayMs := 4000 // 弹出停留时长，默认 4 秒
	if len(os.Args) > 1 {
		if v, err := strconv.ParseFloat(os.Args[1], 64); err == nil && v > 0 && v <= 1 {
			opacity = v
		}
	}
	if len(os.Args) > 2 {
		if v, err := strconv.Atoi(os.Args[2]); err == nil && v >= 500 {
			stayMs = v
		}
	}
	textState.title = "性能模式"
	textState.sub = "功耗墙 65W"
	textState.accent = "#ff4757"
	textState.glyph = "性"

	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:             "perfmode-osd",
			Width:             480,
			Height:            128,
			X:                 24,
			Y:                 24,
			TitleBarStyle:     mygo.TitleBarHiddenInset,
			TitleBarHeight:    0,
			HideWindowButtons: true,
			Vibrancy:          mygo.VibrancyAcrylic,
			Panel:             true, // 工具窗口/面板：不进任务栏、不进 Alt-Tab、置顶
			Hidden:            true, // 平时隐藏，有事件才弹
			Opacity:           opacity,
			Content:           ui.View(view),
		})
		installKeepActive(w.NativeHandle())
		w.OnReadyToShow(func() {
			procSendMessage.Call(w.NativeHandle(), wmNCActivate, 1, ^uintptr(0))
		})

		var animMu sync.Mutex
		var osdGen uint64 // 代次号：每次新触发 +1，旧渐隐检测到代次变化就退出
		showOSD := func(title, sub, accent, glyph string) {
			w.Update(func() {
				textState.title = title
				textState.sub = sub
				textState.accent = accent
				textState.glyph = glyph
			})
			animMu.Lock()
			osdGen++
			myGen := osdGen
			w.SetOpacity(opacity)
			w.ShowInactive() // 只显示不激活，避免出现在任务栏/抢焦点
			animMu.Unlock()
			// 停留 2 秒后渐隐退出；期间若有新触发（代次变了）则放弃本次淡出
			go func() {
				time.Sleep(time.Duration(stayMs) * time.Millisecond)
				animMu.Lock()
				valid := myGen == osdGen
				animMu.Unlock()
				if !valid {
					return // 被新触发接管，停留时间已重新计
				}
				const steps = 14
				for i := 1; i <= steps; i++ {
					animMu.Lock()
					if myGen != osdGen {
						animMu.Unlock()
						return // 连续按：中途又有新触发，停止淡出
					}
					w.SetOpacity(opacity * (1 - float64(i)/float64(steps)))
					animMu.Unlock()
					time.Sleep(22 * time.Millisecond)
				}
				animMu.Lock()
				if myGen == osdGen {
					w.Hide()
					w.SetOpacity(opacity) // 恢复，备下次
				}
				animMu.Unlock()
			}()
		}

		// ---- 系统托盘 ----
		// 菜单点击直接写 EC 并立即弹窗（写 EC 不触发 WMI 事件，所以必须自己弹）
		modeName := map[byte]string{0: "性能模式", 1: "均衡模式", 2: "静音模式"}
		powerWall := map[byte]string{0: "65W", 1: "54W", 2: "45W"}
		modeAccent := map[byte]string{0: "#ff4757", 1: "#2ed573", 2: "#1e90ff"}
		modeGlyph := map[byte]string{0: "性", 1: "衡", 2: "静"}
		blName := []string{"关", "暗", "中", "亮"}
		writeMode := func(raw byte) {
			go func() {
				ec2, e := core.OpenECDirect()
				if e == nil {
					_ = ec2.Write(0xE4, raw)
					ec2.Close()
				}
			}()
			setTrayMode(raw)
			if l, ok := modeName[raw]; ok {
				showOSD(l, "功耗墙 "+powerWall[raw], modeAccent[raw], modeGlyph[raw])
			}
		}
		writeBl := func(lv byte) {
			go func() {
				ec2, e := core.OpenECDirect()
				if e == nil {
					_ = ec2.Write(0x9A, lv)
					ec2.Close()
				}
			}()
			if lv <= 3 {
				showOSD("键盘背光 · "+blName[lv], "档位 "+strconv.Itoa(int(lv))+"/3", "#a55eea", "光")
			}
		}
		// 触摸板：开=0x25:00/0x27:00  关=0x25:01/0x27:02（实测）
		writeTp := func(on bool) {
			go func() {
				ec2, e := core.OpenECDirect()
				if e == nil {
					if on {
						_ = ec2.Write(0x25, 0x00)
						_ = ec2.Write(0x27, 0x00)
					} else {
						_ = ec2.Write(0x25, 0x01)
						_ = ec2.Write(0x27, 0x02)
					}
					ec2.Close()
				}
			}()
			if on {
				showOSD("触摸板 · 开", "已启用", "#00b894", "板")
			} else {
				showOSD("触摸板 · 关", "已禁用", "#d63031", "板")
			}
		}
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "性能模式", Click: func(*mygo.MenuItem, *mygo.Window) { writeMode(0) }},
			{Label: "均衡模式", Click: func(*mygo.MenuItem, *mygo.Window) { writeMode(1) }},
			{Label: "静音模式", Click: func(*mygo.MenuItem, *mygo.Window) { writeMode(2) }},
			mygo.Separator(),
			{Label: "背光 关", Click: func(*mygo.MenuItem, *mygo.Window) { writeBl(0) }},
			{Label: "背光 暗", Click: func(*mygo.MenuItem, *mygo.Window) { writeBl(1) }},
			{Label: "背光 中", Click: func(*mygo.MenuItem, *mygo.Window) { writeBl(2) }},
			{Label: "背光 亮", Click: func(*mygo.MenuItem, *mygo.Window) { writeBl(3) }},
			mygo.Separator(),
			{Label: "触摸板 开", Click: func(*mygo.MenuItem, *mygo.Window) { writeTp(true) }},
			{Label: "触摸板 关", Click: func(*mygo.MenuItem, *mygo.Window) { writeTp(false) }},
			mygo.Separator(),
			{Label: "显示当前状态", Click: func(*mygo.MenuItem, *mygo.Window) {
				// 重新读 EC 当前状态组装，不复用上次弹窗残留的文字（如"蓝牙已断开"）
				go func() {
					ec, err := core.OpenECDirect()
					if err != nil {
						return
					}
					defer ec.Close()
					mode, e := ec.ReadStable(0xE4)
					if e != nil || mode > 2 {
						mode = 0
					}
					showOSD(modeName[mode], "功耗墙 "+powerWall[mode], modeAccent[mode], modeGlyph[mode])
				}()
			}},
			mygo.Separator(),
			{Label: "开机启动", Type: mygo.MenuItemCheckbox, Checked: isAutoStart(),
				Click: func(item *mygo.MenuItem, _ *mygo.Window) {
					if !setAutoStart(item.Checked) {
						log.Printf("开机启动设置失败")
					}
				}},
			mygo.Separator(),
			{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		})
		var trayErr error
		trayRef, trayErr = mygo.NewTray(mygo.TrayOptions{
			ID:      fmt.Sprintf("perfmode-osd-%d", os.Getpid()), // PID 保证每次唯一，避开残留幽灵图标
			Icon:    trayIconPNG,
			ToolTip: "perfmode — 性能/背光控制",
			Menu:    menu,
		})
		if trayErr != nil {
			log.Printf("托盘创建失败: %v", trayErr)
		}

		// ===== 事件驱动：WMI 订阅 Fn 热键，收到事件才弹窗（取代 EC 轮询）=====
		go func() {
			// 启动时读一次当前档位，设托盘图标（不弹窗）
			if ec, err := core.OpenECDirect(); err == nil {
				if m, e := ec.ReadStable(0xE4); e == nil && m <= 2 {
					setTrayMode(m)
				}
				ec.Close()
			}

			ch := make(chan FnEvent, 16)
			go func() {
				if err := startFnWatcher(ch); err != nil {
					log.Printf("WMI 事件订阅失败: %v", err)
				}
			}()
			// 消费事件
			for ev := range ch {
				switch ev.Code {
				case 0x0F: // Fn+F1 性能模式
					setTrayMode(ev.Val)
					if l, ok := modeName[ev.Val]; ok {
						showOSD(l, "功耗墙 "+powerWall[ev.Val], modeAccent[ev.Val], modeGlyph[ev.Val])
					}
				case 0x05: // Fn+F7 背光
					if ev.Val <= 3 {
						showOSD("键盘背光 · "+blName[ev.Val], "档位 "+strconv.Itoa(int(ev.Val))+"/3", "#a55eea", "光")
					}
				case 0x06: // Fn+F4 触摸板
					if ev.Val == 0 {
						showOSD("触摸板 · 开", "已启用", "#00b894", "板")
					} else {
						showOSD("触摸板 · 关", "已禁用", "#d63031", "板")
					}
				}
			}
		}()

		// ===== 蓝牙耳机电量 + 连接/断开监听 =====
		go func() {
			// 上次状态，用于检测变化
			lastConnected := false
			lastBattery := -1
			lastMAC := ""

			for {
				devs := btbat.ReadAll()

				var cur *btbat.Device
				if len(devs) > 0 {
					// 有多个就取第一个（一般只有耳机）
					cur = &devs[0]
				}

				if cur != nil {
					// 已连接
					bat := cur.Battery
					mac := cur.MAC
					name := cur.Name
					if name == "" {
						name = "蓝牙设备"
					}

					changedConn := !lastConnected
					changedBat := bat != lastBattery
					changedMAC := mac != lastMAC

					if changedConn || changedMAC {
						// 刚连上：弹提示 + 显示电量
						w.Update(func() {
							textState.btBattery = bat
							textState.btName = name
						})
						showOSD(name, fmt.Sprintf("已连接 · %d%%", bat), "#0984e3", "耳")
						lastConnected = true
						lastBattery = bat
						lastMAC = mac
					} else if changedBat {
						// 电量变了：更新数据栏，只在低电量时弹提示
						w.Update(func() {
							textState.btBattery = bat
						})
						if bat <= 10 && lastBattery > 10 {
							showOSD(name+" · 低电量", fmt.Sprintf("%d%%，请充电", bat), "#d63031", "电")
						}
						lastBattery = bat
					} else {
						w.Update(func() { textState.btBattery = bat })
					}
				} else {
					// 未连接（读不到电量）
					if lastConnected {
						// 刚断开：用上次记下的设备名
						prevName := textState.btName
						if prevName == "" {
							prevName = "蓝牙设备"
						}
						w.Update(func() {
							textState.btBattery = -1
							textState.btName = ""
						})
						showOSD(prevName+" · 已断开", "连接已中断", "#636e72", "断")
						lastConnected = false
						lastBattery = -1
						lastMAC = ""
					}
				}

				time.Sleep(3 * time.Second)
			}
		}()

		// ===== 低频循环：读传感器 + 功耗（PowerShell 慢），只更新底部数据 =====
		go func() {
			ec, err := core.OpenECDirect()
			if err != nil {
				return
			}
			defer ec.Close()
			for {
				snap, err := core.ReadSensors(ec)
				if err == nil {
					t, f1, f2 := 0, 0, 0
					if v, ok := snap.Values[0x18]; ok {
						t = int(v)
					}
					if len(snap.Fans) > 0 {
						f1 = snap.Fans[0]
					}
					if len(snap.Fans) > 1 {
						f2 = snap.Fans[1]
					}
					pw := readPowerWatt() // 慢操作，隔离在这里
					w.Update(func() {
						textState.temp = t
						textState.fan1 = f1
						textState.fan2 = f2
						textState.power = pw
					})
				}
				time.Sleep(1500 * time.Millisecond) // 低频：数据够用即可
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
