CMDPATH = ../wayland/cmd/go-wl-scanner

PROTOCOLS = proto/wayland.go proto/xdg-shell.go proto/wlr-layer-shell-unstable-v1.go

.PHONY: all clean
all: ${PROTOCOLS}

clean:
	rm -f ${PROTOCOLS}

${CMDPATH}/go-wl-scanner: ${CMDPATH}/*.go
	go build -C ${CMDPATH}

proto/wlr-layer-shell-unstable-v1.go: proto/wlr-layer-shell-unstable-v1.xml ${CMDPATH}/go-wl-scanner
	${CMDPATH}/go-wl-scanner -p proto -o $@ --strip-prefix='zwlr_' --strip-suffix='_v1' $<

proto/%.go: proto/%.xml ${CMDPATH}/go-wl-scanner
	${CMDPATH}/go-wl-scanner -p proto -o $@ $<
