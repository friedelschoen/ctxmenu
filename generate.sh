set -xe

CMDPATH=../wayland/cmd/gowls
GENARGS="--add-cleanup -p proto --strip-prefix=wl_,xdg_ --strip-except=xdg_surface,wl_surface"

go build -C ${CMDPATH}

${CMDPATH}/gowls -o proto/wayland.go $GENARGS proto/wayland.xml
${CMDPATH}/gowls -o proto/xdg-shell.go $GENARGS proto/xdg-shell.xml
${CMDPATH}/gowls -o proto/wlr-layer-shell-unstable-v1.go $GENARGS --strip-prefix='zwlr_' --strip-suffix='_v1' proto/wlr-layer-shell-unstable-v1.xml