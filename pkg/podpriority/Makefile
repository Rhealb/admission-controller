all: build

TAG?=v1.0.0
REGISTRY?=ihub.helium.io:29006
FLAGS=
ENVVAR=
GOOS?=linux
ROOTPATH=`cd ../../; pwd` 
BUILDGOPATH=/tmp/k8splugin-build
BUILDPATH=$(BUILDGOPATH)/src/github.com/Rhealb/admission-controller/pkg/podpriority
IMAGENAME=${REGISTRY}/library/podpriority-admission-controller:${TAG}

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
	@cd $(BUILDPATH) && GOPATH=$(BUILDGOPATH) $(ENVVAR) GOOS=$(GOOS) CGO_ENABLED=0   godep go build -o podpriority

docker:
ifndef REGISTRY
	ERR = $(error REGISTRY is undefined)
	$(ERR)
endif
	docker build --pull -t ${REGISTRY}/library/podpriority-admission-controller:${TAG} .
	docker push ${REGISTRY}/library/podpriority-admission-controller:${TAG}

deletedeploy:
	@kubectl delete -f ../../deploy/podpriority-admission-controller-deployment.yaml 1>/dev/null 2>/dev/null || true

deletehookconfig:
	@kubectl delete MutatingWebhookConfiguration  podpriority 1>/dev/null 2>/dev/null || true

install: deletehookconfig deletedeploy
	./gencerts.sh
	@cat ../../deploy/podpriority-admission-controller-deployment.yaml | sed "s!{image}!${IMAGENAME}!g" > ../../deploy/tmp.yaml
	kubectl create -f ../../deploy/tmp.yaml
	@rm ../../deploy/tmp.yaml

uninstall: deletedeploy deletehookconfig

release: build docker
	rm -f podpriority

clean: buildEnvClean
	@rm -f podpriority

format:
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -d {} + | tee /dev/stderr)" || \
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -w {} + | tee /dev/stderr)"

.PHONY: all deps build test-unit clean format release

