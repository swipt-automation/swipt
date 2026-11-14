package vpnpeer

import (
	"net"
	"net/netip"
	"strings"
	"syscall"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

func scan() ([]tunnel, []netip.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, E.Cause(err, "vpnpeer: read the interfaces")
	}
	common.SortBy(interfaces, func(it net.Interface) string { return it.Name })
	var local []netip.Addr
	var tunnels []tunnel
	indexes := map[int]int{}
	pointToPoint := map[int]bool{}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		var hasIPv4 bool
		for _, address := range addresses {
			network, isNetwork := address.(*net.IPNet)
			if !isNetwork {
				continue
			}
			ip, valid := netip.AddrFromSlice(network.IP)
			if !valid {
				continue
			}
			ip = ip.Unmap()
			local = append(local, ip)
			if ip.Is4() {
				hasIPv4 = true
			}
		}
		if iface.Flags&net.FlagUp == 0 || !strings.HasPrefix(iface.Name, "utun") || !hasIPv4 {
			continue
		}
		indexes[iface.Index] = len(tunnels)
		pointToPoint[iface.Index] = iface.Flags&net.FlagPointToPoint != 0
		tunnels = append(tunnels, tunnel{})
	}
	if len(tunnels) == 0 {
		return nil, local, nil
	}
	routes, routesErr := readRIB(route.RIBTypeRoute)
	for _, message := range routes {
		routeMessage, isRoute := message.(*route.RouteMessage)
		if !isRoute {
			continue
		}
		index, found := indexes[routeMessage.Index]
		if !found {
			continue
		}
		if address, ok := ipv4(routeMessage.Addrs, syscall.RTAX_GATEWAY); ok {
			tunnels[index].gateways = append(tunnels[index].gateways, address)
		}
		if address, ok := ipv4(routeMessage.Addrs, syscall.RTAX_DST); ok {
			tunnels[index].destinations = append(tunnels[index].destinations, address)
		}
	}
	addresses, addressesErr := readRIB(route.RIBTypeInterface)
	for _, message := range addresses {
		addressMessage, isAddress := message.(*route.InterfaceAddrMessage)
		if !isAddress || !pointToPoint[addressMessage.Index] {
			continue
		}
		index, found := indexes[addressMessage.Index]
		if !found {
			continue
		}
		if address, ok := ipv4(addressMessage.Addrs, syscall.RTAX_BRD); ok {
			tunnels[index].peers = append(tunnels[index].peers, address)
		}
	}
	return tunnels, local, E.Errors(routesErr, addressesErr)
}

func onLocalNetwork(address netip.Addr) bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ifaceAddress := range addresses {
			network, isNetwork := ifaceAddress.(*net.IPNet)
			if !isNetwork {
				continue
			}
			ip, valid := netip.AddrFromSlice(network.IP)
			if valid && ip.Unmap() == address {
				return true
			}
		}
	}
	return false
}

func readRIB(kind route.RIBType) ([]route.Message, error) {
	rib, err := route.FetchRIB(syscall.AF_UNSPEC, kind, 0)
	if err != nil {
		return nil, E.Cause(err, "vpnpeer: read the routing table")
	}
	messages, err := route.ParseRIB(kind, rib)
	if err != nil {
		return nil, E.Cause(err, "vpnpeer: parse the routing table")
	}
	return messages, nil
}

func ipv4(addresses []route.Addr, slot int) (netip.Addr, bool) {
	if slot >= len(addresses) {
		return netip.Addr{}, false
	}
	address, isIPv4 := addresses[slot].(*route.Inet4Addr)
	if !isIPv4 {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4(address.IP), true
}

// Virtualization.framework guests report kern.hv_vmm_present as 1
// (vphone-cli 1316940, Research/Patches/hv_vmm_present_usermode_xrefs.md:61-72).
func guestAddresses() []netip.Addr {
	present, err := unix.SysctlUint32("kern.hv_vmm_present")
	if err != nil || present == 0 {
		return nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []netip.Addr
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			network, isNetwork := address.(*net.IPNet)
			if !isNetwork {
				continue
			}
			ip, valid := netip.AddrFromSlice(network.IP)
			if valid && ip.Unmap().Is4() && !ip.Unmap().IsLinkLocalUnicast() {
				result = append(result, ip.Unmap())
			}
		}
	}
	return result
}
