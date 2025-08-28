set -xe

CMDPATH=../wayland/cmd/gowls
GENARGS="-p proto -P wl_,xdg_ -D xdg_surface,wl_surface"

go build -C ${CMDPATH}

${CMDPATH}/gowls $GENARGS proto/wayland.xml
${CMDPATH}/gowls $GENARGS proto/xdg-shell.xml
${CMDPATH}/gowls $GENARGS -P 'zwlr_' -S '_v1' proto/wlr-layer-shell-unstable-v1.xml
${CMDPATH}/gowls $GENARGS -P 'wp_' -S '_v1' -f zwp_tablet_tool_v2 proto/cursor-shape-v1.xml