package openshift

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/openshift/origin/pkg/cmd/util/clientcmd"
	kapi "k8s.io/kubernetes/pkg/api"
	apierrors "k8s.io/kubernetes/pkg/api/errors"
	kclient "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/serviceaccount"

	"github.com/openshift/origin/pkg/bootstrap/docker/errors"
	"github.com/openshift/origin/pkg/cmd/admin/registry"
	"github.com/openshift/origin/pkg/cmd/admin/router"
	"github.com/openshift/origin/pkg/cmd/server/admin"
	"github.com/openshift/origin/pkg/cmd/util/variable"
)

const (
	svcDockerRegistry = "docker-registry"
	svcRouter         = "router"
	masterConfigDir   = "/var/lib/origin/openshift.local.config/master"
)

func imageFormat(tag string) string {
	return fmt.Sprintf("openshift/origin-${component}:%s", tag)
}

// InstallRegistry checks whether a registry is installed and installs one if not already installed
func (h *Helper) InstallRegistry(kubeClient kclient.Interface, f *clientcmd.Factory, configDir, tag string, out io.Writer) error {
	_, err := kubeClient.Services("default").Get(svcDockerRegistry)
	if err == nil {
		// If there's no error, the registry already exists
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.NewError("error retrieving docker registry service").WithCause(err)
	}
	cfg := &registry.RegistryConfig{
		Name:           "registry",
		Type:           "docker-registry",
		ImageTemplate:  variable.NewDefaultImageTemplate(),
		Ports:          "5000",
		Replicas:       1,
		Labels:         "docker-registry=default",
		Volume:         "/registry",
		ServiceAccount: "registry",
	}
	cfg.ImageTemplate.Format = imageFormat(tag)
	cmd := registry.NewCmdRegistry(f, "", "registry", out)
	output := &bytes.Buffer{}
	err = registry.RunCmdRegistry(f, cmd, output, cfg, []string{})
	glog.V(4).Infof("Registry command output:\n%s", output.String())
	return err
}

// InstallRouter installs a default router on the OpenShift server
func (h *Helper) InstallRouter(kubeClient kclient.Interface, f *clientcmd.Factory, configDir, tag, hostIP string, out io.Writer) error {
	_, err := kubeClient.Services("default").Get("router")
	if err == nil {
		// Router service already exists, nothing to do
		return nil
	}

	masterDir := filepath.Join(configDir, "master")

	// Create service account for router
	routerSA := &kapi.ServiceAccount{}
	routerSA.Name = "router"
	_, err = kubeClient.ServiceAccounts("default").Create(routerSA)
	if err != nil {
		return errors.NewError("cannot create router service account").WithCause(err)
	}

	// Add router SA to privileged SCC
	privilegedSCC, err := kubeClient.SecurityContextConstraints().Get("privileged")
	if err != nil {
		return errors.NewError("cannot retrieve privileged SCC").WithCause(err)
	}
	privilegedSCC.Users = append(privilegedSCC.Users, serviceaccount.MakeUsername("default", "router"))
	_, err = kubeClient.SecurityContextConstraints().Update(privilegedSCC)
	if err != nil {
		return errors.NewError("cannot update privileged SCC").WithCause(err)
	}

	// Create router cert
	cmdOutput := &bytes.Buffer{}
	createCertOptions := &admin.CreateServerCertOptions{
		SignerCertOptions: &admin.SignerCertOptions{
			CertFile:   filepath.Join(masterDir, "ca.crt"),
			KeyFile:    filepath.Join(masterDir, "ca.key"),
			SerialFile: filepath.Join(masterDir, "ca.serial.txt"),
		},
		Overwrite: true,
		Hostnames: []string{fmt.Sprintf("%s.xip.io", hostIP)},
		CertFile:  filepath.Join(masterDir, "router.crt"),
		KeyFile:   filepath.Join(masterDir, "router.key"),
		Output:    cmdOutput,
	}
	_, err = createCertOptions.CreateServerCert()
	if err != nil {
		return errors.NewError("cannot create router cert").WithCause(err)
	}

	err = catFiles(filepath.Join(masterDir, "router.pem"),
		filepath.Join(masterDir, "router.crt"),
		filepath.Join(masterDir, "router.key"),
		filepath.Join(masterDir, "ca.crt"))
	if err != nil {
		return err
	}

	cfg := &router.RouterConfig{
		Name:               "router",
		Type:               "haproxy-router",
		ImageTemplate:      variable.NewDefaultImageTemplate(),
		Ports:              "80:80,443:443",
		Replicas:           1,
		Labels:             "router=<name>",
		Credentials:        filepath.Join(masterDir, "admin.kubeconfig"),
		DefaultCertificate: filepath.Join(masterDir, "router.pem"),
		StatsPort:          1936,
		StatsUsername:      "admin",
		HostNetwork:        true,
		HostPorts:          true,
		ServiceAccount:     "router",
	}
	cfg.ImageTemplate.Format = imageFormat(tag)
	output := &bytes.Buffer{}
	cmd := router.NewCmdRouter(f, "", "router", out)
	cmd.SetOutput(output)
	err = router.RunCmdRouter(f, cmd, output, cfg, []string{})
	glog.V(4).Infof("Router command output:\n%s", output.String())
	return err
}

// catFiles concatenates multiple source files into a single destination file
func catFiles(dest string, src ...string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, f := range src {
		in, oerr := os.Open(f)
		if oerr != nil {
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
