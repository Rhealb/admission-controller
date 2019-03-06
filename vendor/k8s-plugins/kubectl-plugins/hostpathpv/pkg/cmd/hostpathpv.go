// Copyright © 2018 NAME HERE <EMAIL ADDRESS>
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

package cmd

import (
	"io"
	"os"
	"path"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var cfgFile string
var user string
var password string

// This represents the base command when called without any subcommands
var RootCmd = newRootCmd(os.Stdin, os.Stdout, os.Stderr)

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func getClientset() (*kubernetes.Clientset, error) {
	config, errConfig := buildConfig(path.Join(homedir.HomeDir(), ".kube", "config"))
	if errConfig != nil {
		return nil, errConfig
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func newRootCmd(in io.Reader, out, err io.Writer) *cobra.Command {
	cmds := &cobra.Command{
		Use:   "hostpathpv",
		Short: T("hostpathpv controls the Kubernetes cluster hostpathpv manager"),
		Long: `
      hostpathpv controls the Kubernetes cluster hostpathpv manager.

      Find more information at:
            https://gitlab.cloud.enndata.cn/kubernetes/k8s-plugins/kubectl-plugins/hostpathpv/`,
		Run: runHelp,
	}
	client, _ := getClientset()
	cmds.AddCommand(NewCmdHostPathPVGet(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVDescribe(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVDelete(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVMove(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVDisable(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVAdd(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVUpgrade(client, out, err))
	cmds.AddCommand(NewCmdHostPathPVScale(client, out, err))
	return cmds
}

func runHelp(cmd *cobra.Command, args []string) {
	cmd.Help()
}
