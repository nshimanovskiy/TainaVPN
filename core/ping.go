package core

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PingTarget is fetched through the proxy; the full round trip is the "ping".
const PingTarget = "cp.cloudflare.com:80"

// Ping measures the real delay of a proxy: connect to the proxy, authenticate,
// open a tunnel to PingTarget and get an HTTP 204 back. Returns milliseconds.
func Ping(proxyType, host string, port int, username, password string, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if strings.EqualFold(proxyType, "http") {
		req := "CONNECT " + PingTarget + " HTTP/1.1\r\nHost: " + PingTarget + "\r\n"
		if username != "" {
			req += "Proxy-Authorization: Basic " + basicAuth(username, password) + "\r\n"
		}
		if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
			return 0, err
		}
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return 0, fmt.Errorf("proxy: %s", resp.Status)
		}
		return finishPing(conn, br, start)
	}
	if err := socksConnect(conn, username, password, PingTarget); err != nil {
		return 0, err
	}
	return finishPing(conn, bufio.NewReader(conn), start)
}

func finishPing(conn net.Conn, br *bufio.Reader, start time.Time) (int, error) {
	host := PingTarget[:strings.LastIndex(PingTarget, ":")]
	if _, err := io.WriteString(conn, "GET /generate_204 HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		return 0, err
	}
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	ms := int(time.Since(start) / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	return ms, nil
}

func basicAuth(u, p string) string {
	return base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
}

// socksConnect performs the SOCKS5 handshake (optional user/pass) and CONNECT.
func socksConnect(conn net.Conn, username, password, target string) error {
	method := byte(0x00)
	if username != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("not a SOCKS5 proxy: %w", err)
	}
	if resp[0] != 0x05 || resp[1] == 0xff {
		return errors.New("SOCKS5: auth method rejected")
	}
	if resp[1] == 0x02 {
		if len(username) > 255 || len(password) > 255 {
			return errors.New("SOCKS5: credentials too long")
		}
		msg := append([]byte{0x01, byte(len(username))}, username...)
		msg = append(msg, byte(len(password)))
		msg = append(msg, password...)
		if _, err := conn.Write(msg); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, resp); err != nil {
			return err
		}
		if resp[1] != 0x00 {
			return errors.New("SOCKS5: wrong username or password")
		}
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	port, _ := strconv.Atoi(portStr)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("SOCKS5: connect failed (code %d)", head[1])
	}
	skip := 0
	switch head[3] {
	case 0x01:
		skip = 6
	case 0x04:
		skip = 18
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		skip = int(l[0]) + 2
	default:
		return errors.New("SOCKS5: bad reply")
	}
	_, err = io.ReadFull(conn, make([]byte, skip))
	return err
}
