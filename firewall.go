package main

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// pilotFirewallTag is the default DO tag the fleet firewall attaches to
// on first rollout. It is intentionally NOT heart-always-online — tag
// one droplet with heart-firewall-pilot in the DO console, smoke-test,
// then opt into fleet-wide attach via heart:firewall-tags.
const pilotFirewallTag = "heart-firewall-pilot"

// provisionHeartFirewall codifies the "no public port on a key-holding
// node" model at the cloud edge:
//
//   - Inbound: SSH (22/tcp) ONLY from the operator IP allowlist, plus
//     ICMP from anywhere for liveness probes / mtr from operators.
//     The conductor admin (8800), hc-http-gw (8090), and the
//     hypothetical HTTP/HTTPS ports stay loopback-bound and are not
//     exposed here — cloudflared reaches them via 127.0.0.1.
//   - Outbound: 53/udp+tcp (DNS), 22/tcp (git-over-SSH for on-droplet
//     cargo build / setup-gateway), 80/tcp (apt), 443/tcp (Cloudflare
//     tunnel, InfluxDB, GitHub releases, Holochain bootstrap/relay),
//     udp/443 (QUIC), udp/1-65535 (Holochain iroh/tx5), and ICMP.
//
// Rollout is deliberately **not** fleet-wide on first enable:
//
//   - heart:operator-cidrs must be set (SSH inbound allowlist). Without
//     it the firewall is skipped entirely.
//   - heart:firewall-tags controls which DO tags receive the firewall.
//     When unset, only pilotFirewallTag ("heart-firewall-pilot") is
//     used — tag a single droplet in the DO console, run smoke-test.sh,
//     then set heart:firewall-tags to the production fleet tags
//     (heart-always-online, heart-always-online-alt, …) for a
//     deliberate fleet-wide attach.
//
// DO Cloud Firewalls are deny-by-default in both directions once
// attached. Attaching to heart-always-online without a pilot pass would
// immediately drop outbound TCP outside 53/80/443 on every tagged
// droplet (breaking git-over-SSH during setup-gateway, among others).
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

	tags, fleetWide, err := firewallTagsFromConfig(ctx)
	if err != nil {
		return err
	}
	if !fleetWide {
		_ = ctx.Log.Info(
			"heart-fleet-firewall attaching to pilot tag only ("+pilotFirewallTag+"); "+
				"tag one droplet, smoke-test, then set heart:firewall-tags for fleet-wide attach",
			nil,
		)
	}

	openWorld := pulumi.StringArray{
		pulumi.String("0.0.0.0/0"),
		pulumi.String("::/0"),
	}

	_, err = digitalocean.NewFirewall(ctx, "heart-fleet-firewall", &digitalocean.FirewallArgs{
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
			// git-over-SSH during on-droplet `cargo build` / setup-gateway
			// (crates.io is 443; private git deps may use ssh:// on 22).
			&digitalocean.FirewallOutboundRuleArgs{
				Protocol:             pulumi.String("tcp"),
				PortRange:            pulumi.String("22"),
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

// firewallTagsFromConfig resolves which DO tags receive the firewall.
//
//   - heart:firewall-tags unset → pilot tag only (NOT fleet-wide).
//   - heart:firewall-tags set → comma-separated explicit tag list
//     (use for fleet-wide attach after pilot smoke-test passes).
func firewallTagsFromConfig(ctx *pulumi.Context) (pulumi.StringArray, bool, error) {
	raw, ok := ctx.GetConfig("heart:firewall-tags")
	if !ok || strings.TrimSpace(raw) == "" {
		return pulumi.StringArray{pulumi.String(pilotFirewallTag)}, false, nil
	}

	tags := pulumi.StringArray{}
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		tags = append(tags, pulumi.String(t))
	}
	if len(tags) == 0 {
		return nil, false, fmt.Errorf("heart:firewall-tags parsed to zero tags (raw=%q)", raw)
	}
	return tags, true, nil
}
