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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"

	"k8s-plugins/admission-controller/pkg/utils/metrics"

	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
)

const (
	NamespaceAllowHostPathAnn  = "io.enndata.namespace/alpha-allowhostpath"
	NamespaceAllowPrivilegeAnn = "io.enndata.namespace/alpha-allowprivilege"
)

type AdmissionServer struct {
	client           *kubernetes.Clientset
	namespacesLister corelisters.NamespaceLister
}

// NewAdmissionServer constructs new AdmissionServer
func NewAdmissionServer(client *kubernetes.Clientset, namespacesLister corelisters.NamespaceLister) *AdmissionServer {
	return &AdmissionServer{client: client, namespacesLister: namespacesLister}
}

func isPodUseHostPath(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath != nil {
			return true
		}
	}
	return false
}

func isPodPrivilge(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged == true {
			return true
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged == true {
			return true
		}
	}
	return false
}
func isNamespaceAllowHostPath(ns *v1.Namespace) bool {
	if ns == nil || ns.Annotations == nil || ns.Annotations[NamespaceAllowHostPathAnn] != "true" {
		return false
	}
	return true
}
func isNamespaceAllowPrivilege(ns *v1.Namespace) bool {
	if ns == nil || ns.Annotations == nil || ns.Annotations[NamespaceAllowPrivilegeAnn] != "true" {
		return false
	}
	return true
}

func (s *AdmissionServer) getNamespace(name string) (*v1.Namespace, error) {
	ns, err := s.namespacesLister.Get(name)
	if err == nil {
		return ns, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	// Could not find in cache, attempt to look up directly
	numAttempts := 3
	retryInterval := time.Duration(rand.Int63n(100)+int64(100)) * time.Millisecond
	for i := 0; i < numAttempts; i++ {
		if i != 0 {
			time.Sleep(retryInterval)
		}
		ns, err := s.client.Core().Namespaces().Get(name, metav1.GetOptions{})
		if err == nil {
			return ns, nil
		}
		if !errors.IsNotFound(err) {
			return nil, err
		}
	}

	return nil, nil
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

func (s *AdmissionServer) admit(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request == nil || ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}
	if ar.Request.Operation != v1beta1.Create && ar.Request.Operation != v1beta1.Update {
		glog.Errorf("unexpect operation %s", ar.Request.Operation)
		return nil
	}

	pod := v1.Pod{}
	if err := json.Unmarshal(ar.Request.Object.Raw, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}

	ns, errGet := s.getNamespace(ar.Request.Namespace)

	useHostPath := isPodUseHostPath(&pod)
	if useHostPath {
		if ns == nil {
			return toAdmissionResponse(fmt.Errorf("pod use hostpath get %s: %v", pod.Namespace, errGet), http.StatusInternalServerError)
		}
		if isNamespaceAllowHostPath(ns) == false {
			return toAdmissionResponse(fmt.Errorf("namespace %s: not support hostpath", pod.Namespace), http.StatusInternalServerError)
		}
	}

	usePrivilege := isPodPrivilge(&pod)
	if usePrivilege {
		if ns == nil {
			return toAdmissionResponse(fmt.Errorf("pod use privilege get %s: %v", pod.Namespace, errGet), http.StatusInternalServerError)
		}
		if isNamespaceAllowPrivilege(ns) == false {
			return toAdmissionResponse(fmt.Errorf("namespace %s: not support privilege", pod.Namespace), http.StatusInternalServerError)
		}
	}
	return allowAdmissionResponse()
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
