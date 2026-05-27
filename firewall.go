package main

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// provisionHeartFirewall codifies the "no public port on a key-holding
// node" model at the cloud edge:
//
//   - Inbound: SSH (22/tcp) ONLY from the operator IP allowlist, plus
//     ICMP from anywhere for liveness probes / mtr from operators.
//     The conductor admin (8800), hc-http-gw (8090), and the
//     hypothetical HTTP/HTTPS ports stay loopback-bound and are not
//     exposed here — cloudflared reaches them via 127.0.0.1.
//   - Outbound: 53/udp+tcp (DNS), 80/tcp (apt), 443/tcp (Cloudflare
//     tunnel, InfluxDB, GitHub releases, Holochain bootstrap/relay),
//     and ICMP. Cloudflare's tunnel transport sometimes uses UDP/443
//     (QUIC), so we open udp/443 too.
//
// Firewall membership is by DO tag (heart-always-online +
// heart-always-online-alt), so the firewall picks up new droplets
// automatically as the fleet scales without a Pulumi diff for every
// droplet.
//
// The operator allowlist comes from a single Pulumi config value:
// `heart:operator-cidrs` (comma-separated CIDRs). For first-cut
// rollouts where operators all SSH from the same VPN egress, this is
// one entry. If unset, this function logs and returns without
// creating the firewall — opt-in until the operator confirms their
// CIDR list. (Misconfiguring this locks operators out of every
// always-online droplet at once, which is the kind of failure mode
// that warrants explicit opt-in.)
func provisionHeartFirewall(ctx *pulumi.Context) error {
	operatorCidrsRaw, ok := ctx.GetConfig("heart:operator-cidrs")
	if !ok || strings.TrimSpace(operatorCidrsRaw) == "" {
		return ctx.Log.Info("heart:operator-cidrs not set; skipping fleet firewall (set it to a comma-separated CIDR list to enable)", nil)
	}

	cidrs := pulumi.StringArray{}
	for _, c := range strings.Split(operatorCidrsRaw, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		cidrs = append(cidrs, pulumi.String(c))
	}
	if len(cidrs) == 0 {
		return fmt.Errorf("heart:operator-cidrs parsed to zero CIDRs (raw=%q); set at least one or unset the key", operatorCidrsRaw)
	}

	openWorld := pulumi.StringArray{
		pulumi.String("0.0.0.0/0"),
		pulumi.String("::/0"),
	}

	tags := pulumi.StringArray{
		pulumi.String("heart-always-online"),
		pulumi.String("heart-always-online-alt"),
		pulumi.String("blockchain-bridging"),
		pulumi.String("blockchain-bridging-alt"),
		pulumi.String("unyt-bridging"),
		pulumi.String("unyt-bridging-alt"),
	}

	_, err := digitalocean.NewFirewall(ctx, "heart-fleet-firewall", &digitalocean.FirewallArgs{
		Name: pulumi.String("heart-fleet-firewall"),
		Tags: tags,
		InboundRules: digitalocean.FirewallInboundRuleArray{
			&digitalocean.FirewallInboundRuleArgs{
				Protocol:        pulumi.String("tcp"),
				PortRange:       pulumi.String("22"),
				SourceAddresses: cidrs,
			},
			&digitalocean.FirewallInboundRuleArgs{
				Protocol:        pulumi.String("icmp"),
				SourceAddresses: openWorld,
			},
		},
		OutboundRules: digitalocean.FirewallOutboundRuleArray{
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("tcp"),
				PortRange:            pulumi.String("53"),
				DestinationAddresses: openWorld,
			},
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("udp"),
				PortRange:            pulumi.String("53"),
				DestinationAddresses: openWorld,
			},
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("tcp"),
				PortRange:            pulumi.String("80"),
				DestinationAddresses: openWorld,
			},
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("tcp"),
				PortRange:            pulumi.String("443"),
				DestinationAddresses: openWorld,
			},
			// Cloudflare's QUIC transport for tunnels speaks udp/443.
			// Without this, cloudflared falls back to http2 over tcp/443
			// (still works, just less efficient).
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("udp"),
				PortRange:            pulumi.String("443"),
				DestinationAddresses: openWorld,
			},
			// Holochain conductor outbound traffic to bootstrap, signal,
			// and relay endpoints uses a wide ephemeral port range.
			// 1-65535/udp covers iroh / tx5 transports without needing
			// to track Holochain's protocol-specific port lists.
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("udp"),
				PortRange:            pulumi.String("1-65535"),
				DestinationAddresses: openWorld,
			},
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("icmp"),
				DestinationAddresses: openWorld,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create fleet firewall: %w", err)
	}

	return nil
}
