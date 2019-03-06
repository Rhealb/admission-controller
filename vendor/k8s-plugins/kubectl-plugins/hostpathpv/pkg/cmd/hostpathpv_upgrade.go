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
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	xfshostpath "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath"
	xfs "k8s-plugins/csi-plugin/hostpathpv/pkg/hostpath/xfsquotamanager/common"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubectl/cmd/templates"
)

var (
	upgrade_valid_resources = `Valid resource types include:

    * pv
    `

	upgrade_long = templates.LongDesc(`
		Upgrade hostpath pv to CSI hostpathpv.

		` + upgrade_valid_resources)

	upgrade_example = templates.Examples(`
		# Upgrade quota path.
		kubectl hostpathpv upgrade pv pvname
		`)
)

const (
	default_upgrade_busyboximage = "127.0.0.1:29006/library/busybox:1.25"
	csihostpathpv_plugin_name    = "xfshostpathplugin"
)

func NewCmdHostPathPVUpgrade(client *kubernetes.Clientset, out io.Writer, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "upgrade (TYPE/NAME ...) [flags]",
		Short:   T("Upgrade hostpath pv"),
		Long:    upgrade_long,
		Example: upgrade_example,
		Run: func(cmd *cobra.Command, args []string) {
			err := RunUpgrade(client, out, errOut, cmd, args)
			if err != nil {
				fmt.Fprintf(out, "run RunUpgrade err:%v\n", err)
			}
		},
	}
	cmd.Flags().String("upgradeimage", default_upgrade_busyboximage, "Image create to change quota dir type")
	cmd.Flags().Duration("deleteinterval", 10*time.Second, "Use quota pod deleting interval")
	cmd.Flags().Bool("force", false, "Upgrade force")
	return cmd
}

func RunUpgrade(clientset *kubernetes.Clientset, out, errOut io.Writer, cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "You must specify the type of resource to get. ", upgrade_valid_resources)
		usageString := "Required resource not specified."
		return UsageErrorf(cmd, usageString)
	}

	resource := args[0]
	upgradeImage := GetFlagString(cmd, "upgradeimage")
	delInterval := GetFlagDuration(cmd, "deleteinterval")
	force := GetFlagBool(cmd, "force")
	switch {
	case resource == "pv":
		pvName := ""
		if len(args) >= 2 {
			pvName = args[1]
		}
		if pvName != "" {
			err := upgradePV(clientset, upgradeImage, strings.Split(pvName, ","), force, delInterval)
			if err != nil {
				fmt.Fprintf(errOut, "\nupgrade pv %s err: %v\n", pvName, err)
			}
		} else {
			return fmt.Errorf("pvname should not be empty")
		}
	default:
		fmt.Fprint(errOut, "You must specify the type of resource to describe. ", upgrade_valid_resources)
		usageString := "Required resource not suport."
		return UsageErrorf(cmd, usageString)
	}

	return nil
}

func getPVsByNames(clientset *kubernetes.Clientset, pvNames []string) ([]*v1.PersistentVolume, error) {
	ret := make([]*v1.PersistentVolume, len(pvNames))
	for i, pvName := range pvNames {
		pv, err := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
		if err != nil {
			return ret, fmt.Errorf("get pv %s err:%v", pvName, err)
		}
		ret[i] = pv
	}
	return ret, nil
}

