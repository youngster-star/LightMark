// Package autostart 管理开机自启动（注册表 HKCU Run 键，无需管理员权限）。
package autostart

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "LightMark"
)

// SetAutoStart 开启或关闭开机自启动。
func SetAutoStart(enable bool, exePath string) error {
	if exePath == "" {
		return fmt.Errorf("可执行文件路径为空")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表失败: %w", err)
	}
	defer k.Close()

	if enable {
		// 路径含空格时加引号
		if strings.Contains(exePath, " ") {
			exePath = `"` + exePath + `"`
		}
		if err := k.SetStringValue(valueName, exePath); err != nil {
			return fmt.Errorf("写入注册表失败: %w", err)
		}
		return nil
	}

	// 关闭：仅当值存在时删除
	if _, _, err := k.GetStringValue(valueName); err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("读取注册表失败: %w", err)
	}
	if err := k.DeleteValue(valueName); err != nil {
		return fmt.Errorf("删除注册表失败: %w", err)
	}
	return nil
}

// IsAutoStartEnabled 返回开机自启动是否已开启。
func IsAutoStartEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("打开注册表失败: %w", err)
	}
	defer k.Close()

	_, _, err = k.GetStringValue(valueName)
	if err == nil {
		return true, nil
	}
	if err == registry.ErrNotExist {
		return false, nil
	}
	return false, fmt.Errorf("读取注册表失败: %w", err)
}
