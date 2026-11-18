//go:build !windows && !linux && !darwin

package tun

import "context"

func platformFacts(_ context.Context, facts *Facts) {
	facts.DNSKnown = false
	facts.FirewallKnown = false
	facts.NftKnown = false
	facts.Notes = append(facts.Notes, "platform is not supported for TUN")
}

func platformNotes(Facts) []string {
	return []string{"unsupported platform"}
}