func upgradePV(clientset *kubernetes.Clientset, upgradeImage string, pvNames []string, upgradeForce bool, delInterval time.Duration) error {
	// init clean up work
	//upgradeSuccess := false
	cleanDeferFunList = make([]cleanDeferFun, 0, 10)
	defer runCleanDeferFun()
	signalChan := make(chan os.Signal)
	go handleSignal(signalChan)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)

	//
	updatePVs, errGetPVs := getPVsByNames(clientset, pvNames)
	if errGetPVs != nil {
		return fmt.Errorf("get pvs err:%v", errGetPVs)
	}

	if err := isValidUpgradPVs(updatePVs); err != nil {
		return err
	}
	var needDeletePods []*v1.Pod

	if dps, deleteNames, err := GetPVsUsingPods(clientset, pvNames); err != nil {
		return err
	} else {
		if upgradeForce == false {
			fmt.Printf("Are you sure to upgrade pv %v by delete pods %v (y/n):", pvNames, deleteNames)
			var ans byte
			fmt.Scanf("%c", &ans)
			if ans != 'y' && ans != 'Y' {
				return nil
			}
		}
		needDeletePods = dps
	}

	statueChan := make(chan string, 0)
	var step int = 1
	completeStep := func(e error) error {
		if e == nil {
			step++
			statueChan <- "OK"
		} else {
			statueChan <- "Fail"
		}
		time.Sleep(10 * time.Microsecond)
		return e
	}
	defer close(statueChan)

	//************************************ step 1 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start create pods to change quotapath type:", step)
	createPods := make([]*v1.Pod, 0)
	for _, updatePV := range updatePVs {
		nodeMountInfos, errInfo := GetPVQuotaPaths(updatePV)
		if errInfo != nil {
			return completeStep(errInfo)
		}
		pods, err := CreatePodsToChangeQuotaPathType(clientset, updatePV.Name, upgradeImage, nodeMountInfos)
		addCleanDeferFun(func() {
			errPodDelete := WaitPodsDeleted(clientset, "", pods, true)
			if errPodDelete != nil {
				fmt.Printf("\nclean : delele change quotapath type pod err:%v\n", errPodDelete)
			}
		})
		createPods = append(createPods, pods...)
		if err != nil {
			return completeStep(err)
		}
	}
	completeStep(nil)

	//************************************ step 2 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start wait change quotapath type pod exist:", step)
	if err := WaitPodQuit(clientset, createPods, 120); err != nil {
		return completeStep(fmt.Errorf("wait change quotapath type pod exist err:%v\n", err))
	}
	completeStep(nil)

	//************************************ step 3 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start create CSI hostpath pv to keep quota dir:", step)
	for _, updatePV := range updatePVs {
		tmpPvName := fmt.Sprintf("%s-csihostpathpv-tmp", GetMd5Hash(updatePV.Name, 10))
		if err := CreateCSIHostPathPV(clientset, tmpPvName, updatePV, false); err != nil {
			return completeStep(err)
		}
		addCleanDeferFun(func() {
			DeletePv(clientset, tmpPvName)
		})
	}
	completeStep(nil)

	//************************************ step 4 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start delete old hostpath pv %v:", step, pvNames)
	for _, pvName := range pvNames {
		if err := DeletePv(clientset, pvName); err != nil {
			return completeStep(err)
		}
	}
	completeStep(nil)

	//************************************ step 5 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start create CSI hostpath pv to replace:", step)
	for _, updatePV := range updatePVs {
		if err := CreateCSIHostPathPV(clientset, updatePV.Name, updatePV, true); err != nil {
			return completeStep(err)
		}
	}
	completeStep(nil)

	//************************************ step 6 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start wait CSI hostpath pv bound:", step)
	for _, updatePV := range updatePVs {
		if updatePV.Status.Phase == v1.VolumeBound {
			if err := WaitPVBound(clientset, updatePV.Name, 40); err != nil {
				return completeStep(err)
			}
		}
	}
	completeStep(nil)

	//************************************ step 7 ***********************************/
	stepPrintf(statueChan, "(Step %d) Start delete using hostpath pv pods:", step)
	if errDelete := DeletePods(clientset, "", needDeletePods, delInterval); errDelete != nil {
		return completeStep(errDelete)
	}
	completeStep(nil)

	fmt.Printf("\nUpgrade hostpath pv %v to csi hostpath pv success\n", pvNames)
	return nil
}

func GetMd5Hash(str string, l int) string {
	has := md5.Sum([]byte(str))
	md5str1 := fmt.Sprintf("%x", has) //将[]byte转成16进制
	if l >= len(md5str1) {
		return md5str1
	}
	return md5str1[:l]
}

