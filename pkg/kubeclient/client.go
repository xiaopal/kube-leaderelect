package kubeclient

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/restmapper"

	dynamic "k8s.io/client-go/deprecated-dynamic"
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
	GetConfigOrDie() *rest.Config
	Namespace() string
	DefaultNamespace() string
	DynamicClientPool() dynamic.ClientPool
	APIResource(apiVersion, kind string) (*metav1.APIResource, *schema.GroupVersionKind, error)
	DynamicClient(apiVersion, kind string) (client dynamic.Interface, resource *metav1.APIResource, err error)
	ResourceClient(apiVersion, kind string) (client dynamic.ResourceInterface, resource *metav1.APIResource, namespace string, err error)
}

//NewClient func
func NewClient(opts *ClientOpts) Client {
	return &client{ClientOpts: *opts}
}

type client struct {
	ClientOpts
	clientConfigOnce sync.Once
	clientConfig     clientcmd.ClientConfig
	dynamicOnce      sync.Once
	restMapper       *restmapper.DeferredDiscoveryRESTMapper
	dynamicPool      dynamic.ClientPool
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

func (c *client) ClientConfig() clientcmd.ClientConfig {
	c.clientConfigOnce.Do(func() {
		c.clientConfig = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.KubeConfigPath},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: c.MasterURL}})
	})
	return c.clientConfig
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
	if ns, _, _ := c.ClientConfig().Namespace(); ns != "" {
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
	return c.ClientConfig().ClientConfig()
}

func (c *client) GetConfigOrDie() *rest.Config {
	config, err := c.GetConfig()
	if err != nil {
		panic(err)
	}
	return config
}

func (c *client) DynamicClientPool() dynamic.ClientPool {
	c.dynamicOnce.Do(func() {
		config := c.GetConfigOrDie()
		config.ContentConfig = dynamic.ContentConfig()
		restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cached.NewMemCacheClient(clientset.NewForConfigOrDie(config).Discovery()))
		restMapper.Reset()
		c.restMapper, c.dynamicPool = restMapper, dynamic.NewClientPool(config, restMapper, dynamic.LegacyAPIPathResolverFunc)
	})
	return c.dynamicPool
}

func (c *client) APIResource(apiVersion, kind string) (*metav1.APIResource, *schema.GroupVersionKind, error) {
	c.DynamicClientPool()
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse apiVersion: %v", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}
	mapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get the resource REST mapping for GroupVersionKind(%s): %v", gvk.String(), err)
	}
	return &metav1.APIResource{
		Name:       mapping.Resource.Resource,
		Namespaced: mapping.Scope == meta.RESTScopeNamespace,
		Kind:       gvk.Kind,
	}, &gvk, nil
}

func (c *client) DynamicClient(apiVersion, kind string) (dynamic.Interface, *metav1.APIResource, error) {
	resource, gvk, err := c.APIResource(apiVersion, kind)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get resource type: %v", err)
	}
	client, err := c.DynamicClientPool().ClientForGroupVersionKind(*gvk)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get client for GroupVersionKind(%s): %v", gvk.String(), err)
	}
	return client, resource, nil
}

func (c *client) ResourceClient(apiVersion, kind string) (dynamic.ResourceInterface, *metav1.APIResource, string, error) {
	client, resource, err := c.DynamicClient(apiVersion, kind)
	if err != nil {
		return nil, nil, "", err
	}
	namespace := c.Namespace()
	if !resource.Namespaced {
		namespace = metav1.NamespaceAll
	}
	return client.Resource(resource, namespace), resource, namespace, nil

}
