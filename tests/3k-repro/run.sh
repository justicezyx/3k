#!/usr/bin/env bash
# Historical repro: clone public NascentCore/3k and check out 17b19cf,
# copy these tests in, and run them. Does not push to that repository.
#
# justicezyx/3k main is ahead of 17b19cf. This script does not run the
# suite against the current checkout. The pin is the tree the gap note
# was verified against.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PIN="17b19cf"
WORKDIR="$(mktemp -d)"
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

export REPRO_MYSQL_DSN="${REPRO_MYSQL_DSN:-repro:repro@tcp(127.0.0.1:3306)/}"

echo "cloning NascentCore/3k @ ${PIN} into ${WORKDIR}"
git clone --filter=blob:none https://github.com/NascentCore/3k "$WORKDIR/3k"
git -C "$WORKDIR/3k" checkout "$PIN"

# Mirror package paths. designmodel stays in this repo and is not copied in.
while IFS= read -r -d '' f; do
  rel="${f#"$ROOT"/}"
  case "$rel" in
    designmodel/*) continue ;;
  esac
  mkdir -p "$WORKDIR/3k/$(dirname "$rel")"
  cp "$f" "$WORKDIR/3k/$rel"
done < <(find "$ROOT" -name '*_test.go' -print0)

echo "=== scheduler logic ==="
(cd "$WORKDIR/3k" && go test -count=1 -timeout 30m -v ./internal/scheduler/logic/ -run 'TestP')

echo "=== scheduler pay ==="
(cd "$WORKDIR/3k" && go test -count=1 -timeout 10m -v ./internal/scheduler/pay/ -run 'TestP')

echo "=== operator synchronizer ==="
(cd "$WORKDIR/3k/cpodoperator" && go test -count=1 -timeout 20m -v ./internal/synchronizer/ -run 'TestP')

echo "=== operator controller ==="
# Pre-existing *_test.go files in this package do not compile at 17b19cf
# (InferenceReconciler has no DeployWebUI). Move them aside. Production
# sources stay put. go test also vets the package, and inference_controller.go
# fails vet (logrus.Infof with no format verb), so vet is off for this package.
ctrl="$WORKDIR/3k/cpodoperator/internal/controller"
mkdir -p "$ctrl/_preexisting_tests"
for f in "$ctrl"/*_test.go; do
  base="$(basename "$f")"
  case "$base" in
    p7_p12_p30_test.go) ;;
    *) mv "$f" "$ctrl/_preexisting_tests/" ;;
  esac
done
(cd "$WORKDIR/3k/cpodoperator" && go test -vet=off -count=1 -timeout 20m -v ./internal/controller/ -run 'TestP')

echo "=== design model ==="
(cd "$ROOT/designmodel" && go test -count=1 -timeout 5m -v ./...)

echo "all requested packages finished"
