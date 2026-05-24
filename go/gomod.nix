# Nix side of go.mod: producer outputs for the flake-input-go_mod
# protocol (amarbel-llc/nixpkgs RFC 0001) plus the tap-dancer-go
# build that self-consumes go-pkgs-test as the producer's own
# contract test.
{
  pkgs,
  self,
  version,
  go,
}:
let
  # extras: tap is polyglot, so anchor go.mod/go.sum/gomod2nix.toml
  # under go/ (mkGoPkgs's default keep-set is repo-root-anchored).
  goPkgs = pkgs.mkGoPkgs {
    src = self;
    extras = [
      "^go/go\\.mod$"
      "^go/go\\.sum$"
      "^go/gomod2nix\\.toml$"
    ];
  };

  tap-dancer-go = pkgs.buildGoApplication {
    pname = "tap-dancer";
    inherit version;
    src = "${goPkgs.go-pkgs-test}/go";
    pwd = "${goPkgs.go-pkgs-test}/go";
    subPackages = [ "cmd/tap-dancer" ];
    modules = ./gomod2nix.toml;
    inherit go;
    GOTOOLCHAIN = "local";
    meta = with pkgs.lib; {
      description = "TAP-14 validator and writer toolkit";
      homepage = "https://github.com/amarbel-llc/tap";
      license = licenses.mit;
    };
  };
in
{
  inherit (goPkgs) go-pkgs go-pkgs-test;
  inherit tap-dancer-go;
}
