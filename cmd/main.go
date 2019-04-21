package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/xiaopal/kube-leaderelect/pkg/appctx"
	"github.com/xiaopal/kube-leaderelect/pkg/kubeclient"
	"github.com/xiaopal/kube-leaderelect/pkg/leaderelect"
)

var (
	logger         *log.Logger
	handlerCommand []string
	bindAddr       string
	kubeClient     kubeclient.Client
	leaderHelper   leaderelect.Helper
	handlerActive  int32
)

func writeJSON(res http.ResponseWriter, statusCode int, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %v", err)
	}
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(statusCode)
	res.Write(body)
	return nil
}

func run() {
	app := appctx.Start()
	defer app.End()

	if bindAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(res http.ResponseWriter, req *http.Request) {
			active := atomic.LoadInt32(&handlerActive) == 1
			payload := map[string]interface{}{"active": active}
			if id := leaderHelper.Identity(); id != "" {
				payload["identity"], payload["leader"], payload["leader-identity"] = id, leaderHelper.IsLeader(), leaderHelper.GetLeader()
			}
			if active {
				writeJSON(res, http.StatusOK, payload)
				return
			}
			writeJSON(res, http.StatusServiceUnavailable, payload)
		})
		server, wg := &http.Server{Addr: bindAddr, Handler: mux}, app.WaitGroup()
		wg.Add(1)
		go func() {
			defer app.EndContext()
			if err := server.ListenAndServe(); err != nil {
				logger.Printf("server: %v", err)
			}
			wg.Done()
		}()
		defer func() {
			if err := server.Close(); err != nil {
				logger.Printf("close server: %v", err)
			}
		}()
	}
	leaderHelper.Run(app.Context(), runHandler)
}

func runHandler(ctx context.Context) {
	handler := exec.CommandContext(ctx, handlerCommand[0], handlerCommand[1:]...)
	handler.Stdin, handler.Stdout, handler.Stderr = os.Stdin, os.Stdout, os.Stderr
	atomic.StoreInt32(&handlerActive, 1)
	defer atomic.StoreInt32(&handlerActive, 0)
	if err := handler.Run(); err != nil {
		logger.Printf("failed to execute handler: %v", err)
	}
}

func main() {
	logger = log.New(os.Stderr, "[kube-leaderelect] ", log.Flags())
	cmd := &cobra.Command{
		Use: fmt.Sprintf("%s [flags] handlerCommand args...", os.Args[0]),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			handlerCommand = args
			if len(handlerCommand) == 0 {
				return fmt.Errorf("handlerCommand required")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			run()
		},
	}
	flags := cmd.Flags()
	flags.AddGoFlagSet(flag.CommandLine)
	kubeClient = kubeclient.NewClient(&kubeclient.ClientOpts{
		DisableAllNamespaces: true,
	})
	kubeClient.BindFlags(flags, "LEADER_ELECT_")
	leaderHelper = leaderelect.NewHelper(&leaderelect.HelperOpts{
		DefaultNamespaceFunc: kubeClient.DefaultNamespace,
		GetConfigFunc:        kubeClient.GetConfig,
	})
	leaderHelper.BindFlags(flags, "")
	flags.StringVar(&bindAddr, "bind-addr", "", "bind addr to serve '/healthz', eg `:8080`")

	if err := cmd.Execute(); err != nil {
		logger.Fatal(err)
	}
}
