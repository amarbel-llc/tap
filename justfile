cmd_nix_dev := "nix develop " + justfile_directory() + " --command "

default: build test

# Build all impls (default = symlinkJoin of cli + rust + bash)
build:
    nix build

build-cli:
    nix build .#tap-dancer-cli

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
