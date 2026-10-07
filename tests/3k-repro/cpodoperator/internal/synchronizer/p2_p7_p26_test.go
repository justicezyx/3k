package synchronizer

import (
	"context"
	"encoding/json"
	"testing"

	cpodv1 "github.com/NascentCore/cpodoperator/api/v1"
	"github.com/NascentCore/cpodoperator/api/v1beta1"
	"github.com/NascentCore/cpodoperator/pkg/provider/sxwl"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func operatorScheme(t *testing.T) *runtime.Scheme {
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
	return s
}

func gpuNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				v1beta1.K8S_LABEL_NV_GPU_PRODUCT: "A100",
				v1beta1.K8S_LABEL_NV_GPU_PRESENT: "true",
				"nvidia.com/gpu.count":           "8",
				"nvidia.com/gpu.memory":          "40960",
				"nvidia.com/cuda.driver.major":   "12",
				"nvidia.com/cuda.driver.minor":   "2",
				"nvidia.com/cuda.driver.rev":     "0",
				"nvidia.com/cuda.runtime.major":  "12",
				"nvidia.com/cuda.runtime.minor":  "2",
				"status":                         "Ready",
				"kubernetes.io/arch":             "amd64",
				"drain":                          "true",
			},
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{OSImage: "ubuntu"},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("64Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("64Gi"),
			},
		},
	}
}

func gpuPod(name, nodeName string, gpus int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"nvidia.com/gpu":      resource.MustParse(itoa(gpus)),
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			}},
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestP2_ObserverCountsOnlyBoundPodGPUs(t *testing.T) {
	node := gpuNode("node-0")

	t.Run("unbound request does not reduce allocatable", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(operatorScheme(t)).WithObjects(node, gpuPod("pending", "", 8)).Build()
		co := &CPodObserver{kubeClient: cl, logger: logr.Discard(), cpodId: "island-a"}
		info, err := co.getResourceInfo(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Nodes[0].GPUAllocatable != 8 || info.Nodes[0].GPUUsed != 0 {
			t.Fatalf("unbound pod changed allocatable=%d used=%d", info.Nodes[0].GPUAllocatable, info.Nodes[0].GPUUsed)
		}
	})

	t.Run("bound request is subtracted and no reservation is applied", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(operatorScheme(t)).WithObjects(node.DeepCopy(), gpuPod("bound", "node-0", 8)).Build()
		co := &CPodObserver{kubeClient: cl, logger: logr.Discard(), cpodId: "island-a"}
		info, err := co.getResourceInfo(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		n := info.Nodes[0]
		if n.GPUAllocatable != n.GPUTotal-n.GPUUsed {
			t.Fatalf("allocatable %d != total %d - used %d", n.GPUAllocatable, n.GPUTotal, n.GPUUsed)
		}
		if n.GPUAllocatable != 0 {
			t.Fatalf("bound 8-GPU pod left allocatable %d", n.GPUAllocatable)
		}
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		if containsAny(body, []string{`"reserved"`, `"reserved_gpu"`, `"gpu_observed_free"`}) {
			t.Fatalf("heartbeat resource JSON carries a reservation field: %s", body)
		}
	})
}

func TestP7_ZeroGPUPerReplica(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(operatorScheme(t)).Build()
	s := &SyncJob{kubeClient: cl, logger: logr.Discard()}
	jobs := []sxwl.PortalTrainningJob{
		{UserID: "user-1", JobName: "ft-16", JobType: "Finetune", GpuNumber: 16, GpuType: "A100"},
		{UserID: "user-1", JobName: "pt-7", JobType: "PyTorch", GpuNumber: 7, GpuType: "A100"},
		{UserID: "user-1", JobName: "pt-16", JobType: "PyTorch", GpuNumber: 16, GpuType: "A100"},
		{UserID: "user-1", JobName: "mpi-8", JobType: "MPI", GpuNumber: 8, GpuType: "A100"},
	}
	s.processTrainningJobs(context.Background(), []sxwl.UserID{"user-1"}, jobs)

	var list v1beta1.CPodJobList
	if err := cl.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]v1beta1.CPodJob{}
	for _, item := range list.Items {
		got[item.Name] = item
	}
	if _, ok := got["ft-16"]; ok {
		t.Fatal("Finetune was turned into a CPodJob")
	}
	if got["pt-7"].Spec.GPURequiredPerReplica != 7 || got["pt-7"].Spec.WorkerReplicas != 1 {
		t.Fatalf("7-GPU job spec = %+v", got["pt-7"].Spec)
	}
	if got["pt-16"].Spec.GPURequiredPerReplica != 0 || got["pt-16"].Spec.WorkerReplicas != 2 {
		t.Fatalf("16-GPU pytorch spec gpu/replica=%d replicas=%d", got["pt-16"].Spec.GPURequiredPerReplica, got["pt-16"].Spec.WorkerReplicas)
	}
	if got["mpi-8"].Spec.GPURequiredPerReplica != 0 || got["mpi-8"].Spec.WorkerReplicas != 1 {
		t.Fatalf("8-GPU mpi spec gpu/replica=%d replicas=%d", got["mpi-8"].Spec.GPURequiredPerReplica, got["mpi-8"].Spec.WorkerReplicas)
	}
}

func TestP26_HeartbeatPayloadFieldsMissing(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(operatorScheme(t)).WithObjects(gpuNode("node-0")).Build()
	co := &CPodObserver{kubeClient: cl, logger: logr.Discard(), cpodId: "island-a"}
	info, err := co.getResourceInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Nodes[0].Status != "Ready" {
		t.Fatalf("status = %q, want the node label status", info.Nodes[0].Status)
	}
	raw, err := json.Marshal(sxwl.HeartBeatPayload{ResourceInfo: info})
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, key := range []string{
		"queue_depth", "queueDepth", "rdma_domain", "rdmaDomain",
		"inventory_epoch", "inventoryEpoch", "drain", "health",
	} {
		if containsJSONKey(body, key) {
			t.Fatalf("heartbeat JSON has %q: %s", key, body)
		}
	}
	if info.Nodes[0].NetworkInfo.Type != "" {
		t.Fatalf("network type set to %q", info.Nodes[0].NetworkInfo.Type)
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func containsJSONKey(body, key string) bool {
	needle := `"` + key + `"`
	return containsAny(body, []string{needle})
}
