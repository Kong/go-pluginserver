package main

import (
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

		rpc.RegisterName("plugin", &PluginServer{ plugins: make(map[string]pluginData) })

		runServer(listener)
	}
}

// --- pluginData  --- //
type pluginData struct {
	code        *plugin.Plugin
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

func (plug *pluginData) setHandlers() {
	handlers := map[string]func(kong *pdk.PDK){}
	config := plug.config

	if h, ok := config.(certificater); ok { handlers["certificate"]   = h.Certificate  }
	if h, ok := config.(rewriter)    ; ok { handlers["rewrite"]       = h.Rewrite      }
	if h, ok := config.(accesser)    ; ok { handlers["access"]        = h.Access       }
	if h, ok := config.(headerFilter); ok { handlers["header_filter"] = h.HeaderFilter }
	if h, ok := config.(bodyFilter)  ; ok { handlers["body_filter"]   = h.BodyFilter   }
	if h, ok := config.(prereader)   ; ok { handlers["preread"]       = h.Preread      }
	if h, ok := config.(logger)      ; ok { handlers["log"]           = h.Log          }

	plug.handlers = handlers
}

// --- PluginServer --- //
type PluginServer struct {
	pluginsDir string
	plugins    map[string]pluginData
}

/// exported method
func (s *PluginServer) SetPluginDir(dir string, reply *string) error {
	s.pluginsDir = dir
	*reply = "ok"
	return nil
}

func (s PluginServer) loadPlugin(name string) (plug pluginData, err error) {
	plug, ok := s.plugins[name]
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

	plug = pluginData{
		code:   code,
		config: constructor(),
	}
	plug.setHandlers()

	s.plugins[name] = plug
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
func (s *PluginServer) GetPluginInfo(name string, info *PluginInfo) error {

	plug, err := s.loadPlugin(name)
	if err != nil {
		return err
	}

	*info = PluginInfo{ Name: name }

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
