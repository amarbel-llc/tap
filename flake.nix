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

        version = "0.2.0";

        tap-dancer-cli = pkgs.buildGoApplication {
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
            tap-dancer-cli
            tap-dancer-rust
            tap-dancer-bash
          ];
        };

        # Filter zz-tests_bats so the hermetic-tests store path only
        # changes when actual test inputs change — not on unrelated
        # repo edits.
        tests-src = pkgs.lib.cleanSourceWith {
          src = ./zz-tests_bats;
          filter =
            path: type:
            let
              bn = builtins.baseNameOf path;
            in
            type == "directory" || pkgs.lib.hasSuffix ".bats" bn || bn == "common.bash";
        };

        # Hermetic bats suite, wired to `nix flake check`. Inherits
        # nothing from the host PATH and runs against the freshly built
        # tap-dancer-cli — so a regression in the emitter (caught by
        # the in-suite `tap-dancer validate` calls) fails the check.
        hermetic-tests =
          pkgs.runCommandLocal "tap-dancer-tests"
            {
              nativeBuildInputs = [
                pkgs.bats
                tap-dancer-cli
                pkgs.coreutils
              ];
            }
            ''
              cd ${tests-src}
              export TAP_DANCER_BIN=${tap-dancer-cli}/bin/tap-dancer
              export BATS_LIB_PATH=${pkgs.bats.libraries.bats-support}/share/bats:${pkgs.bats.libraries.bats-assert}/share/bats
              export TMPDIR=/tmp
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              bats --tap .
              touch $out
            '';
      in
      {
        packages = {
          default = tap-dancer;
          inherit
            tap-dancer
            tap-dancer-cli
            tap-dancer-rust
            tap-dancer-bash
            ;
        };

        checks = {
          tap-tests = hermetic-tests;
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
          ];
          BATS_LIB_PATH = "${pkgs.bats.libraries.bats-support}/share/bats:${pkgs.bats.libraries.bats-assert}/share/bats";
          GOTOOLCHAIN = "local";
        };
      }
    );
}
