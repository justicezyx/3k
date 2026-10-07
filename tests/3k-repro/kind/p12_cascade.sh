#!/usr/bin/env bash
# P12 on a real kind cluster: the cpodoperator creates the checkpoint PVC
# with a controlling ownerReference, and kube-controller-manager garbage
# collection deletes that PVC when the CPodJob is deleted and, separately,
# when the FineTune parent is deleted.
#
# Requires docker, kind, and kubectl. Does not modify NascentCore/3k and
# does not push to that repository. No GPUs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIN="17b19cf"
CLUSTER="${P12_CLUSTER:-p12-cascade}"
SRC="${P12_3K_DIR:-}"
# Transcripts are written to a temp dir and copied into the checkout only
# after the checks pass, so a failed run does not delete committed evidence.
COMMITTED="${P12_EVIDENCE:-$ROOT/kind/evidence}"
EVIDENCE="$(mktemp -d)"
OPERATOR_BIN="${P12_OPERATOR_BIN:-}"
NS_JOB="p12-job"
NS_FT="p12-ft"
JOB_NAME="p12job"
FT_NAME="p12ft"
CPOD_FROM_FT="${FT_NAME}-cpodjob"
PUBLIC_MODEL="model-storage-10e872cd960e38cb"
MODEL_NAME="ZhipuAI/chatglm3-6b"

mkdir -p "$EVIDENCE" "$COMMITTED"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is unavailable (docker not on PATH)" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker is unavailable (daemon not reachable)" >&2
  exit 1
fi
command -v kind >/dev/null 2>&1 || { echo "kind is not installed" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is not installed" >&2; exit 1; }

if [[ -z "$SRC" ]]; then
  SRC="$(mktemp -d)/3k"
  echo "cloning NascentCore/3k @ ${PIN} into ${SRC}"
  git clone --filter=blob:none https://github.com/NascentCore/3k "$SRC"
  git -C "$SRC" checkout "$PIN"
else
  got="$(git -C "$SRC" rev-parse HEAD)"
  case "$got" in
    ${PIN}*) ;;
    *) echo "P12_3K_DIR is ${got}, want ${PIN}" >&2; exit 1 ;;
  esac
fi

KUBECONFIG_FILE="${P12_KUBECONFIG:-$EVIDENCE/kubeconfig}"
if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "creating kind cluster ${CLUSTER}"
  kind create cluster --name "$CLUSTER" --wait 180s
fi
kind get kubeconfig --name "$CLUSTER" > "$KUBECONFIG_FILE"
export KUBECONFIG="$KUBECONFIG_FILE"
kubectl cluster-info >/dev/null
kubectl wait --for=condition=Ready node --all --timeout=180s

python3 - "$SRC" "$EVIDENCE/crds" <<'PY'
import os, sys, yaml
src, out = sys.argv[1], sys.argv[2]
os.makedirs(out, exist_ok=True)

def docs(path):
    buf = []
    with open(path) as f:
        for line in f:
            if line.strip() == "---":
                if any(x.strip() for x in buf):
                    yield "\n".join(buf)
                buf = []
            else:
                buf.append(line.rstrip("\n"))
    if any(x.strip() for x in buf):
        yield "\n".join(buf)

bases = os.path.join(src, "cpodoperator/config/crd/bases")
keep = [
    "cpod.cpod_cpodjobs.yaml",
    "cpod.cpod_finetunes.yaml",
    "cpod.cpod_inferences.yaml",
    "cpod.cpod_jupyterlabs.yaml",
    "cpod.cpod_modelstorages.yaml",
    "cpod.cpod_datasetstorages.yaml",
    "cpod.cpod_yamlresources.yaml",
]
for name in keep:
    open(os.path.join(out, name), "w").write(open(os.path.join(bases, name)).read())

