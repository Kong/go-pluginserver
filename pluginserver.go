package main

import (
	"fmt"
	"os"
	"path"
	"plugin"
	"reflect"
	"strings"
	"sync"
	"time"
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
	s := PluginServer{
		plugins:   map[string]*pluginData{},
		instances: map[int]*instanceData{},
		events:    map[int]*eventData{},
	}

	if *pluginsDir == "" {
		if dir, err := os.Getwd(); err == nil {
			s.pluginsDir = dir
		}
	} else {
		s.pluginsDir = *pluginsDir
	}

	return &s
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

// --- status --- //

type ServerStatusData struct {
	Pid     int
	Plugins map[string]PluginStatusData
}

func (s *PluginServer) GetStatus(n int, reply *ServerStatusData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	*reply = ServerStatusData{
		Pid:     os.Getpid(),
		Plugins: make(map[string]PluginStatusData),
	}

	var err error
	for pluginname := range s.plugins {
		reply.Plugins[pluginname], err = s.getPluginStatus(pluginname)
		if err != nil {
			return err
		}
	}

	return nil
}

// --- pluginData  --- //

type pluginData struct {
	lock              sync.Mutex
	name              string
	code              *plugin.Plugin
	modtime           time.Time
	loadtime          time.Time
	constructor       func() interface{}
	config            interface{}
	lastStartInstance time.Time
	lastCloseInstance time.Time
}

func getModTime(fname string) (modtime time.Time, err error) {
	finfo, err := os.Stat(fname)
	if err != nil {
		return
	}

	modtime = finfo.ModTime()
	return
}

func (s *PluginServer) loadPlugin(name string) (plug *pluginData, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	plug, ok := s.plugins[name]
	if ok {
		return
	}

	plugFName := path.Join(s.pluginsDir, name+".so")
	plugModTime, err := getModTime(plugFName)
	if err != nil {
		return
	}

	code, err := plugin.Open(plugFName)
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
		modtime:     plugModTime,
		loadtime:    time.Now(),
		constructor: constructor,
		config:      constructor(),
	}

	s.plugins[name] = plug

	return
}

type schemaDict map[string]interface{}

func getSchemaDict(t reflect.Type) schemaDict {
	switch t.Kind() {
	case reflect.String:
		return schemaDict{"type": "string"}

	case reflect.Bool:
		return schemaDict{"type": "boolean"}

	case reflect.Int, reflect.Int32:
		return schemaDict{"type": "integer"}

	case reflect.Uint, reflect.Uint32:
		return schemaDict{
			"type":    "integer",
			"between": []int{0, 2147483648},
		}

	case reflect.Float32, reflect.Float64:
		return schemaDict{"type": "number"}

	case reflect.Array:
		elemType := getSchemaDict(t.Elem())
		if elemType == nil {
			break
		}
		return schemaDict{
			"type":     "array",
			"elements": schemaDict{"type": elemType},
		}

	case reflect.Map:
		kType := getSchemaDict(t.Key())
		vType := getSchemaDict(t.Elem())
		if kType == nil || vType == nil {
			break
		}
		return schemaDict{
			"type":   "map",
			"keys":   schemaDict{"type": kType},
			"values": schemaDict{"type": vType},
		}

	case reflect.Struct:
		fieldsArray := []schemaDict{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			typeDecl := getSchemaDict(field.Type)
			if typeDecl == nil {
				// ignore unrepresentable types
				continue
			}
			name := field.Tag.Get("json")
			if name == "" {
				name = strings.ToLower(field.Name)
			}
			fieldsArray = append(fieldsArray, schemaDict{name: typeDecl})
		}
		return schemaDict{
			"type":   "record",
			"fields": fieldsArray,
		}
	}

	return nil
}

// Information obtained from a plugin's compiled code.
type PluginInfo struct {
	Name     string     // plugin name
	ModTime  time.Time  `codec:",omitempty"` // plugin file modification time
	LoadTime time.Time  `codec:",omitempty"` // plugin load time
	Phases   []string   // events it can handle
	Version  string     // version number
	Priority int        // priority info
	Schema   schemaDict // JSON representation of the config schema
}

// GetPluginInfo loads and retrieves information from the compiled plugin.
// TODO: reload if the plugin code has been updated.
//
// RPC exported method
func (s *PluginServer) GetPluginInfo(name string, info *PluginInfo) error {
	plug, err := s.loadPlugin(name)
	if err != nil {
		return err
	}

	*info = PluginInfo{Name: name}

	plug.lock.Lock()
	defer plug.lock.Unlock()
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

	// 	st, _ := getSchemaDict(reflect.TypeOf(plug.config).Elem())
	info.Schema = schemaDict{
		"name": name,
		"fields": []schemaDict{
			schemaDict{"config": getSchemaDict(reflect.TypeOf(plug.config).Elem())},
		},
	}

	return nil
}

type PluginStatusData struct {
	Name              string
	Modtime           int64
	LoadTime          int64
	Instances         []InstanceStatus
	LastStartInstance int64
	LastCloseInstance int64
}

func (s *PluginServer) getPluginStatus(name string) (status PluginStatusData, err error) {
	plug, ok := s.plugins[name]
	if !ok {
		err = fmt.Errorf("plugin %#v not loaded", name)
		return
	}

	instances := []InstanceStatus{}
	for _, instance := range s.instances {
		if instance.plugin == plug {
			instances = append(instances, InstanceStatus{
				Name:      name,
				Id:        instance.id,
				Config:    instance.config,
				StartTime: instance.startTime.Unix(),
			})
		}
	}

	status = PluginStatusData{
		Name:              name,
		Modtime:           plug.modtime.Unix(),
		LoadTime:          plug.loadtime.Unix(),
		Instances:         instances,
		LastStartInstance: plug.lastStartInstance.Unix(),
		LastCloseInstance: plug.lastCloseInstance.Unix(),
	}
	return
}
