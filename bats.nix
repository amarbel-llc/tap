# bats integration test lanes for tap-dancer.
#
# Wraps `batsLane` (provided by amarbel-llc/bats's `lib.${system}.batsLane`
# — see amarbel-llc/nixpkgs#14 for the design rationale and the
# nixpkgs#16 follow-up that migrated the builder out of the nixpkgs
# overlay into amarbel-llc/bats) with tap-dancer-specific defaults:
# `bats-libs` from amarbel-llc/bats on `BATS_LIB_PATH`,
# `TAP_DANCER_BIN` exported via the `binaries` map form, and a
# `BATS_TEST_TIMEOUT` mirroring `zz-tests_bats/justfile`.
#
# Auto-discovers `# bats file_tags=foo,bar` directives at flake-eval
# time and produces one `bats-${tag}` derivation per unique tag plus
# `bats-default` (no filter). Adding/removing tags in a `.bats` file
# invalidates the eval cache — the right behavior, but worth knowing.
#
# Only file-level tags are surfaced; per-`@test` tags are not
# auto-discovered. Use `mkBatsLane` directly for ad-hoc filters.
{
  pkgs,
  go,
  batsLane,
  bats-libs,
  tap-dancer-go,
  tap-dancer-bash,
  batsSrc,
  batsTestTimeout ? "10",
}:
let
  inherit (pkgs) lib;

  # Single source of truth for the per-lane builder. Callers needing
  # ad-hoc filters or alternate base derivations (e.g. tap-dancer-rust)
  # call this directly.
  mkBatsLane =
    {
      filter ? "",
      base ? tap-dancer-go,
    }:
    batsLane {
      inherit base filter batsSrc;
      binaries = {
        TAP_DANCER_BIN = {
          inherit base;
          name = "tap-dancer";
        };
      };
      batsLibPath = [ bats-libs.batsLibPath ];
      extraEnv = {
        BATS_TEST_TIMEOUT = batsTestTimeout;
        TAP_DANCER_LIB = "${tap-dancer-bash}/share/tap-dancer/lib";
        # `go test` invoked from zz-tests_bats/test_runners_ndjson.bats
        # would otherwise see `go 1.26` in the fixture go.mod and try
        # to download a matching toolchain from the network — which
        # the nix sandbox blocks, leading to test timeouts. Match
        # tap-dancer-go's own setting (flake.nix).
        GOTOOLCHAIN = "local";
      };
      # `go` is needed by zz-tests_bats/test_runners_ndjson.bats, which
      # builds a tiny Go module on the fly and runs `tap-dancer go-test`
      # against it. Outside the nix sandbox the devshell already
      # provides Go, but inside the sandbox the toolchain has to be
      # threaded through nativeBuildInputs explicitly.
      nativeBuildInputs = [
        pkgs.jq
        go
      ];
    };

  batsFiles = lib.filter (f: lib.hasSuffix ".bats" f) (builtins.attrNames (builtins.readDir batsSrc));

  # Strip spaces from a tag string so `file_tags=a, b` doesn't produce
  # a derivation named `bats- b`. Tags are identifiers; internal spaces
  # are not valid, so removing all spaces is safe.
  stripSpaces = s: builtins.replaceStrings [ " " ] [ "" ] s;

  extractFileTags =
    file:
    let
      content = builtins.readFile (batsSrc + "/${file}");
      lines = lib.splitString "\n" content;
      tagLines = lib.filter (l: lib.hasPrefix "# bats file_tags=" l) lines;
    in
    if tagLines == [ ] then
      [ ]
    else
      map stripSpaces (
        lib.splitString "," (lib.removePrefix "# bats file_tags=" (builtins.head tagLines))
      );

  allFileTags = lib.unique (lib.concatMap extractFileTags batsFiles);

  batsLaneOutputs =
    lib.listToAttrs (
      map (
        tag:
        lib.nameValuePair "bats-${tag}" (mkBatsLane {
          filter = tag;
        })
      ) allFileTags
    )
    // {
      bats-default = mkBatsLane { };
    };
in
{
  inherit mkBatsLane batsLaneOutputs;
}
