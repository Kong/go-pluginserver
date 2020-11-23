
package kong_plugin_protocol

import (
	"encoding/binary"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"net/rpc"
)



type PluginCodec struct {
	conn io.ReadWriteCloser
	request RpcCall
	response RpcReturn
}


func NewCodec(conn io.ReadWriteCloser) PluginCodec {
	return PluginCodec {
		conn: conn,
	}
}


func (c PluginCodec) ReadRequestHeader(req *rpc.Request) error {
	var size uint32
	err := binary.Read(c.conn, binary.LittleEndian, &size)
	if err != nil {
		return err
	}

	buf := make([]byte, size)
	_, err = io.ReadFull(c.conn, buf)
	if err != nil {
		return err
	}

	err = proto.Unmarshal(buf, &c.request)
	if err != nil {
		return err
	}

	switch c.request.Call.(type) {
		case *RpcCall_CmdGetPluginNames:
			req.ServiceMethod = "plugin.GetPluginNames"

		case *RpcCall_CmdGetPluginInfo:
			req.ServiceMethod = "plugin.GetPluginInfo"

		case *RpcCall_CmdStartInstance:
			req.ServiceMethod = "plugin.StartInstance"

		case *RpcCall_CmdGetInstanceStatus:
			req.ServiceMethod = "plugin.GetInstanceStatus"

		case *RpcCall_CmdCloseInstance:
			req.ServiceMethod = "plugin.CloseInstance"

		case *RpcCall_CmdHandleEvent:
			req.ServiceMethod = "plugin.HandleEvent"

		case *RpcCall_CmdStep:
			req.ServiceMethod = "plugin.Step"
	}

	req.Seq = uint64(c.request.Sequence)

	return nil
}


func (c PluginCodec) ReadRequestBody(body interface{}) error {
	body = c.request
	return nil
}


func (c PluginCodec) WriteResponse(res *rpc.Response, resBody interface{}) error {
	if res.Error != "" {
// 		c.response.Return.StepData.Error = res.Error
		c.response.Return = &RpcReturn_StepData{ StepData: &StepData{ Data: &StepData_Error{ Error: res.Error }}}
		return c.write()
	}

	log.Printf("res: %#v, resBody: %#v", res, resBody)
	return errors.New("don't feel like writing the response.")
}

func (c PluginCodec) write() error {
	buf, err := proto.Marshal(&c.response)
	if err != nil {
		return err
	}

	var size = uint32(len(buf))
	err = binary.Write(c.conn, binary.LittleEndian, &size)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(buf)
	return err
}


func (c PluginCodec) Close() error {
	return c.conn.Close()
}
