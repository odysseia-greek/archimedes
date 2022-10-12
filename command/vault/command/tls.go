package command

import (
	"encoding/base64"
	"fmt"
	"github.com/kpango/glg"
	"github.com/odysseia-greek/archimedes/command"
	"github.com/odysseia-greek/archimedes/util"
	"github.com/odysseia-greek/plato/kubernetes"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"path/filepath"
)

const defaultServiceName = "vault"

func TLS() *cobra.Command {
	var (
		namespace string
		service   string
		kubePath  string
	)
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "create tls secrets",
		Long:  `adds tls support for helm in vault`,
		Run: func(cmd *cobra.Command, args []string) {
			if namespace == "" {
				glg.Debugf("defaulting to %s", defaultNamespace)
				namespace = defaultNamespace
			}

			if service == "" {
				glg.Debugf("defaulting to %s", defaultServiceName)
				service = defaultServiceName
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

			EnableTlS(namespace, service, kubeManager)
		},
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace defaults to odysseia")
	cmd.PersistentFlags().StringVarP(&service, "service", "s", "", "kubernetes namespace defaults to odysseia")
	cmd.PersistentFlags().StringVarP(&kubePath, "kubepath", "k", "", "kubeconfig filepath defaults to ~/.kube/config")

	return cmd
}

func EnableTlS(namespace string, service string, kube kubernetes.KubeClient) {
	glg.Debug("setting up TLS for vault")

	if service == "" {
		service = defaultServiceName
	}
	secretName := "vault-server-tls"
	tmpDir := "/tmp"
	csrName := "vault-csr"

	commandKey := fmt.Sprintf("openssl genrsa -out %s/vault.key 2048", tmpDir)
	err := util.ExecCommand(commandKey, tmpDir)
	if err != nil {
		glg.Error(err)
	}

	keyFromFile, err := os.ReadFile(fmt.Sprintf("%s/vault.key", tmpDir))
	if err != nil {
		glg.Error(err)
	}

	altNames := fmt.Sprintf("DNS.1 = %s", service)
	altNames += fmt.Sprintf("\nDNS.2 = %s.%s", service, namespace)
	altNames += fmt.Sprintf("\nDNS.3 = %s.%s.svc", service, namespace)
	altNames += fmt.Sprintf("\nDNS.4 = %s.%s.svc.cluster.local", service, namespace)

	csrConf := fmt.Sprintf(`[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
%s
IP.1 = 127.0.0.1
`, altNames)
	outputFileCsr := fmt.Sprintf("%s/csr.conf", tmpDir)

	util.WriteFile([]byte(csrConf), outputFileCsr)

	commandCsr := fmt.Sprintf(`openssl req -new -key %s/vault.key \
    -subj "/O=system:nodes/CN=system:node:%s.%s.svc" \
    -out %s/server.csr \
    -config %s/csr.conf`, tmpDir, service, namespace, tmpDir, tmpDir)
	err = util.ExecCommand(commandCsr, tmpDir)
	if err != nil {
		glg.Error(err)
	}

	serverCsr, err := os.ReadFile(fmt.Sprintf("%s/server.csr", tmpDir))
	if err != nil {
		glg.Error(err)
	}

	encodedCsr := base64.StdEncoding.EncodeToString(serverCsr)

	csrYaml := fmt.Sprintf(`apiVersion: certificates.k8s.io/v1
kind: CertificateSigningRequest
metadata:
  name: %s
spec:
  groups:
  - system:authenticated
  request: %s
  signerName: kubernetes.io/kubelet-serving
  usages:
  - digital signature
  - key encipherment
  - server auth
`, csrName, encodedCsr)

	outputFileYaml := fmt.Sprintf("%s/csr.yaml", tmpDir)

	util.WriteFile([]byte(csrYaml), outputFileYaml)

	kubeCommand := fmt.Sprintf("kubectl create -f %s/csr.yaml", tmpDir)
	err = util.ExecCommand(kubeCommand, tmpDir)
	if err != nil {
		glg.Error(err)
	}

	kubeCommand = fmt.Sprintf("kubectl certificate approve %s", csrName)
	err = util.ExecCommand(kubeCommand, tmpDir)
	if err != nil {
		glg.Error(err)
	}

	ca, err := kube.Cluster().GetHostCaCert()
	if err != nil {
		glg.Error(err)
	}

	crtCommand := fmt.Sprintf("kubectl get csr %s -o jsonpath='{.status.certificate}'", csrName)
	cert, err := util.ExecCommandWithReturn(crtCommand, tmpDir)
	if err != nil {
		glg.Error(err)
	}

	decodedCert, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		glg.Error(err)
	}

	data := make(map[string][]byte)
	data["vault.ca"] = ca
	data["vault.crt"] = decodedCert
	data["vault.key"] = keyFromFile

	err = kube.Configuration().CreateSecret(namespace, secretName, data)
	if err != nil {
		glg.Error(err)
	}

	glg.Debug("finished setting up TLS for vault")
}
