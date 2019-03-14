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
	"strings"
	"time"

	"github.com/Rhealb/admission-controller/pkg/common"
	"github.com/Rhealb/admission-controller/pkg/utils/metrics"

	"github.com/golang/glog"
	"k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kube_flag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

var (
	certsDir          = flag.String("certs-dir", "/etc/tls-certs", `Where the TLS cert files are stored.`)
	metricAddress     = flag.String("metric-address", ":8001", "The address to expose Prometheus metrics.")
	address           = flag.String("address", ":8000", "The address to expose server.")
	webHookConfigName = flag.String("config-name", "podpriority", "The podpriority web hook config name.")
	serverName        = flag.String("servername", "", "The server name of this controller.")
	serverUrl         = flag.String("serverurl", "", "The server url of this controller.")
	registConfigAuto  = flag.Bool("auto-regist-config", true, "Need regist hook config automatically")
	systemNamespaces  = flag.String("system-namespaces", "k8splugin,kube-system", "system namespaces")
)

type PreCreatePriorityClass struct {
	Name          string
	Value         int32
	GlobalDefault bool
	Description   string
}

var (
	PreCreatePriorityClassList = []PreCreatePriorityClass{
		{
			Name:          "enndata-podpriority-default",
			Value:         1000,
			GlobalDefault: false,
			Description:   "default PriorityClass",
		},
		{
			Name:          "enndata-podpriority-hostpathpv",
			Value:         2000,
			GlobalDefault: false,
			Description:   "use hostpath pv but is not critical pod",
		},
		{
			Name:          "enndata-podpriority-normal-critical",
			Value:         3000,
			GlobalDefault: false,
			Description:   "not use hostpath pv critical pod (scheduler.alpha.kubernetes.io/critical-pod=true)",
		},
		{
			Name:          "enndata-podpriority-hostpathpv-critical",
			Value:         4000,
			GlobalDefault: false,
			Description:   "use hostpath pv and is critical pod(scheduler.alpha.kubernetes.io/critical-pod=true)",
		},
		{
			Name:          "enndata-podpriority-systempod",
			Value:         5000,
			GlobalDefault: false,
			Description:   "systempod",
		},
	}
)

func GetPriorityClassNameByPodType(t string) (string, int32, error) {
	switch t {
	case "default":
		return PreCreatePriorityClassList[0].Name, PreCreatePriorityClassList[0].Value, nil
	case "hostpathpv":
		return PreCreatePriorityClassList[1].Name, PreCreatePriorityClassList[1].Value, nil
	case "normal-critical":
		return PreCreatePriorityClassList[2].Name, PreCreatePriorityClassList[2].Value, nil
	case "hostpathpv-critical":
		return PreCreatePriorityClassList[3].Name, PreCreatePriorityClassList[3].Value, nil
	case "systempod":
		return PreCreatePriorityClassList[4].Name, PreCreatePriorityClassList[4].Value, nil
	default:
		return PreCreatePriorityClassList[0].Name, PreCreatePriorityClassList[0].Value, fmt.Errorf("unknow type %s", t)
	}
}

func createPriorityClass(clientset *kubernetes.Clientset) error {
	for _, p := range PreCreatePriorityClassList {
		createPC := &v1beta1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        p.Name,
				Annotations: map[string]string{"description": p.Description},
			},
			Value:         p.Value,
			GlobalDefault: p.GlobalDefault,
		}
		_, err := clientset.Scheduling().PriorityClasses().Get(p.Name, metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("get PriorityClass %s err:%v", p.Name, err)
			} else {
				if _, errCreate := clientset.Scheduling().PriorityClasses().Create(createPC); errCreate != nil {
					return fmt.Errorf("create PriorityClass %s err:%v", p.Name, errCreate)
				}
			}
		} else {
			if _, errUpdate := clientset.Scheduling().PriorityClasses().Update(createPC); errUpdate != nil {
				return fmt.Errorf("create PriorityClass %s err:%v", p.Name, errUpdate)
			}
		}
	}
	return nil
}

func main() {
	kube_flag.InitFlags()

	glog.V(1).Infof("hppvtocsipv %s Admission Controller", common.PodPriorityVersion)

	healthCheck := metrics.NewHealthCheck(time.Minute, false)
	metrics.Initialize(*metricAddress, healthCheck)
	metrics.Register()

	certs := common.InitCerts(*certsDir)
	clientset := common.GetClient()

	if err := createPriorityClass(clientset); err != nil {
		glog.Fatalf("createPriorityClass fail:%v", err)
	}
	sharedInformers := informers.NewSharedInformerFactory(clientset, 0)
	stopEverything := make(chan struct{})
	pvInformer := sharedInformers.Core().V1().PersistentVolumes()
	pvcInformer := sharedInformers.Core().V1().PersistentVolumeClaims()
	pvSynced := pvInformer.Informer().HasSynced
	pvcSynced := pvcInformer.Informer().HasSynced

	as := NewAdmissionServer(clientset, pvInformer.Lister(), pvcInformer.Lister(), strings.Split(*systemNamespaces, ","))
	sharedInformers.Start(stopEverything)
	if !cache.WaitForCacheSync(wait.NeverStop, pvSynced, pvcSynced) {
		glog.Fatalf("timed out waiting for pv or pvc caches to sync")
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
		go common.SelfPodPriorityWebHookRegistration(clientset, *webHookConfigName, *serverName, *serverUrl, certs.CaCert)
	}
	glog.Infof("start httpserver")
	server.ListenAndServeTLS("", "")
}
