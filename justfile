cmd_nix_dev := "nix develop " + justfile_directory() + " --command "

default: build test

# Build all impls (default = symlinkJoin of cli + rust + bash)
build:
    nix build

# Used by the spinclass pre-merge hook. Builds the default package
# AND runs `nix flake check` so flake-schema regressions (e.g. the
# go-pkgs/derivation class — see #23, nixpkgs#38/#44) and lane
# breakage (e.g. #26 — bats-default failing at eval) are caught before
# merge instead of after the fact.
build-nix: build
    nix flake check

build-cli:
    nix build .#tap-dancer-go

build-rust:
    nix build .#tap-dancer-rust

build-bash:
    nix build .#tap-dancer-bash

# Compile the scdoc section-7 manpages (tap-ndjson(7), etc.)
build-doc:
    nix build .#tap-dancer-doc

# Regenerate the go/pkgs/* re-export facades from the internal packages
# via dagnabit (driven by the //go:generate dagnabit export directives).
# Must run from the go module root: `go generate` can't drive it because
# it cd's into the package dir, which has no go.mod.
#
# DAGNABIT_CEILING_DIRECTORIES bounds dagnabit's upward formatter-config
# search at the repo root: tap has no on-disk conformist/treefmt config
# (formatting rides the flake's treefmt-nix), so an unbounded walk
# escalates to a stray ancestor conformist.toml (an eng-root checkout) —
# wrong config at best, a hard failure via eng's cwd-guarded conformist
# wrapper at worst. Bounded, the facade-format pass is a documented no-op
# and `nix fmt` remains the tree's formatter.
build-facades:
    {{cmd_nix_dev}} bash -c 'cd go && DAGNABIT_CEILING_DIRECTORIES="$(git rev-parse --show-toplevel)" dagnabit export'

test: test-go test-rust test-bats

test-go:
    {{cmd_nix_dev}} bash -c 'cd go && go test ./...'

test-rust:
    {{cmd_nix_dev}} bash -c 'cd rust && cargo test'

# Requires built CLI on disk at result/bin/tap-dancer
test-bats: build
    TAP_DANCER_BIN=$PWD/result/bin/tap-dancer \
    TAP_DANCER_LIB=$PWD/result/share/tap-dancer/lib \
      {{cmd_nix_dev}} bats zz-tests_bats/

# Run the bats suite as a nix derivation (`bats-default` lane). Same
# code path `nix flake check` exercises — staged, hermetic, runs
# against a freshly built tap-dancer. Slower than `test-bats` but
# proves a clean rebuild still passes the suite.
test-bats-default:
    nix build .#bats-default --print-build-logs --no-link

# Run a tag-filtered bats lane. Lanes are auto-generated from
# `# bats file_tags=` directives in zz-tests_bats/*.bats; `nix flake
# show` lists what's currently available.
test-bats-tags *tags:
    nix build --print-build-logs --no-link .#bats-{{tags}}

fmt *ARGS:
    nix fmt -- {{ARGS}}

lint:
    {{cmd_nix_dev}} bash -c 'cd go && go vet ./...'
    {{cmd_nix_dev}} bash -c 'cd rust && cargo clippy'

# Re-pin flake inputs
update:
    nix flake update

clean:
    rm -rf result result-*

# Cut a release: bump version everywhere, run tests, tag, push, and create a
# GitHub release. Usage: just release 0.3.0
#
# Refuses to run if:
#   - the version arg is not semver (X.Y.Z, optionally with -prerelease)
#   - the working tree is dirty
#   - the tag already exists locally
#   - HEAD does not contain origin/master (would push un-merged work)
release version:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! "{{version}}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$ ]]; then
      echo "release: version must be semver (X.Y.Z[-prerelease]), got: {{version}}" >&2
      exit 1
    fi
    tag="v{{version}}"
    # Subpath-prefixed tag for the Go module at go/. Required so
    # `go get code.linenisgreat.com/tap/go@latest` resolves to a real
    # semver instead of a v0.0.0-<date>-<sha> pseudo-version.
    go_tag="go/$tag"

    if ! git diff --quiet || ! git diff --cached --quiet; then
      echo "release: working tree is dirty; commit or stash first" >&2
      exit 1
    fi
    if git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null; then
      echo "release: tag $tag already exists locally" >&2
      exit 1
    fi
    if git rev-parse --verify --quiet "refs/tags/$go_tag" >/dev/null; then
      echo "release: tag $go_tag already exists locally" >&2
      exit 1
    fi
    git fetch --quiet origin master
    if ! git merge-base --is-ancestor origin/master HEAD; then
      echo "release: HEAD does not contain origin/master; rebase first" >&2
      exit 1
    fi

    echo "VERSION={{version}}" > version.env
    sed -i -E 's/^version = "[^"]+"$/version = "{{version}}"/' rust/Cargo.toml
    sed -i -E 's/^version: .+$/version: {{version}}/' skills/tap14/SKILL.md

    just test

    git add version.env rust/Cargo.toml rust/Cargo.lock skills/tap14/SKILL.md
    git commit -m "release: $tag"
    git tag -s "$tag" -m "$tag"
    git tag -s "$go_tag" -m "$go_tag"

    git push origin HEAD:refs/heads/master
    git push origin "$tag" "$go_tag"

    {{cmd_nix_dev}} gh release create "$tag" --generate-notes --title "$tag"
