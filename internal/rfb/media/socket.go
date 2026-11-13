package media

import (
	"net"
	"net/netip"
	"syscall"

	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func connectLeg(conn *net.UDPConn, address netip.AddrPort) (N.ConnectedPacketBatchWriter, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var connectErr error
	err = rawConn.Control(func(fd uintptr) {
		var local syscall.Sockaddr
		local, connectErr = syscall.Getsockname(socketFD(fd))
		if connectErr != nil {
			return
		}
		_, localInet6 := local.(*syscall.SockaddrInet6)
		var remote syscall.Sockaddr
		switch {
		case localInet6:
			inet6 := &syscall.SockaddrInet6{Port: int(address.Port()), Addr: address.Addr().As16()}
			if zone := address.Addr().Zone(); zone != "" {
				var networkInterface *net.Interface
				networkInterface, connectErr = net.InterfaceByName(zone)
				if connectErr != nil {
					return
				}
				inet6.ZoneId = uint32(networkInterface.Index)
			}
			remote = inet6
		case address.Addr().Unmap().Is4():
			remote = &syscall.SockaddrInet4{Port: int(address.Port()), Addr: address.Addr().Unmap().As4()}
		default:
			connectErr = E.New("media: an IPv4 socket cannot connect to ", address)
			return
		}
		connectErr = syscall.Connect(socketFD(fd), remote)
	})
	err = E.Errors(err, connectErr)
	if err != nil {
		return nil, err
	}
	return bufio.NewConnectedPacketBatchWriter(bufio.NewUnbindPacketConnWithAddr(conn, M.SocksaddrFromNetIP(address))), nil
}
