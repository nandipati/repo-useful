INSTALL_DIR = /usr/local/bin
CONFIG_DIR = $(HOME)/.cs

DEPENDENCIES = \
	github.com/hashicorp/consul/api \
	github.com/hashicorp/nomad/api \
	github.com/jet/nomad-service-alerter/logger \
	github.com/spf13/viper \
	github.com/spf13/cobra \
	github.com/hashicorp/nomad/jobspec \
	github.com/hashicorp/vault/api \
	github.com/davecgh/go-spew/spew \
	github.com/hashicorp/go-discover \
	github.com/aws/aws-sdk-go



all: bin

deps:
	go get -u -v $(DEPENDENCIES)

bin: deps
	go build src/cs.go src/consul_ec2_alb.go
	go build src/update_quotas_usage.go

install: bin
	cp cs update_quotas_usage scripts/consul_event_handler.sh \
	scripts/create_cert.sh \
	scripts/upload_cert_to_aws.sh \
	scripts/run_consul_event_handlers.sh $(INSTALL_DIR)
	chmod 755 $(INSTALL_DIR)/cs
	chmod 755 $(INSTALL_DIR)/update_quotas_usage
	chmod 755 $(INSTALL_DIR)/consul_event_handler.sh
	chmod 755 $(INSTALL_DIR)/run_consul_event_handlers.sh
	chmod 755 $(INSTALL_DIR)/create_cert.sh
	chmod 755 $(INSTALL_DIR)/upload_cert_to_aws.sh
	mkdir -p $(CONFIG_DIR)
	cp config/cs.json $(CONFIG_DIR)

format:
	@echo "--> Running go fmt"
	go fmt src/cs.go  src/update_quotas_usage.go

clean:
	rm cs update_quotas_usage

.PHONY: all format deps
