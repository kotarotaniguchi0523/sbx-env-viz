{
  description = "Statically visualize Docker Sandboxes sbxenv.yaml files";

  # nixpkgs is the only input. flake.lock pins its commit and content hash,
  # and nix/package.nix pins the Go module dependencies with vendorHash.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # A tagged checkout reports its commit; a dirty tree says so. `nix run`
      # runs packages.default through meta.mainProgram, so no apps output.
      version = "0-unstable-" + (self.shortRev or self.dirtyShortRev or "unknown");
    in
    {
      packages = forAllSystems (pkgs: rec {
        sbxray = pkgs.callPackage ./nix/package.nix { inherit version; };
        default = sbxray;
      });

      checks = forAllSystems (pkgs: {
        # Builds the package and runs its install check (--version).
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.sbxray;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
