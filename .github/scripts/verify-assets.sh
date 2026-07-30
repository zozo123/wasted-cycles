#!/usr/bin/env bash
#
# Assert that dist/ satisfies the contract the `run` installer depends on.
#
#   bash .github/scripts/verify-assets.sh
#
# Everything here mirrors ./run literally. If ./run changes, change this too.

set -euo pipefail

root="$PWD"
cd dist

fail=0

# 1. Asset names. `run` builds "wasted-cycles_${os}_${arch}.tar.gz" from
#    `uname -s` lowercased (darwin|linux) and `uname -m` normalized
#    (x86_64|amd64 -> x86_64, arm64|aarch64 -> arm64).
for os in darwin linux; do
  for arch in x86_64 arm64; do
    asset="wasted-cycles_${os}_${arch}.tar.gz"

    if [ ! -f "$asset" ]; then
      echo "missing asset: $asset" >&2
      fail=1
      continue
    fi

    # 2. The exact awk lookup ./run uses against the single checksums.txt.
    expected="$(awk -v file="$asset" '$2 == file { print $1 }' checksums.txt)"
    actual="$(sha256sum "$asset" | awk '{print $1}')"

    if [ -z "$expected" ]; then
      echo "checksums.txt has no entry matching field 2 == $asset" >&2
      echo "(a leading '*' or a directory prefix in the filename column breaks this)" >&2
      fail=1
      continue
    fi

    if [ "$expected" != "$actual" ]; then
      echo "checksum mismatch for $asset (expected=$expected actual=$actual)" >&2
      fail=1
      continue
    fi

    # 3. The binary must be the only entry and live at the archive ROOT under
    #    the name `wasted-cycles`; ./run executes "$tmp_root/wasted-cycles".
    entries="$(tar -tzf "$asset")"
    if [ "$entries" != "wasted-cycles" ]; then
      echo "unexpected archive layout for $asset:" >&2
      echo "$entries" >&2
      fail=1
      continue
    fi

    echo "OK  $asset  $expected"
  done
done

# Windows assets are not consumed by ./run, but hold them to the same
# archive-root rule so a manual download works without nested directories.
for arch in x86_64 arm64; do
  asset="wasted-cycles_windows_${arch}.zip"

  if [ ! -f "$asset" ]; then
    echo "missing asset: $asset" >&2
    fail=1
    continue
  fi

  expected="$(awk -v file="$asset" '$2 == file { print $1 }' checksums.txt)"
  if [ -z "$expected" ]; then
    echo "checksums.txt has no entry matching field 2 == $asset" >&2
    fail=1
    continue
  fi

  entries="$(unzip -Z1 "$asset")"
  if [ "$entries" != "wasted-cycles.exe" ]; then
    echo "unexpected archive layout for $asset:" >&2
    echo "$entries" >&2
    fail=1
    continue
  fi

  echo "OK  $asset  $expected"
done

# 4. Exactly one checksums.txt covering every asset, one line each.
shopt -s nullglob
built=(wasted-cycles_*)
shopt -u nullglob
# Numeric comparison: `wc -l` pads its output with spaces on some platforms.
lines="$(wc -l <checksums.txt | tr -d '[:space:]')"
if [ "${#built[@]}" -ne "$lines" ]; then
  echo "checksums.txt covers $lines of ${#built[@]} assets" >&2
  fail=1
fi

# 5. The version ldflag actually landed: a host-native binary must print the
#    tag rather than the "dev" default baked into cmd/wasted-cycles/main.go.
native="$root/build/linux_amd64/wasted-cycles"
if [ -n "${TAG:-}" ] && [ -x "$native" ]; then
  got="$("$native" --version)"
  if [ "$got" != "$TAG" ]; then
    echo "--version printed '$got', expected '$TAG' (check -X main.version)" >&2
    fail=1
  else
    echo "OK  --version -> $got"
  fi
fi

exit "$fail"
