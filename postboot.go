package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi-command/sdk/go/command/remote"
	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// automationDir is where the unytco/automation checkout lives relative to the
// heart Pulumi program (the workshop sibling layout). Override with
// heart:automation-dir for non-standard checkouts.
const defaultAutomationDir = "../automation"

// provisionPostBoot drives the post-boot deploy from Pulumi via the command
// provider. For each server that has an automation config it:
//
//  1. runs deploy.sh   (reset + install the released .happ + agents)   [local→script]
//  2. waits for the EXTERNAL agent-key approval                        [remote poll]
//  3. (public boxes) installs/refreshes hc-http-gw                     [local→script]
//  4. (public boxes) streams the Pulumi-owned tunnel credentials +     [local→script]
//     brings up the cloudflared connector
//  5. (public boxes) captures the live DNA hash and registers it       [remote + local]
//
// It INVOKES the existing automation scripts rather than reimplementing their
// phases; the only new contract is the HEART_FLEET_IP env var (the
// Pulumi-resolved droplet IP), which the automation resolver prefers over the
// config's literal ip. Idempotent: triggers re-run a step only when its inputs
// change; an unchanged `pulumi up` is a no-op.
//
// The step at (2) is the one place a `pulumi up` blocks on a human: a fleet
// admin must approve the node's key on the external auth server. Pulumi can
// only wait, never approve.
func provisionPostBoot(
	ctx *pulumi.Context,
	fleet []Server,
	droplets map[string]*digitalocean.Droplet,
	tunnels map[string]*tunnelInfo,
	releaseVersion string,
) error {
	cfg := config.New(ctx, "heart")
	sshKey := cfg.GetSecret("ssh-private-key") // pulumi.StringOutput; empty if unset
	sshUser := "root"

	autoDir := defaultAutomationDir
	if v, ok := ctx.GetConfig("heart:automation-dir"); ok && v != "" {
		autoDir = v
	}

	for _, srv := range fleet {
		droplet, ok := droplets[srv.Name]
		if !ok {
			continue
		}
		// Each server's automation config lives at
		// <automation>/config/<name>/deploy.json. Skip servers without one
		// (they're provisioned but not yet wired for orchestrated deploy).
		cfgPath := filepath.Join(autoDir, "config", srv.Name, "deploy.json")
		if _, err := os.Stat(cfgPath); err != nil {
			_ = ctx.Log.Info(fmt.Sprintf("postboot: no automation config at %s; skipping orchestration for %s", cfgPath, srv.Name), nil)
			continue
		}

		ip := droplet.Ipv4Address
		conn := &remote.ConnectionArgs{
			Host:       ip,
			User:       pulumi.String(sshUser),
			PrivateKey: sshKey,
		}
		// Env shared by the local→script commands: the Pulumi-resolved IP and
		// the SSH key (the scripts ssh out themselves).
		scriptEnv := pulumi.StringMap{
			"HEART_FLEET_IP":  ip,
			"SSH_PRIVATE_KEY": sshKey,
		}

		// (0) Wait for first-boot cloud-init to finish before any deploy step.
		// The droplet resource is "created" the moment the VM exists, but
		// Holochain/lair/hc install over several minutes via cloud-init — running
		// deploy.sh before that yields "hc: command not found" /
		// "holochain.service not found" / SSH "connection closed".
		cloudInitReady, err := remote.NewCommand(ctx, srv.Name+"-await-cloudinit", &remote.CommandArgs{
			Connection: conn,
			Create: pulumi.String(`set -uo pipefail
echo "waiting for cloud-init + holochain install to finish..."
sudo cloud-init status --wait >/dev/null 2>&1 || true
deadline=$(( $(date +%s) + 1800 ))
until command -v hc >/dev/null 2>&1 && systemctl list-unit-files holochain.service >/dev/null 2>&1; do
  if [ "$(date +%s)" -ge "$deadline" ]; then echo "cloud-init/holochain not ready after 30m" >&2; exit 75; fi
  sleep 15
done
echo "cloud-init complete; hc + holochain.service present."`),
		}, pulumi.DependsOn([]pulumi.Resource{droplet}))
		if err != nil {
			return fmt.Errorf("postboot %s: cloud-init wait: %w", srv.Name, err)
		}

		// (1) deploy.sh — reset + install the released .happ + agents.
		deployTriggers := pulumi.Array{ip}
		if releaseVersion != "" {
			deployTriggers = append(deployTriggers, pulumi.String(releaseVersion))
		}
		deployCmd, err := local.NewCommand(ctx, srv.Name+"-deploy", &local.CommandArgs{
			Dir:         pulumi.String(autoDir),
			Create:      pulumi.Sprintf("bash scripts/deploy.sh --config config/%s/deploy.json", srv.Name),
			Environment: scriptEnv,
			Triggers:    deployTriggers,
		}, pulumi.DependsOn([]pulumi.Resource{cloudInitReady}))
		if err != nil {
			return fmt.Errorf("postboot %s: deploy command: %w", srv.Name, err)
		}

		// (2) External approval gate. holochain-register on the droplet polls
		// the auth server until a human approves; we block until its journal
		// shows completion (resumable: re-run after approval succeeds fast).
		approval, err := remote.NewCommand(ctx, srv.Name+"-await-approval", &remote.CommandArgs{
			Connection: conn,
			Create: pulumi.String(`set -euo pipefail
echo "Waiting for external admin to approve this node's agent key (hc-auth-iroh-unyt)..."
deadline=$(( $(date +%s) + 3600 ))
until journalctl -u holochain-register.service --no-pager 2>/dev/null | grep -q "Registration complete"; do
  if [ "$(date +%s)" -ge "$deadline" ]; then echo "approval timed out after 60m" >&2; exit 75; fi
  sleep 30
done
echo "agent key approved; conductor registered."`),
		}, pulumi.DependsOn([]pulumi.Resource{deployCmd}))
		if err != nil {
			return fmt.Errorf("postboot %s: approval gate: %w", srv.Name, err)
		}

		// Non-public boxes stop here: provisioned, app installed, registered.
		if !srv.IsPublic() {
			continue
		}

		tun, ok := tunnels[srv.GatewayHostname]
		if !ok {
			_ = ctx.Log.Info(fmt.Sprintf("postboot: %s is public but no Pulumi tunnel for %q (cf-account-id unset?); skipping gateway/tunnel orchestration", srv.Name, srv.GatewayHostname), nil)
			continue
		}

		// (3) hc-http-gw install/refresh (build-from-source — no upstream binaries).
		gatewayCmd, err := local.NewCommand(ctx, srv.Name+"-gateway", &local.CommandArgs{
			Dir:         pulumi.String(autoDir),
			Create:      pulumi.Sprintf("bash scripts/setup-gateway.sh --config config/%s/deploy.json --build-from-source", srv.Name),
			Environment: scriptEnv,
			Triggers:    pulumi.Array{ip, pulumi.String(srv.Name)},
		}, pulumi.DependsOn([]pulumi.Resource{approval}))
		if err != nil {
			return fmt.Errorf("postboot %s: gateway command: %w", srv.Name, err)
		}

		// (4) Connector credentials (Pulumi-owned tunnel) → droplet, then bring
		// up cloudflared. DNS is owned by Pulumi (no `tunnel route dns` here).
		tunnelEnv := pulumi.StringMap{
			"HEART_FLEET_IP":           ip,
			"SSH_PRIVATE_KEY":          sshKey,
			"HEART_TUNNEL_CREDENTIALS": tun.CredentialsJSON, // secret
			"HEART_TUNNEL_ID":          tun.TunnelID,
			"HEART_GATEWAY_HOSTNAME":   pulumi.String(srv.GatewayHostname),
		}
		tunnelCmd, err := local.NewCommand(ctx, srv.Name+"-tunnel", &local.CommandArgs{
			Dir:         pulumi.String(autoDir),
			Create:      pulumi.Sprintf("bash scripts/setup-tunnel.sh --config config/%s/deploy.json", srv.Name),
			Environment: tunnelEnv,
			Triggers:    pulumi.Array{ip, tun.TunnelID},
		}, pulumi.DependsOn([]pulumi.Resource{gatewayCmd}))
		if err != nil {
			return fmt.Errorf("postboot %s: tunnel command: %w", srv.Name, err)
		}

		// (5) Capture the live DNA hash (only exists post-install) and register
		// it with the hash-explorer worker. The hash flows Command stdout →
		// Pulumi output → downstream Command input (which also orders them).
		listCells, err := remote.NewCommand(ctx, srv.Name+"-list-cells", &remote.CommandArgs{
			Connection: conn,
			Create:     pulumi.String(`hc sandbox call --running 8800 list-cells 2>/dev/null | grep -oE 'uhC0k[A-Za-z0-9_-]+' | head -n1`),
		}, pulumi.DependsOn([]pulumi.Resource{tunnelCmd}))
		if err != nil {
			return fmt.Errorf("postboot %s: list-cells: %w", srv.Name, err)
		}
		dnaHash := listCells.Stdout
		ctx.Export(srv.Name+"-dna-hash", dnaHash)
	}

	return nil
}
