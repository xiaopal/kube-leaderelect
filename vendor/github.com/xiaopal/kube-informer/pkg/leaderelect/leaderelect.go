package leaderelect

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
)

//HelperOpts options
type HelperOpts struct {
	Enabled              bool
	LeaseDuration        time.Duration
	RenewDeadline        time.Duration
	RetryPeriod          time.Duration
	ResourceLock         string
	LockObjectName       string
	LockObjectNamespace  string
	GetConfigFunc        func() (*rest.Config, error)
	DefaultNamespaceFunc func() string
}

//Helper interface
type Helper interface {
	BindFlags(flags *pflag.FlagSet, envPrefix string)
	Run(ctx context.Context, handler func(context.Context))
}

//NewHelper func
func NewHelper(opts *HelperOpts) Helper {
	return &helper{*opts}
}

type helper struct {
	HelperOpts
}

func envToDuration(key string, d time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if ret, err := time.ParseDuration(v); err == nil {
			return ret
		}
	}
	return d
}

//BindFlags func
func (h *helper) BindFlags(flags *pflag.FlagSet, envPrefix string) {
	if h.LockObjectName == "" {
		h.LockObjectName = os.Getenv(envPrefix + "LEADER_ELECT")
	}
	if h.LeaseDuration <= 0 {
		h.LeaseDuration = envToDuration(envPrefix+"LEADER_ELECT_LEASE", 15*time.Second)
	}
	if h.RenewDeadline <= 0 {
		h.RenewDeadline = envToDuration(envPrefix+"LEADER_ELECT_RENEW", 10*time.Second)
	}
	if h.RetryPeriod <= 0 {
		h.RetryPeriod = envToDuration(envPrefix+"LEADER_ELECT_RETRY", 2*time.Second)
	}
	flags.StringVar(&h.LockObjectName, "leader-elect", os.Getenv(envPrefix+"LEADER_ELECT"), "leader election: [endpoints|configmaps/]<object name>")
	flags.StringVar(&h.LockObjectNamespace, "leader-elect-namespace", os.Getenv(envPrefix+"LEADER_ELECT_NAMESPACE"), "leader election: object namespace")
	flags.DurationVar(&h.LeaseDuration, "leader-elect-lease", h.LeaseDuration, "leader election: lease duration")
	flags.DurationVar(&h.RenewDeadline, "leader-elect-renew", h.RenewDeadline, "leader election: renew deadline")
	flags.DurationVar(&h.RetryPeriod, "leader-elect-retry", h.RetryPeriod, "leader election: retry period")
}

func (h *helper) ensure() {
	h.LockObjectName = strings.TrimSpace(h.LockObjectName)
	if h.Enabled = (h.LockObjectName != ""); h.Enabled {
		if h.ResourceLock == "" {
			h.ResourceLock = resourcelock.EndpointsResourceLock
			if index := strings.Index(h.LockObjectName, "/"); index > 0 {
				h.ResourceLock, h.LockObjectName = h.LockObjectName[:index], h.LockObjectName[index+1:]
			}
		}
		if h.LockObjectNamespace == "" {
			if h.DefaultNamespaceFunc == nil {
				h.DefaultNamespaceFunc = func() string { return "default" }
			}
			h.LockObjectNamespace = h.DefaultNamespaceFunc()
		}
	}
}

//Run func
func (h *helper) Run(ctx context.Context, handler func(context.Context)) {
	h.ensure()
	if !h.Enabled {
		handler(ctx)
		return
	}
	logger := log.New(os.Stderr, "[leader-election] ", log.Flags())
	ctx, leave := context.WithCancel(ctx)
	config, err := h.GetConfigFunc()
	if err != nil {
		logger.Fatalf("failed to get Config: %v", err)
	}
	lec, err := corev1.NewForConfig(config)
	if err != nil {
		logger.Fatalf("failed to get CoreV1 Client: %v", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		logger.Fatalf("failed to get hostname: %v", err)
	}
	id := hostname + "_" + string(uuid.NewUUID())
	broadcaster := record.NewBroadcaster()
	lock, err := resourcelock.New(
		h.ResourceLock,
		h.LockObjectNamespace,
		h.LockObjectName,
		lec,
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: broadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "kube-informer"}),
		},
	)
	if err != nil {
		logger.Fatalf("failed to init resourcelock: %v", err)
	}

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	le, err := NewLeaderElector(LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: h.LeaseDuration,
		RenewDeadline: h.RenewDeadline,
		RetryPeriod:   h.RetryPeriod,
		Callbacks: LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Printf("leader started: %s", id)
				wg.Add(1)
				defer wg.Done()
				handler(ctx)
				leave()
			},
			OnStoppedLeading: func() {
				logger.Printf("leaving")
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					logger.Printf("entering leader: %s", identity)
					return
				}
				logger.Printf("following leader: %s", identity)
			},
		},
	})
	if err != nil {
		logger.Fatalf("failed to init leaderelector: %v", err)
	}
	le.Run(ctx)
}
