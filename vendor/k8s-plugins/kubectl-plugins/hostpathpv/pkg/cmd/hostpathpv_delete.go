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
	"time"

	xfshostpath "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath"
	//	"k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager"
	xfs "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager/common"
	"k8s-plugins/extender-scheduler/pkg/algorithm"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubectl/cmd/templates"
)

var (
	hostpathpv_delete_valid_resources = `Valid resource types include:

    * pv
    `

	hostpathpv_delete_long = templates.LongDesc(`
		Delete pv quota paths.

		` + hostpathpv_delete_valid_resources)

	hostpathpv_delete_example = templates.Examples(`
		# Delete pv nodename quota path.
		kubectl hostpathpv delete pv pvname --node=nodename --path=quotapath
		
		# Delete pv all quota path.
		kubectl hostpathpv delete pv pvname --all=true
		
		# Delete pv nodename quota path without verify.
		kubectl hostpathpv delete pv pvname --node=nodename --path=quotapath --force=true
		`)
)

func NewCmdHostPathPVDelete(client *kubernetes.Clientset, out io.Writer, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete (TYPE/NAME ...) [flags]",
		Short:   T("Delete pv quota path"),
		Long:    hostpathpv_delete_long,
		Example: hostpathpv_delete_example,
		Run: func(cmd *cobra.Command, args []string) {
			err := RunDelete(client, out, errOut, cmd, args)
			if err != nil {
				fmt.Fprintf(out, "run RunDelete err:%v\n", err)
			}
		},
	}
	cmd.Flags().String("node", "", "Delete quota path of node")
	cmd.Flags().String("path", "", "Delete path used with --node")
	cmd.Flags().Bool("all", false, "Delete all quota path")
	cmd.Flags().Bool("force", false, "Delete force with no verify")
	return cmd
}

func RunDelete(clientset *kubernetes.Clientset, out, errOut io.Writer, cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "You must specify the type of resource to get. ", hostpathpv_delete_valid_resources)
		usageString := "Required resource not specified."
		return UsageErrorf(cmd, usageString)
	}

	resource := args[0]
	nodeName := GetFlagString(cmd, "node")
	hostPath := GetFlagString(cmd, "path")
	deleteAll := GetFlagBool(cmd, "all")
	deleteForce := GetFlagBool(cmd, "force")

	if nodeName == "" && deleteAll == false {
		fmt.Fprint(errOut, "You must specify the node. ")
		return UsageErrorf(cmd, "Required node")
	}
	if deleteAll == true {
		nodeName = ""
	}
	switch {
	case resource == "pvs" || resource == "pv":
		pvName := ""
		if len(args) >= 2 {
			pvName = args[1]
		}
		err := deletePVs(clientset, pvName, nodeName, hostPath, deleteForce)
		if err != nil {
			fmt.Fprintf(errOut, "delete err: %v\n", err)
		}
	default:
		fmt.Fprint(errOut, "You must specify the type of resource to describe. ", hostpathpv_delete_valid_resources)
		usageString := "Required resource not suport."
		return UsageErrorf(cmd, usageString)
	}

	return nil
}

func deletePVs(clientset *kubernetes.Clientset, pvName, nodeName, hostPath string, deleteForce bool) error {
	pv, err := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if algorithm.IsCommonHostPathPV(pv) == false {
		return fmt.Errorf("pv %s is not hostpath pv", pv.Name)
	}

	if pv.Annotations == nil || pv.Annotations[xfs.PVVolumeHostPathMountNode] == "" {
		return fmt.Errorf("pv %s is no quota path to delete", pv.Name)
	}

	mountInfo := pv.Annotations[xfs.PVVolumeHostPathMountNode]

	mountList := xfshostpath.HostPathPVMountInfoList{}
	errUmarshal := json.Unmarshal([]byte(mountInfo), &mountList)
	if errUmarshal != nil {
		return errUmarshal
	}

	newMountList, deletePaths := getDeleteInfo(mountList, nodeName, hostPath)
	if len(deletePaths) == 0 {
		fmt.Printf("not quota path to delete\n")
		return nil
	}
	if deleteForce != true {
		for _, path := range deletePaths {
			fmt.Printf("%s\n", path)
		}
		fmt.Printf("Are sure to delete above quota path (y/n):")
		var ok string
		fmt.Scanf("%s", &ok)
		if ok != "y" {
			return nil
		}
	}
	buf, _ := json.Marshal(newMountList)
	pv.Annotations[xfs.PVVolumeHostPathMountNode] = string(buf)
	errUpdate := updatePV(clientset, pv)
	if errUpdate != nil {
		return errUpdate
	}
	for _, path := range deletePaths {
		fmt.Printf("%s delete ok\n", path)
	}
	return nil
}

func getDeleteInfo(list xfshostpath.HostPathPVMountInfoList,
	nodeName, hostpath string) (newList xfshostpath.HostPathPVMountInfoList, deletePaths []string) {
	for _, mountInfo := range list {
		if nodeName != "" && mountInfo.NodeName == nodeName {
			item := xfshostpath.HostPathPVMountInfo{
				NodeName:   mountInfo.NodeName,
				MountInfos: make(xfshostpath.MountInfoList, 0, len(mountInfo.MountInfos)),
			}
			for _, mountPath := range mountInfo.MountInfos {
				if hostpath == "" || (hostpath != "" && path.Clean(mountPath.HostPath) == path.Clean(hostpath)) {
					deletePaths = append(deletePaths, mountInfo.NodeName+":"+mountPath.HostPath)
				} else {
					item.MountInfos = append(item.MountInfos, mountPath)
				}
			}
			if len(item.MountInfos) > 0 {
				newList = append(newList, item)
			}
		} else if nodeName == "" {
			for _, mountPath := range mountInfo.MountInfos {
				deletePaths = append(deletePaths, mountInfo.NodeName+":"+mountPath.HostPath)
			}
		} else {
			item := xfshostpath.HostPathPVMountInfo{
				NodeName:   mountInfo.NodeName,
				MountInfos: make(xfshostpath.MountInfoList, 0, len(mountInfo.MountInfos)),
			}
			item.MountInfos = append(item.MountInfos, mountInfo.MountInfos...)
			if len(item.MountInfos) > 0 {
				newList = append(newList, item)
			}
		}
	}
	return
}

func updatePV(clientset *kubernetes.Clientset, pv *v1.PersistentVolume) error {
	var err error
	for i := 0; i < 3; i++ {
		_, err = clientset.Core().PersistentVolumes().Update(pv)
		if err == nil {
			return nil
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return err
}
