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
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"sort"

	"github.com/Rhealb/admission-controller/pkg/utils/metrics"

	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	hostPathPVResourceName    = "enndata.cn/hostpathpv"
	hostPathPVShouldBeIgnored = "enndata.cn/hostpathpv-to-csi-ignored" // this is use for updatepipeline debug
)

type AdmissionServer struct {
	client     *kubernetes.Clientset
	driverName string
}

//生成32位md5字串
func GetMd5String(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

//生成Guid字串
func GetGuid(raw []byte) string {
	return GetMd5String(base64.URLEncoding.EncodeToString(raw))
}

// NewAdmissionServer constructs new AdmissionServer
func NewAdmissionServer(client *kubernetes.Clientset, driverName string) *AdmissionServer {
	return &AdmissionServer{
		client:     client,
		driverName: driverName,
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

func isHostpathPV(pv *v1.PersistentVolume) (bool, error) {
	if pv.Spec.HostPath != nil {
		return true, nil
	}
	return false, nil
}

func isPVShouldBeIgnored(pv *v1.PersistentVolume) bool {
	if pv != nil && pv.Annotations != nil && pv.Annotations[hostPathPVShouldBeIgnored] == "true" {
		return true
	}
	return false
}

func changeHostpathPVToCSIPV(pv *v1.PersistentVolume, driverName, uid string) {
	pv.Spec.HostPath = nil
	pv.Spec.CSI = &v1.CSIPersistentVolumeSource{
		Driver:       driverName,
		VolumeHandle: uid,
	}
}

func allowAdmissionResponse() *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Allowed: true,
	}
}

func (s *AdmissionServer) admit(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	pvResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"}
	if ar.Request == nil || ar.Request.Resource != pvResource {
		glog.Errorf("expect resource to be %s", pvResource)
		return nil
	}
	if ar.Request.Operation != v1beta1.Create {
		glog.Errorf("unexpect operation %s", ar.Request.Operation)
		return nil
	}

	pv := v1.PersistentVolume{}
	if err := json.Unmarshal(ar.Request.Object.Raw, &pv); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}
	uid := GetGuid(ar.Request.Object.Raw)
	newPV := pv.DeepCopy()

	if ok, err := isHostpathPV(newPV); err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	} else if ok == false {
		return allowAdmissionResponse()
	}

	if isPVShouldBeIgnored(newPV) == true {
		glog.Infof("hostpathpv %s is ignored", newPV.Name)
		return allowAdmissionResponse()
	}

	changeHostpathPVToCSIPV(newPV, s.driverName, uid)

	if newPVJson, err := json.Marshal(&newPV); err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	} else if patch, errPath := createPatch(ar.Request.Object.Raw, newPVJson); errPath != nil {
		return toAdmissionResponse(errPath, http.StatusInternalServerError)
	} else {
		var patchType = v1beta1.PatchTypeJSONPatch
		glog.Infof("change hostpath pv %s to csi pv", newPV.Name)
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
