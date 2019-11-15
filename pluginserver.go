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

// --- pluginData  --- //
type pluginData struct {
	name        string
	code        *plugin.Plugin
	constructor func() interface{}
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

func (plug *pluginData) setHandlers() {
	handlers := map[string]func(kong *pdk.PDK){}
	config := plug.constructor()

	if h, ok := config.(certificater); ok { handlers["certificate"]   = h.Certificate  }
	if h, ok := config.(rewriter)    ; ok { handlers["rewrite"]       = h.Rewrite      }
	if h, ok := config.(accesser)    ; ok { handlers["access"]        = h.Access       }
	if h, ok := config.(headerFilter); ok { handlers["header_filter"] = h.HeaderFilter }
	if h, ok := config.(bodyFilter)  ; ok { handlers["body_filter"]   = h.BodyFilter   }
	if h, ok := config.(prereader)   ; ok { handlers["preread"]       = h.Preread      }
	if h, ok := config.(logger)      ; ok { handlers["log"]           = h.Log          }

	plug.handlers = handlers
}

// --- instanceData --- //
type instanceData struct {
	id          int
	plugin      *pluginData
	initialized bool
	config      interface{}
	ipc         chan string
	pdk         *pdk.PDK
}

// --- PluginServer --- //
type PluginServer struct {
	lock       sync.RWMutex
	pluginsDir string
	nextId     int
	plugins    map[string]*pluginData
	instances  map[int]instanceData
}

func newServer() *PluginServer {
	return &PluginServer{
		plugins:   map[string]*pluginData{},
		instances: map[int]instanceData{},
	}
}

/// exported method
func (s *PluginServer) SetPluginDir(dir string, reply *string) error {
	s.lock.Lock()
	s.pluginsDir = dir
	s.lock.Unlock()
	*reply = "ok"
	return nil
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
	plug.setHandlers()

	s.lock.Lock()
	s.plugins[name] = plug
	s.lock.Unlock()

	return
}

func getSchemaType(t reflect.Type) (string, bool) {
	//log.Printf("SCHEMA TYPE FOR T IS %s\n", t.String())
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

type PluginInfo struct {
	Name     string
	Phases   []string
	Version  string
	Priority int
	Schema   string
}

/// exported method
func (s PluginServer) GetPluginInfo(name string, info *PluginInfo) error {
	plug, err := s.loadPlugin(name)
	if err != nil {
		return err
	}

	*info = PluginInfo{Name: name}

	info.Phases = make([]string, len(plug.handlers))
	var i = 0
	for name := range plug.handlers {
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

type PluginConfig struct {
	Name   string
	Config []byte
}

type InstanceStatus struct {
	Name   string
	Id     int
	Config interface{}
}

/// exported method
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
		id:     s.nextId,
		plugin: plug,
		config: instanceConfig,
	}

	s.lock.Lock()
	s.nextId++
	s.instances[instance.id] = instance
	s.lock.Unlock()

	*status = InstanceStatus{
		Name:   config.Name,
		Id:     instance.id,
		Config: instance.config,
	}

	return nil
}

/// exported method
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
