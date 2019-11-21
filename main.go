// go-pluginserver is a standalone RPC server that runs
// Go plugins for Kong.
package main

import (
	"flag"
	"github.com/ugorji/go/codec"
	"log"
	"net"
	"net/rpc"
)

var socket = flag.String("socket", "", "Socket to listen into")

func runServer(listener net.Listener) {
	var handle codec.MsgpackHandle
	handle.ReaderBufferSize = 4096
	handle.WriterBufferSize = 4096
	handle.RawToString = true

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept(): %s", err)
			return
		}
		rpcCodec := codec.MsgpackSpecRpc.ServerCodec(conn, &handle)
		go rpc.ServeCodec(rpcCodec)
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
