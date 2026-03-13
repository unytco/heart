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
              netcat
              yq
              jq
              python315
              curl
            ];
          };
        };
    };
}
