package outbound

import (
 "net/netip"
 "testing"
)

func TestAllowed(t *testing.T) {
 cases:=[]struct{address string; allowed bool}{
  {"127.0.0.1",false},
  {"10.0.0.1",false},
  {"192.168.0.1",false},
  {"169.254.169.254",false},
  {"100.64.0.1",false},
  {"198.18.0.1",false},
  {"::1",false},
  {"fc00::1",false},
  {"2001:db8::1",false},
  {"8.8.8.8",true},
  {"1.1.1.1",true},
  {"2606:4700:4700::1111",true},
 }
 for _,tc:=range cases {
  addr:=netip.MustParseAddr(tc.address)
  if got:=Allowed(addr);got!=tc.allowed{t.Errorf("%s: got %v want %v",tc.address,got,tc.allowed)}
 }
}
