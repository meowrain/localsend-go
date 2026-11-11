package handlers

import (
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/config"
)

// PIN 失败计数的窗口与上限。
//
// 依据：官方 LocalSend 的接收端（packages/core/src/http/server/common/pin.rs）
// 用 MAX_PIN_ATTEMPTS = 3 的按 IP 计数，没有时间窗口——一旦失败满 3 次，
// 除非本机重启或该 IP 命中 LRU 淘汰，否则永远 429。那种做法对共享 NAT / 家庭
// 路由器后面的用户很不友好（一个人输错，全屋被锁），所以这里保留 3 次的量级，
// 但加上 10 分钟的滑动窗口：窗口内失败满 pinMaxFailures 次才 429，
// 窗口过去后计数自动归零，用户不用重启发送端就能再试。
//
// 3 次而不是 5 次：本机 PIN 是 6 位数字（10^6 空间），3 次/10 分钟的速率意味着
// 猜中的期望时间远超任何一次对话的长度；同时留给手滑用户的余量也够。
const (
	pinMaxFailures = 3
	pinFailWindow  = 10 * time.Minute
	// pinAttemptMaxEntries 限制计数表条目数：上限满了先清过期项，仍满就整体清零。
	// 不引入 LRU 依赖——被大量 IP 扫描时，整体清零只会让攻击者多得几次机会，
	// 远好过内存被撑爆或引入一个只为这件事存在的第三方库。
	pinAttemptMaxEntries = 1024
)

// pinAttempt 记录单个来源 IP 的失败情况。
type pinAttempt struct {
	failures int
	resetAt  time.Time
}

// pinLimiter 按来源 IP 统计 PIN 失败次数。
type pinLimiter struct {
	mu  sync.Mutex
	m   map[string]*pinAttempt
	now func() time.Time // 测试注入
}

var pinGuard = &pinLimiter{m: make(map[string]*pinAttempt), now: time.Now}

// requiredPIN 返回当前生效的 PIN，空串表示不启用。
// 只读 config 包的全局值，不缓存：--pin 在启动阶段写入，之后不会变。
func requiredPIN() string {
	return config.ConfigData.Functions.PIN
}

// tooMany 判断该 IP 是否已被锁定（窗口内失败次数达到上限）。
func (l *pinLimiter) tooMany(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return false
	}
	if !l.now().Before(a.resetAt) {
		delete(l.m, ip)
		return false
	}
	return a.failures >= pinMaxFailures
}

// fail 记录一次失败，返回累加后的次数。窗口过期时重新开始计。
func (l *pinLimiter) fail(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a, ok := l.m[ip]
	if !ok || !now.Before(a.resetAt) {
		if !ok && len(l.m) >= pinAttemptMaxEntries {
			l.evictLocked(now)
		}
		a = &pinAttempt{resetAt: now.Add(pinFailWindow)}
		l.m[ip] = a
	}
	a.failures++
	return a.failures
}

// evictLocked 在插入新条目前腾空间：先删过期项，仍然满就整体重置。
func (l *pinLimiter) evictLocked(now time.Time) {
	for ip, a := range l.m {
		if !now.Before(a.resetAt) {
			delete(l.m, ip)
		}
	}
	if len(l.m) >= pinAttemptMaxEntries {
		l.m = make(map[string]*pinAttempt)
	}
}

// reset 在 PIN 校验成功后清零该 IP 的计数。
func (l *pinLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}

// checkPIN 按协议 4.1 校验 prepare-upload 的 pin 查询参数。
// 返回 true 表示放行；否则 status/msg 就是要返回给发送端的响应。
func checkPIN(ip, given string) (ok bool, status int, msg string) {
	return checkPINWithLimit(requiredPIN(), ip, given)
}

// checkPINWithLimit 是 checkPIN 的实现，期望值由调用方给出，便于参数化测试。
func checkPINWithLimit(want, ip, given string) (ok bool, status int, msg string) {
	if want == "" {
		return true, 0, ""
	}
	if pinGuard.tooMany(ip) {
		return false, statusTooManyRequests, "too many requests"
	}
	// 完全没带 pin（或只有空白）说明对端还没开始猜：官方只对「pin 错误」计数，
	// 这里也放过它，否则老版本发送端一次 prepare 就会消耗掉本机用户的尝试次数。
	if strings.TrimSpace(given) == "" {
		return false, statusPINRequired, "PIN required"
	}
	// 长度不同直接失败；相同时用常量时间比较，避免逐字节比较泄漏 PIN 内容。
	if len(given) != len(want) || subtle.ConstantTimeCompare([]byte(given), []byte(want)) != 1 {
		pinGuard.fail(ip)
		return false, statusInvalidPIN, "invalid PIN"
	}
	pinGuard.reset(ip)
	return true, 0, ""
}
