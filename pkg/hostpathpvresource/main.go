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
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Rhealb/admission-controller/pkg/utils/metrics"

	"github.com/Rhealb/admission-controller/pkg/common"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"
	kube_flag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var (
	certsDir            = flag.String("certs-dir", "/etc/tls-certs", `Where the TLS cert files are stored.`)
	metricAddress       = flag.String("metric-address", ":8001", "The address to expose Prometheus metrics.")
	address             = flag.String("address", ":8000", "The address to expose server.")
	webHookConfigName   = flag.String("config-name", "hostpathpvresource", "The hostpathpvresource web hook config name.")
	serverName          = flag.String("servername", "", "The server name of this controller.")
	serverUrl           = flag.String("serverurl", "", "The server url of this controller.")
	registConfigAuto    = flag.Bool("auto-regist-config", true, "Need regist hook config automatically")
	hostpathPVScheduler = flag.String("scheduler-name", "enndata-scheduler", "The hostpathpv pods' scheduler")
)

func main() {
	kube_flag.InitFlags()

	glog.V(1).Infof("Namespaces Pod hostpathpvresource %s Admission Controller", common.HostPathPVResourceVersion)

	healthCheck := metrics.NewHealthCheck(time.Minute, false)
	metrics.Initialize(*metricAddress, healthCheck)
	metrics.Register()

	certs := common.InitCerts(*certsDir)
	clientset := common.GetClient()
	sharedInformers := informers.NewSharedInformerFactory(clientset, 0)
	stopEverything := make(chan struct{})

	pvInformer := sharedInformers.Core().V1().PersistentVolumes()
	pvcInformer := sharedInformers.Core().V1().PersistentVolumeClaims()
	pvSynced := pvInformer.Informer().HasSynced
	pvcSynced := pvcInformer.Informer().HasSynced
	as := NewAdmissionServer(clientset, pvInformer.Lister(), pvcInformer.Lister(), *hostpathPVScheduler)
	sharedInformers.Start(stopEverything)
	if !cache.WaitForCacheSync(wait.NeverStop, pvSynced, pvcSynced) {
		fmt.Errorf("timed out waiting for pv or pvc caches to sync")
		os.Exit(-1)
	}
	var sm http.ServeMux
	sm.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		as.Serve(w, r)
		healthCheck.UpdateLastActivity()
	})
	server := &http.Server{
		Addr:      *address,
		TLSConfig: common.ConfigTLS(clientset, certs.ServerCert, certs.ServerKey),
		Handler:   &sm,
	}
	if *registConfigAuto {
		if *serverName == "" && *serverUrl == "" {
			glog.Fatalf("servername and serverurl are all empty")
		}
		go common.SelfPodMutatingWebHookRegistration(clientset, *webHookConfigName, *serverName, *serverUrl, certs.CaCert)
	}
	glog.Infof("start httpserver")
	server.ListenAndServeTLS("", "")
}