wanted = {
    os.path.join(src, "deployment/yaml_apps/mpi-operator.yaml"): {"mpijobs.kubeflow.org"},
    os.path.join(src, "deployment/yaml_apps/kserve.yaml"): {"inferenceservices.serving.kserve.io"},
    # GetBaseJob looks up a PyTorchJob before CreateBaseJob calls GetCKPTPVC.
    # Without this CRD that lookup is a NoMatch error, not NotFound, so the
    # checkpoint PVC is never created.
    os.path.join(src, "deployment/yaml_apps/training-operator.yaml"): {"pytorchjobs.kubeflow.org"},
}
for path, names in wanted.items():
    for text in docs(path):
        head = "\n".join(text.splitlines()[:30])
        if "kind: CustomResourceDefinition" not in head:
            continue
        doc = yaml.safe_load(text)
        if not isinstance(doc, dict) or doc.get("kind") != "CustomResourceDefinition":
            continue
        meta_name = doc.get("metadata", {}).get("name")
        if meta_name not in names:
            continue
        # The pinned InferenceService CRD converts via a kserve webhook that
        # this cascade does not run. Drop conversion so the v1beta1 kind
        # the operator imports can establish. Stored schema is unchanged.
        spec = doc.setdefault("spec", {})
        spec.pop("conversion", None)
        out_name = meta_name.replace(".", "_") + ".yaml"
        with open(os.path.join(out, out_name), "w") as f:
            yaml.safe_dump(doc, f, sort_keys=False)
        print("extracted", meta_name)
PY

echo "applying CRDs"
kubectl apply -f "$EVIDENCE/crds"
kubectl wait --for=condition=Established crd/cpodjobs.cpod.cpod --timeout=120s
kubectl wait --for=condition=Established crd/finetunes.cpod.cpod --timeout=120s
kubectl wait --for=condition=Established crd/mpijobs.kubeflow.org --timeout=180s
kubectl wait --for=condition=Established crd/pytorchjobs.kubeflow.org --timeout=180s
kubectl wait --for=condition=Established crd/inferenceservices.serving.kserve.io --timeout=180s
kubectl wait --for=condition=Established crd/modelstorages.cpod.cpod --timeout=120s
kubectl wait --for=condition=Established crd/datasetstorages.cpod.cpod --timeout=120s

kubectl apply -f - <<'EOF'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: juicefs-sc
provisioner: kubernetes.io/no-provisioner
reclaimPolicy: Retain
volumeBindingMode: WaitForFirstConsumer
EOF

if [[ -z "$OPERATOR_BIN" ]]; then
  OPERATOR_BIN="$EVIDENCE/cpodoperator"
  echo "building cpodoperator from ${SRC}"
  (cd "$SRC/cpodoperator" && go build -o "$OPERATOR_BIN" ./cmd/operator)
fi

OP_LOG="$EVIDENCE/operator.log"
if [[ -f "$EVIDENCE/operator.pid" ]]; then
  old="$(cat "$EVIDENCE/operator.pid" || true)"
  if [[ -n "${old}" ]] && kill -0 "$old" 2>/dev/null; then
    kill "$old" || true
    wait "$old" 2>/dev/null || true
  fi
fi
: > "$OP_LOG"
# Out-of-cluster run of the real operator main from 17b19cf.
# Kubeconfig is cluster-admin, so the in-tree RBAC manifests are not required.
"$OPERATOR_BIN" \
  --metrics-bind-address=127.0.0.1:18080 \
  --health-probe-bind-address=127.0.0.1:18081 \
  --leader-elect=false \
  --storageClassName=juicefs-sc \
  > "$OP_LOG" 2>&1 &
