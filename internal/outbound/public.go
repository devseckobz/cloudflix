// Package outbound prevents untrusted torrent-supplied URLs and trackers
// from making HTTP or UDP requests to the VPS's private network.
package outbound

import (
 "context"
 "errors"
 "net"
 "net/http"
 "net/netip"
 "time"
)

var ErrBlocked = errors.New("outbound address is not public")

var blocked = []netip.Prefix{
 netip.MustParsePrefix("0.0.0.0/8"),
 netip.MustParsePrefix("100.64.0.0/10"),
 netip.MustParsePrefix("169.254.0.0/16"),
 netip.MustParsePrefix("192.0.0.0/24"),
 netip.MustParsePrefix("192.0.2.0/24"),
 netip.MustParsePrefix("198.18.0.0/15"),
 netip.MustParsePrefix("198.51.100.0/24"),
 netip.MustParsePrefix("203.0.113.0/24"),
 netip.MustParsePrefix("224.0.0.0/4"),
 netip.MustParsePrefix("240.0.0.0/4"),
 netip.MustParsePrefix("2001:db8::/32"),
 netip.MustParsePrefix("fc00::/7"),
 netip.MustParsePrefix("fe80::/10"),
}

func Allowed(ip netip.Addr) bool {
 ip = ip.Unmap()
 if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
  return false
 }
 for _,p:=range blocked {if p.Contains(ip) {return false}}
 return true
}

// DialContext always dials a vetted numeric IP, not the original hostname.
// This closes DNS-rebinding between validation and connection establishment.
func DialContext(ctx context.Context, network, address string) (net.Conn,error) {
 host,port,err:=net.SplitHostPort(address)
 if err!=nil{return nil,err}
 resolved,err:=net.DefaultResolver.LookupIPAddr(ctx,host)
 if err!=nil{return nil,err}
 dialer:=net.Dialer{Timeout:8*time.Second}
 for _,ipAddr:=range resolved {
  ip,ok:=netip.AddrFromSlice(ipAddr.IP)
  if !ok || !Allowed(ip){continue}
  conn,err:=dialer.DialContext(ctx,network,net.JoinHostPort(ip.String(),port))
  if err==nil{return conn,nil}
 }
 return nil,ErrBlocked
}

func Transport() *http.Transport {
 return &http.Transport{
  Proxy:nil,
  DialContext:DialContext,
  TLSHandshakeTimeout:8*time.Second,
  ResponseHeaderTimeout:10*time.Second,
  MaxIdleConns:16,
  MaxIdleConnsPerHost:3,
  IdleConnTimeout:30*time.Second,
 }
}

func Client() *http.Client {
 return &http.Client{Transport:Transport(),Timeout:15*time.Second}
}
