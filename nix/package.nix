# sbx-env-viz built from this checkout. flake.nix calls it; it can also be
# used on its own with pkgs.callPackage ./nix/package.nix { version = ...; }.
{
  lib,
  buildGoModule,
  fetchurl,
  go_1_27,
  version,
}:
let
  # go.mod asks for Go 1.27.4 and the build keeps GOTOOLCHAIN=local, so an
  # older nixpkgs Go would refuse the module. Until the pinned nixpkgs ships
  # 1.27.4, build that release from Google's source archive (the hash
  # matches https://dl.google.com/go/go1.27.4.src.tar.gz.sha256). Once
  # nixpkgs catches up, this falls back to its own Go.
  go =
    if lib.versionAtLeast go_1_27.version "1.27.4" then
      go_1_27
    else
      go_1_27.overrideAttrs {
        version = "1.27.4";
        src = fetchurl {
          url = "https://dl.google.com/go/go1.27.4.src.tar.gz";
          hash = "sha256-c8+4VxrsDAEXdofbhxY9Rqog6XeWcncokaoXZU2aYJk=";
        };
      };
in
(buildGoModule.override { inherit go; }) {
  pname = "sbx-env-viz";
  inherit version;

  # Only what the build reads: no e2e module, docs, or release config.
  src = lib.fileset.toSource {
    root = ./..;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
      ../pkg
    ];
  };

  # Recompute after go.mod/go.sum change: set lib.fakeHash, build, copy "got:".
  vendorHash = "sha256-jVWC/OYPOtuP/StnxcAXwJlCKdZ6hYAarrDmaEEWJpk=";

  subPackages = [ "cmd/sbx-env-viz" ];
  env.CGO_ENABLED = "0";
  flags = [ "-trimpath" ];
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  # The unit tests run in CI with the race detector; here, check that the
  # installed binary starts and reports the version it was built with.
  doCheck = false;
  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    "$out/bin/sbx-env-viz" --version | grep -F "sbx-env-viz ${version} "
    runHook postInstallCheck
  '';

  meta = {
    description = "Statically visualize Docker Sandboxes sbxenv.yaml files (TUI, text, JSON)";
    homepage = "https://github.com/kotarotaniguchi0523/sbx-env-viz";
    license = lib.licenses.mit;
    mainProgram = "sbx-env-viz";
    platforms = lib.platforms.darwin ++ lib.platforms.linux;
  };
}
