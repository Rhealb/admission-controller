package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	hostpathcmd "k8s-plugins/kubectl-plugins/hostpathpv/pkg/cmd"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	updatingPipelineName      = "io.enndata.hppvtocsipv/updatingPipeline"
	updatingPipelineStartTime = "io.enndata.hppvtocsipv/updatingPipelineStartTime"
)

type UndoAction func(exitOk bool) (err error)
type Action func() (c bool, undo UndoAction, err error)

func getNowTimeStr() string {
	buf, _ := json.Marshal(time.Now())
	return string(buf)
}

type PipelineStep struct {
	name    string
	action  Action
	timeOut time.Duration
}

type UpdatePipeline struct {
	client        *kubernetes.Clientset
	pvName        string
	needWaitBound bool
	ret           chan error
	saveOldPV     *v1.PersistentVolume
	upgradeImage  string
	updateSteps   []PipelineStep
	stop          <-chan struct{}
}

func (upl *UpdatePipeline) TimeOut() time.Duration {
	var timeout time.Duration
	for _, s := range upl.updateSteps {
		timeout += s.timeOut
	}
	return timeout
}

func (upl *UpdatePipeline) isTimeOut(startTimeStr string) (bool, error) {
	var startTime time.Time
	err := json.Unmarshal([]byte(startTimeStr), &startTime)
	if err != nil {
		return false, fmt.Errorf("Unmarshal timestr %s err:%v", startTime, err)
	}
	return time.Now().Sub(startTime) > upl.TimeOut()*2, nil
}

