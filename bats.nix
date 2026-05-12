# bats integration test lanes for tap-dancer.
#
# Wraps `pkgs.testers.batsLane` (provided by the amarbel-llc/nixpkgs
# overlay — see amarbel-llc/nixpkgs#14) with tap-dancer-specific
# defaults: `bats-libs` from amarbel-llc/bats on `BATS_LIB_PATH`,
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
  bats-libs,
  tap-dancer-go,
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
    pkgs.testers.batsLane {
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
      };
      nativeBuildInputs = [ pkgs.jq ];
    };

  batsFiles = lib.filter (f: lib.hasSuffix ".bats" f) (
    builtins.attrNames (builtins.readDir batsSrc)
  );

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
      lib.splitString "," (lib.removePrefix "# bats file_tags=" (builtins.head tagLines));

  allFileTags = lib.unique (lib.concatMap extractFileTags batsFiles);

  batsLaneOutputs =
    lib.listToAttrs (
      map (tag: lib.nameValuePair "bats-${tag}" (mkBatsLane { filter = tag; })) allFileTags
    )
    // {
      bats-default = mkBatsLane { };
    };
in
{
  inherit mkBatsLane batsLaneOutputs;
}
