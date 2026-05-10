{
  description = "TAP-14 specification and tap-dancer reference implementation";

  inputs = {
    # amarbel-llc fork of nixpkgs. The overlay (`overlays.default`) adds
    # gomod2nix's buildGoApplication / mkGoEnv on top of upstream.
    nixpkgs.url = "github:amarbel-llc/nixpkgs";

    # Master nixpkgs pinned for go_1_26 (fork doesn't track master).
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";

    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    # gomod2nix (devshell tool — generates go/gomod2nix.toml).
    gomod2nix = {
      url = "github:amarbel-llc/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    # Rust build (crane) + stable toolchain (rust-overlay).
    crane.url = "github:ipetkov/crane";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    # bats helper libraries (bats-support, bats-assert, …) bundled as
    # `bats-libs` with a `batsLibPath` passthru. Used both by the
    # devShell's `BATS_LIB_PATH` and by the nix-driven `bats-*` lanes
    # in `bats.nix`. Note: amarbel-llc/bats has a `tap` input, but its
    # `bats-libs` output does not consume it — no circular dependency.
    bats = {
      url = "github:amarbel-llc/bats";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      gomod2nix,
      crane,
      rust-overlay,
      bats,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        # Fork's overlay provides buildGoApplication and mkGoEnv (the
        # gomod2nix-style Go builder, distinct from upstream buildGoModule).
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ nixpkgs.overlays.default ];
        };
        pkgs-master = import nixpkgs-master { inherit system; };
        pkgs-rust = import nixpkgs {
          inherit system;
          overlays = [ (import rust-overlay) ];
        };
        rustToolchain = pkgs-rust.rust-bin.stable.latest.default;
        craneLib = (crane.mkLib pkgs-rust).overrideToolchain rustToolchain;

        # Single source of truth for the project version. version.env is a
        # shell-style file (`VERSION=X.Y.Z`) so the release recipe can
        # `source` it and Nix can parse it without an extra dependency.
        version = builtins.elemAt (builtins.match "^VERSION=([^\n]+)\n?$" (builtins.readFile ./version.env)) 0;

        tap-dancer-go = pkgs.buildGoApplication {
          pname = "tap-dancer";
          inherit version;
          src = ./go;
          pwd = ./go;
          subPackages = [ "cmd/tap-dancer" ];
          modules = ./go/gomod2nix.toml;
          go = pkgs-master.go;
          GOTOOLCHAIN = "local";
          meta = with pkgs.lib; {
            description = "TAP-14 validator and writer toolkit";
            homepage = "https://github.com/amarbel-llc/tap";
            license = licenses.mit;
          };
        };

        rustSrc = craneLib.cleanCargoSource ./rust;
        rustCommonArgs = {
          src = rustSrc;
          pname = "tap-dancer";
          inherit version;
          strictDeps = true;
        };
        rustCargoArtifacts = craneLib.buildDepsOnly rustCommonArgs;

        tap-dancer-rust = craneLib.buildPackage (
          rustCommonArgs // { cargoArtifacts = rustCargoArtifacts; }
        );

        tap-dancer-bash = pkgs.stdenvNoCC.mkDerivation {
          pname = "tap-dancer-bash";
          inherit version;
          src = ./bash;
          dontBuild = true;
          installPhase = ''
            mkdir -p $out/share/tap-dancer/lib/src
            cp load.bash $out/share/tap-dancer/lib/
            cp src/*.bash $out/share/tap-dancer/lib/src/
            mkdir -p $out/nix-support
            echo 'export TAP_DANCER_LIB="'"$out"'/share/tap-dancer/lib"' \
              > $out/nix-support/setup-hook
          '';
        };

        tap-dancer = pkgs.symlinkJoin {
          name = "tap-dancer";
          paths = [
            tap-dancer-go
            tap-dancer-rust
            tap-dancer-bash
          ];
        };

        # Filter zz-tests_bats so bats-lane store paths only change
        # when actual test inputs change — not on unrelated repo
        # edits. The local `justfile` is excluded; lanes invoke bats
        # directly, not through `just`.
        tests-src = pkgs.lib.cleanSourceWith {
          src = ./zz-tests_bats;
          filter =
            path: type:
            let
              bn = builtins.baseNameOf path;
            in
            type == "directory" || pkgs.lib.hasSuffix ".bats" bn || bn == "common.bash";
        };

        bats-libs = bats.packages.${system}.bats-libs;

        # Per-tag bats lane outputs (`bats-default`, plus `bats-${tag}`
        # for every `# bats file_tags=` directive found in zz-tests_bats).
        # See bats.nix for the auto-discovery rules.
        batsLib = import ./bats.nix {
          inherit pkgs bats-libs tap-dancer-go;
          batsSrc = tests-src;
        };
      in
      {
        packages = {
          default = tap-dancer;
          inherit
            tap-dancer
            tap-dancer-go
            tap-dancer-rust
            tap-dancer-bash
            ;
        }
        // batsLib.batsLaneOutputs;

        checks = {
          tap-tests = batsLib.batsLaneOutputs.bats-default;
        };

        devShells.default = pkgs-master.mkShell {
          packages = [
            (pkgs.mkGoEnv { pwd = ./go; })
            pkgs-master.go
            pkgs-master.gofumpt
            pkgs-master.gopls
            pkgs-master.golangci-lint
            gomod2nix.packages.${system}.default

            rustToolchain
            pkgs-rust.cargo-watch
            pkgs-rust.rust-analyzer

            pkgs.bats

            pkgs.shellcheck
            pkgs.shfmt

            pkgs.just
            pkgs.nixfmt-rfc-style

            # Used by the `release` recipe.
            pkgs.gh
          ];
          BATS_LIB_PATH = bats-libs.batsLibPath;
          GOTOOLCHAIN = "local";
        };
      }
    );
}
