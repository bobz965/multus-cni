// Copyright (c) 2021 Multus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package cni

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/testutils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	netfake "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/fake"
	k8s "gopkg.in/k8snetworkplumbingwg/multus-cni.v3/pkg/k8sclient"
	testhelpers "gopkg.in/k8snetworkplumbingwg/multus-cni.v3/pkg/testing"
)

const suiteName = "Thick CNI architecture"

func TestMultusThickCNIArchitecture(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, suiteName)
}

type fakeExec struct{}

// ExecPlugin executes the plugin
func (fe *fakeExec) ExecPlugin(ctx context.Context, pluginPath string, stdinData []byte, environ []string) ([]byte, error) {
	return []byte("{}"), nil
}

// FindInPath finds in path
func (fe *fakeExec) FindInPath(plugin string, paths []string) (string, error) {
	return "", nil
}

// Decode decodes
func (fe *fakeExec) Decode(jsonBytes []byte) (version.PluginInfo, error) {
	return nil, nil
}

var _ = Describe(suiteName, func() {
	const thickCNISocketDirPath = "multus-cni-thick-arch-socket-path"

	var thickPluginRunDir string

	BeforeEach(func() {
		var err error
		thickPluginRunDir, err = ioutil.TempDir("", thickCNISocketDirPath)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(thickPluginRunDir)).To(Succeed())
	})

	Context("the directory does *not* exist", func() {
		It("", func() {
			Expect(FilesystemPreRequirements(thickPluginRunDir)).To(Succeed())
		})
	})

	Context("the directory exists beforehand with the correct permissions", func() {
		BeforeEach(func() {
			Expect(os.MkdirAll(thickPluginRunDir, 0700)).To(Succeed())
		})

		It("verifies the filesystem requirements of the socket dir", func() {
			Expect(FilesystemPreRequirements(thickPluginRunDir)).To(Succeed())
		})
	})

	Context("CNI operations started from the shim", func() {
		const (
			containerID = "123456789"
			ifaceName   = "eth0"
			podName     = "my-little-pod"
		)

		var (
			cniServer *Server
			K8sClient *k8s.ClientInfo
			netns     ns.NetNS
		)

		BeforeEach(func() {
			var err error
			K8sClient = fakeK8sClient()

			Expect(FilesystemPreRequirements(thickPluginRunDir)).To(Succeed())
			cniServer, err = startCNIServer(thickPluginRunDir, K8sClient)
			Expect(err).NotTo(HaveOccurred())

			netns, err = testutils.NewNS()
			Expect(err).NotTo(HaveOccurred())

			// the namespace and podUID parameters below are hard-coded in the generation function
			Expect(prepareCNIEnv(netns.Path(), "test", podName, "testUID")).To(Succeed())
			Expect(createFakePod(K8sClient, podName)).To(Succeed())
		})

		AfterEach(func() {
			Expect(cniServer.Close()).To(Succeed())
			Expect(teardownCNIEnv()).To(Succeed())
			Expect(K8sClient.Client.CoreV1().Pods("test").Delete(
				context.TODO(), podName, metav1.DeleteOptions{}))
		})

		It("ADD works successfully", func() {
			Expect(CmdAdd(cniCmdArgs(containerID, netns.Path(), ifaceName, referenceConfig(thickPluginRunDir)))).To(Succeed())
		})

		It("DEL works successfully", func() {
			Expect(CmdDel(cniCmdArgs(containerID, netns.Path(), ifaceName, referenceConfig(thickPluginRunDir)))).To(Succeed())
		})

		It("CHECK works successfully", func() {
			Expect(CmdCheck(cniCmdArgs(containerID, netns.Path(), ifaceName, referenceConfig(thickPluginRunDir)))).To(Succeed())
		})
	})
})

func fakeK8sClient() *k8s.ClientInfo {
	const magicNumber = 10
	return &k8s.ClientInfo{
		Client:        fake.NewSimpleClientset(),
		NetClient:     netfake.NewSimpleClientset().K8sCniCncfIoV1(),
		EventRecorder: record.NewFakeRecorder(magicNumber),
	}
}

func cniCmdArgs(containerID string, netnsPath string, ifName string, stdinData string) *skel.CmdArgs {
	return &skel.CmdArgs{
		ContainerID: containerID,
		Netns:       netnsPath,
		IfName:      ifName,
		StdinData:   []byte(stdinData)}
}

func prepareCNIEnv(netnsPath string, namespaceName string, podName string, podUID string) error {
	cniArgs := fmt.Sprintf("K8S_POD_NAMESPACE=%s;K8S_POD_NAME=%s;K8S_POD_INFRA_CONTAINER_ID=;K8S_POD_UID=%s", namespaceName, podName, podUID)
	if err := os.Setenv("CNI_COMMAND", "ADD"); err != nil {
		return err
	}
	if err := os.Setenv("CNI_CONTAINERID", "123456789"); err != nil {
		return err
	}
	if err := os.Setenv("CNI_NETNS", netnsPath); err != nil {
		return err
	}
	if err := os.Setenv("CNI_ARGS", cniArgs); err != nil {
		return err
	}
	return nil
}

func teardownCNIEnv() error {
	if err := os.Unsetenv("CNI_COMMAND"); err != nil {
		return err
	}
	if err := os.Unsetenv("CNI_CONTAINERID"); err != nil {
		return err
	}
	if err := os.Unsetenv("CNI_NETNS"); err != nil {
		return err
	}
	if err := os.Unsetenv("CNI_ARGS"); err != nil {
		return err
	}
	return nil
}

func createFakePod(k8sClient *k8s.ClientInfo, podName string) error {
	var err error
	fakePod := testhelpers.NewFakePod(podName, "", "")
	_, err = k8sClient.Client.CoreV1().Pods(fakePod.GetNamespace()).Create(
		context.TODO(), fakePod, metav1.CreateOptions{})
	return err
}

func startCNIServer(runDir string, k8sClient *k8s.ClientInfo) (*Server, error) {
	const period = 0

	cniServer, err := newCNIServer(runDir, k8sClient, &fakeExec{})
	if err != nil {
		return nil, err
	}

	l, err := ServerListener(SocketPath(runDir))
	if err != nil {
		return nil, fmt.Errorf("failed to start the CNI server using socket %s. Reason: %+v", SocketPath(runDir), err)
	}

	cniServer.SetKeepAlivesEnabled(false)
	go utilwait.Forever(func() {
		if err := cniServer.Serve(l); err != nil {
			utilruntime.HandleError(fmt.Errorf("CNI server Serve() failed: %v", err))
		}
	}, period)
	return cniServer, nil
}

func referenceConfig(thickPluginSocketDir string) string {
	const referenceConfigTemplate = `{
        "name": "node-cni-network",
        "type": "multus",
        "socketDir": "%s",
        "defaultnetworkfile": "/tmp/foo.multus.conf",
        "defaultnetworkwaitseconds": 3,
        "delegates": [{
            "name": "weave1",
            "cniVersion": "0.3.1",
            "type": "weave-net"
        }]}`
	return fmt.Sprintf(referenceConfigTemplate, thickPluginSocketDir)
}
