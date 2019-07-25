export GO111MODULE:=on
all:
	go build -mod vendor -o exec

clean:
	rm -f exec