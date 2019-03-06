# admission-controller
　
## 说明

[English](README.md) | [中文](README-zh.md)

**k8s的admission-controller支持通过Hook的形式来调用自定义的admission-controller plugin．这个repo主要包括我们实现的几个admission-controller plugin**

## Plugins

* [hppvtocsipv](https://github.com/Rhealb/admission-controller/tree/master/pkg/hppvtocsipv) 创建hostpath PV时将自动升级为CSI hostpath PV.
* [nshostpathprivilege](https://github.com/Rhealb/admission-controller/tree/master/pkg/nshostpathprivilege) 限制Namespace下的Pod使用hostpath和privilege特权模式．
* [podpriority](https://github.com/Rhealb/admission-controller/tree/master/pkg/podpriority) 将创建的Pod分成几个默认的优先等级在集群资源不够的情况下高优先级的Pod优先被调度．
