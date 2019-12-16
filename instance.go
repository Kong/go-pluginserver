package main

import (
	"encoding/json"
	"fmt"
	"github.com/Kong/go-pdk"
	"log"
	"time"
)

// --- instanceData --- //
type instanceData struct {
	id          int
	plugin      *pluginData
	startTime   time.Time
	initialized bool
	config      interface{}
	handlers    map[string]func(kong *pdk.PDK)
	lastEvent   time.Time
}

type (
	certificater interface{ Certificate(*pdk.PDK) }
	rewriter     interface{ Rewrite(*pdk.PDK) }
	accesser     interface{ Access(*pdk.PDK) }
	prereader    interface{ Preread(*pdk.PDK) }
	logger       interface{ Log(*pdk.PDK) }
)

func getHandlers(config interface{}) map[string]func(kong *pdk.PDK) {
	handlers := map[string]func(kong *pdk.PDK){}

	if h, ok := config.(certificater); ok { handlers["certificate"]   = h.Certificate  }
	if h, ok := config.(rewriter)    ; ok { handlers["rewrite"]       = h.Rewrite      }
	if h, ok := config.(accesser)    ; ok { handlers["access"]        = h.Access       }
	if h, ok := config.(prereader)   ; ok { handlers["preread"]       = h.Preread      }
	if h, ok := config.(logger)      ; ok { handlers["log"]           = h.Log          }

	return handlers
}


func (s *PluginServer) expireInstances() error {
	const instanceTimeout = 60
	expirationCutoff := time.Now().Add(time.Second * -instanceTimeout)

	oldinstances := map[int]bool{}
	for id, inst := range s.instances {
		if inst.startTime.Before(expirationCutoff) && inst.lastEvent.Before(expirationCutoff) {
			oldinstances[id] = true
		}
	}

	for _, evt := range s.events {
		instId := evt.instance.id
		if _, ok := oldinstances[instId]; ok {
			delete(oldinstances, instId)
		}
	}

	for id := range oldinstances {
		inst := s.instances[id]
		log.Printf("closing instance %#v:%v", inst.plugin.name, inst.id)
		delete(s.instances, id)
	}

	return nil
}

// Configuration data for a new plugin instance.
type PluginConfig struct {
	Name   string // plugin name
	Config []byte // configuration data, as a JSON string
}

// Current state of a plugin instance.  TODO: add some statistics
type InstanceStatus struct {
	Name      string      // plugin name
	Id        int         // instance id
	Config    interface{} // configuration data, decoded
	StartTime int64
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

	plug.lock.Lock()
	defer plug.lock.Unlock()

	instanceConfig := plug.constructor()

	if err := json.Unmarshal(config.Config, instanceConfig); err != nil {
		return fmt.Errorf("Decoding config: %w", err)
	}

	instance := instanceData{
		plugin:    plug,
		startTime: time.Now(),
		config:    instanceConfig,
		handlers:  getHandlers(instanceConfig),
	}

	s.lock.Lock()
	instance.id = s.nextInstanceId
	s.nextInstanceId++
	s.instances[instance.id] = &instance

	plug.lastStartInstance = instance.startTime
	s.expireInstances()

	s.lock.Unlock()

	*status = InstanceStatus{
		Name:      config.Name,
		Id:        instance.id,
		Config:    instance.config,
		StartTime: instance.startTime.Unix(),
	}

	log.Printf("Started instance %#v:%v", config.Name, instance.id)


	return nil
}

// InstanceStatus returns a given resource's status (the same given when started)
//
// RPC exported method
func (s *PluginServer) InstanceStatus(id int, status *InstanceStatus) error {
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
func (s *PluginServer) CloseInstance(id int, status *InstanceStatus) error {
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

	log.Printf("closed instance %#v:%v", instance.plugin.name, instance.id)

	s.lock.Lock()
	instance.plugin.lastCloseInstance = time.Now()
	delete(s.instances, id)
	s.expireInstances()
	s.lock.Unlock()

	return nil
}
