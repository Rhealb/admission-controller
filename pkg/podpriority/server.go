/*
Copyright 2018 The Kubernetes Authors.

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
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"k8s-plugins/admission-controller/pkg/utils/metrics"
	"k8s-plugins/extender-scheduler/pkg/algorithm"
	"net/http"
	"sort"

	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type AdmissionServer struct {
	client           *kubernetes.Clientset
	pvInfo           *algorithm.CachedPersistentVolumeInfo
	pvcInfo          *algorithm.CachedPersistentVolumeClaimInfo
	systemNamespaces map[string]struct{}
}

// NewAdmissionServer constructs new AdmissionServer
func NewAdmissionServer(client *kubernetes.Clientset, pvLister corelisters.PersistentVolumeLister,
	pvcLister corelisters.PersistentVolumeClaimLister, systemNamespaces []string) *AdmissionServer {
	strMap := make(map[string]struct{}, len(systemNamespaces))
	for _, str := range systemNamespaces {
		strMap[str] = struct{}{}
	}
	return &AdmissionServer{
		client:           client,
		pvInfo:           &algorithm.CachedPersistentVolumeInfo{PersistentVolumeLister: pvLister},
		pvcInfo:          &algorithm.CachedPersistentVolumeClaimInfo{PersistentVolumeClaimLister: pvcLister},
		systemNamespaces: strMap,
	}
}

func toAdmissionResponse(err error, code int32) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
			Code:    code,
		},
	}
}

func allowAdmissionResponse() *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Allowed: true,
	}
}

func isCriticalPod(pod *v1.Pod) bool {
	if pod.Annotations != nil && pod.Annotations["scheduler.alpha.kubernetes.io/critical-pod"] != "" {
		return true
	}
	return false
}

func (s *AdmissionServer) isSystemPod(pod *v1.Pod) bool {
	if _, exist := s.systemNamespaces[pod.Namespace]; exist {
		return true
	}
	return false
}
func (s *AdmissionServer) isPodUseHostPathPV(pod *v1.Pod) (bool, error) {
	for _, podVolume := range pod.Spec.Volumes {
		pv, err := algorithm.GetPodVolumePV(pod, podVolume, s.pvInfo, s.pvcInfo)
		if err != nil {
			return false, fmt.Errorf("get pod %s:%s volume:%v err:%v", pod.Namespace, pod.Name, podVolume, err)
		}
		if pv == nil {
			continue
		}
		if algorithm.IsCommonHostPathPV(pv) {
			glog.Infof("pod %s:%s use hostpathpv %s", pod.Namespace, pod.Name, pv.Name)
			return true, nil
		}
	}
	return false, nil
}

func (s *AdmissionServer) getPodTypeStr(pod *v1.Pod) (string, error) {
	if s.isSystemPod(pod) {
		glog.Infof("pod %s:%s issystempod", pod.Namespace, pod.Name)
		return "systempod", nil
	}
	isCritical := isCriticalPod(pod)
	isHostpathPV, err := s.isPodUseHostPathPV(pod)
	if err != nil {
		return "", err
	}
	glog.Infof("pod %s:%s iscritical:%t, isUseHospathpv:%t", pod.Namespace, pod.Name, isCritical, isHostpathPV)
	switch {
	case isCritical == false && isHostpathPV == false:
		return "default", nil
	case isCritical == true && isHostpathPV == false:
		return "normal-critical", nil
	case isCritical == false && isHostpathPV == true:
		return "hostpathpv", nil
	case isCritical == true && isHostpathPV == true:
		return "hostpathpv-critical", nil
	}
	return "default", nil
}

func (s *AdmissionServer) admit(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request == nil || ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}
	if ar.Request.Operation != v1beta1.Create {
		glog.Errorf("unexpect operation %s", ar.Request.Operation)
		return nil
	}

	pod := v1.Pod{}
	if err := json.Unmarshal(ar.Request.Object.Raw, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}
	if pod.Spec.PriorityClassName != "" {
		glog.Infof("pod %s:%s is set PriorityClassName:%s", ar.Request.Namespace, ar.Request.Name, pod.Spec.PriorityClassName)
		return allowAdmissionResponse()
	}
	clonePod := pod.DeepCopy()
	clonePod.Namespace = ar.Request.Namespace
	clonePod.Name = ar.Request.Name
	typeStr, err := s.getPodTypeStr(clonePod)
	if err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}
	pcName, priority, _ := GetPriorityClassNameByPodType(typeStr)

	pod.Spec.PriorityClassName = pcName
	pod.Spec.Priority = &priority
	if newPodJson, err := json.Marshal(&pod); err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	} else if patch, errPath := createPatch(ar.Request.Object.Raw, newPodJson); errPath != nil {
		return toAdmissionResponse(errPath, http.StatusInternalServerError)
	} else {
		var patchType = v1beta1.PatchTypeJSONPatch
		glog.Infof("change pod %s PriorityClassName %s to %s", pod.Name, pod.Spec.PriorityClassName, pod.Spec.PriorityClassName)
		return &v1beta1.AdmissionResponse{
			Allowed:   true,
			PatchType: &patchType,
			Patch:     patch,
		}
	}
}

// Serve is a handler function of AdmissionServer
func (s *AdmissionServer) Serve(w http.ResponseWriter, r *http.Request) {
	timer := metrics.NewAdmissionLatency()

	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		io.WriteString(w, "UnsupportedMediaType: "+contentType)
		timer.Observe(metrics.Error, metrics.Unknown)
		return
	}

	ar := v1beta1.AdmissionReview{}
	if err := json.Unmarshal(body, &ar); err != nil {
		glog.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "Failed to decode request body err: "+err.Error())
		timer.Observe(metrics.Error, metrics.Unknown)
		return
	}
	reviewResponse := s.admit(ar)
	response := v1beta1.AdmissionReview{
		Response: reviewResponse,
	}
	metrics.OnAdmittedPod(reviewResponse.Allowed)
	resp, err := json.Marshal(response)
	if err != nil {
		glog.Error(err)
		timer.Observe(metrics.Error, metrics.Pod)
		return
	}

	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
		timer.Observe(metrics.Error, metrics.Pod)
		return
	}

	timer.Observe(metrics.Applied, metrics.Pod)
}

func createPatch(oldPod, newPod []byte) ([]byte, error) {
	patchOperations, err := jsonpatch.CreatePatch(oldPod, newPod)
	if err != nil {
		return nil, err
	}
	sort.Sort(jsonpatch.ByPath(patchOperations))
	var b bytes.Buffer
	b.WriteString("[")
	l := len(patchOperations)
	for i, patchOperation := range patchOperations {
		buf, err := patchOperation.MarshalJSON()
		if err != nil {
			return nil, err
		}
		b.Write(buf)
		if i < l-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("]")
	return b.Bytes(), nil
}
