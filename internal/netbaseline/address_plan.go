package netbaseline

import (
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"sort"
)

// These are imported customer decisions. The engine only applies the approved
// pool, allocation size and explicit index; it does not invent a network design.
type AddressPlanning struct {
	ManagementVRF string        `json:"management_vrf"`
	Pools         []AddressPool `json:"pools"`
	Rules         []AddressRule `json:"rules"`
}
type AddressPool struct {
	ID         string   `json:"id"`
	Scope      string   `json:"scope"`
	CIDR       string   `json:"cidr"`
	UnitPrefix int      `json:"unit_prefix"`
	VRF        string   `json:"vrf"`
	Reserved   []string `json:"reserved,omitempty"`
}
type AddressRule struct {
	ObjectUID  string `json:"object_uid"`
	PoolRef    string `json:"pool_ref"`
	UnitIndex  uint32 `json:"unit_index"`
	HostOffset uint32 `json:"host_offset"`
	PeerUID    string `json:"peer_uid,omitempty"`
}
type AddressOverride struct {
	ObjectUID string `json:"object_uid"`
	PoolRef   string `json:"pool_ref"`
	Address   string `json:"address"`
}
type AddressAssignment struct {
	PortName      string `json:"port_name,omitempty"`
	ObjectUID     string `json:"object_uid"`
	OwnerSwitchID string `json:"owner_switch_id,omitempty"`
	Serial        string `json:"serial,omitempty"`
	BatchRef      string `json:"batch_ref,omitempty"`
	PoolRef       string `json:"pool_ref"`
	Scope         string `json:"scope"`
	VRF           string `json:"vrf"`
	Address       string `json:"address"`
	CIDR          string `json:"cidr"`
	UnitCIDR      string `json:"unit_cidr"`
	PeerUID       string `json:"peer_uid,omitempty"`
	PeerAddress   string `json:"peer_address,omitempty"`
	Adjusted      bool   `json:"adjusted"`
}

