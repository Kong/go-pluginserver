// go-pluginserver is a standalone RPC server that runs
// Go plugins for Kong.
package main

import (
	"flag"
	"fmt"
	"github.com/ugorji/go/codec"
	"io"
	"log"
	"net"
// // 	"net/rpc"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

var version = "development"

/* flags */
var (
	kongPrefix     = flag.String("kong-prefix", "/usr/local/kong", "Kong prefix path (specified by the -p argument commonly used in the kong cli)")
	dump           = flag.String("dump-plugin-info", "", "Dump info about `plugin` as a MessagePack object")
	dumpAllPlugins = flag.Bool("dump-all-plugins", false, "Dump info about all available plugins")
	pluginsDir     = flag.String("plugins-directory", "", "Set directory `path` where to search plugins")
	showVersion    = flag.Bool("version", false, "Print binary and runtime version")
)

var socket string

func init() {
	flag.Parse()
	socket = *kongPrefix + "/" + "go_pluginserver.sock"

	if *kongPrefix == "" && *dump == "" {
		flag.Usage()
		os.Exit(2)
	}
}

func printVersion() {
	fmt.Printf("Version: %s\nRuntime Version: %s\n", version, runtime.Version())
}

func dumpInfo() {
	s := newServer()

	info := PluginInfo{}
	err := s.GetPluginInfo(*dump, &info)
	if err != nil {
		log.Printf("%s", err)
	}

	var handle codec.MsgpackHandle
	handle.ReaderBufferSize = 4096
	handle.WriterBufferSize = 4096
	handle.RawToString = true
	handle.MapType = reflect.TypeOf(map[string]interface{}(nil))

	enc := codec.NewEncoder(os.Stdout, &handle)
	_ = enc.Encode(info)
}

func dumpAll() {
	s := newServer()

	pluginPaths, err := filepath.Glob(path.Join(s.pluginsDir, "/*.so"))
	if err != nil {
		log.Printf("can't get plugin names from %s: %s", s.pluginsDir, err)
		return
	}

	infos := make([]PluginInfo, len(pluginPaths))

	for i, pluginPath := range pluginPaths {
		pluginName := strings.TrimSuffix(path.Base(pluginPath), ".so")

		err = s.GetPluginInfo(pluginName, &infos[i])
		if err != nil {
			log.Printf("can't load Plugin %s: %s", pluginName, err)
			continue
		}
	}

	var handle codec.JsonHandle
	enc := codec.NewEncoder(os.Stdout, &handle)
	_ = enc.Encode(infos)
}

func runServer(listener net.Listener, s *PluginServer) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		// TODO: push PID notification

		pbCodec := NewCodec(conn)
		go serveCodec(pbCodec, s)
	}
}

func serveCodec(pbCodec PluginCodec, s *PluginServer) {
	for {
		err := pbCodec.Handle(s)
		if err != nil {
			if err != io.EOF {
				log.Println("rpc:", err)
			}
			break
		}
	}
	pbCodec.Close()
}

func startServer() {
	err := os.Remove(socket)
	if err != nil && !os.IsNotExist(err) {
		log.Printf(`removing "%s": %s`, kongPrefix, err)
		return
	}

	listener, err := net.Listen("unix", socket)
	if err != nil {
		log.Printf(`listen("%s"): %s`, socket, err)
		return
	}

// 	rpc.RegisterName("plugin", newServer())
	runServer(listener, newServer())
}

func isParentAlive() bool {
	return os.Getppid() != 1 // assume ppid 1 means process was adopted by init
}

func main() {
	if *showVersion == true {
		printVersion()
		os.Exit(0)
	}

	if *dump != "" {
		dumpInfo()
		os.Exit(0)
	}

	if *dumpAllPlugins {
		dumpAll()
		os.Exit(0)
	}

	if socket != "" {
		go func() {
			for {
				if !isParentAlive() {
					log.Printf("Kong exited; shutting down...")
					os.Exit(0)
				}

				time.Sleep(1 * time.Second)
			}
		}()

		startServer()
	}
}
