package pool

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DetectCountry finds the country of the proxy's exit IP by making a request
// through the proxy to public geo-IP services.
func DetectCountry(px Proxy, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout, Transport: transportVia(px, timeout)}
	var lastErr error
	for _, svc := range []struct {
		url   string
		field string
	}{
		{"https://api.country.is/", "country"},
		{"http://ip-api.com/json/?fields=status,countryCode", "countryCode"},
		{"https://ipwho.is/?fields=country_code", "country_code"},
	} {
		c, err := fetchCountry(client, svc.url, svc.field)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func fetchCountry(client *http.Client, u, field string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "tainavpn-server")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&data); err != nil {
		return "", err
	}
	c, _ := data[field].(string)
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) != 2 || !isLetters(c) {
		return "", fmt.Errorf("%s: no country in response", u)
	}
	return c, nil
}

func transportVia(px Proxy, timeout time.Duration) *http.Transport {
	t := &http.Transport{TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, DisableKeepAlives: true}
	if px.Type == "http" {
		pu := &url.URL{Scheme: "http", Host: px.Addr()}
		if px.Username != "" {
			pu.User = url.UserPassword(px.Username, px.Password)
		}
		t.Proxy = http.ProxyURL(pu)
		return t
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return DialSOCKS5(ctx, px, addr, timeout)
	}
	return t
}

// DialSOCKS5 opens a TCP connection to addr through a SOCKS5 proxy.
func DialSOCKS5(ctx context.Context, px Proxy, addr string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", px.Addr())
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := socksAuth(conn, px); err != nil {
		conn.Close()
		return nil, err
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port, _ := strconv.Atoi(portStr)
	if len(host) > 255 {
		conn.Close()
		return nil, errors.New("host too long")
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		conn.Close()
		return nil, err
	}
	if head[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect failed (code %d)", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			conn.Close()
			return nil, err
		}
		skip = int(l[0]) + 2
	default:
		conn.Close()
		return nil, errors.New("bad SOCKS5 reply")
	}
	if _, err := io.ReadFull(conn, make([]byte, skip)); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
