export GO111MODULE:=on

ROOT_DIR := $(CURDIR)
BUILD_DIR := $(ROOT_DIR)/_output
BIN_DIR := $(BUILD_DIR)/bin
BUILD_SCRIPT := $(ROOT_DIR)/build/build.sh

executor:
	go build -mod vendor -o $(BIN_DIR)/executor

rpm: executor
	$(BUILD_SCRIPT) executor

all: executor

clean:
	rm -rf $(BIN_DIR)