func (upl *UpdatePipeline) stepCheck() (c bool, undo UndoAction, err error) {
	var pipelineName string
	if hostname, err := os.Hostname(); err != nil {
		return false, nil, err
	} else {
		pipelineName = hostname
	}
	pv, err := upl.client.Core().PersistentVolumes().Get(upl.pvName, metav1.GetOptions{})
	if err != nil {
		return false, nil, err
	} else if pv.Spec.HostPath == nil {
		return false, nil, fmt.Errorf("pv %s is not hostpathpv", pv.Name)
	} else if pv.Annotations != nil && pv.Annotations[updatingPipelineName] != pipelineName && pv.Annotations[updatingPipelineStartTime] != "" {
		if timeout, err := upl.isTimeOut(pv.Annotations[updatingPipelineStartTime]); timeout == false {
			glog.Infof("this is %s, pv %s is updating by %s\n", pipelineName, upl.pvName, pv.Annotations[updatingPipelineName])
			return false, nil, nil
		} else if err != nil {
			return false, nil, err
		}
	}
	if pv.Annotations == nil {
		pv.Annotations = make(map[string]string)
	}
	pv.Annotations[updatingPipelineName] = pipelineName
	pv.Annotations[updatingPipelineStartTime] = getNowTimeStr()
	_, errUpdate := upl.client.Core().PersistentVolumes().Update(pv)
	if errUpdate != nil {
		return false, nil, errUpdate
	}
	undo = func(exitOk bool) error {
		if exitOk {
			return nil
		}
		curPV, err := upl.client.Core().PersistentVolumes().Get(upl.pvName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		delete(curPV.Annotations, updatingPipelineName)
		delete(curPV.Annotations, updatingPipelineStartTime)
		if len(curPV.Annotations) == 0 {
			curPV.Annotations = nil
		}
		_, errUpdate := upl.client.Core().PersistentVolumes().Update(curPV)
		if errUpdate != nil {
			return errUpdate
		}
		return nil
	}
	return true, undo, nil
}

func (upl *UpdatePipeline) stepCreatePodToChangeQuotaType() (c bool, undo UndoAction, err error) {
	pv, err := upl.client.Core().PersistentVolumes().Get(upl.pvName, metav1.GetOptions{})
	if err != nil {
		return false, nil, err
	} else if pv.Spec.HostPath == nil {
		return false, nil, fmt.Errorf("pv %s is not hostpathpv", pv.Name)
	}
	nodeMountInfos, errInfo := hostpathcmd.GetPVQuotaPaths(pv)
	if errInfo != nil {
		return false, nil, errInfo
	}
	pods, err := hostpathcmd.CreatePodsToChangeQuotaPathType(upl.client, upl.pvName, upl.upgradeImage, nodeMountInfos)

	undo = func(exitOk bool) error {
		return hostpathcmd.WaitPodsDeleted(upl.client, "", pods, true)
	}
	if err != nil {
		return false, undo, err
	}
	if err := hostpathcmd.WaitPodQuit(upl.client, pods, 120); err != nil {
		return false, undo, err
	}
	return true, undo, nil
}

func (upl *UpdatePipeline) Done() chan error {
	return upl.ret
}

func (upl *UpdatePipeline) stepCreateTmpPV() (c bool, undo UndoAction, err error) {
	pv, err := upl.client.Core().PersistentVolumes().Get(upl.pvName, metav1.GetOptions{})
	if err != nil {
		return false, nil, err
	} else if pv.Spec.HostPath == nil {
		return false, nil, fmt.Errorf("pv %s is not hostpathpv", pv.Name)
	}

	tmpPvName := fmt.Sprintf("%s-csihostpathpv-tmp", hostpathcmd.GetMd5Hash(pv.Name, 10))
	if err := hostpathcmd.CreateCSIHostPathPV(upl.client, tmpPvName, pv, false); err != nil {
		return false, nil, err
	}
	undo = func(exitOk bool) error {
		return hostpathcmd.DeletePv(upl.client, tmpPvName)
	}
	return true, undo, nil
}

func (upl *UpdatePipeline) stepDeleteOldPV() (c bool, undo UndoAction, err error) {
	pv, err := upl.client.Core().PersistentVolumes().Get(upl.pvName, metav1.GetOptions{})
	if err != nil {
		return false, nil, err
	} else if pv.Spec.HostPath == nil {
		return false, nil, fmt.Errorf("pv %s is not hostpathpv", pv.Name)
	}
	upl.saveOldPV = pv
	errDelete := hostpathcmd.DeletePv(upl.client, upl.pvName)
	if errDelete != nil {
		return false, nil, errDelete
	}
	undo = func(exitOk bool) error {
		if exitOk {
			return nil
		}
		_, errCreate := upl.client.Core().PersistentVolumes().Create(pv)
		return errCreate
	}
	return true, undo, nil
}

func (upl *UpdatePipeline) stepCreateCSIPV() (c bool, undo UndoAction, err error) {
	if err := hostpathcmd.CreateCSIHostPathPV(upl.client, upl.pvName, upl.saveOldPV, true); err != nil {
		return false, nil, err
	} else {
		undo = func(exitOk bool) error {
			if exitOk {
				return nil
			}
			return hostpathcmd.DeletePv(upl.client, upl.pvName)
		}
		return true, undo, nil
	}
}

func (upl *UpdatePipeline) stepWaitCSIPVBound() (c bool, undo UndoAction, err error) {
	if upl.needWaitBound == true {
		err := hostpathcmd.WaitPVBound(upl.client, upl.pvName, 40)
		if err != nil {
			return false, nil, err
		}
	}
	return true, nil, nil
}

func (upl *UpdatePipeline) stepRestartPods() (c bool, undo UndoAction, err error) {
	dps, deleteNames, err := hostpathcmd.GetPVsUsingPods(upl.client, []string{upl.pvName})
	glog.Infof("UpdatePipeline start restart pods %v of pv %s", deleteNames, upl.pvName)
	errDelete := hostpathcmd.DeletePods(upl.client, "", dps, 1*time.Minute)
	if errDelete != nil {
		glog.Errorf("UpdatePipeline delete pods err:%v", errDelete)
	}
	return true, nil, nil
}

func (upl *UpdatePipeline) Run() {
	var ret error
	undoActions := make([]UndoAction, 0, len(upl.updateSteps))
	timeOutCh := make(chan struct{}, 0)
	go func() {
		time.Sleep(upl.TimeOut())
		close(timeOutCh)
	}()
	glog.Infof("UpdatePipeline of %s started, timeout:%v", upl.pvName, upl.TimeOut())
	undo := func(exitOk bool) {
		glog.Infof("start recover of pv %s", upl.pvName)

		errs := make([]error, 0, len(undoActions))
		for i := len(undoActions) - 1; i >= 0; i-- {
			undo := undoActions[i]
			if err := undo(exitOk); err != nil {
				errs = append(errs, err)
			}
		}
		undoActions = []UndoAction{}
		glog.Infof("end recover of %s errs:%v", upl.pvName, errs)
	}
	defer func() {
		if ret != nil {
			undo(false)
		} else {
			undo(true)
		}
		upl.ret <- ret
	}()
	for i, step := range upl.updateSteps {
		stopped := false
		glog.Infof("UpdatePipeline [%s]: Step[%d][%s]: start\n", upl.pvName, i, step.name)
		select {
		case <-timeOutCh:
			ret = fmt.Errorf("UpdatePipeline [%s]: timeout", upl.pvName)
			stopped = true
		case <-upl.stop:
			ret = fmt.Errorf("UpdatePipeline [%s]: stopped", upl.pvName)
			stopped = true
		default:
		}
		if stopped {
			break
		}
		c, undo, err := step.action()
		if undo != nil {
			undoActions = append(undoActions, undo)
		}
		if err != nil {
			ret = err
			glog.Errorf("UpdatePipeline [%s]: Step[%d][%s]: action err:%v\n", upl.pvName, i, step.name, err)
			break
		} else if c == false {
			glog.Infof("UpdatePipeline [%s]: Step[%d][%s]: stop and break\n", upl.pvName, i, step.name)
			break
		}
		glog.Infof("UpdatePipeline [%s]: Step[%d][%s]: success\n", upl.pvName, i, step.name)
	}
	glog.Infof("UpdatePipeline of %s stopped", upl.pvName)
}

type PVUpdateManager struct {
	client         *kubernetes.Clientset
	updateInterval time.Duration
	stopCh         chan struct{}
	running        bool
	upgradeImage   string
	mu             sync.Mutex
}

func NewPVUpdateManager(client *kubernetes.Clientset, updateInterval time.Duration, upgradeImage string) *PVUpdateManager {
	return &PVUpdateManager{
		client:         client,
		updateInterval: updateInterval,
		stopCh:         make(chan struct{}),
		running:        false,
		upgradeImage:   upgradeImage,
	}
}

func (pvum *PVUpdateManager) Start() error {
	if pvum.IsRunning() == true {
		return nil
	}
	pvum.running = true

	sharedInformers := informers.NewSharedInformerFactory(pvum.client, 0)
	pvInformer := sharedInformers.Core().V1().PersistentVolumes()
	pvSynced := pvInformer.Informer().HasSynced

	syncPVChan := make(chan *v1.PersistentVolume, 100)
	syncPVMap := make(map[string]bool)
	var mu sync.Mutex
	isPVWaitSync := func(pvName string) bool {
		mu.Lock()
		defer mu.Unlock()
		_, exist := syncPVMap[pvName]
		return exist
	}
	pvSyncAdd := func(pvName string) {
		mu.Lock()
		defer mu.Unlock()
		syncPVMap[pvName] = true
	}
	pvSyncDone := func(pvName string) {
		mu.Lock()
		defer mu.Unlock()
		delete(syncPVMap, pvName)
	}
	pvEventHandlerFuncs := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			newPV := obj.(*v1.PersistentVolume)
			if newPV.Spec.HostPath != nil && isPVWaitSync(newPV.Name) == false {
				pvSyncAdd(newPV.Name)
				syncPVChan <- newPV
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newPV := newObj.(*v1.PersistentVolume)
			if newPV.Spec.HostPath != nil && isPVWaitSync(newPV.Name) == false {
				pvSyncAdd(newPV.Name)
				syncPVChan <- newPV
			}
		},
	}
	pvInformer.Informer().AddEventHandler(pvEventHandlerFuncs)

	sharedInformers.Start(pvum.stopCh)

	if !cache.WaitForCacheSync(wait.NeverStop, pvSynced) {
		return fmt.Errorf("timed out waiting for namespace caches to sync")
	}

	go func() {
		defer func() {
			pvum.running = false
		}()

		for {
			var pv *v1.PersistentVolume
			select {
			case <-pvum.stopCh:
				glog.Infof("Update will be stopped")
			case pv = <-syncPVChan:
			}
			if pv == nil {
				break
			}
			if pv.Spec.HostPath != nil {
				pipeline := pvum.createPVUpdatePipeline(pvum.client, pv.Name, pvum.upgradeImage, pv.Status.Phase == v1.VolumeBound, pvum.stopCh)
				go pipeline.Run()
				err := <-pipeline.Done()
				pvSyncDone(pv.Name)
				if err != nil {
					glog.Errorf("update pv %s to csi err:%v", pv.Name, err)
				} else {
					time.Sleep(pvum.updateInterval)
				}
			}
		}
	}()
	glog.Infof("PVUpdateManager start exit")
	return nil
}

