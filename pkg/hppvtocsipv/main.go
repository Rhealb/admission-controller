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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rhealb/admission-controller/pkg/utils/metrics"

	"github.com/Rhealb/admission-controller/pkg/common"

	"github.com/golang/glog"
	kube_flag "k8s.io/apiserver/pkg/util/flag"
)

var (
	certsDir            = flag.String("certs-dir", "/etc/tls-certs", `Where the TLS cert files are stored.`)
	metricAddress       = flag.String("metric-address", ":8001", "The address to expose Prometheus metrics.")
	address             = flag.String("address", ":8000", "The address to expose server.")
	webHookConfigName   = flag.String("config-name", "hppvtocsipv", "The hppvtocsipv web hook config name.")
	serverName          = flag.String("servername", "", "The server name of this controller.")
	serverUrl           = flag.String("serverurl", "", "The server url of this controller.")
	registConfigAuto    = flag.Bool("auto-regist-config", true, "Need regist hook config automatically")
	csiDriverName       = flag.String("csi-driver-name", "xfshostpathplugin", "The csi hostpathpv driver name.")
	updateOldHostpathPV = flag.Bool("update-hostpathpv-csi", false, "The update these hostpathpv to csi hostpathpv.")
	updatePVInterVal    = flag.Duration("update-hostpathpv-csi-interval", 1*time.Hour, "update intervals between two hostpathpv")
	upgradeImage        = flag.String("upgradeimage", "127.0.0.1:29006/library/busybox:1.25", "Image create to change quota dir type")
)

func main() {
	kube_flag.InitFlags()

	glog.V(1).Infof("hppvtocsipv %s Admission Controller", common.HostPathPVToCSIPVVersion)

	healthCheck := metrics.NewHealthCheck(time.Minute, false)
	metrics.Initialize(*metricAddress, healthCheck)
	metrics.Register()

	certs := common.InitCerts(*certsDir)
	clientset := common.GetClient()

	as := NewAdmissionServer(clientset, *csiDriverName)

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
		go common.SelfPVMutatingWebHookRegistration(clientset, *webHookConfigName, *serverName, *serverUrl, certs.CaCert)
	}
	if *updateOldHostpathPV == true {
		glog.Infof("NewPVUpdateManager updatePVInterVal:%v", *updatePVInterVal)
		updateManager := NewPVUpdateManager(clientset, *updatePVInterVal, *upgradeImage)
		updateManager.Start()

		signalChan := make(chan os.Signal)
		go func() {
			select {
			case <-signalChan:
				updateManager.Stop()
				break
			}
		}()
		signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	}
	glog.Infof("start httpserver")
	server.ListenAndServeTLS("", "")
}
