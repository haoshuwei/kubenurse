package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/postfinance/kubenurse/cmd/ubinurse-watcher/app"
	"github.com/postfinance/kubenurse/pkg/projectinfo"
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM,
		syscall.SIGQUIT, syscall.SIGILL, syscall.SIGTRAP, syscall.SIGABRT)
	stop := make(chan struct{})
	go func() {
		<-s
		close(stop)
	}()

	cmd := app.NewUbinurseWatcherCommand(stop)
	cmd.Flags().AddGoFlagSet(flag.CommandLine)
	if err := cmd.Execute(); err != nil {
		klog.Fatalf("%s failed: %s", projectinfo.GetServerName(), err)
	}
}
