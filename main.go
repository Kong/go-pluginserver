// go-pluginserver is a standalone RPC server that runs
// Go plugins for Kong.
package main

import (
	"flag"
	"runtime"
	"github.com/ugorji/go/codec"
	"log"
	"net"
	"net/rpc"
	"os"
	"reflect"
	"fmt"
)

var version = "development"

/* flags */
var (
	kongPrefix = flag.String("kong-prefix", "/usr/local/kong", "Kong prefix path (specified by the -p argument commonly used in the kong cli)")
	dump = flag.String("dump-plugin-info", "", "Dump info about `plugin` as a MessagePack object")
	pluginsDir = flag.String("plugins-directory", "", "Set directory `path` where to search plugins")
	showVersion = flag.Bool("version", false, "Print binary and runtime version")
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

func runServer(listener net.Listener) {
	var handle codec.MsgpackHandle
	handle.ReaderBufferSize = 4096
	handle.WriterBufferSize = 4096
	handle.RawToString = true
	handle.MapType = reflect.TypeOf(map[string]interface{}(nil))

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept(): %s", err)
			return
		}

		enc := codec.NewEncoder(conn, &handle)
		_ = enc.Encode([]interface{}{2, "serverPid", os.Getpid()})

		rpcCodec := codec.MsgpackSpecRpc.ServerCodec(conn, &handle)
		go rpc.ServeCodec(rpcCodec)
	}
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

	rpc.RegisterName("plugin", newServer())

	runServer(listener)
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

	if socket != "" {
		startServer()
	}
}
