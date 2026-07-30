#!/usr/bin/env bash
#
# Build every release target into dist/ and write dist/checksums.txt.
#
#   TAG=v1.2.3 SOURCE_DATE_EPOCH=1730000000 bash .github/scripts/package.sh
#
# Both the release workflow and CI run this exact script, so the artifacts CI
# validates on a pull request are produced the same way as the ones a tag
# publishes. Requires GNU tar/coreutils (the GitHub ubuntu runner image).

set -euo pipefail

TAG="${TAG:-dev}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --pretty=%ct)}"
export SOURCE_DATE_EPOCH
export CGO_ENABLED=0
export GOFLAGS="${GOFLAGS:--mod=readonly}"
export TZ=UTC

rm -rf dist build
mkdir -p dist build

# "<GOOS> <GOARCH>". The asset name normalizes amd64 -> x86_64 to match the
# `run` installer, which derives its arch token from `uname -m` (x86_64 on
# Intel/AMD, arm64 on Apple Silicon, aarch64 folded to arm64). Fed by a
# here-doc rather than a pipe so the loop body runs in this shell and `set -e`
# failures abort the script.
while read -r goos goarch; do
  [ -n "${goos:-}" ] || continue

  case "$goarch" in
    amd64) archname="x86_64" ;;
    arm64) archname="arm64" ;;
    *)
      echo "unhandled GOARCH: $goarch" >&2
      exit 1
      ;;
  esac

  stage="build/${goos}_${goarch}"
  rm -rf "$stage"
  mkdir -p "$stage"

  binary="wasted-cycles"
  if [ "$goos" = "windows" ]; then
    binary="wasted-cycles.exe"
  fi

  echo "==> building ${goos}/${goarch} -> ${stage}/${binary}"
  # `-X main.version=` is the correct symbol path: `version` lives in package
  # main at cmd/wasted-cycles/main.go, and the linker names main-package
  # symbols `main.<ident>` regardless of the import path.
  GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -buildvcs=false \
    -ldflags "-s -w -X main.version=${TAG}" \
    -o "${stage}/${binary}" \
    ./cmd/wasted-cycles

  touch -d "@${SOURCE_DATE_EPOCH}" "${stage}/${binary}"

  if [ "$goos" = "windows" ]; then
    asset="wasted-cycles_${goos}_${archname}.zip"
    # -X drops extra file attributes, -j stores the bare filename so the .exe
    # lands at the archive root.
    (cd "$stage" && zip -q -X -j "../../dist/${asset}" "$binary")
  else
    asset="wasted-cycles_${goos}_${archname}.tar.gz"
    # The binary must sit at the archive ROOT named `wasted-cycles`: `run`
    # extracts with `tar -xzf ... -C "$tmp_root"` and then executes
    # "$tmp_root/wasted-cycles". gzip -n omits the timestamp/name so the
    # archive bytes are stable.
    tar \
      --format=ustar \
      --sort=name \
      --owner=0 --group=0 --numeric-owner \
      --mtime="@${SOURCE_DATE_EPOCH}" \
      -C "$stage" -cf - "$binary" |
      gzip -9 -n >"dist/${asset}"
  fi

  echo "    packaged dist/${asset}"
done <<'TARGETS'
darwin amd64
darwin arm64
linux amd64
linux arm64
windows amd64
windows arm64
TARGETS

# Two-space `sha256sum` format with BARE filenames (no `./`, no directory
# prefix, no `*` binary-mode marker) so the installer's
#   awk -v file="$asset" '$2 == file { print $1 }'
# matches on the second field. One checksums.txt covering every asset.
(cd dist && sha256sum wasted-cycles_* >checksums.txt)

ls -l dist
cat dist/checksums.txt
