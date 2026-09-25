// Package hotkey 封装 Windows 全局热键的注册与监听。
// 基于 user32 的 RegisterHotKey/GetMessage 实现，无需额外依赖。
package hotkey

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Windows 常量：修饰键、虚拟键码与消息。
const (
	// MOD_ALT Alt 修饰键
	MOD_ALT = 0x0001
	// MOD_CONTROL Ctrl 修饰键
	MOD_CONTROL = 0x0002
	// VK_D 字母 D 的虚拟键码
	VK_D = 0x44
	// WM_HOTKEY 热键触发消息
	WM_HOTKEY = 0x0312
	// WM_QUIT 消息循环退出消息
	WM_QUIT = 0x0012
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	user32                 = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

// msg 对应 Windows MSG 结构体的子集，满足消息循环需要。
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX, ptY int32
}

// Handle 一个已注册热键的句柄，Stop 用于注销热键并结束监听线程。
type Handle struct {
	once sync.Once
	stop func()
}

// Stop 注销热键并结束监听线程，可安全重复调用。
func (h *Handle) Stop() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		if h.stop != nil {
			h.stop()
		}
	})
}

// regResult 注册结果：Handle 与错误通过 channel 从监听线程传回调用方。
type regResult struct {
	h   *Handle
	err error
}

// Register 注册全局热键（mods 为 MOD_* 组合，vk 为虚拟键码）。
// 注册成功后在锁定的 OS 线程上运行消息循环，热键触发时异步回调 onTrigger。
// 返回 Handle 用于注销；注册失败（如热键被占用）返回错误。
func Register(mods, vk uint32, onTrigger func()) (*Handle, error) {
	if onTrigger == nil {
		return nil, fmt.Errorf("热键回调不能为空")
	}

	resCh := make(chan regResult, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// id=1 的进程内热键标识；hwnd=0 表示线程级热键
		const hotkeyID = 1
		ret, _, callErr := procRegisterHotKey.Call(0, hotkeyID, uintptr(mods), uintptr(vk))
		if ret == 0 {
			resCh <- regResult{err: fmt.Errorf("注册热键失败（可能已被其他程序占用）: %v", callErr)}
			return
		}

		// 记录线程 ID，Stop 时向该线程投递 WM_QUIT 结束消息循环
		tid, _, _ := procGetCurrentThreadId.Call()
		done := make(chan struct{})
		h := &Handle{
			// 仅投递退出消息：线程级热键必须在注册线程上注销，
			// 实际 UnregisterHotKey 在消息循环退出后于该线程内执行，
			// done 关闭前 stop 会阻塞等待，保证 Stop 返回时热键已注销
			stop: func() {
				procPostThreadMessageW.Call(tid, WM_QUIT, 0, 0)
				<-done
			},
		}
		resCh <- regResult{h: h}

		// 消息循环：WM_HOTKEY → 异步回调（避免阻塞消息泵）；GetMessage 返回 0/-1 时退出
		var m msg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			if m.message == WM_HOTKEY {
				go onTrigger()
			}
		}
		// 在注册线程上注销热键，保证后续可重新注册
		procUnregisterHotKey.Call(0, hotkeyID)
		close(done)
	}()

	res := <-resCh
	if res.err != nil {
		return nil, res.err
	}
	return res.h, nil
}

// RegisterAltD 注册 Alt+D 全局热键，用于从任意界面唤出主窗口。
func RegisterAltD(onTrigger func()) (*Handle, error) {
	return Register(MOD_ALT, VK_D, onTrigger)
}
