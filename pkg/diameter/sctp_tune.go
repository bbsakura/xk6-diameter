package diameter

import (
	"fmt"
	"reflect"
	"syscall"
	"unsafe"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/ishidawataru/sctp"
	"golang.org/x/sys/unix"
)

// cf. https://github.com/thebagchi/sctp-go/blob/master/sctp_structs.go
// /*
//  * 7.1.2 Association Parameters (SCTP_ASSOCINFO)
//  *
//  *   This option is used to both examine and set various association and
//  *   endpoint parameters.
//  */
// struct sctp_assocparams {
// 	sctp_assoc_t	sasoc_assoc_id;
// 	__u16		sasoc_asocmaxrxt;
// 	__u16		sasoc_number_peer_destinations;
// 	__u32		sasoc_peer_rwnd;
// 	__u32		sasoc_local_rwnd;
// 	__u32		sasoc_cookie_life;
// };

type SCTPAssocParams struct {
	AssocID             uint32
	MaxRetrans          uint16
	NumPeerDestinations uint16
	PeerRWND            uint32
	LocalRWND           uint32
	CookieLife          uint32
}

// cf. https://github.com/leostratus/netinet/blob/master/sctp.h
const SCTP_ASSOCINFO = 0x00000002

func getSCTPFdFromConn(sc *sctp.SCTPConn) (int, error) {
	v := reflect.ValueOf(sc).Elem()

	field := v.FieldByName("_fd")
	if !field.IsValid() {
		return -1, fmt.Errorf("field '_fd' not found in sctp.SCTPConn (library structure might have changed)")
	}

	ptr := unsafe.Pointer(field.UnsafeAddr())
	realPtr := (*int)(ptr)

	fd := *realPtr
	return fd, nil
}

func getSCTPFdFromListener(sl *sctp.SCTPListener) (int, error) {
	v := reflect.ValueOf(sl).Elem()
	field := v.FieldByName("fd")
	if !field.IsValid() {
		return -1, fmt.Errorf("field '_fd' not found in sctp.SCTPConn (library structure might have changed)")
	}

	ptr := unsafe.Pointer(field.UnsafeAddr())
	realPtr := (*int)(ptr)

	fd := *realPtr
	return fd, nil
}

func getsockopt(fd int, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	r0, r1, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		unix.IPPROTO_SCTP,
		optname,
		optval,
		optlen,
		0)
	if errno != 0 {
		return r0, r1, errno
	}
	return r0, r1, nil
}

func setsockopt(fd int, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	r0, r1, errno := syscall.Syscall6(syscall.SYS_SETSOCKOPT,
		uintptr(fd),
		unix.IPPROTO_SCTP,
		optname,
		optval,
		optlen,
		0)
	if errno != 0 {
		return r0, r1, errno
	}
	return r0, r1, nil
}

func tunesockfd(fd, size int) error {
	// set send buffer size
	err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, size)
	if err != nil {
		return fmt.Errorf("failed to set SO_SNDBUF to %d: %w", size, err)
	}
	// set receive buffer size
	err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, size)
	if err != nil {
		return fmt.Errorf("failed to set SO_RCVBUF to %d: %w", size, err)
	}
	if err := setPeerRwnd(fd, uint32(size)); err != nil {
		return fmt.Errorf("setPeerRwnd: %w", err)
	}
	return nil
}

func tunesock(sc *sctp.SCTPConn, size int) error {
	fd, err := getSCTPFdFromConn(sc)
	if err != nil {
		return fmt.Errorf("failed to get SCTP socket fd: %w", err)
	}
	err = tunesockfd(fd, size)
	if err != nil {
		return fmt.Errorf("failed to tune socket: %w", err)
	}
	return nil
}
func startReceiverSCTP(cli *sm.Client, network, localAddr, remoteAddr string) (diam.Conn, error) {
	laddr, err := sctp.ResolveSCTPAddr(network, localAddr)
	if err != nil {
		return nil, fmt.Errorf("ResolveSCTPAddr(local): %w", err)
	}
	raddr, err := sctp.ResolveSCTPAddr(network, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("ResolveSCTPAddr(remote): %w", err)
	}
	sc, err := sctp.DialSCTP(network, laddr, raddr)
	if err != nil {
		return nil, fmt.Errorf("DialSCTP: %w", err)
	}
	if err := tunesock(sc, 4<<20); err != nil {
		return nil, fmt.Errorf("setPeerRwnd: %w", err)
	}

	conn, err := cli.NewConn(sc, sc.RemoteAddr().String())
	if err != nil {
		return nil, fmt.Errorf("NewConn: %w", err)
	}

	return conn, nil
}

func setPeerRwnd(fd int, size uint32) error {
	var params SCTPAssocParams
	optlen := uintptr(unsafe.Sizeof(params))

	if _, _, err := getsockopt(
		fd,
		uintptr(SCTP_ASSOCINFO),
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&optlen)),
	); err != nil {
		return fmt.Errorf("getsockopt SCTP_ASSOCINFO: %w", err)
	}

	params.PeerRWND = size
	params.LocalRWND = size

	if _, _, err := setsockopt(
		fd,
		uintptr(SCTP_ASSOCINFO),
		uintptr(unsafe.Pointer(&params)),
		optlen,
	); err != nil {
		return fmt.Errorf("setsockopt SCTP_ASSOCINFO: %w", err)
	}

	return nil
}
