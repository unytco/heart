package main

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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

// TestFirstUnsafeByte covers the byte classes cloud-init's YAML loader rejects.
// The non-ASCII half is the one that took the fleet down; the C0 controls and DEL
// fail the same way ("control characters are not allowed") and are just as easy to
// paste in - an ANSI escape copied out of colourised CLI output carries 0x1b.
func TestFirstUnsafeByte(t *testing.T) {
	safe := []string{"", "#cloud-config\nruncmd:\n  - echo hi\n", "tabs\tand\r\nnewlines"}
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
// whole-config-discarded outage, reached without a single non-ASCII byte.
func TestFirstUnsafeValueByte(t *testing.T) {
	if i := firstUnsafeValueByte("https://auth.example/path"); i >= 0 {
		t.Errorf("firstUnsafeValueByte(clean URL) = %d, want -1", i)
	}
	for name, s := range map[string]string{
		"trailing newline": "https://auth.example\n",
		"embedded newline": "https://auth\nexample",
		"carriage return":  "https://auth.example\r",
		"tab":              "https://auth\texample",
	} {
		if i := firstUnsafeValueByte(s); i < 0 {
			t.Errorf("%s: firstUnsafeValueByte(%q) = -1, want an index", name, s)
		}
	}
}

// TestCloudInitDataFieldsAreComplete keeps fields() honest against the struct it
// mirrors: a value added to cloudInitData and the template but forgotten here is
// silently unscreened, and its key is what the error message would have named.
// Derived from the type, so it covers future fields automatically.
func TestCloudInitDataFieldsAreComplete(t *testing.T) {
	if got, want := len(cloudInitData{}.fields()), reflect.TypeOf(cloudInitData{}).NumField(); got != want {
		t.Errorf("fields() lists %d values, but cloudInitData has %d - a field is unscreened", got, want)
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
func TestCreateFleetAbortsOnUnsafeConfig(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", `{"heart:release":"v0-1-0","heart:project-name":"unyt","heart:influx-token":"tok-example","heart:auth-server":"https://auth.example/`+string(rune(0x2019))+`"}`)
	mocks := &countingMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		if _, err := createFleet(ctx); err == nil {
			t.Error("createFleet built a fleet from user-data cloud-init would discard")
		}
		return nil
	}, pulumi.WithMocks("heart", "test", mocks))
	if err != nil {
		t.Fatalf("pulumi.RunErr: %v", err)
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
			_, err := renderCloudInit(cloudInitData{AuthServer: c.auth})
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

	_, err := renderCloudInit(cloudInitData{InfluxToken: secret})
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
	_, err = renderCloudInit(cloudInitData{InfluxToken: secret, InfluxOrg: "org—x"})
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
