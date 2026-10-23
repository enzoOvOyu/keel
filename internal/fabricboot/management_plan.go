package fabricboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

type ManagementRules struct {
	CIDR         string `json:"cidr"`
	FirstAddress string `json:"first_address"`
	Gateway      string `json:"gateway"`
	BatchPrefix  string `json:"batch_prefix"`
	BatchBy      string `json:"batch_by"`
}
type ManagementDevice struct {
	ID     string `json:"switch_id"`
	Serial string `json:"serial_number"`
	DC     string `json:"dc"`
	Pod    string `json:"pod"`
	Role   string `json:"role"`
	Rack   string `json:"rack"`
	Index  string `json:"idx"`
}
type ManagementAssignment struct {
	SwitchID string `json:"switch_id"`
	Serial   string `json:"serial_number"`
	Hostname string `json:"hostname"`
	Address  string `json:"management_ip"`
	Gateway  string `json:"gateway"`
	Batch    string `json:"batch_ref"`
}

var hostnamePart = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var batchPrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,39}$`)

func ValidateManagementRules(r ManagementRules) error {
	p, e := netip.ParsePrefix(r.CIDR)
	if e != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() > 30 || p.Bits() < 8 {
		return errors.New("management CIDR must be a canonical IPv4 /8 through /30 pool")
	}
	first, e := netip.ParseAddr(r.FirstAddress)
	if e != nil || !usableManagementIP(p, first) {
		return errors.New("first address must be a usable host in the management pool")
	}
	gateway, e := netip.ParseAddr(r.Gateway)
	if e != nil || !usableManagementIP(p, gateway) {
		return errors.New("gateway must be a usable host in the management pool")
	}
	if !batchPrefixPattern.MatchString(r.BatchPrefix) || r.BatchBy != "rack" && r.BatchBy != "pod" {
		return errors.New("approved batch prefix and rack/pod grouping are required")
	}
	return nil
}
func usableManagementIP(p netip.Prefix, a netip.Addr) bool {
	if !a.Is4() || !p.Contains(a) || a == p.Addr() {
		return false
	}
	next := a.Next()
	return next.IsValid() && p.Contains(next)
}

// Execute approved rules only. Existing bindings reserve addresses before new
// devices are allocated; adding a BOM row cannot renumber existing switches.
// No overrides are accepted: they require a separate approved change workflow.
func BuildManagementPlan(r ManagementRules, devices []ManagementDevice, existing []ManagementAssignment, batchSize int) ([]ManagementAssignment, error) {
	if err := ValidateManagementRules(r); err != nil {
		return nil, err
	}
	if batchSize < 1 || batchSize > 500 {
		return nil, errors.New("fabric.batch_size must be between 1 and 500")
	}
	if len(devices) == 0 {
		return nil, errors.New("project BOM contains no active switches")
	}
	pool, _ := netip.ParsePrefix(r.CIDR)
	next, _ := netip.ParseAddr(r.FirstAddress)
	saved := map[string]ManagementAssignment{}
	used := map[string]bool{r.Gateway: true}
	names := map[string]string{}
	for _, a := range existing {
		ip, e := netip.ParseAddr(a.Address)
		if e != nil || !usableManagementIP(pool, ip) || used[a.Address] || saved[a.SwitchID].SwitchID != "" || a.Gateway != r.Gateway {
			return nil, errors.New("existing management allocation conflicts with approved rules")
		}
		saved[a.SwitchID] = a
		used[a.Address] = true
	}
	input := append([]ManagementDevice(nil), devices...)
	sort.Slice(input, func(i, j int) bool {
		a, b := input[i], input[j]
		return strings.ToLower(strings.Join([]string{a.DC, a.Pod, a.Role, a.Rack, a.Index}, "/"))+"/"+a.Serial+"/"+a.ID < strings.ToLower(strings.Join([]string{b.DC, b.Pod, b.Role, b.Rack, b.Index}, "/"))+"/"+b.Serial+"/"+b.ID
	})
	groups := map[string]int{}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, old := range existing {
		counts[old.Batch]++
		names[old.Hostname] = old.SwitchID
		if counts[old.Batch] > batchSize {
			return nil, errors.New("existing batch exceeds fabric.batch_size")
		}
	}
	out := make([]ManagementAssignment, 0, len(input))
	for _, d := range input {
		if d.ID == "" || d.Serial == "" || seen[d.ID] {
			return nil, errors.New("switch identity is missing or duplicated")
		}
		seen[d.ID] = true
		parts := []string{strings.ToLower(d.DC), strings.ToLower(d.Pod), strings.ToLower(d.Role), strings.ToLower(d.Rack), strings.ToLower(d.Index)}
		for _, part := range parts {
			if !hostnamePart.MatchString(part) {
				return nil, fmt.Errorf("switch %s has an invalid hostname component; use lowercase letters, digits and hyphens", d.Serial)
			}
		}
		host := strings.Join(parts, "-")
		if len(host) > 63 || (names[host] != "" && names[host] != d.ID) {
			return nil, errors.New("generated hostname exceeds 63 characters or is duplicated")
		}
		names[host] = d.ID
		if old, ok := saved[d.ID]; ok {
			if old.Hostname != host || old.Serial != d.Serial || !strings.HasPrefix(old.Batch, r.BatchPrefix+"-") {
				return nil, errors.New("existing binding requires an approved change; automatic reassignment is forbidden")
			}
			out = append(out, old)
			continue
		}
		for usableManagementIP(pool, next) && used[next.String()] {
			next = next.Next()
		}
		if !usableManagementIP(pool, next) {
			return nil, errors.New("management address pool exhausted")
		}
		group := parts[0] + "-" + parts[1]
		if r.BatchBy == "rack" {
			group += "-" + parts[3]
		}
		batch := ""
		for {
			batch = fmt.Sprintf("%s-%s-%03d", r.BatchPrefix, group, groups[group]+1)
			if len(batch) > 160 {
				return nil, errors.New("generated batch reference exceeds 160 characters")
			}
			if counts[batch] < batchSize {
				break
			}
			groups[group]++
		}
		out = append(out, ManagementAssignment{SwitchID: d.ID, Serial: d.Serial, Hostname: host, Address: next.String(), Gateway: r.Gateway, Batch: batch})
		used[next.String()] = true
		counts[batch]++
		next = next.Next()
	}
	return out, nil
}
func ManagementPlanDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
