//go:build linux && !android

package core

import (
	"net/netip"

	"github.com/sagernet/nftables"
	"github.com/sagernet/nftables/expr"
	"golang.org/x/sys/unix"
)

// Linux: a dedicated nftables table with an output chain whose policy is drop.
// Needs CAP_NET_ADMIN (the .deb grants it to the binary).
const ksTable = "tainavpn_killswitch"

func EnableKillSwitch(opts KillSwitchOptions) error {
	c, err := nftables.New()
	if err != nil {
		return err
	}
	table := &nftables.Table{Name: ksTable, Family: nftables.TableFamilyINet}
	c.AddTable(table) // make sure it exists before deleting (DelTable of a missing table fails the batch)
	c.DelTable(table)
	c.AddTable(table)
	policy := nftables.ChainPolicyDrop
	chain := c.AddChain(&nftables.Chain{
		Name: "output", Table: table, Type: nftables.ChainTypeFilter,
		Hooknum: nftables.ChainHookOutput, Priority: nftables.ChainPriorityFilter, Policy: &policy,
	})
	accept := &expr.Verdict{Kind: expr.VerdictAccept}
	add := func(e ...expr.Any) { c.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: append(e, accept)}) }

	add(ifname(unix.NFT_META_OIFNAME, "lo")...)
	if opts.TunName != "" {
		add(ifname(unix.NFT_META_OIFNAME, opts.TunName)...)
	}
	allowed := append([]netip.Prefix{}, LocalNetworks...)
	allowed = append(allowed, opts.TunPrefixes...)
	allowed = append(allowed, hostPrefixes(opts.AllowIPs)...)
	for _, p := range allowed {
		add(daddr(p)...)
	}
	return c.Flush()
}

func DisableKillSwitch() error {
	c, err := nftables.New()
	if err != nil {
		return err
	}
	if !KillSwitchActive() {
		return nil
	}
	c.DelTable(&nftables.Table{Name: ksTable, Family: nftables.TableFamilyINet})
	return c.Flush()
}

func KillSwitchActive() bool {
	c, err := nftables.New()
	if err != nil {
		return false
	}
	t, err := c.ListTableOfFamily(ksTable, nftables.TableFamilyINet)
	return err == nil && t != nil
}

func ifname(key expr.MetaKey, name string) []expr.Any {
	b := make([]byte, 16)
	copy(b, name)
	return []expr.Any{
		&expr.Meta{Key: key, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: b},
	}
}

// daddr matches the destination address against a prefix (IPv4 or IPv6).
func daddr(p netip.Prefix) []expr.Any {
	p = p.Masked()
	proto, offset, size := byte(unix.NFPROTO_IPV4), uint32(16), uint32(4)
	if p.Addr().Is6() {
		proto, offset, size = byte(unix.NFPROTO_IPV6), 24, 16
	}
	mask := make([]byte, size)
	for i := 0; i < p.Bits(); i++ {
		mask[i/8] |= 1 << (7 - i%8)
	}
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: size},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: size, Mask: mask, Xor: make([]byte, size)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: p.Addr().AsSlice()},
	}
}
