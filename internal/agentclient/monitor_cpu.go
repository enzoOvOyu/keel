package agentclient

import "strconv"
import "strings"

type monitorCPUCounter struct {
	total, idle uint64
	initialized bool
}

// Guest time is already included in user/nice; only the first eight counters
// contribute to total. A first sample, reset or malformed line remains unknown.
func (c *monitorCPUCounter) observe(raw []byte) (float64, bool) {
	lines := strings.SplitN(string(raw), "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		return 0, false
	}
	var total, idle uint64
	for i := 1; i <= 8; i++ {
		v, e := strconv.ParseUint(fields[i], 10, 64)
		if e != nil || total+v < total {
			return 0, false
		}
		total += v
		if i == 4 || i == 5 {
			if idle+v < idle {
				return 0, false
			}
			idle += v
		}
	}
	oldTotal, oldIdle, initialized := c.total, c.idle, c.initialized
	c.total, c.idle, c.initialized = total, idle, true
	if !initialized || total <= oldTotal || idle < oldIdle || idle-oldIdle > total-oldTotal {
		return 0, false
	}
	return 100 * float64((total-oldTotal)-(idle-oldIdle)) / float64(total-oldTotal), true
}
