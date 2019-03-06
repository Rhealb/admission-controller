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

package common

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/golang/glog"
	"k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	nshpWebhookConfigName = "nshp-webhook-config"
)

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func GetClientByConfig(kubeconfig string) (*kubernetes.Clientset, error) {
	config, errConfig := buildConfig(kubeconfig)
	if errConfig != nil {
		return nil, errConfig
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// get a clientset with in-cluster config.
func GetClient() *kubernetes.Clientset {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	return clientset
}

// retrieve the CA cert that will signed the cert used by the
// "GenericAdmissionWebhook" plugin admission controller.
func getAPIServerCert(clientset *kubernetes.Clientset) []byte {
	c, err := clientset.CoreV1().ConfigMaps("kube-system").Get("extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		glog.Fatal(err)
	}

	pem, ok := c.Data["client-ca-file"]
	if !ok {
		glog.Fatalf(fmt.Sprintf("cannot find the ca.crt in the configmap, configMap.Data is %#v", c.Data))
	}
	glog.V(4).Info("client-ca-file=", pem)
	return []byte(pem)
}

func ConfigTLS(clientset *kubernetes.Clientset, serverCert, serverKey []byte) *tls.Config {
	cert := getAPIServerCert(clientset)
	apiserverCA := x509.NewCertPool()
	apiserverCA.AppendCertsFromPEM(cert)

	sCert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		glog.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{sCert},
		ClientCAs:    apiserverCA,
		// Consider changing to tls.RequireAndVerifyClientCert.
		ClientAuth: tls.NoClientCert,
	}
}

// register this webhook admission controller with the kube-apiserver
// by creating ValidatingWebhookConfiguration.
func SelfPodValidatingWebHookRegistration(clientset *kubernetes.Clientset, configName, serverName, serverUrl string, caCert []byte) {
	client := clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	_, err := client.Get(configName, metav1.GetOptions{})
	if err == nil {
		if err2 := client.Delete(configName, nil); err2 != nil {
			glog.Fatal(err2)
		}
	}
	config := v1beta1.WebhookClientConfig{
		CABundle: caCert,
	}
	if serverUrl != "" {
		config.URL = &serverUrl
	} else {
		config.Service = &v1beta1.ServiceReference{
			Namespace: AdmissionControllerNS,
			Name:      serverName,
		}
	}

	var ft v1beta1.FailurePolicyType = v1beta1.Fail
	webhookConfig := &v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
		},
		Webhooks: []v1beta1.Webhook{
			{
				Name: "nshp.enndata.cn",
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						metav1.LabelSelectorRequirement{
							Key:      "enndata.cn/ignore-admission-controller-webhook",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"true"},
						},
					},
				},
				FailurePolicy: &ft,
				Rules: []v1beta1.RuleWithOperations{
					{
						Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
						Rule: v1beta1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					}},
				ClientConfig: config,
			},
		},
	}
	if _, err := client.Create(webhookConfig); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof("Self registration as ValidatingWebhook %s succeeded.", configName)
	}

}

// register this webhook admission controller with the kube-apiserver
// by creating MutatingWebhookConfiguration.
func SelfPodMutatingWebHookRegistration(clientset *kubernetes.Clientset, configName, serverName, serverUrl string, caCert []byte) {
	client := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	_, err := client.Get(configName, metav1.GetOptions{})
	if err == nil {
		if err2 := client.Delete(configName, nil); err2 != nil {
			glog.Fatal(err2)
		}
	}
	config := v1beta1.WebhookClientConfig{
		CABundle: caCert,
	}
	if serverUrl != "" {
		config.URL = &serverUrl
	} else {
		config.Service = &v1beta1.ServiceReference{
			Namespace: AdmissionControllerNS,
			Name:      serverName,
		}
	}

	var ft v1beta1.FailurePolicyType = v1beta1.Fail
	webhookConfig := &v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
		},
		Webhooks: []v1beta1.Webhook{
			{
				Name: "hppvr.enndata.cn",
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						metav1.LabelSelectorRequirement{
							Key:      "enndata.cn/ignore-admission-controller-webhook",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"true"},
						},
					},
				},
				FailurePolicy: &ft,
				Rules: []v1beta1.RuleWithOperations{
					{
						Operations: []v1beta1.OperationType{v1beta1.Create},
						Rule: v1beta1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					}},
				ClientConfig: config,
			},
		},
	}
	if _, err := client.Create(webhookConfig); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof("Self registration as MutatingWebhook %s succeeded.", configName)
	}

}

// register this webhook admission controller with the kube-apiserver
// by creating MutatingWebhookConfiguration.
func SelfPVMutatingWebHookRegistration(clientset *kubernetes.Clientset, configName, serverName, serverUrl string, caCert []byte) {
	client := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	_, err := client.Get(configName, metav1.GetOptions{})
	if err == nil {
		if err2 := client.Delete(configName, nil); err2 != nil {
			glog.Fatal(err2)
		}
	}
	config := v1beta1.WebhookClientConfig{
		CABundle: caCert,
	}
	if serverUrl != "" {
		config.URL = &serverUrl
	} else {
		config.Service = &v1beta1.ServiceReference{
			Namespace: AdmissionControllerNS,
			Name:      serverName,
		}
	}

	var ft v1beta1.FailurePolicyType = v1beta1.Ignore
	webhookConfig := &v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
		},
		Webhooks: []v1beta1.Webhook{
			{
				Name: "hppvtocsipv.enndata.cn",
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						metav1.LabelSelectorRequirement{
							Key:      "enndata.cn/ignore-admission-controller-webhook",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"true"},
						},
					},
				},
				FailurePolicy: &ft,
				Rules: []v1beta1.RuleWithOperations{
					{
						Operations: []v1beta1.OperationType{v1beta1.Create},
						Rule: v1beta1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"persistentvolumes"},
						},
					}},
				ClientConfig: config,
			},
		},
	}
	if _, err := client.Create(webhookConfig); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof("Self registration as MutatingWebhook %s succeeded.", configName)
	}
}

// register this webhook admission controller with the kube-apiserver
// by creating MutatingWebhookConfiguration.
func SelfPodPriorityWebHookRegistration(clientset *kubernetes.Clientset, configName, serverName, serverUrl string, caCert []byte) {
	client := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	_, err := client.Get(configName, metav1.GetOptions{})
	if err == nil {
		if err2 := client.Delete(configName, nil); err2 != nil {
			glog.Fatal(err2)
		}
	}
	config := v1beta1.WebhookClientConfig{
		CABundle: caCert,
	}
	if serverUrl != "" {
		config.URL = &serverUrl
	} else {
		config.Service = &v1beta1.ServiceReference{
			Namespace: AdmissionControllerNS,
			Name:      serverName,
		}
	}

	var ft v1beta1.FailurePolicyType = v1beta1.Ignore
	webhookConfig := &v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
		},
		Webhooks: []v1beta1.Webhook{
			{
				Name:          "podpriority.enndata.cn",
				FailurePolicy: &ft,
				Rules: []v1beta1.RuleWithOperations{
					{
						Operations: []v1beta1.OperationType{v1beta1.Create},
						Rule: v1beta1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					}},
				ClientConfig: config,
			},
		},
	}
	if _, err := client.Create(webhookConfig); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof("Self registration as MutatingWebhook %s succeeded.", configName)
	}
}
