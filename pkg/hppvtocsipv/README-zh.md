# hostpathpvresource admission-controller
　
## 说明

[English](README.md) | [中文](README-zh.md)

**该模块主要用在[enndata-scheduler](https://gitlab.cloud.enndata.cn/kubernetes/k8s-plugins/blob/master/extender-scheduler/README-zh.md) 使用Deployment部署的时候．该模块的主要功能是在Pod创建时判断是否使用了Hostpath PV, 如果是则将Pod的schedulerName设为enndata-scheduler.**

## 部署
+ **1) 下载代码及编译：**

		$ git clone ssh://git@gitlab.cloud.enndata.cn:10885/kubernetes/k8s-plugins.git
		$ cd k8s-plugins/admission-controller/pkg/hppvtocsipv
		$ make release REGISTRY=10.19.140.200:29006
    
    （make release 将编译代码且制作相应docker image 10.19.140.200:29006/library/hppvr-admission-controller:$TAG , 并将其push到registry．也可以只执行make build 生成hostpathpvresource可执行文件，详情可以查看该目录下的Makefile）
	  
+ **2) 生成证书：**
因为该插件是一个https的服务器所以需要一些相关证书，所以专门提供了一个证书生成的脚本gencerts.sh，执行它将在k8splugin namespace下生成一个secret hppvr-tls-certs．

		$ ./gencerts.sh
		$  kubectl -n k8splugin get secret hppvr-tls-certs
		NAME             TYPE      DATA      AGE
		hppvr-tls-certs   Opaque    4         5m

+ **3) 安装：**
　　该插件是被apiserver所访问的，apiserver支持2中访问方式serverurl和servername, ，具体可以修改**k8s-plugins/admission-controller/deploy/hppvr-admission-controller-deployment.yaml**里command启动参数**serverurl或者servername**,如我们可以改为**--servername=hppvr-webhook**, 这里的hppvr-webhook是我们在k8splugin namespace下创建了一个hppvr-webhook svc (或者指定--serverurl=https://ip:port).改完之后可以执行如下命令部署：

		$ cd k8s-plugins/admission-controller/pkg/hppvtocsipv
		$ make install REGISTRY=127.0.0.1:29006

+ **4) 卸载：**

		$ cd k8s-plugins/admission-controller/pkg/hppvtocsipv
		$ make uninstall

## 测试
	$ cat keeptruepv.yaml
	apiVersion: v1
	kind: PersistentVolume
	metadata:
	    annotations:
	        csi.volume.kubernetes.io/volume-attributes: '{"keep":"true","foronepod":"true"}'
	        io.enndata.user/alpha-pvhostpathquotaforonepod: "true"
	        io.enndata.user/alpha-pvhostpathmountpolicy: "keep"
	    name: keeptruepv
	spec:
	    accessModes:
	    - ReadWriteMany
	    capacity:
	        storage: 50Mi 
	    csi:
	        driver: xfshostpathplugin
	        volumeHandle: csi-xfshostpath-patricktest-keeptruepv1
	    persistentVolumeReclaimPolicy: Retain

	$ cat keeptruepvc.yaml
	apiVersion: v1
	kind: PersistentVolumeClaim
	metadata:   
	    name: keeptruepvc
	    namespace: patricktest 
	spec:
	    volumeName: keeptruepv
	    accessModes:
	    - ReadWriteMany
	    resources:
  	         requests:
 	              storage: 50Mi
	
	$ cat pod.yaml
	apiVersion: v1
	kind: Pod
	metadata:
	    name: hostpathpvresourcetest
	    namespace: patricktest
	spec:
	    volumes:
	    - name: hp
	      persistentVolumeClaim:
	          claimName: keeptruepvc
	    containers:
	    - name: test
	      image: busybox
	      volumeMounts:
	       - mountPath: /tmp
	         name: hp 
	      command:
	      - "sleep"
	      - "1000000000"
	      resources:
	         limits:
	             cpu: 200m
	             memory: 256Mi
	         requests:
	              cpu: 200m
	              memory: 256Mi
	$ kubectl create -f keeptruepv.yaml -f keeptruepvc.yaml -f pod.yaml
	$ kubectl get pod hostpathpvresourcetest -o json | grep schedulerName
        "schedulerName": "enndata-scheduler",