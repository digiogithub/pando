package linux

import (
	"context"
	"fmt"
	"reflect"

	"github.com/godbus/dbus/v5"
)

// accessibleRef identifies a single AT-SPI2 accessible object: the D-Bus
// bus name (well-known or unique, e.g. ":1.42") owning it and its object
// path within that bus. AT-SPI2 represents this pair on the wire as the
// D-Bus struct signature "(so)".
type accessibleRef struct {
	Bus  string
	Path dbus.ObjectPath
}

// pathOf converts a plain string (as stored in core.Element.WindowID) into
// a dbus.ObjectPath.
func pathOf(s string) dbus.ObjectPath { return dbus.ObjectPath(s) }

func (r accessibleRef) String() string {
	return string(r.Bus) + string(r.Path)
}

// soRef mirrors the wire shape of an AT-SPI "(so)" accessible reference so
// godbus can decode into it positionally.
type soRef struct {
	Bus  string
	Path dbus.ObjectPath
}

func (s soRef) ref() accessibleRef { return accessibleRef{Bus: s.Bus, Path: s.Path} }

// busConn is the minimal D-Bus surface the traversal/action code in this
// package depends on. It exists so tests can substitute a fake, in-memory
// AT-SPI tree instead of a real accessibility bus.
type busConn interface {
	// call invokes iface.method on dest/path with args, returning the reply
	// body.
	call(ctx context.Context, dest string, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error)
	// getAllProps calls org.freedesktop.DBus.Properties.GetAll(iface) on
	// dest/path.
	getAllProps(ctx context.Context, dest string, path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error)
	// close releases the underlying connection, if any.
	close() error
}

// dbusConn is the real busConn implementation, backed by a *dbus.Conn
// connected to the AT-SPI2 accessibility bus.
type dbusConn struct {
	conn *dbus.Conn
}

func newDbusConn(conn *dbus.Conn) *dbusConn { return &dbusConn{conn: conn} }

func (d *dbusConn) call(ctx context.Context, dest string, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	obj := d.conn.Object(dest, path)
	call := obj.CallWithContext(ctx, iface+"."+method, 0, args...)
	if call.Err != nil {
		return nil, fmt.Errorf("atspi: %s.%s on %s%s: %w", iface, method, dest, path, call.Err)
	}
	return call.Body, nil
}

func (d *dbusConn) getAllProps(ctx context.Context, dest string, path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error) {
	obj := d.conn.Object(dest, path)
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, iface)
	if call.Err != nil {
		return nil, fmt.Errorf("atspi: GetAll(%s) on %s%s: %w", iface, dest, path, call.Err)
	}
	var props map[string]dbus.Variant
	if err := call.Store(&props); err != nil {
		return nil, fmt.Errorf("atspi: decode GetAll(%s) on %s%s: %w", iface, dest, path, err)
	}
	return props, nil
}

// storeSoRefSlice decodes an AT-SPI "a(so)" reply argument (an array of
// accessible references) into out. godbus decodes each STRUCT element of a
// D-Bus array as a []interface{} of its fields, but the outer array itself
// may come back as either []interface{} or a concretely-typed
// [][]interface{} depending on the code path godbus took to build the
// reply, so both shapes are accepted via reflection; dbus.Store cannot
// decode array-of-struct generically into a []struct{...} destination (the
// element's static Go type is interface{}, which trips its convertibility
// check), so each element is decoded individually instead.
func storeSoRefSlice(raw interface{}, out *[]soRef) error {
	rv := reflect.ValueOf(raw)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("atspi: expected an array reply, got %T", raw)
	}
	result := make([]soRef, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		fields, ok := elem.([]interface{})
		if !ok || len(fields) != 2 {
			return fmt.Errorf("atspi: expected element %d to be a 2-field (so) struct, got %T", i, elem)
		}
		var s soRef
		if err := dbus.Store(fields, &s.Bus, &s.Path); err != nil {
			return fmt.Errorf("atspi: decode (so) element %d: %w", i, err)
		}
		result = append(result, s)
	}
	*out = result
	return nil
}

func (d *dbusConn) close() error {
	if d.conn == nil {
		return nil
	}
	return d.conn.Close()
}