var addressPoolName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)
var addressVRFName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,62}$`)
var addressObjectUID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidateAddressPlanning(plan AddressPlanning) error {
	if !addressVRFName.MatchString(plan.ManagementVRF) {
		return errors.New("approved management VRF is required")
	}
	if len(plan.Pools) < 1 || len(plan.Pools) > 128 || len(plan.Rules) < 1 || len(plan.Rules) > 5000 {
		return errors.New("address planning requires 1..128 approved pools and 1..5000 rules")
	}
	pools := map[string]AddressPool{}
	prefixes := map[string]netip.Prefix{}
	for _, pool := range plan.Pools {
		prefix, e := netip.ParsePrefix(pool.CIDR)
		if !addressPoolName.MatchString(pool.ID) || !addressVRFName.MatchString(pool.VRF) || e != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() || prefix.Addr().IsLoopback() || pool.UnitPrefix < prefix.Bits() || pool.UnitPrefix > prefix.Addr().BitLen() || len(pool.Reserved) > 128 {
			return errors.New("pool requires a canonical unicast CIDR, explicit VRF and valid allocation prefix")
		}
		if _, exists := pools[pool.ID]; exists {
			return errors.New("duplicate approved pool ID")
		}
		switch pool.Scope {
		case "loopback":
			if pool.UnitPrefix != prefix.Addr().BitLen() {
				return errors.New("loopback pools allocate individual host addresses")
			}
		case "p2p", "bgp_peer":
		default:
			return errors.New("planning extension supports loopback, p2p and bgp_peer; management remains owned by the saved ZTP plan")
		}
		for _, raw := range pool.Reserved {
			reserved, e := netip.ParsePrefix(raw)
			if e != nil || reserved != reserved.Masked() || reserved.Addr().BitLen() != prefix.Addr().BitLen() || !prefix.Contains(reserved.Addr()) || reserved.Bits() < prefix.Bits() {
				return errors.New("reserved ranges must be canonical prefixes entirely inside their pool")
			}
		}
		for id, other := range pools {
			op := prefixes[id]
			if other.VRF == pool.VRF && prefix.Overlaps(op) {
				// The same addresses may intentionally back loopback and BGP identities.
				// Different allocation domains cannot silently overlap in one VRF.
				if prefix != op || pool.UnitPrefix != other.UnitPrefix || pool.Scope == other.Scope {
					return errors.New("overlapping pools in a VRF require identical ranges/allocation units and distinct declared scopes")
				}
			}
		}
		pools[pool.ID] = pool
		prefixes[pool.ID] = prefix
	}
	seen := map[string]bool{}
	for _, rule := range plan.Rules {
		pool, ok := pools[rule.PoolRef]
		key := rule.PoolRef + "/" + rule.ObjectUID
		if !ok || !addressObjectUID.MatchString(rule.ObjectUID) || seen[key] {
			return errors.New("rules require one unique stable object/pool binding")
		}
		seen[key] = true
		if rule.PeerUID != "" && (!addressObjectUID.MatchString(rule.PeerUID) || rule.PeerUID == rule.ObjectUID) {
			return errors.New("peer references require a different stable object")
		}
		if pool.Scope == "loopback" && rule.PeerUID != "" || pool.Scope == "p2p" && rule.PeerUID == "" {
			return errors.New("loopback has no peer; p2p requires explicit reciprocal peer bindings")
		}
		if _, _, e := deriveApprovedAddress(prefixes[pool.ID], pool.UnitPrefix, rule.UnitIndex, rule.HostOffset); e != nil {
			return e
		}
	}
	return nil
}
func addressInteger(address netip.Addr) *big.Int {
	if address.Is4() {
		raw := address.As4()
		return new(big.Int).SetBytes(raw[:])
	}
	raw := address.As16()
	return new(big.Int).SetBytes(raw[:])
}
func integerAddress(value *big.Int, bits int) (netip.Addr, error) {
	if value.Sign() < 0 || value.BitLen() > bits {
		return netip.Addr{}, errors.New("address pool exhausted")
	}
	if bits == 32 {
		var raw [4]byte
		value.FillBytes(raw[:])
		return netip.AddrFrom4(raw), nil
	}
	var raw [16]byte
	value.FillBytes(raw[:])
	return netip.AddrFrom16(raw), nil
}
func deriveApprovedAddress(pool netip.Prefix, unitBits int, index, host uint32) (netip.Addr, netip.Prefix, error) {
	bits := pool.Addr().BitLen()
	hostBits := bits - unitBits
	offset := new(big.Int).Lsh(new(big.Int).SetUint64(uint64(index)), uint(hostBits))
	subnetInt := new(big.Int).Add(addressInteger(pool.Addr()), offset)
	subnet, e := integerAddress(subnetInt, bits)
	if e != nil || !pool.Contains(subnet) {
		return netip.Addr{}, netip.Prefix{}, errors.New("approved allocation index exceeds pool capacity")
	}
	block := netip.PrefixFrom(subnet, unitBits)
	address, e := integerAddress(new(big.Int).Add(subnetInt, new(big.Int).SetUint64(uint64(host))), bits)
	if e != nil || !block.Contains(address) {
		return netip.Addr{}, netip.Prefix{}, errors.New("approved host offset exceeds its allocation unit")
	}
	return address, block, nil
}
func BuildAddressPlan(plan AddressPlanning, overrides []AddressOverride) ([]AddressAssignment, error) {
	if e := ValidateAddressPlanning(plan); e != nil {
		return nil, e
	}
	if len(overrides) > 5000 {
		return nil, errors.New("too many manual address adjustments")
	}
	pools := map[string]AddressPool{}
	for _, pool := range plan.Pools {
		pools[pool.ID] = pool
	}
	adjustments := map[string]string{}
	for _, override := range overrides {
		key := override.PoolRef + "/" + override.ObjectUID
		if _, exists := adjustments[key]; exists {
			return nil, errors.New("duplicate manual adjustment")
		}
		adjustments[key] = override.Address
	}
	out := []AddressAssignment{}
	byKey := map[string]int{}
	used := map[string]AddressAssignment{}
	for _, rule := range plan.Rules {
		pool := pools[rule.PoolRef]
		prefix, _ := netip.ParsePrefix(pool.CIDR)
		address, unit, _ := deriveApprovedAddress(prefix, pool.UnitPrefix, rule.UnitIndex, rule.HostOffset)
		item := AddressAssignment{ObjectUID: rule.ObjectUID, PoolRef: pool.ID, Scope: pool.Scope, VRF: pool.VRF, PeerUID: rule.PeerUID}
		key := pool.ID + "/" + rule.ObjectUID
		if raw, ok := adjustments[key]; ok {
			override, e := netip.ParseAddr(raw)
			if e != nil || override.Is4In6() || override.String() != raw || override.BitLen() != address.BitLen() || !prefix.Contains(override) {
				return nil, errors.New("manual adjustment must be a canonical address inside its approved pool")
			}
			item.Adjusted = override != address
			address = override
			unit = netip.PrefixFrom(address, pool.UnitPrefix).Masked()
			delete(adjustments, key)
		}
		if address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() {
			return nil, errors.New("unicast assignment required")
		}
		if address.Is4() && unit.Bits() <= 30 {
			last := new(big.Int).Add(addressInteger(unit.Addr()), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(32-unit.Bits())), big.NewInt(1)))
			if address == unit.Addr() || addressInteger(address).Cmp(last) == 0 {
				return nil, errors.New("IPv4 network/broadcast addresses are not usable host allocations")
			}
		}
		for _, raw := range pool.Reserved {
			reserved, _ := netip.ParsePrefix(raw)
			if reserved.Contains(address) {
				return nil, errors.New("assignment uses an approved reserved address range")
			}
		}
		item.Address = address.String()
		item.CIDR = netip.PrefixFrom(address, pool.UnitPrefix).String()
		item.UnitCIDR = unit.String()
		addressKey := pool.VRF + "/" + item.Address
		if other, exists := used[addressKey]; exists && (other.ObjectUID != item.ObjectUID || other.UnitCIDR != item.UnitCIDR) {
			return nil, errors.New("duplicate address in an approved VRF")
		}
		used[addressKey] = item
		byKey[key] = len(out)
		out = append(out, item)
	}
	if len(adjustments) > 0 {
		return nil, errors.New("manual adjustment does not reference an approved object/pool rule")
	}
	for i := range out {
		item := &out[i]
		if item.PeerUID == "" {
			continue
		}
		index, exists := byKey[item.PoolRef+"/"+item.PeerUID]
		if !exists {
			return nil, errors.New("approved peer has no matching allocation")
		}
		peer := out[index]
		if peer.PeerUID != item.ObjectUID || peer.Address == item.Address || (item.Scope == "p2p" && peer.UnitCIDR != item.UnitCIDR) {
			return nil, fmt.Errorf("non-reciprocal or mismatched peer allocation in pool %s", item.PoolRef)
		}
		item.PeerAddress = peer.Address
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PoolRef != b.PoolRef {
			return a.PoolRef < b.PoolRef
		}
		return a.ObjectUID < b.ObjectUID
	})
	return out, nil
}
