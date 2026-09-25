package hotkey

import (
	"testing"
	"time"
)

// TestRegisterRoundTrip 用冷门组合键验证注册→注销→重复注册链路可用，
// 避免与用户已占用的 Alt+D 冲突。无桌面会话环境下该测试可能失败，属预期。
func TestRegisterRoundTrip(t *testing.T) {
	// Ctrl+Alt+K（VK 0x4B）足够冷门
	h, err := Register(MOD_CONTROL|MOD_ALT, 0x4B, func() {})
	if err != nil {
		t.Fatalf("注册热键失败: %v", err)
	}
	if h == nil || h.stop == nil {
		t.Fatal("注册成功但 Handle 为空")
	}
	h.Stop()
	time.Sleep(100 * time.Millisecond)

	// 注销后应能再次注册同一组合键
	if _, err := Register(MOD_CONTROL|MOD_ALT, 0x4B, func() {}); err != nil {
		t.Fatalf("注销后重新注册失败: %v", err)
	}
	h.Stop()
}

// TestRegisterNilCallback 空回调应报错，不注册任何系统资源。
func TestRegisterNilCallback(t *testing.T) {
	if _, err := Register(MOD_ALT, VK_D, nil); err == nil {
		t.Fatal("空回调应返回错误")
	}
}
