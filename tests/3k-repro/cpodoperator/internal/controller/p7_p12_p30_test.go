package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cpodv1 "github.com/NascentCore/cpodoperator/api/v1"
	"github.com/NascentCore/cpodoperator/api/v1beta1"
	mpiv2 "github.com/kubeflow/mpi-operator/pkg/apis/kubeflow/v2beta1"
	tov1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := cpodv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := tov1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := mpiv2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newReconciler(t *testing.T, objs ...client.Object) *CPodJobReconciler {
	t.Helper()
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objs...).Build()
	return &CPodJobReconciler{
		Client: cl,
		Scheme: testScheme(t),
		Option: &CPodJobOption{StorageClassName: "repro-sc"},
	}
}

func cpodJob(name string, kind v1beta1.JobType, gpuPerReplica, replicas int32) *v1beta1.CPodJob {
	return &v1beta1.CPodJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "user-1", UID: types.UID("uid-" + name)},
		Spec: v1beta1.CPodJobSpec{
			JobType:               kind,
			GPURequiredPerReplica: gpuPerReplica,
			WorkerReplicas:        replicas,
			GPUType:               "A100",
			Image:                 "example.invalid/train:latest",
		},
	}
}

func gpuQuantity(list corev1.ResourceList) int64 {
	q, ok := list[corev1.ResourceName("nvidia.com/gpu")]
	if !ok {
		return -1
	}
	return q.Value()
}

// TestP7_PyTorchAndMPIRequestZeroGPU builds the base job from a CPodJob whose
// per-replica GPU count is 0 (what processTrainningJobs stores when GpuNumber >= 8).
// PyTorch and MPI put that 0 on nvidia.com/gpu. The >1 branch sets NprocPerNode
// from WorkerReplicas and does not write a GPU count.
func TestP7_PyTorchAndMPIRequestZeroGPU(t *testing.T) {
	ctx := context.Background()

	t.Run("pytorch", func(t *testing.T) {
		c := newReconciler(t)
		job := cpodJob("pt-16", v1beta1.JobTypePytorch, 0, 2)
		if err := c.CreateBaseJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		got := &tov1.PyTorchJob{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name}, got); err != nil {
			t.Fatal(err)
		}
		worker := got.Spec.PyTorchReplicaSpecs[tov1.PyTorchJobReplicaTypeWorker]
		q := gpuQuantity(worker.Template.Spec.Containers[0].Resources.Requests)
		if q != 0 {
			t.Fatalf("pytorch nvidia.com/gpu request = %d, want 0", q)
		}
		if got.Spec.NprocPerNode == nil || *got.Spec.NprocPerNode != "2" {
			t.Fatalf("NprocPerNode = %v, want 2 (worker replicas, not a GPU count)", got.Spec.NprocPerNode)
		}
		for _, vol := range worker.Template.Spec.Volumes {
			if vol.Name == "shm" {
				t.Fatal("shm was added even though GPURequiredPerReplica is 0")
			}
		}
	})

	t.Run("mpi", func(t *testing.T) {
		c := newReconciler(t)
		job := cpodJob("mpi-8", v1beta1.JobTypeMPI, 0, 1)
		if err := c.CreateBaseJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		got := &mpiv2.MPIJob{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name}, got); err != nil {
			t.Fatal(err)
		}
		worker := got.Spec.MPIReplicaSpecs[mpiv2.MPIReplicaTypeWorker]
		q := gpuQuantity(worker.Template.Spec.Containers[0].Resources.Requests)
		if q != 0 {
			t.Fatalf("mpi nvidia.com/gpu request = %d, want 0", q)
		}
	})
}

