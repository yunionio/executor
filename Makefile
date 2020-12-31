export GO111MODULE:=on

ROOT_DIR := $(CURDIR)
BUILD_DIR := $(ROOT_DIR)/_output
BIN_DIR := $(BUILD_DIR)/bin
RPM_SCRIPT := $(ROOT_DIR)/build/build.sh
DEB_SCRIPT := $(ROOT_DIR)/build/build_deb.sh

executor:
	go build -mod vendor -o $(BIN_DIR)/executor

rpm: executor
	$(RPM_SCRIPT) executor

deb: executor
	$(DEB_SCRIPT) executor

all: executor

clean:
	rm -rf $(BIN_DIR)