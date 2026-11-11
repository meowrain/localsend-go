package discovery

import (
	"strings"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
)

// normalizeDeviceType 把公告里的 deviceType 规范化成协议 7.1 的取值：
// 为空或未知一律回退 desktop，与官方实现一致；大小写与空白做归一。
//
// 该字段只影响对端 UI 图标（协议 7.1 明确写了「no difference in the protocol」），
// 所以缺失时不该丢弃整条公告，只做兜底即可。
func normalizeDeviceType(raw string) string {
	dt := strings.ToLower(strings.TrimSpace(raw))
	switch dt {
	case "mobile", "desktop", "web", "headless", "server":
		return dt
	}
	return shared.DefaultDeviceType
}
