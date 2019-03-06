/*Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path"

	xfshostpath "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath"
	xfs "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager/common"
	"k8s-plugins/extender-scheduler/pkg/algorithm"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubectl/cmd/templates"
)

var (
	add_valid_resources = `Valid resource types include:

    * pv
    `

	add_long = templates.LongDesc(`
		Add pv quota paths.

		` + add_valid_resources)

	add_example = templates.Examples(`
		# Add pv nodename quota path.
		kubectl hostpathpv add pv pvname --node=nodename --path=quotapath
		
		`)
)

func NewCmdHostPathPVAdd(client *kubernetes.Clientset, out io.Writer, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add (TYPE/NAME ...) [flags]",
		Short:   T("Add pv quota path"),
		Long:    add_long,
		Example: add_example,
		Run: func(cmd *cobra.Command, args []string) {
			err := RunAdd(client, out, errOut, cmd, args)
			if err != nil {
				fmt.Fprintf(out, "run RunAdd err:%v\n", err)
			}
		},
	}
	cmd.Flags().String("node", "", "Add quota path of node")
	cmd.Flags().String("path", "", "Add path used with --node")
	return cmd
}

func RunAdd(clientset *kubernetes.Clientset, out, errOut io.Writer, cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "You must specify the type of resource to get. ", add_valid_resources)
		usageString := "Required resource not specified."
		return UsageErrorf(cmd, usageString)
	}

	resource := args[0]
	nodeName := GetFlagString(cmd, "node")
	hostPath := GetFlagString(cmd, "path")

	if nodeName == "" {
		fmt.Fprint(errOut, "You must specify the node. ")
		return UsageErrorf(cmd, "Required node")
	}
	if hostPath == "" {
		fmt.Fprint(errOut, "You must specify the hostPath. ")
		return UsageErrorf(cmd, "Required hostpath")
	}
	switch {
	case resource == "pvs" || resource == "pv":
		pvName := ""
		if len(args) >= 2 {
			pvName = args[1]
		}
		err := addPVs(clientset, pvName, nodeName, hostPath)
		if err != nil {
			fmt.Fprintf(errOut, "add err: %v\n", err)
		}
	default:
		fmt.Fprint(errOut, "You must specify the type of resource to describe. ", add_valid_resources)
		usageString := "Required resource not suport."
		return UsageErrorf(cmd, usageString)
	}

	return nil
}

func addPVs(clientset *kubernetes.Clientset, pvName, nodeName, hostPath string) error {
	pv, err := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if algorithm.IsCommonHostPathPV(pv) == false {
		return fmt.Errorf("pv %s is not hostpath pv", pv.Name)
	}

	if pv.Annotations == nil {
		pv.Annotations = make(map[string]string)
	}

	mountInfo := pv.Annotations[xfs.PVVolumeHostPathMountNode]

	mountList := xfshostpath.HostPathPVMountInfoList{}
	errUmarshal := json.Unmarshal([]byte(mountInfo), &mountList)
	if errUmarshal != nil {
		return errUmarshal
	}

	mountList, errAdd := getAddInfo(mountList, nodeName, hostPath)
	if errAdd != nil {
		return errAdd
	}
	buf, _ := json.Marshal(mountList)
	pv.Annotations[xfs.PVVolumeHostPathMountNode] = string(buf)
	errUpdate := updatePV(clientset, pv)
	if errUpdate != nil {
		return errUpdate
	}

	fmt.Printf("%s:%s add ok\n", nodeName, hostPath)
	return nil
}

func getAddInfo(list xfshostpath.HostPathPVMountInfoList,
	nodeName, hostpath string) (xfshostpath.HostPathPVMountInfoList, error) {
	for i, mountInfo := range list {
		if mountInfo.NodeName == nodeName {
			for _, mountPath := range mountInfo.MountInfos {
				if path.Clean(mountPath.HostPath) == path.Clean(hostpath) {
					return list, fmt.Errorf("%s:%s is exist", nodeName, hostpath)
				}
			}
			list[i].MountInfos = append(list[i].MountInfos, xfshostpath.MountInfo{
				HostPath:             hostpath,
				VolumeQuotaSize:      0,
				VolumeCurrentSize:    0,
				VolumeCurrentFileNum: 0,
				PodInfo:              nil,
			})
			return list, nil
		}
	}
	list = append(list, xfshostpath.HostPathPVMountInfo{
		NodeName: nodeName,
		MountInfos: xfshostpath.MountInfoList{xfshostpath.MountInfo{
			HostPath:             hostpath,
			VolumeQuotaSize:      0,
			VolumeCurrentSize:    0,
			VolumeCurrentFileNum: 0,
			PodInfo:              nil,
		}},
	})

	return list, nil
}
