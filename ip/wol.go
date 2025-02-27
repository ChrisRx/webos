package ip

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	macAddressBlocksCount = 6
	wolMACAddressCount    = 16
	wolSyncByte           = 0xff
	wolSyncCount          = 6
)

func SendWOLPacket(addr string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	raddr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:9")
	if err != nil {
		return err
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	magic := bytes.Repeat([]byte{wolSyncByte}, 6+6*16)
	macAddrressBlocks := strings.Split(addr, ":")
	for i := 0; i < wolMACAddressCount; i++ {
		for j := 0; j < len(macAddrressBlocks); j++ {
			index := wolSyncCount + i*macAddressBlocksCount + j
			v, err := strconv.ParseUint(macAddrressBlocks[j], 16, 8)
			if err != nil {
				return err
			}
			magic[index] = byte(v)
		}
	}
	if _, err := conn.WriteTo(magic, raddr); err != nil {
		return err
	}
	return nil
}
