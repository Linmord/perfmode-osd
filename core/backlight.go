// 键盘背光控制。
//
// EC[0x9A] = 背光档位，Fn+F7 循环 0/1/2/3。
// 通过按键前后 EC RAM 快照对照实测确认（2026-11-21）：
// 0=关 1=暗 2=中 3=亮，写入后固件立即接受并回读一致。
// 注意：旧实现误写 EC[0xE2]（固件维护的状态字节，恒为 0），故无效。
package core

import "fmt"

// KBBLOffset 是键盘背光档位字段在 EC 中的偏移
const KBBLOffset = 0x9A

// BacklightLevels 是背光档位数量（0..3）
const BacklightLevels = 4

// BacklightNames 是各档位名称
var BacklightNames = [4]string{"关", "暗", "中", "亮"}

// ReadBacklight 读取当前背光档位
func ReadBacklight(ec *ECDirect) (byte, error) {
	return ec.ReadStable(KBBLOffset)
}

// SetBacklight 设置背光档位，写后连续回读确认（固件可能因按键覆盖）
func SetBacklight(ec *ECDirect, level byte) error {
	if level >= BacklightLevels {
		return fmt.Errorf("背光档位必须在 0..%d 之间", BacklightLevels-1)
	}
	for i := 0; i < 4; i++ {
		if err := ec.Write(KBBLOffset, level); err != nil {
			return err
		}
		got, err := ec.ReadStable(KBBLOffset)
		if err == nil && got == level {
			return nil
		}
	}
	return fmt.Errorf("背光写入 %d 未生效（可能被 Fn 键覆盖）", level)
}
