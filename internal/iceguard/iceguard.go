// Package iceguard decides which gathered ICE candidates may leave the SFU.
//
// ICE_ADVERTISE_IP names the addresses this server is reachable at. Setting it
// installs a Pion rewrite rule that replaces the address on *host* candidates,
// and turns STUN off by default so no server-reflexive ones are gathered — so
// in the shipped configuration nothing else is advertised.
//
// That makes the safe thing the default. It does not make the unsafe thing
// impossible. An operator who sets DISABLE_STUN=false gets reflexive candidates
// again, the rewrite rule does not cover them, and the SFU advertises whatever
// address a STUN server reported — which is the shape of the leak in GRYT-768.
// A future code path that gathers candidates some other way lands in the same
// place.
//
// So the addresses are checked on the way out as well as rewritten on the way
// in. This is the check.
package iceguard

// Allowed reports whether a candidate carrying this address may be sent.
//
// An empty advertise list means the operator has not named any address, so
// nothing is being asserted about which are correct and every candidate goes.
// That is the ordinary LAN and STUN case, and refusing there would break every
// deployment that does not set the variable.
//
// With a list, membership is exact. Not a prefix or a subnet test: the variable
// is a list of addresses this server is reachable at, and an address that is
// merely near one of them is still an address nobody chose to publish.
func Allowed(address string, advertise []string) bool {
	if len(advertise) == 0 {
		return true
	}

	for _, allowed := range advertise {
		if address == allowed {
			return true
		}
	}

	return false
}
