package app

import "github.com/digiogithub/pando/internal/nativeui/kit"

// WebViewHost returns the window that hosts embedded web views (the main
// window), or nil before it exists and in headless tests. Use it as
// kit.WebPane.Host:
//
//	pane := &kit.WebPane{Host: a.WebViewHost}
func (a *Context) WebViewHost() kit.WebViewHost {
	if h, ok := a.dispatcher().(kit.WebViewHost); ok {
		return h
	}
	return nil
}
