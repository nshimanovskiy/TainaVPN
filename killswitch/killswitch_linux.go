//go:build linux

package killswitch

import (
	"encoding/binary"
	"net/netip"

	"github.com/sagernet/nftables"
	"github.com/sagernet/nftables/expr"
	"golang.org/x/sys/unix"
)

const tableName = "tainavpn_killswitch"

func ifname(n string) []byte {
	b := make([]byte, 16)
	copy(b, n+"\x00")
	return b
}

func accept() expr.Any { return &expr.Verdict{Kind: expr.VerdictAccept} }

// destPrefix matches the destination address of IPv4 or IPv6 packets against a prefix.
func destPrefix(p netip.Prefix) []expr.Any {
	p = p.Masked()
	family, offset, size := byte(nftables.TableFamilyIPv4), uint32(16), uint32(4)
	if p.Addr().Is6() {
		family, offset, size = byte(nftables.TableFamilyIPv6), 24, 16
	}
	mask := make([]byte, size)
	for i := 0; i < p.Bits(); i++ {
		mask[i/8] |= 0x80 >> (i % 8)
	}
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{family}},
		&expr.Payload{OperationType: expr.PayloadLoad, DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: size},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: size, Mask: mask, Xor: make([]byte, size)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: p.Addr().AsSlice()},
	}
}

func udpDport(port uint16) []expr.Any {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, port)
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_UDP}},
		&expr.Payload{OperationType: expr.PayloadLoad, DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: b},
	}
}

// Enable installs (or replaces) the kill switch rules: everything leaving the
// machine is dropped except the loopback, the VPN interface, the allowed hosts,
// DHCP and (optionally) the local network.
func Enable(opts Options) error {
	nft, err := nftables.New()
	if err != nil {
		return err
	}
	defer nft.CloseLasting()
	if old, err := nft.ListTableOfFamily(tableName, nftables.TableFamilyINet); err == nil && old != nil {
		nft.DelTable(old)
	}
	table := nft.AddTable(&nftables.Table{Name: tableName, Family: nftables.TableFamilyINet})
	policy := nftables.ChainPolicyDrop
	chain := nft.AddChain(&nftables.Chain{
		Name:     "output",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policy,
	})
	add := func(exprs ...expr.Any) {
		nft.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: append(exprs, accept())})
	}
	for _, name := range []string{"lo", TunName} {
		add(&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1}, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(name)})
	}
	// DHCP (v4 and v6) so the machine keeps its address
	add(udpDport(67)...)
	add(udpDport(547)...)
	prefixes := []netip.Prefix{}
	for _, a := range resolve(opts.Allow) {
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	if opts.AllowLAN {
		prefixes = append(prefixes, LANPrefixes...)
	}
	for _, p := range prefixes {
		add(destPrefix(p)...)
	}
	return nft.Flush()
}

// Disable removes the kill switch rules (no error if they are not installed).
func Disable() error {
	nft, err := nftables.New()
	if err != nil {
		return err
	}
	defer nft.CloseLasting()
	table, err := nft.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	if err != nil || table == nil {
		return nil
	}
	nft.DelTable(table)
	return nft.Flush()
}

// Active reports whether the kill switch rules are installed.
func Active() bool {
	nft, err := nftables.New()
	if err != nil {
		return false
	}
	defer nft.CloseLasting()
	table, err := nft.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	return err == nil && table != nil
}
