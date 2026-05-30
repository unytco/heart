package main

import (
	"fmt"
	"sort"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
)

// Service is a workload a heart droplet hosts. A server's PRIMARY service
// gives it its name; co-located services (Server.Extra) are additional
// workloads on the same box. Every box implicitly runs the conductor; the
// hash-explorer primary additionally implies the gateway + tunnel stack.
//
// This taxonomy is intentionally coarse: it only distinguishes what affects
// provisioning (droplet name, size, region, tags, and whether a public
// ingress is needed). Finer-grained units that automation installs on a box
// (the pricing-oracle cron, the watchtower observer, the hf-swap timer) are
// carried as Extra tags so `pulumi stack output fleet` can tell automation
// which box runs them — they do not change the droplet itself.
type Service string

const (
	// Primaries — a server is named for exactly one of these.
	ServiceAlwaysOnline  Service = "always-online"   // always-on unyt app conductor (network availability)
	ServiceHashExplorer  Service = "hash-explorer"   // unyt app conductor fronted by hc-http-gw + tunnel
	ServiceHotBridge     Service = "hot-bridge"      // HOT <-> mHOT blockchain bridge + UI
	ServiceHFInfraBridge Service = "hf-infra-bridge" // HF <-> infra unyt bridge

	// Implied / capability services (derived from the primary, not named).
	ServiceConductor Service = "conductor" // holochain + lair + register — every box
	ServiceGateway   Service = "gateway"   // hc-http-gw on 127.0.0.1:8090
	ServiceTunnel    Service = "tunnel"    // cloudflared connector

	// Co-located extras — installed by automation on a box whose primary is
	// something else. Surfaced as tags for discovery, not provisioning logic.
	ServicePricingOracle Service = "pricing-oracle"
	ServiceWatchtower    Service = "watchtower"

	// Primary in its own right — hf-swap runs on a dedicated box (hf-swapping)
	// rather than co-located, so services keep to their own servers.
	ServiceHFSwap Service = "hf-swap"
)

// publicServices are primaries that need a public ingress and therefore
// imply the gateway + tunnel supporting stack on their box.
var publicServices = map[Service]bool{
	ServiceHashExplorer: true,
}

// singleInstanceServices may run on at most one server per fleet — the
// underlying software does not support more than one. This preserves the
// `>1` guard the old count-based main.go enforced for the bridges.
var singleInstanceServices = map[Service]bool{
	ServiceHotBridge:     true,
	ServiceHFInfraBridge: true,
}

// Server is one droplet, named for its primary service.
type Server struct {
	Name    string                   // Pulumi logical name + DO droplet Name, e.g. "hash-explorer-1"
	Primary Service                  // the service this box exists for; drives Name + the first tag
	Extra   []Service                // co-located workloads beyond the implied stack
	Size    digitalocean.DropletSlug // DO size slug
	Region  digitalocean.Region      // DO region

	// GatewayHostname is the public hostname fronting this box's hc-http-gw,
	// e.g. "unyt-tunnel.unyt.co". Required iff the box runs the tunnel; the
	// Cloudflare tunnel + CNAME are declared per distinct hostname. Empty for
	// non-public boxes.
	GatewayHostname string
	// GatewayApps are the installed app-ids this box's gateway fronts.
	// Informational — exported so automation can render the gateway env.
	GatewayApps []string
}

