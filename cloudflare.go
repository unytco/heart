package main

import (
	"fmt"

	"github.com/pulumi/pulumi-cloudflare/sdk/v6/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// cloudflareTunnelOutputs is what the Pulumi-managed Cloudflare tunnel
// returns to the rest of the program: a connector token (Pulumi secret
// output) that every heart droplet bakes into /etc/cloudflared/token,
// and the public gateway hostname that the explorer's proxy worker
// already forwards to.
type cloudflareTunnelOutputs struct {
	Token    pulumi.StringOutput
	Hostname string
}

// provisionCloudflareTunnel declares the Cloudflare-side resources for
// the always-on hc-http-gw tunnel:
//
//   - A ZeroTrust Cloudflared tunnel with ConfigSrc=cloudflare
//     (remote-managed: ingress rules live in Cloudflare state, NOT in a
//     config.yml on each droplet — every droplet only needs the token).
//   - A single ingress rule routing the public hostname to the local
//     hc-http-gw on 127.0.0.1:8090.
//   - A proxied CNAME on the configured zone pointing at the tunnel's
//     <id>.cfargotunnel.com endpoint.
//
// The returned Token output is baked into the cloud-init template;
// cloudflared on each droplet uses it to register as a connector
// replica. Cloudflare load-balances across healthy replicas of the
// same tunnel id, so every always-on droplet contributes to HA for
// free.
//
// We intentionally pick remote-managed (ConfigSrc=cloudflare) so
// changing ingress is a `pulumi up` rather than a fleet-wide config
// rewrite. Droplets carry exactly one Cloudflare-touching secret on
// disk (the token), nothing else.
func provisionCloudflareTunnel(ctx *pulumi.Context) (*cloudflareTunnelOutputs, error) {
	// Pulumi-managed Cloudflare tunnel is opt-in. When `heart:cf-account-id`
	// is unset, we skip every CF resource and return zero values for the
	// cloud-init template (CloudflareToken="", GatewayHostname=""). This
	// is the hybrid-management path: the CF tunnel lives in the CF
	// dashboard, operators install cloudflared per-droplet with the token
	// from the dashboard's "Install connector" wizard, and Pulumi only
	// owns droplets / project membership / firewall. To flip back to
	// Pulumi-owned tunnel, set all of:
	//   heart:cf-account-id, heart:cf-zone-name, heart:gw-hostname,
	//   heart:cloudflare-tunnel-secret, cloudflare:apiToken
	// and re-run `pulumi up`.
	accountID, ok := ctx.GetConfig("heart:cf-account-id")
	if !ok {
		if err := ctx.Log.Info("heart:cf-account-id not set; skipping Pulumi-managed Cloudflare tunnel (hybrid mode — manage tunnel via CF dashboard)", nil); err != nil {
			return nil, err
		}
		return &cloudflareTunnelOutputs{
			Token:    pulumi.String("").ToStringOutput(),
			Hostname: "",
		}, nil
	}
	zoneName, ok := ctx.GetConfig("heart:cf-zone-name")
	if !ok {
		return nil, fmt.Errorf("heart:cf-account-id is set but heart:cf-zone-name is not — set both to enable Pulumi-managed tunnel, or unset cf-account-id for hybrid mode")
	}
	hostname, ok := ctx.GetConfig("heart:gw-hostname")
	if !ok {
		return nil, fmt.Errorf("heart:cf-account-id is set but heart:gw-hostname is not — set both to enable Pulumi-managed tunnel, or unset cf-account-id for hybrid mode")
	}
	// TunnelSecret must be a base64 string of at least 32 bytes; the
	// connector token Cloudflare hands out is derived from it. Operators
	// generate this once via `openssl rand -base64 32` and store it as a
	// Pulumi secret. Rotating it invalidates all connectors briefly.
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

	tunnel, err := cloudflare.NewZeroTrustTunnelCloudflared(ctx, "unyt-tunnel", &cloudflare.ZeroTrustTunnelCloudflaredArgs{
		AccountId:    pulumi.String(accountID),
		Name:         pulumi.String("unyt-tunnel"),
		ConfigSrc:    pulumi.String("cloudflare"),
		TunnelSecret: pulumi.String(tunnelSecret),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloudflare tunnel: %w", err)
	}

	// Single named-hostname ingress. Cloudflare implicitly adds a
	// catch-all (http_status:404) rule at the end; explicit catch-all is
	// only required for `local` config sources.
	_, err = cloudflare.NewZeroTrustTunnelCloudflaredConfig(ctx, "unyt-tunnel-ingress", &cloudflare.ZeroTrustTunnelCloudflaredConfigArgs{
		AccountId: pulumi.String(accountID),
		TunnelId:  tunnel.ID().ToStringOutput(),
		Config: &cloudflare.ZeroTrustTunnelCloudflaredConfigConfigArgs{
			Ingresses: cloudflare.ZeroTrustTunnelCloudflaredConfigConfigIngressArray{
				&cloudflare.ZeroTrustTunnelCloudflaredConfigConfigIngressArgs{
					Hostname: pulumi.String(hostname),
					Service:  pulumi.String("http://127.0.0.1:8090"),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set Cloudflare tunnel ingress: %w", err)
	}

	// `Proxied: true` is what makes Cloudflare actually route through the
	// tunnel for this hostname — without it the CNAME would resolve but
	// connections would go directly to cfargotunnel.com (which doesn't
	// terminate TLS for arbitrary records).
	cnameContent := tunnel.ID().ToStringOutput().ApplyT(func(id string) string {
		return fmt.Sprintf("%s.cfargotunnel.com", id)
	}).(pulumi.StringOutput)
	_, err = cloudflare.NewDnsRecord(ctx, "unyt-tunnel-dns", &cloudflare.DnsRecordArgs{
		ZoneId:  pulumi.String(zoneID),
		Name:    pulumi.String(hostname),
		Type:    pulumi.String("CNAME"),
		Content: cnameContent,
		Proxied: pulumi.Bool(true),
		Ttl:     pulumi.Float64(1), // 1 = "Auto"; required for proxied records.
		Comment: pulumi.String("Managed by heart Pulumi program (unyt-tunnel tunnel)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}

	tokenOut := cloudflare.GetZeroTrustTunnelCloudflaredTokenOutput(ctx, cloudflare.GetZeroTrustTunnelCloudflaredTokenOutputArgs{
		AccountId: pulumi.String(accountID),
		TunnelId:  tunnel.ID().ToStringOutput(),
	}, nil)

	ctx.Export("gatewayHostname", pulumi.String(hostname))
	ctx.Export("tunnelId", tunnel.ID())

	return &cloudflareTunnelOutputs{
		Token:    tokenOut.Token(),
		Hostname: hostname,
	}, nil
}
