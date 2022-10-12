package command

import (
	"fmt"
	"github.com/kpango/glg"
	"github.com/odysseia-greek/archimedes/command"
	"github.com/odysseia-greek/plato/kubernetes"
	"github.com/odysseia-greek/plato/vault"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

const defaultNamespace = "odysseia"
const defaultKubeConfig = "/.kube/config"

func Unseal() *cobra.Command {
	var (
		key       string
		namespace string
		kubePath  string
	)
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Unseal your vault",
		Long: `Allows you unseal the vault, it takes
- Key`,
		Run: func(cmd *cobra.Command, args []string) {

			if namespace == "" {
				glg.Debugf("defaulting to %s", defaultNamespace)
				namespace = defaultNamespace
			}

			if kubePath == "" {
				glg.Debugf("defaulting to %s", command.DefaultKubeConfig)
				homeDir, err := os.UserHomeDir()
				if err != nil {
					glg.Error(err)
				}

				kubePath = filepath.Join(homeDir, command.DefaultKubeConfig)
			}

			cfg, err := ioutil.ReadFile(kubePath)
			if err != nil {
				glg.Error("error getting kubeconfig")
			}

			kubeManager, err := kubernetes.NewKubeClient(cfg, namespace)
			if err != nil {
				glg.Fatal("error creating kubeclient")
			}

			glg.Info("is it secret? Is it safe? Well no longer!")
			glg.Debug("unsealing kube vault")
			UnsealVault(key, namespace, kubeManager)
		},
	}

	cmd.PersistentFlags().StringVarP(&key, "key", "k", "", "unseal key, if not set cluster-keys will be used")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace defaults to odysseia")
	cmd.PersistentFlags().StringVarP(&kubePath, "kubepath", "k", "", "kubeconfig filepath defaults to ~/.kube/config")

	return cmd
}

func UnsealVault(key, namespace string, kube kubernetes.KubeClient) {
	if key == "" {
		glg.Info("key was not given, trying to get key from cluster-keys.json")
		clusterKeys, err := vault.GetClusterKeys()
		if err != nil {
			glg.Fatal("could not get cluster keys")
		}
		key = clusterKeys.VaultUnsealKey
		glg.Info("key found")
	}

	vaultSelector := "app.kubernetes.io/name=vault"
	var podName string

	pods, err := kube.Workload().GetPodsBySelector(namespace, vaultSelector)
	if err != nil {
		glg.Error(err)
	}
	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, "vault") {
			if pod.Status.Phase == "Running" {
				glg.Debugf(fmt.Sprintf("%s is running in release %s", pods.Items[0].Name, namespace))
				podName = pod.Name
				break
			}
		}
	}

	command := []string{"vault", "operator", "unseal", key}

	vaultUnsealed, err := kube.Workload().ExecNamedPod(namespace, podName, command)
	if err != nil {
		glg.Error(err)
	}

	glg.Info(vaultUnsealed)
}
