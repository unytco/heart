package main

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"

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
		"bootstrap-url", "signal-url", "relay-url", "auth-server",
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
}

// TestRenderCloudInit renders the cloud-config with representative data and
// checks the invariants that matter for provisioning: every template action is
// resolved, the output is valid cloud-init YAML, and the shared base-image files
// (cloudinit/base/) are injected byte-for-byte so they cannot drift from the copy
// the local-testnet fleet image COPYs. It also pins the prod-vs-local split: the
// InfluxDB metrics env is a drop-in, not part of the shared base unit.
func TestRenderCloudInit(t *testing.T) {
	data := cloudInitData{
		HolochainVersion:   "holochain-0.6.2-rc.0",
		HoloKeyutilVersion: "v0.1.0",
		BootstrapURL:       "https://bootstrap.example",
		SignalURL:          "http://not-used:1234",
		RelayURL:           "https://relay.example",
		AuthServer:         "https://auth.example",
		InfluxURL:          "https://influx.example",
		InfluxOrg:          "org123",
		InfluxBucket:       "unyt",
		InfluxToken:        "tok-example",
	}
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
	baseDecoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(base.content))
	if strings.Contains(string(baseDecoded), "HOLOCHAIN_INFLUXIVE_EXTERNAL") {
		t.Error("base holochain.service must not carry InfluxDB env (it belongs in the drop-in)")
	}
	dropin, ok := files["/etc/systemd/system/holochain.service.d/10-metrics.conf"]
	if !ok {
		t.Fatal("missing holochain.service.d/10-metrics.conf drop-in")
	}
	if !strings.Contains(dropin.content, "HOLOCHAIN_INFLUXIVE_EXTERNAL_HOST") || !strings.Contains(dropin.content, data.InfluxURL) {
		t.Errorf("metrics drop-in missing templated InfluxDB env")
	}
}
