# nshostpathprivilege admission-controller
　
## Explanation

[English](README.md) | [中文](README-zh.md)

**We can use hostpath to map any directory on Node (including important system directories such as root directories) to Pod. And we can also set the privilege mode of the container to perform higher privilege operations such as (mount, even restart the machine). The above hostpath and privilege are very dangerous for the node running the Pod. The nshostpath privilege admission-controller is designed to limit the use of these two permissions, which can limit the use of only certain specific Namespace.**

## deploy

+ **1) Download code and compile：**

		$ git clone ssh://git@gitlab.cloud.enndata.cn:10885/kubernetes/k8s-plugins.git
		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make release REGISTRY=10.19.140.200:29006

	  （'make release' will compile and build the docker image 10.19.140.200:29006/library/nshp-admission-controller:v0.1.0, then push the image to our registry． You can also only execute 'make build' to generate the nshostpathprivilege executable file. For more information, see Makefile in this directory）
	  
+ **2) Generating certificate：**

Because the plug-in is an HTTPS server and needs some certificates, a certificate generation script gencerts.sh is provided, which will generate a secret nshp-tls-certs under k8 splugin namespace．

		$ ./gencerts.sh 10.19.137.140 10.19.137.141 10.19.137.142
		$  kubectl -n k8splugin get secret nshp-tls-certs
		NAME             TYPE      DATA      AGE
		nshp-tls-certs   Opaque    4         5m

+ **3) Install：**

The plug-in is accessed by apiserver, apiserver supports access mode serverurl and servername. You can modify the command startup parameter **serverurl or servername** in **k8s-plugins/admission-controller/deploy/nshp-admission-controller-deployment.yaml**, such as **--servername=nshp-webhook**. After edit nshp-admission-controller-deployment.yaml, we can deploy the following commands

		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make install

+ **4) Uninstall：**

		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make uninstall

## Testing
Opening and closing the hostpath or privilge are defined by annotation of namespace, **"io.enndata.namespace/alpha-allowhostpath"** controls the opening and closing of hostpath, **"io.enndata.namespace/alpha-allowprivilege:"** controls the opening and closing of privilege(only the new create pod takes effect). Take the patricktest namespace as an example, the hostpath is on and privilege is off(you can modify these two switches by using **'kubectl edits ns patricktest'**):

		$ kubectl get ns patricktest -o yaml
		apiVersion: v1
		kind: Namespace
		  metadata:
		    annotations:
			   io.enndata.namespace/alpha-allowhostpath: "true"
			   io.enndata.namespace/alpha-allowprivilege: "false"
		    creationTimestamp: 2017-04-24T01:57:36Z
			labels:
			   X_NAMESPACE: patricktest
			name: patricktest
			resourceVersion: "2495944020"
			selfLink: /api/v1/namespaces/patricktest
			uid: 63e662c1-2891-11e7-a09b-1866da19caf3
		spec:
		   finalizers:
		   - kubernetes
		status:
		   phase: Active
		
		$ cd k8s-plugins/admission-controller/test
		$ kubectl create -f hostpathpodtest.yaml
		pod/hostpathpodtest created
		$ kubectl create -f privilegepodtest.yaml
		Error from server: error when creating "privilegepodtest.yaml": admission webhook "nshp.enndata.cn" denied the request: namespace patricktest: not support privilege