func WaitPVBound(clientset *kubernetes.Clientset, pvName string, timeOut int) error {
	for i := 0; i < timeOut; i++ {
		pv, err := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if pv.Status.Phase == v1.VolumeBound {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("wait pod quit timeout")
}

func createPv(clientset *kubernetes.Clientset, pv *v1.PersistentVolume) (*v1.PersistentVolume, error) {
	clonePv := pv.DeepCopy()
	clonePv.ResourceVersion = ""
	return clientset.Core().PersistentVolumes().Create(clonePv)
}

func IsNotFound(err error) bool {
	return strings.Contains(err.Error(), "not found")
}

func DeletePv(clientset *kubernetes.Clientset, name string) error {

	if err := clientset.Core().PersistentVolumes().Delete(name, nil); err != nil {
		return fmt.Errorf("delete pv err:%v", err)
	}
	for {
		curPv, err := clientset.Core().PersistentVolumes().Get(name, metav1.GetOptions{})
		if err != nil {
			if IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("get pv err:%v", err)
		} else {
			time.Sleep(100 * time.Microsecond)
		}
		curPv.Finalizers = nil
		clientset.Core().PersistentVolumes().Update(curPv)
	}
}

//func listAllPods(clientset *kubernetes.Clientset) ([]*v1.Pod, error) {
//	allPods, errPods := clientset.Core().Pods(v1.NamespaceAll).List(metav1.ListOptions{})
//	if errPods != nil {
//		return []*v1.Pod{}, fmt.Errorf("get pods err:%v", errPods)
//	}
//	return allPods, nil
//}

func GetPVsUsingPods(clientset *kubernetes.Clientset, pvNames []string) ([]*v1.Pod, []string, error) {
	pvcMaps := make(map[string]bool)
	var namespaces string

	for _, pvName := range pvNames {
		pv, errGetPV := clientset.Core().PersistentVolumes().Get(pvName, metav1.GetOptions{})
		if errGetPV != nil {
			return []*v1.Pod{}, []string{}, fmt.Errorf("get pv %s err:%v", pvName, errGetPV)
		}
		if pv.Spec.ClaimRef != nil {
			pvcNs := pv.Spec.ClaimRef.Namespace
			pvcName := pv.Spec.ClaimRef.Name
			if namespaces == "" {
				namespaces = pvcNs
			} else if namespaces != pvcNs {
				return []*v1.Pod{}, []string{}, fmt.Errorf("has pv belong %s, %s two namespace", namespaces, pvcNs)
			}
			pvcMaps[pvcName] = true
		}
	}
	allPods, errPods := clientset.Core().Pods(namespaces).List(metav1.ListOptions{})
	if errPods != nil {
		return []*v1.Pod{}, []string{}, errPods
	}
	ret := make([]*v1.Pod, 0, len(allPods.Items))
	retName := make([]string, 0, len(allPods.Items))
	for i := range allPods.Items {
		pod := &allPods.Items[i]
		for _, v := range pod.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && pvcMaps[v.PersistentVolumeClaim.ClaimName] == true {
				ret = append(ret, pod)
				retName = append(retName, pod.Name)
				break
			}
		}
	}
	return ret, retName, nil
}

func CreateCSIHostPathPV(clientset *kubernetes.Clientset, pvName string, updatePV *v1.PersistentVolume, copyLable bool) error {
	newAnns := make(map[string]string)
	newLable := make(map[string]string)
	if updatePV.Annotations != nil {
		for k, v := range updatePV.Annotations {
			newAnns[k] = v
		}
	}
	if copyLable == false {
		newLable["name"] = pvName
		newLable["app"] = "kubectlhostpathpv"
	} else {
		if updatePV.Labels != nil {
			for k, v := range updatePV.Labels {
				newLable[k] = v
			}
		}
	}
	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvName,
			Labels:      newLable,
			Annotations: newAnns,
		},
		Spec: v1.PersistentVolumeSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Capacity:    v1.ResourceList{v1.ResourceStorage: updatePV.Spec.Capacity[v1.ResourceStorage]},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{Driver: csihostpathpv_plugin_name, VolumeHandle: string(updatePV.UID)},
			},
		},
	}
	var lastErr error
	for i := 1; i <= 5; i++ {
		_, err := clientset.Core().PersistentVolumes().Create(pv)
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Microsecond)
	}
	return lastErr
}

