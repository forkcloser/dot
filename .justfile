# This file is the project's own — add recipes below. Keep the import: it
# mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# The FIRST recipe defined here becomes `just`'s default.
lint: do::lint::go::default do::lint::go::deadcode do::lint::default
fix: do::fix::go::default do::fix::default
test: oracle
    #!/usr/bin/env bash
    set -euo pipefail
    # With a native dot pinned for this platform, `oracle` has fetched it:
    # the tests are given it, and fail where they would skip when it is not
    # there or is not the engine's release. limen fails the same way for a
    # pin that is not there and for pins it cannot read, so a pin that is
    # always there tells the two apart.
    limen pins get dot-linux-amd64 version > /dev/null
    host="$(go env GOHOSTOS)-$(go env GOHOSTARCH)"
    if limen pins get "dot-${host}" url > /dev/null 2>&1; then
      exe="$(go env GOEXE)"
      export DOT_ORACLE="${PWD}/build/oracle/graphviz-${host}/dot${exe}"
      export DOT_ORACLE_REQUIRED=1
    fi
    just do test go unit
    just do test go race
# The security workflow runs `just security`.
security: do::security::default

# The oracle of the tests that hold the command to Graphviz's own dot: the
# native dot forkcloser/testbin builds, unpatched, pinned per platform in
# pins.yaml (dot-<os>-<arch>) and verified against its digest. It lands in
# build/oracle/graphviz-<os>-<arch>. A platform with no pin gets nothing, and
# the tests take the dot DOT_ORACLE names or skip.
[doc('Fetch the pinned native dot the tests hold the command to (build/oracle)')]
oracle:
    #!/usr/bin/env bash
    set -euo pipefail
    host="$(go env GOHOSTOS)-$(go env GOHOSTARCH)"
    # See `test` for this first line.
    limen pins get dot-linux-amd64 version > /dev/null
    if ! url="$(limen pins get "dot-${host}" url 2> /dev/null)"; then
      echo "no native dot is pinned for ${host}" >&2
      exit 0
    fi
    sum="$(limen pins get "dot-${host}" sha256)"
    version="$(limen pins get "dot-${host}" version)"
    dl="build/oracle/dl/graphviz-${host}-${version}.tar.gz"
    mkdir -p build/oracle/dl
    if [ ! -f "${dl}" ]; then
      curl --proto '=https' --tlsv1.2 -fsSL --retry 5 --retry-delay 3 -o "${dl}.part" "${url}"
      mv "${dl}.part" "${dl}"
    fi
    echo "${sum}  ${dl}" | sha256sum -c - > /dev/null || {
      echo "dot-${host}: sha256 mismatch against pins.yaml" >&2
      rm -f "${dl}"
      exit 1
    }
    # Unpacked anew each time: the directory's name carries no version.
    rm -rf "build/oracle/graphviz-${host}"
    tar -xzf "${dl}" -C build/oracle
