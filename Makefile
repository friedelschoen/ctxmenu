CMDPATH = ../wayland/cmd/go-wl-scanner

PROTOCOLS = proto/wayland.go proto/xdg-shell.go

.PHONY: all clean
all: ${PROTOCOLS}

clean:
	rm -f ${PROTOCOLS}

${CMDPATH}/go-wl-scanner: ${CMDPATH}/*.go
	go build -C ${CMDPATH}

proto/%.go: proto/%.xml ${CMDPATH}/go-wl-scanner
	${CMDPATH}/go-wl-scanner -p proto -o $@ $<