echo $! > "$EVIDENCE/operator.pid"
cleanup() {
  if [[ -f "$EVIDENCE/operator.pid" ]]; then
    pid="$(cat "$EVIDENCE/operator.pid" || true)"
    if [[ -n "${pid}" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" || true
      wait "$pid" 2>/dev/null || true
    fi
  fi
  rm -rf "$EVIDENCE"
}
trap cleanup EXIT

echo "waiting for operator"
for _ in $(seq 1 40); do
  if grep -q "starting manager" "$OP_LOG"; then
    break
  fi
  if ! kill -0 "$(cat "$EVIDENCE/operator.pid")" 2>/dev/null; then
    echo "operator exited before it became ready" >&2
    cat "$OP_LOG" >&2
    exit 1
  fi
  sleep 1
done
grep -q "starting manager" "$OP_LOG"

run() {
  local file="$1"
  shift
  {
    echo "\$ $*"
    "$@"
  } > "$file" 2>&1
}

# Parse ownerReferences. The transcript starts with a "$ kubectl" line.
assert_owner() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys, yaml
path, kind, name = sys.argv[1:]
lines = open(path).read().splitlines()
if lines and lines[0].startswith("$ "):
    lines = lines[1:]
docs = [d for d in yaml.safe_load_all("\n".join(lines)) if isinstance(d, dict)]
if len(docs) != 1:
    sys.exit("%s: expected one object, got %d" % (path, len(docs)))
refs = (docs[0].get("metadata") or {}).get("ownerReferences") or []
for ref in refs:
    if (ref.get("kind") == kind and ref.get("name") == name
            and ref.get("controller") is True and ref.get("blockOwnerDeletion") is True):
        print("owner ref ok: %s/%s" % (kind, name))
        sys.exit(0)
sys.exit("%s: no controlling ownerReference kind=%s name=%s" % (path, kind, name))
PY
}

wait_exists() {
  local kind="$1" ns="$2" name="$3"
  for _ in $(seq 1 45); do
    if kubectl get "$kind" -n "$ns" "$name" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${kind}/${name} in ${ns}" >&2
  kubectl get "$kind" -n "$ns" >&2 || true
  echo "--- operator log tail ---" >&2
  tail -80 "$OP_LOG" >&2 || true
  return 1
}

wait_gone() {
  local kind="$1" ns="$2" name="$3"
  for _ in $(seq 1 45); do
    if ! kubectl get "$kind" -n "$ns" "$name" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${kind}/${name} in ${ns} to be garbage-collected" >&2
  kubectl get "$kind" -n "$ns" "$name" -o yaml >&2 || true
  return 1
}

# Drop leftovers from a previous run of this script.
kubectl delete namespace "$NS_JOB" "$NS_FT" public --wait=true --ignore-not-found --timeout=180s >/dev/null

echo "=== CPodJob delete cascades to the checkpoint PVC ==="
kubectl create namespace "$NS_JOB"
kubectl apply -f - <<EOF
apiVersion: cpod.cpod/v1beta1
kind: CPodJob
metadata:
  name: ${JOB_NAME}
  namespace: ${NS_JOB}
spec:
  image: busybox:1.36
  jobType: pytorch
  ckptPath: /data/ckpt
  ckptVolumeSize: 10
EOF
wait_exists pvc "$NS_JOB" "${JOB_NAME}-ckpt"
run "$EVIDENCE/01-cpodjob-before.txt" kubectl get cpodjob,pvc -n "$NS_JOB" -o wide
run "$EVIDENCE/02-cpodjob-pvc.yaml" kubectl get pvc -n "$NS_JOB" "${JOB_NAME}-ckpt" -o yaml
# Controlling owner ref, same fields generateOwnerRefCPodJob sets.
assert_owner "$EVIDENCE/02-cpodjob-pvc.yaml" CPodJob "$JOB_NAME"
run "$EVIDENCE/03-cpodjob-delete.txt" kubectl delete cpodjob -n "$NS_JOB" "$JOB_NAME" --cascade=background --wait=true
wait_gone pvc "$NS_JOB" "${JOB_NAME}-ckpt"
run "$EVIDENCE/04-cpodjob-after.txt" kubectl get cpodjob,pvc -n "$NS_JOB"
# kubectl get exits 0 even when the table is empty. Require NotFound in the file.
grep -q "No resources found" "$EVIDENCE/04-cpodjob-after.txt"

echo "=== FineTune delete follows the owner chain to the checkpoint PVC ==="
kubectl create namespace public
kubectl create namespace "$NS_FT"
# Public model the FineTune reconciler copies before it creates the CPodJob.
# CopyPublicModelStorage reads this PV's CSI volumeHandle.
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: p12-public-model-pv
spec:
  capacity:
    storage: 1Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  storageClassName: juicefs-sc
  csi:
    driver: csi.juicefs.com
    volumeHandle: p12-public-model-handle
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: p12-public-model-pvc
  namespace: public
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: juicefs-sc
  volumeName: p12-public-model-pv
  resources:
    requests:
      storage: 1Gi
---
apiVersion: cpod.cpod/v1
kind: ModelStorage
metadata:
  name: ${PUBLIC_MODEL}
  namespace: public
spec:
  modelname: chatglm3-6b
  template: alpaca
  pvc: p12-public-model-pvc
EOF
kubectl patch modelstorage -n public "$PUBLIC_MODEL" --subresource=status --type=merge \
  -p '{"status":{"phase":"done"}}'
# Private dataset already done, so PrepareData does not start a downloader
# and CreateBaseJob reaches GetCKPTPVC.
kubectl apply -f - <<EOF
apiVersion: cpod.cpod/v1
kind: DataSetStorage
metadata:
  name: p12ds
  namespace: ${NS_FT}
spec:
  datasetname: p12ds
  datasettype: dataset
EOF
kubectl patch datasetstorage -n "$NS_FT" p12ds --subresource=status --type=merge \
  -p '{"status":{"phase":"done"}}'
kubectl apply -f - <<EOF
apiVersion: cpod.cpod/v1beta1
kind: FineTune
metadata:
  name: ${FT_NAME}
  namespace: ${NS_FT}
spec:
  model: ${MODEL_NAME}
  dataset: p12ds
  datasetIsPublic: false
  gpuCount: 1
EOF
wait_exists cpodjob "$NS_FT" "$CPOD_FROM_FT"
wait_exists pvc "$NS_FT" "${CPOD_FROM_FT}-ckpt"
run "$EVIDENCE/05-finetune-before.txt" kubectl get finetune,cpodjob,pvc -n "$NS_FT" -o wide
run "$EVIDENCE/06-finetune-cpodjob.yaml" kubectl get cpodjob -n "$NS_FT" "$CPOD_FROM_FT" -o yaml
run "$EVIDENCE/07-finetune-pvc.yaml" kubectl get pvc -n "$NS_FT" "${CPOD_FROM_FT}-ckpt" -o yaml
assert_owner "$EVIDENCE/06-finetune-cpodjob.yaml" FineTune "$FT_NAME"
assert_owner "$EVIDENCE/07-finetune-pvc.yaml" CPodJob "$CPOD_FROM_FT"
run "$EVIDENCE/08-finetune-delete.txt" kubectl delete finetune -n "$NS_FT" "$FT_NAME" --cascade=background --wait=true
wait_gone cpodjob "$NS_FT" "$CPOD_FROM_FT"
wait_gone pvc "$NS_FT" "${CPOD_FROM_FT}-ckpt"
# The public-model copy PVC has no owner ref to the FineTune. It is not part
# of the checkpoint cascade, so the check is the CPodJob and the ckpt claim.
{
  echo "\$ kubectl get cpodjob -n ${NS_FT} ${CPOD_FROM_FT}"
  kubectl get cpodjob -n "$NS_FT" "$CPOD_FROM_FT" || true
  echo
  echo "\$ kubectl get pvc -n ${NS_FT} ${CPOD_FROM_FT}-ckpt"
  kubectl get pvc -n "$NS_FT" "${CPOD_FROM_FT}-ckpt" || true
  echo
  echo "\$ kubectl get pvc -n ${NS_FT}"
  kubectl get pvc -n "$NS_FT"
} > "$EVIDENCE/09-finetune-after.txt" 2>&1
grep -q "Error from server (NotFound): cpodjobs.cpod.cpod \"${CPOD_FROM_FT}\" not found" "$EVIDENCE/09-finetune-after.txt"
grep -q "Error from server (NotFound): persistentvolumeclaims \"${CPOD_FROM_FT}-ckpt\" not found" "$EVIDENCE/09-finetune-after.txt"

# One line per claim the controller created. Later retries only wait for bind.
# A missing line fails the script. wait_exists on the PVC is not this gate.
grep "ckpt pvc not found, create it" "$OP_LOG" > "$EVIDENCE/10-operator-ckpt-lines.txt"
grep -q "\"name\":\"${JOB_NAME}\"" "$EVIDENCE/10-operator-ckpt-lines.txt"
grep -q "\"name\":\"${CPOD_FROM_FT}\"" "$EVIDENCE/10-operator-ckpt-lines.txt"

mkdir -p "$COMMITTED"
cp -f "$EVIDENCE"/[0-9][0-9]-*.txt "$EVIDENCE"/[0-9][0-9]-*.yaml "$COMMITTED"/
echo "P12 cascade reproduced. Evidence copied to ${COMMITTED}"