// Services returns the full sorted service set the box runs: conductor +
// primary + (gateway, tunnel if the primary is public) + extras.
func (s Server) Services() []Service {
	set := map[Service]bool{ServiceConductor: true, s.Primary: true}
	if publicServices[s.Primary] {
		set[ServiceGateway] = true
		set[ServiceTunnel] = true
	}
	for _, e := range s.Extra {
		set[e] = true
	}
	out := make([]Service, 0, len(set))
	for svc := range set {
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsPublic reports whether the box runs the cloudflared tunnel connector
// (i.e. its primary needs a public ingress).
func (s Server) IsPublic() bool { return publicServices[s.Primary] }

// Tags returns the DO tags for firewall / project grouping / discovery:
// "service:<primary>" plus one tag per active service.
func (s Server) Tags() []string {
	tags := []string{"service:" + string(s.Primary)}
	for _, svc := range s.Services() {
		tags = append(tags, string(svc))
	}
	return tags
}

// Fleet is the single declarative source of truth for the deployment
// topology. One entry == one droplet. Edit this slice to add, remove,
// resize, or relocate a server.
//
// Per-version note: a fleet is materialized per app-version release via a
// dedicated Pulumi stack (heart:release-version). Names stay short (the
// version namespaces via the per-version DO project + stack), and a
// "version:<release>" tag is added by main.go at droplet-creation time.
//
// Confirmed with the user (2026-05-29): two always-online nodes whose job is to
// run watchtower observers; hf-swap on its own dedicated box (hf-swapping);
// one hash-explorer box; and the two existing bridge boxes keeping their
// current names for now. Old heart-always-online-4 is not carried over.
// Principle: keep each service on its own server.
var Fleet = []Server{
	{
		// was heart-always-online-1.
		Name: "always-online-1", Primary: ServiceAlwaysOnline,
		Extra:  []Service{ServiceWatchtower},
		Size:   digitalocean.DropletSlugDropletS2VCPU4GB,
		Region: digitalocean.RegionAMS3,
	},
	{
		// was heart-always-online-2.
		Name: "always-online-2", Primary: ServiceAlwaysOnline,
		Extra:  []Service{ServiceWatchtower},
		Size:   digitalocean.DropletSlugDropletS2VCPU4GB,
		Region: digitalocean.RegionNYC2,
	},
	{
		// Dedicated hf-swap box (was co-located on always-online-1).
		Name: "hf-swapping", Primary: ServiceHFSwap,
		Size:   digitalocean.DropletSlugDropletS2VCPU4GB,
		Region: digitalocean.RegionBLR1,
	},
	{
		// was heart-always-online-3 — the only box running tunnel + gateway.
		Name: "hash-explorer-1", Primary: ServiceHashExplorer,
		Size:            digitalocean.DropletSlugDropletS2VCPU4GB,
		Region:          digitalocean.RegionSFO2,
		GatewayHostname: "unyt-tunnel.unyt.co",
		GatewayApps:     []string{"always-on-node"},
	},
	{
		// name kept as-is for now (user request).
		Name: "hot-2-mhot-bridge", Primary: ServiceHotBridge,
		Extra:  []Service{ServicePricingOracle},
		Size:   digitalocean.DropletSlugDropletS4VCPU8GB,
		Region: digitalocean.RegionNYC2,
	},
	{
		// name kept as-is for now (user request).
		Name: "hf-2-infra-bridge", Primary: ServiceHFInfraBridge,
		Size:   digitalocean.DropletSlugDropletS4VCPU8GB,
		Region: digitalocean.RegionSFO2,
	},
}

// validateFleet fails the program (before any resource is declared) on a
// malformed manifest: duplicate names, a public box missing its hostname, or
// more than one instance of a single-instance service. Run at the top of main.
func validateFleet(fleet []Server) error {
	seenName := map[string]bool{}
	instanceCount := map[Service]int{}
	for _, s := range fleet {
		if s.Name == "" {
			return fmt.Errorf("fleet entry with empty Name (primary %q)", s.Primary)
		}
		if seenName[s.Name] {
			return fmt.Errorf("duplicate server name %q in Fleet", s.Name)
		}
		seenName[s.Name] = true

		if s.Primary == "" {
			return fmt.Errorf("server %q has no Primary service", s.Name)
		}
		if s.IsPublic() && s.GatewayHostname == "" {
			return fmt.Errorf("server %q runs a public service (%s) but has no GatewayHostname", s.Name, s.Primary)
		}
		for _, svc := range s.Services() {
			instanceCount[svc]++
		}
	}
	for svc := range singleInstanceServices {
		if instanceCount[svc] > 1 {
			return fmt.Errorf("service %q is single-instance but %d servers run it (the software does not support more than one)", svc, instanceCount[svc])
		}
	}
	return nil
}
