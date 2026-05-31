package main

import (
	"strconv"
	"testing"
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
