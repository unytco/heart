package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// cloudInitData is the value passed to text/template when rendering
// cloud-config.yaml, once per server (so each droplet's /etc/heart-fleet/
// metadata carries its own service identity).
//
//   - InfluxToken is the InfluxDB write token droplets use for telemetry.
//   - GatewayHostname is the public hc-http-gw hostname (empty for boxes
//     that don't front a tunnel). Operator scripts read it from
//     /etc/heart-fleet/metadata; on-droplet ingress is bound by the
//     locally-managed cloudflared config.yml + credentials.json that
//     automation streams in post-boot.
//   - PrimaryService / Services let on-box scripts self-identify their role.
type cloudInitData struct {
	InfluxToken     string
	GatewayHostname string
	PrimaryService  string
	Services        string
}

// renderCloudInit reads a cloud-config template from disk and returns it as a
// plain string ready for the Droplet UserData field. All substitutions are
// plain strings, so there's no async Pulumi Output to thread through.
func renderCloudInit(templatePath string, data cloudInitData) (string, error) {
	cloudInitRaw, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read cloud-init config %q: %w", templatePath, err)
	}
	tmpl, err := template.New("cloud-init").Parse(string(cloudInitRaw))
	if err != nil {
		return "", fmt.Errorf("failed to parse cloud-init template %q: %w", templatePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render cloud-init template %q: %w", templatePath, err)
	}
	return buf.String(), nil
}

// priorName maps a server's current logical name to the name it had under the
// old count-based program, so a `pulumi up` on an imported/migrated stack
// RENAMES the droplet (via Aliases) instead of destroying + recreating it
// (which would wipe the lair keystore, agent key, and auth material). Harmless
// on a fresh stack where the old names never existed.
var priorName = map[string]string{
	"always-online-1": "heart-always-online-1",
	"always-online-2": "heart-always-online-2",
	"hash-explorer-1": "heart-always-online-3",
	// The bridge boxes keep their existing names (hot-2-mhot-bridge,
	// hf-2-infra-bridge), so they need no alias. If migrating old count-based
	// Pulumi state (logical names blockchain-bridging-1 / unyt-bridging-1),
	// add aliases here before the first `pulumi up` to rename-not-replace.
}

func servicesCSV(svcs []Service) string {
	parts := make([]string, len(svcs))
	for i, s := range svcs {
		parts[i] = string(s)
	}
	return strings.Join(parts, ",")
}

func servicesStrings(svcs []Service) []string {
	parts := make([]string, len(svcs))
	for i, s := range svcs {
		parts[i] = string(s)
	}
	return parts
}

