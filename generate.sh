set -xe

CMDPATH=../wayland/cmd/go-wl-scanner
GENARGS="--strip-prefix=wl_,xdg_ --strip-except=xdg_surface,wl_surface"


go build -C ${CMDPATH}

${CMDPATH}/go-wl-scanner -p proto -o proto/wayland.go $GENARGS proto/wayland.xml
${CMDPATH}/go-wl-scanner -p proto -o proto/xdg-shell.go $GENARGS proto/xdg-shell.xml
${CMDPATH}/go-wl-scanner -p proto -o proto/wlr-layer-shell-unstable-v1.go $GENARGS --strip-prefix='zwlr_' --strip-suffix='_v1' proto/wlr-layer-shell-unstable-v1.xml