// Package netbind decides which addresses the server listens on.
//
// Default: loopback plus every local Tailscale address, never 0.0.0.0. A NAS
// laptop may sit on a LAN with other people's devices (or a VirtualBox
// host-only network); the tailnet is the only network meant to reach it.
// Tailscale can come up after the server does, so addresses are re-scanned.
package netbind

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var (
	tailnetV4 = mustCIDR("100.64.0.0/10")       // Tailscale CGNAT range
	tailnetV6 = mustCIDR("fd7a:115c:a1e0::/48") // Tailscale ULA range
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// TailscaleIPs returns this machine's tailnet addresses.
func TailscaleIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if tailnetV4.Contains(ipn.IP) || tailnetV6.Contains(ipn.IP) {
			out = append(out, ipn.IP)
		}
	}
	return out
}

type Binder struct {
	Server   *http.Server
	Port     int
	Explicit []string // if set, exactly these and no scanning

	mu    sync.Mutex
	bound map[string]net.Listener
}

func (b *Binder) wanted() []string {
	if len(b.Explicit) > 0 {
		return b.Explicit
	}
	port := strconv.Itoa(b.Port)
	out := []string{net.JoinHostPort("127.0.0.1", port)}
	for _, ip := range TailscaleIPs() {
		out = append(out, net.JoinHostPort(ip.String(), port))
	}
	return out
}

// Sync binds any wanted address not yet bound. It returns the bound list.
func (b *Binder) Sync() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bound == nil {
		b.bound = map[string]net.Listener{}
	}
	for _, addr := range b.wanted() {
		if _, ok := b.bound[addr]; ok {
			continue
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			log.Printf("listen %s: %v", addr, err)
			continue
		}
		log.Printf("listening on http://%s", addr)
		b.bound[addr] = l
		go func() {
			if err := b.Server.Serve(l); err != nil && err != http.ErrServerClosed {
				log.Printf("serve %s: %v", addr, err)
			}
			b.mu.Lock()
			delete(b.bound, addr)
			b.mu.Unlock()
		}()
	}
	var out []string
	for a := range b.bound {
		out = append(out, a)
	}
	return out
}

// Watch re-runs Sync every interval until ctx ends. A Tailscale address that
// disappears is left to fail on its own; its listener exits and is rebound
// when the address returns.
func (b *Binder) Watch(ctx context.Context, every time.Duration) {
	if len(b.Explicit) > 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.Sync()
		}
	}
}
