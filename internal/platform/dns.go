// Package platform holds OS-specific workarounds.
package platform

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// FixDNS makes Go's pure resolver work on Android/Termux, where
// /etc/resolv.conf does not exist and lookups otherwise go to localhost:53.
// It prefers the nameservers from Termux's $PREFIX/etc/resolv.conf and
// falls back to public resolvers. It is a no-op on normal Linux systems.
func FixDNS() {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return
	}
	servers := termuxNameservers()
	if len(servers) == 0 {
		servers = []string{"1.1.1.1", "8.8.8.8"}
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			var lastErr error
			for _, s := range servers {
				conn, err := d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}

func termuxNameservers() []string {
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "/data/data/com.termux/files/usr"
	}
	f, err := os.Open(filepath.Join(prefix, "etc", "resolv.conf"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			out = append(out, fields[1])
		}
	}
	return out
}
