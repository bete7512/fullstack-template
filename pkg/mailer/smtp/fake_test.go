package smtp_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSMTP accepts sessions on addr and hands each DATA payload to data.
type fakeSMTP struct {
	addr string
	data chan string
}

func startFake(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String(), data: make(chan string, 1)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake ESMTP\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			fmt.Fprint(conn, "250-fake\r\n250 8BITMIME\r\n")
		case strings.HasPrefix(cmd, "DATA"):
			fmt.Fprint(conn, "354 go ahead\r\n")
			var b strings.Builder
			for l, err := r.ReadString('\n'); err == nil && l != ".\r\n"; l, err = r.ReadString('\n') {
				b.WriteString(l)
			}
			f.data <- b.String()
			fmt.Fprint(conn, "250 queued\r\n")
		case strings.HasPrefix(cmd, "QUIT"):
			fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}