// TestP12_CheckpointPVCOwnerRefCascade creates the checkpoint PVC with a
// controlling owner ref. releaseSavedModel strips an owner ref only on the
// model-save PVC, and only when that path is set. The checkpoint ref stays.
func TestP12_CheckpointPVCOwnerRefCascade(t *testing.T) {
	ctx := context.Background()
	c := newReconciler(t)
	job := cpodJob("job-ckpt", v1beta1.JobTypePytorch, 1, 1)
	job.Spec.CKPTVolumeSize = 10
	job.Spec.ModelSavePath = "/models"
	job.Spec.ModelSaveVolumeSize = 10

	pvc, err := c.GetCKPTPVC(ctx, job)
	if pvc != nil {
		t.Fatal("GetCKPTPVC returned the pvc on the create path")
	}
	if err == nil {
		t.Fatal("GetCKPTPVC returned nil error after creating the claim")
	}
	ckpt := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name + "-ckpt"}, ckpt); err != nil {
		t.Fatal(err)
	}
	if len(ckpt.OwnerReferences) != 1 {
		t.Fatalf("ckpt owner refs = %d", len(ckpt.OwnerReferences))
	}
	ref := ckpt.OwnerReferences[0]
	if ref.Kind != "CPodJob" || ref.Name != job.Name {
		t.Fatalf("owner ref = %+v", ref)
	}
	if ref.Controller == nil || !*ref.Controller || ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Fatalf("owner ref is not controlling: %+v", ref)
	}

	modelPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.GetModelSavePVCName(job),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				c.generateOwnerRefCPodJob(ctx, job),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	if err := c.Create(ctx, modelPVC); err != nil {
		t.Fatal(err)
	}
	if err := c.releaseSavedModel(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: modelPVC.Name}, modelPVC); err != nil {
		t.Fatal(err)
	}
	if len(modelPVC.OwnerReferences) != 0 {
		t.Fatalf("model-save owner refs left in place: %+v", modelPVC.OwnerReferences)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name + "-ckpt"}, ckpt); err != nil {
		t.Fatal(err)
	}
	if len(ckpt.OwnerReferences) != 1 || ckpt.OwnerReferences[0].Controller == nil || !*ckpt.OwnerReferences[0].Controller {
		t.Fatal("checkpoint controlling owner ref was removed")
	}

	// Empty model-save path returns before any owner edit.
	job.Spec.ModelSavePath = ""
	job.Spec.ModelSaveVolumeSize = 0
	if err := c.releaseSavedModel(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: job.Name + "-ckpt"}, ckpt); err != nil {
		t.Fatal(err)
	}
	if ckpt.OwnerReferences[0].Controller == nil || !*ckpt.OwnerReferences[0].Controller {
		t.Fatal("empty model-save path stripped the checkpoint owner ref")
	}
}

// TestP30_TwoRDMALabels shows needAllocateRDMADevice counts
// feature.node.kubernetes.io/rdma.available, while e2e/a_ib_test.go counts
// feature.node.kubernetes.io/rdma.capable. Two nodes with only the e2e label
// do not allocate an RDMA device.
func TestP30_TwoRDMALabels(t *testing.T) {
	ctx := context.Background()
	capable := func(n string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   n,
			Labels: map[string]string{"feature.node.kubernetes.io/rdma.capable": "true"},
		}}
	}
	available := func(n string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   n,
			Labels: map[string]string{"feature.node.kubernetes.io/rdma.available": "true"},
		}}
	}
	job := cpodJob("rdma", v1beta1.JobTypePytorch, 1, 2)
	c := newReconciler(t, capable("n0"), capable("n1"))
	if c.needAllocateRDMADevice(ctx, job) {
		t.Fatal("rdma.capable nodes were treated as rdma.available")
	}
	c = newReconciler(t, available("n0"), available("n1"))
	if !c.needAllocateRDMADevice(ctx, job) {
		t.Fatal("two rdma.available nodes did not allocate RDMA")
	}
	c = newReconciler(t, available("only-one"))
	if c.needAllocateRDMADevice(ctx, job) {
		t.Fatal("a single rdma.available node allocated RDMA")
	}

	src := readUp(t, "e2e/a_ib_test.go")
	if !strings.Contains(src, "feature.node.kubernetes.io/rdma.capable") {
		t.Fatal("e2e/a_ib_test.go no longer mentions rdma.capable")
	}
	ctrl := readUp(t, "cpodoperator/internal/controller/cpodjob_controller.go")
	if !strings.Contains(ctrl, "feature.node.kubernetes.io/rdma.available") {
		t.Fatal("controller no longer mentions rdma.available")
	}
}

func readUp(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("file %s not found", rel)
		}
		dir = parent
	}
}
