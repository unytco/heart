package main

import (
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi-cloudflare/sdk/v6/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// tunnelInfo is what a Pulumi-managed Cloudflare tunnel returns to the rest
// of the program for one public gateway hostname.
//
//   - CredentialsJSON is the cloudflared credentials file contents (a Pulumi
//     SECRET output) — {AccountTag, TunnelID, TunnelSecret}. automation
//     streams it to /etc/cloudflared/<TunnelID>.json on each droplet that
//     joins this tunnel, exactly as the locally-managed model expects.
//   - TunnelID is the tunnel UUID (the CNAME points at <id>.cfargotunnel.com).
type tunnelInfo struct {
	CredentialsJSON pulumi.StringOutput
	TunnelID        pulumi.StringOutput
}

var hostnameSlug = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

// provisionCloudflareTunnels declares the Cloudflare-side resources for every
// public-facing server in the fleet: one ZeroTrust tunnel + one proxied CNAME
// per distinct GatewayHostname.
//
// We deliberately use ConfigSrc="local": ingress stays in /etc/cloudflared/
// config.yml on each droplet (unchanged from today's locally-managed model),
// so Pulumi owns ONLY tunnel-creation + DNS, not the on-droplet connector.
// This reverses commit af6454e's removal of the CF provider WITHOUT reverting
// the cloud-config half of that commit (the droplet still runs the connector
// from its config.yml). cloudflared connects with the per-tunnel
// credentials.json we export as a secret — no remote ingress resource.
//
// Cloudflare load-balances across healthy connector replicas sharing one
// tunnel id, so every droplet that joins a hostname's tunnel contributes to HA.
//
// Opt-in: when heart:cf-account-id is unset we skip all CF resources and
// return an empty map (hybrid mode — tunnel managed out-of-band). To enable,
// set: heart:cf-account-id, heart:cf-zone-name, cloudflare:apiToken, and a
// per-tunnel secret heart:cloudflare-tunnel-secret (shared across hostnames if
// only one is given). Returns map keyed by gateway hostname.
func provisionCloudflareTunnels(ctx *pulumi.Context, fleet []Server) (map[string]*tunnelInfo, error) {
	out := map[string]*tunnelInfo{}

	accountID, ok := ctx.GetConfig("heart:cf-account-id")
	if !ok {
		if err := ctx.Log.Info("heart:cf-account-id not set; skipping Pulumi-managed Cloudflare tunnels (hybrid mode — manage tunnel out-of-band)", nil); err != nil {
			return nil, err
		}
		return out, nil
	}
	zoneName, ok := ctx.GetConfig("heart:cf-zone-name")
	if !ok {
		return nil, fmt.Errorf("heart:cf-account-id is set but heart:cf-zone-name is not — set both to enable Pulumi-managed tunnels, or unset cf-account-id for hybrid mode")
	}
	// TunnelSecret must be base64 of >=32 bytes; the credentials file and the
	// connector token both derive from it. Generate once via
	// `openssl rand -base64 32` and store as a Pulumi secret. One secret is
	// shared across all hostnames here; split into per-hostname keys later if
	// distinct tunnels must rotate independently.
	tunnelSecret, ok := ctx.GetConfig("heart:cloudflare-tunnel-secret")
	if !ok {
		return nil, fmt.Errorf("required config 'heart:cloudflare-tunnel-secret' not set (generate with `openssl rand -base64 32` then `pulumi config set --secret heart:cloudflare-tunnel-secret <value>`)")
	}

	zone, err := cloudflare.LookupZones(ctx, &cloudflare.LookupZonesArgs{
		Name: pulumi.StringRef(zoneName),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to look up Cloudflare zone %q: %w", zoneName, err)
	}
	if len(zone.Results) == 0 {
		return nil, fmt.Errorf("no Cloudflare zone matched name %q", zoneName)
	}
	zoneID := zone.Results[0].Id

	// Distinct public hostnames across the fleet, in fleet order.
	seen := map[string]bool{}
	for _, s := range fleet {
		if !s.IsPublic() || s.GatewayHostname == "" || seen[s.GatewayHostname] {
			continue
		}
		seen[s.GatewayHostname] = true
		host := s.GatewayHostname
		// A stable, DNS-safe Pulumi resource name per hostname.
		base := hostnameSlug.ReplaceAllString(host, "-")

		tunnel, err := cloudflare.NewZeroTrustTunnelCloudflared(ctx, base+"-tunnel", &cloudflare.ZeroTrustTunnelCloudflaredArgs{
			AccountId:    pulumi.String(accountID),
			Name:         pulumi.String(base),
			ConfigSrc:    pulumi.String("local"),
			TunnelSecret: pulumi.String(tunnelSecret),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Cloudflare tunnel for %q: %w", host, err)
		}

		// Proxied CNAME <host> -> <tunnel-id>.cfargotunnel.com. Proxied is
		// what actually routes through the tunnel; Ttl=1 ("Auto") is required
		// for proxied records.
		cnameContent := tunnel.ID().ToStringOutput().ApplyT(func(id string) string {
			return fmt.Sprintf("%s.cfargotunnel.com", id)
		}).(pulumi.StringOutput)
		_, err = cloudflare.NewDnsRecord(ctx, base+"-dns", &cloudflare.DnsRecordArgs{
			ZoneId:  pulumi.String(zoneID),
			Name:    pulumi.String(host),
			Type:    pulumi.String("CNAME"),
			Content: cnameContent,
			Proxied: pulumi.Bool(true),
			Ttl:     pulumi.Float64(1),
			Comment: pulumi.String(fmt.Sprintf("Managed by heart Pulumi program (%s tunnel)", base)),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create DNS record for %q: %w", host, err)
		}

		// Build the cloudflared credentials.json contents as a secret output.
		// Format matches what `cloudflared tunnel create` writes and what
		// automation/scripts/setup-tunnel.sh streams to the droplet.
		credsJSON := tunnel.ID().ToStringOutput().ApplyT(func(id string) string {
			// tunnelSecret is already base64; embed verbatim.
			return fmt.Sprintf(`{"AccountTag":%q,"TunnelID":%q,"TunnelSecret":%q}`, accountID, id, tunnelSecret)
		}).(pulumi.StringOutput)

		ctx.Export(base+"-tunnel-id", tunnel.ID())

		out[host] = &tunnelInfo{
			CredentialsJSON: pulumi.ToSecret(credsJSON).(pulumi.StringOutput),
			TunnelID:        tunnel.ID().ToStringOutput(),
		}
	}

	return out, nil
}
