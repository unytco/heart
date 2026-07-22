package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"text/template"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gopkg.in/yaml.v3"
)

// defaultsFile holds the default value for every optional heart config key. A
// per-stack `pulumi config set heart:<key> ...` always overrides what is here.
const defaultsFile = "defaults.yaml"

// loadDefaults reads defaultsFile into a key -> value map. Keys are bare (no
// "heart:" prefix) and match the keys passed to cfgOr / cfgIntOr.
func loadDefaults(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read defaults file %s: %w", path, err)
	}
	defaults := map[string]string{}
	if err := yaml.Unmarshal(raw, &defaults); err != nil {
		return nil, fmt.Errorf("failed to parse defaults file %s: %w", path, err)
	}
	return defaults, nil
}

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

// nodeType describes one kind of droplet in a release fleet. All types boot the
// same cloud-config (Holochain conductor + lair + registration); they differ
// only in name/tag, size, how many to create, and backup policy.
//
// To add a new server type: append an entry here and add its <countKey> and
// <sizeKey> to defaults.yaml. Nothing else needs to change.
type nodeType struct {
	name         string // droplet name + tag prefix, e.g. "heart-always-online"
	sizeKey      string // config/defaults key for the droplet size slug
	countKey     string // config/defaults key for how many to create
	maxCount     int    // 0 = unlimited; >0 rejects counts above the cap
	weeklyBackup bool   // set a weekly backup policy (otherwise DO's default)
}

var nodeTypes = []nodeType{
	{name: "progenitor", sizeKey: "progenitor-size", countKey: "progenitor-count", maxCount: 1, weeklyBackup: true},
	{name: "heart-always-online", sizeKey: "always-online-size", countKey: "always-online-count", weeklyBackup: true},
	{name: "blockchain-bridging", sizeKey: "bridging-size", countKey: "blockchain-bridging-count", maxCount: 1},
	{name: "unyt-bridging", sizeKey: "bridging-size", countKey: "unyt-bridging-count", maxCount: 1},
	{name: "hf-swapper", sizeKey: "hf-swapper-size", countKey: "hf-swapper-count", weeklyBackup: true},
	{name: "hash-explorer", sizeKey: "hash-explorer-size", countKey: "hash-explorer-count", weeklyBackup: true},
	{name: "notary", sizeKey: "notary-size", countKey: "notary-count", weeklyBackup: true},
}

// cfgRequired returns the value for heart:<key> or an error if it is unset.
func cfgRequired(ctx *pulumi.Context, key string) (string, error) {
	v, ok := ctx.GetConfig("heart:" + key)
	if !ok {
		return "", fmt.Errorf("required config value 'heart:%s' not set", key)
	}
	return v, nil
}

// cfgOr returns the value for heart:<key>, falling back to the default from
// defaults.yaml (logging the fallback) when the stack does not set it. It is an
// error for a key to have neither a stack value nor a default.
func cfgOr(ctx *pulumi.Context, defaults map[string]string, key string) (string, error) {
	if v, ok := ctx.GetConfig("heart:" + key); ok {
		return v, nil
	}
	def, ok := defaults[key]
	if !ok {
		return "", fmt.Errorf("config value 'heart:%s' is not set and has no default in %s", key, defaultsFile)
	}
	if err := ctx.Log.Info(fmt.Sprintf("config value 'heart:%s' not set, defaulting to %q (from %s)", key, def, defaultsFile), nil); err != nil {
		return "", err
	}
	return def, nil
}

