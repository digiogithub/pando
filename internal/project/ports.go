package project

import (
	"fmt"
	"net"
)

const defaultWebPort = 8800

func chooseAvailablePort(host string, preferred int) (int, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if preferred <= 0 {
		preferred = defaultWebPort
	}

	candidates := []int{preferred}
	for offset := 1; offset <= 10; offset++ {
		candidates = append(candidates, preferred+offset)
	}

	for _, port := range candidates {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
		if err == nil {
			_ = ln.Close()
			return port, nil
		}
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("failed to find available port near %d: %w", preferred, err)
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to determine random available port")
	}

	return addr.Port, nil
}
