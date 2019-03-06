# nshostpathprivilege admission-controller
　
## 说明

[English](README.md) | [中文](README-zh.md)

**我们可以使用hostpath将Node上的任意目录(包括根目录等重要系统目录)映射到Pod里，同时我们也可以将容器的权限设置为privilege模式从而可以执行更高权限的操作如(mount, 甚至重启机器等). 如上2种权限对于运行该Pod的node来说是非常危险的, nshostpathprivilege admission-controller就是设计用来限制这2种权限使用的，它可以限制只有某些特定Namespace可以使用．**

## 部署
+ **1) 下载代码及编译：**

		$ git clone ssh://git@gitlab.cloud.enndata.cn:10885/kubernetes/k8s-plugins.git
		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make release REGISTRY=10.19.140.200:29006
    
    （make release 将编译代码且制作相应docker image 10.19.140.200:29006/library/nshp-admission-controller:v0.1.0 , 并将其push到registry．也可以只执行make build 生成nshostpathprivilege可执行文件，详情可以查看该目录下的Makefile）
	  
+ **2) 生成证书：**
因为该插件是一个https的服务器所以需要一些相关证书，所以专门提供了一个证书生成的脚本gencerts.sh，执行它将在k8splugin namespace下生成一个secret nshp-tls-certs．

		$ ./gencerts.sh 10.19.137.140 10.19.137.141 10.19.137.142
		$  kubectl -n k8splugin get secret nshp-tls-certs
		NAME             TYPE      DATA      AGE
		nshp-tls-certs   Opaque    4         5m

+ **3) 安装：**
　　该插件是被apiserver所访问的，apiserver支持2中访问方式serverurl和servername, ，具体可以修改**k8s-plugins/admission-controller/deploy/nshp-admission-controller-deployment.yaml**里command启动参数**serverurl或者servername**,如我们可以改为**--servername=nshp-webhook**, 这里的nshp-webhook是我们在k8splugin namespace下创建了一个nshp-webhook svc (或者指定--serverurl=https://ip:port).改完之后可以执行如下命令部署：

		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make install

+ **4) 卸载：**

		$ cd k8s-plugins/admission-controller/pkg/nshostpathprivilege
		$ make uninstall

## 测试
开启和关闭是通过namespace 的annotation来定义的，**"io.enndata.namespace/alpha-allowhostpath"**来控制hostpath的开启和关闭，**"io.enndata.namespace/alpha-allowprivilege:"**来控制privilege的开启和关闭（修改之后只对新创建的pod生效之前已经创建好的pod不受影响）．如下我们以patricktest这个namespace为例hostpath是开启的，privilege是关闭的(你可以通过kubectl edit ns patricktest来修改这2个开关)：

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