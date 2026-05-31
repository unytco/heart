package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"text/template"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// releasePattern is the set of characters allowed in heart:release. The value is
// interpolated into both droplet names and the release:<x> tag, and DigitalOcean
// tag names reject dots, so we constrain it to letters, digits, '-' and '_'.
var releasePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateRelease rejects release labels that would be invalid as a droplet name
// or tag, failing fast at preview time instead of partway through `pulumi up`.
func validateRelease(release string) error {
	if !releasePattern.MatchString(release) {
		return fmt.Errorf("invalid config value 'heart:release'=%q: use only letters, digits, '-' and '_' (e.g. v0-7-0, not v0.7.0) - dots are rejected by DigitalOcean tag names", release)
	}
	return nil
}

// cloudInitData is the set of values rendered into cloudinit/cloud-config.yaml.
// Everything here is per-release config so two release fleets are fully
// independent: their own Holochain version, network endpoints and metrics bucket.
type cloudInitData struct {
	HolochainVersion   string
	HoloKeyutilVersion string
	BootstrapURL       string
	SignalURL          string
	RelayURL           string
	AuthServer         string
	InfluxURL          string
	InfluxOrg          string
	InfluxBucket       string
	InfluxToken        string
}

var regions = []digitalocean.Region{
	digitalocean.RegionAMS3,
	digitalocean.RegionNYC2,
	digitalocean.RegionSFO2,
	digitalocean.RegionBLR1,
}

// cfgRequired returns the value for heart:<key> or an error if it is unset.
func cfgRequired(ctx *pulumi.Context, key string) (string, error) {
	v, ok := ctx.GetConfig("heart:" + key)
	if !ok {
		return "", fmt.Errorf("required config value 'heart:%s' not set", key)
	}
	return v, nil
}

// cfgOr returns the value for heart:<key>, or def (logging the fallback) when unset.
func cfgOr(ctx *pulumi.Context, key, def string) (string, error) {
	if v, ok := ctx.GetConfig("heart:" + key); ok {
		return v, nil
	}
	if err := ctx.Log.Info(fmt.Sprintf("config value 'heart:%s' not set, defaulting to %q", key, def), nil); err != nil {
		return "", err
	}
	return def, nil
}

