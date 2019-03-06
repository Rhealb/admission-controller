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
	"k8s-plugins/admission-controller/pkg/utils/metrics"
	"net/http"
	"os"
	"time"

	"k8s-plugins/admission-controller/pkg/common"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"
	kube_flag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var (
	certsDir          = flag.String("certs-dir", "/etc/tls-certs", `Where the TLS cert files are stored.`)
	metricAddress     = flag.String("metric-address", ":8001", "The address to expose Prometheus metrics.")
	address           = flag.String("address", ":8000", "The address to expose server.")
	webHookConfigName = flag.String("config-name", "nshostpathprivilege", "The nshostpathprivilege web hook config name.")
	serverName        = flag.String("servername", "", "The server name of this controller.")
	serverUrl         = flag.String("serverurl", "", "The server url of this controller.")
	registConfigAuto  = flag.Bool("auto-regist-config", true, "Need regist hook config automatically")
	kubeConfig        = flag.String("kubeconfig", "", "kube config file path")
)

func main() {
	kube_flag.InitFlags()

	glog.V(1).Infof("Namespaces Pod hostpath privilege %s Admission Controller", common.NSHostpathPrivilegeVersion)

	healthCheck := metrics.NewHealthCheck(time.Minute, false)
	metrics.Initialize(*metricAddress, healthCheck)
	metrics.Register()

	certs := common.InitCerts(*certsDir)
	clientset, err := common.GetClientByConfig(*kubeConfig)
	if err != nil {
		glog.Errorf("get kube client err:%v", err)
		os.Exit(1)
	}
	sharedInformers := informers.NewSharedInformerFactory(clientset, 0)
	stopEverything := make(chan struct{})

	nsInformer := sharedInformers.Core().V1().Namespaces()
	nsSynced := nsInformer.Informer().HasSynced
	as := NewAdmissionServer(clientset, nsInformer.Lister())
	sharedInformers.Start(stopEverything)
	if !cache.WaitForCacheSync(wait.NeverStop, nsSynced) {
		fmt.Errorf("timed out waiting for namespace caches to sync")
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
		go common.SelfPodValidatingWebHookRegistration(clientset, *webHookConfigName, *serverName, *serverUrl, certs.CaCert)
	}

	server.ListenAndServeTLS("", "")
}