// cfgIntOr returns the integer value for heart:<key>, falling back to defaults.yaml.
func cfgIntOr(ctx *pulumi.Context, defaults map[string]string, key string) (int, error) {
	s, err := cfgOr(ctx, defaults, key)
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

	defaults, err := loadDefaults(defaultsFile)
	if err != nil {
		return nil, err
	}

	holochainVersion, err := cfgOr(ctx, defaults, "holochain-version")
	if err != nil {
		return nil, err
	}
	holoKeyutilVersion, err := cfgOr(ctx, defaults, "holo-keyutil-version")
	if err != nil {
		return nil, err
	}
	bootstrapURL, err := cfgOr(ctx, defaults, "bootstrap-url")
	if err != nil {
		return nil, err
	}
	signalURL, err := cfgOr(ctx, defaults, "signal-url")
	if err != nil {
		return nil, err
	}
	relayURL, err := cfgOr(ctx, defaults, "relay-url")
	if err != nil {
		return nil, err
	}
	authServer, err := cfgOr(ctx, defaults, "auth-server")
	if err != nil {
		return nil, err
	}
	influxURL, err := cfgOr(ctx, defaults, "influx-url")
	if err != nil {
		return nil, err
	}
	influxOrg, err := cfgOr(ctx, defaults, "influx-org")
	if err != nil {
		return nil, err
	}
	influxBucket, err := cfgOr(ctx, defaults, "influx-bucket")
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

	var urns pulumi.StringArray
	// ips maps a release-independent server key (e.g. heart-always-online-1) to its
	// public IPv4, exported as the "ips" stack output. `make up` writes this to
	// releases/<release>/ips.json, which the automation repo references by key.
	ips := pulumi.Map{}
	for _, nt := range nodeTypes {
		size, err := cfgOr(ctx, defaults, nt.sizeKey)
		if err != nil {
			return nil, err
		}
		count, err := cfgIntOr(ctx, defaults, nt.countKey)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, fmt.Errorf("config value 'heart:%s' cannot be negative, got %d", nt.countKey, count)
		}
		if nt.maxCount > 0 && count > nt.maxCount {
			// The software does not support more than maxCount of this type.
			return nil, fmt.Errorf("config value 'heart:%s' cannot be greater than %d, got %d", nt.countKey, nt.maxCount, count)
		}

		for i := 1; i <= count; i++ {
			name := fmt.Sprintf("%s-%s-%d", nt.name, release, i)
			args := &digitalocean.DropletArgs{
				Image:      pulumi.String("ubuntu-24-04-x64"),
				Name:       pulumi.String(name),
				Region:     pulumi.String(regions[i%len(regions)]),
				Size:       pulumi.String(size),
				Ipv6:       pulumi.Bool(true),
				Tags:       pulumi.StringArray{pulumi.String(nt.name), pulumi.String("release:" + release)},
				SshKeys:    pulumi.ToStringArray(sshFingerprints),
				Monitoring: pulumi.Bool(true),
				Backups:    pulumi.Bool(true),
				UserData:   pulumi.String(cloudInit),
			}
			if nt.weeklyBackup {
				args.BackupPolicy = &digitalocean.DropletBackupPolicyArgs{
					Plan:    pulumi.String("weekly"),
					Weekday: pulumi.String("TUE"),
					Hour:    pulumi.Int(8),
				}
			}
			droplet, err := digitalocean.NewDroplet(ctx, name, args, pulumi.IgnoreChanges([]string{"sshKeys"}))
			if err != nil {
				return nil, err
			}
			urns = append(urns, droplet.DropletUrn)
			ips[ipsKey(nt, i)] = droplet.Ipv4Address
		}
	}

	ctx.Export("ips", ips)

	return urns, nil
}

// ipsKey is the release-independent key used for a node's IPv4 in the "ips"
// stack output (and thus in releases/<release>/ips.json), e.g. "heart-always-online-1".
func ipsKey(nt nodeType, i int) string {
	return fmt.Sprintf("%s-%d", nt.name, i)
}

// renderCloudInit reads the cloud-config template plus the shared base-image
// files and renders them into the final cloud-init user-data.
func renderCloudInit(data cloudInitData) (string, error) {
	raw, err := os.ReadFile("cloudinit/cloud-config.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to read cloud-init config: %w", err)
	}

	// The invariant base-image files (cloudinit/base/) are the single source of
	// truth shared with the local-testnet fleet image (automation/local/fleet/):
	// the two systemd units and the first-boot core. They are injected via
	// cloud-init's `encoding: b64` so a multi-line file lands as one YAML scalar
	// (no indentation gymnastics) and is byte-for-byte what the fleet Dockerfile
	// COPYs. A change to a base file reaches BOTH targets. Per-target divergences
	// live outside these files (prod's InfluxDB metrics env in a
	// holochain.service.d drop-in below; the fleet's first-boot ordering drop-in
	// and its own wrapper).
	lairUnitB64, err := readBaseFileB64("cloudinit/base/lair-keystore.service")
	if err != nil {
		return "", err
	}
	holochainUnitB64, err := readBaseFileB64("cloudinit/base/holochain.service")
	if err != nil {
		return "", err
	}
	firstBootCoreB64, err := readBaseFileB64("cloudinit/base/first-boot-core.sh")
	if err != nil {
		return "", err
	}

	// Embed cloudInitData so its fields stay accessible as {{ .BootstrapURL }}
	// etc., and add the base64 blobs the write_files entries consume.
	tmplData := struct {
		cloudInitData
		LairKeystoreUnitB64 string
		HolochainUnitB64    string
		FirstBootCoreB64    string
	}{
		cloudInitData:       data,
		LairKeystoreUnitB64: lairUnitB64,
		HolochainUnitB64:    holochainUnitB64,
		FirstBootCoreB64:    firstBootCoreB64,
	}

	tmpl, err := template.New("cloud-init").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("failed to parse cloud-init template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tmplData); err != nil {
		return "", fmt.Errorf("failed to render cloud-init template: %w", err)
	}
	return buf.String(), nil
}

// readBaseFileB64 reads a shared base-image file (cloudinit/base/) and returns
// its base64 encoding for injection into a cloud-init `encoding: b64`
// write_files entry.
func readBaseFileB64(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read base-image file %s: %w", path, err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
