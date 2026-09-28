// Package singleinstance 实现应用单实例约束（Windows）。
//
// 原理：
//  1. 以命名互斥体（Global\LightMark_SingleInstance）作为跨进程"实例锁"。
//     首个实例创建互斥体成功，后续实例 CreateMutex 后 GetLastError 返回
//     ERROR_ALREADY_EXISTS，据此判定已有实例在运行。
//  2. 首个实例额外创建一扇隐藏的消息窗口，并运行消息循环。后续实例通过
//     FindWindow 找到该窗口，向其投递自定义激活消息（WM_LIGHTMARK_ACTIVATE），
//     由首个实例响应该消息唤起并聚焦主窗口，随后新实例直接退出。
//
// 由于 LightMark 常驻系统托盘（关闭窗口不退出进程），用户重复双击图标是
// 高频操作，此机制保证：同一时刻至多一个进程在运行，重复启动会复用已有
// 进程并唤出主窗口，而非再开一份。
package singleinstance

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	mutexName    = "Global\\LightMark_SingleInstance"
	windowName   = "LightMark_SingleInstance_Window"
	windowClass  = "LightMark_SingleInstance_Class"
	activateMsg  = 0x8000 + 1 // WM_APP+1，自定义激活消息
	errorAlready = 183         // ERROR_ALREADY_EXISTS
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procReleaseMutex     = kernel32.NewProc("ReleaseMutex")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
	procRegisterClassW   = user32.NewProc("RegisterClassW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
)

// WM_QUIT 消息值（消息循环退出条件）。
const wmQuit = 0x0012

// msg Windows MSG 结构体子集，满足消息循环需要。
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX, ptY int32
}

// Single 单实例句柄，持有互斥体句柄与消息窗口句柄。
type Single struct {
	mutex   syscall.Handle
	hwnd    uintptr
	doneCh  chan struct{}
	started bool
}

// Acquire 尝试获取单实例锁，并返回是否为首个实例。
//
//   - 返回 (true, s, nil)：本进程是首个实例，s 为有效句柄，应继续正常启动，
//     并通过 s.Wait(onActivate) 进入消息循环响应后续实例的激活请求。
//   - 返回 (false, nil, nil)：已有实例在运行。此时本进程应唤起已有实例并退出。
//
// 失败（返回 error）通常意味着系统调用异常，调用方可按正常启动降级处理。
func Acquire() (bool, *Single, error) {
	mutexNamePtr, _ := syscall.UTF16PtrFromString(mutexName)
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexNamePtr)))
	if h == 0 {
		return false, nil, fmt.Errorf("创建互斥体失败: %v", callErr)
	}
	mutex := syscall.Handle(h)

	// CreateMutexW 成功但已存在时，Call 的第三个返回值 err 为 Errno(183)=
	// ERROR_ALREADY_EXISTS。注意：不能手动调 GetLastError，Go 的 Call 会
	// 将其消费并封装进 err 返回值。
	if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == errorAlready {
		// 已有实例：本进程只负责"唤起"，关闭互斥体句柄后立即返回
		procCloseHandle.Call(uintptr(mutex))
		return false, nil, nil
	}

	s := &Single{
		mutex:  mutex,
		doneCh: make(chan struct{}),
	}
	return true, s, nil
}

// NotifyExisting 向已运行实例的隐藏消息窗口投递激活消息，唤出其主窗口。
// 用于非首个实例进程在退出前唤起已有实例。找不到窗口时静默返回。
func NotifyExisting() {
	winNamePtr, _ := syscall.UTF16PtrFromString(windowName)
	classPtr, _ := syscall.UTF16PtrFromString(windowClass)
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(winNamePtr)))
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(hwnd, activateMsg, 0, 0)
}

// Wait 在独立线程运行消息循环，响应后续实例的激活请求。
// 首个实例启动后应调用本方法；onActivate 在收到激活消息时被异步回调，
// 用于唤起并聚焦主窗口。Release 被调用后消息循环退出。
func (s *Single) Wait(onActivate func()) error {
	if s == nil || s.started {
		return nil
	}
	if onActivate == nil {
		return fmt.Errorf("激活回调不能为空")
	}
	onActivateFn = onActivate

	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// 注册窗口类（带窗口过程，处理激活消息）
		classNamePtr, _ := syscall.UTF16PtrFromString(windowClass)
		var wndProc uintptr = syscall.NewCallback(windowProc)
		wc := &wndClassW{
			style:    0,
			lpfnWndProc: wndProc,
			cbClsExtra: 0,
			cbWndExtra: 0,
			hInstance: 0,
			hIcon:     0,
			hCursor:   0,
			hbrBackground: 0,
			lpszMenuName:  0,
			lpszClassName: uintptr(unsafe.Pointer(classNamePtr)),
		}
		if ret, _, regErr := procRegisterClassW.Call(uintptr(unsafe.Pointer(wc))); ret == 0 {
			// 1410 = ERROR_CLASS_ALREADY_EXISTS，同名类已存在可忽略
			if errno, ok := regErr.(syscall.Errno); !ok || uintptr(errno) != 1410 {
				errCh <- fmt.Errorf("注册窗口类失败: %v", regErr)
				close(s.doneCh)
				return
			}
		}

		// 创建隐藏消息窗口
		winNamePtr, _ := syscall.UTF16PtrFromString(windowName)
		hwnd, _, callErr := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(classNamePtr)),
			uintptr(unsafe.Pointer(winNamePtr)),
			0, // WS_OVERLAPPED 无可见样式
			0, 0, 0, 0,
			0, 0, 0, 0,
		)
		if hwnd == 0 {
			errCh <- fmt.Errorf("创建消息窗口失败: %v", callErr)
			close(s.doneCh)
			return
		}
		s.hwnd = hwnd
		errCh <- nil

		// 消息循环：GetMessageW 返回 <=0（收到 WM_QUIT）即退出
		var m msg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		close(s.doneCh)
	}()

	if err := <-errCh; err != nil {
		return err
	}
	s.started = true
	return nil
}

// Release 释放单实例资源：向消息窗口投递 WM_QUIT 结束消息循环，并释放互斥体。
// 应在应用 shutdown 阶段调用，且可安全重复调用。
func (s *Single) Release() {
	if s == nil {
		return
	}
	if s.hwnd != 0 {
		procPostMessageW.Call(s.hwnd, wmQuit, 0, 0)
		s.hwnd = 0
	}
	if s.doneCh != nil {
		<-s.doneCh
		s.doneCh = nil
	}
	if s.mutex != 0 {
		procReleaseMutex.Call(uintptr(s.mutex))
		procCloseHandle.Call(uintptr(s.mutex))
		s.mutex = 0
	}
}

// wndClassW Windows WNDCLASSW 结构体。
type wndClassW struct {
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
}

// 激活回调的全局引用：窗口过程为 C 回调，需经由包级变量转发到 App 的方法。
var onActivateFn func()

// windowProc 消息窗口过程：收到激活消息时异步调用 onActivateFn。
func windowProc(hwnd, uMsg, wParam, lParam uintptr) uintptr {
	if uMsg == activateMsg {
		if onActivateFn != nil {
			go onActivateFn()
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uMsg, wParam, lParam)
	return ret
}
