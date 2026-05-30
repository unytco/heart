{
  description = "Dev shell for Pulumi to configure HEART services";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs = { ... }@inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [ "aarch64-darwin" "x86_64-linux" ];

      perSystem = { pkgs, ... }:
        let
          # Bundle the Pulumi binary with the language package to remove the
          # warning about them not being in the same directory.
          # See: https://github.com/pulumi/pulumi/issues/14525
          pulumiBundle = pkgs.stdenv.mkDerivation {
            name = "pulumi-bundle-${pkgs.pulumi.version}";
            phases = [ "installPhase" "fixupPhase" ];
            buildInputs = with pkgs; [ pulumi pulumiPackages.pulumi-go ];
            installPhase = ''
              mkdir -p $out/bin
              mkdir -p $out/share
              cp ${pkgs.pulumi}/bin/* $out/bin
              cp -r ${pkgs.pulumi}/share/* $out/share
              cp ${pkgs.pulumiPackages.pulumi-go}/bin/* $out/bin
            '';
          };
        in
        {
          formatter = pkgs.nixpkgs-fmt;
          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              pulumiBundle
              # Go toolchain — the Pulumi program is `runtime: go`, so
              # `pulumi up` shells out to `go` to compile it, and AGENTS.md's
              # `nix develop -c go build/vet` workflow needs it on PATH.
              go
              netcat
              yq
              jq
              python315
              curl
              # DigitalOcean CLI — used by operator runbooks to look up
              # droplet IPs, manage SSH keys, and probe firewall state
              # alongside `pulumi`. Keeping it in the devShell avoids a
              # global `apt install` step in the deploy docs.
              doctl
              # cloudflared — already on PATH for many laptops via the
              # apt repo, but pinning it through the devShell guarantees
              # `cloudflared tunnel info`, `cloudflared tunnel route
              # dns`, etc. resolve to a known-good version regardless
              # of host-level install. Avoids the laptop's "your
              # version is outdated" warning bleeding into operator
              # output.
              cloudflared
            ];
          };
        };
    };
}
