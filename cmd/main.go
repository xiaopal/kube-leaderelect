package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/xiaopal/kube-informer/pkg/appctx"
	"github.com/xiaopal/kube-informer/pkg/kubeclient"
	"github.com/xiaopal/kube-informer/pkg/leaderelect"
)

var (
	logger         *log.Logger
	handlerCommand []string
	kubeClient     kubeclient.Client
	leaderHelper   leaderelect.Helper
	handlerError   error
)

func run(cmd *cobra.Command, args []string) error {
	handlerCommand = args
	if len(handlerCommand) == 0 {
		return fmt.Errorf("handlerCommand required")
	}

	app := appctx.Start()
	defer app.End()

	leaderHelper.Run(app.Context(), runHandler)
	return handlerError
}

func runHandler(ctx context.Context) {
	handler := exec.CommandContext(ctx, handlerCommand[0], handlerCommand[1:]...)
	handler.Stdin, handler.Stdout, handler.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := handler.Run(); err != nil {
		handlerError = fmt.Errorf("failed to execute handler: %v", err)
	}
}

func main() {
	logger = log.New(os.Stderr, "[kube-leaderelect] ", log.Flags())
	cmd := &cobra.Command{
		Use:  fmt.Sprintf("%s [flags] handlerCommand args...", os.Args[0]),
		RunE: run,
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

	if err := cmd.Execute(); err != nil {
		logger.Fatal(err)
	}
}
