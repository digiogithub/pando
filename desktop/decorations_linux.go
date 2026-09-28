//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#ifdef GDK_WINDOWING_WAYLAND
#include <gdk/gdkwayland.h>
#endif

// On Wayland, GTK3 announces server-side decorations for an undecorated
// window through the KDE server-decoration protocol, so compositors that
// honour it (KDE, COSMIC) draw their own title bar over a frameless window.
// Announcing client-side decorations instead tells them the app draws its own.
static gboolean pando_announce_csd_idle(gpointer data) {
#ifdef GDK_WINDOWING_WAYLAND
	GList *toplevels = gtk_window_list_toplevels();
	for (GList *it = toplevels; it != NULL; it = it->next) {
		GtkWidget *widget = GTK_WIDGET(it->data);
		GdkWindow *window = gtk_widget_get_window(widget);
		if (window == NULL || !GDK_IS_WAYLAND_WINDOW(window)) {
			continue;
		}
		if (gtk_window_get_decorated(GTK_WINDOW(widget))) {
			continue;
		}
		gdk_wayland_window_announce_csd(window);
	}
	g_list_free(toplevels);
#endif
	return G_SOURCE_REMOVE;
}

static void pando_announce_csd(void) {
	g_idle_add(pando_announce_csd_idle, NULL);
}
*/
import "C"

// suppressServerDecorations stops Wayland compositors from drawing a native
// title bar on the frameless window. It is queued on the GTK main loop, so it
// is safe to call from any goroutine, and idempotent.
func suppressServerDecorations() {
	C.pando_announce_csd()
}
