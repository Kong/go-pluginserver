// go-pluginserver is a standalone RPC server that runs
// Go plugins for Kong.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/kong/go-pdk"
	"github.com/ugorji/go/codec"
	"log"
	"net"
	"net/rpc"
	"path"
	"plugin"
	"reflect"
	"strings"
	"sync"
)

var socket = flag.String("socket", "", "Socket to listen into")

func runServer(listener net.Listener) {
	var handle codec.MsgpackHandle

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept(): %s", err)
			return
		}
		rpcCodec := codec.MsgpackSpecRpc.ServerCodec(conn, &handle)
		rpc.ServeCodec(rpcCodec)
	}
}

func main() {
	flag.Parse()

	if *socket != "" {
		listener, err := net.Listen("unix", *socket)
		if err != nil {
			log.Printf(`listen("%s"): %s`, socket, err)
			return
		}

		rpc.RegisterName("plugin", newServer())

		runServer(listener)
	}
}

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
	name        string
	code        *plugin.Plugin
	constructor func() interface{}
	config      interface{}
}

func (s PluginServer) loadPlugin(name string) (plug *pluginData, err error) {
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

	info.Schema = out.String()

	return nil
}

// --- instanceData --- //
type instanceData struct {
	id          int
	plugin      *pluginData
	initialized bool
	config      interface{}
	handlers    map[string]func(kong *pdk.PDK)
}

type (
	certificater interface{ Certificate(*pdk.PDK) }
	rewriter     interface{ Rewrite(*pdk.PDK) }
	accesser     interface{ Access(*pdk.PDK) }
	headerFilter interface{ HeaderFilter(*pdk.PDK) }
	bodyFilter   interface{ BodyFilter(*pdk.PDK) }
	prereader    interface{ Preread(*pdk.PDK) }
	logger       interface{ Log(*pdk.PDK) }
)

func getHandlers(config interface{}) map[string]func(kong *pdk.PDK) {
	handlers := map[string]func(kong *pdk.PDK){}

	if h, ok := config.(certificater); ok { handlers["certificate"]   = h.Certificate  }
	if h, ok := config.(rewriter)    ; ok { handlers["rewrite"]       = h.Rewrite      }
	if h, ok := config.(accesser)    ; ok { handlers["access"]        = h.Access       }
	if h, ok := config.(headerFilter); ok { handlers["header_filter"] = h.HeaderFilter }
	if h, ok := config.(bodyFilter)  ; ok { handlers["body_filter"]   = h.BodyFilter   }
	if h, ok := config.(prereader)   ; ok { handlers["preread"]       = h.Preread      }
	if h, ok := config.(logger)      ; ok { handlers["log"]           = h.Log          }

	return handlers
}

// Configuration data for a new plugin instance.
type PluginConfig struct {
	Name   string // plugin name
	Config []byte // configuration data, as a JSON string
}

// Current state of a plugin instance.  TODO: add some statistics
type InstanceStatus struct {
	Name   string      // plugin name
	Id     int         // instance id
	Config interface{} // configuration data, decoded
}

// StartInstance starts a plugin instance, as requred by configuration data.  More than
// one instance can be started for a single plugin.  If the configuration changes,
// a new instance should be started and the old one closed.
//
// RPC exported method
func (s *PluginServer) StartInstance(config PluginConfig, status *InstanceStatus) error {
	plug, err := s.loadPlugin(config.Name)
	if err != nil {
		return err
	}

	instanceConfig := plug.constructor()

	if err := json.Unmarshal(config.Config, instanceConfig); err != nil {
		return fmt.Errorf("Decoding config: %w", err)
	}

	instance := instanceData{
		plugin:   plug,
		config:   instanceConfig,
		handlers: getHandlers(instanceConfig),
	}

	s.lock.Lock()
	instance.id = s.nextInstanceId
	s.nextInstanceId++
	s.instances[instance.id] = &instance
	s.lock.Unlock()

	*status = InstanceStatus{
		Name:   config.Name,
		Id:     instance.id,
		Config: instance.config,
	}

	return nil
}

// InstanceStatus returns a given resource's status (the same given when started)
//
// RPC exported method
func (s PluginServer) InstanceStatus(id int, status *InstanceStatus) error {
	s.lock.RLock()
	instance, ok := s.instances[id]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No plugin instance %d", id)
	}

	*status = InstanceStatus{
		Name:   instance.plugin.name,
		Id:     instance.id,
		Config: instance.config,
	}

	return nil
}

// CloseInstance is used when an instance shouldn't be used anymore.
// Doesn't kill any running event but the instance is no longer accesible,
// so it's not possible to start a new event with it and will be garbage
// collected after the last reference event finishes.
// Returns the status just before closing.
//
// RPC exported method
func (s PluginServer) CloseInstance(id int, status *InstanceStatus) error {
	s.lock.RLock()
	instance, ok := s.instances[id]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No plugin instance %d", id)
	}

	*status = InstanceStatus{
		Name:   instance.plugin.name,
		Id:     instance.id,
		Config: instance.config,
	}

	// kill?

	s.lock.Lock()
	delete(s.instances, id)
	s.lock.Unlock()

	return nil
}

// Incoming data for a new event.
// TODO: add some relevant data to reduce number of callbacks.
type StartEventData struct {
	InstanceId int    // Instance ID to start the event
	EventName  string // event name (not handler method name)
	// ....
}

type eventData struct {
	id       int           // event id
	instance *instanceData // plugin instance
	ipc      chan string   // communication channel (TODO: use decoded structs)
	pdk      *pdk.PDK      // go-pdk instance
}

// HandleEvent starts the call/{callback/response}*/finish cycle.
// More than one event can be run concurrenty for a single plugin instance,
// they all receive the same object instance, so should be careful if it's
// mutated or holds references to mutable data.
//
// RPC exported data
func (s PluginServer) HandleEvent(in StartEventData, out *StepData) error {
	s.lock.RLock()
	instance, ok := s.instances[in.InstanceId]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No plugin instance %d", in.InstanceId)
	}

	h, ok := instance.handlers[in.EventName]
	if !ok {
		return fmt.Errorf("undefined method %s on plugin %s",
			in.EventName, instance.plugin.name)
	}

	ipc := make(chan string)

	event := eventData{
		instance: instance,
		ipc:      ipc,
		pdk:      pdk.Init(ipc),
	}

	s.lock.Lock()
	event.id = s.nextEventId
	s.nextEventId++
	s.events[event.id] = &event
	s.lock.Unlock()

	//log.Printf("Will launch goroutine for key %d / operation %s\n", key, op)
	go func() {
		_ = <-ipc
		h(event.pdk)
		ipc <- "ret"

		s.lock.Lock()
		delete(s.events, event.id)
		s.lock.Unlock()
	}()

	*out = StepData{EventId: event.id, Data: "ok"}
	return nil
}

// A callback's response/request.
// TODO: use decoded structure instead of a JSON string.
type StepData struct {
	EventId int    // event cycle to which this belongs
	Data    string // carried data
}

// Step carries a callback's anser back from Kong to the plugin,
// the return value is either a new callback request or a finish signal.
//
// RPC exported method
func (s PluginServer) Step(in StepData, out *StepData) error {
	s.lock.RLock()
	event, ok := s.events[in.EventId]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No running event %d", in.EventId)
	}

	event.ipc <- in.Data
	outStr := <-event.ipc
	*out = StepData{Data: outStr} // TODO: decode outStr

	return nil
}
