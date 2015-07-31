OUT_DIR = _output
OUT_PKG_DIR = Godeps/_workspace/pkg

export GOFLAGS

all build:
	hack/build.sh $(WHAT)
.PHONY: all build

install:
	cp -f $(OUT_DIR)/local/go/bin/cni-docker-plugin /usr/bin/

clean:
	rm -rf $(OUT_DIR) $(OUT_PKG_DIR)
.PHONY: clean

