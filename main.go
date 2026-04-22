package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"text/template"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type cloudInitData struct {
	InfluxToken string
}

var regions = []digitalocean.Region{
	digitalocean.RegionAMS3,
	digitalocean.RegionNYC2,
	digitalocean.RegionSFO2,
	digitalocean.RegionBLR1,
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		projectName, ok := ctx.GetConfig("heart:project-name")
		if !ok {
			return fmt.Errorf("required config value 'heart:project-name' not set")
		}

		project, err := digitalocean.LookupProject(ctx, &digitalocean.LookupProjectArgs{
			Name: &projectName,
		}, nil)
		if err != nil {
			return err
		}

		allDefaultDropletUrns, err := createDefault(ctx)
		if err != nil {
			return err
		}

		allAltDropletURNs, err := createAlt(ctx)
		if err != nil {
			return err
		}

		allDropletURNs := append(allDefaultDropletUrns, allAltDropletURNs...)
		if len(allDropletURNs) > 0 {
			_, err = digitalocean.NewProjectResources(ctx, "heart-project-resources", &digitalocean.ProjectResourcesArgs{
				Project:   pulumi.String(project.Id),
				Resources: allDropletURNs,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func createDefault(ctx *pulumi.Context) (pulumi.StringArray, error) {
	cloudInitRaw, err := os.ReadFile("cloudinit/default/cloud-config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read default cloud-init config: %w", err)
	}
	cloudInitTmpl, err := template.New("cloud-init").Parse(string(cloudInitRaw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse cloud-init template: %w", err)
	}

	influxToken, ok := ctx.GetConfig("heart:influx-token")
	if !ok {
		return nil, fmt.Errorf("required config value 'influx-token' not set")
	}

	var cloudInitBuf bytes.Buffer
	if err := cloudInitTmpl.Execute(&cloudInitBuf, cloudInitData{
		InfluxToken: influxToken,
	}); err != nil {
		return nil, fmt.Errorf("failed to render cloud-init template: %w", err)
	}
	defaultCloudInit := cloudInitBuf.String()

	getSshKeysResult, err := digitalocean.GetSshKeys(ctx, &digitalocean.GetSshKeysArgs{}, nil)
	if err != nil {
		return nil, err
	}

	var sshFingerprints []string
	for _, key := range getSshKeysResult.SshKeys {
		sshFingerprints = append(sshFingerprints, key.Fingerprint)
	}

	// heart-always-online droplets
	alwaysOnlineCountStr, ok := ctx.GetConfig("heart:heart-always-online-count")
	if !ok {
		alwaysOnlineCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:heart-always-online-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	alwaysOnlineCount, err := strconv.Atoi(alwaysOnlineCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:heart-always-online-count'=%q: %w", alwaysOnlineCountStr, err)
	}

	var alwaysOnlineURNs pulumi.StringArray
	for i := 1; i <= alwaysOnlineCount; i++ {
		name := fmt.Sprintf("heart-always-online-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS2VCPU4GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("heart-always-online")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			BackupPolicy: &digitalocean.DropletBackupPolicyArgs{
				Plan:    pulumi.String("weekly"),
				Weekday: pulumi.String("TUE"),
				Hour:    pulumi.Int(8),
			},
			UserData: pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		alwaysOnlineURNs = append(alwaysOnlineURNs, droplet.DropletUrn)
	}

	// blockchain-bridging droplets
	blockchainBridgingCountStr, ok := ctx.GetConfig("heart:blockchain-bridging-count")
	if !ok {
		blockchainBridgingCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:blockchain-bridging-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	blockchainBridgingCount, err := strconv.Atoi(blockchainBridgingCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:blockchain-bridging-count'=%q: %w", blockchainBridgingCountStr, err)
	}
	if blockchainBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:blockchain-bridging-count' cannot be greater than 1, got %d", blockchainBridgingCount)
	}

	var blockchainBridgingURNs pulumi.StringArray
	for i := 1; i <= blockchainBridgingCount; i++ {
		name := fmt.Sprintf("blockchain-bridging-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS4VCPU8GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("blockchain-bridging")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		blockchainBridgingURNs = append(blockchainBridgingURNs, droplet.DropletUrn)
	}

	// unyt-bridging droplets
	unytBridgingCountStr, ok := ctx.GetConfig("heart:unyt-bridging-count")
	if !ok {
		unytBridgingCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:unyt-bridging-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	unytBridgingCount, err := strconv.Atoi(unytBridgingCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:unyt-bridging-count'=%q: %w", unytBridgingCountStr, err)
	}
	if unytBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:unyt-bridging-count' cannot be greater than 1, got %d", unytBridgingCount)
	}

	var unytBridgingURNs pulumi.StringArray
	for i := 1; i <= unytBridgingCount; i++ {
		name := fmt.Sprintf("unyt-bridging-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS4VCPU8GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("unyt-bridging")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		unytBridgingURNs = append(unytBridgingURNs, droplet.DropletUrn)
	}

	allDropletURNs := append(alwaysOnlineURNs, append(blockchainBridgingURNs, unytBridgingURNs...)...)

	return allDropletURNs, nil
}

func createAlt(ctx *pulumi.Context) (pulumi.StringArray, error) {
	cloudInitRaw, err := os.ReadFile("cloudinit/alt/cloud-config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read alt cloud-init config: %w", err)
	}
	cloudInitTmpl, err := template.New("cloud-init").Parse(string(cloudInitRaw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse cloud-init template: %w", err)
	}

	influxToken, ok := ctx.GetConfig("heart:influx-token")
	if !ok {
		return nil, fmt.Errorf("required config value 'influx-token' not set")
	}

	var cloudInitBuf bytes.Buffer
	if err := cloudInitTmpl.Execute(&cloudInitBuf, cloudInitData{
		InfluxToken: influxToken,
	}); err != nil {
		return nil, fmt.Errorf("failed to render cloud-init template: %w", err)
	}
	defaultCloudInit := cloudInitBuf.String()

	getSshKeysResult, err := digitalocean.GetSshKeys(ctx, &digitalocean.GetSshKeysArgs{}, nil)
	if err != nil {
		return nil, err
	}

	var sshFingerprints []string
	for _, key := range getSshKeysResult.SshKeys {
		sshFingerprints = append(sshFingerprints, key.Fingerprint)
	}

	// heart-always-online droplets
	alwaysOnlineCountStr, ok := ctx.GetConfig("heart:heart-always-online-alt-count")
	if !ok {
		alwaysOnlineCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:heart-always-online-alt-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	alwaysOnlineCount, err := strconv.Atoi(alwaysOnlineCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:heart-always-online-alt-count'=%q: %w", alwaysOnlineCountStr, err)
	}

	var alwaysOnlineURNs pulumi.StringArray
	for i := 1; i <= alwaysOnlineCount; i++ {
		name := fmt.Sprintf("heart-always-online-alt-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS2VCPU4GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("heart-always-online-alt")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			BackupPolicy: &digitalocean.DropletBackupPolicyArgs{
				Plan:    pulumi.String("weekly"),
				Weekday: pulumi.String("TUE"),
				Hour:    pulumi.Int(8),
			},
			UserData: pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		alwaysOnlineURNs = append(alwaysOnlineURNs, droplet.DropletUrn)
	}

	// blockchain-bridging droplets
	blockchainBridgingCountStr, ok := ctx.GetConfig("heart:blockchain-bridging-alt-count")
	if !ok {
		blockchainBridgingCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:blockchain-bridging-alt-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	blockchainBridgingCount, err := strconv.Atoi(blockchainBridgingCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:blockchain-bridging-alt-count'=%q: %w", blockchainBridgingCountStr, err)
	}
	if blockchainBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:blockchain-bridging-alt-count' cannot be greater than 1, got %d", blockchainBridgingCount)
	}

	var blockchainBridgingURNs pulumi.StringArray
	for i := 1; i <= blockchainBridgingCount; i++ {
		name := fmt.Sprintf("blockchain-bridging-alt-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS4VCPU8GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("blockchain-bridging-alt")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		blockchainBridgingURNs = append(blockchainBridgingURNs, droplet.DropletUrn)
	}

	// unyt-bridging droplets
	unytBridgingCountStr, ok := ctx.GetConfig("heart:unyt-bridging-alt-count")
	if !ok {
		unytBridgingCountStr = "1"
		if err := ctx.Log.Info("config value 'heart:unyt-bridging-alt-count' not found, defaulting to 1", nil); err != nil {
			return nil, err
		}
	}
	unytBridgingCount, err := strconv.Atoi(unytBridgingCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid config value 'heart:unyt-bridging-alt-count'=%q: %w", unytBridgingCountStr, err)
	}
	if unytBridgingCount > 1 {
		// The software does not support this
		return nil, fmt.Errorf("config value 'heart:unyt-bridging-alt-count' cannot be greater than 1, got %d", unytBridgingCount)
	}

	var unytBridgingURNs pulumi.StringArray
	for i := 1; i <= unytBridgingCount; i++ {
		name := fmt.Sprintf("unyt-bridging-alt-%d", i)
		droplet, err := digitalocean.NewDroplet(ctx, name, &digitalocean.DropletArgs{
			Image:      pulumi.String("ubuntu-24-04-x64"),
			Name:       pulumi.String(name),
			Region:     pulumi.String(regions[i%len(regions)]),
			Size:       pulumi.String(digitalocean.DropletSlugDropletS4VCPU8GB),
			Ipv6:       pulumi.Bool(true),
			Tags:       pulumi.StringArray{pulumi.String("unyt-bridging-alt")},
			SshKeys:    pulumi.ToStringArray(sshFingerprints),
			Monitoring: pulumi.Bool(true),
			Backups:    pulumi.Bool(true),
			UserData:   pulumi.String(defaultCloudInit),
		}, pulumi.IgnoreChanges([]string{"sshKeys"}))
		if err != nil {
			return nil, err
		}
		unytBridgingURNs = append(unytBridgingURNs, droplet.DropletUrn)
	}

	allAltDropletURNs := append(alwaysOnlineURNs, append(blockchainBridgingURNs, unytBridgingURNs...)...)

	return allAltDropletURNs, nil
}
