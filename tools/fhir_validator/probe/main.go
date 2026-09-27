//go:build linux

// This independent qualification probe is never included in the product worker.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// The owned canaries live on an internal Docker network in TEST-NET-2, a
// documentation range that is never routed, so no real host is addressed.
var canaryNetwork = &net.IPNet{IP: net.IPv4(198, 51, 100, 0), Mask: net.CIDRMask(24, 32)}

func emit(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
func main() {
	mode := flag.String("mode", "", "serve, dial, capture, or filesystem")
	address := flag.String("address", "", "owned canary address")
	duration := flag.Duration("duration", 60*time.Second, "bounded observation duration")
	flag.Parse()
	if *duration < time.Millisecond || *duration > 5*time.Minute {
		os.Exit(2)
	}
	switch *mode {
	case "serve":
		serve()
	case "dial":
		dial(*address)
	case "capture":
		capture(*duration)
	case "filesystem":
		filesystem()
	default:
		os.Exit(2)
	}
}
func serve() {
	listener, e := net.Listen("tcp", ":8080")
	if e != nil {
		os.Exit(2)
	}
	var count atomic.Uint64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 65536))
		emit(map[string]any{"event": "request", "count": n})
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "readmit-owned-egress-canary")
	})
	emit(map[string]any{"event": "ready", "port": 8080})
	_ = (&http.Server{Handler: handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second}).Serve(listener)
}
func dial(address string) {
	host, port, e := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if e != nil || ip == nil || !(ip.IsPrivate() || canaryNetwork.Contains(ip)) || port != "8080" {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, e := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if e != nil {
		emit(map[string]any{"connected": false})
		return
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	_, e = fmt.Fprintf(connection, "GET /readmit-egress-probe HTTP/1.1\r\nHost: owned-canary\r\nConnection: close\r\n\r\n")
	raw, readErr := io.ReadAll(io.LimitReader(connection, 4096))
	emit(map[string]any{"connected": e == nil && readErr == nil && strings.Contains(string(raw), "readmit-owned-egress-canary")})
}

// capture counts every frame on every interface of the network namespace it
// shares with the worker. Link-layer housekeeping (ARP, ICMPv6 neighbour
// discovery) is counted apart from transport: any TCP or UDP frame, in either
// direction on any interface including loopback, is a connection attempt.
func capture(duration time.Duration) {
	descriptor, e := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ALL)))
	if e != nil {
		emit(map[string]any{"event": "unavailable"})
		os.Exit(2)
	}
	defer syscall.Close(descriptor)
	_ = syscall.SetsockoptInt(descriptor, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 1<<20)
	_ = syscall.SetsockoptTimeval(descriptor, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Usec: 200000})
	if e = syscall.Bind(descriptor, &syscall.SockaddrLinklayer{Protocol: htons(syscall.ETH_P_ALL)}); e != nil {
		os.Exit(2)
	}
	interfaces, _ := net.Interfaces()
	names := []string{}
	for _, v := range interfaces {
		names = append(names, v.Name)
	}
	emit(map[string]any{"event": "ready", "interfaces": names})
	done := make(chan struct{})
	go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(done) }()
	deadline := time.Now().Add(duration)
	buffer := make([]byte, 65536)
	received, outbound, tcp, udp, other := 0, 0, 0, 0, 0
	for time.Now().Before(deadline) {
		select {
		case <-done:
			deadline = time.Time{}
			continue
		default:
		}
		n, from, err := syscall.Recvfrom(descriptor, buffer, 0)
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
			continue
		}
		if err != nil {
			emit(map[string]any{"event": "error"})
			os.Exit(2)
		}
		received++
		if link, ok := from.(*syscall.SockaddrLinklayer); ok && link.Pkttype == syscall.PACKET_OUTGOING {
			outbound++
		}
		switch transport(buffer[:n]) {
		case syscall.IPPROTO_TCP:
			tcp++
		case syscall.IPPROTO_UDP:
			udp++
		case -1:
		default:
			other++
		}
		if received > 100000 {
			emit(map[string]any{"event": "limit"})
			os.Exit(2)
		}
	}
	stats := struct{ Packets, Drops uint32 }{}
	size := uint32(unsafe.Sizeof(stats))
	_, _, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(descriptor), syscall.SOL_PACKET, syscall.PACKET_STATISTICS, uintptr(unsafe.Pointer(&stats)), uintptr(unsafe.Pointer(&size)), 0)
	emit(map[string]any{"event": "complete", "received": received, "outbound": outbound, "tcp": tcp, "udp": udp, "other_ip": other, "kernel_packets": stats.Packets, "dropped": stats.Drops, "statistics_available": errno == 0})
}
func htons(v uint16) uint16 { return v<<8 | v>>8 }

// transport returns the IP protocol of an Ethernet frame (loopback frames
// carry a zero Ethernet header), or -1 for a frame that is not IP.
func transport(frame []byte) int {
	if len(frame) < 14 {
		return -1
	}
	switch binary.BigEndian.Uint16(frame[12:14]) {
	case 0x0800:
		if len(frame) >= 14+20 {
			return int(frame[14+9])
		}
	case 0x86dd:
		if len(frame) >= 14+40 {
			if next := int(frame[14+6]); next != syscall.IPPROTO_ICMPV6 {
				return next
			}
		}
	}
	return -1
}
func filesystem() {
	blocked := func(path string) bool {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return true
		}
		_, _ = f.Write([]byte("owned"))
		_ = f.Close()
		_ = os.Remove(path)
		return false
	}
	script := "/work/noexec-probe"
	e := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700)
	noexec := false
	if e == nil {
		noexec = exec.Command(script).Run() != nil
		_ = os.Remove(script)
	}
	emit(map[string]any{"outside_write_blocked": blocked("/tmp/readmit-582-escape"), "input_write_blocked": blocked("/input/readmit-582-escape"), "image_write_blocked": blocked("/readmit-582-escape"), "work_file_created": e == nil, "work_execution_blocked": noexec})
}
