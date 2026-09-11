package netdetect

import (
	"net"
	"strings"
)

type Address struct {
	IP        string `json:"ip"`
	Family    string `json:"family"` // v4 | v6
	Prefix    int    `json:"prefix"`
	Global    bool   `json:"global"`
	Interface string `json:"interface"`
	Kind      string `json:"kind"` // address | prefix
}

func Scan() []Address {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Address
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := iface.Name
		if strings.HasPrefix(name, "br-") || name == "docker0" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "particeps") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
				continue
			}
			ones, bits := ipn.Mask.Size()
			fam := "v4"
			if ip.To4() == nil {
				fam = "v6"
				if !ip.IsGlobalUnicast() {
					continue
				}
			} else if ip.IsPrivate() {
				// still show as candidate; admin decides
			}
			out = append(out, Address{
				IP:        ip.String(),
				Family:    fam,
				Prefix:    ones,
				Global:    ip.IsGlobalUnicast() && !ip.IsPrivate(),
				Interface: name,
				Kind:      "address",
			})
			if fam == "v6" && ones < 128 && ones >= 48 {
				pfx := ip.Mask(ipn.Mask).String() + "/" + itoa(ones)
				out = append(out, Address{
					IP:        pfx,
					Family:    "v6",
					Prefix:    ones,
					Global:    true,
					Interface: name,
					Kind:      "prefix",
				})
				_ = bits
			}
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