// cfgIntOr returns the integer value for heart:<key>, or def when unset.
func cfgIntOr(ctx *pulumi.Context, key string, def int) (int, error) {
	s, err := cfgOr(ctx, key, strconv.Itoa(def))
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid config value 'heart:%s'=%q: %w", key, s, err)
	}
	return n, nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		projectName, err := cfgRequired(ctx, "project-name")
		if err != nil {
			return err
		}

		project, err := digitalocean.LookupProject(ctx, &digitalocean.LookupProjectArgs{
			Name: &projectName,
		}, nil)
		if err != nil {
			return err
		}

		urns, err := createFleet(ctx)
		if err != nil {
			return err
		}

		if len(urns) > 0 {
			_, err = digitalocean.NewProjectResources(ctx, "heart-project-resources", &digitalocean.ProjectResourcesArgs{
				Project:   pulumi.String(project.Id),
				Resources: urns,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// createFleet provisions one release's set of nodes. Every droplet name and tag
// is namespaced by heart:release so multiple release fleets coexist in the same
// DigitalOcean project. Run one Pulumi stack per release.
func createFleet(ctx *pulumi.Context) (pulumi.StringArray, error) {
	release, err := cfgRequired(ctx, "release")
	if err != nil {
		return nil, err
	}
	if err := validateRelease(release); err != nil {
		return nil, err
	}
	influxToken, err := cfgRequired(ctx, "influx-token")
	if err != nil {
		return nil, err
	}

	holochainVersion, err := cfgOr(ctx, "holochain-version", "holochain-0.6.1")
	if err != nil {
		return nil, err
	}
	holoKeyutilVersion, err := cfgOr(ctx, "holo-keyutil-version", "v0.1.0")
	if err != nil {
		return nil, err
	}
	bootstrapURL, err := cfgOr(ctx, "bootstrap-url", "https://hc-auth-iroh-unyt.holochain.org/")
	if err != nil {
		return nil, err
	}
	signalURL, err := cfgOr(ctx, "signal-url", "http://not-used:1234")
	if err != nil {
		return nil, err
	}
	relayURL, err := cfgOr(ctx, "relay-url", "https://iroh-relay-unyt.holochain.org")
	if err != nil {
		return nil, err
	}
	authServer, err := cfgOr(ctx, "auth-server", "https://hc-auth-iroh-unyt.holochain.org")
	if err != nil {
		return nil, err
	}
	influxURL, err := cfgOr(ctx, "influx-url", "https://us-east-1-1.aws.cloud2.influxdata.com")
	if err != nil {
		return nil, err
	}
	influxOrg, err := cfgOr(ctx, "influx-org", "711f755560a58686")
	if err != nil {
		return nil, err
	}
	influxBucket, err := cfgOr(ctx, "influx-bucket", "unyt")
	if err != nil {
		return nil, err
	}

	cloudInit, err := renderCloudInit(cloudInitData{
		HolochainVersion:   holochainVersion,
		HoloKeyutilVersion: holoKeyutilVersion,
		BootstrapURL:       bootstrapURL,
		SignalURL:          signalURL,
		RelayURL:           relayURL,
		AuthServer:         authServer,
		InfluxURL:          influxURL,
		InfluxOrg:          influxOrg,
		InfluxBucket:       influxBucket,
		InfluxToken:        influxToken,
	})
	if err != nil {
		return nil, err
	}

	getSshKeysResult, err := digitalocean.GetSshKeys(ctx, &digitalocean.GetSshKeysArgs{}, nil)
	if err != nil {
		return nil, err
	}
	var sshFingerprints []string
	for _, key := range getSshKeysResult.SshKeys {
		sshFingerprints = append(sshFingerprints, key.Fingerprint)
	}

	alwaysOnlineSize, err := cfgOr(ctx, "always-online-size", string(digitalocean.DropletSlugDropletS2VCPU4GB))
	if err != nil {
		return nil, err
	}
	bridgingSize, err := cfgOr(ctx, "bridging-size", string(digitalocean.DropletSlugDropletS4VCPU8GB))
	if err != nil {
		return nil, err
	}

	alwaysOnlineCount, err := cfgIntOr(ctx, "always-online-count", 1)
	if err != nil {
		return nil, err
	}
	blockchainBridgingCount, err := cfgIntOr(ctx, "blockchain-bridging-count", 1)
	if err != nil {
		return nil, err
	}
	if blockchainBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:blockchain-bridging-count' cannot be greater than 1, got %d", blockchainBridgingCount)
	}
	unytBridgingCount, err := cfgIntOr(ctx, "unyt-bridging-count", 1)
	if err != nil {
		return nil, err
	}
	if unytBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:unyt-bridging-count' cannot be greater than 1, got %d", unytBridgingCount)
	}

	var urns pulumi.StringArray

	// always-online droplets
	for i := 1; i <= alwaysOnlineCount; i++ {
		name := fmt.Sprintf("heart-always-online-%s-%d", release, i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(alwaysOnlineSize),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("heart-always-online"), pulumi.String("release:" + release)},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			BackupPolicy: &digitalocean.DropletBackupPolicyArgs{
				Plan:    pulumi.String("weekly"),
				Weekday: pulumi.String("TUE"),
				Hour:    pulumi.Int(8),
			},
			UserData: pulumi.String(cloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		urns = append(urns, droplet.DropletUrn)
	}

	// blockchain-bridging droplets
	for i := 1; i <= blockchainBridgingCount; i++ {
		name := fmt.Sprintf("blockchain-bridging-%s-%d", release, i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(bridgingSize),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("blockchain-bridging"), pulumi.String("release:" + release)},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(cloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		urns = append(urns, droplet.DropletUrn)
	}

	// unyt-bridging droplets
	for i := 1; i <= unytBridgingCount; i++ {
		name := fmt.Sprintf("unyt-bridging-%s-%d", release, i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(bridgingSize),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("unyt-bridging"), pulumi.String("release:" + release)},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(cloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		urns = append(urns, droplet.DropletUrn)
	}

	return urns, nil
}

// renderCloudInit reads the cloud-config template and renders it with data.
func renderCloudInit(data cloudInitData) (string, error) {
	raw, err := os.ReadFile("cloudinit/cloud-config.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to read cloud-init config: %w", err)
	}
	tmpl, err := template.New("cloud-init").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("failed to parse cloud-init template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render cloud-init template: %w", err)
	}
	return buf.String(), nil
}
