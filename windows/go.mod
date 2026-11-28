module fastybrowser-windows

go 1.27.4

require (
	github.com/wailsapp/go-webview2 v1.0.23
	golang.org/x/sys v0.50.0
)

require github.com/jchv/go-winloader v0.0.0-20210711035445-715c2860da7e // indirect

replace github.com/wailsapp/go-webview2 => ./third_party/go-webview2
