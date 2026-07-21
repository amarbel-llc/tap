{
  description = "TAP-14 specification and tap-dancer reference implementation";

  inputs = {
    # amarbel-llc fork of nixpkgs. The overlay (`overlays.default`) adds
    # gomod2nix's buildGoApplication / mkGoEnv on top of upstream.
    igloo.url = "https://code.linenisgreat.com/igloo/archive/master.tar.gz";

    # Master nixpkgs pinned for the devshell's Go tooling
    # (gofumpt/gopls/golangci-lint). Go itself comes from the fork's
    # `pkgs.go_1_26` (1.26.3); nixpkgs-master can be dropped once the
    # fork's master tracks an equivalent set of Go tools.
    nixpkgs-master.url = "github:NixOS/nixpkgs/567a49d1913ce81ac6e9582e3553dd90a955875f";

    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    # `nix fmt` entry point. Config lives in ./treefmt.nix.
    treefmt-nix.url = "github:numtide/treefmt-nix";
    treefmt-nix.inputs.nixpkgs.follows = "igloo";

    # gomod2nix (devshell tool — generates go/gomod2nix.toml).
    gomod2nix = {
      url = "https://code.linenisgreat.com/gomod2nix/archive/master.tar.gz";
    };

    # Rust build (crane) + stable toolchain (rust-overlay).
    crane.url = "github:ipetkov/crane";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "igloo";
    };

    # bats helper libraries (bats-support, bats-assert, …) bundled as
    # `bats-libs` with a `batsLibPath` passthru. Used both by the
    # devShell's `BATS_LIB_PATH` and by the nix-driven `bats-*` lanes
    # in `bats.nix`. Note: amarbel-llc/bats has a `tap` input, but its
    # `bats-libs` output does not consume it — no circular dependency.
    bats = {
      url = "https://code.linenisgreat.com/bats/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # Provides the `dagnabit` codegen tool used by the
    # `//go:generate dagnabit export` directives that produce the
    # go/pkgs/* re-export shims. Devshell-only; not a build dependency.
    purse-first = {
      url = "https://code.linenisgreat.com/purse-first/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.gomod2nix.follows = "gomod2nix";
    };
    utils.inputs.systems.follows = "igloo/systems";
    gomod2nix.inputs.nixpkgs-master.follows = "nixpkgs-master";
    igloo.inputs.nixpkgs-master.follows = "nixpkgs-master";
    igloo.inputs.treefmt-nix.follows = "treefmt-nix";
    gomod2nix.inputs.flake-utils.follows = "utils";
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      utils,
      gomod2nix,
      crane,
      rust-overlay,
      bats,
      treefmt-nix,
      purse-first,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        # The amarbel-llc/nixpkgs fork's default.nix shim auto-applies the
        # fork overlay on import (eng#60), so buildGoApplication and mkGoEnv
        # (the gomod2nix-style Go builder, distinct from upstream
        # buildGoModule) are available without an explicit overlays entry.
        pkgs = import igloo { inherit system; };
        pkgs-master = import nixpkgs-master { inherit system; };
        pkgs-rust = import igloo {
          inherit system;
          overlays = [ (import rust-overlay) ];
        };
        rustToolchain = pkgs-rust.rust-bin.stable.latest.default;
        craneLib = (crane.mkLib pkgs-rust).overrideToolchain rustToolchain;

        # Single source of truth for the project version. version.env is a
        # shell-style file (`VERSION=X.Y.Z`) so the release recipe can
        # `source` it and Nix can parse it without an extra dependency.
        version = builtins.elemAt (builtins.match "^VERSION=([^\n]+)\n?$" (builtins.readFile ./version.env)) 0;

        goModule = import ./go/gomod.nix {
          inherit pkgs self version;
          go = pkgs.go_1_26;
        };
        inherit (goModule) tap-dancer-go;

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

        # Section-7 manpages compiled from scdoc sources in ./doc. The
        # tap-ndjson(7) page is the sole normative specification of the
        # format-ndjson wire format (see doc/tap-ndjson.7.scd). Follows
        # the eng-manpages(7) reference derivation.
        tap-dancer-doc = pkgs.stdenvNoCC.mkDerivation {
          pname = "tap-dancer-doc";
          inherit version;
          src = ./doc;
          nativeBuildInputs = [ pkgs.scdoc ];
          dontUnpack = true;
          dontBuild = true;
          installPhase = ''
            mkdir -p $out/share/man/man7
            for f in $src/*.7.scd; do
              [ -e "$f" ] || continue
              scdoc < "$f" > "$out/share/man/man7/$(basename "$f" .scd)"
            done
          '';
        };

        tap-dancer = pkgs.symlinkJoin {
          name = "tap-dancer";
          paths = [
            tap-dancer-go
            tap-dancer-rust
            tap-dancer-bash
            tap-dancer-doc
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
          inherit
            pkgs
            bats-libs
            tap-dancer-go
            tap-dancer-bash
            ;
          # batsLane migrated out of amarbel-llc/nixpkgs's overlay into
          # amarbel-llc/bats — see amarbel-llc/nixpkgs#16. tap consumes
          # it from the bats flake input now.
          batsLane = bats.lib.${system}.batsLane;
          # Same go that builds tap-dancer-go, so the bats sandbox runs
          # `go test` against the same toolchain as the real binary.
          go = pkgs.go_1_26;
          batsSrc = tests-src;
          # 10s (the bats.nix default and zz-tests_bats/justfile's value)
          # is too aggressive for the nix sandbox, where
          # test_runners_ndjson.bats's per-test `go test` invocations
          # compile stdlib from cold. 60s gives generous margin over
          # the ~5s/test we actually observe.
          batsTestTimeout = "60";
        };

        treefmtEval = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;
      in
      {
        packages = {
          default = tap-dancer;
          inherit
            tap-dancer
            tap-dancer-go
            tap-dancer-rust
            tap-dancer-bash
            tap-dancer-doc
            ;
          inherit (goModule) go-pkgs go-pkgs-test;
        }
        // batsLib.batsLaneOutputs;

        checks = {
          tap-tests = batsLib.batsLaneOutputs.bats-default;
          formatting = treefmtEval.config.build.check self;
        };

        formatter = treefmtEval.config.build.wrapper;

        devShells.default = pkgs-master.mkShell {
          packages = [
            (pkgs.mkGoEnv { pwd = ./go; })
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

            # Used by the `release` recipe.
            pkgs.gh

            # Codegen tool for the `//go:generate dagnabit export` shims.
            purse-first.packages.${system}.dagnabit
          ];
          BATS_LIB_PATH = bats-libs.batsLibPath;
          GOTOOLCHAIN = "local";
        };
      }
    );
}
