#!/bin/sh

# sh generate.sh
go build -C cmd/ctxmenu -v
cmd/ctxmenu/ctxmenu -l <<EOF
Terminal
Settings
Applications
	IMG:./icons/web.png		Web Browser		firefox
	IMG:./icons/gimp.png	Image editor	gimp

Reboot
Shutdown
EOF
