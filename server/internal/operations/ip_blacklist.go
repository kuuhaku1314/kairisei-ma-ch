package operations

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

const MaxIPBlacklistEntries = 512

type IPBlacklist struct {
	entries  []string
	prefixes []netip.Prefix
}

func CompileIPBlacklist(entries []string) (IPBlacklist, error) {
	if len(entries) > MaxIPBlacklistEntries {
		return IPBlacklist{}, fmt.Errorf("IP 黑名单最多 %d 项", MaxIPBlacklistEntries)
	}
	var result IPBlacklist
	seen := make(map[netip.Prefix]struct{}, len(entries))
	for i, entry := range entries {
		prefix, err := blacklistPrefix(strings.TrimSpace(entry))
		if err != nil {
			return IPBlacklist{}, fmt.Errorf("IP 黑名单第 %d 项须为 IPv4、IPv6 或 CIDR 网段", i+1)
		}
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		label := prefix.String()
		if prefix.IsSingleIP() {
			label = prefix.Addr().String()
		}
		result.entries = append(result.entries, label)
		result.prefixes = append(result.prefixes, prefix)
	}
	return result, nil
}

func blacklistPrefix(value string) (netip.Prefix, error) {
	if addr, err := netip.ParseAddr(value); err == nil && addr.Zone() == "" {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return netip.Prefix{}, errors.New("invalid IPv4-mapped prefix length")
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked(), nil
}

func (policy IPBlacklist) Entries() []string {
	if len(policy.entries) == 0 {
		return []string{}
	}
	return slices.Clone(policy.entries)
}

// IPBlocked reads the published prefixes and the actual socket peer.
func (runtime *Runtime) IPBlocked(remoteAddr string) bool {
	prefixes := runtime.current.Load().IPBlacklist.prefixes
	if len(prefixes) == 0 {
		return false
	}
	peer, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return false
	}
	addr := peer.Addr().Unmap().WithZone("")
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
