package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gopkg.in/yaml.v3"
)

func TestValidateRelease(t *testing.T) {
	valid := []string{"v0-7-0", "v1", "release_2", "abc-123_X"}
	for _, r := range valid {
		if err := validateRelease(r); err != nil {
			t.Errorf("validateRelease(%q) = %v, want nil", r, err)
		}
	}

	invalid := []string{"v0.7.0", "", "v 1", "rel:1", "héllo", "a/b"}
	for _, r := range invalid {
		if err := validateRelease(r); err == nil {
			t.Errorf("validateRelease(%q) = nil, want error", r)
		}
	}
}

func TestIpsKey(t *testing.T) {
	cases := map[string]struct {
		nt   nodeType
		i    int
		want string
	}{
		"always-online":    {nodeType{name: "heart-always-online"}, 1, "heart-always-online-1"},
		"always-online-2":  {nodeType{name: "heart-always-online"}, 2, "heart-always-online-2"},
		"singleton bridge": {nodeType{name: "blockchain-bridging", maxCount: 1}, 1, "blockchain-bridging-1"},
		"hash-explorer":    {nodeType{name: "hash-explorer"}, 1, "hash-explorer-1"},
	}
	for name, c := range cases {
		if got := ipsKey(c.nt, c.i); got != c.want {
			t.Errorf("%s: ipsKey(%q, %d) = %q, want %q", name, c.nt.name, c.i, got, c.want)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	defaults, err := loadDefaults(defaultsFile)
	if err != nil {
		t.Fatalf("loadDefaults(%q) = %v", defaultsFile, err)
	}

	// Every optional key createFleet reads must have a default, or a stack that
	// omits it would fail at `pulumi up`.
	wantKeys := []string{
		"holochain-version", "holo-keyutil-version",
		"bootstrap-url", "relay-url", "auth-server",
		"influx-url", "influx-org", "influx-bucket",
	}
	// The per-node-type size/count keys are derived from the registry so this
	// test covers every server type automatically.
	for _, nt := range nodeTypes {
		wantKeys = append(wantKeys, nt.sizeKey, nt.countKey)
	}
	for _, k := range wantKeys {
		if _, ok := defaults[k]; !ok {
			t.Errorf("%s is missing key %q", defaultsFile, k)
		}
	}

	// Every node type's count must parse as an integer.
	for _, nt := range nodeTypes {
		if _, err := strconv.Atoi(defaults[nt.countKey]); err != nil {
			t.Errorf("%s[%q]=%q is not an integer: %v", defaultsFile, nt.countKey, defaults[nt.countKey], err)
		}
	}

	// The conductor-config allow-lists are a snapshot of ONE Holochain version's
	// serde contract. TestConductorConfigKeysMatchHolochainSchema checks them
	// against a real binary, but it skips where there is none - which is CI - so
	// on its own a bump here would leave them silently describing the wrong
	// version, and a key 0.7 allowed but 0.8 removed would reach a droplet rather
	// than a test. Failing on the pin is what always forces the re-derive.
	if want := "holochain-0.7."; !strings.HasPrefix(defaults["holochain-version"], want) {
		t.Errorf("%s[%q]=%q is no longer a %sx release: re-derive conductorConfigKeys07, networkConfigKeys07, adminInterfaceKeys07, keystoreKeys07, websocketDriverKeys07 and dbSyncLevels07 against that version (run TestConductorConfigKeysMatchHolochainSchema with HEART_HOLOCHAIN_BIN set to its binary), then update this check",
			defaultsFile, "holochain-version", defaults["holochain-version"], want)
	}

	// A default is interpolated into cloud-init exactly as a stack value is, so
	// one carrying a rejected byte only surfaces at `make preview` against a live
	// stack - the failure this screen exists to move earlier. Checked here so
	// editing defaults.yaml is what fails, not the next deploy.
	for k, v := range defaults {
		if v == "" {
			t.Errorf("%s[%q] is empty, which renderCloudInit rejects", defaultsFile, k)
		}
		if i := firstUnsafeValueByte(v); i >= 0 {
			t.Errorf("%s[%q] has byte %#x at position %d, which renderCloudInit rejects", defaultsFile, k, v[i], i)
		}
		if strings.TrimSpace(v) != v {
			t.Errorf("%s[%q] begins or ends with whitespace, which renderCloudInit rejects", defaultsFile, k)
		}
	}
}

// validCloudInitData is the baseline every render test starts from: a complete
// set of values renderCloudInit accepts. Complete because the screen rejects an
// empty value, so a fixture that sets one field and leaves the rest zero would
// abort on whichever field it forgot rather than on the thing under test. Every
// value is distinct, so an assertion cannot be satisfied by the wrong field.
func validCloudInitData() cloudInitData {
	return cloudInitData{
		HolochainVersion:   "holochain-0.7.0",
		HoloKeyutilVersion: "v0.2.0",
		BootstrapURL:       "https://bootstrap.example",
		RelayURL:           "https://relay.example",
		AuthServer:         "https://auth.example",
		InfluxURL:          "https://influx.example",
		InfluxOrg:          "org123",
		InfluxBucket:       "unyt",
		InfluxToken:        "tok-example",
	}
}

// TestValidCloudInitDataIsDistinct pins the half of validCloudInitData's
// contract that three assertion groups quietly lean on. Those groups check that
// each value landed under its OWN key, which is only a swap check while the
// values differ: two fields sharing one would satisfy each other's assertion and
// disable the detection with the suite still green.
func TestValidCloudInitDataIsDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, f := range validCloudInitData().fields() {
		if prev, dup := seen[f.val]; dup {
			t.Errorf("'heart:%s' and 'heart:%s' share the value %q, which disables swap detection", prev, f.key, f.val)
		}
		seen[f.val] = f.key
	}
}

// TestRenderCloudInit renders the cloud-config with representative data and
// checks the invariants that matter for provisioning: every template action is
// resolved, the output is valid cloud-init YAML, and the shared base-image files
// (cloudinit/base/) are injected byte-for-byte so they cannot drift from the copy
// the local-testnet fleet image COPYs. It also pins the prod-vs-local split: the
// InfluxDB metrics env is a drop-in, not part of the shared base unit.
func TestRenderCloudInit(t *testing.T) {
	data := validCloudInitData()
	out, err := renderCloudInit(data)
	if err != nil {
		t.Fatalf("renderCloudInit: %v", err)
	}

	// Every template action must have rendered.
	if strings.Contains(out, "{{") {
		t.Errorf("rendered cloud-init still contains an unrendered template action")
	}

	// It must be valid cloud-init YAML with the expected write_files.
	var parsed struct {
		WriteFiles []struct {
			Path     string `yaml:"path"`
			Encoding string `yaml:"encoding"`
			Content  string `yaml:"content"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("rendered cloud-init is not valid YAML: %v", err)
	}
	files := map[string]struct{ enc, content string }{}
	for _, wf := range parsed.WriteFiles {
		files[wf.Path] = struct{ enc, content string }{wf.Encoding, wf.Content}
	}

	// The shared base-image files must be injected verbatim (base64 of the
	// cloudinit/base/ source), so prod and the fleet cannot drift apart.
	for _, tc := range []struct{ path, src string }{
		{"/etc/systemd/system/lair-keystore.service", "cloudinit/base/lair-keystore.service"},
		{"/etc/systemd/system/holochain.service", "cloudinit/base/holochain.service"},
		{"/usr/local/bin/holochain-first-boot-core", "cloudinit/base/first-boot-core.sh"},
	} {
		f, ok := files[tc.path]
		if !ok {
			t.Errorf("rendered cloud-init is missing write_files entry %s", tc.path)
			continue
		}
		if f.enc != "b64" {
			t.Errorf("%s: encoding = %q, want b64", tc.path, f.enc)
		}
		decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(f.content))
		if derr != nil {
			t.Errorf("%s: content is not valid base64: %v", tc.path, derr)
			continue
		}
		want, rerr := os.ReadFile(tc.src)
		if rerr != nil {
			t.Fatalf("read %s: %v", tc.src, rerr)
		}
		if string(decoded) != string(want) {
			t.Errorf("%s: injected content is not byte-for-byte %s", tc.path, tc.src)
		}
	}

	// The InfluxDB metrics env is a prod-only drop-in — keeping it out of the base
	// unit is exactly what lets the local fleet reuse that unit unchanged.
	base := files["/etc/systemd/system/holochain.service"]
	baseDecoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(base.content))
	if derr != nil {
		// Without this the Contains below is vacuously satisfied by "".
		t.Fatalf("base holochain.service is not valid base64: %v", derr)
	}
	if strings.Contains(string(baseDecoded), "HOLOCHAIN_INFLUXIVE_EXTERNAL") {
		t.Error("base holochain.service must not carry InfluxDB env (it belongs in the drop-in)")
	}
	dropin, ok := files["/etc/systemd/system/holochain.service.d/10-metrics.conf"]
	if !ok {
		t.Fatal("missing holochain.service.d/10-metrics.conf drop-in")
	}
	// Each value under its OWN key, not merely present somewhere: these are three
	// adjacent Environment= lines built from three adjacent config reads, which is
	// where a copy-paste puts the bucket in the host's slot. The fixture's values
	// are distinct precisely so a swap cannot satisfy this.
	for env, want := range map[string]string{
		"HOLOCHAIN_INFLUXIVE_EXTERNAL_HOST":   data.InfluxURL,
		"HOLOCHAIN_INFLUXIVE_EXTERNAL_BUCKET": data.InfluxBucket,
		"HOLOCHAIN_INFLUXIVE_EXTERNAL_TOKEN":  data.InfluxToken,
	} {
		if line := fmt.Sprintf("%s=%s\"", env, want); !strings.Contains(dropin.content, line) {
			t.Errorf("metrics drop-in has no %q; it reads:\n%s", line, dropin.content)
		}
	}

	// Same shape for telegraf.conf, whose four values come from the same block.
	telegraf, ok := files["/etc/telegraf/telegraf.conf"]
	if !ok {
		t.Fatal("missing /etc/telegraf/telegraf.conf")
	}
	for key, want := range map[string]string{
		"urls":         fmt.Sprintf("[%q]", data.InfluxURL),
		"token":        fmt.Sprintf("%q", data.InfluxToken),
		"organization": fmt.Sprintf("%q", data.InfluxOrg),
		"bucket":       fmt.Sprintf("%q", data.InfluxBucket),
	} {
		if line := fmt.Sprintf("%s = %s", key, want); !strings.Contains(telegraf.content, line) {
			t.Errorf("telegraf.conf has no %q", line)
		}
	}

	// And the two version assignments, adjacent in the same first-boot script.
	firstBoot, ok := files["/usr/local/bin/holochain-first-boot"]
	if !ok {
		t.Fatal("missing /usr/local/bin/holochain-first-boot")
	}
	for env, want := range map[string]string{
		"HOLOCHAIN_VERSION":    data.HolochainVersion,
		"HOLO_KEYUTIL_VERSION": data.HoloKeyutilVersion,
	} {
		if line := fmt.Sprintf("%s=%q", env, want); !strings.Contains(firstBoot.content, line) {
			t.Errorf("holochain-first-boot has no %q", line)
		}
	}
}

// TestFirstUnsafeByte covers the byte classes cloud-init's YAML loader rejects.
// The non-ASCII half is the one that took the fleet down; the C0 controls and DEL
// fail the same way ("control characters are not allowed") and are just as easy to
// paste in - an ANSI escape copied out of colourised CLI output carries 0x1b.
func TestFirstUnsafeByte(t *testing.T) {
	// The last one is the document half of the value rule: the assembled
	// user-data is full of quotes, backslashes, '$' and backticks - they are the
	// shell and the quoting itself - so only the value predicate rejects them.
	safe := []string{"", "#cloud-config\nruncmd:\n  - echo hi\n", "tabs\tand\r\nnewlines", "V=\"$(id -u)\" # a\\b `date`\n"}
	for _, s := range safe {
		if i := firstUnsafeByte(s); i >= 0 {
			t.Errorf("firstUnsafeByte(%q) = %d, want -1", s, i)
		}
	}

	for _, b := range []byte{0x00, 0x01, 0x08, 0x0b, 0x0c, 0x1b, 0x7f, 0x80, 0xc2, 0xe2, 0xff} {
		s := "ab" + string([]byte{b}) + "cd"
		if i := firstUnsafeByte(s); i != 2 {
			t.Errorf("firstUnsafeByte(byte %#x at index 2) = %d, want 2", b, i)
		}
	}

	// Position cases: a UTF-8 BOM at the head is how this bug comes back (an
	// editor save, an iconv round-trip), and the live incident's byte sat 5361
	// bytes in, well past any prefix a truncated scan would cover.
	edges := map[string]struct {
		s    string
		want int
	}{
		"BOM at the head":    {"\xef\xbb\xbf#cloud-config\n", 0},
		"unsafe final byte":  {"# comment\xe2", 9},
		"deep in a big file": {strings.Repeat("# padding\n", 900) + "\xe2", 9000},
	}
	for name, c := range edges {
		if i := firstUnsafeByte(c.s); i != c.want {
			t.Errorf("%s: firstUnsafeByte = %d, want %d", name, i, c.want)
		}
	}
}

// TestFirstUnsafeValueByte pins the tighter rule for an interpolated config
// value. The document legitimately contains newlines; a value never does, and one
// that carries it ends its scalar early and breaks the YAML around it - the same
// whole-config-discarded outage, reached without a single non-ASCII byte. The
// five screened bytes break the same scalar the other way round: every value
// lands inside double quotes, which is what makes a '#' or a ': ' in a URL
// harmless, so it may not carry a byte that is still live in there. Which byte
// is live where differs per site - the map is on firstUnsafeValueByte.
func TestFirstUnsafeValueByte(t *testing.T) {
	// '#' and ':' are exactly what the quoting exists to carry, so they must
	// still pass the screen - rejecting them here would be the wrong fix.
	for name, s := range map[string]string{
		"clean URL": "https://auth.example/path",
		"fragment":  "https://auth.example/path#frag",
		"port":      "https://auth.example:8443/path",
	} {
		if i := firstUnsafeValueByte(s); i >= 0 {
			t.Errorf("%s: firstUnsafeValueByte(%q) = %d, want -1", name, s, i)
		}
	}

	for name, s := range map[string]string{
		"trailing newline": "https://auth.example\n",
		"embedded newline": "https://auth\nexample",
		"carriage return":  "https://auth.example\r",
		"tab":              "https://auth\texample",
		"double quote":     `https://auth.example/"`,
		"backslash":        `https://auth.example\x`,
		// The first-boot script assigns these inside double quotes and runs as
		// root, so a command substitution here is a boot-time shell, not a typo.
		"command substitution": `https://auth.example/$(id -u)`,
		"variable expansion":   `https://auth.example/${HOME}`,
		"backtick":             "https://auth.example/`id`",
		// A systemd specifier in Environment=: an unknown one makes systemd drop
		// the whole assignment, a known one expands. Percent-encoding in a URL is
		// the way in, and telegraf reads the same value from TOML where '%' is
		// inert - so the two metrics sinks would disagree rather than both fail.
		"percent encoding":  `https://influx.example/a%20b`,
		"systemd specifier": `https://influx.example/%H`,
	} {
		if i := firstUnsafeValueByte(s); i < 0 {
			t.Errorf("%s: firstUnsafeValueByte(%q) = -1, want an index", name, s)
		}
	}
}

// TestRenderCloudInitRejectsPaddedValue covers the two members of the bad-value
// family that carry no rejected byte at all. Padding is what quoting changed
// rather than fixed - an unquoted scalar trimmed a trailing space, a quoted one
// carries it to the conductor - and empty is the same `pulumi config set` slip
// with nothing in it: an empty holochain-version renders a release URL with a
// hole, first-boot's `curl -f` 404s under `set -eo pipefail`, and the droplet
// arrives with no conductor. Both reach the original outage without tripping the
// byte screen.
func TestRenderCloudInitRejectsPaddedValue(t *testing.T) {
	base := validCloudInitData()
	if _, err := renderCloudInit(base); err != nil {
		t.Fatalf("renderCloudInit(clean) = %v, want nil", err)
	}
	for name, padded := range map[string]string{
		"trailing space": "https://auth.example ",
		"leading space":  " https://auth.example",
	} {
		t.Run(name, func(t *testing.T) {
			data := base
			data.AuthServer = padded
			_, err := renderCloudInit(data)
			if err == nil {
				t.Fatal("renderCloudInit accepted a padded value; the node would use it verbatim")
			}
			if want := "config value 'heart:auth-server' must not begin or end with whitespace"; !strings.Contains(err.Error(), want) {
				t.Errorf("renderCloudInit error = %q, want it to contain %q", err, want)
			}
		})
	}

	// Every field, not just one: an empty value is a config slip that can land on
	// any of them, and each has its own boot-time consequence. Blanked through
	// the struct rather than fields() so a field the list forgot has no way to
	// look covered here. base's values are distinct, so the key lookup is exact.
	keyOf := map[string]string{}
	for _, f := range base.fields() {
		keyOf[f.val] = f.key
	}
	fieldsOf := reflect.ValueOf(&base).Elem()
	for i := range fieldsOf.NumField() {
		key, ok := keyOf[fieldsOf.Field(i).String()]
		if !ok {
			t.Fatalf("fields() does not list cloudInitData.%s", fieldsOf.Type().Field(i).Name)
		}
		data := base
		reflect.ValueOf(&data).Elem().Field(i).SetString("")
		_, err := renderCloudInit(data)
		if err == nil {
			t.Errorf("renderCloudInit accepted an empty 'heart:%s'; the node would fail at first boot", key)
			continue
		}
		if want := fmt.Sprintf("config value 'heart:%s' is empty", key); !strings.Contains(err.Error(), want) {
			t.Errorf("empty 'heart:%s': error = %q, want it to contain %q", key, err, want)
		}
	}

	// The messages report lengths, not the value: the same screens run over
	// heart:influx-token, whose padding must not print the token to find.
	data := base
	data.InfluxToken = "tok-example "
	_, err := renderCloudInit(data)
	if err == nil {
		t.Fatal("renderCloudInit accepted a padded token")
	}
	if strings.Contains(err.Error(), "tok-example") {
		t.Errorf("padded-value error leaked the token: %v", err)
	}
}

// TestRenderCloudInitConductorURLs pins what the CONDUCTOR receives, which is
// not what cloud-init receives: bootstrap and relay are interpolated into
// /etc/holochain/conductor-config.yaml, a second YAML document nested inside the
// user-data as a literal block scalar. A block scalar is literal, so a '#' or a
// ': ' in a URL is invisible to the outer parse - cloud-init writes the file out
// happily and only Holochain's own parse trips, on a node that then has no
// bootstrap peer and nothing saying why. Unquoted, ': ' makes the document
// unparseable ("mapping values are not allowed in this context") and a leading
// '#' comments the value away to null. Quoting is the fix; this checks the
// consumer gets the URL back, not the quotes with it.
func TestRenderCloudInitConductorURLs(t *testing.T) {
	base := validCloudInitData()
	for name, shape := range map[string]string{
		"plain":        "https://%s.example",
		"host port":    "https://%s.example:8443/path",
		"fragment":     "https://%s.example/path#frag",
		"colon space":  "https://%s.example/a: b",
		"leading hash": "#%s.example",
	} {
		t.Run(name, func(t *testing.T) {
			// Both at once, so a quote missed on either of the two template
			// lines fails here rather than hiding behind the other - and each
			// carries its own name, so a swap between the two adjacent lines
			// (bootstrap reading the relay URL, a fleet with no bootstrap peer)
			// fails too rather than matching by coincidence.
			want := map[string]string{
				"bootstrap_url": fmt.Sprintf(shape, "bootstrap"),
				"relay_url":     fmt.Sprintf(shape, "relay"),
			}
			data := base
			data.BootstrapURL = want["bootstrap_url"]
			data.RelayURL = want["relay_url"]

			var inner struct {
				Network struct {
					BootstrapURL string `yaml:"bootstrap_url"`
					RelayURL     string `yaml:"relay_url"`
				} `yaml:"network"`
			}
			renderConductorConfig(t, data, &inner)
			for field, got := range map[string]string{
				"bootstrap_url": inner.Network.BootstrapURL,
				"relay_url":     inner.Network.RelayURL,
			} {
				if got != want[field] {
					t.Errorf("%s: conductor reads %q, want the configured %q", field, got, want[field])
				}
			}
		})
	}
}

// conductorPath is the second YAML document nested in the user-data: written out
// by cloud-init, but parsed by Holochain, so its own validity is a separate
// question from the user-data's (see TestRenderCloudInitConductorURLs).
const conductorPath = "/etc/holochain/conductor-config.yaml"

// renderConductorConfig renders the cloud-config, pulls conductorPath's content
// back out of the write_files, and unmarshals it into the caller's value. Every
// failure along the way is fatal: a caller that got no config would otherwise
// assert against a zero value and pass.
func renderConductorConfig(t *testing.T, data cloudInitData, into any) {
	t.Helper()
	out, err := renderCloudInit(data)
	if err != nil {
		t.Fatalf("renderCloudInit: %v", err)
	}
	var parsed struct {
		WriteFiles []struct {
			Path    string `yaml:"path"`
			Content string `yaml:"content"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("rendered cloud-init is not valid YAML: %v", err)
	}
	conductor := ""
	for _, wf := range parsed.WriteFiles {
		if wf.Path == conductorPath {
			conductor = wf.Content
		}
	}
	if conductor == "" {
		t.Fatalf("rendered cloud-init has no %s write_files entry", conductorPath)
	}
	if err := yaml.Unmarshal([]byte(conductor), into); err != nil {
		t.Fatalf("%s is not valid YAML - the conductor would refuse to start: %v", conductorPath, err)
	}
}

// The keys Holochain 0.7 knows in each of the five mappings the template writes,
// derived from holochain_conductor_api 0.7.0 (src/config/conductor.rs,
// config/interface.rs, config/conductor/keystore_config.rs). Every one of those
// types is #[serde(deny_unknown_fields)], so these are allow-lists, not hints.
// TestConductorConfigKeysMatchHolochainSchema re-derives them from the released
// binary whenever one is on PATH, and TestLoadDefaults fails on a version bump -
// between them, a hand edit here that drifts from Holochain does not stay quiet.
//
// They describe a PRODUCTION build: the three kitsune disable_* fields on
// NetworkConfig are #[cfg(feature = "test-utils")], so a released holochain
// binary rejects them exactly as it would a typo, and listing them here would
// wave through a config no fleet can boot.
var (
	conductorConfigKeys07 = map[string]bool{
		"tracing_override": true, "wasm_backend": true, "data_root_path": true,
		"keystore": true, "admin_interfaces": true, "network": true,
		"db_sync_level": true, "db_max_readers": true,
		"incoming_request_concurrency_limit": true, "restore_chain_quorum": true,
		"tuning_params": true, "tracing_scope": true,
	}
	networkConfigKeys07 = map[string]bool{
		"base64_auth_material_bootstrap": true, "base64_auth_material_relay": true,
		"bootstrap_url": true, "relay_url": true, "request_timeout_s": true,
		"target_arc_factor": true, "report": true, "advanced": true,
	}
	adminInterfaceKeys07 = map[string]bool{"driver": true}
	// KeystoreConfig and InterfaceDriver are internally tagged enums: `type`
	// selects the variant, and only that variant's own fields may sit beside it.
	// So these two lists are only the right lists while the config still names
	// the variant they were taken from - which is why the discriminators are
	// asserted below rather than allow-listed like any other key.
	keystoreVariant07     = "lair_server"
	keystoreKeys07        = map[string]bool{"type": true, "connection_url": true}
	driverVariant07       = "websocket"
	websocketDriverKeys07 = map[string]bool{
		"type": true, "port": true, "danger_bind_addr": true, "allowed_origins": true,
	}
	// 0.7 renamed db_sync_strategy to db_sync_level and dropped the Fast variant
	// we ran on 0.6. The key is omitted from the template to take the default
	// (Normal); this is what a re-added `Fast` would fail.
	dbSyncLevels07 = map[string]bool{"Full": true, "Normal": true, "Off": true}
)

// TestRenderCloudInitConductorConfigIsValidFor07 is the closest a Go test gets to
// "0.7 would accept this file". Every type in it is #[serde(deny_unknown_fields)],
// so a key 0.7 does not know is not ignored - the parse fails, holochain.service
// never opens its admin port, and the droplet arrives with no conductor and
// nothing saying why. That is precisely how 0.6's signal_url and db_sync_strategy
// would land on a 0.7 node, and it is invisible to every other test here: the
// user-data stays valid cloud-init either way.
//
// Three shapes, because an unknown key is only the loudest of them. Unknown keys
// across all five mappings; the two enum discriminators, since a changed variant
// silently swaps which key set is even the right one; and the keys something
// downstream requires - Holochain itself, or a sed in the register script that
// exits 0 when it matches nothing. It still cannot check types, so a real 0.7
// parse remains the fleet-verify step.
func TestRenderCloudInitConductorConfigIsValidFor07(t *testing.T) {
	var config map[string]any
	renderConductorConfig(t, validCloudInitData(), &config)
	if len(config) == 0 {
		t.Fatalf("%s parsed to no keys at all - the scan is not reading the file", conductorPath)
	}

	// admin_interfaces is a list; the template writes exactly one entry, and each
	// entry is {driver: {...}}. Indexed so a second entry cannot go unchecked.
	interfaces, ok := config["admin_interfaces"].([]any)
	if !ok || len(interfaces) == 0 {
		t.Fatalf("%s has no admin_interfaces list (got %T) - nothing could reach the conductor", conductorPath, config["admin_interfaces"])
	}
	network := asMap(t, "network", config["network"])
	keystore := asMap(t, "keystore", config["keystore"])

	type mapping struct {
		what  string
		got   map[string]any
		known map[string]bool
	}
	checks := []mapping{
		{"ConductorConfig", config, conductorConfigKeys07},
		{"NetworkConfig", network, networkConfigKeys07},
		{"KeystoreConfig", keystore, keystoreKeys07},
	}
	drivers := map[string]map[string]any{}
	for i, iface := range interfaces {
		what := fmt.Sprintf("admin_interfaces[%d]", i)
		entry := asMap(t, what, iface)
		driver := asMap(t, what+".driver", entry["driver"])
		drivers[what] = driver
		// The entry itself, not only its driver: AdminInterfaceConfig denies
		// unknown fields too, so `port:` written one level too high - the easy
		// slip with a two-level shape - is fatal in exactly the same way.
		checks = append(checks, mapping{what, entry, adminInterfaceKeys07}, mapping{what + ".driver", driver, websocketDriverKeys07})
	}
	for _, tc := range checks {
		for k := range tc.got {
			if !tc.known[k] {
				t.Errorf("%s sets %s.%s, which Holochain 0.7 does not know: deny_unknown_fields makes it a fatal parse error, so the node boots with no conductor", conductorPath, tc.what, k)
			}
		}
	}

	// The discriminators the two key sets above were taken from. A different
	// variant is not an unknown key, so nothing above would notice - it would
	// just be checking the wrong variant's fields against the right variant's.
	if got := keystore["type"]; got != keystoreVariant07 {
		t.Errorf("keystore.type = %v, want %q - keystoreKeys07 describes that variant and no other", got, keystoreVariant07)
	}
	for what, driver := range drivers {
		if got := driver["type"]; got != driverVariant07 {
			t.Errorf("%s.type = %v, want %q - websocketDriverKeys07 describes that variant and no other", what, got, driverVariant07)
		}
		// danger_bind_addr is the one value worth pinning by name: heart creates
		// no firewall, so a droplet's admin websocket is one non-null value away
		// from the public internet, and 0.7 accepts it without comment.
		if got := driver["danger_bind_addr"]; got != nil {
			t.Errorf("%s.danger_bind_addr = %v, want null - a bound admin websocket on a droplet with no firewall lets anyone who reaches it control the conductor", what, got)
		}
	}

	// Keys whose ABSENCE is the failure, which an allow-list cannot see. All but
	// the last are ones a released 0.7 refuses to start without. The last it
	// happily defaults, but holochain-register rewrites that line with `sed`, and
	// sed exits 0 having matched nothing - so the node reports "Registration
	// complete" and simply never carries auth material to the bootstrap server.
	//
	// Hand-written rather than read off the schema's `required` arrays, which
	// answer a different question: ConductorConfig has no `required` at all and
	// still rejects a missing data_root_path after the parse, while
	// NetworkConfig lists `advanced`, which serde accepts absent.
	required := []struct {
		section string
		in      map[string]any
		key     string
	}{
		{"", config, "data_root_path"},
		{"keystore", keystore, "connection_url"},
		{"network", network, "bootstrap_url"},
		{"network", network, "relay_url"},
		{"network", network, "base64_auth_material_bootstrap"},
	}
	for what, driver := range drivers {
		required = append(required, struct {
			section string
			in      map[string]any
			key     string
		}{what + ".driver", driver, "allowed_origins"})
	}
	for _, req := range required {
		if _, set := req.in[req.key]; !set {
			t.Errorf("%s omits %s%s - Holochain requires it, or holochain-register's sed rewrites nothing and says so to no one", conductorPath, req.section+".", req.key)
		}
	}

	if level, set := config["db_sync_level"]; set {
		if s, ok := level.(string); !ok || !dbSyncLevels07[s] {
			t.Errorf("db_sync_level = %v, want one of Full/Normal/Off (0.7 dropped 0.6's Fast)", level)
		}
	}
}

// TestRenderCloudInitConductorConfigCouplings pins the three values the conductor
// config shares with a shell script somewhere else in the user-data. Holochain
// accepts either side of each on its own, so nothing above can see them: they
// fail only as behaviour on a booted droplet, and quietly.
//
// The admin port is the loud one - register's `nc -z` loop just times out, and
// systemd retries it forever (StartLimitIntervalSec=0). The other two are silent:
// both are `sed` targets, and sed exits 0 when it matches nothing, so first boot
// and registration each report success over a conductor config they did not
// actually patch - one left holding a placeholder for a lair URL, the other never
// given auth material at all.
func TestRenderCloudInitConductorConfigCouplings(t *testing.T) {
	out, err := renderCloudInit(validCloudInitData())
	if err != nil {
		t.Fatalf("renderCloudInit: %v", err)
	}
	firstBootCore, err := os.ReadFile("cloudinit/base/first-boot-core.sh")
	if err != nil {
		t.Fatalf("read first-boot-core.sh: %v", err)
	}

	var config struct {
		Keystore struct {
			ConnectionURL string `yaml:"connection_url"`
		} `yaml:"keystore"`
		AdminInterfaces []struct {
			Driver struct {
				Port int `yaml:"port"`
			} `yaml:"driver"`
		} `yaml:"admin_interfaces"`
	}
	renderConductorConfig(t, validCloudInitData(), &config)
	if len(config.AdminInterfaces) == 0 {
		t.Fatal("conductor config declares no admin interface")
	}

	// The register script waits on, then dials, a hard-coded ADMIN_PORT.
	if want := fmt.Sprintf("ADMIN_PORT=%d", config.AdminInterfaces[0].Driver.Port); !strings.Contains(out, want) {
		t.Errorf("conductor listens on %d but no script sets %q - registration would wait out its 60s and retry forever", config.AdminInterfaces[0].Driver.Port, want)
	}
	// first-boot-core.sh seds the lair connection URL over this placeholder.
	if placeholder := config.Keystore.ConnectionURL; !strings.Contains(string(firstBootCore), placeholder) {
		t.Errorf("conductor config carries keystore.connection_url %q, which first-boot-core.sh never rewrites - the conductor would start against a placeholder", placeholder)
	}
	// holochain-register seds the bootstrap auth material over its line.
	if !strings.Contains(out, "s|base64_auth_material_bootstrap:") {
		t.Error("no script rewrites base64_auth_material_bootstrap - the node would register and then never authenticate to the bootstrap server")
	}
}

// TestFirstBootUsesTheHc07AgentCommand pins the third thing 0.7 moved. `hc
// sandbox call` no longer exists (`call` became `hc client call --port`, in a
// new hc_client crate), and this is the line that mints the node's agent key -
// so reverting it strands first boot with an unrecognised subcommand. It is a
// shell line inside rendered user-data, which no Go test can execute; asserting
// the rendered text is the most that can be checked before a real droplet.
func TestFirstBootUsesTheHc07AgentCommand(t *testing.T) {
	out, err := renderCloudInit(validCloudInitData())
	if err != nil {
		t.Fatalf("renderCloudInit: %v", err)
	}
	if !strings.Contains(out, `hc client call --port "${ADMIN_PORT}" new-agent`) {
		t.Error("first boot does not create the agent key with `hc client call --port ... new-agent`")
	}
	if strings.Contains(out, "hc sandbox call") {
		t.Error("rendered user-data still calls `hc sandbox call`, which Holochain 0.7 removed")
	}
}

// asMap fails rather than returning an empty map for a missing or mistyped
// section, so a block that stopped rendering cannot read as clean.
func asMap(t *testing.T, what string, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s has no `%s` mapping (got %T) - a section the conductor needs stopped rendering", conductorPath, what, v)
	}
	return m
}

// TestCloudInitTemplateQuotesEveryAction holds half of what the value screen
// rests on: every value lands inside double quotes, which is what carries a '#'
// or a ': ' into the conductor config intact, and a {{ . }} added bare reopens
// that hole silently - the rendered document stays valid cloud-init either way,
// which is what makes it worth a gate rather than a note in AGENTS.md.
//
// It is only half. Quoted is not the same as safe: the screen's byte list is
// per-language (see firstUnsafeValueByte), and a quote count cannot tell which
// of the four embedded languages an action landed in. A fifth site inside quotes
// - a `sed "s|a|{{ .X }}|"`, where '|' and '&' are live and neither is screened
// - passes this gate. Adding an interpolation site means re-deriving that byte
// list for it, not just quoting it.
func TestCloudInitTemplateQuotesEveryAction(t *testing.T) {
	raw, err := os.ReadFile("cloudinit/cloud-config.yaml")
	if err != nil {
		t.Fatalf("read cloud-init template: %v", err)
	}
	src := string(raw)
	actions := regexp.MustCompile(`\{\{.*?\}\}`).FindAllStringIndex(src, -1)
	// The template interpolates every cloudInitData field plus the three base-file
	// blobs, several more than once; a scan seeing fewer is not seeing the file.
	if want := len(cloudInitData{}.fields()); len(actions) < want {
		t.Fatalf("found %d template actions, want at least %d - the scan is not reading the whole template", len(actions), want)
	}
	for _, loc := range actions {
		// Fail closed: skip only the keywords that emit nothing of their own.
		// Matching "{{ ." instead would wave through every other form that DOES
		// emit a value - {{- .X }} (the trim marker this YAML template invites),
		// {{ printf … }}, {{ template … }}, {{ $u }} - which is the silent pass
		// this gate exists to prevent.
		body := strings.TrimLeft(src[loc[0]+2:loc[1]-2], "- \t")
		fields := strings.Fields(body)
		// A comment emits nothing whether or not it is written with inner spaces,
		// so match it by prefix; the keywords are matched whole-word, since a
		// helper named `iferror` or `withDefault` must not pass as `if` / `with`.
		if len(fields) == 0 || strings.HasPrefix(fields[0], "/*") || nonEmittingActions[fields[0]] {
			continue
		}
		// Inside the quotes, not necessarily against them: systemd's
		// Environment="NAME={{ .X }}" quotes the assignment rather than the value.
		// An odd number of quotes earlier on the line means the action opens
		// inside one, and a later quote on the line means it closes there too.
		// Escaped quotes do not count - the template builds JSON bodies as
		// -d "{\"k\": \"…\"}", where counting them would read as balanced.
		lineStart := 1 + strings.LastIndexByte(src[:loc[0]], '\n')
		lineEnd := len(src)
		if n := strings.IndexByte(src[loc[1]:], '\n'); n >= 0 {
			lineEnd = loc[1] + n
		}
		before, after := unescapedQuotes(src[lineStart:loc[0]]), unescapedQuotes(src[loc[1]:lineEnd])
		if len(before)%2 == 1 && len(after) > 0 {
			continue
		}
		t.Errorf("cloudinit/cloud-config.yaml:%d: %s is not inside double quotes: %s",
			1+strings.Count(src[:loc[0]], "\n"), src[loc[0]:loc[1]], strings.TrimSpace(src[lineStart:lineEnd]))
	}
}

// nonEmittingActions are the text/template keywords that put nothing in the
// output themselves, so they have no scalar to sit inside. Deliberately short:
// anything not listed is treated as emitting and must be quoted, which is why
// `template` and `block` are absent - both DO emit. A non-emitting form that is
// missing (`break`, `continue`, a `$x :=` assignment) fails loudly naming the
// action, which is the direction to be wrong in: it makes someone read this list
// rather than quietly widening it.
var nonEmittingActions = map[string]bool{
	"if": true, "else": true, "end": true, "range": true, "with": true, "define": true,
}

// unescapedQuotes returns the indices of the double quotes in s that actually
// open or close a string, skipping any preceded by an odd run of backslashes.
func unescapedQuotes(s string) []int {
	var at []int
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			at = append(at, i)
		}
	}
	return at
}

// TestCloudInitDataFieldsAreComplete keeps fields() honest against the struct it
// mirrors: a value added to cloudInitData and the template but forgotten here is
// silently unscreened, and its key is what the error message would have named.
// Derived from the type, so it covers future fields automatically.
func TestCloudInitDataFieldsAreComplete(t *testing.T) {
	if got, want := len(cloudInitData{}.fields()), reflect.TypeOf(cloudInitData{}).NumField(); got != want {
		t.Errorf("fields() lists %d values, but cloudInitData has %d - a field is unscreened", got, want)
	}

	// Counting is not enough: a copy-paste in a ten-line list can name one key
	// while reading its neighbour's value, leaving a field unscreened with the
	// count still right. Distinct sentinels make the missed field say its name.
	var sentinels cloudInitData
	v := reflect.ValueOf(&sentinels).Elem()
	for i := range v.NumField() {
		if v.Field(i).Kind() != reflect.String {
			t.Fatalf("cloudInitData.%s is not a string, which fields() assumes", v.Type().Field(i).Name)
		}
		v.Field(i).SetString(fmt.Sprintf("sentinel-%d", i))
	}
	read := map[string]bool{}
	for _, f := range sentinels.fields() {
		read[f.val] = true
	}
	for i := range v.NumField() {
		if !read[fmt.Sprintf("sentinel-%d", i)] {
			t.Errorf("fields() never reads cloudInitData.%s - it reaches cloud-init unscreened", v.Type().Field(i).Name)
		}
	}

	// Every key must be a real heart:<key>. defaults.yaml holds them all except
	// influx-token, which is a required secret and so deliberately has no default.
	defaults, err := loadDefaults(defaultsFile)
	if err != nil {
		t.Fatalf("loadDefaults: %v", err)
	}
	for _, f := range (cloudInitData{}).fields() {
		if f.key == "influx-token" {
			continue
		}
		if _, ok := defaults[f.key]; !ok {
			t.Errorf("fields() names 'heart:%s', which is not a key in %s", f.key, defaultsFile)
		}
	}
}

// countingMocks records resource registrations so a test can assert the guard
// aborted before anything was provisioned.
type countingMocks struct{ droplets int }

func (m *countingMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if args.TypeToken == "digitalocean:index/droplet:Droplet" {
		m.droplets++
	}
	return args.Name + "-id", args.Inputs, nil
}

func (m *countingMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return args.Args, nil
}

// TestCreateFleetAbortsOnUnsafeConfig is the assertion the rest of this change
// exists for: detecting the bad byte is worth nothing unless it stops the fleet.
// A guard whose error were logged and swallowed would hand every droplet empty
// user-data - the original outage, restored, and just as quiet.
//
// The error is asserted, not merely required to be non-nil: createFleet reads a
// dozen config keys before it ever renders, so a fixture that lost one (or a
// defaults.yaml that dropped a default) would abort with zero droplets for a
// reason that has nothing to do with the guard, and pass.
func TestCreateFleetAbortsOnUnsafeConfig(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", `{"heart:release":"v0-1-0","heart:project-name":"unyt","heart:influx-token":"tok-example","heart:auth-server":"https://auth.example/`+string(rune(0x2019))+`"}`)
	mocks := &countingMocks{}
	var gotErr error
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, gotErr = createFleet(ctx)
		return nil
	}, pulumi.WithMocks("heart", "test", mocks))
	if err != nil {
		t.Fatalf("pulumi.RunErr: %v", err)
	}
	if gotErr == nil {
		t.Fatal("createFleet built a fleet from user-data cloud-init would discard")
	}
	if want := "config value 'heart:auth-server' must be printable ASCII"; !strings.Contains(gotErr.Error(), want) {
		t.Errorf("createFleet error = %q, want the guard's %q", gotErr, want)
	}
	if mocks.droplets != 0 {
		t.Errorf("registered %d droplets after the guard tripped, want 0", mocks.droplets)
	}
}

