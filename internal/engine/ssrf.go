package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
)

// SSRFError is returned when a connection attempt is rejected because
// the target IP falls inside a blocked network range. Wrapping it into
// its own type so callers can distinguish SSRF blocks from other
// network errors (e.g. via errors.As).
type SSRFError struct {
	Host string
	IP   net.IP
}

func (e *SSRFError) Error() string {
	return fmt.Sprintf(
		"SSRF blocked: %s resolves to private address %s "+
			"(set --allow-private-networks on the button or "+
			"BUTTONS_ALLOW_PRIVATE_NETWORKS=1 to override)",
		e.Host, e.IP,
	)
}

// privateNetworkCIDRs is the default blocklist applied to every URL
// button that does not explicitly opt in to private-network access.
// Each range is documented with its purpose so future contributors
// can reason about inclusions / exclusions.
var privateNetworkCIDRs = []string{
	// IPv4 loopback.
	"127.0.0.0/8",
	// RFC 1918 private ranges.
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	// RFC 3927 IPv4 link-local — includes AWS/GCP/Azure instance
	// metadata at 169.254.169.254. Blocking this is the #1 reason
	// SSRF protection exists in a server context.
	"169.254.0.0/16",
	// RFC 6598 carrier-grade NAT.
	"100.64.0.0/10",
	// "This network" per RFC 791.
	"0.0.0.0/8",
	// IPv4 multicast (RFC 5771) and reserved space (RFC 1112).
	"224.0.0.0/4",
	"240.0.0.0/4",
	// IPv6 loopback, ULA, link-local, multicast.
	"::1/128",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
}

// privateNetworks is the parsed form of privateNetworkCIDRs, populated
// at package init so repeated dial operations don't re-parse the CIDRs.
var privateNetworks = parseCIDRs(privateNetworkCIDRs)

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			// Hard panic — a typo in a package-level blocklist is a
			// programmer error, not a runtime concern.
			panic("engine: invalid CIDR in privateNetworkCIDRs: " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// isPrivateIP reports whether the given IP belongs to any of the
// currently configured blocked ranges.
func isPrivateIP(ip net.IP, blocklist []*net.IPNet) bool {
	for _, n := range blocklist {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// privateNetworksGloballyAllowed returns true when the user has set
// BUTTONS_ALLOW_PRIVATE_NETWORKS=1 in their environment. Meant as a
// dev-mode opt-out so developers don't have to recreate every local
// button with --allow-private-networks.
func privateNetworksGloballyAllowed() bool {
	return os.Getenv("BUTTONS_ALLOW_PRIVATE_NETWORKS") == "1"
}

// newSafeDialContext returns a net.Dialer DialContext function that
// rejects connections to any IP inside the given blocklist. Exported
// as a constructor (rather than a package-level function) so tests
// can supply a custom blocklist or an empty one.
//
// The dialer performs DNS resolution once, validates every resolved
// address against the blocklist, and then dials by the first resolved
// IP. Dialing by IP (rather than by hostname) prevents the classic
// DNS-rebinding attack where an attacker returns a public IP on the
// first lookup and a private IP on the second.
func newSafeDialContext(blocklist []*net.IPNet) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// Literal IP in the URL — no DNS step.
		if ip := net.ParseIP(host); ip != nil {
			if isPrivateIP(ip, blocklist) {
				return nil, &SSRFError{Host: host, IP: ip}
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}

		// Hostname — resolve and validate every A/AAAA record. If any
		// record lands in the blocklist we refuse the whole request,
		// on the pessimistic assumption the attacker controls the
		// hostname's DNS.
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IPs resolved for host %q", host)
		}
		for _, ipAddr := range ips {
			if isPrivateIP(ipAddr.IP, blocklist) {
				return nil, &SSRFError{Host: host, IP: ipAddr.IP}
			}
		}

		// Dial by the resolved IP, not the hostname, so the connection
		// can't be redirected to a different address between our check
		// and the actual connect.
		target := ips[0].IP.String()
		if strings.Contains(target, ":") {
			// Bracket IPv6 literals for the "host:port" joiner.
			target = "[" + target + "]"
		}
		return (&net.Dialer{}).DialContext(ctx, network, target+":"+port)
	}
}
