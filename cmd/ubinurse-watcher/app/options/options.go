package options

import (
	"os"
	"strconv"
	"time"

	kubeutil "github.com/postfinance/kubenurse/pkg/kubernetes"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type ServerOptions struct {
	EnableIngress bool
}

type ServerConfig struct {
	EnableIngress         bool
	Client                kubernetes.Interface
	SharedInformerFactory informers.SharedInformerFactory
}

func NewServerOptions() *ServerOptions {
	options := &ServerOptions{
		EnableIngress: false,
	}
	return options
}

func (o *ServerOptions) Config() (*ServerConfig, error) {
	var err error
	cfg := &ServerConfig{
		EnableIngress: o.EnableIngress,
	}
	cfg.Client, err = kubeutil.CreateClientSet("")
	if err != nil {
		return nil, err
	}
	cfg.SharedInformerFactory = informers.NewSharedInformerFactory(cfg.Client, 24*time.Hour)

	klog.Info("ubinurse-watcher server config: %#+v", cfg)
	return cfg, nil
}

func (o *ServerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.EnableIngress, "enable-ingress", o.EnableIngress, "If enable ingress check")
}

func (o *ServerOptions) AddFromEnv() {
	enableIngress := os.Getenv("ENABLE_INGRESS")
	if enableIngress != "" {
		v, err := strconv.ParseBool(enableIngress)
		if err == nil {
			o.EnableIngress = v
		}
	}
}
