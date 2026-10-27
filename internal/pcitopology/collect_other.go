//go:build !linux

package pcitopology

import "context"

func CollectSysfs(context.Context) ([]Node, []string) {
	return []Node{}, []string{"pci_sysfs_platform_unsupported"}
}
