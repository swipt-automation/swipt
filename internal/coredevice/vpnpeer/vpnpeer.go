package vpnpeer

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
)

// LocalDevVPN defaults its peer address to 10.7.0.1/32; StosVPN defaults
// tunnelFakeIp to 10.7.0.1 (LocalDevVPN af3fd69,
// LocalDevVPN/Constants.swift:16; StosVPN 010bc14,
// TunnelProv/PacketTunnelProvider.swift:12).
var fixedAddress = netip.AddrFrom4([4]byte{10, 7, 0, 1})

const answerLimit = time.Second

type tunnel struct {
	gateways     []netip.Addr
	destinations []netip.Addr
	peers        []netip.Addr
}

// SideStore minimuxer takes utun interfaces that have IPv4 addresses, sorted
// by name, then tries route gateways, route destinations and point-to-point
// peers, in that order (minimuxer 12be70d,
// Sources/Services/DeviceConnectionManager.swift:206-214, 260-283).
func candidates(allowLocal bool) ([]netip.Addr, error) {
	if allowLocal {
		if addresses := guestAddresses(); len(addresses) > 0 {
			return addresses, nil
		}
	}
	tunnels, local, err := scan()
	var result []netip.Addr
	add := func(address netip.Addr) {
		if address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() ||
			address == netip.AddrFrom4([4]byte{255, 255, 255, 255}) ||
			slices.Contains(local, address) || slices.Contains(result, address) {
			return
		}
		result = append(result, address)
	}
	for _, tunnel := range tunnels {
		for _, list := range [][]netip.Addr{tunnel.gateways, tunnel.destinations, tunnel.peers} {
			for _, address := range list {
				add(address)
			}
		}
	}
	add(fixedAddress)
	return result, err
}

type Peer struct {
	Address      netip.Addr
	LocalNetwork bool
}

func Find(ctx context.Context, port uint16, allowLocal bool) (Peer, error) {
	addresses, scanErr := candidates(allowLocal)
	if len(addresses) == 0 {
		return Peer{}, E.Errors(E.New("vpnpeer: no VPN address to try"), scanErr)
	}
	return loopBack(ctx, addresses, port, scanErr)
}

func Check(ctx context.Context, address netip.AddrPort) error {
	_, err := loopBack(ctx, []netip.Addr{address.Addr()}, address.Port(), nil)
	return err
}

func loopBack(ctx context.Context, addresses []netip.Addr, port uint16, scanErr error) (Peer, error) {
	ctx, cancel := context.WithTimeout(ctx, answerLimit)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", ":0")
	if err != nil {
		return Peer{}, E.Cause(err, "vpnpeer: listen for the loopback")
	}
	defer listener.Close()
	loopbackPort := M.AddrPortFromNet(listener.Addr()).Port()
	tokens := make([][16]byte, len(addresses))
	for index := range tokens {
		rand.Read(tokens[index][:])
	}
	type arrival struct {
		index int
		local netip.Addr
	}
	arrivals := make(chan arrival)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetReadDeadline(deadline)
				var token [16]byte
				if _, err := io.ReadFull(conn, token[:]); err != nil {
					return
				}
				index := slices.Index(tokens, token)
				if index < 0 {
					return
				}
				select {
				case arrivals <- arrival{index: index, local: M.AddrPortFromNet(conn.LocalAddr()).Addr().Unmap()}:
				case <-ctx.Done():
				}
			}()
		}
	}()
	type failure struct {
		index int
		err   error
	}
	failures := make(chan failure, len(addresses))
	for index, address := range addresses {
		go func() {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "tcp", netip.AddrPortFrom(address, port).String())
			if err == nil {
				_ = conn.Close()
				conn, err = dialer.DialContext(ctx, "tcp", netip.AddrPortFrom(address, loopbackPort).String())
			}
			if err == nil {
				_, err = conn.Write(tokens[index][:])
				_ = conn.Close()
			}
			if err != nil {
				failures <- failure{index: index, err: err}
			}
		}()
	}
	errs := make([]error, len(addresses))
wait:
	for range addresses {
		select {
		case failure := <-failures:
			errs[failure.index] = failure.err
		case arrival := <-arrivals:
			address := addresses[arrival.index]
			return Peer{Address: address, LocalNetwork: address != arrival.local && onLocalNetwork(arrival.local)}, nil
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			break wait
		}
	}
	return Peer{}, E.Cause(E.Errors(append(errs, scanErr)...), "none of the VPN addresses ", strings.Join(F.MapToString(addresses), ", "), " loops back to port ", port)
}
