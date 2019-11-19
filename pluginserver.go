package main

import (
	"fmt"
	"path"
	"plugin"
	"reflect"
	"strings"
	"sync"
)

// --- PluginServer --- //

// Holds the execution status of the plugin server.
type PluginServer struct {
	lock           sync.RWMutex
	pluginsDir     string
	plugins        map[string]*pluginData
	instances      map[int]*instanceData
	events         map[int]*eventData
	nextInstanceId int
	nextEventId    int
}

// Create a new server context.
func newServer() *PluginServer {
	return &PluginServer{
		plugins:   map[string]*pluginData{},
		instances: map[int]*instanceData{},
		events:    map[int]*eventData{},
	}
}

// SetPluginDir tells the server where to find the plugins.
//
// RPC exported method
func (s *PluginServer) SetPluginDir(dir string, reply *string) error {
	s.lock.Lock()
	s.pluginsDir = dir
	s.lock.Unlock()
	*reply = "ok"
	return nil
}

// --- pluginData  --- //

type pluginData struct {
	lock        sync.Mutex
	name        string
	code        *plugin.Plugin
	constructor func() interface{}
	config      interface{}
}

func (s *PluginServer) loadPlugin(name string) (plug *pluginData, err error) {
	s.lock.RLock()
	plug, ok := s.plugins[name]
	s.lock.RUnlock()
	if ok {
		return
	}

	code, err := plugin.Open(path.Join(s.pluginsDir, name+".so"))
	if err != nil {
		err = fmt.Errorf("failed to open plugin %s: %w", name, err)
		return
	}

	constructorSymbol, err := code.Lookup("New")
	if err != nil {
		err = fmt.Errorf("No constructor function on plugin %s: %w", name, err)
		return
	}

	constructor, ok := constructorSymbol.(func() interface{})
	if !ok {
		err = fmt.Errorf("Wrong constructor signature on plugin %s: %w", name, err)
		return
	}

	plug = &pluginData{
		name:        name,
		code:        code,
		constructor: constructor,
		config:      constructor(),
	}

	s.lock.Lock()
	s.plugins[name] = plug
	s.lock.Unlock()

	return
}

func getSchemaType(t reflect.Type) (string, bool) {
	switch t.Kind() {
	case reflect.String:
		return `"string"`, true
	case reflect.Bool:
		return `"boolean"`, true
	case reflect.Int, reflect.Int32:
		return `"integer"`, true
	case reflect.Uint, reflect.Uint32:
		return `"integer","between":[0,2147483648]`, true
	case reflect.Float32, reflect.Float64:
		return `"number"`, true
	case reflect.Array:
		elemType, ok := getSchemaType(t.Elem())
		if !ok {
			break
		}
		return `"array","elements":{"type":` + elemType + `}`, true
	case reflect.Map:
		kType, ok := getSchemaType(t.Key())
		vType, ok := getSchemaType(t.Elem())
		if !ok {
			break
		}
		return `"map","keys":{"type":` + kType + `},"values":{"type":` + vType + `}`, true
	case reflect.Struct:
		var out strings.Builder
		out.WriteString(`"record","fields":[`)
		n := t.NumField()
		for i := 0; i < n; i++ {
			field := t.Field(i)
			typeDecl, ok := getSchemaType(field.Type)
			if !ok {
				// ignore unrepresentable types
				continue
			}
			if i > 0 {
				out.WriteString(`,`)
			}
			name := field.Tag.Get("json")
			if name == "" {
				name = strings.ToLower(field.Name)
			}
			out.WriteString(`{"`)
			out.WriteString(name)
			out.WriteString(`":{"type":`)
			out.WriteString(typeDecl)
			out.WriteString(`}}`)
		}
		out.WriteString(`]`)
		return out.String(), true
	}
	return "", false
}

// Information obtained from a plugin's compiled code.
type PluginInfo struct {
	Name     string   // plugin name
	Phases   []string // events it can handle
	Version  string   // version number
	Priority int      // priority info
	Schema   string   // JSON representation of the config schema
}

// GetPluginInfo loads and retrieves information from the compiled plugin.
// TODO: reload if the plugin code has been updated.
//
// RPC exported method
func (s PluginServer) GetPluginInfo(name string, info *PluginInfo) error {
	plug, err := s.loadPlugin(name)
	if err != nil {
		return err
	}

	*info = PluginInfo{Name: name}

	plug.lock.Lock()
	handlers := getHandlers(plug.config)

	info.Phases = make([]string, len(handlers))
	var i = 0
	for name := range handlers {
		info.Phases[i] = name
		i++
	}

	v, _ := plug.code.Lookup("Version")
	if v != nil {
		info.Version = v.(string)
	}

	prio, _ := plug.code.Lookup("Priority")
	if prio != nil {
		info.Priority = prio.(int)
	}

	var out strings.Builder
	out.WriteString(`{"name":"`)
	out.WriteString(name)
	out.WriteString(`","fields":[{"config":{"type":`)

	st, _ := getSchemaType(reflect.TypeOf(plug.config).Elem())
	out.WriteString(st)

	out.WriteString(`}}]}`)
	plug.lock.Unlock()

	info.Schema = out.String()

	return nil
}
