all: build

TAG?=v1.0.0
REGISTRY?=ihub.helium.io:29006
FLAGS=
ENVVAR=
GOOS?=linux
ROOTPATH=`cd ../../; pwd` 
BUILDGOPATH=/tmp/k8splugin-build
IMAGENAME=${REGISTRY}/library/hppvr-admission-controller:${TAG}
BUILDPATH=$(BUILDGOPATH)/src/github.com/Rhealb/admission-controller/pkg/hostpathpvresource

.IGNORE : deletehookconfig
.IGNORE : deletedeploy
.IGNORE : buildEnvClean

deps:
	@go get github.com/tools/godep
	
buildEnvClean:
	@rm -rf $(BUILDGOPATH) 1>/dev/null 2>/dev/null || true

buildEnv: buildEnvClean
	@mkdir -p $(BUILDGOPATH)/src/github.com/Rhealb/ 1>/dev/null 2>/dev/null
	@ln -s $(ROOTPATH) $(BUILDGOPATH)/src/github.com/Rhealb/admission-controller
	
build: buildEnv clean deps 
	@cd $(BUILDPATH) && GOPATH=$(BUILDGOPATH) $(ENVVAR) GOOS=$(GOOS) CGO_ENABLED=0   godep go build ./...
	@cd $(BUILDPATH) && GOPATH=$(BUILDGOPATH) $(ENVVAR) GOOS=$(GOOS) CGO_ENABLED=0   godep go build -o hostpathpvresource

docker:
ifndef REGISTRY
	ERR = $(error REGISTRY is undefined)
	$(ERR)
endif
	docker build --pull -t ${REGISTRY}/library/hppvr-admission-controller:${TAG} .
	docker push ${REGISTRY}/library/hppvr-admission-controller:${TAG}

deletedeploy:
	@kubectl delete -f ../../deploy/hppvr-admission-controller-deployment.yaml 1>/dev/null 2>/dev/null || true

deletehookconfig:
	@kubectl delete MutatingWebhookConfiguration  hostpathpvresource 1>/dev/null 2>/dev/null || true

install: deletehookconfig deletedeploy
	./gencerts.sh
	@cat ../../deploy/hppvr-admission-controller-deployment.yaml | sed "s!{image}!${IMAGENAME}!g" > ../../deploy/tmp.yaml
	@kubectl label ns k8splugin enndata.cn/ignore-admission-controller-webhook=true --overwrite=true
	kubectl create -f ../../deploy/tmp.yaml
	@rm ../../deploy/tmp.yaml

uninstall: deletedeploy deletehookconfig

release: build docker
	rm -f nshostpathprivilege

clean: buildEnvClean
	@rm -f hostpathpvresource

format:
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -d {} + | tee /dev/stderr)" || \
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -w {} + | tee /dev/stderr)"

.PHONY: all deps build test-unit clean format release