// TestEnsureCloudInitSafe pins the error's coordinates: cloud-init reports its own
// byte position when it rejects user-data, so ours has to line up with it.
func TestEnsureCloudInitSafe(t *testing.T) {
	if err := ensureCloudInitSafe("#cloud-config\nruncmd:\n  - echo hi\n", nil); err != nil {
		t.Errorf("ensureCloudInitSafe(clean) = %v, want nil", err)
	}

	// The em-dash (e2 80 94) starts at byte 10, on line 2.
	err := ensureCloudInitSafe("ab\n# unit — shared\n", nil)
	if err == nil {
		t.Fatal("ensureCloudInitSafe(non-ASCII) = nil, want error")
	}
	if want := "byte 0xe2 at offset 10, rendered line 2,"; !strings.Contains(err.Error(), want) {
		t.Errorf("ensureCloudInitSafe error = %q, want it to contain %q", err, want)
	}
}

// writeCloudInitFixture builds a minimal cloudinit/ tree and chdirs into it, so
// renderCloudInit (which reads relative paths) renders from it instead of the repo.
func writeCloudInitFixture(t *testing.T, cloudConfig string) {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "cloudinit", "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	// Contents are irrelevant: base files are base64-encoded before injection.
	for _, f := range []string{"lair-keystore.service", "holochain.service", "first-boot-core.sh"} {
		if err := os.WriteFile(filepath.Join(base, f), []byte("# "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cloudinit", "cloud-config.yaml"), []byte(cloudConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

// TestRenderCloudInitRejectsUnsafeBytes pins the guard to renderCloudInit, from
// both sources that reach the rendered user-data: the yaml file and the config
// values interpolated into it. A smart quote pasted into an endpoint costs a whole
// fleet just as an em-dash in a comment does. Each case asserts which error it
// got, so a fixture that breaks for an unrelated reason cannot pass as a catch.
func TestRenderCloudInitRejectsUnsafeBytes(t *testing.T) {
	cases := map[string]struct {
		comment  string
		auth     string
		wantErr  bool
		wantText string
	}{
		"clean":                       {comment: "# base units - shared", auth: "https://auth.example"},
		"em-dash in a yaml comment":   {comment: "# base units — shared", auth: "https://auth.example", wantErr: true, wantText: "user-data must be printable ASCII"},
		"smart quote in config value": {comment: "# base units - shared", auth: "https://auth.example/’", wantErr: true, wantText: "config value 'heart:auth-server'"},
		"nbsp in config value":        {comment: "# base units - shared", auth: "https://auth.example/" + string(rune(0x00a0)), wantErr: true, wantText: "config value 'heart:auth-server'"},
		"ansi escape in config value": {comment: "# base units - shared", auth: "https://auth.example/\x1b[0m", wantErr: true, wantText: "config value 'heart:auth-server'"},
		// Every byte here is ASCII: this is the case the document-level predicate
		// waves through, since the document itself is legitimately multi-line.
		"newline in config value": {comment: "# base units - shared", auth: "https://auth.example\n", wantErr: true, wantText: "config value 'heart:auth-server'"},
		// Also ASCII and single-line. The fixture's `echo "{{ .AuthServer }}"` is
		// the shell instance of what the conductor config has in YAML: the value
		// sits inside double quotes, and each of these is still live in at least
		// one of the four embedded languages the template interpolates into (the
		// map is on firstUnsafeValueByte). The shell one runs as root.
		"double quote in config value": {comment: "# base units - shared", auth: `https://auth.example/"`, wantErr: true, wantText: "config value 'heart:auth-server'"},
		"backslash in config value":    {comment: "# base units - shared", auth: `https://auth.example\x`, wantErr: true, wantText: "config value 'heart:auth-server'"},
		"substitution in config value": {comment: "# base units - shared", auth: `https://auth.example/$(id -u)`, wantErr: true, wantText: "config value 'heart:auth-server'"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			writeCloudInitFixture(t, "#cloud-config\n"+c.comment+`
runcmd:
  - echo "{{ .AuthServer }}"
write_files:
  - path: /etc/systemd/system/lair-keystore.service
    encoding: b64
    content: "{{ .LairKeystoreUnitB64 }}"
`)
			data := validCloudInitData()
			data.AuthServer = c.auth
			_, err := renderCloudInit(data)
			if !c.wantErr {
				if err != nil {
					t.Errorf("renderCloudInit(clean) = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("renderCloudInit accepted unsafe user-data; cloud-init would discard it at boot")
			}
			if !strings.Contains(err.Error(), c.wantText) {
				t.Errorf("renderCloudInit error = %q, want it to contain %q", err, c.wantText)
			}
		})
	}
}

// TestRenderCloudInitErrorMasksSecrets guards the guard: its snippet is cut from
// user-data holding the InfluxDB token in clear text, and Pulumi prints a program
// error verbatim. The fixture renders the token at BOTH sites the real
// cloud-config does (telegraf.conf and the metrics drop-in), with the offending
// byte past the second one so the window clips it mid-value - a single-occurrence
// blanking, or one that is not equal-length, fails here.
func TestRenderCloudInitErrorMasksSecrets(t *testing.T) {
	// Non-repeating, so the sliding fragment scan below is a real per-position
	// check rather than one comparison repeated; 88 bytes is a realistic token.
	var sb strings.Builder
	for i := 0; sb.Len() < 88; i++ {
		fmt.Fprintf(&sb, "%02x", i*7%256)
	}
	secret := sb.String()[:88]

	tmpl := "#cloud-config\ntoken = \"{{ .InfluxToken }}\"\n" +
		strings.Repeat("# padding\n", 20) +
		"Environment=\"HOLOCHAIN_INFLUXIVE_EXTERNAL_TOKEN={{ .InfluxToken }}\"\n# trailing — comment\n"
	writeCloudInitFixture(t, tmpl)

	data := validCloudInitData()
	data.InfluxToken = secret
	_, err := renderCloudInit(data)
	if err == nil {
		t.Fatal("renderCloudInit accepted a non-ASCII yaml comment")
	}
	if want := "user-data must be printable ASCII"; !strings.Contains(err.Error(), want) {
		t.Fatalf("renderCloudInit error = %q, want it to contain %q", err, want)
	}

	// The offset must be the byte's position in the RENDERED output, not in the
	// template: the two token expansions shift it, so this is what pins the guard
	// to the rendered user-data. Equal-length blanking is what keeps the reported
	// offset and the snippet in register.
	rendered := strings.ReplaceAll(tmpl, "{{ .InfluxToken }}", secret)
	wantOffset := fmt.Sprintf("at offset %d", strings.IndexByte(rendered, 0xe2))
	if !strings.Contains(err.Error(), wantOffset) {
		t.Errorf("renderCloudInit error = %q, want it to report %q", err, wantOffset)
	}
	if want := "trailing"; !strings.Contains(err.Error(), want) {
		t.Errorf("guard snippet lost the offending byte's context (want %q in it): %v", want, err)
	}

	const minLeak = 8
	checked := 0
	for i := 0; i+minLeak <= len(secret); i++ {
		checked++
		if frag := secret[i : i+minLeak]; strings.Contains(err.Error(), frag) {
			t.Fatalf("guard error leaked token fragment %q: %v", frag, err)
		}
	}
	if checked == 0 {
		t.Fatal("token fixture is shorter than minLeak; the leak check ran on nothing")
	}

	// A bad byte in a config value is screened before rendering, so it names the
	// key rather than an offset the operator cannot map back to anything.
	writeCloudInitFixture(t, tmpl)
	data.InfluxOrg = "org—x"
	_, err = renderCloudInit(data)
	if err == nil {
		t.Fatal("renderCloudInit accepted a non-ASCII config value")
	}
	if want := "config value 'heart:influx-org'"; !strings.Contains(err.Error(), want) {
		t.Errorf("renderCloudInit error = %q, want it to contain %q", err, want)
	}
}

// TestCloudInitTreeIsASCII pins the invariant renderCloudInit's guard cannot see:
// cloudinit/base/* is base64-encoded before injection, so non-ASCII there survives
// the render. Keeping the whole tree ASCII means no one has to track which files
// are injected raw and which are encoded before moving content between them.
func TestCloudInitTreeIsASCII(t *testing.T) {
	seen := 0
	err := filepath.WalkDir("cloudinit", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		// Only tracked sources. This runs in a working tree (no CI job walks it),
		// where .DS_Store, editor swap files and .orig leftovers all carry bytes
		// that would fail the check without saying anything about the repo.
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen++
		if i := firstUnsafeByte(string(b)); i >= 0 {
			t.Errorf("%s:%d has unsafe byte %#x; keep cloudinit/ printable-ASCII only", path, 1+strings.Count(string(b[:i]), "\n"), b[i])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cloudinit/: %v", err)
	}
	// The walk is relative, so a stray chdir would otherwise make this vacuous.
	if want := 4; seen < want {
		t.Fatalf("walked %d files under cloudinit/, want at least %d", seen, want)
	}
}

// TestConductorConfigKeysMatchHolochainSchema re-derives the allow-lists above
// from the horse's mouth: `holochain --config-schema` prints the conductor
// config's JSON schema, generated from the very serde types that will parse the
// file on the droplet. It is the difference between a hand-copied snapshot and a
// checked one - without it the lists are only as good as whoever last read the
// Rust, and they are wrong in exactly the way that boots a fleet with no
// conductor.
//
// Skipped when no holochain is on PATH, which is the case in CI (ubuntu-latest +
// setup-go, no nix). That is why the lists stay hand-written and separately
// asserted rather than being read from the schema at run time: the cheap gate has
// to work everywhere, and this one upgrades it to authoritative wherever a
// matching binary exists. Point HEART_HOLOCHAIN_BIN at one to run it explicitly.
//
// Setting that variable is asking for the check, so anything that would stop it
// running is then a failure rather than a skip. Otherwise the documented
// invocation degrades in silence - heart's own dev shell ships no holochain, so
// `HEART_HOLOCHAIN_BIN=$(which holochain)` expands to nothing there, and a run
// that verified none of this still prints ok.
func TestConductorConfigKeysMatchHolochainSchema(t *testing.T) {
	bin, asked := os.LookupEnv("HEART_HOLOCHAIN_BIN")
	// An empty value is a `$(which holochain)` that found nothing, not a request.
	if asked && bin == "" {
		t.Fatal("HEART_HOLOCHAIN_BIN is set but empty - it was probably $(which holochain) on a machine without one")
	}
	if !asked {
		var err error
		if bin, err = exec.LookPath("holochain"); err != nil {
			t.Skip("no holochain on PATH and HEART_HOLOCHAIN_BIN unset; the hand-derived lists stand unverified in this run")
		}
	}

	unusable := t.Skipf
	if asked {
		unusable = t.Fatalf
	}
	version, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version: %v", bin, err)
	}
	// A 0.8 binary would "disagree" with lists that are correct for the version
	// we deploy, which is a different finding than a wrong list - and one this
	// test is not the place to report. Still a failure if it was asked for by
	// name: whoever set the variable wanted these lists checked.
	if got := strings.TrimSpace(string(version)); !strings.HasPrefix(got, "holochain 0.7.") {
		unusable("%s is %q, not the 0.7.x line these lists describe", bin, got)
		return
	}

	raw, err := exec.Command(bin, "--config-schema").Output()
	if err != nil {
		t.Fatalf("%s --config-schema: %v", bin, err)
	}
	var schema struct {
		Title      string                 `json:"title"`
		Properties map[string]any         `json:"properties"`
		Defs       map[string]schemaEntry `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse --config-schema output: %v", err)
	}
	if schema.Title != "ConductorConfig" {
		t.Fatalf("--config-schema describes %q, not ConductorConfig", schema.Title)
	}

	for _, tc := range []struct {
		name string
		want map[string]bool
		got  map[string]any
	}{
		{"conductorConfigKeys07", conductorConfigKeys07, schema.Properties},
		{"networkConfigKeys07", networkConfigKeys07, schema.Defs["NetworkConfig"].Properties},
		{"adminInterfaceKeys07", adminInterfaceKeys07, schema.Defs["AdminInterfaceConfig"].Properties},
		{"keystoreKeys07", keystoreKeys07, variantProperties(t, schema.Defs["KeystoreConfig"], keystoreVariant07)},
		{"websocketDriverKeys07", websocketDriverKeys07, variantProperties(t, schema.Defs["InterfaceDriver"], driverVariant07)},
	} {
		if len(tc.got) == 0 {
			t.Errorf("%s: the schema has no such mapping - this test is not reading what it thinks it is", tc.name)
			continue
		}
		for k := range tc.got {
			if !tc.want[k] {
				t.Errorf("%s is missing %q, which this holochain accepts", tc.name, k)
			}
		}
		for k := range tc.want {
			if _, ok := tc.got[k]; !ok {
				t.Errorf("%s allows %q, which this holochain does not know - a config using it would not parse", tc.name, k)
			}
		}
	}

	// DbSyncLevel is a plain string enum, so its variants come from the consts.
	levels := map[string]bool{}
	for _, v := range schema.Defs["DbSyncLevel"].OneOf {
		levels[v.Const] = true
	}
	if !reflect.DeepEqual(levels, dbSyncLevels07) {
		t.Errorf("dbSyncLevels07 = %v, but this holochain accepts %v", dbSyncLevels07, levels)
	}
}

// schemaEntry is the slice of JSON-schema shape these lists need: a plain object
// has Properties, an internally-tagged enum has a OneOf of them, each pinning its
// discriminator with a Const.
type schemaEntry struct {
	Properties map[string]any `json:"properties"`
	OneOf      []struct {
		Const      string `json:"const"`
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	} `json:"oneOf"`
}

// variantProperties returns the property names of one variant of a tagged enum,
// found by the `const` its `type` property is pinned to.
func variantProperties(t *testing.T, e schemaEntry, variant string) map[string]any {
	t.Helper()
	for _, v := range e.OneOf {
		if v.Properties["type"].Const != variant {
			continue
		}
		props := map[string]any{}
		for k := range v.Properties {
			props[k] = struct{}{}
		}
		return props
	}
	t.Errorf("the schema has no %q variant", variant)
	return nil
}
