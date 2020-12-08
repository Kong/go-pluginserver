
package main

import (
	"encoding/binary"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"github.com/Kong/go-pluginserver/kong_plugin_protocol"
)



type PluginCodec struct {
	conn io.ReadWriteCloser
	request kong_plugin_protocol.RpcCall
	request_body interface{}
	response kong_plugin_protocol.RpcReturn
}


func NewCodec(conn io.ReadWriteCloser) PluginCodec {
	log.Printf("NewCodec")
	return PluginCodec {
		conn: conn,
	}
}


// handle one full call/result (request/response)
// return an error only for closing the connection.
// procedure errors are sent in the result.
func (c *PluginCodec) Handle(s *PluginServer) error {
	c.readRequest()
	c.doCall(s)
	c.writeResponse()

	return nil
}


func (c *PluginCodec) readRequest() error {
	var size uint32
	err := binary.Read(c.conn, binary.LittleEndian, &size)
	if err != nil {
		return err
	}
	log.Printf("got size: %d", size)

	buf := make([]byte, size)
	_, err = io.ReadFull(c.conn, buf)
	if err != nil {
		return err
	}
	log.Printf("got msg: %v", buf)

	err = proto.Unmarshal(buf, &c.request)
	if err != nil {
		return err
	}

	return nil
}

func (c *PluginCodec) doCall(s *PluginServer) error {
	c.response.Sequence = c.request.Sequence

	switch x := c.request.Call.(type) {
		case *kong_plugin_protocol.RpcCall_CmdGetPluginNames:
			log.Printf("method: %v, x: %v", "plugin.GetPluginNames", x)

		case *kong_plugin_protocol.RpcCall_CmdGetPluginInfo:
			log.Printf("method: %v, x: %v", "plugin.GetPluginInfo", x)

		case *kong_plugin_protocol.RpcCall_CmdStartInstance:
			log.Printf("method: %v, x: %v", "plugin.StartInstance", x)
			status := InstanceStatus{}
			err := s.StartInstance(PluginConfig{
				Name : x.CmdStartInstance.Name,
				Config : []byte(x.CmdStartInstance.Config),
			}, &status)
			if err != nil {
				c.setError(err)
				return nil
			}
			c.response.Return = &kong_plugin_protocol.RpcReturn_InstanceStatus{
				InstanceStatus: &kong_plugin_protocol.InstanceStatus{
					Name : status.Name,
					InstanceId : int32(status.Id),
					Config : "---",
					StartedAt : status.StartTime,
				},
			}
			return nil

		case *kong_plugin_protocol.RpcCall_CmdGetInstanceStatus:
			log.Printf("method: %v, x: %v", "plugin.GetInstanceStatus", x)
			status := InstanceStatus{}
			err := s.InstanceStatus(int(x.CmdGetInstanceStatus.InstanceId), &status)
			if err != nil {
				c.setError(err)
				return nil
			}
			c.response.Return = &kong_plugin_protocol.RpcReturn_InstanceStatus{
				InstanceStatus: &kong_plugin_protocol.InstanceStatus{
					Name : status.Name,
					InstanceId : int32(status.Id),
					Config : "---",
					StartedAt : status.StartTime,
				},
			}
			return nil

		case *kong_plugin_protocol.RpcCall_CmdCloseInstance:
			log.Printf("method: %v, x: %v", "plugin.CloseInstance", x)
			status := InstanceStatus{}
			err := s.CloseInstance(int(x.CmdCloseInstance.InstanceId), &status)
			if err != nil {
				c.setError(err)
				return nil
			}
			c.response.Return = &kong_plugin_protocol.RpcReturn_InstanceStatus{
				InstanceStatus: &kong_plugin_protocol.InstanceStatus{
					Name : status.Name,
					InstanceId : int32(status.Id),
					Config : "---",
					StartedAt : status.StartTime,
				},
			}
			return nil

		case *kong_plugin_protocol.RpcCall_CmdHandleEvent:
			log.Printf("method: %v, x: %v", "plugin.HandleEvent", x)
			outStep := StepData{}
			err := s.HandleEvent(StartEventData{
				InstanceId : int(x.CmdHandleEvent.InstanceId),
				EventName : x.CmdHandleEvent.EventName,
			}, &outStep)
			if err != nil {
				c.setError(err)
				return nil
			}
			c.response.Return = &kong_plugin_protocol.RpcReturn_StepData{
				StepData: stepDataToPB(outStep),
			}
			return nil

		case *kong_plugin_protocol.RpcCall_CmdStep:
			log.Printf("method: %v, x: %v", "plugin.Step", x)
			outStep := StepData{}
			err := s.Step(stepDataFromPB(x.CmdStep), &outStep)
			if err != nil {
				c.setError(err)
				return nil
			}
			c.response.Return = &kong_plugin_protocol.RpcReturn_StepData{
				StepData: stepDataToPB(outStep),
			}
			return nil


		default:
			log.Printf("no luck")
			return errors.New("bad request.")
	}
	return nil
}

func (c *PluginCodec) setError(err error) {
	c.response.Return = &kong_plugin_protocol.RpcReturn_StepData{
		StepData: &kong_plugin_protocol.StepData{
			Data: &kong_plugin_protocol.StepData_Error{
				Error: err.Error(),
			} } }
}


func stepDataToPB(outStep StepData) *kong_plugin_protocol.StepData {
	log.Printf("stepDataToPB: outstepData: %v", outStep.Data)
	return &kong_plugin_protocol.StepData{
		EventId: int64(outStep.EventId),
// 		Data: stepDataToPB(outStep.Data),
	}
}

func stepDataFromPB(inStep *kong_plugin_protocol.StepData) StepData {
	log.Printf("stepDataFromPB: inStep.Data: %v", inStep.Data)
	return StepData{
		EventId: int(inStep.EventId),
// 		Data: stepDataFromPB(x.CmdStep.Data),
	}
}



func (c *PluginCodec) writeResponse() error {
	log.Printf("write: %#v", c.response)
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

	c.response = kong_plugin_protocol.RpcReturn{}
	return err
}


func (c *PluginCodec) Close() error {
	return c.conn.Close()
}
