all: build

TAG?=v1.0.0
REGISTRY?=ihub.helium.io:29006
FLAGS=
ENVVAR=
GOOS?=linux
ROOTPATH=`cd ../../; pwd` 
BUILDGOPATH=/tmp/k8splugin-build
BUILDPATH=$(BUILDGOPATH)/src/github.com/Rhealb/admission-controller/pkg/nshostpathprivilege
IMAGENAME=${REGISTRY}/library/nshp-admission-controller:${TAG}
MASTERS?="127.0.0.1"
BINMOVEPATH="/opt/bin"
SVCMOVEPATH="/etc/systemd/system/"
MASTERUSER?=root

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
	@cd $(BUILDPATH) && GOPATH=$(BUILDGOPATH) $(ENVVAR) GOOS=$(GOOS) CGO_ENABLED=0   godep go build -o nshostpathprivilege

docker:
ifndef REGISTRY
	ERR = $(error REGISTRY is undefined)
	$(ERR)
endif
	docker build --pull -t ${REGISTRY}/library/nshp-admission-controller:${TAG} .
	docker push ${REGISTRY}/library/nshp-admission-controller:${TAG}

deletedeploy:
	kubectl delete -f ../../deploy/nshp-admission-controller-deployment.yaml 1>/dev/null 2>/dev/null || true

deletehookconfig:
	kubectl delete ValidatingWebhookConfiguration  nshostpathprivilege

install: deletehookconfig deletedeploy
	@./gencerts.sh
	@cat ../../deploy/nshp-admission-controller-deployment.yaml | sed "s!{hppvtocsipv-image}!${IMAGENAME}!g" > ../../deploy/tmp.yaml
	@kubectl label ns k8splugin enndata.cn/ignore-admission-controller-webhook=true --overwrite=true
	kubectl create -f ../../deploy/tmp.yaml
	@rm ../../deploy/tmp.yaml
	
systemd: build
	@rm -rf tls-certs
	./gencerts.sh false localhost
	./systemd.sh $(BINMOVEPATH) $(SVCMOVEPATH) $(MASTERUSER) $(MASTERS)
	@rm nshostpathprivilege
	@rm -rf tls-certs
	

uninstall: deletedeploy deletehookconfig

release: build docker
	rm -f nshostpathprivilege

clean: buildEnvClean
	@rm -f nshostpathprivilege

format:
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -d {} + | tee /dev/stderr)" || \
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -w {} + | tee /dev/stderr)"

.PHONY: all deps build test-unit clean format release

