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
	"fmt"
	"io"
	xfshostpath "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath"
	"k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager/common"
	"k8s-plugins/extender-scheduler/pkg/algorithm"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubectl/cmd/templates"
)

var (
	scale_valid_resources = `Valid resource types include:

    * pv
    `

	scale_long = templates.LongDesc(`
		Scale capacity.

		` + disable_valid_resources)

	scale_example = templates.Examples(`
		# Scale up pv's capacity example.
		kubectl hostpathpv scale pv pvname up 100
		
		# Scale down pv's capacity example.
		kubectl hostpathpv scale pv pvname down 100
        
        # Scale pv's capacity to example.
        kubectl hostpathpv scale pv pvname to 1100
		`)
)

func NewCmdHostPathPVScale(client *kubernetes.Clientset, out io.Writer, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "scale (TYPE/NAME ...) [flags]",
		Short:   T("Scale up, down or to pv's capacity"),
		Long:    scale_long,
		Example: scale_example,
		Run: func(cmd *cobra.Command, args []string) {
			err := RunScale(client, out, errOut, cmd, args)
			if err != nil {
				fmt.Fprintf(out, "run RunScale err:%v\n", err)
			}
		},
	}
	return cmd
}

func RunScale(clientset *kubernetes.Clientset, out, errOut io.Writer, cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "You must specify the type of resource to disable. ", scale_valid_resources)
		usageString := "Required resource not specified."
		return UsageErrorf(cmd, usageString)
	}

	resource := args[0]

	switch {
	case resource == "pvs" || resource == "pv":
		if len(args) == 4 {
			pvName := args[1]
			op := args[2]
			sizeStr := args[3]
			if op != "down" && op != "up" && op != "to" {
				return fmt.Errorf("scale not support scale %s operation", op)
			}
			size, err := strconv.Atoi(sizeStr)
			if err != nil {
				return fmt.Errorf("size %s is not valid", sizeStr)
			}
			err = scalePV(clientset, pvName, op, int64(size)*1024*1024)
			if err != nil {
				fmt.Fprintf(errOut, "scale pv err: %v\n", err)
			}
		} else {
			return fmt.Errorf("len(args) != 4")
		}
	default:
		fmt.Fprint(errOut, "You must specify the type of resource to disable. ", scale_valid_resources)
		usageString := "Required resource not suport."
		return UsageErrorf(cmd, usageString)
	}

	return nil
}

func scalePV(clientset *kubernetes.Clientset, pvName, op string, size int64) error {
	pv, errGetPV := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
	if errGetPV != nil {
		return fmt.Errorf("get pv err:%v", errGetPV)
	}
	if algorithm.IsCommonHostPathPV(pv) == false {
		return fmt.Errorf("pv %s is not a hostpath pv", pvName)
	}
	var toSize int64
	if curSize, errGetSize := algorithm.GetHostPathPVCapacity(pv); errGetSize != nil {
		return fmt.Errorf("get pv %s capacity err:%v", pvName, errGetSize)
	} else {
		switch op {
		case "up":
			toSize = curSize + size
		case "down":
			toSize = curSize - size
		case "to":
			toSize = size
		}
		if curSize == toSize && pv.Annotations != nil && pv.Annotations[common.PVHostPathCapacityAnn] == "" {
			fmt.Printf("pv %s cursize is %s no change\n", pvName, strings.Trim(convertIntToString(curSize), " "))
			return nil
		}
	}

	algorithm.SetHostpathPVCapacity(pv, toSize)
	_, err := clientset.Core().PersistentVolumes().Update(pv)
	if err != nil {
		return fmt.Errorf("set pv %s capacity to %s err:%v", pvName, strings.Trim(convertIntToString(toSize), " "), err)
	}
	if algorithm.IsCSIHostPathPV(pv) == false { // only csi plugin support show more scale detail info
		fmt.Printf("set pv %s capacity to %s success\n", pvName, strings.Trim(convertIntToString(toSize), " "))
		return nil
	}
	mountList, errMountList := algorithm.GetHostPathPVMountInfoList(pv)
	if errMountList != nil {
		return fmt.Errorf("get pv %s mount info err:%v", pvName, errMountList)
	}
	checked := make(map[string]bool)
	allSuccess := true
	allChecked := true
	for i := 0; i < 20; i++ {
		if pv, errGetPV := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{}); errGetPV == nil {
			scaleStates := xfshostpath.GetPVScaleStates(pv)
			allChecked = true
			allSuccess = true

			for _, infos := range mountList {
				nodeName := infos.NodeName
				for _, mountInfo := range infos.MountInfos {
					id := fmt.Sprintf("%s:%s", nodeName, mountInfo.HostPath)
					if _, exist := checked[id]; exist {
						continue
					}
					if exist, stateStr := xfshostpath.GetNodePathSyncState(scaleStates, nodeName, mountInfo.HostPath, toSize); exist {
						if stateStr == "" {
							fmt.Printf("node [%s] path [%s] scale to %s success\n", nodeName, mountInfo.HostPath, strings.Trim(convertIntToString(toSize), " "))
						} else {
							allSuccess = false
							fmt.Printf("node [%s] path [%s] scale to %s fail because [%s]\n", nodeName, mountInfo.HostPath, strings.Trim(convertIntToString(toSize), " "), stateStr)
						}
						checked[id] = true
					} else {
						allChecked = false
					}
				}
			}
			if allChecked {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}
	if allChecked == false || allSuccess == false {
		fmt.Printf("scale pv %s %s %s fail\n", pvName, op, strings.Trim(convertIntToString(size), " "))
	} else {
		fmt.Printf("scale pv %s %s %s success, cur capacity:%s\n", pvName, op, strings.Trim(convertIntToString(size), " "), strings.Trim(convertIntToString(toSize), " "))
	}
	return nil
}
