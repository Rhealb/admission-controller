# admission-controller
　
## Explanation

[English](README.md) | [中文](README-zh.md)

**K8s admission-controller supports calling custom admission-controller plugin in the form of Hook．This repo includes several admission-controller plugins we implemented**

## Plugins

* [hppvtocsipv](https://github.com/Rhealb/admission-controller/tree/master/pkg/hppvtocsipv) Upgrade to CSI hostpath PV automatically when creating hostpath PV.
* [nshostpathprivilege](https://github.com/Rhealb/admission-controller/tree/master/pkg/nshostpathprivilege) Restrict Pod under Namespace to use hostpath and privilege modes．
* [podpriority](https://github.com/Rhealb/admission-controller/tree/master/pkg/podpriority) Divide the created Pods into default priority levels. High priority Pods are scheduled when cluster resources are insufficient.．
