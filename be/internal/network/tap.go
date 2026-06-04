// Package network manages tap devices and bridge attachment for VMs.
package network

import (
	"crypto/sha1"
	"fmt"
	"net"
	"strings"

	"github.com/vishvananda/netlink"
)

// CreateTap creates (or reuses) a tap device named `name`, brings it up, and
// attaches it to bridge `bridgeName`. The MAC is set on the tap itself (purely
// cosmetic; the VM uses its own MAC).
//
// Idempotent: if a tap with the same name already exists and is already on
// the right bridge, no error is returned.
func CreateTap(name, bridgeName, mac string) error {
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("network: bridge %q not found: %w", bridgeName, err)
	}
	if _, ok := br.(*netlink.Bridge); !ok {
		return fmt.Errorf("network: %q is not a bridge", bridgeName)
	}

	la := netlink.NewLinkAttrs()
	la.Name = name
	tap := &netlink.Tuntap{
		LinkAttrs: la,
		Mode:      netlink.TUNTAP_MODE_TAP,
		Flags:     netlink.TUNTAP_DEFAULTS,
	}

	existing, err := netlink.LinkByName(name)
	if err == nil {
		// reuse
		if err := netlink.LinkSetMaster(existing, br.(*netlink.Bridge)); err != nil {
			return fmt.Errorf("network: attach existing tap %q to bridge: %w", name, err)
		}
		if err := netlink.LinkSetUp(existing); err != nil {
			return fmt.Errorf("network: set up existing tap %q: %w", name, err)
		}
		return nil
	}

	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("network: create tap %q: %w", name, err)
	}

	if mac != "" {
		hw, err := net.ParseMAC(mac)
		if err == nil {
			_ = netlink.LinkSetHardwareAddr(tap, hw)
		}
	}
	if err := netlink.LinkSetMaster(tap, br.(*netlink.Bridge)); err != nil {
		return fmt.Errorf("network: attach tap %q to bridge %q: %w", name, bridgeName, err)
	}
	if err := netlink.LinkSetUp(tap); err != nil {
		return fmt.Errorf("network: set up tap %q: %w", name, err)
	}
	return nil
}

// DeleteTap removes a tap device. Missing device is not an error.
func DeleteTap(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		// Treat any lookup error as "already gone" — keeps cleanup idempotent.
		return nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("network: delete tap %q: %w", name, err)
	}
	return nil
}

// DeriveTapName produces a stable tap device name from a VM ID.
// Linux interface names are limited to 15 characters, so we hash the ID
// and take a short hex prefix.
func DeriveTapName(prefix, vmID string) string {
	sum := sha1.Sum([]byte(vmID))
	hex := fmt.Sprintf("%x", sum[:])
	avail := 15 - len(prefix)
	if avail < 4 {
		avail = 4
	}
	if avail > len(hex) {
		avail = len(hex)
	}
	return prefix + hex[:avail]
}

// DeriveMAC produces a stable, locally-administered MAC address from a VM ID.
// `prefix` should be the first three octets, e.g. "AA:FC:00".
func DeriveMAC(prefix, vmID string) string {
	prefix = strings.TrimSpace(prefix)
	sum := sha1.Sum([]byte(vmID))
	return fmt.Sprintf("%s:%02X:%02X:%02X", prefix, sum[0], sum[1], sum[2])
}
