# podpriority admission-controller
　
## 说明

[English](README.md) | [中文](README-zh.md)

**根据k8s的调度原理先创建的Pod将被先调度后创建的Pod则后被调度，在集群资源充足的情况下自然问题不大，但是在资源不足时如果一些关键性的服务Pod在后面创建而此时的集群资源不足则会导致这些关键服务无法启动，同时依赖这些关键服务的Pod虽然被调度了当时也无法运行．官方为了解决类似问题提供了一个基于抢占的优先调度策略(详情可以参考kube-scheduler代码)，但是默认这个是不起作用的(即所有Pod默认还是平等的谁先调度谁就优先抢占资源)．**

**本模块的作用就是给创建的Pod默认进行优先级分类，目前默认会给Pod分为如下几个等级：**

+ **1) enndata-podpriority-default:** 默认优先级,优先值为1000.
+ **2) enndata-podpriority-hostpathpv:** 如果Pod使用了hostpath pv则默认会设为该级别，优先值为2000.
+ **3) enndata-podpriority-normal-critical:** 如果Pod是critical的(Pod的annotation包含scheduler.alpha.kubernetes.io/critical-pod=true)则默认会设为该级别，优先值为3000.
+ **4) enndata-podpriority-hostpathpv-critical:** 如果Pod使用了hostpath pv且是critical pod(Pod的annotation包含scheduler.alpha.kubernetes.io/critical-pod=true)则默认会设为该级别，优先值为4000.
+ **5) enndata-podpriority-systempod:** 系统Pod会被设为该级别，可以通过启动参素system-namespaces来制定哪些namespace的所有Pod为该级别.

**之所以会区分是否使用hostpath pv是hostpath pv大部分情况下(使用Keep策略的)是有粘性的Pod在挂了之后，只能回之前node,如果该node资源不够则只能pending,如果这个时候可以将没用该node粘性的Pod感到别的Node将是种不错的选择．而之所以要设置一个systempod级别是一些系统pod可能没有使用hostpath pv等为了不让这些Pod被抢占专门定义了一种最高优先级．（注意：该模只会对那些Pod在创建时没有指定优先级的情况下有作用，如果想不走默认优先级分类可以在创建的时候指定一个优先级在小于5000）**

## 部署
+ **1) 下载代码及编译：**

    	$ git clone ssh://git@gitlab.cloud.enndata.cn:10885/kubernetes/k8s-plugins.git
		$ cd k8s-plugins/admission-controller/pkg/podpriority
		$ make release REGISTRY=10.19.140.200:29006
    
    （make release 将编译代码且制作相应docker image 10.19.140.200:29006/library/podpriority-admission-controller:$TAG , 并将其push到registry．也可以只执行make build 生成podpriority可执行文件，详情可以查看该目录下的Makefile）
	  
+ **2) 生成证书：**
因为该插件是一个https的服务器所以需要一些相关证书，所以专门提供了一个证书生成的脚本gencerts.sh，执行它将在k8splugin namespace下生成一个secret podpriority-tls-certs．

		$ ./gencerts.sh
		$  kubectl -n k8splugin get secret podpriority-tls-certs
		NAME             TYPE      DATA      AGE
		podpriority-tls-certs   Opaque    4         5m

+ **3) 安装：**
　　该插件是被apiserver所访问的，apiserver支持2中访问方式serverurl和servername, ，具体可以修改**k8s-plugins/admission-controller/deploy/hppvr-admission-controller-deployment.yaml**里command启动参数**serverurl或者servername**,如我们可以改为**--servername=hppvr-webhook**, 这里的hppvr-webhook是我们在k8splugin namespace下创建了一个hppvr-webhook svc (或者指定--serverurl=https://ip:port).改完之后可以执行如下命令部署：

		$ cd k8s-plugins/admission-controller/pkg/podpriority
		$ make install REGISTRY=127.0.0.1:29006

+ **4) 卸载：**

		$ cd k8s-plugins/admission-controller/pkg/podpriority
		$ make uninstall

## 测试
+ **1 enndata-podpriority-default测试：**


		$ cd k8s-plugins/admission-controller/pkg/podpriority/test
		$ kubectl create -f default-pod-test.yaml
		$ kubectl -n patricktest get pod | grep pod-priority-default-test
		pod-priority-default-test-c595f7dfd-4nwk9   1/1     Running   0          3m
		$ kubectl -n patricktest get pod pod-priority-default-test-c595f7dfd-4nwk9 -o json | grep -i -E "\"priority"
        "priority": 1000,
        "priorityClassName": "enndata-podpriority-default",

+ **2 enndata-podpriority-hostpathpv测试：**


		$ cd k8s-plugins/admission-controller/pkg/podpriority/test
		$ kubectl create -f hostpathpv-pod-test.yaml 
		$ kubectl -n patricktest get pod | grep pod-priority-hostpath-test
		pod-priority-hostpath-test-75bc98745c-2w5jh   1/1     Running   0          3m
		$ kubectl -n patricktest get pod pod-priority-hostpath-test-75bc98745c-2w5jh -o json | grep -i -E "\"priority"
        "priority": 2000,
        "priorityClassName": "enndata-podpriority-hostpathpv",
        
+ **3 enndata-podpriority-normal-critical测试：**


		$ cd k8s-plugins/admission-controller/pkg/podpriority/test
		$ kubectl create -f critical-pod-test.yaml
		$ kubectl -n patricktest get pod | grep pod-priority-critical-test
		pod-priority-critical-test-68dd94c64d-gl54v   1/1     Running   0          3m
		$ kubectl -n patricktest get pod pod-priority-critical-test-68dd94c64d-gl54v -o json | grep -i -E "\"priority"
        "priority": 3000,
        "priorityClassName": "enndata-podpriority-normal-critical",
        
+ **4 enndata-podpriority-hostpathpv-critical测试：**


		$ cd k8s-plugins/admission-controller/pkg/podpriority/test
		$ kubectl create -f critical-hostpath-pod-test.yaml
		$ kubectl -n patricktest get pod | grep pod-priority-critical-hostpath-test
		pod-priority-critical-hostpath-test-755c7d58f5-k4vjh   1/1     Running   0          3m
		$ kubectl -n patricktest get pod pod-priority-critical-hostpath-test-755c7d58f5-k4vjh -o json | grep -i -E "\"priority"
        "priority": ４000,
        "priorityClassName": "enndata-podpriority-hostpathpv-critical",