package platform

import (
	"strings"
	"time"
)

var ztpStages = []string{"powered", "dhcp", "booturl", "identity", "lookup", "fetch", "config", "restart", "managed"}

func nextZTPStage(state string) string {
	for i, v := range ztpStages {
		if state == v && i+1 < len(ztpStages) {
			return ztpStages[i+1]
		}
	}
	return ""
}
func validZTPChannel(channel string) bool {
	return channel == "239" || channel == "143" || channel == "60-43"
}
func validZTPEvidence(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 300 && !strings.ContainsAny(value, "\r\n\x00")
}
func ztpRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	return time.Duration(10*(1<<(attempt-1))) * time.Second
}
