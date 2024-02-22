package app

import (
	"sync"

	"github.com/postfinance/kubenurse/cmd/ubinurse-watcher/app/options"
	"github.com/postfinance/kubenurse/pkg/projectinfo"
	"github.com/postfinance/kubenurse/pkg/ubinurse-watcher/informers"
	"github.com/postfinance/kubenurse/pkg/ubinurse-watcher/server"
	"github.com/spf13/cobra"
)

func NewUbinurseWatcherCommand(stopCh <-chan struct{}) *cobra.Command {
	serverOptions := options.NewServerOptions()
	cmd := &cobra.Command{
		Use:   "Launch " + projectinfo.GetServerName(),
		Short: projectinfo.GetServerName() + " sends requests to " + projectinfo.GetUbinurseName(),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := serverOptions.Config()
			if err != nil {
				return err
			}
			if err := run(cfg, stopCh); err != nil {
				return err
			}
			return nil
		},
		Args: cobra.NoArgs,
	}

	serverOptions.AddFlags(cmd.Flags())
	serverOptions.AddFromEnv()
	return cmd
}

func run(cfg *options.ServerConfig, stopCh <-chan struct{}) error {
	var wg sync.WaitGroup
	informers.RegisterInformersForUbinurseWatcher(cfg.SharedInformerFactory)
	//cfg.SharedInformerFactory.Start(stopCh)
	server := server.NewServer(cfg.Client, cfg.SharedInformerFactory)
	cfg.SharedInformerFactory.Start(stopCh)
	if err := server.Run(stopCh); err != nil {
		return err
	}

	<-stopCh
	wg.Wait()
	return nil
}
