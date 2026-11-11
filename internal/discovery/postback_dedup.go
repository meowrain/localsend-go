package discovery

import (
	"sync"
	"time"
)

// postbackTTL 是同一条公告（同一来源 IP + 同一指纹）两次回发之间的最小间隔。
//
// 对端每 2 秒公告一次，而一次 register 回发最坏要等 httpTimeout×2（http 失败后
// 再试 https）。没有去重时，慢速网络里回发会持续堆积，也对对端造成无意义的压力。
// 10 秒足以覆盖「一条公告触发一次回发」的正常节奏，又不会漏掉对端重启后的重新发现
// （重启会换指纹 → 键不同 → 立即回发）。
const postbackTTL = 10 * time.Second

// postbackDeduper 记录最近回发过的对端，避免对同一条公告反复回发。
// 零值可用；内部用 map + 互斥锁，支持并发调用（回发在主循环的 goroutine 里）。
type postbackDeduper struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time // 便于单测注入时钟
	ttl  time.Duration
}

// allow 判断 key 现在是否该回发；返回 true 时同时记录本次时间。
func (d *postbackDeduper) allow(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if d.now != nil {
		now = d.now()
	}
	ttl := d.ttl
	if ttl <= 0 {
		ttl = postbackTTL
	}
	if d.last == nil {
		d.last = make(map[string]time.Time)
	}
	if last, ok := d.last[key]; ok && now.Sub(last) < ttl {
		return false
	}
	d.last[key] = now
	// 顺手清理过期条目：条目数与局域网设备数同阶，不需要更复杂的数据结构。
	for k, at := range d.last {
		if now.Sub(at) >= ttl {
			delete(d.last, k)
		}
	}
	return true
}

// postbackDedupKey 用来源 IP + 对端指纹标识一次公告。
func postbackDedupKey(ip, fingerprint string) string {
	return ip + "|" + fingerprint
}

// lastPostback 是全局去重表，replyToAnnouncement 与单测共用。
var lastPostback postbackDeduper