func CreatePodsToChangeQuotaPathType(clientset *kubernetes.Clientset, pvName, imageName string, nodeMountInfos map[string][]string) ([]*v1.Pod, error) {
	ret := make([]*v1.Pod, 0, len(nodeMountInfos))
	timeout := int64(100)

	getVolumeAndMountVolume := func(paths []string) ([]v1.Volume, []v1.VolumeMount, string) {
		vs := make([]v1.Volume, 0, len(paths))
		vms := make([]v1.VolumeMount, 0, len(paths))
		cmd := ""
		for i, p := range paths {
			p = path.Clean(p)
			dirName := fmt.Sprintf("dir%d", i)
			csisDirName := fmt.Sprintf("csis%d", i)
			csiDir := path.Join(path.Dir(p), "csis")
			csiFileName := path.Base(p)
			vms = append(vms, v1.VolumeMount{Name: dirName, MountPath: fmt.Sprintf("/changequotapath/dir%d", i)})
			vms = append(vms, v1.VolumeMount{Name: csisDirName, MountPath: fmt.Sprintf("/changequotapath/csis%d", i)})
			vs = append(vs, v1.Volume{Name: dirName, VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: p}}})
			vs = append(vs, v1.Volume{Name: csisDirName, VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: csiDir}}})
			if i > 0 {
				cmd += fmt.Sprintf(" && (echo true > /changequotapath/dir%d/csi || echo true > /changequotapath/csis%d/%s)", i, i, csiFileName)
			} else {
				cmd = fmt.Sprintf("(echo true > /changequotapath/dir%d/csi || echo true > /changequotapath/csis%d/%s)", i, i, csiFileName)
			}
		}
		return vs, vms, cmd
	}
	for nodeName, paths := range nodeMountInfos {
		vs, vms, cmd := getVolumeAndMountVolume(paths)
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("change-%s-%s-quotapath-tmppod", nodeName, pvName),
				Namespace: metav1.NamespaceSystem,
			},
			Spec: v1.PodSpec{
				NodeName:              nodeName,
				ActiveDeadlineSeconds: &timeout,
				RestartPolicy:         v1.RestartPolicyNever,
				Volumes:               vs,
				Containers: []v1.Container{
					{
						Name:            "change",
						Image:           imageName,
						ImagePullPolicy: v1.PullIfNotPresent,
						Command:         []string{"/bin/sh"},
						Args:            []string{"-c", cmd},
						VolumeMounts:    vms,
						Resources: v1.ResourceRequirements{
							Limits:   v1.ResourceList{"cpu": resource.MustParse("0m"), "memory": resource.MustParse("0Mi")},
							Requests: v1.ResourceList{"cpu": resource.MustParse("0m"), "memory": resource.MustParse("0Mi")},
						},
					},
				},
			},
		}
		createPod, err := clientset.Core().Pods(metav1.NamespaceSystem).Create(pod)
		ret = append(ret, createPod)
		if err != nil {
			return ret, fmt.Errorf("create pod %s:%s err:%v", pod.Namespace, pod.Name, err)
		}
	}
	return ret, nil
}

func GetPVQuotaPaths(pv *v1.PersistentVolume) (map[string][]string, error) {
	if pv.Annotations == nil || pv.Annotations[xfs.PVVolumeHostPathMountNode] == "" {
		return map[string][]string{}, nil
	}
	mountInfo := pv.Annotations[xfs.PVVolumeHostPathMountNode]

	mountList := xfshostpath.HostPathPVMountInfoList{}
	errUmarshal := json.Unmarshal([]byte(mountInfo), &mountList)
	if errUmarshal != nil {
		return map[string][]string{}, errUmarshal
	}
	ret := make(map[string][]string)
	for _, nodeMount := range mountList {
		paths := make([]string, 0, len(nodeMount.MountInfos))
		for _, info := range nodeMount.MountInfos {
			paths = append(paths, info.HostPath)
		}
		ret[nodeMount.NodeName] = paths
	}
	return ret, nil
}

func isValidUpgradPVs(pvs []*v1.PersistentVolume) error {
	for _, pv := range pvs {
		if pv.Spec.HostPath == nil {
			return fmt.Errorf("pv %s is not a hostpath pv", pv.Name)
		}
	}
	return nil
}