func (pvum *PVUpdateManager) createPVUpdatePipeline(client *kubernetes.Clientset, pvName, upgradeImage string, isBound bool, stop <-chan struct{}) *UpdatePipeline {
	ret := &UpdatePipeline{
		client:        client,
		pvName:        pvName,
		needWaitBound: isBound,
		ret:           make(chan error),
		updateSteps:   []PipelineStep{},
		upgradeImage:  upgradeImage,
		stop:          stop,
	}
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "Check", action: ret.stepCheck, timeOut: 3 * time.Second})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "CreateChangePod", action: ret.stepCreatePodToChangeQuotaType, timeOut: 3 * time.Minute})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "CreateTmpPV", action: ret.stepCreateTmpPV, timeOut: 3 * time.Second})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "DeleteOldPV", action: ret.stepDeleteOldPV, timeOut: 3 * time.Second})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "CreateCSIPV", action: ret.stepCreateCSIPV, timeOut: 3 * time.Second})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "WaitCSIPVBound", action: ret.stepWaitCSIPVBound, timeOut: 50 * time.Second})
	ret.updateSteps = append(ret.updateSteps, PipelineStep{name: "RestartPods", action: ret.stepRestartPods, timeOut: 5 * time.Minute})
	return ret
}

func (pvum *PVUpdateManager) IsRunning() bool {
	pvum.mu.Lock()
	defer pvum.mu.Unlock()
	return pvum.running
}

func (pvum *PVUpdateManager) Stop() error {
	pvum.mu.Lock()
	defer pvum.mu.Unlock()
	if pvum.running == false {
		return nil
	} else {
		close(pvum.stopCh)
		pvum.running = false
		return nil
	}
}
