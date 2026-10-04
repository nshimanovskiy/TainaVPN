package core

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fake SOCKS5 + HTTP-CONNECT proxy that answers the HTTP request itself with 204
func fakeProxy(t *testing.T, kind string) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				if kind == "socks" {
					buf := make([]byte, 3)
					io.ReadFull(br, buf)
					c.Write([]byte{5, 2})
					h := make([]byte, 2)
					io.ReadFull(br, h)
					u := make([]byte, h[1]+1)
					io.ReadFull(br, u)
					p := make([]byte, u[len(u)-1])
					io.ReadFull(br, p)
					if string(u[:len(u)-1]) != "u" || string(p) != "p" {
						c.Write([]byte{1, 1})
						return
					}
					c.Write([]byte{1, 0})
					hd := make([]byte, 5)
					io.ReadFull(br, hd)
					io.ReadFull(br, make([]byte, int(hd[4])+2))
					c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				} else {
					req, _ := http.ReadRequest(br)
					if req.Header.Get("Proxy-Authorization") != "Basic dTpw" {
						c.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
						return
					}
					c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
				}
				http.ReadRequest(br)
				time.Sleep(20 * time.Millisecond)
				c.Write([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
			}(c)
		}
	}()
	return l.Addr().String()
}

func TestPing(t *testing.T) {
	for _, kind := range []string{"socks", "http"} {
		addr := fakeProxy(t, kind)
		host, port, _ := net.SplitHostPort(addr)
		pn := 0
		for _, ch := range port {
			pn = pn*10 + int(ch-'0')
		}
		ms, err := Ping(kind, host, pn, "u", "p", 3*time.Second)
		if err != nil || ms < 20 {
			t.Fatalf("%s: ms=%d err=%v", kind, ms, err)
		}
		_, err = Ping(kind, host, pn, "u", "bad", 3*time.Second)
		if err == nil {
			t.Fatalf("%s: wrong password accepted", kind)
		}
		t.Logf("%s ok: %d ms, bad pass -> %v", kind, ms, strings.TrimSpace(err.Error()))
	}
}
