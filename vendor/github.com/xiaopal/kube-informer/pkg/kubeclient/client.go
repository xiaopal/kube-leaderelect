package kubeclient

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//ClientOpts options
type ClientOpts struct {
	KubeConfigPath       string
	MasterURL            string
	Namespace            string
	AllNamespaces        bool
	DisableAllNamespaces bool
}

//Client interface
type Client interface {
	BindFlags(flags *pflag.FlagSet, envPrefix string)
	GetConfig() (*rest.Config, error)
	Namespace() string
	DefaultNamespace() string
}

//NewClient func
func NewClient(opts *ClientOpts) Client {
	return &client{ClientOpts: *opts}
}

type client struct {
	ClientOpts
	once         sync.Once
	clientConfig clientcmd.ClientConfig
}

//BindFlags func
func (c *client) BindFlags(flags *pflag.FlagSet, envPrefix string) {
	if c.KubeConfigPath == "" {
		defaultPath := os.Getenv(envPrefix + "KUBECONFIG")
		if defaultPath == "" {
			if defaultPath = os.Getenv("KUBECONFIG"); defaultPath == "" {
				if home := os.Getenv("HOME"); home != "" {
					defaultPath = filepath.Join(home, ".kube", "config")
				}
			}
		}
		if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
			c.KubeConfigPath = defaultPath
		}
	}
	flags.StringVar(&c.KubeConfigPath, "kubeconfig", c.KubeConfigPath, "path to the kubeconfig file")
	flags.StringVarP(&c.MasterURL, "server", "s", os.Getenv(envPrefix+"SERVER"), "URL of the Kubernetes API server")
	flags.StringVarP(&c.ClientOpts.Namespace, "namespace", "n", os.Getenv(envPrefix+"NAMESPACE"), "namespace")
	if !c.DisableAllNamespaces {
		flags.BoolVar(&c.AllNamespaces, "all-namespaces", os.Getenv(envPrefix+"ALL_NAMESPACES") != "", "all namespaces")
	}
}

func (c *client) ensure() {
	c.once.Do(func() {
		c.clientConfig = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.KubeConfigPath},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: c.MasterURL}})
	})
}

func (c *client) Namespace() string {
	if c.AllNamespaces {
		return metav1.NamespaceAll
	}
	if !c.AllNamespaces && c.ClientOpts.Namespace == "" {
		return c.DefaultNamespace()
	}
	return c.ClientOpts.Namespace
}

func (c *client) DefaultNamespace() string {
	c.ensure()
	if ns, _, _ := c.clientConfig.Namespace(); ns != "" {
		return ns
	}
	if data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			return ns
		}
	}
	return "default"
}

func (c *client) GetConfig() (*rest.Config, error) {
	c.ensure()
	return c.clientConfig.ClientConfig()
}
