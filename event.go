package main

import (
	"fmt"
	"github.com/Kong/go-pdk"
	"time"
)

// Incoming data for a new event.
// TODO: add some relevant data to reduce number of callbacks.
type StartEventData struct {
	InstanceId int    // Instance ID to start the event
	EventName  string // event name (not handler method name)
	// ....
}

type eventData struct {
	id       int              // event id
	instance *instanceData    // plugin instance
	ipc      chan interface{} // communication channel (TODO: use decoded structs)
	pdk      *pdk.PDK         // go-pdk instance
}

// HandleEvent starts the call/{callback/response}*/finish cycle.
// More than one event can be run concurrenty for a single plugin instance,
// they all receive the same object instance, so should be careful if it's
// mutated or holds references to mutable data.
//
// RPC exported method
func (s *PluginServer) HandleEvent(in StartEventData, out *StepData) error {
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

	ipc := make(chan interface{})

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
		event.instance.lastEvent = time.Now()
		delete(s.events, event.id)
		s.lock.Unlock()
	}()

	ipc <- "run" // kickstart the handler

	*out = StepData{EventId: event.id, Data: <-ipc}
	return nil
}

// A callback's response/request.
type StepData struct {
	EventId int         // event cycle to which this belongs
	Data    interface{} // carried data
}

// Step carries a callback's anser back from Kong to the plugin,
// the return value is either a new callback request or a finish signal.
//
// RPC exported method
func (s *PluginServer) Step(in StepData, out *StepData) error {
	s.lock.RLock()
	event, ok := s.events[in.EventId]
	s.lock.RUnlock()
	if !ok {
		return fmt.Errorf("No running event %d", in.EventId)
	}

	event.ipc <- in.Data
	outStr := <-event.ipc
	*out = StepData{EventId: in.EventId, Data: outStr}

	return nil
}
