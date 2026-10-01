package handlers

import (
	"net"
	"syscall"
)

// tcpNotsentLowat is TCP_NOTSENT_LOWAT (linux/tcp.h); the syscall package
// doesn't name it.
const tcpNotsentLowat = 25

// limitSendQueue caps how much video may sit unsent in the kernel for this
// connection, so a slow viewer's writes block (and it skips ahead to a
// keyframe) instead of frames queueing for seconds in the socket. Best
// effort: errors leave the default.
func limitSendQueue(c net.Conn, bytes int) {
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpNotsentLowat, bytes)
	})
}
