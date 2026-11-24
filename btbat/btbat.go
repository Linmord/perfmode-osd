// Package btbat 读经典蓝牙设备电量
//
// 关键发现：用 CfgMgr32 的 CM_Get_DevNode_PropertyW 读
// DEVPKEY_BLUETOOTH_BATTERY {104EA319-6EE2-4701-BD47-8DDBF425BBE5} pid=2
// 能拿到电量，而 PowerShell 的 Get-PnpDeviceProperty 读不到同一属性。
//
// 原理参考: github.com/SpLlry/SplusXBTMeter (BTC.cs)
package btbat

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// devPropKey 对应 DEVPROPKEY
type devPropKey struct {
	fmtid windows.GUID
	pid   uint32
}

// spDevInfoData 对应 SP_DEVINFO_DATA
type spDevInfoData struct {
	cbSize    uint32
	classGuid windows.GUID
	devInst   uint32
	reserved  uintptr
}

// Device 一个蓝牙设备的电量信息
type Device struct {
	Name      string // 设备名（从 instanceID 推断或留空）
	MAC       string // 形如 45:7D:43:A9:6C:0F
	Battery   int    // 0-100，-1 表示读不到
	InstanceID string
}

var (
	// DEVPKEY_BLUETOOTH_BATTERY = {104EA319-6EE2-4701-BD47-8DDBF425BBE5}, pid=2
	devKeyBluetoothBattery = devPropKey{
		fmtid: windows.GUID{
			Data1: 0x104EA319,
			Data2: 0x6EE2,
			Data3: 0x4701,
			Data4: [8]byte{0xBD, 0x47, 0x8D, 0xDB, 0xF4, 0x25, 0xBB, 0xE5},
		},
		pid: 2,
	}

	// DEVPKEY_Device_FriendlyName = {A45C254E-DF1C-4EFD-8020-67D146A850E0}, pid=14
	devKeyFriendlyName = devPropKey{
		fmtid: windows.GUID{
			Data1: 0xA45C254E,
			Data2: 0xDF1C,
			Data3: 0x4EFD,
			Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0},
		},
		pid: 14,
	}

	setupapi                         = windows.NewLazySystemDLL("setupapi.dll")
	cfgMgr32                         = windows.NewLazySystemDLL("cfgmgr32.dll")
	procSetupDiGetClassDevs          = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo        = setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceInstanceID   = setupapi.NewProc("SetupDiGetDeviceInstanceIdW")
	procSetupDiDestroyDeviceInfoList = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procCMLocateDevNode              = cfgMgr32.NewProc("CM_Locate_DevNodeW")
	procCMGetDevNodeProperty         = cfgMgr32.NewProc("CM_Get_DevNode_PropertyW")
)

const (
	digcfPresent          = 0x00000002
	cmLocateDevNodeNormal = 0x00000000
	crSuccess             = 0
)

// ReadAll 枚举所有蓝牙设备并读电量。
// 只返回读到电量的设备（未连接的设备不报电量）。
func ReadAll() []Device {
	// GUID_DEVCLASS_SYSTEM = {4D36E97D-E325-11CE-BFC1-08002BE10318}
	classGuid := windows.GUID{
		Data1: 0x4D36E97D,
		Data2: 0xE325,
		Data3: 0x11CE,
		Data4: [8]byte{0xBF, 0xC1, 0x08, 0x00, 0x2B, 0xE1, 0x03, 0x18},
	}

	hDevInfo, _, _ := procSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&classGuid)), 0, 0, digcfPresent)
	if hDevInfo == 0 || hDevInfo == ^uintptr(0) {
		return nil
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hDevInfo)

	var out []Device
	var devInfo spDevInfoData
	devInfo.cbSize = uint32(unsafe.Sizeof(devInfo))
	index := uintptr(0)

	for {
		r, _, _ := procSetupDiEnumDeviceInfo.Call(hDevInfo, index, uintptr(unsafe.Pointer(&devInfo)))
		if r == 0 {
			break
		}
		index++

		buf := make([]uint16, 512)
		var reqSize uint32
		r2, _, _ := procSetupDiGetDeviceInstanceID.Call(
			hDevInfo, uintptr(unsafe.Pointer(&devInfo)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			uintptr(unsafe.Pointer(&reqSize)))
		if r2 == 0 {
			continue
		}
		instanceID := windows.UTF16ToString(buf)
		if !strings.Contains(strings.ToUpper(instanceID), "BTHENUM") {
			continue
		}

		idPtr, err := windows.UTF16PtrFromString(instanceID)
		if err != nil {
			continue
		}
		var devNode uint32
		cr, _, _ := procCMLocateDevNode.Call(
			uintptr(unsafe.Pointer(&devNode)),
			uintptr(unsafe.Pointer(idPtr)),
			cmLocateDevNodeNormal)
		if uint32(cr) != crSuccess {
			continue
		}

		var propType uint32
		var propValue byte
		propSize := uint32(1)
		cr2, _, _ := procCMGetDevNodeProperty.Call(
			uintptr(devNode),
			uintptr(unsafe.Pointer(&devKeyBluetoothBattery)),
			uintptr(unsafe.Pointer(&propType)),
			uintptr(unsafe.Pointer(&propValue)),
			uintptr(unsafe.Pointer(&propSize)),
			0)
		if uint32(cr2) != crSuccess {
			continue
		}

		battery := int(propValue)
		if battery > 100 {
			battery = 100
		}
		name := readFriendlyName(devNode)
		out = append(out, Device{
			Name:       cleanDeviceName(name),
			MAC:        extractMAC(instanceID),
			Battery:    battery,
			InstanceID: instanceID,
		})
	}
	return out
}

