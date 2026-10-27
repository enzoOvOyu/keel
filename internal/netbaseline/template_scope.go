package netbaseline

import (
	"errors"
	"sort"
)

// Device types reuse the stable DeviceModel registry. Domains classify the
// imported complete intent; tags never cause an unreviewed fragment merge.
type TemplateScope struct {
	DeviceModelIDs []string `json:"device_model_ids"`
	Roles          []string `json:"roles"`
	Domains        []string `json:"domains"`
}

func (s TemplateScope) Validate() error {
	if len(s.DeviceModelIDs) < 1 || len(s.DeviceModelIDs) > 64 || len(s.Roles) < 1 || len(s.Roles) > 4 || len(s.Domains) < 1 || len(s.Domains) > 7 {
		return errors.New("template scope requires 1..64 device models, 1..4 roles and 1..7 configuration domains")
	}
	models, roles, domains := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, id := range s.DeviceModelIDs {
		if !addressObjectUID.MatchString(id) || models[id] {
			return errors.New("template scope requires unique stable device model IDs")
		}
		models[id] = true
	}
	for _, role := range s.Roles {
		switch role {
		case "leaf", "spine", "superspine", "mgmt":
		default:
			return errors.New("invalid network template role")
		}
		if roles[role] {
			return errors.New("duplicate network template role")
		}
		roles[role] = true
	}
	for _, domain := range s.Domains {
		switch domain {
		case "base", "ports", "mlag", "bgp_evpn", "rocev2", "acl", "system":
		default:
			return errors.New("invalid network configuration domain")
		}
		if domains[domain] {
			return errors.New("duplicate network template domain")
		}
		domains[domain] = true
	}
	return nil
}
func (s TemplateScope) Canonical() *TemplateScope {
	out := &TemplateScope{DeviceModelIDs: append([]string(nil), s.DeviceModelIDs...), Roles: append([]string(nil), s.Roles...), Domains: append([]string(nil), s.Domains...)}
	sort.Strings(out.DeviceModelIDs)
	sort.Strings(out.Roles)
	sort.Strings(out.Domains)
	return out
}
func (s TemplateScope) Matches(model, role string) bool {
	foundModel, foundRole := false, false
	for _, v := range s.DeviceModelIDs {
		foundModel = foundModel || v == model
	}
	for _, v := range s.Roles {
		foundRole = foundRole || v == role
	}
	return foundModel && foundRole
}