// tagSafe makes a value usable as a DigitalOcean tag value: DO tags allow only
// lowercase letters, numbers, colons, dashes, and underscores. Any other
// character (notably the dots in a version like "v0.90.0") becomes a dash.
func tagSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == ':':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, s)
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Effective fleet. Optionally override the public gateway hostname per
		// stack via heart:gw-hostname (e.g. a per-version or test hostname like
		// unyt-tunnel-v090.unyt.co) without editing fleet.go — handy to avoid
		// colliding with a production DNS record during a review/cutover.
		fleet := Fleet
		if gw, ok := ctx.GetConfig("heart:gw-hostname"); ok && gw != "" {
			fleet = append([]Server(nil), Fleet...)
			for i := range fleet {
				if fleet[i].IsPublic() {
					fleet[i].GatewayHostname = gw
				}
			}
		}

		if err := validateFleet(fleet); err != nil {
			return err
		}

		projectName, ok := ctx.GetConfig("heart:project-name")
		if !ok {
			return fmt.Errorf("required config value 'heart:project-name' not set")
		}
		influxToken, ok := ctx.GetConfig("heart:influx-token")
		if !ok {
			return fmt.Errorf("required config value 'heart:influx-token' not set")
		}
		// Optional: the app-version release this fleet serves. Threaded into a
		// "version:<v>" droplet tag and the post-boot .happ download URL. One
		// stack per release (heart:release-version) is how multiple versions'
		// fleets run side by side. See plan § Multi-version strategy.
		releaseVersion, _ := ctx.GetConfig("heart:release-version")

		project, err := digitalocean.LookupProject(ctx, &digitalocean.LookupProjectArgs{
			Name: &projectName,
		}, nil)
		if err != nil {
			return err
		}

		sshKeysResult, err := digitalocean.GetSshKeys(ctx, &digitalocean.GetSshKeysArgs{}, nil)
		if err != nil {
			return err
		}
		var sshFingerprints []string
		for _, key := range sshKeysResult.SshKeys {
			sshFingerprints = append(sshFingerprints, key.Fingerprint)
		}

		// Cloudflare tunnels + DNS for public hostnames (opt-in via
		// heart:cf-account-id). Returns connector credentials per hostname.
		tunnels, err := provisionCloudflareTunnels(ctx, fleet)
		if err != nil {
			return err
		}

		droplets := map[string]*digitalocean.Droplet{}
		var allDropletURNs pulumi.StringArray
		fleetOut := pulumi.Map{}

		for _, srv := range fleet {
			cloudInit, err := renderCloudInit("cloudinit/default/cloud-config.yaml", cloudInitData{
				InfluxToken:     influxToken,
				GatewayHostname: srv.GatewayHostname,
				PrimaryService:  string(srv.Primary),
				Services:        servicesCSV(srv.Services()),
			})
			if err != nil {
				return err
			}

			tags := srv.Tags()
			// DO droplet display name carries the release version so each
			// version's fleet is distinguishable in the DO console, e.g.
			// "always-online-1-v0-90". The Pulumi logical name and the fleet
			// export key stay the base name so automation configs keep resolving.
			doName := srv.Name
			if releaseVersion != "" {
				v := tagSafe(releaseVersion)
				tags = append(tags, "version:"+v)
				doName = srv.Name + "-" + v
			}

			// Ignore sshKeys (only applied at create) and userData: cloud-init
			// runs only at first boot, so re-rendering it (e.g. a metadata or
			// hostname change) must NOT force-replace a live droplet — that
			// would destroy the lair keystore + agent key. Genuine cloud-init
			// updates are rolled out by recreating a node deliberately.
			opts := []pulumi.ResourceOption{
				pulumi.IgnoreChanges([]string{"sshKeys", "userData"}),
				// DigitalOcean droplet create/delete can be slow (backups
				// enabled + API latency); the provider default timeout was too
				// tight and delete failed with "context deadline exceeded".
				pulumi.Timeouts(&pulumi.CustomTimeouts{Create: "20m", Delete: "20m"}),
			}
			if old, ok := priorName[srv.Name]; ok {
				opts = append(opts, pulumi.Aliases([]pulumi.Alias{{Name: pulumi.String(old)}}))
			}

			droplet, err := digitalocean.NewDroplet(ctx, srv.Name, &digitalocean.DropletArgs{
				Image:      pulumi.String("ubuntu-24-04-x64"),
				Name:       pulumi.String(doName),
				Region:     pulumi.String(srv.Region),
				Size:       pulumi.String(srv.Size),
				Ipv6:       pulumi.Bool(true),
				Tags:       pulumi.ToStringArray(tags),
				SshKeys:    pulumi.ToStringArray(sshFingerprints),
				Monitoring: pulumi.Bool(true),
				Backups:    pulumi.Bool(true),
				BackupPolicy: &digitalocean.DropletBackupPolicyArgs{
					Plan:    pulumi.String("weekly"),
					Weekday: pulumi.String("TUE"),
					Hour:    pulumi.Int(8),
				},
				UserData: pulumi.String(cloudInit),
			}, opts...)
			if err != nil {
				return err
			}
			droplets[srv.Name] = droplet
			allDropletURNs = append(allDropletURNs, droplet.DropletUrn)

			fleetOut[srv.Name] = pulumi.Map{
				"name":            pulumi.String(doName),
				"ipv4":            droplet.Ipv4Address,
				"ipv6":            droplet.Ipv6Address,
				"service":         pulumi.String(srv.Primary),
				"region":          pulumi.String(srv.Region),
				"tags":            pulumi.ToStringArray(tags),
				"services":        pulumi.ToStringArray(servicesStrings(srv.Services())),
				"gatewayHostname": pulumi.String(srv.GatewayHostname),
				"gatewayApps":     pulumi.ToStringArray(srv.GatewayApps),
			}
		}

		if len(allDropletURNs) > 0 {
			_, err = digitalocean.NewProjectResources(ctx, "heart-project-resources", &digitalocean.ProjectResourcesArgs{
				Project:   pulumi.String(project.Id),
				Resources: allDropletURNs,
			})
			if err != nil {
				return err
			}
		}

		if err := provisionHeartFirewall(ctx); err != nil {
			return err
		}

		// Workstream B: drive the post-boot deploy (install .happ, await key
		// approval, gateway, tunnel, registry) from Pulumi via the command
		// provider. Opt-in via heart:orchestrate-postboot to keep `pulumi up`
		// infra-only until the operator wants end-to-end.
		if orchestrate, _ := ctx.GetConfig("heart:orchestrate-postboot"); orchestrate == "true" {
			if err := provisionPostBoot(ctx, fleet, droplets, tunnels, releaseVersion); err != nil {
				return err
			}
		}

		ctx.Export("fleet", fleetOut)
		return nil
	})
}