// ReadByMAC 读指定 MAC 的电量，读不到返回 -1
func ReadByMAC(mac string) int {
	target := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""))
	for _, d := range ReadAll() {
		got := strings.ToUpper(strings.ReplaceAll(d.MAC, ":", ""))
		if got == target {
			return d.Battery
		}
	}
	return -1
}

// cleanDeviceName 把服务节点名清理成纯设备名
// 例如 "LE208 Hands-Free AG" → "LE208"，"LE208 Avrcp Transport" → "LE208"
func cleanDeviceName(name string) string {
	if name == "" {
		return ""
	}
	s := name
	// 去掉 Windows 加的服务后缀
	for _, suffix := range []string{
		" Hands-Free AG", " Hands-Free", " Avrcp Transport",
		" A2DP SNK", " A2DP", " Audio", " HFP", " HSP",
		" Bluetooth Device", " (RFCOMM)", " Peripheral Device",
	} {
		s = strings.TrimSuffix(s, suffix)
	}
	// 有些名字是 "耳机 (LE208)" 这种，取括号里
	if i := strings.Index(s, "("); i > 0 && strings.HasSuffix(s, ")") {
		inner := strings.TrimSuffix(s[i+1:], ")")
		if inner != "" {
			s = inner
		}
	}
	return strings.TrimSpace(s)
}

// readFriendlyName 读设备的 FriendlyName（DEVPKEY_Device_FriendlyName）
func readFriendlyName(devNode uint32) string {
	// 先问大小
	var propType uint32
	var propSize uint32
	procCMGetDevNodeProperty.Call(
		uintptr(devNode),
		uintptr(unsafe.Pointer(&devKeyFriendlyName)),
		uintptr(unsafe.Pointer(&propType)),
		0,
		uintptr(unsafe.Pointer(&propSize)),
		0)
	if propSize == 0 || propSize > 2048 {
		return ""
	}
	buf := make([]byte, propSize)
	cr, _, _ := procCMGetDevNodeProperty.Call(
		uintptr(devNode),
		uintptr(unsafe.Pointer(&devKeyFriendlyName)),
		uintptr(unsafe.Pointer(&propType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&propSize)),
		0)
	if uint32(cr) != crSuccess {
		return ""
	}
	// DEVPROP_TYPE_STRING = 0x12，内容是 UTF-16LE
	if propType == 0x12 {
		u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), propSize/2)
		return windows.UTF16ToString(u16)
	}
	return ""
}

// extractMAC 从 instanceID 提取 12 位 MAC
func extractMAC(instanceID string) string {
	upper := strings.ToUpper(instanceID)
	parts := strings.Split(upper, "&")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := parts[i]
		if idx := strings.Index(seg, "_"); idx >= 0 {
			seg = seg[:idx]
		}
		seg = strings.TrimSpace(seg)
		if len(seg) == 12 && isHex12(seg) {
			return formatMAC(seg)
		}
	}
	if idx := strings.Index(upper, "DEV_"); idx >= 0 {
		s := upper[idx+4:]
		if len(s) >= 12 && isHex12(s[:12]) {
			return formatMAC(s[:12])
		}
	}
	return ""
}

func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

func isHex12(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := 0; i < 12; i++ {
		if !isHexChar(s[i]) {
			return false
		}
	}
	return true
}

func formatMAC(m string) string {
	return m[0:2] + ":" + m[2:4] + ":" + m[4:6] + ":" + m[6:8] + ":" + m[8:10] + ":" + m[10:12]
}
