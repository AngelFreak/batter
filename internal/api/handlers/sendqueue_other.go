//go:build !linux

package handlers

import "net"

func limitSendQueue(net.Conn, int) {}
