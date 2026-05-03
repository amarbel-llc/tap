cmd_nix_dev := "nix develop " + justfile_directory() + " --command "

default: build test

# Build all impls (default = symlinkJoin of cli + rust + bash)
build:
    nix build

build-cli:
    nix build .#tap-dancer-go

build-rust:
    nix build .#tap-dancer-rust

build-bash:
    nix build .#tap-dancer-bash

test: test-go test-rust test-bats

test-go:
    {{cmd_nix_dev}} bash -c 'cd go && go test ./...'

test-rust:
    {{cmd_nix_dev}} bash -c 'cd rust && cargo test'

# Requires built CLI on disk at result/bin/tap-dancer
test-bats: build-cli
    TAP_DANCER_BIN=$PWD/result/bin/tap-dancer \
      {{cmd_nix_dev}} bats zz-tests_bats/

fmt:
    {{cmd_nix_dev}} bash -c 'cd go && gofumpt -w .'
    {{cmd_nix_dev}} bash -c 'cd rust && cargo fmt'
    {{cmd_nix_dev}} nixfmt flake.nix

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
    # `go get github.com/amarbel-llc/tap/go@latest` resolves to a real
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
