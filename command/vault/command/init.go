package command

import (
	"fmt"
	"github.com/kpango/glg"
	"github.com/odysseia-greek/archimedes/command"
	"github.com/odysseia-greek/archimedes/util"
	"github.com/odysseia-greek/plato/kubernetes"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func Init() *cobra.Command {
	var (
		namespace string
		kubePath  string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Inits your vault",
		Long: `Allows you to init the vault, it takes
- Namespace
- Filepath`,
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
			initVault(namespace, kubeManager)
		},
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace defaults to odysseia")
	cmd.PersistentFlags().StringVarP(&kubePath, "kubepath", "k", "", "kubeconfig filepath defaults to ~/.kube/config")

	return cmd
}

func initVault(namespace string, kube kubernetes.KubeClient) []byte {
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

	command := []string{"vault", "operator", "init", "-key-shares=1", "-key-threshold=1", "-format=json"}

	vaultInit, err := kube.Workload().ExecNamedPod(namespace, podName, command)
	if err != nil {
		glg.Error(err)
		return nil
	}

	_, callingFile, _, _ := runtime.Caller(0)
	callingDir := filepath.Dir(callingFile)
	dirParts := strings.Split(callingDir, string(os.PathSeparator))
	var odysseiaPath []string
	for i, part := range dirParts {
		if part == "odysseia" {
			odysseiaPath = dirParts[0 : i+1]
		}
	}
	l := "/"
	for _, path := range odysseiaPath {
		l = filepath.Join(l, path)
	}

	fileName := fmt.Sprintf("cluster-keys-%s.json", namespace)
	clusterKeys := filepath.Join(l, "solon", "vault_config", fileName)

	util.WriteFile([]byte(vaultInit), clusterKeys)

	glg.Debugf("wrote data: %s to dest: %s", vaultInit, clusterKeys)

	return []byte(vaultInit)
}
